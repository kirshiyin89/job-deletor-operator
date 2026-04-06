package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batchv1alpha1 "github.com/kirshiyin89/jobdeletor-operator/api/v1alpha1"
)

var _ = Describe("JobDeletor Controller", func() {

	Context("When reconciling a resource", func() {

		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {

			resource := &batchv1alpha1.JobDeletor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: batchv1alpha1.JobDeletorSpec{
					MaxAge:               "1m",
					CheckIntervalSeconds: 60,
				},
			}

			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {

			By("removing the custom resource")

			Eventually(func() error {

				found := &batchv1alpha1.JobDeletor{}

				if err := k8sClient.Get(ctx, typeNamespacedName, found); err != nil {
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				}

				found.Finalizers = nil
				return k8sClient.Update(ctx, found)

			}, "10s", "100ms").Should(Succeed())

			found := &batchv1alpha1.JobDeletor{}

			if err := k8sClient.Get(ctx, typeNamespacedName, found); err == nil {
				Expect(k8sClient.Delete(ctx, found)).To(Succeed())
			}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, &batchv1alpha1.JobDeletor{})
				return errors.IsNotFound(err)
			}, "10s", "100ms").Should(BeTrue())
		})

		It("should initialize status and finalizer", func() {

			Eventually(func(g Gomega) {

				obj := &batchv1alpha1.JobDeletor{}
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, obj)).To(Succeed())

				g.Expect(obj.Status.Phase).NotTo(BeEmpty())
				g.Expect(obj.Finalizers).NotTo(BeEmpty())

			}).Should(Succeed())

		})

		It("should delete a completed Job older than MaxAge", func() {

			By("waiting until the controller becomes active")

			Eventually(func(g Gomega) {

				fresh := &batchv1alpha1.JobDeletor{}
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, fresh)).To(Succeed())
				g.Expect(fresh.Status.Phase).To(Equal(PhaseActive))

			}, "15s", "250ms").Should(Succeed())

			By("creating an old completed job")

			oldJob := makeCompletedJob("old-job", "default")
			Expect(k8sClient.Create(ctx, oldJob)).To(Succeed())

			oldJob.Status = batchv1.JobStatus{
				StartTime:      &metav1.Time{Time: time.Now().Add(-49 * time.Hour)},
				CompletionTime: &metav1.Time{Time: time.Now().Add(-48 * time.Hour)},
				Conditions: []batchv1.JobCondition{
					{
						Type:               batchv1.JobSuccessCriteriaMet,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               batchv1.JobComplete,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
				},
			}

			Expect(k8sClient.Status().Update(ctx, oldJob)).To(Succeed())

			By("triggering a reconcile by updating the spec")

			Eventually(func() error {

				existing := &batchv1alpha1.JobDeletor{}

				if err := k8sClient.Get(ctx, typeNamespacedName, existing); err != nil {
					return err
				}

				existing.Spec.MaxAge = "1h"
				existing.Spec.Namespaces = []string{"default"}

				return k8sClient.Update(ctx, existing)

			}, "10s", "100ms").Should(Succeed())

			By("waiting for the job to be deleted")

			Eventually(func() bool {

				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "old-job",
					Namespace: "default",
				}, &batchv1.Job{})

				return errors.IsNotFound(err)

			}, "30s", "250ms").Should(BeTrue())

			By("verifying the JobsDeleted counter")

			Eventually(func() int32 {

				fetched := &batchv1alpha1.JobDeletor{}

				if err := k8sClient.Get(ctx, typeNamespacedName, fetched); err != nil {
					return 0
				}

				return fetched.Status.JobsDeleted

			}, "10s", "250ms").Should(BeNumerically(">=", 1))

		})
	})
})

// helper to create a minimal Job
func makeCompletedJob(name, namespace string) *batchv1.Job {

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "worker", Image: "busybox:latest"},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
}
