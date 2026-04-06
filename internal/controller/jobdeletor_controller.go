package controller

import (
	"context"
	"fmt"
	"time"

	batchv1alpha1 "github.com/kirshiyin89/jobdeletor-operator/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	jobDeletorFinalizer = "batch.curiousdevscorner.org/finalizer"

	PhaseStarting = "Starting"
	PhaseActive   = "Active"
	PhaseFailed   = "Failed"
)

var jobsDeletedTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "jobdeletor_jobs_deleted_total",
		Help: "Total number of Jobs deleted by the operator",
	},
)

func init() {
	metrics.Registry.MustRegister(jobsDeletedTotal)
}

type JobDeletorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=batch.curiousdevscorner.org,resources=jobdeletors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch.curiousdevscorner.org,resources=jobdeletors/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch.curiousdevscorner.org,resources=jobdeletors/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
func (r *JobDeletorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	jobDeletor := &batchv1alpha1.JobDeletor{}
	if err := r.Get(ctx, req.NamespacedName, jobDeletor); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("JobDeletor resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get JobDeletor")
		return ctrl.Result{}, err
	}

	// Initialize status if needed
	if jobDeletor.Status.Phase == "" {
		jobDeletor.Status.Phase = PhaseStarting
		jobDeletor.Status.Message = "JobDeletor is starting"
		if err := r.Status().Update(ctx, jobDeletor); err != nil {
			logger.Error(err, "Failed to update JobDeletor status")
			return ctrl.Result{}, err
		}
		r.Recorder.Event(jobDeletor, corev1.EventTypeNormal, "Started", "JobDeletor is starting")
		logger.Info("Initialized JobDeletor status")
		return ctrl.Result{}, nil
	}

	// refetch the jobdeletor to ensure we work with a fresh copy
	if err := r.Get(ctx, req.NamespacedName, &batchv1alpha1.JobDeletor{}); err != nil {
		logger.Error(err, "Failed to re-fetch JobDeletor")
		return ctrl.Result{}, err
	}

	// Set phase to Active
	if jobDeletor.Status.Phase == PhaseStarting {
		jobDeletor.Status.Phase = PhaseActive
		jobDeletor.Status.Message = "JobDeletor is actively monitoring and deleting old Jobs"
		if err := r.Status().Update(ctx, jobDeletor); err != nil {
			logger.Error(err, "Failed to update status to Active")
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, jobDeletor); err != nil {
			logger.Error(err, "Failed to re-fetch after Active update")
			return ctrl.Result{}, err
		}
		r.Recorder.Event(jobDeletor, corev1.EventTypeNormal, "Active", "JobDeletor is now active")
	}

	if err := r.Get(ctx, req.NamespacedName, jobDeletor); err != nil {
		logger.Error(err, "Failed to re-fetch JobDeletor")
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer
	if jobDeletor.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(jobDeletor, jobDeletorFinalizer) {
			controllerutil.AddFinalizer(jobDeletor, jobDeletorFinalizer)
			if err := r.Update(ctx, jobDeletor); err != nil {
				logger.Error(err, "Failed to add finalizer")
				return ctrl.Result{}, err
			}
			logger.Info("Added finalizer to JobDeletor")
		}
	} else {
		if controllerutil.ContainsFinalizer(jobDeletor, jobDeletorFinalizer) {
			if err := r.finalizeJobDeletor(ctx, jobDeletor); err != nil {
				logger.Error(err, "Failed to finalize JobDeletor")
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(jobDeletor, jobDeletorFinalizer)
			if err := r.Update(ctx, jobDeletor); err != nil {
				logger.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
			logger.Info("Successfully finalized JobDeletor")
		}
		return ctrl.Result{}, nil
	}

	// Delete old jobs. The function returns the list of deleted job names
	// so we can update all status fields in a single Status().Update() call
	// here in Reconcile, avoiding cache-race.
	deletedJobs, err := r.deleteOldJobs(ctx, jobDeletor)
	if err != nil {
		jobDeletor.Status.Phase = PhaseFailed
		jobDeletor.Status.Message = fmt.Sprintf("Failed to delete old Jobs: %v", err)
		if statusErr := r.Status().Update(ctx, jobDeletor); statusErr != nil {
			logger.Error(statusErr, "Failed to update status to Failed")
		}
		return ctrl.Result{}, err
	}

	// Update all status fields in one shot.
	// Doing this here — after deleteOldJobs returns — means we only ever
	// call Status().Update() once per reconcile cycle at this point, so
	// there is no intermediate ResourceVersion change that could conflict.
	if len(deletedJobs) > 0 {
		jobDeletor.Status.JobsDeleted += int32(len(deletedJobs))
		jobDeletor.Status.LastDeletedJobs = append(deletedJobs, jobDeletor.Status.LastDeletedJobs...)
		if len(jobDeletor.Status.LastDeletedJobs) > 10 {
			jobDeletor.Status.LastDeletedJobs = jobDeletor.Status.LastDeletedJobs[:10]
		}
	}
	now := metav1.Now()
	jobDeletor.Status.LastCheckTime = &now
	if err := r.Status().Update(ctx, jobDeletor); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	checkInterval := time.Duration(jobDeletor.Spec.CheckIntervalSeconds) * time.Second
	if checkInterval == 0 {
		checkInterval = 3600 * time.Second
	}

	logger.Info("Reconciliation complete", "requeueAfter", checkInterval)
	return ctrl.Result{RequeueAfter: checkInterval}, nil
}

// deleteOldJobs finds and deletes Jobs older than MaxAge.
// It returns the list of deleted job names so the caller can update status
// in a single write, avoiding intermediate ResourceVersion conflicts.
func (r *JobDeletorReconciler) deleteOldJobs(ctx context.Context, jobDeletor *batchv1alpha1.JobDeletor) ([]string, error) {
	logger := log.FromContext(ctx)

	namespaces := jobDeletor.Spec.Namespaces
	if len(namespaces) == 0 {
		namespaceList := &corev1.NamespaceList{}
		if err := r.List(ctx, namespaceList); err != nil {
			return nil, fmt.Errorf("failed to list namespaces: %w", err)
		}
		for _, ns := range namespaceList.Items {
			namespaces = append(namespaces, ns.Name)
		}
	}

	maxAge, err := time.ParseDuration(jobDeletor.Spec.MaxAge)
	if err != nil {
		return nil, fmt.Errorf("invalid maxAge value: %w", err)
	}

	cutoffTime := time.Now().Add(-maxAge)
	var deletedJobs []string

	for _, namespace := range namespaces {
		jobList := &batchv1.JobList{}
		listOpts := []client.ListOption{client.InNamespace(namespace)}

		if len(jobDeletor.Spec.Selector) > 0 {
			listOpts = append(listOpts, client.MatchingLabels(jobDeletor.Spec.Selector))
		}

		if err := r.List(ctx, jobList, listOpts...); err != nil {
			return nil, fmt.Errorf("failed to list jobs in namespace %s: %w", namespace, err)
		}

		for _, job := range jobList.Items {
			if job.DeletionTimestamp != nil {
				continue
			}
			if job.Status.CompletionTime == nil || !job.Status.CompletionTime.Time.Before(cutoffTime) {
				continue
			}

			logger.Info("Deleting old Job",
				"job", job.Name,
				"namespace", job.Namespace,
				"completionTime", job.Status.CompletionTime.Time,
				"age", time.Since(job.Status.CompletionTime.Time))

			deleteOptions := client.DeleteOptions{}
			propagationPolicy := metav1.DeletePropagationBackground
			deleteOptions.PropagationPolicy = &propagationPolicy

			if err := r.Delete(ctx, &job, &deleteOptions); err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Failed to delete Job", "job", job.Name, "namespace", job.Namespace)
					continue
				}
				logger.Info("Job already deleted, skipping", "job", job.Name, "namespace", job.Namespace)
				continue
			}

			deletedJobs = append(deletedJobs, fmt.Sprintf("%s/%s", job.Namespace, job.Name))
			jobsDeletedTotal.Inc()
			logger.Info("Metric incremented", "metric", "jobdeletor_jobs_deleted_total", "job", job.Name)
			r.Recorder.Eventf(jobDeletor, corev1.EventTypeNormal, "JobDeleted",
				"Successfully deleted Job %s/%s", job.Namespace, job.Name)
		}
	}

	if len(deletedJobs) > 0 {
		logger.Info("Deleted jobs", "count", len(deletedJobs), "jobs", deletedJobs)
	}

	return deletedJobs, nil
}

func (r *JobDeletorReconciler) finalizeJobDeletor(ctx context.Context, jobDeletor *batchv1alpha1.JobDeletor) error {
	logger := log.FromContext(ctx)
	logger.Info("Finalizing JobDeletor", "name", jobDeletor.Name)
	logger.Info("JobDeletor finalized successfully", "totalJobsDeleted", jobDeletor.Status.JobsDeleted)
	return nil
}

func (r *JobDeletorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// enable event recording
	r.Recorder = mgr.GetEventRecorderFor("jobdeletor-operator")
	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1alpha1.JobDeletor{}).
		Complete(r)
}
