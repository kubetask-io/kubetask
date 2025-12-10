// Copyright Contributors to the KubeTask project

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubetaskv1alpha1 "github.com/xuezhaojun/kubetask/api/v1alpha1"
)

var _ = Describe("BatchRun E2E Tests", func() {
	var (
		agent     *kubetaskv1alpha1.Agent
		agentName string
	)

	BeforeEach(func() {
		// Create a Agent with echo agent for all tests
		agentName = uniqueName("echo-ws-br")
		agent = &kubetaskv1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      agentName,
				Namespace: testNS,
			},
			Spec: kubetaskv1alpha1.AgentSpec{
				AgentImage:         echoImage,
				ServiceAccountName: testServiceAccount,
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

	Context("BatchRun with inline BatchSpec", func() {
		It("should create Tasks for each variableContext and complete successfully", func() {
			batchRunName := uniqueName("batchrun-inline")
			commonContent := "# Common Guidelines\n\nShared guidelines for all tasks."
			var1Content := "# Variable 1\n\nContent for task 1."
			var2Content := "# Variable 2\n\nContent for task 2."
			var3Content := "# Variable 3\n\nContent for task 3."

			By("Creating BatchRun with 3 variableContexts")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						AgentRef: agentName,
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
											Inline: &var1Content,
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
											Inline: &var2Content,
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
											Inline: &var3Content,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			batchRunKey := types.NamespacedName{Name: batchRunName, Namespace: testNS}

			By("Verifying BatchRun status is initialized with 3 tasks")
			Eventually(func() int {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return 0
				}
				return br.Status.Progress.Total
			}, timeout, interval).Should(Equal(3))

			By("Verifying BatchRun transitions to Running")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return ""
				}
				return br.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseRunning))

			By("Verifying all 3 Tasks are created")
			for i := 0; i < 3; i++ {
				taskName := fmt.Sprintf("%s-task-%d", batchRunName, i)
				taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
				Eventually(func() bool {
					task := &kubetaskv1alpha1.Task{}
					return k8sClient.Get(ctx, taskKey, task) == nil
				}, timeout, interval).Should(BeTrue(), fmt.Sprintf("Task %d should be created", i))
			}

			By("Waiting for all Tasks to complete")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return ""
				}
				return br.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseCompleted))

			By("Verifying progress shows 3 completed")
			finalBR := &kubetaskv1alpha1.BatchRun{}
			Expect(k8sClient.Get(ctx, batchRunKey, finalBR)).Should(Succeed())
			Expect(finalBR.Status.Progress.Completed).Should(Equal(3))
			Expect(finalBR.Status.Progress.Running).Should(Equal(0))
			Expect(finalBR.Status.Progress.Failed).Should(Equal(0))
			Expect(finalBR.Status.CompletionTime).ShouldNot(BeNil())

			By("Verifying each Task has correct merged contexts")
			for i := 0; i < 3; i++ {
				taskName := fmt.Sprintf("%s-task-%d", batchRunName, i)
				taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
				task := &kubetaskv1alpha1.Task{}
				Expect(k8sClient.Get(ctx, taskKey, task)).Should(Succeed())
				// Each task should have common + variable contexts
				Expect(task.Spec.Contexts).Should(HaveLen(2))
				Expect(task.Spec.AgentRef).Should(Equal(agentName))
			}

			By("Verifying echo output contains task content")
			for i := 0; i < 3; i++ {
				taskName := fmt.Sprintf("%s-task-%d", batchRunName, i)
				jobName := fmt.Sprintf("%s-job", taskName)
				logs := getPodLogs(ctx, testNS, jobName)
				Expect(logs).Should(ContainSubstring("Common Guidelines"))
				Expect(logs).Should(ContainSubstring(fmt.Sprintf("Variable %d", i+1)))
			}

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("BatchRun with Batch reference", func() {
		It("should create Tasks from referenced Batch", func() {
			batchName := uniqueName("batch-ref")
			batchRunName := uniqueName("batchrun-ref")
			commonContent := "# Common from Batch\n\nThis is from a referenced Batch."
			varContent := "# Variable Content"

			By("Creating Batch resource")
			batch := &kubetaskv1alpha1.Batch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.BatchSpec{
					AgentRef: agentName,
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
										Inline: &varContent,
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
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchRef: batchName,
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			batchRunKey := types.NamespacedName{Name: batchRunName, Namespace: testNS}

			By("Waiting for BatchRun to complete")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return ""
				}
				return br.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseCompleted))

			By("Verifying Task was created and has correct AgentRef")
			taskName := fmt.Sprintf("%s-task-0", batchRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			task := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, task)).Should(Succeed())
			Expect(task.Spec.AgentRef).Should(Equal(agentName))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, batch)).Should(Succeed())
		})
	})

	Context("BatchRun pause and resume", func() {
		It("should start paused when annotation is set at creation and resume when removed", func() {
			batchRunName := uniqueName("batchrun-pause")
			content := "# Pause Test Task"

			By("Creating BatchRun with pause annotation already set")
			// Create with pause annotation from the start to avoid race condition
			// where fast-completing tasks finish before we can pause
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: testNS,
					Annotations: map[string]string{
						kubetaskv1alpha1.AnnotationPause: "true",
					},
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						AgentRef: agentName,
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

			batchRunKey := types.NamespacedName{Name: batchRunName, Namespace: testNS}

			By("Verifying BatchRun starts in Paused state")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return ""
				}
				return br.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhasePaused))

			By("Removing pause annotation to resume")
			pausedBR := &kubetaskv1alpha1.BatchRun{}
			Expect(k8sClient.Get(ctx, batchRunKey, pausedBR)).Should(Succeed())
			delete(pausedBR.Annotations, kubetaskv1alpha1.AnnotationPause)
			Expect(k8sClient.Update(ctx, pausedBR)).Should(Succeed())

			By("Verifying BatchRun transitions to Running or completes")
			Eventually(func() bool {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return false
				}
				// Accept either Running (if caught mid-execution) or Succeeded (if completed quickly)
				return br.Status.Phase == kubetaskv1alpha1.BatchRunPhaseRunning ||
					br.Status.Phase == kubetaskv1alpha1.BatchRunPhaseCompleted
			}, timeout, interval).Should(BeTrue())

			By("Verifying BatchRun eventually completes successfully")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return ""
				}
				return br.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("BatchRun progress tracking", func() {
		It("should accurately track progress as tasks complete", func() {
			batchRunName := uniqueName("batchrun-progress")
			content := "# Progress Test"

			By("Creating BatchRun with multiple tasks")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						AgentRef: agentName,
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
							{}, {}, {}, {}, // 4 tasks
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			batchRunKey := types.NamespacedName{Name: batchRunName, Namespace: testNS}

			By("Verifying initial progress (Total=4, Pending=4)")
			Eventually(func() int {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return 0
				}
				return br.Status.Progress.Total
			}, timeout, interval).Should(Equal(4))

			By("Waiting for BatchRun to complete")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return ""
				}
				return br.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseCompleted))

			By("Verifying final progress (Completed=4)")
			finalBR := &kubetaskv1alpha1.BatchRun{}
			Expect(k8sClient.Get(ctx, batchRunKey, finalBR)).Should(Succeed())
			Expect(finalBR.Status.Progress.Total).Should(Equal(4))
			Expect(finalBR.Status.Progress.Completed).Should(Equal(4))
			Expect(finalBR.Status.Progress.Running).Should(Equal(0))
			Expect(finalBR.Status.Progress.Pending).Should(Equal(0))
			Expect(finalBR.Status.Progress.Failed).Should(Equal(0))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})

	Context("BatchRun Task ownership", func() {
		It("should set owner references on Tasks for garbage collection", func() {
			batchRunName := uniqueName("batchrun-gc")
			content := "# GC Test"

			By("Creating BatchRun")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						AgentRef: agentName,
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
						VariableContexts: []kubetaskv1alpha1.ContextSet{{}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			batchRunKey := types.NamespacedName{Name: batchRunName, Namespace: testNS}
			taskName := fmt.Sprintf("%s-task-0", batchRunName)
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}

			By("Waiting for Task to be created")
			Eventually(func() bool {
				task := &kubetaskv1alpha1.Task{}
				return k8sClient.Get(ctx, taskKey, task) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Task has owner reference to BatchRun")
			task := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, task)).Should(Succeed())
			Expect(task.OwnerReferences).Should(HaveLen(1))
			Expect(task.OwnerReferences[0].Name).Should(Equal(batchRunName))
			Expect(task.OwnerReferences[0].Kind).Should(Equal("BatchRun"))
			Expect(task.Labels).Should(HaveKeyWithValue("kubetask.io/batch-run", batchRunName))

			By("Deleting BatchRun")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())

			By("Verifying BatchRun is deleted")
			Eventually(func() bool {
				br := &kubetaskv1alpha1.BatchRun{}
				return k8sClient.Get(ctx, batchRunKey, br) != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Task is garbage collected")
			Eventually(func() bool {
				task := &kubetaskv1alpha1.Task{}
				return k8sClient.Get(ctx, taskKey, task) != nil
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("BatchRun with complex context sets", func() {
		It("should handle multiple files per variableContext", func() {
			batchRunName := uniqueName("batchrun-complex")
			commonContent := "# Common Base"
			var1File1 := "# Task 1 - File A"
			var1File2 := "# Task 1 - File B"
			var2File1 := "# Task 2 - File A"
			var2File2 := "# Task 2 - File B"

			By("Creating BatchRun with multiple files per variableContext")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						AgentRef: agentName,
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
										FilePath: "/workspace/file-a.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: &var1File1,
										},
									},
								},
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/file-b.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: &var1File2,
										},
									},
								},
							},
							{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/file-a.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: &var2File1,
										},
									},
								},
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/file-b.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: &var2File2,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			batchRunKey := types.NamespacedName{Name: batchRunName, Namespace: testNS}

			By("Waiting for BatchRun to complete")
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return ""
				}
				return br.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseCompleted))

			By("Verifying Task 0 has all contexts (1 common + 2 variable)")
			task0Key := types.NamespacedName{Name: fmt.Sprintf("%s-task-0", batchRunName), Namespace: testNS}
			task0 := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task0Key, task0)).Should(Succeed())
			Expect(task0.Spec.Contexts).Should(HaveLen(3))

			By("Verifying echo output for Task 0")
			logs0 := getPodLogs(ctx, testNS, fmt.Sprintf("%s-task-0-job", batchRunName))
			Expect(logs0).Should(ContainSubstring("Common Base"))
			Expect(logs0).Should(ContainSubstring("Task 1 - File A"))
			Expect(logs0).Should(ContainSubstring("Task 1 - File B"))

			By("Verifying echo output for Task 1")
			logs1 := getPodLogs(ctx, testNS, fmt.Sprintf("%s-task-1-job", batchRunName))
			Expect(logs1).Should(ContainSubstring("Common Base"))
			Expect(logs1).Should(ContainSubstring("Task 2 - File A"))
			Expect(logs1).Should(ContainSubstring("Task 2 - File B"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
		})
	})
})
