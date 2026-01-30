// Copyright Contributors to the KubeOpenCode project

//go:build integration

// See suite_test.go for explanation of the "integration" build tag pattern.

package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
)

var _ = Describe("TaskController", func() {
	const (
		taskNamespace = "default"
	)

	Context("When creating a Task with description", func() {
		It("Should create a Pod and update Task status", func() {
			taskName := "test-task-description"
			description := "# Test Task\n\nThis is a test task."

			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
				},
			}

			By("Creating the Task")
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is updated to Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Checking Pod is created")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Pod has correct labels")
			Expect(createdPod.Labels).Should(HaveKeyWithValue("app", "kubeopencode"))
			Expect(createdPod.Labels).Should(HaveKeyWithValue("kubeopencode.io/task", taskName))

			By("Verifying Pod cleanup is handled via finalizer (no OwnerReference)")
			Expect(createdPod.OwnerReferences).Should(BeEmpty())

			By("Verifying Pod uses default executor image")
			Expect(createdPod.Spec.Containers).Should(HaveLen(1))
			Expect(createdPod.Spec.Containers[0].Image).Should(Equal(DefaultExecutorImage))

			By("Verifying Pod has OpenCode init container")
			Expect(createdPod.Spec.InitContainers).ShouldNot(BeEmpty())
			Expect(createdPod.Spec.InitContainers[0].Name).Should(Equal("opencode-init"))
			Expect(createdPod.Spec.InitContainers[0].Image).Should(Equal(DefaultAgentImage))

			By("Verifying Task status has PodName set")
			Expect(createdTask.Status.PodName).Should(Equal(podName))
			Expect(createdTask.Status.StartTime).ShouldNot(BeNil())

			By("Checking context ConfigMap is created")
			configMapName := taskName + ContextConfigMapSuffix
			configMapLookupKey := types.NamespacedName{Name: configMapName, Namespace: taskNamespace}
			createdConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, configMapLookupKey, createdConfigMap) == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdConfigMap.Data).Should(HaveKey("workspace-task.md"))
			Expect(createdConfigMap.Data["workspace-task.md"]).Should(ContainSubstring(description))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent reference", func() {
		It("Should use executor image from Agent for worker container and agent image for init container", func() {
			taskName := "test-task-agent"
			agentConfigName := "test-agent-config"
			customAgentImage := "custom-opencode:v1.0.0"
			customExecutorImage := "custom-executor:v1.0.0"
			description := "# Test with Agent"

			By("Creating Agent with custom images")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentConfigName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					AgentImage:         customAgentImage,
					ExecutorImage:      customExecutorImage,
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with Agent reference")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentConfigName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod uses custom executor image for worker container")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() string {
				if err := k8sClient.Get(ctx, podLookupKey, createdPod); err != nil {
					return ""
				}
				if len(createdPod.Spec.Containers) == 0 {
					return ""
				}
				return createdPod.Spec.Containers[0].Image
			}, timeout, interval).Should(Equal(customExecutorImage))

			By("Checking Pod uses custom agent image for init container")
			Expect(createdPod.Spec.InitContainers).ShouldNot(BeEmpty())
			Expect(createdPod.Spec.InitContainers[0].Name).Should(Equal("opencode-init"))
			Expect(createdPod.Spec.InitContainers[0].Image).Should(Equal(customAgentImage))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has credentials", func() {
		It("Should mount credentials as env vars and files", func() {
			taskName := "test-task-creds"
			agentName := "test-workspace-creds"
			secretName := "test-secret"
			envName := "API_TOKEN"
			mountPath := "/home/agent/.ssh/id_rsa"
			description := "# Test with credentials"

			By("Creating Secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: taskNamespace,
				},
				Data: map[string][]byte{
					"token": []byte("secret-token-value"),
					"key":   []byte("ssh-private-key"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating Agent with credentials")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					Credentials: []kubeopenv1alpha1.Credential{
						{
							Name: "api-token",
							SecretRef: kubeopenv1alpha1.SecretReference{
								Name: secretName,
								Key:  stringPtr("token"),
							},
							Env: &envName,
						},
						{
							Name: "ssh-key",
							SecretRef: kubeopenv1alpha1.SecretReference{
								Name: secretName,
								Key:  stringPtr("key"),
							},
							MountPath: &mountPath,
						},
					},
					WorkspaceDir: "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod has credential env var")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, podLookupKey, createdPod); err != nil {
					return false
				}
				return len(createdPod.Spec.Containers) > 0
			}, timeout, interval).Should(BeTrue())

			var tokenEnv *corev1.EnvVar
			for _, env := range createdPod.Spec.Containers[0].Env {
				if env.Name == envName {
					tokenEnv = &env
					break
				}
			}
			Expect(tokenEnv).ShouldNot(BeNil())
			Expect(tokenEnv.ValueFrom).ShouldNot(BeNil())
			Expect(tokenEnv.ValueFrom.SecretKeyRef.Name).Should(Equal(secretName))
			Expect(tokenEnv.ValueFrom.SecretKeyRef.Key).Should(Equal("token"))

			By("Checking Pod has credential volume mount")
			var sshMount *corev1.VolumeMount
			for _, mount := range createdPod.Spec.Containers[0].VolumeMounts {
				if mount.MountPath == mountPath {
					sshMount = &mount
					break
				}
			}
			Expect(sshMount).ShouldNot(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has podSpec.labels", func() {
		It("Should apply labels to the Pod", func() {
			taskName := "test-task-labels"
			agentName := "test-workspace-labels"
			description := "# Test with podSpec.labels"

			By("Creating Agent with podSpec.labels")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
						Labels: map[string]string{
							"network-policy": "agent-restricted",
							"team":           "platform",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod template has custom labels")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, podLookupKey, createdPod); err != nil {
					return false
				}
				return createdPod.Labels != nil
			}, timeout, interval).Should(BeTrue())

			Expect(createdPod.Labels).Should(HaveKeyWithValue("network-policy", "agent-restricted"))
			Expect(createdPod.Labels).Should(HaveKeyWithValue("team", "platform"))
			// Also verify base labels are still present
			Expect(createdPod.Labels).Should(HaveKeyWithValue("app", "kubeopencode"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has podSpec.scheduling", func() {
		It("Should apply scheduling configuration to the Pod", func() {
			taskName := "test-task-scheduling"
			agentName := "test-workspace-scheduling"
			description := "# Test with podSpec.scheduling"

			By("Creating Agent with podSpec.scheduling")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
						Scheduling: &kubeopenv1alpha1.PodScheduling{
							NodeSelector: map[string]string{
								"kubernetes.io/os": "linux",
								"node-type":        "gpu",
							},
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
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod has node selector")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() map[string]string {
				if err := k8sClient.Get(ctx, podLookupKey, createdPod); err != nil {
					return nil
				}
				return createdPod.Spec.NodeSelector
			}, timeout, interval).ShouldNot(BeNil())

			Expect(createdPod.Spec.NodeSelector).Should(HaveKeyWithValue("kubernetes.io/os", "linux"))
			Expect(createdPod.Spec.NodeSelector).Should(HaveKeyWithValue("node-type", "gpu"))

			By("Checking Pod has tolerations")
			// Check that our custom toleration is present (Pods may also have default tolerations)
			var foundDedicatedToleration bool
			for _, t := range createdPod.Spec.Tolerations {
				if t.Key == "dedicated" && t.Value == "ai-workload" {
					foundDedicatedToleration = true
					break
				}
			}
			Expect(foundDedicatedToleration).Should(BeTrue(), "Expected toleration for dedicated=ai-workload")

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When creating a Task with inline Text context", func() {
		It("Should resolve and mount Context content", func() {
			taskName := "test-task-context-inline"
			contextContent := "# Coding Standards\n\nFollow these guidelines."
			description := "Review the code"

			By("Creating Task with inline context")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type:      kubeopenv1alpha1.ContextTypeText,
							Text:      contextContent,
							MountPath: "/workspace/guides/standards.md",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking context ConfigMap is created with resolved content")
			contextConfigMapName := taskName + ContextConfigMapSuffix
			contextConfigMapLookupKey := types.NamespacedName{Name: contextConfigMapName, Namespace: taskNamespace}
			createdContextConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, contextConfigMapLookupKey, createdContextConfigMap) == nil
			}, timeout, interval).Should(BeTrue())

			// Task.md should contain description
			Expect(createdContextConfigMap.Data["workspace-task.md"]).Should(ContainSubstring(description))
			// Mounted context should be at its own key
			Expect(createdContextConfigMap.Data["workspace-guides-standards.md"]).Should(ContainSubstring(contextContent))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When creating a Task with inline ConfigMap Context without key and mountPath", func() {
		It("Should aggregate all ConfigMap keys to context file", func() {
			taskName := "test-task-configmap-all-keys"
			configMapName := "test-guides-configmap"
			description := "Review the guides"

			By("Creating ConfigMap with multiple keys")
			guidesConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: taskNamespace,
				},
				Data: map[string]string{
					"style-guide.md":    "# Style Guide\n\nFollow these styles.",
					"security-guide.md": "# Security Guide\n\nFollow security practices.",
				},
			}
			Expect(k8sClient.Create(ctx, guidesConfigMap)).Should(Succeed())

			By("Creating Task with inline ConfigMap context (no mountPath)")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeConfigMap,
							ConfigMap: &kubeopenv1alpha1.ConfigMapContext{
								Name: configMapName,
								// No Key specified - should aggregate all keys
							},
							// No MountPath - should aggregate to context file
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking all ConfigMap keys are aggregated to context file")
			contextConfigMapName := taskName + ContextConfigMapSuffix
			contextConfigMapLookupKey := types.NamespacedName{Name: contextConfigMapName, Namespace: taskNamespace}
			createdContextConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, contextConfigMapLookupKey, createdContextConfigMap) == nil
			}, timeout, interval).Should(BeTrue())

			// Description should be in task.md
			taskMdContent := createdContextConfigMap.Data["workspace-task.md"]
			Expect(taskMdContent).Should(ContainSubstring(description))
			// task.md should NOT contain context
			Expect(taskMdContent).ShouldNot(ContainSubstring("<context"))

			// Context should be in context file
			contextFileContent := createdContextConfigMap.Data["workspace-.kubeopencode-context.md"]
			// Context wrapper should be present
			Expect(contextFileContent).Should(ContainSubstring("<context"))
			Expect(contextFileContent).Should(ContainSubstring("</context>"))
			// All ConfigMap keys should be wrapped in <file> tags
			Expect(contextFileContent).Should(ContainSubstring(`<file name="security-guide.md">`))
			Expect(contextFileContent).Should(ContainSubstring("# Security Guide"))
			Expect(contextFileContent).Should(ContainSubstring(`<file name="style-guide.md">`))
			Expect(contextFileContent).Should(ContainSubstring("# Style Guide"))
			Expect(contextFileContent).Should(ContainSubstring("</file>"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, guidesConfigMap)).Should(Succeed())
		})
	})

	Context("When creating a Task with inline Text context without mountPath", func() {
		It("Should append context to context file with XML tags", func() {
			taskName := "test-task-context-aggregate"
			contextContent := "# Security Guidelines\n\nFollow security best practices."
			description := "Review security compliance"

			By("Creating Task with inline Text context (no mountPath)")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeText,
							Text: contextContent,
							// No MountPath - should be appended to context file
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking context is appended to context file with XML tags")
			contextConfigMapName := taskName + ContextConfigMapSuffix
			contextConfigMapLookupKey := types.NamespacedName{Name: contextConfigMapName, Namespace: taskNamespace}
			createdContextConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, contextConfigMapLookupKey, createdContextConfigMap) == nil
			}, timeout, interval).Should(BeTrue())

			// Description should be in task.md
			taskMdContent := createdContextConfigMap.Data["workspace-task.md"]
			Expect(taskMdContent).Should(ContainSubstring(description))
			// task.md should NOT contain context
			Expect(taskMdContent).ShouldNot(ContainSubstring("<context"))

			// Context should be in context file
			contextFileContent := createdContextConfigMap.Data["workspace-.kubeopencode-context.md"]
			Expect(contextFileContent).Should(ContainSubstring("<context"))
			Expect(contextFileContent).Should(ContainSubstring(contextContent))
			Expect(contextFileContent).Should(ContainSubstring("</context>"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has inline contexts", func() {
		It("Should merge agent contexts with task contexts in context file", func() {
			taskName := "test-task-agent-contexts"
			agentName := "test-agent-with-contexts"
			agentContextContent := "# Agent Guidelines\n\nThese are default guidelines."
			taskContextContent := "# Task Guidelines\n\nThese are task-specific guidelines."
			description := "Do the task"

			By("Creating Agent with inline context")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeText,
							Text: agentContextContent,
							// No mountPath - should be appended to context file
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with inline context")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeText,
							Text: taskContextContent,
							// No mountPath - should be appended to context file
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking context ConfigMap contains both contexts")
			contextConfigMapName := taskName + ContextConfigMapSuffix
			contextConfigMapLookupKey := types.NamespacedName{Name: contextConfigMapName, Namespace: taskNamespace}
			createdContextConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, contextConfigMapLookupKey, createdContextConfigMap) == nil
			}, timeout, interval).Should(BeTrue())

			// Description should be in task.md
			taskMdContent := createdContextConfigMap.Data["workspace-task.md"]
			Expect(taskMdContent).Should(ContainSubstring(description))
			// task.md should NOT contain context
			Expect(taskMdContent).ShouldNot(ContainSubstring(agentContextContent))
			Expect(taskMdContent).ShouldNot(ContainSubstring(taskContextContent))

			// Both contexts should be in context file
			contextFileContent := createdContextConfigMap.Data["workspace-.kubeopencode-context.md"]
			Expect(contextFileContent).Should(ContainSubstring(agentContextContent))
			Expect(contextFileContent).Should(ContainSubstring(taskContextContent))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When a Task's Job completes successfully", func() {
		It("Should update Task status to Completed", func() {
			taskName := "test-task-success"
			description := "# Success test"

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Pod to be created")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			By("Simulating Pod success")
			createdPod.Status.Phase = corev1.PodSucceeded
			Expect(k8sClient.Status().Update(ctx, createdPod)).Should(Succeed())

			By("Checking Task status is Completed")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Checking CompletionTime is set")
			finalTask := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, finalTask)).Should(Succeed())
			Expect(finalTask.Status.CompletionTime).ShouldNot(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When a Task's Job fails", func() {
		It("Should update Task status to Failed", func() {
			taskName := "test-task-failure"
			description := "# Failure test"

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Pod to be created")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			By("Simulating Pod failure")
			createdPod.Status.Phase = corev1.PodFailed
			Expect(k8sClient.Status().Update(ctx, createdPod)).Should(Succeed())

			By("Checking Task status is Failed")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When Agent has maxConcurrentTasks set", func() {
		It("Should queue Tasks when at capacity", func() {
			agentName := "test-agent-concurrency"
			maxConcurrent := int32(1)
			description1 := "# Task 1"
			description2 := "# Task 2"

			By("Creating Agent with maxConcurrentTasks=1")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					MaxConcurrentTasks: &maxConcurrent,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating first Task")
			task1 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-concurrent-1",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1LookupKey := types.NamespacedName{Name: "test-task-concurrent-1", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Verifying first Task has agent label")
			task1Updated := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task1LookupKey, task1Updated)).Should(Succeed())
			Expect(task1Updated.Labels).Should(HaveKeyWithValue(AgentLabelKey, agentName))

			By("Creating second Task")
			task2 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-concurrent-2",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Checking second Task is Queued")
			task2LookupKey := types.NamespacedName{Name: "test-task-concurrent-2", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseQueued))

			By("Verifying second Task has Queued condition")
			task2Updated := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task2LookupKey, task2Updated)).Should(Succeed())
			Expect(task2Updated.Labels).Should(HaveKeyWithValue(AgentLabelKey, agentName))

			// Check for Queued condition
			var queuedCondition *metav1.Condition
			for i := range task2Updated.Status.Conditions {
				if task2Updated.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeQueued {
					queuedCondition = &task2Updated.Status.Conditions[i]
					break
				}
			}
			Expect(queuedCondition).ShouldNot(BeNil())
			Expect(queuedCondition.Status).Should(Equal(metav1.ConditionTrue))
			Expect(queuedCondition.Reason).Should(Equal(kubeopenv1alpha1.ReasonAgentAtCapacity))

			By("Simulating first Task completion")
			pod1Name := fmt.Sprintf("%s-pod", "test-task-concurrent-1")
			pod1LookupKey := types.NamespacedName{Name: pod1Name, Namespace: taskNamespace}
			pod1 := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, pod1LookupKey, pod1)).Should(Succeed())
			pod1.Status.Phase = corev1.PodSucceeded
			Expect(k8sClient.Status().Update(ctx, pod1)).Should(Succeed())

			By("Waiting for first Task to complete")
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Checking second Task transitions to Running")
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, task2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should allow unlimited Tasks when maxConcurrentTasks is 0", func() {
			agentName := "test-agent-unlimited"
			maxConcurrent := int32(0) // 0 means unlimited
			description1 := "# Task 1"
			description2 := "# Task 2"

			By("Creating Agent with maxConcurrentTasks=0 (unlimited)")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					MaxConcurrentTasks: &maxConcurrent,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating first Task")
			task1 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-unlimited-1",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1LookupKey := types.NamespacedName{Name: "test-task-unlimited-1", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Creating second Task")
			task2 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-unlimited-2",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Checking second Task is also Running (not Queued)")
			task2LookupKey := types.NamespacedName{Name: "test-task-unlimited-2", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, task2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should allow unlimited Tasks when maxConcurrentTasks is not set", func() {
			agentName := "test-agent-no-limit"
			description1 := "# Task 1"
			description2 := "# Task 2"

			By("Creating Agent without maxConcurrentTasks (nil)")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					// MaxConcurrentTasks not set (nil)
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating first Task")
			task1 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-nolimit-1",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1LookupKey := types.NamespacedName{Name: "test-task-nolimit-1", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Creating second Task")
			task2 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-nolimit-2",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Checking second Task is also Running (not Queued)")
			task2LookupKey := types.NamespacedName{Name: "test-task-nolimit-2", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, task2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When stopping a Running Task via annotation", func() {
		It("Should delete Pod and set Task status to Completed with Stopped condition", func() {
			taskName := "test-task-stop"
			agentName := "test-agent-stop"
			description := "# Stop test"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to be Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Checking Pod is created")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			By("Adding stop annotation to Task")
			currentTask := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, currentTask)).Should(Succeed())
			if currentTask.Annotations == nil {
				currentTask.Annotations = make(map[string]string)
			}
			currentTask.Annotations[AnnotationStop] = "true"
			Expect(k8sClient.Update(ctx, currentTask)).Should(Succeed())

			By("Checking Task status is Completed")
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Checking Pod is deleted")
			// Pod should be deleted when stop annotation is set
			Eventually(func() bool {
				deletedPod := &corev1.Pod{}
				err := k8sClient.Get(ctx, podLookupKey, deletedPod)
				return err != nil // Pod should not be found
			}, timeout, interval).Should(BeTrue())

			By("Checking Task has Stopped condition")
			finalTask := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, finalTask)).Should(Succeed())

			var stoppedCondition *metav1.Condition
			for i := range finalTask.Status.Conditions {
				if finalTask.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeStopped {
					stoppedCondition = &finalTask.Status.Conditions[i]
					break
				}
			}
			Expect(stoppedCondition).ShouldNot(BeNil())
			Expect(stoppedCondition.Status).Should(Equal(metav1.ConditionTrue))
			Expect(stoppedCondition.Reason).Should(Equal(kubeopenv1alpha1.ReasonUserStopped))

			By("Checking CompletionTime is set")
			Expect(finalTask.Status.CompletionTime).ShouldNot(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Context validation", func() {
		It("Should fail when inline Git context has no mountPath", func() {
			taskName := "test-task-inline-git-no-mountpath"
			description := "Test inline Git validation"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-inline-git-validation",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with inline Git context without mountPath")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agent.Name},
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeGit,
							Git: &kubeopenv1alpha1.GitContext{
								Repository: "https://github.com/example/repo",
							},
							// No MountPath - should fail validation
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Failed with validation error")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Verifying error message mentions mountPath requirement")
			var readyCondition *metav1.Condition
			for i := range createdTask.Status.Conditions {
				if createdTask.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeReady {
					readyCondition = &createdTask.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Message).Should(ContainSubstring("git context requires mountPath"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should fail when multiple contexts use same mountPath", func() {
			taskName := "test-task-mountpath-conflict"
			description := "Test mountPath conflict detection"
			conflictPath := "/workspace/config.yaml"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-mountpath-conflict",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with two inline contexts with same mountPath")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agent.Name},
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type:      kubeopenv1alpha1.ContextTypeText,
							Text:      "content from context 1",
							MountPath: conflictPath,
						},
						{
							Type:      kubeopenv1alpha1.ContextTypeText,
							Text:      "content from context 2",
							MountPath: conflictPath, // Same path - should fail
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Failed with conflict error")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Verifying error message mentions mount path conflict")
			var readyCondition *metav1.Condition
			for i := range createdTask.Status.Conditions {
				if createdTask.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeReady {
					readyCondition = &createdTask.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Message).Should(ContainSubstring("mount path conflict"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should succeed when inline Git context has mountPath specified", func() {
			taskName := "test-task-inline-git-with-mountpath"
			description := "Test inline Git with mountPath"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-inline-git-valid",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with inline Git context with mountPath")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agent.Name},
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeGit,
							Git: &kubeopenv1alpha1.GitContext{
								Repository: "https://github.com/example/repo",
							},
							MountPath: "/workspace/my-repo", // Has mountPath - should succeed
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Running (not Failed)")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Runtime Context", func() {
		It("should inject RuntimeSystemPrompt when inline Runtime context is used", func() {
			ctx := context.Background()
			taskName := "task-runtime-context-inline"
			taskNamespace := "default"
			agentName := "agent-runtime-inline"
			description := "Test task with Runtime context"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with inline Runtime context")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type:    kubeopenv1alpha1.ContextTypeRuntime,
							Runtime: &kubeopenv1alpha1.RuntimeContext{},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Running phase")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Checking context ConfigMap contains RuntimeSystemPrompt")
			cmName := taskName + "-context"
			cmLookupKey := types.NamespacedName{Name: cmName, Namespace: taskNamespace}
			cm := &corev1.ConfigMap{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, cmLookupKey, cm); err != nil {
					return false
				}
				return cm.Data != nil
			}, timeout, interval).Should(BeTrue())

			// Verify ConfigMap contains RuntimeSystemPrompt content in context file
			// Runtime context (like all contexts without mountPath) goes to context file
			contextFileContent, exists := cm.Data["workspace-.kubeopencode-context.md"]
			Expect(exists).To(BeTrue(), "workspace-.kubeopencode-context.md key should exist in ConfigMap")
			Expect(contextFileContent).To(ContainSubstring("KubeOpenCode Runtime Context"))
			Expect(contextFileContent).To(ContainSubstring("TASK_NAME"))
			Expect(contextFileContent).To(ContainSubstring("TASK_NAMESPACE"))
			Expect(contextFileContent).To(ContainSubstring("kubectl get task"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("TaskTemplate", func() {
		It("Should merge TaskTemplate contexts with Task contexts", func() {
			taskName := "test-task-with-template"
			templateName := "test-task-template"
			agentName := "test-agent-template"
			templateContext := "# Template Guidelines\n\nFollow these template rules."
			taskContext := "# Task Guidelines\n\nFollow these task-specific rules."
			templateDescription := "Default template description"
			taskDescription := "Task-specific description"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating TaskTemplate")
			template := &kubeopenv1alpha1.TaskTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      templateName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskTemplateSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &templateDescription,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeText,
							Text: templateContext,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, template)).Should(Succeed())

			By("Creating Task with TaskTemplateRef")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					TaskTemplateRef: &kubeopenv1alpha1.TaskTemplateReference{Name: templateName},
					Description:     &taskDescription,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeText,
							Text: taskContext,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Checking context ConfigMap contains both template and task contexts")
			configMapName := taskName + ContextConfigMapSuffix
			configMapLookupKey := types.NamespacedName{Name: configMapName, Namespace: taskNamespace}
			createdConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, configMapLookupKey, createdConfigMap) == nil
			}, timeout, interval).Should(BeTrue())

			// Task description should be in task.md
			taskMdContent := createdConfigMap.Data["workspace-task.md"]
			Expect(taskMdContent).Should(ContainSubstring(taskDescription))
			// task.md should NOT contain contexts
			Expect(taskMdContent).ShouldNot(ContainSubstring(templateContext))
			Expect(taskMdContent).ShouldNot(ContainSubstring(taskContext))

			// Both contexts should be in context file
			contextFileContent := createdConfigMap.Data["workspace-.kubeopencode-context.md"]
			Expect(contextFileContent).Should(ContainSubstring(templateContext))
			Expect(contextFileContent).Should(ContainSubstring(taskContext))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, template)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should use TaskTemplate description when Task has no description", func() {
			taskName := "test-task-template-desc"
			templateName := "test-template-desc"
			agentName := "test-agent-template-desc"
			templateDescription := "This is the template default description"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating TaskTemplate with description")
			template := &kubeopenv1alpha1.TaskTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      templateName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskTemplateSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &templateDescription,
				},
			}
			Expect(k8sClient.Create(ctx, template)).Should(Succeed())

			By("Creating Task without description (should use template's)")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					TaskTemplateRef: &kubeopenv1alpha1.TaskTemplateReference{Name: templateName},
					// No Description - should use template's
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				createdTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Checking context ConfigMap contains template description")
			configMapName := taskName + ContextConfigMapSuffix
			configMapLookupKey := types.NamespacedName{Name: configMapName, Namespace: taskNamespace}
			createdConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, configMapLookupKey, createdConfigMap) == nil
			}, timeout, interval).Should(BeTrue())

			taskMdContent := createdConfigMap.Data["workspace-task.md"]
			Expect(taskMdContent).Should(ContainSubstring(templateDescription))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, template)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should use TaskTemplate agentRef when Task has no agentRef", func() {
			taskName := "test-task-template-agentref"
			templateName := "test-template-agentref"
			agentName := "test-agent-from-template"
			customAgentImage := "custom-agent-image:v1.0.0"
			description := "Test task using template agentRef"

			By("Creating Agent with custom image")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					AgentImage:         customAgentImage,
					WorkspaceDir:       "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating TaskTemplate with agentRef")
			template := &kubeopenv1alpha1.TaskTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      templateName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskTemplateSpec{
					AgentRef: &kubeopenv1alpha1.AgentReference{Name: agentName},
				},
			}
			Expect(k8sClient.Create(ctx, template)).Should(Succeed())

			By("Creating Task without agentRef (should use template's)")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					TaskTemplateRef: &kubeopenv1alpha1.TaskTemplateReference{Name: templateName},
					Description:     &description,
					// No AgentRef - should use template's
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod uses agent from template")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() string {
				if err := k8sClient.Get(ctx, podLookupKey, createdPod); err != nil {
					return ""
				}
				if len(createdPod.Spec.InitContainers) == 0 {
					return ""
				}
				return createdPod.Spec.InitContainers[0].Image
			}, timeout, interval).Should(Equal(customAgentImage))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, template)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should fail when TaskTemplate is not found", func() {
			taskName := "test-task-missing-template"
			description := "Test task with missing template"

			By("Creating Task with non-existent TaskTemplateRef")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					TaskTemplateRef: &kubeopenv1alpha1.TaskTemplateReference{Name: "non-existent-template"},
					Description:     &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Failed")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Checking error message mentions TaskTemplate not found")
			var readyCondition *metav1.Condition
			for i := range createdTask.Status.Conditions {
				if createdTask.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeReady {
					readyCondition = &createdTask.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Reason).Should(Equal(kubeopenv1alpha1.ReasonTaskTemplateError))
			Expect(readyCondition.Message).Should(ContainSubstring("TaskTemplate"))
			Expect(readyCondition.Message).Should(ContainSubstring("not found"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has OpenCode config", func() {
		It("Should set OPENCODE_CONFIG env var and mount config file", func() {
			agentName := "agent-with-config"
			taskName := "test-task-with-config"
			description := "Test task with config"

			configJSON := `{"model": "google/gemini-2.5-pro", "small_model": "google/gemini-2.5-flash"}`

			// Create Agent with Config
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					AgentImage:         "test-opencode:latest",
					ExecutorImage:      "test-executor:latest",
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
					Config:             &configJSON,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			// Create Task referencing the Agent
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod is created")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying OPENCODE_CONFIG env var is set")
			agentContainer := createdPod.Spec.Containers[0]
			var foundOpenCodeConfigEnv bool
			for _, env := range agentContainer.Env {
				if env.Name == OpenCodeConfigEnvVar {
					Expect(env.Value).Should(Equal(OpenCodeConfigPath))
					foundOpenCodeConfigEnv = true
					break
				}
			}
			Expect(foundOpenCodeConfigEnv).Should(BeTrue(), "OPENCODE_CONFIG env var should be set")

			By("Verifying context-init container mounts /tools volume")
			var contextInitContainer *corev1.Container
			for i, initC := range createdPod.Spec.InitContainers {
				if initC.Name == "context-init" {
					contextInitContainer = &createdPod.Spec.InitContainers[i]
					break
				}
			}
			Expect(contextInitContainer).ShouldNot(BeNil(), "context-init container should exist")

			var hasToolsMount bool
			for _, vm := range contextInitContainer.VolumeMounts {
				if vm.Name == ToolsVolumeName && vm.MountPath == ToolsMountPath {
					hasToolsMount = true
					break
				}
			}
			Expect(hasToolsMount).Should(BeTrue(), "context-init should mount /tools volume for config")

			By("Verifying ConfigMap contains config content")
			configMapName := taskName + ContextConfigMapSuffix
			configMapLookupKey := types.NamespacedName{Name: configMapName, Namespace: taskNamespace}
			createdConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, configMapLookupKey, createdConfigMap) == nil
			}, timeout, interval).Should(BeTrue())
			expectedConfigKey := sanitizeConfigMapKey(OpenCodeConfigPath)
			Expect(createdConfigMap.Data).Should(HaveKey(expectedConfigKey))
			Expect(createdConfigMap.Data[expectedConfigKey]).Should(Equal(configJSON))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should reject invalid JSON in config", func() {
			agentName := "agent-invalid-config"
			taskName := "test-task-invalid-config"
			description := "Test task with invalid config"

			invalidConfigJSON := `{"model": "google/gemini-2.5-pro", invalid json}`

			// Create Agent with invalid Config
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					AgentImage:         "test-opencode:latest",
					ExecutorImage:      "test-executor:latest",
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
					Config:             &invalidConfigJSON,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			// Create Task referencing the Agent
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status shows error due to invalid JSON")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Verifying error condition mentions invalid JSON")
			var readyCondition *metav1.Condition
			for i, cond := range createdTask.Status.Conditions {
				if cond.Type == kubeopenv1alpha1.ConditionTypeReady {
					readyCondition = &createdTask.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Message).Should(ContainSubstring("invalid JSON"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Task Cleanup", func() {
		It("Should delete Task after TTL expires", func() {
			taskName := "test-task-ttl-cleanup"
			agentName := "test-agent-ttl-cleanup"
			description := "Test TTL cleanup"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating KubeOpenCodeConfig with TTL cleanup")
			ttlSeconds := int32(2) // 2 seconds for quick test
			config := &kubeopenv1alpha1.KubeOpenCodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.KubeOpenCodeConfigSpec{
					Cleanup: &kubeopenv1alpha1.CleanupConfig{
						TTLSecondsAfterFinished: &ttlSeconds,
					},
				},
			}
			Expect(k8sClient.Create(ctx, config)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to be Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Simulating Pod completion")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			createdPod.Status.Phase = corev1.PodSucceeded
			createdPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name: "agent",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 0,
						},
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, createdPod)).Should(Succeed())

			By("Waiting for Task to be Completed")
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Waiting for Task to be deleted due to TTL")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, taskLookupKey, createdTask)
				return err != nil // Task should be deleted (NotFound)
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, config)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should not delete Task when no cleanup is configured", func() {
			taskName := "test-task-no-cleanup"
			agentName := "test-agent-no-cleanup"
			description := "Test no cleanup"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task without KubeOpenCodeConfig")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to be Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Simulating Pod completion")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			createdPod.Status.Phase = corev1.PodSucceeded
			createdPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name: "agent",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 0,
						},
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, createdPod)).Should(Succeed())

			By("Waiting for Task to be Completed")
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))

			By("Verifying Task still exists after waiting")
			// Wait a bit and verify Task is not deleted
			Consistently(func() bool {
				err := k8sClient.Get(ctx, taskLookupKey, createdTask)
				return err == nil // Task should still exist
			}, "3s", interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			// Wait for Task to be fully deleted (finalizer processed)
			Eventually(func() bool {
				err := k8sClient.Get(ctx, taskLookupKey, &kubeopenv1alpha1.Task{})
				return apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should delete oldest Tasks when retention limit is exceeded", func() {
			agentName := "test-agent-retention"
			description := "Test retention cleanup"

			By("Creating Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating KubeOpenCodeConfig with retention limit of 2")
			maxRetained := int32(2)
			config := &kubeopenv1alpha1.KubeOpenCodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.KubeOpenCodeConfigSpec{
					Cleanup: &kubeopenv1alpha1.CleanupConfig{
						MaxRetainedTasks: &maxRetained,
					},
				},
			}
			Expect(k8sClient.Create(ctx, config)).Should(Succeed())

			By("Creating and completing 3 Tasks")
			taskNames := []string{"test-task-retention-1", "test-task-retention-2", "test-task-retention-3"}
			for _, taskName := range taskNames {
				task := &kubeopenv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{
						Name:      taskName,
						Namespace: taskNamespace,
					},
					Spec: kubeopenv1alpha1.TaskSpec{
						AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
						Description: &description,
					},
				}
				Expect(k8sClient.Create(ctx, task)).Should(Succeed())

				// Wait for Task to be Running
				taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
				createdTask := &kubeopenv1alpha1.Task{}
				Eventually(func() kubeopenv1alpha1.TaskPhase {
					if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
						return ""
					}
					return createdTask.Status.Phase
				}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

				// Simulate Pod completion
				podName := fmt.Sprintf("%s-pod", taskName)
				podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
				createdPod := &corev1.Pod{}
				Eventually(func() bool {
					return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
				}, timeout, interval).Should(BeTrue())

				createdPod.Status.Phase = corev1.PodSucceeded
				createdPod.Status.ContainerStatuses = []corev1.ContainerStatus{
					{
						Name: "agent",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 0,
							},
						},
					},
				}
				Expect(k8sClient.Status().Update(ctx, createdPod)).Should(Succeed())

				// Wait for Task to be Completed
				Eventually(func() kubeopenv1alpha1.TaskPhase {
					if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
						return ""
					}
					return createdTask.Status.Phase
				}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseCompleted))
			}

			By("Waiting for retention limit to reduce Tasks to 2")
			// One of the Tasks should be deleted, leaving exactly 2
			Eventually(func() int {
				count := 0
				for _, taskName := range taskNames {
					taskKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
					task := &kubeopenv1alpha1.Task{}
					if err := k8sClient.Get(ctx, taskKey, task); err == nil {
						// Only count if not being deleted (no deletion timestamp)
						if task.DeletionTimestamp == nil {
							count++
						}
					}
				}
				return count
			}, timeout, interval).Should(Equal(2))

			By("Verifying exactly 2 Tasks remain")
			remainingCount := 0
			for _, taskName := range taskNames {
				taskKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
				task := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, task); err == nil && task.DeletionTimestamp == nil {
					remainingCount++
				}
			}
			Expect(remainingCount).To(Equal(2))

			By("Cleaning up")
			for _, taskName := range taskNames {
				task := &kubeopenv1alpha1.Task{}
				taskKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
				if err := k8sClient.Get(ctx, taskKey, task); err == nil {
					Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
				}
			}
			// Wait for all Tasks to be fully deleted
			for _, taskName := range taskNames {
				taskKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, taskKey, &kubeopenv1alpha1.Task{})
					return apierrors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())
			}
			Expect(k8sClient.Delete(ctx, config)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When Agent has quota configured", func() {
		It("Should queue Tasks when quota is exceeded", func() {
			agentName := "test-agent-quota"
			maxTaskStarts := int32(2)
			windowSeconds := int32(60)
			description1 := "# Task 1"
			description2 := "# Task 2"
			description3 := "# Task 3"

			By("Creating Agent with quota: maxTaskStarts=2, windowSeconds=60")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Quota: &kubeopenv1alpha1.QuotaConfig{
						MaxTaskStarts: maxTaskStarts,
						WindowSeconds: windowSeconds,
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating first Task")
			task1 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-quota-1",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1LookupKey := types.NamespacedName{Name: "test-task-quota-1", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Creating second Task")
			task2 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-quota-2",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Waiting for second Task to be Running")
			task2LookupKey := types.NamespacedName{Name: "test-task-quota-2", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Creating third Task (should exceed quota)")
			task3 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-quota-3",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description3,
				},
			}
			Expect(k8sClient.Create(ctx, task3)).Should(Succeed())

			By("Checking third Task is Queued due to quota")
			task3LookupKey := types.NamespacedName{Name: "test-task-quota-3", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task3LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseQueued))

			By("Verifying third Task has Queued condition with QuotaExceeded reason")
			task3Updated := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task3LookupKey, task3Updated)).Should(Succeed())

			var queuedCondition *metav1.Condition
			for i := range task3Updated.Status.Conditions {
				if task3Updated.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeQueued {
					queuedCondition = &task3Updated.Status.Conditions[i]
					break
				}
			}
			Expect(queuedCondition).ShouldNot(BeNil())
			Expect(queuedCondition.Status).Should(Equal(metav1.ConditionTrue))
			Expect(queuedCondition.Reason).Should(Equal(kubeopenv1alpha1.ReasonQuotaExceeded))

			By("Verifying Agent has TaskStartHistory populated")
			agentLookupKey := types.NamespacedName{Name: agentName, Namespace: taskNamespace}
			agentUpdated := &kubeopenv1alpha1.Agent{}
			Expect(k8sClient.Get(ctx, agentLookupKey, agentUpdated)).Should(Succeed())
			Expect(len(agentUpdated.Status.TaskStartHistory)).Should(Equal(2))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, task2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, task3)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should work with both maxConcurrentTasks and quota", func() {
			agentName := "test-agent-quota-capacity"
			maxConcurrent := int32(1)
			maxTaskStarts := int32(5)
			windowSeconds := int32(60)
			description1 := "# Task 1"
			description2 := "# Task 2"

			By("Creating Agent with maxConcurrentTasks=1 and quota: maxTaskStarts=5")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					MaxConcurrentTasks: &maxConcurrent,
					Quota: &kubeopenv1alpha1.QuotaConfig{
						MaxTaskStarts: maxTaskStarts,
						WindowSeconds: windowSeconds,
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating first Task")
			task1 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-both-limits-1",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1LookupKey := types.NamespacedName{Name: "test-task-both-limits-1", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Creating second Task (should be queued due to capacity, not quota)")
			task2 := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-both-limits-2",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Checking second Task is Queued due to capacity")
			task2LookupKey := types.NamespacedName{Name: "test-task-both-limits-2", Namespace: taskNamespace}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				updatedTask := &kubeopenv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseQueued))

			By("Verifying second Task is queued due to capacity (not quota)")
			task2Updated := &kubeopenv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task2LookupKey, task2Updated)).Should(Succeed())

			var queuedCondition *metav1.Condition
			for i := range task2Updated.Status.Conditions {
				if task2Updated.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeQueued {
					queuedCondition = &task2Updated.Status.Conditions[i]
					break
				}
			}
			Expect(queuedCondition).ShouldNot(BeNil())
			Expect(queuedCondition.Status).Should(Equal(metav1.ConditionTrue))
			Expect(queuedCondition.Reason).Should(Equal(kubeopenv1alpha1.ReasonAgentAtCapacity))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, task2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Cross-namespace Agent with AllowedNamespaces", func() {
		It("Should allow Task when allowedNamespaces is empty (default: all allowed)", func() {
			agentName := "test-agent-cross-ns-empty"
			agentNamespace := "default"
			taskNamespace := "default" // Using default namespace for simplicity
			description := "Test cross-namespace access"

			By("Creating Agent without AllowedNamespaces (should allow all)")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: agentNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					// AllowedNamespaces is empty - should allow all namespaces
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task referencing the Agent")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-cross-ns-allowed",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef: &kubeopenv1alpha1.AgentReference{
						Name:      agentName,
						Namespace: agentNamespace,
					},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Running (not Failed)")
			taskLookupKey := types.NamespacedName{Name: task.Name, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should allow Task when namespace matches AllowedNamespaces pattern", func() {
			agentName := "test-agent-pattern-match"
			agentNamespace := "default"
			taskNamespace := "default" // Matches pattern
			description := "Test pattern matching"

			By("Creating Agent with AllowedNamespaces pattern")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: agentNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					AllowedNamespaces:  []string{"default", "team-*"},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task in matching namespace")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-pattern-match",
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef: &kubeopenv1alpha1.AgentReference{
						Name:      agentName,
						Namespace: agentNamespace,
					},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Running")
			taskLookupKey := types.NamespacedName{Name: task.Name, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should reject Task when namespace doesn't match AllowedNamespaces", func() {
			agentName := "test-agent-ns-denied"
			agentNamespace := "default"
			description := "Test denied access"

			By("Creating test namespace for denied access")
			deniedNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-denied-ns",
				},
			}
			err := k8sClient.Create(ctx, deniedNs)
			if err != nil && !apierrors.IsAlreadyExists(err) {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("Creating Agent with AllowedNamespaces that doesn't include test-denied-ns")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: agentNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					AllowedNamespaces:  []string{"prod-*", "staging"},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task in non-matching namespace")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-ns-denied",
					Namespace: "test-denied-ns",
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef: &kubeopenv1alpha1.AgentReference{
						Name:      agentName,
						Namespace: agentNamespace,
					},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Failed with AgentError reason")
			taskLookupKey := types.NamespacedName{Name: task.Name, Namespace: "test-denied-ns"}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Verifying error message mentions namespace not allowed")
			var readyCondition *metav1.Condition
			for i := range createdTask.Status.Conditions {
				if createdTask.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeReady {
					readyCondition = &createdTask.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Reason).Should(Equal(kubeopenv1alpha1.ReasonAgentError))
			Expect(readyCondition.Message).Should(ContainSubstring("not allowed"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should support glob patterns like team-*", func() {
			agentName := "test-agent-glob-pattern"
			agentNamespace := "default"
			description := "Test glob pattern matching"

			By("Creating test namespace matching pattern")
			teamNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "team-alpha",
				},
			}
			err := k8sClient.Create(ctx, teamNs)
			if err != nil && !apierrors.IsAlreadyExists(err) {
				Expect(err).ShouldNot(HaveOccurred())
			}

			By("Creating Agent with glob pattern in AllowedNamespaces")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: agentNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					AllowedNamespaces:  []string{"team-*"},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task in matching namespace (team-alpha)")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-glob-match",
					Namespace: "team-alpha",
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef: &kubeopenv1alpha1.AgentReference{
						Name:      agentName,
						Namespace: agentNamespace,
					},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Running (pattern matched)")
			taskLookupKey := types.NamespacedName{Name: task.Name, Namespace: "team-alpha"}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Custom agent command", func() {
		It("Should use custom command when specified", func() {
			agentName := "test-agent-custom-cmd"
			taskName := "test-task-custom-cmd"
			description := "Test custom command"

			By("Creating Agent with custom command")
			customCmd := []string{"sh", "-c", "/tools/opencode run --format json \"$(cat /workspace/task.md)\""}
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            customCmd,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod uses custom command")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			Expect(createdPod.Spec.Containers[0].Command).Should(Equal(customCmd))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should use default command when not specified", func() {
			agentName := "test-agent-default-cmd"
			taskName := "test-task-default-cmd"
			description := "Test default command"

			By("Creating Agent without command (should use default)")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					// Command is not specified
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod uses default command")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			// Default command should contain opencode run
			Expect(len(createdPod.Spec.Containers[0].Command)).Should(BeNumerically(">", 0))
			// Verify it's the default command pattern (sh -c "...")
			Expect(createdPod.Spec.Containers[0].Command[0]).Should(Equal("sh"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Pod resource requirements", func() {
		It("Should apply resource requests and limits to agent container", func() {
			agentName := "test-agent-resources"
			taskName := "test-task-resources"
			description := "Test resource requirements"

			By("Creating Agent with resource requirements")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					PodSpec: &kubeopenv1alpha1.AgentPodSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
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
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod has resource requirements")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			container := createdPod.Spec.Containers[0]
			Expect(container.Resources.Requests.Cpu().String()).Should(Equal("500m"))
			Expect(container.Resources.Requests.Memory().String()).Should(Equal("512Mi"))
			Expect(container.Resources.Limits.Cpu().String()).Should(Equal("2"))
			Expect(container.Resources.Limits.Memory().String()).Should(Equal("2Gi"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Credentials - Entire Secret as environment variables", func() {
		It("Should expose all Secret keys as environment variables when no key is specified", func() {
			taskName := "test-task-entire-secret-env"
			agentName := "test-agent-entire-secret-env"
			secretName := "test-entire-secret-env"
			description := "Test entire secret as env vars"

			By("Creating Secret with multiple keys")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: taskNamespace,
				},
				Data: map[string][]byte{
					"API_KEY":    []byte("api-key-value"),
					"API_SECRET": []byte("api-secret-value"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating Agent with credential referencing entire Secret (no key)")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Credentials: []kubeopenv1alpha1.Credential{
						{
							Name: "api-credentials",
							SecretRef: kubeopenv1alpha1.SecretReference{
								Name: secretName,
								// Key is not specified - entire Secret becomes env vars
							},
							// No MountPath, no Env - all keys become env vars
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod has all Secret keys as environment variables")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			// Check for envFrom with secretRef
			container := createdPod.Spec.Containers[0]
			var foundSecretEnvFrom bool
			for _, envFrom := range container.EnvFrom {
				if envFrom.SecretRef != nil && envFrom.SecretRef.Name == secretName {
					foundSecretEnvFrom = true
					break
				}
			}
			Expect(foundSecretEnvFrom).Should(BeTrue(), "Expected envFrom with secretRef for entire secret")

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})

		It("Should mount entire Secret as directory when mountPath is specified", func() {
			taskName := "test-task-entire-secret-dir"
			agentName := "test-agent-entire-secret-dir"
			secretName := "test-entire-secret-dir"
			mountPath := "/etc/credentials"
			description := "Test entire secret as directory"

			By("Creating Secret with multiple keys")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: taskNamespace,
				},
				Data: map[string][]byte{
					"ca.crt":     []byte("ca-cert-data"),
					"client.crt": []byte("client-cert-data"),
					"client.key": []byte("client-key-data"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating Agent with credential referencing entire Secret with mountPath")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Credentials: []kubeopenv1alpha1.Credential{
						{
							Name: "tls-certs",
							SecretRef: kubeopenv1alpha1.SecretReference{
								Name: secretName,
								// Key is not specified - entire Secret
							},
							MountPath: &mountPath, // Mount as directory
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod has Secret mounted as directory")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			// Check for volume mount at mountPath
			container := createdPod.Spec.Containers[0]
			var foundMount bool
			for _, vm := range container.VolumeMounts {
				if vm.MountPath == mountPath {
					foundMount = true
					break
				}
			}
			Expect(foundMount).Should(BeTrue(), "Expected volume mount at %s", mountPath)

			// Check for corresponding secret volume
			var foundSecretVolume bool
			for _, vol := range createdPod.Spec.Volumes {
				if vol.Secret != nil && vol.Secret.SecretName == secretName {
					foundSecretVolume = true
					break
				}
			}
			Expect(foundSecretVolume).Should(BeTrue(), "Expected secret volume for %s", secretName)

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("ConfigMap Context directory mount", func() {
		It("Should mount entire ConfigMap as directory when key is not specified and mountPath is set", func() {
			taskName := "test-task-configmap-dir"
			configMapName := "test-configmap-dir"
			mountPath := "/workspace/config"
			description := "Test ConfigMap directory mount"

			By("Creating ConfigMap with multiple keys")
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: taskNamespace,
				},
				Data: map[string]string{
					"config.yaml":   "key: value\nenabled: true",
					"settings.json": `{"setting": "value"}`,
					"readme.txt":    "This is a readme file",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

			By("Creating Task with ConfigMap context (no key, with mountPath)")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeConfigMap,
							ConfigMap: &kubeopenv1alpha1.ConfigMapContext{
								Name: configMapName,
								// Key is not specified - mount entire ConfigMap
							},
							MountPath: mountPath, // Mount as directory
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Pod has ConfigMap volume for directory mount")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			// Check for ConfigMap volume (used by context-init to copy to workspace)
			var foundConfigMapVolume bool
			for _, vol := range createdPod.Spec.Volumes {
				if vol.ConfigMap != nil && vol.ConfigMap.Name == configMapName {
					foundConfigMapVolume = true
					break
				}
			}
			Expect(foundConfigMapVolume).Should(BeTrue(), "Expected ConfigMap volume for %s", configMapName)

			// Check for context-init container that copies directory contents
			var foundContextInit bool
			for _, initC := range createdPod.Spec.InitContainers {
				if initC.Name == "context-init" {
					foundContextInit = true
					// Verify it has DIR_MAPPINGS environment variable
					for _, env := range initC.Env {
						if env.Name == "DIR_MAPPINGS" {
							Expect(env.Value).Should(ContainSubstring(mountPath))
							break
						}
					}
					break
				}
			}
			Expect(foundContextInit).Should(BeTrue(), "Expected context-init container for directory mount")

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, configMap)).Should(Succeed())
		})
	})

	Context("Missing ConfigMap context", func() {
		It("Should fail when ConfigMap is not found", func() {
			taskName := "test-task-missing-configmap"
			description := "Test missing ConfigMap"

			By("Creating Task with ConfigMap context that doesn't exist")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Type: kubeopenv1alpha1.ContextTypeConfigMap,
							ConfigMap: &kubeopenv1alpha1.ConfigMapContext{
								Name: "non-existent-configmap",
								Key:  "key1",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Failed")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseFailed))

			By("Verifying error condition is set")
			var readyCondition *metav1.Condition
			for i := range createdTask.Status.Conditions {
				if createdTask.Status.Conditions[i].Type == kubeopenv1alpha1.ConditionTypeReady {
					readyCondition = &createdTask.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Message).Should(ContainSubstring("not found"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	// NOTE: URL Context type is defined in the API (ContextTypeURL) but not yet implemented
	// in the controller. This test is marked as Pending until the feature is implemented.
	Context("URL Context", func() {
		PIt("Should create url-fetch init container for URL context (TODO: implement URL context type)", func() {
			// This test verifies URL context type creates a url-fetch init container.
			// Currently returns "unknown context type: URL" error.
		})
	})

	Context("Git Context configuration", func() {
		It("Should create git-init container with correct arguments", func() {
			taskName := "test-task-git-context"
			description := "Test Git context"
			mountPath := "/workspace/source"

			By("Creating Task with Git context")
			depth := 1
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubeopenv1alpha1.ContextItem{
						{
							Name: "source-code",
							Type: kubeopenv1alpha1.ContextTypeGit,
							Git: &kubeopenv1alpha1.GitContext{
								Repository: "https://github.com/example/repo.git",
								Ref:        "main",
								Depth:      &depth,
							},
							MountPath: mountPath,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Checking Pod has git-init-0 container")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			// Git init containers are named git-init-0, git-init-1, etc.
			var foundGitInit bool
			for _, initC := range createdPod.Spec.InitContainers {
				if initC.Name == "git-init-0" {
					foundGitInit = true
					// Verify git-init has correct environment variables
					var foundRepoUrl, foundRef bool
					for _, env := range initC.Env {
						if env.Name == "GIT_REPO" && env.Value == "https://github.com/example/repo.git" {
							foundRepoUrl = true
						}
						if env.Name == "GIT_REF" && env.Value == "main" {
							foundRef = true
						}
					}
					Expect(foundRepoUrl).Should(BeTrue(), "Expected GIT_REPO env var")
					Expect(foundRef).Should(BeTrue(), "Expected GIT_REF env var")
					break
				}
			}
			Expect(foundGitInit).Should(BeTrue(), "Expected git-init-0 container")

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("Server-mode Task execution", func() {
		It("Should create Pod with --attach flag pointing to server URL", func() {
			agentName := "test-server-agent-task"
			taskName := "test-task-server-mode"
			description := "Test server mode task"

			By("Creating Server-mode Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      "quay.io/kubeopencode/kubeopencode-agent-devbox:latest",
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "test-agent",
					ServerConfig: &kubeopenv1alpha1.ServerConfig{
						Port: 4096,
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Waiting for Agent Deployment to be created")
			deploymentName := ServerDeploymentName(agentName)
			Eventually(func() error {
				var deployment appsv1.Deployment
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: taskNamespace,
				}, &deployment)
			}, timeout, interval).Should(Succeed())

			By("Creating Task with Server-mode Agent")
			task := &kubeopenv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubeopenv1alpha1.TaskSpec{
					AgentRef:    &kubeopenv1alpha1.AgentReference{Name: agentName},
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubeopenv1alpha1.Task{}
			Eventually(func() kubeopenv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubeopenv1alpha1.TaskPhaseRunning))

			By("Checking Pod command contains --attach flag")
			podName := fmt.Sprintf("%s-pod", taskName)
			podLookupKey := types.NamespacedName{Name: podName, Namespace: taskNamespace}
			createdPod := &corev1.Pod{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, podLookupKey, createdPod) == nil
			}, timeout, interval).Should(BeTrue())

			// Server mode should use --attach flag in command
			container := createdPod.Spec.Containers[0]
			commandStr := fmt.Sprintf("%v", container.Command)
			Expect(commandStr).Should(ContainSubstring("--attach"))

			// Should contain server URL
			expectedURL := ServerURL(agentName, taskNamespace, 4096)
			Expect(commandStr).Should(ContainSubstring(expectedURL))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})
})
