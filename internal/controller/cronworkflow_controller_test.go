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

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var _ = Describe("CronWorkflow Controller", func() {
	const (
		cronWorkflowNamespace = "default"
	)

	Context("When creating a CronWorkflow with inline workflow", func() {
		It("Should create WorkflowRuns based on schedule", func() {
			cronWorkflowName := uniqueCronWorkflowName("inline-cron-wf")

			By("Creating a CronWorkflow with inline workflow spec")
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: cronWorkflowNamespace,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule: "* * * * *", // Every minute
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Name: "stage-1",
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "task-1",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("CronWorkflow task"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			By("Checking CronWorkflow was created")
			cronWorkflowKey := types.NamespacedName{Name: cronWorkflowName, Namespace: cronWorkflowNamespace}
			createdCronWorkflow := &kubetaskv1alpha1.CronWorkflow{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cronWorkflowKey, createdCronWorkflow)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Setting fake clock to after CronWorkflow creation time to trigger schedule")
			fakeClock.SetTime(createdCronWorkflow.CreationTimestamp.Time.Add(time.Minute))

			By("Checking that a WorkflowRun is created eventually")
			runList := &kubetaskv1alpha1.WorkflowRunList{}
			Eventually(func() int {
				err := k8sClient.List(ctx, runList, client.InNamespace(cronWorkflowNamespace))
				if err != nil {
					return 0
				}
				count := 0
				for _, run := range runList.Items {
					if run.Labels[CronWorkflowLabelKey] == cronWorkflowName {
						count++
					}
				}
				return count
			}, timeout*3, interval).Should(BeNumerically(">=", 1))

			By("Checking the WorkflowRun has correct labels and annotations")
			for _, run := range runList.Items {
				if run.Labels[CronWorkflowLabelKey] == cronWorkflowName {
					Expect(run.Labels[CronWorkflowLabelKey]).To(Equal(cronWorkflowName))
					Expect(run.Annotations[CronWorkflowScheduledTimeAnnotation]).NotTo(BeEmpty())
					// Verify the inline spec was copied
					Expect(run.Spec.Inline).NotTo(BeNil())
					Expect(run.Spec.Inline.Stages).To(HaveLen(1))
					Expect(run.Spec.Inline.Stages[0].Name).To(Equal("stage-1"))
				}
			}

			By("Checking CronWorkflow status is updated")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cronWorkflowKey, createdCronWorkflow)
				if err != nil {
					return false
				}
				return createdCronWorkflow.Status.LastScheduleTime != nil || len(createdCronWorkflow.Status.Active) > 0
			}, timeout*3, interval).Should(BeTrue())

			By("Cleaning up CronWorkflow")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())
		})
	})

	Context("When creating a CronWorkflow with workflowRef", func() {
		It("Should create WorkflowRuns that reference the Workflow", func() {
			workflowName := uniqueCronWorkflowName("template-wf")
			cronWorkflowName := uniqueCronWorkflowName("ref-cron-wf")

			By("Creating a Workflow template")
			workflow := &kubetaskv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowName,
					Namespace: cronWorkflowNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowSpec{
					Stages: []kubetaskv1alpha1.WorkflowStage{
						{
							Name: "build",
							Tasks: []kubetaskv1alpha1.WorkflowTask{
								{
									Name: "compile",
									Spec: kubetaskv1alpha1.TaskSpec{
										Description: stringPtr("Compile code"),
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflow)).Should(Succeed())

			By("Creating a CronWorkflow that references the Workflow")
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: cronWorkflowNamespace,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule:    "* * * * *",
					WorkflowRef: workflowName,
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			By("Checking CronWorkflow was created")
			cronWorkflowKey := types.NamespacedName{Name: cronWorkflowName, Namespace: cronWorkflowNamespace}
			createdCronWorkflow := &kubetaskv1alpha1.CronWorkflow{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cronWorkflowKey, createdCronWorkflow)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Setting fake clock to after CronWorkflow creation time to trigger schedule")
			fakeClock.SetTime(createdCronWorkflow.CreationTimestamp.Time.Add(time.Minute))

			By("Checking that a WorkflowRun is created with workflowRef")
			runList := &kubetaskv1alpha1.WorkflowRunList{}
			Eventually(func() int {
				err := k8sClient.List(ctx, runList, client.InNamespace(cronWorkflowNamespace))
				if err != nil {
					return 0
				}
				count := 0
				for _, run := range runList.Items {
					if run.Labels[CronWorkflowLabelKey] == cronWorkflowName {
						count++
					}
				}
				return count
			}, timeout*3, interval).Should(BeNumerically(">=", 1))

			By("Checking the WorkflowRun has workflowRef set")
			for _, run := range runList.Items {
				if run.Labels[CronWorkflowLabelKey] == cronWorkflowName {
					Expect(run.Spec.WorkflowRef).To(Equal(workflowName))
					Expect(run.Spec.Inline).To(BeNil())
				}
			}

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, workflow)).Should(Succeed())
		})
	})

	Context("When CronWorkflow is suspended", func() {
		It("Should not create new WorkflowRuns", func() {
			cronWorkflowName := uniqueCronWorkflowName("suspended-cron-wf")

			By("Creating a suspended CronWorkflow")
			suspended := true
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: cronWorkflowNamespace,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule: "* * * * *",
					Suspend:  &suspended,
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "suspended-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Should not run"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			By("Waiting a bit to ensure no WorkflowRuns are created")
			time.Sleep(2 * time.Second)

			By("Checking no WorkflowRuns are created for suspended CronWorkflow")
			runList := &kubetaskv1alpha1.WorkflowRunList{}
			Consistently(func() int {
				err := k8sClient.List(ctx, runList, client.InNamespace(cronWorkflowNamespace))
				if err != nil {
					return -1
				}
				count := 0
				for _, run := range runList.Items {
					if run.Labels[CronWorkflowLabelKey] == cronWorkflowName {
						count++
					}
				}
				return count
			}, time.Second*3, interval).Should(Equal(0))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())
		})
	})

	Context("When CronWorkflow has active WorkflowRun (Forbid policy)", func() {
		It("Should not create new WorkflowRuns while one is active", func() {
			cronWorkflowName := uniqueCronWorkflowName("forbid-cron-wf")

			By("Creating a CronWorkflow")
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: cronWorkflowNamespace,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule: "* * * * *",
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "long-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Long running task"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			By("Checking CronWorkflow was created")
			cronWorkflowKey := types.NamespacedName{Name: cronWorkflowName, Namespace: cronWorkflowNamespace}
			createdCronWorkflow := &kubetaskv1alpha1.CronWorkflow{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cronWorkflowKey, createdCronWorkflow)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Setting fake clock to trigger first run")
			fakeClock.SetTime(createdCronWorkflow.CreationTimestamp.Time.Add(time.Minute))

			By("Waiting for first WorkflowRun to be created")
			runList := &kubetaskv1alpha1.WorkflowRunList{}
			Eventually(func() int {
				err := k8sClient.List(ctx, runList, client.InNamespace(cronWorkflowNamespace))
				if err != nil {
					return 0
				}
				count := 0
				for _, run := range runList.Items {
					if run.Labels[CronWorkflowLabelKey] == cronWorkflowName {
						count++
					}
				}
				return count
			}, timeout*3, interval).Should(BeNumerically(">=", 1))

			firstRunCount := 0
			for _, run := range runList.Items {
				if run.Labels[CronWorkflowLabelKey] == cronWorkflowName {
					firstRunCount++
				}
			}

			By("Advancing clock by another minute (should not create new run due to Forbid policy)")
			fakeClock.Advance(time.Minute)

			By("Checking no additional WorkflowRuns are created while one is active")
			Consistently(func() int {
				err := k8sClient.List(ctx, runList, client.InNamespace(cronWorkflowNamespace))
				if err != nil {
					return -1
				}
				count := 0
				for _, run := range runList.Items {
					if run.Labels[CronWorkflowLabelKey] == cronWorkflowName {
						count++
					}
				}
				return count
			}, time.Second*3, interval).Should(Equal(firstRunCount))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())
		})
	})

	Context("When CronWorkflow has invalid schedule", func() {
		It("Should set error condition in status", func() {
			cronWorkflowName := uniqueCronWorkflowName("invalid-sched-cron-wf")

			By("Creating a CronWorkflow with invalid schedule")
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: cronWorkflowNamespace,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule: "invalid-schedule",
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Test"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			By("Checking CronWorkflow status has error condition")
			cronWorkflowKey := types.NamespacedName{Name: cronWorkflowName, Namespace: cronWorkflowNamespace}
			createdCronWorkflow := &kubetaskv1alpha1.CronWorkflow{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cronWorkflowKey, createdCronWorkflow)
				if err != nil {
					return false
				}
				for _, cond := range createdCronWorkflow.Status.Conditions {
					if cond.Type == "Scheduled" && cond.Status == metav1.ConditionFalse && cond.Reason == "InvalidSchedule" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())
		})
	})
})

// uniqueCronWorkflowName generates a unique CronWorkflow name for tests
func uniqueCronWorkflowName(base string) string {
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}
