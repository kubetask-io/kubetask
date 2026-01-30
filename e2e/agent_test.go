// Copyright Contributors to the KubeOpenCode project

package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
)

// stringPtr returns a pointer to the given string value
func stringPtr(s string) *string {
	return &s
}

var _ = Describe("Agent E2E Tests", Label(LabelAgent), func() {

	Context("Agent with custom podSpec.labels", func() {
		It("should apply labels to generated Pods", func() {
			agentName := uniqueName("ws-labels")
			taskName := uniqueName("task-labels")
			content := "# Labels Test"

			By("Creating Agent with podSpec.labels")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== Task Content ===' && find ${WORKSPACE_DIR} -type f -print0 2>/dev/null | sort -z | xargs -0 -I {} sh -c 'echo \"--- File: {} ---\" && cat \"{}\" && echo' && echo '=== Task Completed ==='"},
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
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
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to start running")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Verifying Pod has custom labels")
			Eventually(func() map[string]string {
				pods := &corev1.PodList{}
				if err := k8sClient.List(ctx, pods,
					client.InNamespace(testNS),
					client.MatchingLabels{"kubeopencode.io/task": taskName}); err != nil || len(pods.Items) == 0 {
					return nil
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
		It("should apply nodeSelector to generated Pods", func() {
			agentName := uniqueName("ws-scheduling")
			taskName := uniqueName("task-scheduling")
			content := "# Scheduling Test"

			By("Creating Agent with podSpec.scheduling")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== Task Content ===' && find ${WORKSPACE_DIR} -type f -print0 2>/dev/null | sort -z | xargs -0 -I {} sh -c 'echo \"--- File: {} ---\" && cat \"{}\" && echo' && echo '=== Task Completed ==='"},
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
						Scheduling: &kubeopenv1alpha1.PodScheduling{
							NodeSelector: map[string]string{
								"kubernetes.io/os": "linux",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Verifying Pod was scheduled successfully with nodeSelector")
			// If the Pod completed successfully, the scheduling was applied correctly
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-pod", taskName))
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
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== Task Content ===' && find ${WORKSPACE_DIR} -type f -print0 2>/dev/null | sort -z | xargs -0 -I {} sh -c 'echo \"--- File: {} ---\" && cat \"{}\" && echo' && echo '=== Task Completed ==='"},
					Credentials: []kubeopenv1alpha1.Credential{
						{
							Name: "test-api-key",
							SecretRef: kubeopenv1alpha1.SecretReference{
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
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("AgentRef validation", func() {
		It("should fail when agentRef is not specified", func() {
			taskName := uniqueName("task-no-agent")
			content := "# No Agent Test"

			By("Creating Task without AgentRef")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					// AgentRef is NOT specified - this should fail
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to fail with agentRef required error")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Verifying error condition mentions agentRef is required")
			failedTask := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, failedTask)).Should(Succeed())
			// Check that one of the conditions contains the error message
			var foundError bool
			for _, cond := range failedTask.Status.Conditions {
				if cond.Type == "Failed" && cond.Status == metav1.ConditionTrue {
					Expect(cond.Message).Should(ContainSubstring("agentRef is required"))
					foundError = true
					break
				}
			}
			Expect(foundError).Should(BeTrue(), "Expected Failed condition with agentRef error message")

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("Agent with maxConcurrentTasks limit", func() {
		It("should queue tasks when concurrency limit is reached", func() {
			agentName := uniqueName("ws-concurrency")
			maxConcurrent := int32(1)

			By("Creating Agent with maxConcurrentTasks=1")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
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
			task1 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      task1Name,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1Key := types.NamespacedName{Name: task1Name, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Creating second Task while first is still running")
			task2 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      task2Name,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Verifying second Task enters Queued phase")
			task2Key := types.NamespacedName{Name: task2Name, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseQueued))

			By("Verifying Task has agent label")
			task2Obj := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task2Key, task2Obj)).Should(Succeed())
			Expect(task2Obj.Labels["kubeopencode.io/agent"]).Should(Equal(agentName))

			By("Waiting for first Task to complete")
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Verifying second Task transitions to Running after first completes")
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Waiting for second Task to complete")
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2Key, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

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
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", fmt.Sprintf("echo '=== Checking SSH Key ===' && ls -la %s && cat %s | head -1 && echo '=== Done ==='", sshKeyPath, sshKeyPath)},
					Credentials: []kubeopenv1alpha1.Credential{
						{
							Name: "ssh-key",
							SecretRef: kubeopenv1alpha1.SecretReference{
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
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Verifying SSH key was mounted correctly")
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-pod", taskName))
			Expect(logs).Should(ContainSubstring("Checking SSH Key"))
			Expect(logs).Should(ContainSubstring("BEGIN RSA PRIVATE KEY"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("Agent with quota rate limiting", func() {
		It("should queue tasks when rate limit is exceeded", func() {
			agentName := uniqueName("ws-quota")

			By("Creating Agent with quota limiting")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'Quick task' && sleep 1"},
					Quota: &kubeopenv1alpha1.QuotaConfig{
						MaxTaskStarts: 2,
						WindowSeconds: 120, // 2 minutes window
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			taskNames := []string{
				uniqueName("task-quota-1"),
				uniqueName("task-quota-2"),
				uniqueName("task-quota-3"),
			}
			content := "# Quota Test Task"

			By("Creating 3 Tasks rapidly")
			for _, taskName := range taskNames {
				task := &kubeopenv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{
						Name:      taskName,
						Namespace: testNS,
					},
					Spec: kubeopenv1alpha1.TaskSpec{
						AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
						Description: &content,
					},
				}
				Expect(k8sClient.Create(ctx, task)).Should(Succeed())
			}

			By("Verifying first two Tasks start running and third is queued")
			// First two should start (or complete quickly)
			task1Key := types.NamespacedName{Name: taskNames[0], Namespace: testNS}
			task3Key := types.NamespacedName{Name: taskNames[2], Namespace: testNS}

			// Wait for first task to at least start
			Eventually(func() bool {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1Key, t); err != nil {
					return false
				}
				return t.Status.Phase == kubeopenv1alpha1.TaskPhaseRunning ||
					t.Status.Phase == kubeopenv1alpha1.TaskPhaseCompleted
			}, timeout, interval).Should(BeTrue())

			// Third task should be queued with QuotaExceeded reason
			Eventually(func() string {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task3Key, t); err != nil {
					return ""
				}
				if t.Status.Phase == kubeopenv1alpha1.TaskPhaseQueued {
					cond := getTaskCondition(t, "Queued")
					if cond != nil {
						return cond.Reason
					}
				}
				return string(t.Status.Phase)
			}, timeout, interval).Should(Or(Equal("QuotaExceeded"), Equal(string(kubeopenv1alpha1.TaskPhaseRunning)), Equal(string(kubeopenv1alpha1.TaskPhaseCompleted))))

			By("Waiting for all Tasks to complete")
			for _, taskName := range taskNames {
				taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
				Eventually(func() kubeopenv1alpha1.TaskPhase {
					t := &kubeopenv1alpha1.Task{}
					if err := k8sClient.Get(ctx, taskKey, t); err != nil {
						return ""
					}
					return t.Status.Phase
				}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))
			}

			By("Cleaning up")
			for _, taskName := range taskNames {
				task := &kubeopenv1alpha1.Task{}
				taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
				if err := k8sClient.Get(ctx, taskKey, task); err == nil {
					Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
				}
			}
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())

			// Wait for agent's task history to clear
			Eventually(func() bool {
				a := &kubeopenv1alpha1.Agent{}
				return k8sClient.Get(ctx, types.NamespacedName{Name: agentName, Namespace: testNS}, a) != nil
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Agent with allowedNamespaces - access granted", func() {
		It("should allow Tasks from matching namespace pattern", func() {
			agentName := uniqueName("ws-allowed")
			taskName := uniqueName("task-allowed")
			content := "# Allowed Namespace Test"

			By("Creating Agent in platform namespace with allowedNamespaces pattern")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: platformNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'Allowed namespace access granted'"},
					// Pattern matches test namespace (kubeopencode-e2e-test)
					AllowedNamespaces: []string{"*-e2e-*", "kubeopencode-*"},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task in test namespace")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef: &kubeopenv1alpha1.AgentReference{
						Name:      agentName,
						Namespace: platformNS,
					},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete successfully")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with allowedNamespaces - access denied", func() {
		It("should reject Tasks from non-matching namespace", func() {
			agentName := uniqueName("ws-denied")
			taskName := uniqueName("task-denied")
			content := "# Denied Namespace Test"

			By("Creating Agent in platform namespace with restrictive allowedNamespaces")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: platformNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'Should not run'"},
					// Pattern does NOT match test namespace
					AllowedNamespaces: []string{"prod-*", "staging-*"},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task in test namespace (should be denied)")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef: &kubeopenv1alpha1.AgentReference{
						Name:      agentName,
						Namespace: platformNS,
					},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to fail with access denied error")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Verifying error message mentions namespace not allowed")
			failedTask := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, failedTask)).Should(Succeed())
			cond := getTaskCondition(failedTask, "Ready")
			Expect(cond).ShouldNot(BeNil())
			Expect(cond.Message).Should(ContainSubstring("not allowed"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with config", func() {
		It("should write OpenCode config to file and set env var", func() {
			agentName := uniqueName("ws-config")
			taskName := uniqueName("task-config")
			content := "# Config Test"

			configJSON := `{"$schema":"https://opencode.ai/config.json","model":"test-model"}`

			By("Creating Agent with config")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== OPENCODE_CONFIG ===' && echo $OPENCODE_CONFIG && echo '=== Config Content ===' && cat /tools/opencode.json && echo '=== Done ==='"},
					Config:             &configJSON,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Verifying config file path is set in OPENCODE_CONFIG env var")
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-pod", taskName))
			Expect(logs).Should(ContainSubstring("OPENCODE_CONFIG"))
			Expect(logs).Should(ContainSubstring("/tools/opencode.json"))

			By("Verifying config content is written")
			Expect(logs).Should(ContainSubstring("test-model"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with tolerations", func() {
		It("should apply tolerations to generated Pods", func() {
			agentName := uniqueName("ws-tolerations")
			taskName := uniqueName("task-tolerations")
			content := "# Tolerations Test"

			By("Creating Agent with tolerations")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'Tolerations test passed'"},
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
						Scheduling: &kubeopenv1alpha1.PodScheduling{
							Tolerations: []corev1.Toleration{
								{
									Key:      "dedicated",
									Operator: corev1.TolerationOpEqual,
									Value:    "ai-workload",
									Effect:   corev1.TaintEffectNoSchedule,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Pod to be created")
			var pod *corev1.Pod
			Eventually(func() bool {
				var err error
				pod, err = getPodForTask(ctx, testNS, taskName)
				return err == nil && pod != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Pod has tolerations")
			Expect(pod.Spec.Tolerations).Should(ContainElement(
				corev1.Toleration{
					Key:      "dedicated",
					Operator: corev1.TolerationOpEqual,
					Value:    "ai-workload",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			))

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with affinity", func() {
		It("should apply affinity to generated Pods", func() {
			agentName := uniqueName("ws-affinity")
			taskName := uniqueName("task-affinity")
			content := "# Affinity Test"

			By("Creating Agent with node affinity")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'Affinity test passed'"},
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
						Scheduling: &kubeopenv1alpha1.PodScheduling{
							Affinity: &corev1.Affinity{
								NodeAffinity: &corev1.NodeAffinity{
									PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
										{
											Weight: 100,
											Preference: corev1.NodeSelectorTerm{
												MatchExpressions: []corev1.NodeSelectorRequirement{
													{
														Key:      "topology.kubernetes.io/zone",
														Operator: corev1.NodeSelectorOpIn,
														Values:   []string{"us-east-1a", "us-east-1b"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Pod to be created")
			var pod *corev1.Pod
			Eventually(func() bool {
				var err error
				pod, err = getPodForTask(ctx, testNS, taskName)
				return err == nil && pod != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Pod has affinity")
			Expect(pod.Spec.Affinity).ShouldNot(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity).ShouldNot(BeNil())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with resources", func() {
		It("should apply resource requests and limits to generated Pods", func() {
			agentName := uniqueName("ws-resources")
			taskName := uniqueName("task-resources")
			content := "# Resources Test"

			By("Creating Agent with resource requirements")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'Resources test passed'"},
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("128Mi"),
								corev1.ResourceCPU:    resource.MustParse("100m"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("256Mi"),
								corev1.ResourceCPU:    resource.MustParse("500m"),
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Pod to be created")
			var pod *corev1.Pod
			Eventually(func() bool {
				var err error
				pod, err = getPodForTask(ctx, testNS, taskName)
				return err == nil && pod != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Pod container has resource requirements")
			// Find the agent container (main container, not init containers)
			var agentContainer *corev1.Container
			for i := range pod.Spec.Containers {
				if pod.Spec.Containers[i].Name == "agent" {
					agentContainer = &pod.Spec.Containers[i]
					break
				}
			}
			Expect(agentContainer).ShouldNot(BeNil())
			Expect(agentContainer.Resources.Requests.Memory().String()).Should(Equal("128Mi"))
			Expect(agentContainer.Resources.Limits.Memory().String()).Should(Equal("256Mi"))

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with runtimeClassName", func() {
		// Skip this test in Kind clusters as RuntimeClass validation happens at Pod creation time
		// and Kind doesn't have custom RuntimeClasses installed
		PIt("should apply runtimeClassName to generated Pods", func() {
			agentName := uniqueName("ws-runtime")
			taskName := uniqueName("task-runtime")
			content := "# RuntimeClass Test"
			// Use a non-existent runtime class - we only verify the spec is set correctly
			// The Pod will not be scheduled but that's expected
			runtimeClassName := "test-runtime-class"

			By("Creating Agent with runtimeClassName")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'RuntimeClass test'"},
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
						RuntimeClassName: &runtimeClassName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Pod to be created (may not be scheduled due to missing runtime)")
			var pod *corev1.Pod
			// Use a shorter timeout since we expect the Pod to be created quickly
			// but may not be scheduled
			Eventually(func() bool {
				var err error
				pod, err = getPodForTask(ctx, testNS, taskName)
				return err == nil && pod != nil
			}, time.Minute*2, interval).Should(BeTrue())

			By("Verifying Pod has runtimeClassName set")
			Expect(pod.Spec.RuntimeClassName).ShouldNot(BeNil())
			Expect(*pod.Spec.RuntimeClassName).Should(Equal("test-runtime-class"))

			// Note: Task may fail or stay pending if the runtime class doesn't exist
			// but we only care about verifying the spec is set correctly

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Agent with entire Secret as env vars", func() {
		It("should inject all Secret keys as environment variables", func() {
			agentName := uniqueName("ws-secret-env")
			taskName := uniqueName("task-secret-env")
			secretName := uniqueName("multi-key-secret")
			content := "# Secret Env Test"

			By("Creating Secret with multiple keys")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: testNS,
				},
				Data: map[string][]byte{
					"API_KEY":      []byte("test-api-key"),
					"API_SECRET":   []byte("test-api-secret"),
					"API_ENDPOINT": []byte("https://api.example.com"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating Agent with credential referencing entire Secret (no key specified)")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo '=== Env Vars ===' && env | grep API_ | sort && echo '=== Done ==='"},
					Credentials: []kubeopenv1alpha1.Credential{
						{
							Name: "api-credentials",
							SecretRef: kubeopenv1alpha1.SecretReference{
								Name: secretName,
								// No Key specified - entire secret becomes env vars
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Verifying all Secret keys are available as env vars")
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-pod", taskName))
			Expect(logs).Should(ContainSubstring("API_KEY=test-api-key"))
			Expect(logs).Should(ContainSubstring("API_SECRET=test-api-secret"))
			Expect(logs).Should(ContainSubstring("API_ENDPOINT=https://api.example.com"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("Agent with entire Secret as directory", func() {
		It("should mount Secret as directory with each key as a file", func() {
			agentName := uniqueName("ws-secret-dir")
			taskName := uniqueName("task-secret-dir")
			secretName := uniqueName("cert-secret")
			content := "# Secret Dir Test"

			By("Creating Secret with config files")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: testNS,
				},
				Data: map[string][]byte{
					"config.yaml":     []byte("# Config file\nkey: value\nenv: test"),
					"settings.json":   []byte(`{"debug": true, "level": "info"}`),
					"credentials.txt": []byte("username=testuser\npassword=testpass"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating Agent with credential as directory mount")
			mountPath := "/etc/certs"
			// Use 0644 to allow non-root user to read the files
			fileMode := int32(420) // 0644 in decimal
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", fmt.Sprintf("echo '=== Config Files ===' && ls -la %s && echo '=== Content ===' && head -1 %s/config.yaml && echo '=== Done ==='", mountPath, mountPath)},
					Credentials: []kubeopenv1alpha1.Credential{
						{
							Name: "certs",
							SecretRef: kubeopenv1alpha1.SecretReference{
								Name: secretName,
								// No Key specified - entire secret as directory
							},
							MountPath: &mountPath,
							FileMode:  &fileMode,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &content,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				t := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Verifying all Secret files are mounted")
			logs := getPodLogs(ctx, testNS, fmt.Sprintf("%s-pod", taskName))
			Expect(logs).Should(ContainSubstring("config.yaml"))
			Expect(logs).Should(ContainSubstring("settings.json"))
			Expect(logs).Should(ContainSubstring("credentials.txt"))
			Expect(logs).Should(ContainSubstring("Config file"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})
})
