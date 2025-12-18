// Copyright Contributors to the KubeTask project

package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var _ = Describe("CronWorkflow E2E Tests", func() {
	var (
		agent     *kubetaskv1alpha1.Agent
		agentName string
	)

	BeforeEach(func() {
		// Create an Agent with echo agent for all tests
		agentName = uniqueName("cwf-echo")
		agent = &kubetaskv1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      agentName,
				Namespace: testNS,
			},
			Spec: kubetaskv1alpha1.AgentSpec{
				AgentImage:         echoImage,
				ServiceAccountName: testServiceAccount,
				WorkspaceDir:       "/workspace",
				Command:            []string{"sh", "-c", "echo '=== CronWorkflow Task ===' && cat ${WORKSPACE_DIR}/task.md && echo '=== Done ==='"},
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

	Context("CronWorkflow with inline workflow", func() {
		It("should create WorkflowRun on schedule and update status", func() {
			cronWorkflowName := uniqueName("cwf-inline")

			By("Creating a CronWorkflow with a schedule that triggers soon")
			// Use a schedule that triggers every minute for testing
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule: "* * * * *", // Every minute
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Name: "cron-stage",
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "cron-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Task from CronWorkflow"),
											AgentRef:    agentName,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			cronWorkflowKey := types.NamespacedName{Name: cronWorkflowName, Namespace: testNS}

			By("Waiting for CronWorkflow to create a WorkflowRun")
			// Wait up to 2 minutes for the cron to trigger
			Eventually(func() bool {
				workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
				if err := k8sClient.List(ctx, workflowRuns,
					client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName}); err != nil {
					return false
				}
				return len(workflowRuns.Items) > 0
			}, time.Minute*2, interval).Should(BeTrue())

			By("Verifying CronWorkflow status.lastScheduleTime is set")
			Eventually(func() bool {
				cwf := &kubetaskv1alpha1.CronWorkflow{}
				if err := k8sClient.Get(ctx, cronWorkflowKey, cwf); err != nil {
					return false
				}
				return cwf.Status.LastScheduleTime != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying the created WorkflowRun has correct owner reference")
			workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
			Expect(k8sClient.List(ctx, workflowRuns,
				client.InNamespace(testNS),
				client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName})).Should(Succeed())
			Expect(workflowRuns.Items).ShouldNot(BeEmpty())

			wfr := workflowRuns.Items[0]
			Expect(wfr.OwnerReferences).Should(HaveLen(1))
			Expect(wfr.OwnerReferences[0].Kind).Should(Equal("CronWorkflow"))
			Expect(wfr.OwnerReferences[0].Name).Should(Equal(cronWorkflowName))

			By("Waiting for WorkflowRun to complete")
			wfrKey := types.NamespacedName{Name: wfr.Name, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.WorkflowPhase {
				w := &kubetaskv1alpha1.WorkflowRun{}
				if err := k8sClient.Get(ctx, wfrKey, w); err != nil {
					return ""
				}
				return w.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.WorkflowPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())
		})
	})

	Context("CronWorkflow with workflowRef", func() {
		It("should resolve Workflow template and create WorkflowRun", func() {
			workflowName := uniqueName("wf-cron-ref")
			cronWorkflowName := uniqueName("cwf-ref")

			By("Creating a Workflow template")
			workflow := &kubetaskv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkflowSpec{
					Stages: []kubetaskv1alpha1.WorkflowStage{
						{
							Name: "ref-stage",
							Tasks: []kubetaskv1alpha1.WorkflowTask{
								{
									Name: "ref-task",
									Spec: kubetaskv1alpha1.TaskSpec{
										Description: stringPtr("Task from referenced Workflow"),
										AgentRef:    agentName,
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
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule:    "* * * * *", // Every minute
					WorkflowRef: workflowName,
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			By("Waiting for CronWorkflow to create a WorkflowRun")
			Eventually(func() bool {
				workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
				if err := k8sClient.List(ctx, workflowRuns,
					client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName}); err != nil {
					return false
				}
				return len(workflowRuns.Items) > 0
			}, time.Minute*2, interval).Should(BeTrue())

			By("Verifying the WorkflowRun references the Workflow")
			workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
			Expect(k8sClient.List(ctx, workflowRuns,
				client.InNamespace(testNS),
				client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName})).Should(Succeed())
			Expect(workflowRuns.Items).ShouldNot(BeEmpty())

			wfr := workflowRuns.Items[0]
			Expect(wfr.Spec.WorkflowRef).Should(Equal(workflowName))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, workflow)).Should(Succeed())
		})
	})

	Context("CronWorkflow suspend and resume", func() {
		It("should not create WorkflowRun when suspended", func() {
			cronWorkflowName := uniqueName("cwf-suspend")
			suspend := true

			By("Creating a suspended CronWorkflow")
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule: "* * * * *", // Every minute
					Suspend:  &suspend,
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Name: "suspend-stage",
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "suspend-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Task that should not run"),
											AgentRef:    agentName,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			cronWorkflowKey := types.NamespacedName{Name: cronWorkflowName, Namespace: testNS}

			By("Waiting past a schedule time")
			time.Sleep(70 * time.Second) // Wait past one minute mark

			By("Verifying no WorkflowRun was created")
			workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
			Expect(k8sClient.List(ctx, workflowRuns,
				client.InNamespace(testNS),
				client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName})).Should(Succeed())
			Expect(workflowRuns.Items).Should(BeEmpty())

			By("Resuming the CronWorkflow")
			cwf := &kubetaskv1alpha1.CronWorkflow{}
			Expect(k8sClient.Get(ctx, cronWorkflowKey, cwf)).Should(Succeed())
			cwf.Spec.Suspend = nil // Resume
			Expect(k8sClient.Update(ctx, cwf)).Should(Succeed())

			By("Waiting for WorkflowRun to be created after resume")
			Eventually(func() bool {
				workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
				if err := k8sClient.List(ctx, workflowRuns,
					client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName}); err != nil {
					return false
				}
				return len(workflowRuns.Items) > 0
			}, time.Minute*2, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())
		})
	})

	Context("CronWorkflow garbage collection", func() {
		It("should clean up WorkflowRuns when CronWorkflow is deleted", func() {
			cronWorkflowName := uniqueName("cwf-gc")

			By("Creating a CronWorkflow")
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule: "* * * * *",
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
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			cronWorkflowKey := types.NamespacedName{Name: cronWorkflowName, Namespace: testNS}

			By("Waiting for a WorkflowRun to be created")
			Eventually(func() bool {
				workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
				if err := k8sClient.List(ctx, workflowRuns,
					client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName}); err != nil {
					return false
				}
				return len(workflowRuns.Items) > 0
			}, time.Minute*2, interval).Should(BeTrue())

			By("Getting created WorkflowRun name")
			workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
			Expect(k8sClient.List(ctx, workflowRuns,
				client.InNamespace(testNS),
				client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName})).Should(Succeed())
			wfrName := workflowRuns.Items[0].Name

			By("Deleting CronWorkflow")
			cwf := &kubetaskv1alpha1.CronWorkflow{}
			Expect(k8sClient.Get(ctx, cronWorkflowKey, cwf)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, cwf)).Should(Succeed())

			By("Verifying CronWorkflow is deleted")
			Eventually(func() bool {
				c := &kubetaskv1alpha1.CronWorkflow{}
				return k8sClient.Get(ctx, cronWorkflowKey, c) != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying WorkflowRun is garbage collected")
			Eventually(func() bool {
				wfr := &kubetaskv1alpha1.WorkflowRun{}
				return k8sClient.Get(ctx, types.NamespacedName{Name: wfrName, Namespace: testNS}, wfr) != nil
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("CronWorkflow status tracking", func() {
		It("should track active WorkflowRuns and lastSuccessfulTime", func() {
			cronWorkflowName := uniqueName("cwf-status")

			By("Creating a CronWorkflow")
			cronWorkflow := &kubetaskv1alpha1.CronWorkflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronWorkflowName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.CronWorkflowSpec{
					Schedule: "* * * * *",
					Inline: &kubetaskv1alpha1.WorkflowSpec{
						Stages: []kubetaskv1alpha1.WorkflowStage{
							{
								Tasks: []kubetaskv1alpha1.WorkflowTask{
									{
										Name: "status-task",
										Spec: kubetaskv1alpha1.TaskSpec{
											Description: stringPtr("Task for status tracking"),
											AgentRef:    agentName,
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronWorkflow)).Should(Succeed())

			cronWorkflowKey := types.NamespacedName{Name: cronWorkflowName, Namespace: testNS}

			By("Waiting for WorkflowRun to be created and complete")
			var wfrName string
			Eventually(func() bool {
				workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
				if err := k8sClient.List(ctx, workflowRuns,
					client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/cronworkflow": cronWorkflowName}); err != nil {
					return false
				}
				if len(workflowRuns.Items) == 0 {
					return false
				}
				wfrName = workflowRuns.Items[0].Name
				return workflowRuns.Items[0].Status.Phase == kubetaskv1alpha1.WorkflowPhaseCompleted
			}, timeout, interval).Should(BeTrue())

			By("Verifying CronWorkflow status.lastSuccessfulTime is set")
			Eventually(func() bool {
				cwf := &kubetaskv1alpha1.CronWorkflow{}
				if err := k8sClient.Get(ctx, cronWorkflowKey, cwf); err != nil {
					return false
				}
				return cwf.Status.LastSuccessfulTime != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying CronWorkflow status fields")
			cwf := &kubetaskv1alpha1.CronWorkflow{}
			Expect(k8sClient.Get(ctx, cronWorkflowKey, cwf)).Should(Succeed())
			Expect(cwf.Status.LastScheduleTime).ShouldNot(BeNil())
			Expect(cwf.Status.LastSuccessfulTime).ShouldNot(BeNil())
			GinkgoWriter.Printf("CronWorkflow %s: lastSchedule=%v, lastSuccess=%v\n",
				cronWorkflowName, cwf.Status.LastScheduleTime, cwf.Status.LastSuccessfulTime)

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronWorkflow)).Should(Succeed())

			// Also clean up the WorkflowRun if it still exists
			wfr := &kubetaskv1alpha1.WorkflowRun{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: wfrName, Namespace: testNS}, wfr); err == nil {
				_ = k8sClient.Delete(ctx, wfr)
			}
		})
	})
})
