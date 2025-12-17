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

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var _ = Describe("WorkflowRun Controller", func() {
	const (
		workflowRunNamespace = "default"
	)

	Context("When creating a WorkflowRun with inline spec", func() {
		It("Should create Tasks for each stage sequentially", func() {
			workflowRunName := uniqueWorkflowRunName("inline-workflow-run")

			By("Creating a WorkflowRun with inline spec containing 2 stages")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: workflowRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Name: "stage-lint",
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "lint",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Run linting"),
										},
									},
								},
							},
							{
								Name: "stage-test",
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "test-unit",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Run unit tests"),
										},
									},
									{
										Name: "test-e2e",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Run e2e tests"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Checking WorkflowRun was created and initialized")
			workflowRunLookupKey := types.NamespacedName{Name: workflowRunName, Namespace: workflowRunNamespace}
			createdWorkflowRun := &kubetaskv1alpha1.WorkflowRun{}
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				err := k8sClient.Get(ctx, workflowRunLookupKey, createdWorkflowRun)
				if err != nil {
					return ""
				}
				return createdWorkflowRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseRunning))

			By("Checking stage 0 Task was created")
			stage0TaskName := fmt.Sprintf("%s-lint", workflowRunName)
			stage0Task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: stage0TaskName, Namespace: workflowRunNamespace}, stage0Task)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying stage 0 Task has correct labels")
			Expect(stage0Task.Labels[WorkflowRunLabelKey]).To(Equal(workflowRunName))
			Expect(stage0Task.Labels[WorkflowRunStageLabelKey]).To(Equal("stage-lint"))
			Expect(stage0Task.Labels[WorkflowRunStageIndexLabelKey]).To(Equal("0"))

			By("Verifying stage 0 Task has no depends-on annotation")
			_, hasDependsOn := stage0Task.Annotations[WorkflowRunDependsOnAnnotation]
			Expect(hasDependsOn).To(BeFalse())

			By("Checking WorkflowRun status shows currentStage = 0")
			Expect(createdWorkflowRun.Status.CurrentStage).To(Equal(int32(0)))
			Expect(createdWorkflowRun.Status.TotalTasks).To(Equal(int32(3)))

			By("Cleaning up WorkflowRun")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("When creating a WorkflowRun with workflowRef", func() {
		It("Should resolve Workflow template and create Tasks", func() {
			workflowName := uniqueWorkflowName("template-workflow")
			workflowRunName := uniqueWorkflowRunName("ref-workflow-run")

			By("Creating a Workflow template")
			workflow := &kubetaskv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowName,
					Namespace: workflowRunNamespace,
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

			By("Creating a WorkflowRun that references the Workflow")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: workflowRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					WorkflowRef: workflowName,
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Checking WorkflowRun was created and initialized")
			workflowRunLookupKey := types.NamespacedName{Name: workflowRunName, Namespace: workflowRunNamespace}
			createdWorkflowRun := &kubetaskv1alpha1.WorkflowRun{}
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				err := k8sClient.Get(ctx, workflowRunLookupKey, createdWorkflowRun)
				if err != nil {
					return ""
				}
				return createdWorkflowRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseRunning))

			By("Checking Task was created with workflow ref label")
			taskName := fmt.Sprintf("%s-compile", workflowRunName)
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: taskName, Namespace: workflowRunNamespace}, task)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(task.Labels[WorkflowRunLabelKey]).To(Equal(workflowRunName))
			Expect(task.Labels[WorkflowRefLabelKey]).To(Equal(workflowName))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, workflow)).Should(Succeed())
		})
	})

	Context("When stage name is not specified", func() {
		It("Should auto-generate stage names", func() {
			workflowRunName := uniqueWorkflowRunName("autogen-stage-run")

			By("Creating a WorkflowRun without stage names")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: workflowRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "first-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("First task"),
										},
									},
								},
							},
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "second-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Second task"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Checking WorkflowRun status has auto-generated stage names")
			workflowRunLookupKey := types.NamespacedName{Name: workflowRunName, Namespace: workflowRunNamespace}
			createdWorkflowRun := &kubetaskv1alpha1.WorkflowRun{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, workflowRunLookupKey, createdWorkflowRun)
				if err != nil {
					return false
				}
				return len(createdWorkflowRun.Status.StageStatuses) == 2
			}, timeout, interval).Should(BeTrue())

			Expect(createdWorkflowRun.Status.StageStatuses[0].Name).To(Equal("stage-0"))
			Expect(createdWorkflowRun.Status.StageStatuses[1].Name).To(Equal("stage-1"))

			By("Checking Task has auto-generated stage label")
			taskName := fmt.Sprintf("%s-first-task", workflowRunName)
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: taskName, Namespace: workflowRunNamespace}, task)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			Expect(task.Labels[WorkflowRunStageLabelKey]).To(Equal("stage-0"))

			By("Cleaning up WorkflowRun")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("When stage Tasks complete", func() {
		It("Should advance to next stage and add depends-on annotation", func() {
			workflowRunName := uniqueWorkflowRunName("advance-stage-run")

			By("Creating a WorkflowRun with 2 stages")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: workflowRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "stage0-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Stage 0 task"),
										},
									},
								},
							},
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "stage1-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Stage 1 task"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Waiting for stage 0 Task to be created")
			stage0TaskName := fmt.Sprintf("%s-stage0-task", workflowRunName)
			stage0Task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: stage0TaskName, Namespace: workflowRunNamespace}, stage0Task)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Manually marking stage 0 Task as Completed")
			stage0Task.Status.Phase = kubetaskv1alpha1.TaskPhaseCompleted
			Expect(k8sClient.Status().Update(ctx, stage0Task)).Should(Succeed())

			By("Waiting for stage 1 Task to be created")
			stage1TaskName := fmt.Sprintf("%s-stage1-task", workflowRunName)
			stage1Task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: stage1TaskName, Namespace: workflowRunNamespace}, stage1Task)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying stage 1 Task has depends-on annotation")
			Expect(stage1Task.Annotations[WorkflowRunDependsOnAnnotation]).To(Equal(stage0TaskName))

			By("Checking WorkflowRun advanced to stage 1")
			workflowRunLookupKey := types.NamespacedName{Name: workflowRunName, Namespace: workflowRunNamespace}
			updatedWorkflowRun := &kubetaskv1alpha1.WorkflowRun{}
			Eventually(func() int32 {
				err := k8sClient.Get(ctx, workflowRunLookupKey, updatedWorkflowRun)
				if err != nil {
					return -1
				}
				return updatedWorkflowRun.Status.CurrentStage
			}, timeout, interval).Should(Equal(int32(1)))

			By("Cleaning up WorkflowRun")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("When all stages complete", func() {
		It("Should mark WorkflowRun as Completed", func() {
			workflowRunName := uniqueWorkflowRunName("complete-workflow-run")

			By("Creating a WorkflowRun with 1 stage")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: workflowRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "only-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("The only task"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Waiting for Task to be created")
			taskName := fmt.Sprintf("%s-only-task", workflowRunName)
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: taskName, Namespace: workflowRunNamespace}, task)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Manually marking Task as Completed")
			task.Status.Phase = kubetaskv1alpha1.TaskPhaseCompleted
			Expect(k8sClient.Status().Update(ctx, task)).Should(Succeed())

			By("Waiting for WorkflowRun to be Completed")
			workflowRunLookupKey := types.NamespacedName{Name: workflowRunName, Namespace: workflowRunNamespace}
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				updatedWorkflowRun := &kubetaskv1alpha1.WorkflowRun{}
				err := k8sClient.Get(ctx, workflowRunLookupKey, updatedWorkflowRun)
				if err != nil {
					return ""
				}
				return updatedWorkflowRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseCompleted))

			By("Cleaning up WorkflowRun")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("When a Task fails", func() {
		It("Should fail the WorkflowRun immediately (Fail Fast)", func() {
			workflowRunName := uniqueWorkflowRunName("fail-fast-run")

			By("Creating a WorkflowRun with 2 stages")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: workflowRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "failing-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("This task will fail"),
										},
									},
								},
							},
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "should-not-run",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("This task should not be created"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Waiting for stage 0 Task to be created")
			failingTaskName := fmt.Sprintf("%s-failing-task", workflowRunName)
			failingTask := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: failingTaskName, Namespace: workflowRunNamespace}, failingTask)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Manually marking Task as Failed")
			failingTask.Status.Phase = kubetaskv1alpha1.TaskPhaseFailed
			Expect(k8sClient.Status().Update(ctx, failingTask)).Should(Succeed())

			By("Waiting for WorkflowRun to be Failed")
			workflowRunLookupKey := types.NamespacedName{Name: workflowRunName, Namespace: workflowRunNamespace}
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				updatedWorkflowRun := &kubetaskv1alpha1.WorkflowRun{}
				err := k8sClient.Get(ctx, workflowRunLookupKey, updatedWorkflowRun)
				if err != nil {
					return ""
				}
				return updatedWorkflowRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseFailed))

			By("Verifying stage 1 Task was never created")
			shouldNotRunTaskName := fmt.Sprintf("%s-should-not-run", workflowRunName)
			shouldNotRunTask := &kubetaskv1alpha1.Task{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: shouldNotRunTaskName, Namespace: workflowRunNamespace}, shouldNotRunTask)
			Expect(err).To(HaveOccurred()) // Task should not exist

			By("Cleaning up WorkflowRun")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("When stage has multiple parallel tasks", func() {
		It("Should create all tasks and set depends-on with multiple task names", func() {
			workflowRunName := uniqueWorkflowRunName("parallel-tasks-run")

			By("Creating a WorkflowRun with parallel tasks in stage 0")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: workflowRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "parallel-a",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Parallel task A"),
										},
									},
									{
										Name: "parallel-b",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Parallel task B"),
										},
									},
								},
							},
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "depends-on-parallel",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Depends on parallel tasks"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Waiting for both stage 0 Tasks to be created")
			taskAName := fmt.Sprintf("%s-parallel-a", workflowRunName)
			taskBName := fmt.Sprintf("%s-parallel-b", workflowRunName)

			taskA := &kubetaskv1alpha1.Task{}
			taskB := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				errA := k8sClient.Get(ctx, types.NamespacedName{Name: taskAName, Namespace: workflowRunNamespace}, taskA)
				errB := k8sClient.Get(ctx, types.NamespacedName{Name: taskBName, Namespace: workflowRunNamespace}, taskB)
				return errA == nil && errB == nil
			}, timeout, interval).Should(BeTrue())

			By("Marking both tasks as Completed")
			taskA.Status.Phase = kubetaskv1alpha1.TaskPhaseCompleted
			Expect(k8sClient.Status().Update(ctx, taskA)).Should(Succeed())
			taskB.Status.Phase = kubetaskv1alpha1.TaskPhaseCompleted
			Expect(k8sClient.Status().Update(ctx, taskB)).Should(Succeed())

			By("Waiting for stage 1 Task to be created")
			dependsOnTaskName := fmt.Sprintf("%s-depends-on-parallel", workflowRunName)
			dependsOnTask := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: dependsOnTaskName, Namespace: workflowRunNamespace}, dependsOnTask)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying stage 1 Task depends-on annotation contains both task names")
			dependsOn := dependsOnTask.Annotations[WorkflowRunDependsOnAnnotation]
			Expect(dependsOn).To(ContainSubstring(taskAName))
			Expect(dependsOn).To(ContainSubstring(taskBName))

			By("Cleaning up WorkflowRun")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("When WorkflowRun references non-existent Workflow", func() {
		It("Should fail with error condition", func() {
			workflowRunName := uniqueWorkflowRunName("missing-workflow-run")

			By("Creating a WorkflowRun that references a non-existent Workflow")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: workflowRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					WorkflowRef: "non-existent-workflow",
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Checking WorkflowRun fails")
			workflowRunLookupKey := types.NamespacedName{Name: workflowRunName, Namespace: workflowRunNamespace}
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				updatedWorkflowRun := &kubetaskv1alpha1.WorkflowRun{}
				err := k8sClient.Get(ctx, workflowRunLookupKey, updatedWorkflowRun)
				if err != nil {
					return ""
				}
				return updatedWorkflowRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseFailed))

			By("Cleaning up WorkflowRun")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})
})

// uniqueWorkflowRunName generates a unique workflow run name for tests
func uniqueWorkflowRunName(base string) string {
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}
