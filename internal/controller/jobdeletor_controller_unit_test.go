package controller

import (
	"context"
	"testing"
	"time"

	batchv1alpha1 "github.com/kirshiyin89/jobdeletor-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDeleteOldJobs(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = batchv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = batchv1alpha1.AddToScheme(scheme)

	now := time.Now()

	oldJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "old-job",
			Namespace: "default",
		},
		Status: batchv1.JobStatus{
			CompletionTime: &metav1.Time{Time: now.Add(-48 * time.Hour)},
		},
	}

	newJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-job",
			Namespace: "default",
		},
		Status: batchv1.JobStatus{
			CompletionTime: &metav1.Time{Time: now.Add(-1 * time.Hour)},
		},
	}

	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}

	jobDeletor := &batchv1alpha1.JobDeletor{
		Spec: batchv1alpha1.JobDeletorSpec{
			MaxAge: "24h",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&oldJob, &newJob, &ns).
		Build()

	reconciler := &JobDeletorReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	deleted, err := reconciler.deleteOldJobs(context.Background(), jobDeletor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted job, got %d", len(deleted))
	}

	if deleted[0] != "default/old-job" {
		t.Fatalf("unexpected deleted job: %v", deleted[0])
	}
}
