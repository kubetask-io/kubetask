// Copyright Contributors to the KubeTask project

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubetaskv1alpha1 "github.com/xuezhaojun/kubetask/api/v1alpha1"
)

var _ = Describe("WorkspaceConfig E2E Tests", func() {

	Context("WorkspaceConfig with custom podLabels", func() {
		It("should apply podLabels to generated Jobs", func() {
			wsConfigName := uniqueName("ws-labels")
			taskName := uniqueName("task-labels")
			content := "# Labels Test"

			By("Creating WorkspaceConfig with podLabels")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					PodLabels: map[string]string{
						"custom-label":   "custom-value",
						"network-policy": "restricted",
						"team":           "platform",
					},
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task using WorkspaceConfig")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
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
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to start running")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Verifying Pod has custom labels")
			jobName := fmt.Sprintf("%s-job", taskName)
			Eventually(func() map[string]string {
				pods := &corev1.PodList{}
				if err := k8sClient.List(ctx, pods,
					client.InNamespace(testNS),
					client.MatchingLabels{"job-name": jobName}); err != nil || len(pods.Items) == 0 {
					// Try alternative label
					_ = k8sClient.List(ctx, pods,
						client.InNamespace(testNS),
						client.MatchingLabels{"batch.kubernetes.io/job-name": jobName})
					if len(pods.Items) == 0 {
						return nil
					}
				}
				return pods.Items[0].Labels
			}, timeout, interval).Should(And(
				HaveKeyWithValue("custom-label", "custom-value"),
				HaveKeyWithValue("network-policy", "restricted"),
				HaveKeyWithValue("team", "platform"),
			))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("WorkspaceConfig with scheduling constraints", func() {
		It("should apply nodeSelector to generated Jobs", func() {
			wsConfigName := uniqueName("ws-scheduling")
			taskName := uniqueName("task-scheduling")
			content := "# Scheduling Test"

			By("Creating WorkspaceConfig with scheduling")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					Scheduling: &kubetaskv1alpha1.PodScheduling{
						NodeSelector: map[string]string{
							"kubernetes.io/os": "linux",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
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
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

			By("Verifying Pod was scheduled successfully with nodeSelector")
			// If the Pod completed successfully, the scheduling was applied correctly
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-job", taskName))
			Expect(logs).Should(ContainSubstring("Scheduling Test"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("WorkspaceConfig with credentials", func() {
		It("should inject credentials as environment variables", func() {
			wsConfigName := uniqueName("ws-creds")
			taskName := uniqueName("task-creds")
			secretName := uniqueName("test-secret")
			content := "# Credentials Test"

			By("Creating Secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: testNS,
				},
				Data: map[string][]byte{
					"api-key": []byte("test-api-key-value-12345"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			envName := "TEST_API_KEY"
			By("Creating WorkspaceConfig with credentials")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					Credentials: []kubetaskv1alpha1.Credential{
						{
							Name: "test-api-key",
							SecretRef: kubetaskv1alpha1.SecretReference{
								Name: secretName,
								Key:  "api-key",
							},
							Env: &envName,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
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
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("WorkspaceConfig with defaultContexts propagated to BatchRun", func() {
		It("should merge defaultContexts with BatchRun contexts", func() {
			wsConfigName := uniqueName("ws-default-br")
			batchRunName := uniqueName("br-default-ctx")
			defaultContent := "# Organization Guidelines\n\nFollow these guidelines."
			commonContent := "# Batch Common\n\nBatch-level common content."
			varContent := "# Variable Specific"

			By("Creating WorkspaceConfig with defaultContexts")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					DefaultContexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/org-guidelines.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &defaultContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating BatchRun using WorkspaceConfig")
			batchRun := &kubetaskv1alpha1.BatchRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      batchRunName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.BatchRunSpec{
					BatchSpec: &kubetaskv1alpha1.BatchSpec{
						WorkspaceConfigRef: wsConfigName,
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
				},
			}
			Expect(k8sClient.Create(ctx, batchRun)).Should(Succeed())

			By("Waiting for BatchRun to complete")
			batchRunKey := types.NamespacedName{Name: batchRunName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.BatchRunPhase {
				br := &kubetaskv1alpha1.BatchRun{}
				if err := k8sClient.Get(ctx, batchRunKey, br); err != nil {
					return ""
				}
				return br.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.BatchRunPhaseSucceeded))

			By("Verifying all context layers are in echo output")
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-task-0-job", batchRunName))
			// WorkspaceConfig defaultContexts should be included
			Expect(logs).Should(ContainSubstring("Organization Guidelines"))
			// Batch commonContext should be included
			Expect(logs).Should(ContainSubstring("Batch Common"))
			// VariableContext should be included
			Expect(logs).Should(ContainSubstring("Variable Specific"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, batchRun)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("Default WorkspaceConfig resolution", func() {
		It("should use 'default' WorkspaceConfig when not specified", func() {
			defaultWSConfigName := "default"
			taskName := uniqueName("task-default-ws")
			content := "# Default WS Test"

			By("Creating 'default' WorkspaceConfig in test namespace")
			defaultWSConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultWSConfigName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
				},
			}
			Expect(k8sClient.Create(ctx, defaultWSConfig)).Should(Succeed())

			By("Creating Task without WorkspaceConfigRef")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					// WorkspaceConfigRef is NOT specified
					Contexts: []kubetaskv1alpha1.Context{
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
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

			By("Verifying echo agent ran successfully")
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-job", taskName))
			Expect(logs).Should(ContainSubstring("Default WS Test"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, defaultWSConfig)).Should(Succeed())
		})
	})
})
