// Copyright Contributors to the KubeTask project

//go:build integration

// See suite_test.go for explanation of the "integration" build tag pattern.

package controller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubetaskv1alpha1 "github.com/xuezhaojun/kubetask/api/v1alpha1"
)

var _ = Describe("TaskController", func() {
	const (
		taskNamespace = "default"
	)

	Context("When creating a Task with inline context", func() {
		It("Should create a Job and update Task status", func() {
			taskName := "test-task-inline"
			inlineContent := "# Test Task\n\nThis is a test task."

			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
					},
				},
			}

			By("Creating the Task")
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is updated to Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Checking Job is created")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, jobLookupKey, createdJob) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Job has correct labels")
			Expect(createdJob.Labels).Should(HaveKeyWithValue("app", "kubetask"))
			Expect(createdJob.Labels).Should(HaveKeyWithValue("kubetask.io/task", taskName))

			By("Verifying Job has owner reference to Task")
			Expect(createdJob.OwnerReferences).Should(HaveLen(1))
			Expect(createdJob.OwnerReferences[0].Name).Should(Equal(taskName))

			By("Verifying Job uses default agent image")
			Expect(createdJob.Spec.Template.Spec.Containers).Should(HaveLen(1))
			Expect(createdJob.Spec.Template.Spec.Containers[0].Image).Should(Equal(DefaultAgentImage))

			By("Verifying Task status has JobName set")
			Expect(createdTask.Status.JobName).Should(Equal(jobName))
			Expect(createdTask.Status.StartTime).ShouldNot(BeNil())

			By("Checking context ConfigMap is created")
			configMapName := taskName + ContextConfigMapSuffix
			configMapLookupKey := types.NamespacedName{Name: configMapName, Namespace: taskNamespace}
			createdConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, configMapLookupKey, createdConfigMap) == nil
			}, timeout, interval).Should(BeTrue())
			// Key is sanitized from FilePath: /workspace/task.md -> workspace-task.md
			Expect(createdConfigMap.Data).Should(HaveKey("workspace-task.md"))
			Expect(createdConfigMap.Data["workspace-task.md"]).Should(ContainSubstring(inlineContent))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When creating a Task with WorkspaceConfig reference", func() {
		It("Should use agent image from WorkspaceConfig", func() {
			taskName := "test-task-wsconfig"
			wsConfigName := "test-workspace-config"
			customAgentImage := "custom-agent:v1.0.0"
			inlineContent := "# Test with WorkspaceConfig"

			By("Creating WorkspaceConfig")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					AgentImage:         customAgentImage,
					ServiceAccountName: "test-agent",
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task with WorkspaceConfig reference")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job uses custom agent image")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() string {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return ""
				}
				if len(createdJob.Spec.Template.Spec.Containers) == 0 {
					return ""
				}
				return createdJob.Spec.Template.Spec.Containers[0].Image
			}, timeout, interval).Should(Equal(customAgentImage))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("When creating a Task with WorkspaceConfig that has toolsImage", func() {
		It("Should create Job with init container for tools", func() {
			taskName := "test-task-tools"
			wsConfigName := "test-workspace-tools"
			toolsImage := "tools-image:v1.0.0"
			inlineContent := "# Test with tools"

			By("Creating WorkspaceConfig with toolsImage")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					ToolsImage:         toolsImage,
					ServiceAccountName: "test-agent",
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job has init container for tools")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() int {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return 0
				}
				return len(createdJob.Spec.Template.Spec.InitContainers)
			}, timeout, interval).Should(Equal(1))

			Expect(createdJob.Spec.Template.Spec.InitContainers[0].Name).Should(Equal("copy-tools"))
			Expect(createdJob.Spec.Template.Spec.InitContainers[0].Image).Should(Equal(toolsImage))

			By("Verifying PATH environment variable is set")
			var pathEnv *corev1.EnvVar
			for _, env := range createdJob.Spec.Template.Spec.Containers[0].Env {
				if env.Name == "PATH" {
					pathEnv = &env
					break
				}
			}
			Expect(pathEnv).ShouldNot(BeNil())
			Expect(pathEnv.Value).Should(ContainSubstring("/tools/bin"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("When creating a Task with WorkspaceConfig that has credentials", func() {
		It("Should mount credentials as env vars and files", func() {
			taskName := "test-task-creds"
			wsConfigName := "test-workspace-creds"
			secretName := "test-secret"
			envName := "API_TOKEN"
			mountPath := "/home/agent/.ssh/id_rsa"
			inlineContent := "# Test with credentials"

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

			By("Creating WorkspaceConfig with credentials")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					ServiceAccountName: "test-agent",
					Credentials: []kubetaskv1alpha1.Credential{
						{
							Name: "api-token",
							SecretRef: kubetaskv1alpha1.SecretReference{
								Name: secretName,
								Key:  "token",
							},
							Env: &envName,
						},
						{
							Name: "ssh-key",
							SecretRef: kubetaskv1alpha1.SecretReference{
								Name: secretName,
								Key:  "key",
							},
							MountPath: &mountPath,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job has credential env var")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return false
				}
				return len(createdJob.Spec.Template.Spec.Containers) > 0
			}, timeout, interval).Should(BeTrue())

			var tokenEnv *corev1.EnvVar
			for _, env := range createdJob.Spec.Template.Spec.Containers[0].Env {
				if env.Name == envName {
					tokenEnv = &env
					break
				}
			}
			Expect(tokenEnv).ShouldNot(BeNil())
			Expect(tokenEnv.ValueFrom).ShouldNot(BeNil())
			Expect(tokenEnv.ValueFrom.SecretKeyRef.Name).Should(Equal(secretName))
			Expect(tokenEnv.ValueFrom.SecretKeyRef.Key).Should(Equal("token"))

			By("Checking Job has credential volume mount")
			var sshMount *corev1.VolumeMount
			for _, mount := range createdJob.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.MountPath == mountPath {
					sshMount = &mount
					break
				}
			}
			Expect(sshMount).ShouldNot(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("When creating a Task with WorkspaceConfig that has podLabels", func() {
		It("Should apply podLabels to the Job's pod template", func() {
			taskName := "test-task-labels"
			wsConfigName := "test-workspace-labels"
			inlineContent := "# Test with podLabels"

			By("Creating WorkspaceConfig with podLabels")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					ServiceAccountName: "test-agent",
					PodLabels: map[string]string{
						"network-policy": "agent-restricted",
						"team":           "platform",
					},
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job pod template has custom labels")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return false
				}
				return createdJob.Spec.Template.Labels != nil
			}, timeout, interval).Should(BeTrue())

			Expect(createdJob.Spec.Template.Labels).Should(HaveKeyWithValue("network-policy", "agent-restricted"))
			Expect(createdJob.Spec.Template.Labels).Should(HaveKeyWithValue("team", "platform"))
			// Also verify base labels are still present
			Expect(createdJob.Spec.Template.Labels).Should(HaveKeyWithValue("app", "kubetask"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("When creating a Task with WorkspaceConfig that has scheduling", func() {
		It("Should apply scheduling configuration to the Job", func() {
			taskName := "test-task-scheduling"
			wsConfigName := "test-workspace-scheduling"
			inlineContent := "# Test with scheduling"

			By("Creating WorkspaceConfig with scheduling")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					ServiceAccountName: "test-agent",
					Scheduling: &kubetaskv1alpha1.PodScheduling{
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
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job has node selector")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() map[string]string {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return nil
				}
				return createdJob.Spec.Template.Spec.NodeSelector
			}, timeout, interval).ShouldNot(BeNil())

			Expect(createdJob.Spec.Template.Spec.NodeSelector).Should(HaveKeyWithValue("kubernetes.io/os", "linux"))
			Expect(createdJob.Spec.Template.Spec.NodeSelector).Should(HaveKeyWithValue("node-type", "gpu"))

			By("Checking Job has tolerations")
			Expect(createdJob.Spec.Template.Spec.Tolerations).Should(HaveLen(1))
			Expect(createdJob.Spec.Template.Spec.Tolerations[0].Key).Should(Equal("dedicated"))
			Expect(createdJob.Spec.Template.Spec.Tolerations[0].Value).Should(Equal("ai-workload"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("When creating a Task with multiple file paths", func() {
		It("Should mount files at specified paths", func() {
			taskName := "test-task-multi-path"
			configPath := "/workspace/config/settings.json"
			configContent := `{"debug": true}`
			inlineContent := "# Main task content"

			By("Creating Task with multiple file paths")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: configPath,
								Source: kubetaskv1alpha1.FileSource{
									Inline: &configContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job has both file mounts")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return false
				}
				return len(createdJob.Spec.Template.Spec.Containers) > 0
			}, timeout, interval).Should(BeTrue())

			var configMount *corev1.VolumeMount
			for _, mount := range createdJob.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.MountPath == configPath {
					configMount = &mount
					break
				}
			}
			Expect(configMount).ShouldNot(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When creating a Task with ConfigMap reference", func() {
		It("Should resolve content from ConfigMap", func() {
			taskName := "test-task-configmap"
			configMapName := "test-task-content"
			configMapKey := "task-content"
			configMapContent := "# Task from ConfigMap\n\nThis content comes from a ConfigMap."

			By("Creating ConfigMap")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: taskNamespace,
				},
				Data: map[string]string{
					configMapKey: configMapContent,
				},
			}
			Expect(k8sClient.Create(ctx, cm)).Should(Succeed())

			By("Creating Task with ConfigMap reference")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									ConfigMapKeyRef: &kubetaskv1alpha1.ConfigMapKeySelector{
										Name: configMapName,
										Key:  configMapKey,
									},
								},
							},
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
			// Key is sanitized from FilePath: /workspace/task.md -> workspace-task.md
			Expect(createdContextConfigMap.Data["workspace-task.md"]).Should(ContainSubstring(configMapContent))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, cm)).Should(Succeed())
		})
	})

	Context("When creating a Task with WorkspaceConfig that has defaultContexts", func() {
		It("Should merge defaultContexts with task contexts", func() {
			taskName := "test-task-default-contexts"
			wsConfigName := "test-workspace-default-ctx"
			defaultContent := "# Default Guidelines\n\nThese are default guidelines."
			taskContent := "# Specific Task\n\nThis is the specific task content."

			By("Creating WorkspaceConfig with defaultContexts")
			wsConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wsConfigName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					ServiceAccountName: "test-agent",
					DefaultContexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/guidelines.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &defaultContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: wsConfigName,
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &taskContent,
								},
							},
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

			// Each context has its own key based on FilePath
			// /workspace/guidelines.md -> workspace-guidelines.md
			// /workspace/task.md -> workspace-task.md
			Expect(createdContextConfigMap.Data["workspace-guidelines.md"]).Should(ContainSubstring(defaultContent))
			Expect(createdContextConfigMap.Data["workspace-task.md"]).Should(ContainSubstring(taskContent))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, wsConfig)).Should(Succeed())
		})
	})

	Context("When a Task's Job completes successfully", func() {
		It("Should update Task status to Succeeded", func() {
			taskName := "test-task-success"
			inlineContent := "# Success test"

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Job to be created")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, jobLookupKey, createdJob) == nil
			}, timeout, interval).Should(BeTrue())

			By("Simulating Job success")
			createdJob.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, createdJob)).Should(Succeed())

			By("Checking Task status is Succeeded")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

			By("Checking CompletionTime is set")
			finalTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, finalTask)).Should(Succeed())
			Expect(finalTask.Status.CompletionTime).ShouldNot(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When a Task's Job fails", func() {
		It("Should update Task status to Failed", func() {
			taskName := "test-task-failure"
			inlineContent := "# Failure test"

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Contexts: []kubetaskv1alpha1.Context{
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/task.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &inlineContent,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Job to be created")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, jobLookupKey, createdJob) == nil
			}, timeout, interval).Should(BeTrue())

			By("Simulating Job failure")
			createdJob.Status.Failed = 1
			Expect(k8sClient.Status().Update(ctx, createdJob)).Should(Succeed())

			By("Checking Task status is Failed")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseFailed))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})
})
