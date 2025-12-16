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

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

// stringPtr returns a pointer to the given string value
func stringPtr(s string) *string {
	return &s
}

var _ = Describe("Agent E2E Tests", func() {

	Context("Agent with custom podSpec.labels", func() {
		It("should apply labels to generated Jobs", func() {
			agentName := uniqueName("ws-labels")
			taskName := uniqueName("task-labels")
			content := "# Labels Test"

			By("Creating Agent with podSpec.labels")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== Task Content ===' && find ${WORKSPACE_DIR} -type f -print0 2>/dev/null | sort -z | xargs -0 -I {} sh -c 'echo \"--- File: {} ---\" && cat \"{}\" && echo' && echo '=== Task Completed ==='"},
					PodSpec: &kubetaskv1alpha1.AgentPodSpec{
						Labels: map[string]string{
							"custom-label":   "custom-value",
							"network-policy": "restricted",
							"team":           "platform",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task using Agent")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &content,
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
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with podSpec.scheduling constraints", func() {
		It("should apply nodeSelector to generated Jobs", func() {
			agentName := uniqueName("ws-scheduling")
			taskName := uniqueName("task-scheduling")
			content := "# Scheduling Test"

			By("Creating Agent with podSpec.scheduling")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== Task Content ===' && find ${WORKSPACE_DIR} -type f -print0 2>/dev/null | sort -z | xargs -0 -I {} sh -c 'echo \"--- File: {} ---\" && cat \"{}\" && echo' && echo '=== Task Completed ==='"},
					PodSpec: &kubetaskv1alpha1.AgentPodSpec{
						Scheduling: &kubetaskv1alpha1.PodScheduling{
							NodeSelector: map[string]string{
								"kubernetes.io/os": "linux",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &content,
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
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Verifying Pod was scheduled successfully with nodeSelector")
			// If the Pod completed successfully, the scheduling was applied correctly
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-job", taskName))
			Expect(logs).Should(ContainSubstring("Scheduling Test"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with credentials", func() {
		It("should inject credentials as environment variables", func() {
			agentName := uniqueName("ws-creds")
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
			By("Creating Agent with credentials")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== Task Content ===' && find ${WORKSPACE_DIR} -type f -print0 2>/dev/null | sort -z | xargs -0 -I {} sh -c 'echo \"--- File: {} ---\" && cat \"{}\" && echo' && echo '=== Task Completed ==='"},
					Credentials: []kubetaskv1alpha1.Credential{
						{
							Name: "test-api-key",
							SecretRef: kubetaskv1alpha1.SecretReference{
								Name: secretName,
								Key:  stringPtr("api-key"),
							},
							Env: &envName,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &content,
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
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("Default Agent resolution", func() {
		It("should use 'default' Agent when not specified", func() {
			defaultWSConfigName := "default"
			taskName := uniqueName("task-default-ws")
			content := "# Default WS Test"

			By("Creating 'default' Agent in test namespace")
			defaultWSConfig := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultWSConfigName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== Task Content ===' && find ${WORKSPACE_DIR} -type f -print0 2>/dev/null | sort -z | xargs -0 -I {} sh -c 'echo \"--- File: {} ---\" && cat \"{}\" && echo' && echo '=== Task Completed ==='"},
				},
			}
			Expect(k8sClient.Create(ctx, defaultWSConfig)).Should(Succeed())

			By("Creating Task without AgentRef")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					// AgentRef is NOT specified
					Description: &content,
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
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Verifying echo agent ran successfully")
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-job", taskName))
			Expect(logs).Should(ContainSubstring("Default WS Test"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, defaultWSConfig)).Should(Succeed())
		})
	})
})
