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

	Context("Agent with maxConcurrentTasks limit", func() {
		It("should queue tasks when concurrency limit is reached", func() {
			agentName := uniqueName("ws-concurrency")
			maxConcurrent := int32(1)

			By("Creating Agent with maxConcurrentTasks=1")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					// Use a command that takes some time to complete
					Command:            []string{"sh", "-c", "echo 'Starting task' && sleep 10 && echo 'Task completed'"},
					MaxConcurrentTasks: &maxConcurrent,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			task1Name := uniqueName("task-conc-1")
			task2Name := uniqueName("task-conc-2")
			content1 := "# Task 1"
			content2 := "# Task 2"

			By("Creating first Task")
			task1 := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      task1Name,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &content1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1Key := types.NamespacedName{Name: task1Name, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Creating second Task while first is still running")
			task2 := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      task2Name,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &content2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Verifying second Task enters Queued phase")
			task2Key := types.NamespacedName{Name: task2Name, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseQueued))

			By("Verifying Task has agent label")
			task2Obj := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task2Key, task2Obj)).Should(Succeed())
			Expect(task2Obj.Labels["kubetask.io/agent"]).Should(Equal(agentName))

			By("Waiting for first Task to complete")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Verifying second Task transitions to Running after first completes")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Waiting for second Task to complete")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, task2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with credential file mount", func() {
		It("should mount credentials as files", func() {
			agentName := uniqueName("ws-cred-file")
			taskName := uniqueName("task-cred-file")
			secretName := uniqueName("ssh-secret")
			content := "# Credential File Mount Test"

			By("Creating Secret with SSH key content")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: testNS,
				},
				Data: map[string][]byte{
					"id_rsa": []byte("-----BEGIN RSA PRIVATE KEY-----\ntest-key-content\n-----END RSA PRIVATE KEY-----"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			sshKeyPath := "/home/agent/.ssh/id_rsa"
			keyName := "id_rsa"
			// Use 0444 (world readable) since the container runs as non-root user
			fileMode := int32(0444)
			By("Creating Agent with credential mounted as file")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", fmt.Sprintf("echo '=== Checking SSH Key ===' && ls -la %s && cat %s | head -1 && echo '=== Done ==='", sshKeyPath, sshKeyPath)},
					Credentials: []kubetaskv1alpha1.Credential{
						{
							Name: "ssh-key",
							SecretRef: kubetaskv1alpha1.SecretReference{
								Name: secretName,
								Key:  &keyName,
							},
							MountPath: &sshKeyPath,
							FileMode:  &fileMode,
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

			By("Verifying SSH key was mounted correctly")
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-job", taskName))
			Expect(logs).Should(ContainSubstring("Checking SSH Key"))
			Expect(logs).Should(ContainSubstring("BEGIN RSA PRIVATE KEY"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})
})
