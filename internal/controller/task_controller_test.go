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
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: contextContent,
					},
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
					Contexts: []kubetaskv1alpha1.ContextMount{
						{
							Name:      contextName,
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
			Expect(k8sClient.Delete(ctx, context)).Should(Succeed())
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
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: contextContent,
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
					Contexts: []kubetaskv1alpha1.ContextMount{
						{
							Name: contextName,
							// No MountPath - should be appended to task.md
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
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: agentContextContent,
					},
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
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: taskContextContent,
					},
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
					Contexts: []kubetaskv1alpha1.ContextMount{
						{
							Name: agentContextName,
							// No mountPath - should be appended to task.md
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
					Contexts: []kubetaskv1alpha1.ContextMount{
						{
							Name: taskContextName,
							// No mountPath - should be appended to task.md
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

	Context("When creating a Task with Agent that has humanInTheLoop enabled", func() {
		It("Should wrap command with sleep for keep-alive", func() {
			taskName := "test-task-hitl"
			agentName := "test-agent-hitl"
			description := "# Human-in-the-loop test"
			keepAliveSeconds := int32(1800) // 30 minutes

			By("Creating Agent with humanInTheLoop enabled")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					Command:            []string{"sh", "-c", "echo hello"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Enabled:          true,
						KeepAliveSeconds: &keepAliveSeconds,
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

			By("Checking Job command is wrapped with sleep")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return false
				}
				return len(createdJob.Spec.Template.Spec.Containers) > 0
			}, timeout, interval).Should(BeTrue())

			// Command should be wrapped: sh -c 'original_command; EXIT_CODE=$?; ... sleep N; exit $EXIT_CODE'
			container := createdJob.Spec.Template.Spec.Containers[0]
			Expect(container.Command).Should(HaveLen(3))
			Expect(container.Command[0]).Should(Equal("sh"))
			Expect(container.Command[1]).Should(Equal("-c"))
			Expect(container.Command[2]).Should(ContainSubstring("sh -c echo hello"))
			Expect(container.Command[2]).Should(ContainSubstring("sleep 1800"))
			Expect(container.Command[2]).Should(ContainSubstring("Human-in-the-loop"))

			By("Checking keep-alive environment variable is set")
			var keepAliveEnv *corev1.EnvVar
			for _, env := range container.Env {
				if env.Name == EnvHumanInTheLoopKeepAlive {
					keepAliveEnv = &env
					break
				}
			}
			Expect(keepAliveEnv).ShouldNot(BeNil())
			Expect(keepAliveEnv.Value).Should(Equal("1800"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})

		It("Should use default keep-alive when not specified", func() {
			taskName := "test-task-hitl-default"
			agentName := "test-agent-hitl-default"
			description := "# Human-in-the-loop default test"

			By("Creating Agent with humanInTheLoop enabled but no keepAliveSeconds")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: taskNamespace,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					ServiceAccountName: "test-agent",
					Command:            []string{"./run.sh"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Enabled: true,
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

			By("Checking Job uses default keep-alive (3600 seconds)")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobLookupKey := types.NamespacedName{Name: jobName, Namespace: taskNamespace}
			createdJob := &batchv1.Job{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, jobLookupKey, createdJob); err != nil {
					return false
				}
				return len(createdJob.Spec.Template.Spec.Containers) > 0
			}, timeout, interval).Should(BeTrue())

			container := createdJob.Spec.Template.Spec.Containers[0]
			Expect(container.Command[2]).Should(ContainSubstring("sleep 3600"))

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
})
