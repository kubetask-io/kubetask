// Copyright Contributors to the KubeTask project

//go:build integration

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubetaskv1alpha1 "github.com/xuezhaojun/kubetask/api/v1alpha1"
)

var _ = Describe("CronTask Controller", func() {
	const (
		cronTaskName      = "test-crontask"
		cronTaskNamespace = "default"
	)

	Context("When creating a CronTask", func() {
		It("Should create Tasks based on schedule", func() {
			By("Creating a CronTask with a schedule that triggers immediately")
			// Use a schedule that should trigger immediately (every minute)
			cronTask := &kubetaskv1alpha1.CronTask{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronTaskName,
					Namespace: cronTaskNamespace,
				},
				Spec: kubetaskv1alpha1.CronTaskSpec{
					Schedule:          "* * * * *", // Every minute
					ConcurrencyPolicy: kubetaskv1alpha1.ForbidConcurrent,
					TaskTemplate: kubetaskv1alpha1.TaskTemplateSpec{
						Spec: kubetaskv1alpha1.TaskSpec{
							Contexts: []kubetaskv1alpha1.Context{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/task.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: stringPtr("Test task from CronTask"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronTask)).Should(Succeed())

			By("Checking CronTask was created")
			cronTaskLookupKey := types.NamespacedName{Name: cronTaskName, Namespace: cronTaskNamespace}
			createdCronTask := &kubetaskv1alpha1.CronTask{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cronTaskLookupKey, createdCronTask)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Checking that a Task is created eventually")
			taskList := &kubetaskv1alpha1.TaskList{}
			Eventually(func() int {
				err := k8sClient.List(ctx, taskList, client.InNamespace(cronTaskNamespace))
				if err != nil {
					return 0
				}
				// Count tasks owned by this CronTask
				count := 0
				for _, task := range taskList.Items {
					if task.Labels[CronTaskLabelKey] == cronTaskName {
						count++
					}
				}
				return count
			}, timeout*3, interval).Should(BeNumerically(">=", 1))

			By("Checking the Task has correct labels")
			for _, task := range taskList.Items {
				if task.Labels[CronTaskLabelKey] == cronTaskName {
					Expect(task.Labels[CronTaskLabelKey]).To(Equal(cronTaskName))
					Expect(task.Annotations[ScheduledTimeAnnotation]).NotTo(BeEmpty())
				}
			}

			By("Checking CronTask status is updated")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cronTaskLookupKey, createdCronTask)
				if err != nil {
					return false
				}
				return createdCronTask.Status.LastScheduleTime != nil
			}, timeout*3, interval).Should(BeTrue())

			By("Cleaning up CronTask")
			Expect(k8sClient.Delete(ctx, cronTask)).Should(Succeed())
		})
	})

	Context("When CronTask is suspended", func() {
		It("Should not create new Tasks", func() {
			suspendedCronTaskName := "suspended-crontask"

			By("Creating a suspended CronTask")
			suspended := true
			cronTask := &kubetaskv1alpha1.CronTask{
				ObjectMeta: metav1.ObjectMeta{
					Name:      suspendedCronTaskName,
					Namespace: cronTaskNamespace,
				},
				Spec: kubetaskv1alpha1.CronTaskSpec{
					Schedule:          "* * * * *",
					Suspend:           &suspended,
					ConcurrencyPolicy: kubetaskv1alpha1.ForbidConcurrent,
					TaskTemplate: kubetaskv1alpha1.TaskTemplateSpec{
						Spec: kubetaskv1alpha1.TaskSpec{
							Contexts: []kubetaskv1alpha1.Context{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/task.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: stringPtr("Suspended task"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronTask)).Should(Succeed())

			By("Waiting a bit to ensure no Tasks are created")
			time.Sleep(2 * time.Second)

			By("Checking no Tasks are created for suspended CronTask")
			taskList := &kubetaskv1alpha1.TaskList{}
			Consistently(func() int {
				err := k8sClient.List(ctx, taskList, client.InNamespace(cronTaskNamespace))
				if err != nil {
					return -1
				}
				count := 0
				for _, task := range taskList.Items {
					if task.Labels[CronTaskLabelKey] == suspendedCronTaskName {
						count++
					}
				}
				return count
			}, time.Second*3, interval).Should(Equal(0))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronTask)).Should(Succeed())
		})
	})

	Context("When CronTask has history limits", func() {
		It("Should clean up old Tasks based on limits", func() {
			historyLimitCronTaskName := "history-limit-crontask"

			By("Creating a CronTask with low history limits")
			successLimit := int32(1)
			failedLimit := int32(1)
			cronTask := &kubetaskv1alpha1.CronTask{
				ObjectMeta: metav1.ObjectMeta{
					Name:      historyLimitCronTaskName,
					Namespace: cronTaskNamespace,
				},
				Spec: kubetaskv1alpha1.CronTaskSpec{
					Schedule:                    "* * * * *",
					ConcurrencyPolicy:           kubetaskv1alpha1.AllowConcurrent,
					SuccessfulTasksHistoryLimit: &successLimit,
					FailedTasksHistoryLimit:     &failedLimit,
					TaskTemplate: kubetaskv1alpha1.TaskTemplateSpec{
						Spec: kubetaskv1alpha1.TaskSpec{
							Contexts: []kubetaskv1alpha1.Context{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/task.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: stringPtr("History limit test"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronTask)).Should(Succeed())

			By("Waiting for CronTask to be created")
			cronTaskLookupKey := types.NamespacedName{Name: historyLimitCronTaskName, Namespace: cronTaskNamespace}
			createdCronTask := &kubetaskv1alpha1.CronTask{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cronTaskLookupKey, createdCronTask)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronTask)).Should(Succeed())
		})
	})
})

// stringPtr returns a pointer to the given string
func stringPtr(s string) *string {
	return &s
}

// Helper to get unique CronTask name for tests
func uniqueCronTaskName(base string) string {
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}
