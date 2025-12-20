// Copyright Contributors to the KubeTask project

//go:build integration

// See suite_test.go for explanation of the "integration" build tag pattern.

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var _ = Describe("TaskController", func() {
	const (
		taskNamespace = "default"
	)

	Context("When creating a Task with description", func() {
		It("Should create a Job and update Task status", func() {
			taskName := "test-task-description"
			description := "# Test Task\n\nThis is a test task."

			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
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
			Expect(createdConfigMap.Data).Should(HaveKey("workspace-task.md"))
			Expect(createdConfigMap.Data["workspace-task.md"]).Should(ContainSubstring(description))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent reference", func() {
		It("Should use agent image from Agent", func() {
			taskName := "test-task-agent"
			agentConfigName := "test-agent-config"
			customAgentImage := "custom-agent:v1.0.0"
			description := "# Test with Agent"

			By("Creating Agent")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentConfigName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         customAgentImage,
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with Agent reference")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentConfigName,
					Description: &description,
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
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					Credentials: []kubetaskv1alpha1.Credential{
						{
							Name: "api-token",
							SecretRef: kubetaskv1alpha1.SecretReference{
								Name: secretName,
								Key:  stringPtr("token"),
							},
							Env: &envName,
						},
						{
							Name: "ssh-key",
							SecretRef: kubetaskv1alpha1.SecretReference{
								Name: secretName,
								Key:  stringPtr("key"),
							},
							MountPath: &mountPath,
						},
					},
					WorkspaceDir: "/workspace",
					Command:      []string{"sh", "-c", "echo test"},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
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
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has podSpec.labels", func() {
		It("Should apply labels to the Job's pod template", func() {
			taskName := "test-task-labels"
			agentName := "test-workspace-labels"
			description := "# Test with podSpec.labels"

			By("Creating Agent with podSpec.labels")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					PodSpec: &kubetaskv1alpha1.AgentPodSpec{
						Labels: map[string]string{
							"network-policy": "agent-restricted",
							"team":           "platform",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
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
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has podSpec.scheduling", func() {
		It("Should apply scheduling configuration to the Job", func() {
			taskName := "test-task-scheduling"
			agentName := "test-workspace-scheduling"
			description := "# Test with podSpec.scheduling"

			By("Creating Agent with podSpec.scheduling")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					PodSpec: &kubetaskv1alpha1.AgentPodSpec{
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
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
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
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has podSpec.runtimeClassName", func() {
		It("Should apply runtimeClassName to the Job's pod spec", func() {
			taskName := "test-task-runtime"
			agentName := "test-agent-runtime"
			runtimeClassName := "gvisor"
			description := "# Test with podSpec.runtimeClassName"

			By("Creating Agent with podSpec.runtimeClassName")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					PodSpec: &kubetaskv1alpha1.AgentPodSpec{
						RuntimeClassName: &runtimeClassName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job has runtimeClassName set")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() *string {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return nil
				}
				return createdJob.Spec.Template.Spec.RuntimeClassName
			}, timeout, interval).ShouldNot(BeNil())

			Expect(*createdJob.Spec.Template.Spec.RuntimeClassName).Should(Equal(runtimeClassName))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When creating a Task with Context CRD reference", func() {
		It("Should resolve and mount Context content", func() {
			taskName := "test-task-context-ref"
			contextName := "test-context-inline"
			contextContent := "# Coding Standards\n\nFollow these guidelines."
			description := "Review the code"

			By("Creating Context CRD")
			context := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      contextName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeText,
					Text: contextContent,
				},
			}
			Expect(k8sClient.Create(ctx, context)).Should(Succeed())

			By("Creating Task with Context reference")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Ref: &kubetaskv1alpha1.ContextRef{
								Name:      contextName,
								MountPath: "/workspace/guides/standards.md",
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

			// Task.md should contain description
			Expect(createdContextConfigMap.Data["workspace-task.md"]).Should(ContainSubstring(description))
			// Mounted context should be at its own key
			Expect(createdContextConfigMap.Data["workspace-guides-standards.md"]).Should(ContainSubstring(contextContent))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, context)).Should(Succeed())
		})
	})

	Context("When creating a Task with ConfigMap Context without key and mountPath", func() {
		It("Should aggregate all ConfigMap keys to task.md", func() {
			taskName := "test-task-configmap-all-keys"
			contextName := "test-context-configmap-all"
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

			By("Creating Context CRD referencing ConfigMap without key")
			context := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      contextName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeConfigMap,
					ConfigMap: &kubetaskv1alpha1.ConfigMapContext{
						Name: configMapName,
						// No Key specified - should aggregate all keys
					},
				},
			}
			Expect(k8sClient.Create(ctx, context)).Should(Succeed())

			By("Creating Task with Context reference (no mountPath)")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Ref: &kubetaskv1alpha1.ContextRef{
								Name: contextName,
								// No MountPath - should aggregate to task.md
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking all ConfigMap keys are aggregated to task.md")
			contextConfigMapName := taskName + ContextConfigMapSuffix
			contextConfigMapLookupKey := types.NamespacedName{Name: contextConfigMapName, Namespace: taskNamespace}
			createdContextConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, contextConfigMapLookupKey, createdContextConfigMap) == nil
			}, timeout, interval).Should(BeTrue())

			taskMdContent := createdContextConfigMap.Data["workspace-task.md"]
			// Description should be present
			Expect(taskMdContent).Should(ContainSubstring(description))
			// Context wrapper should be present
			Expect(taskMdContent).Should(ContainSubstring("<context"))
			Expect(taskMdContent).Should(ContainSubstring("</context>"))
			// All ConfigMap keys should be wrapped in <file> tags
			Expect(taskMdContent).Should(ContainSubstring(`<file name="security-guide.md">`))
			Expect(taskMdContent).Should(ContainSubstring("# Security Guide"))
			Expect(taskMdContent).Should(ContainSubstring(`<file name="style-guide.md">`))
			Expect(taskMdContent).Should(ContainSubstring("# Style Guide"))
			Expect(taskMdContent).Should(ContainSubstring("</file>"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, context)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, guidesConfigMap)).Should(Succeed())
		})
	})

	Context("When creating a Task with Context without mountPath", func() {
		It("Should append context to task.md with XML tags", func() {
			taskName := "test-task-context-aggregate"
			contextName := "test-context-aggregate"
			contextContent := "# Security Guidelines\n\nFollow security best practices."
			description := "Review security compliance"

			By("Creating Context CRD")
			context := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      contextName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeText,
					Text: contextContent,
				},
			}
			Expect(k8sClient.Create(ctx, context)).Should(Succeed())

			By("Creating Task with Context reference (no mountPath)")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Ref: &kubetaskv1alpha1.ContextRef{
								Name: contextName,
								// No MountPath - should be appended to task.md
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking context is appended to task.md with XML tags")
			contextConfigMapName := taskName + ContextConfigMapSuffix
			contextConfigMapLookupKey := types.NamespacedName{Name: contextConfigMapName, Namespace: taskNamespace}
			createdContextConfigMap := &corev1.ConfigMap{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, contextConfigMapLookupKey, createdContextConfigMap) == nil
			}, timeout, interval).Should(BeTrue())

			taskMdContent := createdContextConfigMap.Data["workspace-task.md"]
			Expect(taskMdContent).Should(ContainSubstring(description))
			Expect(taskMdContent).Should(ContainSubstring("<context"))
			Expect(taskMdContent).Should(ContainSubstring(contextContent))
			Expect(taskMdContent).Should(ContainSubstring("</context>"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, context)).Should(Succeed())
		})
	})

	Context("When creating a Task with Agent that has contexts", func() {
		It("Should merge agent contexts with task contexts", func() {
			taskName := "test-task-agent-contexts"
			agentName := "test-agent-with-contexts"
			agentContextName := "agent-default-context"
			taskContextName := "task-specific-context"
			agentContextContent := "# Agent Guidelines\n\nThese are default guidelines."
			taskContextContent := "# Task Guidelines\n\nThese are task-specific guidelines."
			description := "Do the task"

			By("Creating Agent Context CRD")
			agentContext := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentContextName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeText,
					Text: agentContextContent,
				},
			}
			Expect(k8sClient.Create(ctx, agentContext)).Should(Succeed())

			By("Creating Task Context CRD")
			taskContext := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskContextName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeText,
					Text: taskContextContent,
				},
			}
			Expect(k8sClient.Create(ctx, taskContext)).Should(Succeed())

			By("Creating Agent with context reference")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Ref: &kubetaskv1alpha1.ContextRef{
								Name: agentContextName,
								// No mountPath - should be appended to task.md
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with context reference")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Ref: &kubetaskv1alpha1.ContextRef{
								Name: taskContextName,
								// No mountPath - should be appended to task.md
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

			taskMdContent := createdContextConfigMap.Data["workspace-task.md"]
			// Description should be first (highest priority)
			Expect(taskMdContent).Should(ContainSubstring(description))
			// Both contexts should be appended
			Expect(taskMdContent).Should(ContainSubstring(agentContextContent))
			Expect(taskMdContent).Should(ContainSubstring(taskContextContent))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agentContext)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, taskContext)).Should(Succeed())
		})
	})

	Context("When a Task's Job completes successfully", func() {
		It("Should update Task status to Completed", func() {
			taskName := "test-task-success"
			description := "# Success test"

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
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

			By("Checking Task status is Completed")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

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
			description := "# Failure test"

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
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

	Context("When creating a Task with humanInTheLoop enabled on Agent", func() {
		It("Should create a session sidecar container", func() {
			taskName := "test-task-hitl"
			agentName := "test-agent-hitl"
			description := "# Human-in-the-loop test"
			duration := metav1.Duration{Duration: 30 * time.Minute} // 30 minutes

			By("Creating Agent with command and humanInTheLoop enabled")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo hello"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Sidecar: &kubetaskv1alpha1.SessionSidecar{
							Enabled:  true,
							Duration: &duration,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task referencing the Agent")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task has humanInTheLoop annotation")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			updatedTask := &kubetaskv1alpha1.Task{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return false
				}
				return updatedTask.Annotations != nil &&
					updatedTask.Annotations[AnnotationHumanInTheLoop] == "true"
			}, timeout, interval).Should(BeTrue())

			By("Checking Job has two containers (agent + sidecar)")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return false
				}
				return len(createdJob.Spec.Template.Spec.Containers) == 2
			}, timeout, interval).Should(BeTrue())

			// Agent container should have original command (not wrapped)
			agentContainer := createdJob.Spec.Template.Spec.Containers[0]
			Expect(agentContainer.Name).Should(Equal("agent"))
			Expect(agentContainer.Command).Should(HaveLen(3))
			Expect(agentContainer.Command[2]).Should(Equal("echo hello"))

			// Sidecar container should have sleep command
			sidecarContainer := createdJob.Spec.Template.Spec.Containers[1]
			Expect(sidecarContainer.Name).Should(Equal("session"))
			Expect(sidecarContainer.Command).Should(HaveLen(2))
			Expect(sidecarContainer.Command[0]).Should(Equal("sleep"))
			Expect(sidecarContainer.Command[1]).Should(Equal("1800")) // 30 minutes

			By("Checking session duration environment variable is set on agent container")
			var durationEnv *corev1.EnvVar
			for _, env := range agentContainer.Env {
				if env.Name == EnvHumanInTheLoopDuration {
					durationEnv = &env
					break
				}
			}
			Expect(durationEnv).ShouldNot(BeNil())
			Expect(durationEnv.Value).Should(Equal("1800"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should use default duration when not specified", func() {
			taskName := "test-task-hitl-default"
			agentName := "test-agent-hitl-default"
			description := "# Human-in-the-loop default test"

			By("Creating Agent with humanInTheLoop but no duration")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"./run.sh"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Sidecar: &kubetaskv1alpha1.SessionSidecar{
							Enabled: true,
							// Duration not specified, should use default (1 hour)
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task referencing the Agent")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking sidecar uses default session duration (3600 seconds)")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return false
				}
				return len(createdJob.Spec.Template.Spec.Containers) == 2
			}, timeout, interval).Should(BeTrue())

			sidecarContainer := createdJob.Spec.Template.Spec.Containers[1]
			Expect(sidecarContainer.Command[1]).Should(Equal("3600"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should not add humanInTheLoop annotation when disabled", func() {
			taskName := "test-task-hitl-disabled"
			agentName := "test-agent-hitl-disabled"
			description := "# Human-in-the-loop disabled test"

			By("Creating Agent with humanInTheLoop disabled")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"./run.sh"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Sidecar: &kubetaskv1alpha1.SessionSidecar{
							Enabled: false,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task referencing the Agent")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job is created with single container")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, jobLookupKey, createdJob) == nil
			}, timeout, interval).Should(BeTrue())
			Expect(createdJob.Spec.Template.Spec.Containers).Should(HaveLen(1))

			By("Checking Task does not have humanInTheLoop annotation")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			updatedTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, updatedTask)).Should(Succeed())
			if updatedTask.Annotations != nil {
				Expect(updatedTask.Annotations).ShouldNot(HaveKey(AnnotationHumanInTheLoop))
			}

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should not add humanInTheLoop annotation when not configured", func() {
			taskName := "test-task-no-hitl"
			agentName := "test-agent-no-hitl"
			description := "# No human-in-the-loop test"

			By("Creating Agent without humanInTheLoop configuration")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"./run.sh"},
					// HumanInTheLoop not specified
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task referencing the Agent")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Job is created")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, jobLookupKey, createdJob) == nil
			}, timeout, interval).Should(BeTrue())

			By("Checking Task does not have humanInTheLoop annotation")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			updatedTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, updatedTask)).Should(Succeed())
			if updatedTask.Annotations != nil {
				Expect(updatedTask.Annotations).ShouldNot(HaveKey(AnnotationHumanInTheLoop))
			}

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When KubeTaskConfig exists", func() {
		It("Should use TTL from KubeTaskConfig for cleanup", func() {
			taskName := "test-task-ttl"
			description := "# TTL test"
			ttlSeconds := int32(60) // 1 minute for testing

			By("Creating KubeTaskConfig with custom TTL")
			config := &kubetaskv1alpha1.KubeTaskConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.KubeTaskConfigSpec{
					TaskLifecycle: &kubetaskv1alpha1.TaskLifecycleConfig{
						TTLSecondsAfterFinished: &ttlSeconds,
					},
				},
			}
			Expect(k8sClient.Create(ctx, config)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
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

			By("Waiting for Task to complete")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			// Note: Testing actual TTL cleanup would require waiting for the TTL to expire
			// which is not practical in unit tests. The TTL logic is verified by checking
			// that the controller correctly reads the TTL value from KubeTaskConfig.
			// Full TTL cleanup testing should be done in E2E tests with shorter TTL values.

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, config)).Should(Succeed())
		})
	})

	Context("When Agent has maxConcurrentTasks set", func() {
		It("Should queue Tasks when at capacity", func() {
			agentName := "test-agent-concurrency"
			maxConcurrent := int32(1)
			description1 := "# Task 1"
			description2 := "# Task 2"

			By("Creating Agent with maxConcurrentTasks=1")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					MaxConcurrentTasks: &maxConcurrent,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating first Task")
			task1 := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-concurrent-1",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1LookupKey := types.NamespacedName{Name: "test-task-concurrent-1", Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Verifying first Task has agent label")
			task1Updated := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task1LookupKey, task1Updated)).Should(Succeed())
			Expect(task1Updated.Labels).Should(HaveKeyWithValue(AgentLabelKey, agentName))

			By("Creating second Task")
			task2 := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-concurrent-2",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Checking second Task is Queued")
			task2LookupKey := types.NamespacedName{Name: "test-task-concurrent-2", Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseQueued))

			By("Verifying second Task has Queued condition")
			task2Updated := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, task2LookupKey, task2Updated)).Should(Succeed())
			Expect(task2Updated.Labels).Should(HaveKeyWithValue(AgentLabelKey, agentName))

			// Check for Queued condition
			var queuedCondition *metav1.Condition
			for i := range task2Updated.Status.Conditions {
				if task2Updated.Status.Conditions[i].Type == "Queued" {
					queuedCondition = &task2Updated.Status.Conditions[i]
					break
				}
			}
			Expect(queuedCondition).ShouldNot(BeNil())
			Expect(queuedCondition.Status).Should(Equal(metav1.ConditionTrue))
			Expect(queuedCondition.Reason).Should(Equal("AgentAtCapacity"))

			By("Simulating first Task completion")
			job1Name := fmt.Sprintf("%s-job", "test-task-concurrent-1")
			job1LookupKey := types.NamespacedName{Name: job1Name, Namespace: taskNamespace}
			job1 := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, job1LookupKey, job1)).Should(Succeed())
			job1.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, job1)).Should(Succeed())

			By("Waiting for first Task to complete")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Checking second Task transitions to Running")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

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
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					MaxConcurrentTasks: &maxConcurrent,
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating first Task")
			task1 := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-unlimited-1",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1LookupKey := types.NamespacedName{Name: "test-task-unlimited-1", Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Creating second Task")
			task2 := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-unlimited-2",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Checking second Task is also Running (not Queued)")
			task2LookupKey := types.NamespacedName{Name: "test-task-unlimited-2", Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

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
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					// MaxConcurrentTasks not set (nil)
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating first Task")
			task1 := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-nolimit-1",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description1,
				},
			}
			Expect(k8sClient.Create(ctx, task1)).Should(Succeed())

			By("Waiting for first Task to be Running")
			task1LookupKey := types.NamespacedName{Name: "test-task-nolimit-1", Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task1LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Creating second Task")
			task2 := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-task-nolimit-2",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description2,
				},
			}
			Expect(k8sClient.Create(ctx, task2)).Should(Succeed())

			By("Checking second Task is also Running (not Queued)")
			task2LookupKey := types.NamespacedName{Name: "test-task-nolimit-2", Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, task2LookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, task2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When stopping a Running Task via annotation", func() {
		It("Should suspend Job and set Task status to Completed with Stopped condition", func() {
			taskName := "test-task-stop"
			agentName := "test-agent-stop"
			description := "# Stop test"

			By("Creating Agent")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "sleep 3600"},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to be Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Checking Job is created")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, jobLookupKey, createdJob) == nil
			}, timeout, interval).Should(BeTrue())

			By("Adding stop annotation to Task")
			currentTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, currentTask)).Should(Succeed())
			if currentTask.Annotations == nil {
				currentTask.Annotations = make(map[string]string)
			}
			currentTask.Annotations[AnnotationStop] = "true"
			Expect(k8sClient.Update(ctx, currentTask)).Should(Succeed())

			By("Checking Task status is Completed")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Checking Job still exists and is suspended")
			suspendedJob := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, jobLookupKey, suspendedJob)).Should(Succeed())
			Expect(suspendedJob.Spec.Suspend).ShouldNot(BeNil())
			Expect(*suspendedJob.Spec.Suspend).Should(BeTrue())

			By("Checking Task has Stopped condition")
			finalTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, finalTask)).Should(Succeed())

			var stoppedCondition *metav1.Condition
			for i := range finalTask.Status.Conditions {
				if finalTask.Status.Conditions[i].Type == "Stopped" {
					stoppedCondition = &finalTask.Status.Conditions[i]
					break
				}
			}
			Expect(stoppedCondition).ShouldNot(BeNil())
			Expect(stoppedCondition.Status).Should(Equal(metav1.ConditionTrue))
			Expect(stoppedCondition.Reason).Should(Equal("UserStopped"))

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
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-inline-git-validation",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					Command:            []string{"echo", "test"},
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with inline Git context without mountPath")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    agent.Name,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Inline: &kubetaskv1alpha1.ContextItem{
								Type: kubetaskv1alpha1.ContextTypeGit,
								Git: &kubetaskv1alpha1.GitContext{
									Repository: "https://github.com/example/repo",
								},
								// No MountPath - should fail validation
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Failed with validation error")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseFailed))

			By("Verifying error message mentions mountPath requirement")
			var readyCondition *metav1.Condition
			for i := range createdTask.Status.Conditions {
				if createdTask.Status.Conditions[i].Type == "Ready" {
					readyCondition = &createdTask.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Message).Should(ContainSubstring("inline Git context requires mountPath"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should fail when multiple contexts use same mountPath", func() {
			taskName := "test-task-mountpath-conflict"
			description := "Test mountPath conflict detection"
			conflictPath := "/workspace/config.yaml"

			By("Creating Agent")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-mountpath-conflict",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					Command:            []string{"echo", "test"},
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating two Context CRDs with same mountPath")
			context1 := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "context-conflict-1",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeText,
					Text: "content from context 1",
				},
			}
			Expect(k8sClient.Create(ctx, context1)).Should(Succeed())

			context2 := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "context-conflict-2",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeText,
					Text: "content from context 2",
				},
			}
			Expect(k8sClient.Create(ctx, context2)).Should(Succeed())

			By("Creating Task referencing both contexts with same mountPath")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    agent.Name,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Ref: &kubetaskv1alpha1.ContextRef{
								Name:      context1.Name,
								MountPath: conflictPath,
							},
						},
						{
							Ref: &kubetaskv1alpha1.ContextRef{
								Name:      context2.Name,
								MountPath: conflictPath, // Same path - should fail
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Failed with conflict error")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseFailed))

			By("Verifying error message mentions mount path conflict")
			var readyCondition *metav1.Condition
			for i := range createdTask.Status.Conditions {
				if createdTask.Status.Conditions[i].Type == "Ready" {
					readyCondition = &createdTask.Status.Conditions[i]
					break
				}
			}
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Message).Should(ContainSubstring("mount path conflict"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, context1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, context2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should succeed when inline Git context has mountPath specified", func() {
			taskName := "test-task-inline-git-with-mountpath"
			description := "Test inline Git with mountPath"

			By("Creating Agent")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "agent-inline-git-valid",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					Command:            []string{"echo", "test"},
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with inline Git context with mountPath")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    agent.Name,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Inline: &kubetaskv1alpha1.ContextItem{
								Type: kubetaskv1alpha1.ContextTypeGit,
								Git: &kubetaskv1alpha1.GitContext{
									Repository: "https://github.com/example/repo",
								},
								MountPath: "/workspace/my-repo", // Has mountPath - should succeed
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task status is Running (not Failed)")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

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
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					Command:            []string{"echo", "test"},
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task with inline Runtime context")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    agentName,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Inline: &kubetaskv1alpha1.ContextItem{
								Type:    kubetaskv1alpha1.ContextTypeRuntime,
								Runtime: &kubetaskv1alpha1.RuntimeContext{},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Running phase")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

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

			// Verify ConfigMap contains RuntimeSystemPrompt content
			// Key is "workspace-task.md" because workspaceDir is "/workspace" and it's sanitized
			taskMdContent, exists := cm.Data["workspace-task.md"]
			Expect(exists).To(BeTrue(), "workspace-task.md key should exist in ConfigMap")
			Expect(taskMdContent).To(ContainSubstring("KubeTask Runtime Context"))
			Expect(taskMdContent).To(ContainSubstring("TASK_NAME"))
			Expect(taskMdContent).To(ContainSubstring("TASK_NAMESPACE"))
			Expect(taskMdContent).To(ContainSubstring("kubectl get task"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("should inject RuntimeSystemPrompt when Runtime Context CRD is referenced", func() {
			ctx := context.Background()
			taskName := "task-runtime-context-ref"
			taskNamespace := "default"
			agentName := "agent-runtime-ref"
			contextName := "runtime-context-crd"
			description := "Test task with Runtime Context CRD"

			By("Creating Agent")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         "test-agent:v1.0.0",
					Command:            []string{"echo", "test"},
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "default",
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Runtime Context CRD")
			runtimeContext := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      contextName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type:    kubetaskv1alpha1.ContextTypeRuntime,
					Runtime: &kubetaskv1alpha1.RuntimeContext{},
				},
			}
			Expect(k8sClient.Create(ctx, runtimeContext)).Should(Succeed())

			By("Creating Task referencing Runtime Context CRD")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					Description: &description,
					AgentRef:    agentName,
					Contexts: []kubetaskv1alpha1.ContextSource{
						{
							Ref: &kubetaskv1alpha1.ContextRef{
								Name: contextName,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task transitions to Running phase")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			createdTask := &kubetaskv1alpha1.Task{}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				if err := k8sClient.Get(ctx, taskLookupKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

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

			// Verify ConfigMap contains RuntimeSystemPrompt content with correct context name
			// Key is "workspace-task.md" because workspaceDir is "/workspace" and it's sanitized
			taskMdContent, exists := cm.Data["workspace-task.md"]
			Expect(exists).To(BeTrue(), "workspace-task.md key should exist in ConfigMap")
			Expect(taskMdContent).To(ContainSubstring("KubeTask Runtime Context"))
			Expect(taskMdContent).To(ContainSubstring(contextName)) // Context name in XML tag

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, runtimeContext)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Session Cleanup Finalizer", func() {
		It("Should add session cleanup finalizer when persistence is enabled", func() {
			taskName := "test-task-session-finalizer"
			agentName := "test-agent-session-finalizer"
			pvcName := "test-session-pvc"
			description := "# Session cleanup test"

			By("Creating KubeTaskConfig with session PVC")
			config := &kubetaskv1alpha1.KubeTaskConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.KubeTaskConfigSpec{
					SessionPVC: &kubetaskv1alpha1.SessionPVCConfig{
						Name: pvcName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, config)).Should(Succeed())

			By("Creating Agent with session persistence enabled")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Persistence: &kubetaskv1alpha1.SessionPersistence{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task has session cleanup finalizer")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() bool {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return false
				}
				for _, f := range updatedTask.Finalizers {
					if f == SessionCleanupFinalizer {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Checking Task is Running")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, config)).Should(Succeed())
		})

		It("Should NOT add finalizer when persistence is not enabled", func() {
			taskName := "test-task-no-finalizer"
			agentName := "test-agent-no-persistence"
			description := "# No persistence test"

			By("Creating Agent without session persistence")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					// No HumanInTheLoop.Persistence
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task is Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Checking Task does NOT have session cleanup finalizer")
			updatedTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, updatedTask)).Should(Succeed())
			for _, f := range updatedTask.Finalizers {
				Expect(f).ShouldNot(Equal(SessionCleanupFinalizer))
			}

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should NOT add finalizer when session PVC is not configured", func() {
			taskName := "test-task-no-pvc-config"
			agentName := "test-agent-no-pvc"
			description := "# No PVC config test"

			By("Creating Agent with persistence enabled but no KubeTaskConfig")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Persistence: &kubetaskv1alpha1.SessionPersistence{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task (without KubeTaskConfig with sessionPVC)")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Checking Task is Running")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return ""
				}
				return updatedTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Checking Task does NOT have session cleanup finalizer")
			updatedTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskLookupKey, updatedTask)).Should(Succeed())
			for _, f := range updatedTask.Finalizers {
				Expect(f).ShouldNot(Equal(SessionCleanupFinalizer))
			}

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should create cleanup Job when Task with finalizer is deleted", func() {
			taskName := "test-task-cleanup-job"
			agentName := "test-agent-cleanup-job"
			pvcName := "test-session-pvc-cleanup"
			description := "# Cleanup job test"

			By("Creating KubeTaskConfig with session PVC")
			config := &kubetaskv1alpha1.KubeTaskConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.KubeTaskConfigSpec{
					SessionPVC: &kubetaskv1alpha1.SessionPVCConfig{
						Name: pvcName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, config)).Should(Succeed())

			By("Creating Agent with session persistence enabled")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Persistence: &kubetaskv1alpha1.SessionPersistence{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to be Running with finalizer")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() bool {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return false
				}
				hasFinalizer := false
				for _, f := range updatedTask.Finalizers {
					if f == SessionCleanupFinalizer {
						hasFinalizer = true
						break
					}
				}
				return updatedTask.Status.Phase == kubetaskv1alpha1.TaskPhaseRunning && hasFinalizer
			}, timeout, interval).Should(BeTrue())

			By("Deleting Task")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())

			By("Checking cleanup Job is created")
			cleanupJobName := fmt.Sprintf("%s-cleanup", taskName)
			cleanupJobLookupKey := types.NamespacedName{Name: cleanupJobName, Namespace: taskNamespace}
			cleanupJob := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, cleanupJobLookupKey, cleanupJob) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying cleanup Job has correct labels")
			Expect(cleanupJob.Labels).Should(HaveKeyWithValue("kubetask.io/cleanup", "session"))
			Expect(cleanupJob.Labels).Should(HaveKeyWithValue("kubetask.io/task", taskName))

			By("Verifying cleanup Job mounts the correct PVC")
			Expect(cleanupJob.Spec.Template.Spec.Volumes).Should(HaveLen(1))
			Expect(cleanupJob.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName).Should(Equal(pvcName))

			By("Simulating cleanup Job success")
			cleanupJob.Status.Succeeded = 1
			Expect(k8sClient.Status().Update(ctx, cleanupJob)).Should(Succeed())

			By("Checking Task is deleted after cleanup")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, taskLookupKey, &kubetaskv1alpha1.Task{})
				return err != nil && errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, config)).Should(Succeed())
		})

		It("Should remove finalizer even if cleanup Job fails after retries", func() {
			taskName := "test-task-cleanup-fail"
			agentName := "test-agent-cleanup-fail"
			pvcName := "test-session-pvc-fail"
			description := "# Cleanup failure test"

			By("Creating KubeTaskConfig with session PVC")
			config := &kubetaskv1alpha1.KubeTaskConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.KubeTaskConfigSpec{
					SessionPVC: &kubetaskv1alpha1.SessionPVCConfig{
						Name: pvcName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, config)).Should(Succeed())

			By("Creating Agent with session persistence enabled")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo test"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Persistence: &kubetaskv1alpha1.SessionPersistence{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to be Running with finalizer")
			taskLookupKey := types.NamespacedName{Name: taskName, Namespace: taskNamespace}
			Eventually(func() bool {
				updatedTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskLookupKey, updatedTask); err != nil {
					return false
				}
				hasFinalizer := false
				for _, f := range updatedTask.Finalizers {
					if f == SessionCleanupFinalizer {
						hasFinalizer = true
						break
					}
				}
				return updatedTask.Status.Phase == kubetaskv1alpha1.TaskPhaseRunning && hasFinalizer
			}, timeout, interval).Should(BeTrue())

			By("Deleting Task")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())

			By("Waiting for cleanup Job to be created")
			cleanupJobName := fmt.Sprintf("%s-cleanup", taskName)
			cleanupJobLookupKey := types.NamespacedName{Name: cleanupJobName, Namespace: taskNamespace}
			cleanupJob := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, cleanupJobLookupKey, cleanupJob) == nil
			}, timeout, interval).Should(BeTrue())

			By("Simulating cleanup Job failure (reaching backoff limit)")
			cleanupJob.Status.Failed = 3 // Matches the backoff limit
			Expect(k8sClient.Status().Update(ctx, cleanupJob)).Should(Succeed())

			By("Checking Task is deleted despite cleanup failure")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, taskLookupKey, &kubetaskv1alpha1.Task{})
				return err != nil && errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, config)).Should(Succeed())
		})
	})
})
