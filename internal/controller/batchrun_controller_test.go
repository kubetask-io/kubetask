// Copyright Contributors to the KubeTask project

//go:build integration

// See suite_test.go for explanation of the "integration" build tag pattern.

package controller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubetaskv1alpha1 "github.com/xuezhaojun/kubetask/api/v1alpha1"
)

var _ = Describe("BatchRunController", func() {
	const (
		batchRunNamespace = "default"
	)

	Context("When creating a BatchRun with inline BatchSpec", func() {
		It("Should initialize status and create Tasks", func() {
			batchRunName := "test-batchrun-inline"
			commonContent := "# Common Task Guidelines"
			variableContent1 := "# Task 1 specific"
			variableContent2 := "# Task 2 specific"

			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						CommonContext: []kubetaskv1alpha1.Context{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/common.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &commonContent,
									},
								},
							},
						},
						VariableContexts: []kubetaskv1alpha1.ContextSet{
							{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/variable.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: &variableContent1,
										},
									},
								},
							},
							{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/variable.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: &variableContent2,
										},
									},
								},
							},
						},
					},
				},
			}

			By("Creating the BatchRun")
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Checking BatchRun status is initialized")
			batchRunLookupKey := types.NamespacedName{Name: batchRunName, Namespace: batchRunNamespace}
			createdBatchRun := &kubetaskv1alpha1.BatchRun{}
			Eventually(func() int {
				if err := k8sClient.Get(ctx, batchRunLookupKey, createdBatchRun); err != nil {
					return 0
				}
				return createdBatchRun.Status.Progress.Total
			}, timeout, interval).Should(Equal(2))

			By("Verifying progress status is correct")
			Expect(createdBatchRun.Status.StartTime).ShouldNot(BeNil())
			Expect(createdBatchRun.Status.Tasks).Should(HaveLen(2))

			By("Checking Tasks are created")
			Eventually(func() bool {
				for i := 0; i < 2; i++ {
					taskName := fmt.Sprintf("%s-task-%d", batchRunName, i)
					task := &kubetaskv1alpha1.Task{}
					taskKey := types.NamespacedName{Name: taskName, Namespace: batchRunNamespace}
					if err := k8sClient.Get(ctx, taskKey, task); err != nil {
						return false
					}
				}
				return true
			}, timeout, interval).Should(BeTrue())

			By("Verifying Task 0 has merged contexts")
			task0 := &kubetaskv1alpha1.Task{}
			task0Key := types.NamespacedName{Name: fmt.Sprintf("%s-task-0", batchRunName), Namespace: batchRunNamespace}
			Expect(k8sClient.Get(ctx, task0Key, task0)).Should(Succeed())
			Expect(task0.Spec.Contexts).Should(HaveLen(2)) // common + variable
			Expect(task0.OwnerReferences).Should(HaveLen(1))
			Expect(task0.OwnerReferences[0].Name).Should(Equal(batchRunName))

			By("Verifying BatchRun phase transitions to Running")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				if err := k8sClient.Get(ctx, batchRunLookupKey, createdBatchRun); err != nil {
					return ""
				}
				return createdBatchRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("When creating a BatchRun with BatchRef", func() {
		It("Should fetch Batch and create Tasks", func() {
			batchName := "test-batch-ref"
			batchRunName := "test-batchrun-ref"
			commonContent := "# Common from Batch"
			variableContent := "# Variable context"

			By("Creating Batch")
			batch := &kubetaskv1alpha1.Batch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchSpec{
					CommonContext: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/common.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &commonContent,
								},
							},
						},
					},
					VariableContexts: []kubetaskv1alpha1.ContextSet{
						{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/variable.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &variableContent,
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batch)).Should(Succeed())

			By("Creating BatchRun with BatchRef")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchRef: batchName,
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Checking BatchRun status is initialized from Batch")
			batchRunLookupKey := types.NamespacedName{Name: batchRunName, Namespace: batchRunNamespace}
			createdBatchRun := &kubetaskv1alpha1.BatchRun{}
			Eventually(func() int {
				if err := k8sClient.Get(ctx, batchRunLookupKey, createdBatchRun); err != nil {
					return 0
				}
				return createdBatchRun.Status.Progress.Total
			}, timeout, interval).Should(Equal(1))

			By("Checking Task is created")
			taskName := fmt.Sprintf("%s-task-0", batchRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: batchRunNamespace}
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, taskKey, task) == nil
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, batch)).Should(Succeed())
		})
	})

	Context("When a BatchRun's Tasks complete successfully", func() {
		It("Should update BatchRun status to Succeeded", func() {
			batchRunName := "test-batchrun-success"
			content := "# Success test"

			By("Creating BatchRun")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						CommonContext: []kubetaskv1alpha1.Context{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/task.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &content,
									},
								},
							},
						},
						VariableContexts: []kubetaskv1alpha1.ContextSet{
							{}, // Just one task with no variable context
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Waiting for Task to be created")
			taskName := fmt.Sprintf("%s-task-0", batchRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: batchRunNamespace}
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, taskKey, task) == nil
			}, timeout, interval).Should(BeTrue())

			By("Simulating Task success")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskKey, task); err != nil {
					return ""
				}
				return task.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			task.Status.Phase = kubetaskv1alpha1.TaskPhaseSucceeded
			now := metav1.Now()
			task.Status.CompletionTime = &now
			Expect(k8sClient.Status().Update(ctx, task)).Should(Succeed())

			By("Checking BatchRun status is Succeeded")
			batchRunLookupKey := types.NamespacedName{Name: batchRunName, Namespace: batchRunNamespace}
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				updatedBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, updatedBatchRun); err != nil {
					return ""
				}
				return updatedBatchRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseSucceeded))

			By("Verifying progress is updated")
			finalBatchRun := &kubetaskv1alpha1.BatchRun{}
			Expect(k8sClient.Get(ctx, batchRunLookupKey, finalBatchRun)).Should(Succeed())
			Expect(finalBatchRun.Status.Progress.Completed).Should(Equal(1))
			Expect(finalBatchRun.Status.Progress.Running).Should(Equal(0))
			Expect(finalBatchRun.Status.CompletionTime).ShouldNot(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("When a BatchRun's Task fails", func() {
		It("Should update BatchRun status to Failed", func() {
			batchRunName := "test-batchrun-failure"
			content := "# Failure test"

			By("Creating BatchRun")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						CommonContext: []kubetaskv1alpha1.Context{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/task.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &content,
									},
								},
							},
						},
						VariableContexts: []kubetaskv1alpha1.ContextSet{
							{},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Waiting for Task to be created and running")
			taskName := fmt.Sprintf("%s-task-0", batchRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: batchRunNamespace}
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskKey, task); err != nil {
					return ""
				}
				return task.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Simulating Task failure")
			task.Status.Phase = kubetaskv1alpha1.TaskPhaseFailed
			now := metav1.Now()
			task.Status.CompletionTime = &now
			Expect(k8sClient.Status().Update(ctx, task)).Should(Succeed())

			By("Checking BatchRun status is Failed")
			batchRunLookupKey := types.NamespacedName{Name: batchRunName, Namespace: batchRunNamespace}
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				updatedBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, updatedBatchRun); err != nil {
					return ""
				}
				return updatedBatchRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseFailed))

			By("Verifying progress is updated")
			finalBatchRun := &kubetaskv1alpha1.BatchRun{}
			Expect(k8sClient.Get(ctx, batchRunLookupKey, finalBatchRun)).Should(Succeed())
			Expect(finalBatchRun.Status.Progress.Failed).Should(Equal(1))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("When creating a BatchRun with WorkspaceConfigRef in BatchSpec", func() {
		It("Should propagate WorkspaceConfigRef to Tasks", func() {
			batchRunName := "test-batchrun-wsconfig"
			wsConfigName := "test-batch-wsconfig"
			content := "# WorkspaceConfig test"

			By("Creating WorkspaceConfig")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					AgentImage:         "custom-agent:v2.0.0",
					ServiceAccountName: "test-agent",
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating BatchRun with WorkspaceConfigRef")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						WorkspaceConfigRef: wsConfigName,
						CommonContext: []kubetaskv1alpha1.Context{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/task.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &content,
									},
								},
							},
						},
						VariableContexts: []kubetaskv1alpha1.ContextSet{
							{},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Checking Task has WorkspaceConfigRef")
			taskName := fmt.Sprintf("%s-task-0", batchRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: batchRunNamespace}
			task := &kubetaskv1alpha1.Task{}
			Eventually(func() string {
				if err := k8sClient.Get(ctx, taskKey, task); err != nil {
					return ""
				}
				return task.Spec.WorkspaceConfigRef
			}, timeout, interval).Should(Equal(wsConfigName))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("When pausing a BatchRun", func() {
		It("Should transition to Paused phase and stop creating new tasks", func() {
			batchRunName := "test-batchrun-pause"
			content := "# Pause test"

			By("Creating BatchRun with multiple tasks")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						CommonContext: []kubetaskv1alpha1.Context{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/task.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &content,
									},
								},
							},
						},
						VariableContexts: []kubetaskv1alpha1.ContextSet{
							{}, // Task 0
							{}, // Task 1
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Waiting for BatchRun to be running")
			batchRunLookupKey := types.NamespacedName{Name: batchRunName, Namespace: batchRunNamespace}
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				updatedBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, updatedBatchRun); err != nil {
					return ""
				}
				return updatedBatchRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseRunning))

			By("Adding pause annotation")
			updatedBatchRun := &kubetaskv1alpha1.BatchRun{}
			Expect(k8sClient.Get(ctx, batchRunLookupKey, updatedBatchRun)).Should(Succeed())
			if updatedBatchRun.Annotations == nil {
				updatedBatchRun.Annotations = make(map[string]string)
			}
			updatedBatchRun.Annotations[kubetaskv1alpha1.AnnotationPause] = "true"
			Expect(k8sClient.Update(ctx, updatedBatchRun)).Should(Succeed())

			By("Checking BatchRun phase is Paused")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				pausedBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, pausedBatchRun); err != nil {
					return ""
				}
				return pausedBatchRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhasePaused))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("When resuming a paused BatchRun", func() {
		It("Should transition back to Running phase", func() {
			batchRunName := "test-batchrun-resume"
			content := "# Resume test"

			By("Creating BatchRun with pause annotation")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
					Annotations: map[string]string{
						kubetaskv1alpha1.AnnotationPause: "true",
					},
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						CommonContext: []kubetaskv1alpha1.Context{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/task.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &content,
									},
								},
							},
						},
						VariableContexts: []kubetaskv1alpha1.ContextSet{
							{},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Waiting for BatchRun to be paused")
			batchRunLookupKey := types.NamespacedName{Name: batchRunName, Namespace: batchRunNamespace}
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				updatedBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, updatedBatchRun); err != nil {
					return ""
				}
				return updatedBatchRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhasePaused))

			By("Removing pause annotation")
			pausedBatchRun := &kubetaskv1alpha1.BatchRun{}
			Expect(k8sClient.Get(ctx, batchRunLookupKey, pausedBatchRun)).Should(Succeed())
			delete(pausedBatchRun.Annotations, kubetaskv1alpha1.AnnotationPause)
			Expect(k8sClient.Update(ctx, pausedBatchRun)).Should(Succeed())

			By("Checking BatchRun transitions to Running or Pending")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				resumedBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, resumedBatchRun); err != nil {
					return ""
				}
				return resumedBatchRun.Status.Phase
			}, timeout, interval).Should(BeElementOf(
				kubetaskv1alpha1.BatchRunPhaseRunning,
				kubetaskv1alpha1.BatchRunPhasePending,
			))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("When creating a BatchRun with multiple variable contexts", func() {
		It("Should create correct number of Tasks with merged contexts", func() {
			batchRunName := "test-batchrun-multi"
			commonContent := "# Common Guidelines"

			var variableContents []string
			for i := 0; i < 3; i++ {
				variableContents = append(variableContents, fmt.Sprintf("# Task %d content", i))
			}

			var variableContexts []kubetaskv1alpha1.ContextSet
			for _, vc := range variableContents {
				content := vc
				variableContexts = append(variableContexts, kubetaskv1alpha1.ContextSet{
					{
						Type: kubetaskv1alpha1.ContextTypeFile,
						File: &kubetaskv1alpha1.FileContext{
							FilePath: "/workspace/variable.md",
							Source: kubetaskv1alpha1.FileSource{
								Inline: &content,
							},
						},
					},
				})
			}

			By("Creating BatchRun with 3 variable contexts")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						CommonContext: []kubetaskv1alpha1.Context{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/common.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &commonContent,
									},
								},
							},
						},
						VariableContexts: variableContexts,
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Checking all 3 Tasks are created")
			batchRunLookupKey := types.NamespacedName{Name: batchRunName, Namespace: batchRunNamespace}
			Eventually(func() int {
				createdBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, createdBatchRun); err != nil {
					return 0
				}
				return createdBatchRun.Status.Progress.Total
			}, timeout, interval).Should(Equal(3))

			By("Verifying each Task has correct contexts")
			for i := 0; i < 3; i++ {
				taskName := fmt.Sprintf("%s-task-%d", batchRunName, i)
				taskKey := types.NamespacedName{Name: taskName, Namespace: batchRunNamespace}
				task := &kubetaskv1alpha1.Task{}
				Eventually(func() bool {
					return k8sClient.Get(ctx, taskKey, task) == nil
				}, timeout, interval).Should(BeTrue())
				// Each task should have common + variable contexts
				Expect(task.Spec.Contexts).Should(HaveLen(2))
			}

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("When BatchRun has partial success and failure", func() {
		It("Should report Failed status with correct progress", func() {
			batchRunName := "test-batchrun-partial"
			content := "# Partial test"

			By("Creating BatchRun with 2 tasks")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: batchRunNamespace,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						CommonContext: []kubetaskv1alpha1.Context{
							{
								Type: kubetaskv1alpha1.ContextTypeFile,
								File: &kubetaskv1alpha1.FileContext{
									FilePath: "/workspace/task.md",
									Source: kubetaskv1alpha1.FileSource{
										Inline: &content,
									},
								},
							},
						},
						VariableContexts: []kubetaskv1alpha1.ContextSet{
							{}, // Task 0 - will succeed
							{}, // Task 1 - will fail
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Waiting for Tasks to be created")
			batchRunLookupKey := types.NamespacedName{Name: batchRunName, Namespace: batchRunNamespace}
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				updatedBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, updatedBatchRun); err != nil {
					return ""
				}
				return updatedBatchRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseRunning))

			By("Simulating Task 0 success")
			task0Key := types.NamespacedName{Name: fmt.Sprintf("%s-task-0", batchRunName), Namespace: batchRunNamespace}
			task0 := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, task0Key, task0); err != nil {
					return ""
				}
				return task0.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			task0.Status.Phase = kubetaskv1alpha1.TaskPhaseSucceeded
			now := metav1.Now()
			task0.Status.CompletionTime = &now
			Expect(k8sClient.Status().Update(ctx, task0)).Should(Succeed())

			By("Simulating Task 1 failure")
			task1Key := types.NamespacedName{Name: fmt.Sprintf("%s-task-1", batchRunName), Namespace: batchRunNamespace}
			task1 := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, task1Key, task1); err != nil {
					return ""
				}
				return task1.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			task1.Status.Phase = kubetaskv1alpha1.TaskPhaseFailed
			task1.Status.CompletionTime = &now
			Expect(k8sClient.Status().Update(ctx, task1)).Should(Succeed())

			By("Checking BatchRun status is Failed")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				updatedBatchRun := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunLookupKey, updatedBatchRun); err != nil {
					return ""
				}
				return updatedBatchRun.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseFailed))

			By("Verifying progress shows 1 completed, 1 failed")
			finalBatchRun := &kubetaskv1alpha1.BatchRun{}
			Expect(k8sClient.Get(ctx, batchRunLookupKey, finalBatchRun)).Should(Succeed())
			Expect(finalBatchRun.Status.Progress.Completed).Should(Equal(1))
			Expect(finalBatchRun.Status.Progress.Failed).Should(Equal(1))
			Expect(finalBatchRun.Status.Progress.Running).Should(Equal(0))
			Expect(finalBatchRun.Status.Progress.Pending).Should(Equal(0))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})
})
