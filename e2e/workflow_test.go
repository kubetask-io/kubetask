// Copyright Contributors to the KubeTask project

// Package e2e contains end-to-end tests for KubeTask
package e2e

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

// WorkflowRun label/annotation constants (same as in controller)
const (
	WorkflowRunLabelKey         = "kubetask.io/workflow-run"
	WorkflowRefLabelKey         = "kubetask.io/workflow"
	WorkflowStageLabelKey       = "kubetask.io/stage"
	WorkflowStageIndexLabelKey  = "kubetask.io/stage-index"
	WorkflowDependsOnAnnotation = "kubetask.io/depends-on"
)

var _ = Describe("WorkflowRun E2E Tests", func() {
	var (
		agent     *kubetaskv1alpha1.Agent
		agentName string
	)

	BeforeEach(func() {
		// Create an Agent with echo agent for all tests
		agentName = uniqueName("wf-echo")
		agent = &kubetaskv1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      agentName,
				Namespace: testNS,
			},
			Spec: kubetaskv1alpha1.AgentSpec{
				AgentImage:         echoImage,
				ServiceAccountName: testServiceAccount,
				WorkspaceDir:       "/workspace",
				Command:            []string{"sh", "-c", "echo '=== Workflow Task ===' && cat ${WORKSPACE_DIR}/task.md && echo '=== Done ==='"},
			},
		}
		Expect(k8sClient.Create(ctx, agent)).Should(Succeed())
	})

	AfterEach(func() {
		// Clean up Agent
		if agent != nil {
			_ = k8sClient.Delete(ctx, agent)
		}
	})

	Context("Simple WorkflowRun with single stage (inline)", func() {
		It("should create Task and complete WorkflowRun successfully", func() {
			workflowRunName := uniqueName("wfr-single")

			By("Creating a WorkflowRun with 1 stage and 1 task (inline)")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Name: "only-stage",
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "only-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("This is the only task in the workflow"),
											AgentRef:    agentName,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Waiting for WorkflowRun to initialize and start running")
			workflowRunKey := types.NamespacedName{Name: workflowRunName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				if err := k8sClient.Get(ctx, workflowRunKey, wfr); err != nil {
					return ""
				}
				return wfr.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseRunning))

			By("Verifying Task was created with correct labels")
			taskName := fmt.Sprintf("%s-only-task", workflowRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, taskKey, task) == nil
			}, timeout, interval).Should(BeTrue())

			Expect(task.Labels[WorkflowRunLabelKey]).To(Equal(workflowRunName))
			Expect(task.Labels[WorkflowStageLabelKey]).To(Equal("only-stage"))
			Expect(task.Labels[WorkflowStageIndexLabelKey]).To(Equal("0"))

			By("Waiting for Task to complete")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Waiting for WorkflowRun to complete")
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				if err := k8sClient.Get(ctx, workflowRunKey, wfr); err != nil {
					return ""
				}
				return wfr.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseCompleted))

			By("Verifying WorkflowRun status")
			completedWFR := &kubetaskv1alpha1.WorkflowRun{}
			Expect(k8sClient.Get(ctx, workflowRunKey, completedWFR)).Should(Succeed())
			Expect(completedWFR.Status.TotalTasks).To(Equal(int32(1)))
			Expect(completedWFR.Status.CompletedTasks).To(Equal(int32(1)))
			Expect(completedWFR.Status.CompletionTime).NotTo(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("WorkflowRun with workflowRef", func() {
		It("should resolve Workflow template and execute successfully", func() {
			workflowName := uniqueName("wf-template")
			workflowRunName := uniqueName("wfr-ref")

			By("Creating a Workflow template")
			workflow := &kubetaskv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkflowSpec{
					Stages: []kubetaskv1alpha1.WorkflowStage{
						{
							Name: "build-stage",
							Tasks: []kubetaskv1alpha1.WorkflowTask{
								{
									Name: "build-task",
									Spec: kubetaskv1alpha1.TaskSpec{
										Description: stringPtr("Build task from template"),
										AgentRef:    agentName,
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
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					WorkflowRef: workflowName,
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			By("Waiting for WorkflowRun to start running")
			workflowRunKey := types.NamespacedName{Name: workflowRunName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				if err := k8sClient.Get(ctx, workflowRunKey, wfr); err != nil {
					return ""
				}
				return wfr.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseRunning))

			By("Verifying Task was created with workflow ref label")
			taskName := fmt.Sprintf("%s-build-task", workflowRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, taskKey, task) == nil
			}, timeout, interval).Should(BeTrue())

			Expect(task.Labels[WorkflowRunLabelKey]).To(Equal(workflowRunName))
			Expect(task.Labels[WorkflowRefLabelKey]).To(Equal(workflowName))

			By("Waiting for WorkflowRun to complete")
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				if err := k8sClient.Get(ctx, workflowRunKey, wfr); err != nil {
					return ""
				}
				return wfr.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, workflow)).Should(Succeed())
		})
	})

	Context("Multi-stage WorkflowRun", func() {
		It("should execute stages sequentially and set depends-on annotations", func() {
			workflowRunName := uniqueName("wfr-multi")

			By("Creating a WorkflowRun with 2 stages")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Name: "stage-one",
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "first-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("First task in workflow"),
											AgentRef:    agentName,
										},
									},
								},
							},
							{
								Name: "stage-two",
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "second-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Second task depends on first"),
											AgentRef:    agentName,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			workflowRunKey := types.NamespacedName{Name: workflowRunName, Namespace: testNS}

			By("Waiting for first stage Task to be created")
			firstTaskName := fmt.Sprintf("%s-first-task", workflowRunName)
			firstTaskKey := types.NamespacedName{Name: firstTaskName, Namespace: testNS}
			firstTask := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, firstTaskKey, firstTask) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying first task has no depends-on annotation")
			_, hasDependsOn := firstTask.Annotations[WorkflowDependsOnAnnotation]
			Expect(hasDependsOn).To(BeFalse())

			By("Waiting for first Task to complete")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, firstTaskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Waiting for second stage Task to be created")
			secondTaskName := fmt.Sprintf("%s-second-task", workflowRunName)
			secondTaskKey := types.NamespacedName{Name: secondTaskName, Namespace: testNS}
			secondTask := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, secondTaskKey, secondTask) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying second task has depends-on annotation pointing to first task")
			Expect(secondTask.Annotations[WorkflowDependsOnAnnotation]).To(Equal(firstTaskName))

			By("Waiting for WorkflowRun to complete")
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				if err := k8sClient.Get(ctx, workflowRunKey, wfr); err != nil {
					return ""
				}
				return wfr.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("WorkflowRun with parallel tasks in a stage", func() {
		It("should create all parallel tasks and set depends-on with multiple task names", func() {
			workflowRunName := uniqueName("wfr-parallel")

			By("Creating a WorkflowRun with parallel tasks")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: testNS,
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
											AgentRef:    agentName,
										},
									},
									{
										Name: "parallel-b",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Parallel task B"),
											AgentRef:    agentName,
										},
									},
								},
							},
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "final-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Final task after parallel"),
											AgentRef:    agentName,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			workflowRunKey := types.NamespacedName{Name: workflowRunName, Namespace: testNS}

			By("Waiting for both parallel Tasks to be created")
			taskAName := fmt.Sprintf("%s-parallel-a", workflowRunName)
			taskBName := fmt.Sprintf("%s-parallel-b", workflowRunName)

			Eventually(func() bool {
				taskA := &kubetaskv1alpha1.Task{}
				taskB := &kubetaskv1alpha1.Task{}
				errA := k8sClient.Get(ctx, types.NamespacedName{Name: taskAName, Namespace: testNS}, taskA)
				errB := k8sClient.Get(ctx, types.NamespacedName{Name: taskBName, Namespace: testNS}, taskB)
				return errA == nil && errB == nil
			}, timeout, interval).Should(BeTrue())

			By("Waiting for both parallel Tasks to complete")
			Eventually(func() bool {
				taskA := &kubetaskv1alpha1.Task{}
				taskB := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: taskAName, Namespace: testNS}, taskA); err != nil {
					return false
				}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: taskBName, Namespace: testNS}, taskB); err != nil {
					return false
				}
				return taskA.Status.Phase == kubetaskv1alpha1.TaskPhaseCompleted &&
					taskB.Status.Phase == kubetaskv1alpha1.TaskPhaseCompleted
			}, timeout, interval).Should(BeTrue())

			By("Waiting for final Task to be created")
			finalTaskName := fmt.Sprintf("%s-final-task", workflowRunName)
			finalTask := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, types.NamespacedName{Name: finalTaskName, Namespace: testNS}, finalTask) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying final task depends-on contains both parallel task names")
			dependsOn := finalTask.Annotations[WorkflowDependsOnAnnotation]
			Expect(dependsOn).To(ContainSubstring(taskAName))
			Expect(dependsOn).To(ContainSubstring(taskBName))
			// Verify it's comma-separated
			parts := strings.Split(dependsOn, ",")
			Expect(parts).To(HaveLen(2))

			By("Waiting for WorkflowRun to complete")
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				if err := k8sClient.Get(ctx, workflowRunKey, wfr); err != nil {
					return ""
				}
				return wfr.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseCompleted))

			By("Verifying WorkflowRun completed all tasks")
			wfr := &kubetaskv1alpha1.WorkflowRun{}
			Expect(k8sClient.Get(ctx, workflowRunKey, wfr)).Should(Succeed())
			Expect(wfr.Status.TotalTasks).To(Equal(int32(3)))
			Expect(wfr.Status.CompletedTasks).To(Equal(int32(3)))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("WorkflowRun with auto-generated stage names", func() {
		It("should auto-generate stage names when not specified", func() {
			workflowRunName := uniqueName("wfr-autogen")

			By("Creating a WorkflowRun without stage names")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								// No name specified
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "task-one",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Task in auto-named stage"),
											AgentRef:    agentName,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			workflowRunKey := types.NamespacedName{Name: workflowRunName, Namespace: testNS}

			By("Waiting for WorkflowRun to initialize")
			Eventually(func() int {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				if err := k8sClient.Get(ctx, workflowRunKey, wfr); err != nil {
					return 0
				}
				return len(wfr.Status.StageStatuses)
			}, timeout, interval).Should(Equal(1))

			By("Verifying stage name was auto-generated")
			wfr := &kubetaskv1alpha1.WorkflowRun{}
			Expect(k8sClient.Get(ctx, workflowRunKey, wfr)).Should(Succeed())
			Expect(wfr.Status.StageStatuses[0].Name).To(Equal("stage-0"))

			By("Verifying Task has auto-generated stage label")
			taskName := fmt.Sprintf("%s-task-one", workflowRunName)
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, types.NamespacedName{Name: taskName, Namespace: testNS}, task) == nil
			}, timeout, interval).Should(BeTrue())

			Expect(task.Labels[WorkflowStageLabelKey]).To(Equal("stage-0"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())
		})
	})

	Context("WorkflowRun garbage collection", func() {
		It("should clean up Tasks when WorkflowRun is deleted", func() {
			workflowRunName := uniqueName("wfr-gc")

			By("Creating a WorkflowRun")
			workflowRun := &kubetaskv1alpha1.WorkflowRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkflowRunSpec{
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "gc-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Task for GC test"),
											AgentRef:    agentName,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflowRun)).Should(Succeed())

			workflowRunKey := types.NamespacedName{Name: workflowRunName, Namespace: testNS}
			taskName := fmt.Sprintf("%s-gc-task", workflowRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}

			By("Waiting for Task to be created")
			Eventually(func() bool {
				task := &kubetaskv1alpha1.Task{}
				return k8sClient.Get(ctx, taskKey, task) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Task has owner reference to WorkflowRun")
			task := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, task)).Should(Succeed())
			Expect(task.OwnerReferences).To(HaveLen(1))
			Expect(task.OwnerReferences[0].Name).To(Equal(workflowRunName))
			Expect(task.OwnerReferences[0].Kind).To(Equal("WorkflowRun"))

			By("Deleting WorkflowRun")
			Expect(k8sClient.Delete(ctx, workflowRun)).Should(Succeed())

			By("Verifying WorkflowRun is deleted")
			Eventually(func() bool {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				return k8sClient.Get(ctx, workflowRunKey, wfr) != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Task is garbage collected")
			Eventually(func() bool {
				task := &kubetaskv1alpha1.Task{}
				return k8sClient.Get(ctx, taskKey, task) != nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})
