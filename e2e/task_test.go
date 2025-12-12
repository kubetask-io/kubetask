// Copyright Contributors to the KubeTask project

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var _ = Describe("Task E2E Tests", func() {
	var (
		agent     *kubetaskv1alpha1.Agent
		agentName string
	)

	BeforeEach(func() {
		// Create a Agent with echo agent for all tests
		agentName = uniqueName("echo-ws")
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

	Context("Task with description using echo agent", func() {
		It("should create a Job that echoes task content and complete successfully", func() {
			taskName := uniqueName("task-echo")
			taskContent := "# Hello E2E Test\n\nThis is a test task for the echo agent.\n\n## Expected Output\nThe echo agent should display this content."

			By("Creating a Task with description")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &taskContent,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to transition to Running")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Verifying Job is created")
			jobName := fmt.Sprintf("%s-job", taskName)
			jobKey := types.NamespacedName{Name: jobName, Namespace: testNS}
			job := &batchv1.Job{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, jobKey, job) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Job uses echo agent image")
			Expect(job.Spec.Template.Spec.Containers).Should(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Image).Should(Equal(echoImage))

			By("Waiting for Job to complete successfully")
			Eventually(func() int32 {
				if err := k8sClient.Get(ctx, jobKey, job); err != nil {
					return 0
				}
				return job.Status.Succeeded
			}, timeout, interval).Should(Equal(int32(1)))

			By("Verifying Task status is Completed")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Verifying pod logs contain the task content")
			logs := getPodLogs(ctx, testNS, jobName)
			Expect(logs).Should(ContainSubstring("=== Task Content ==="))
			Expect(logs).Should(ContainSubstring("Hello E2E Test"))
			Expect(logs).Should(ContainSubstring("=== Task Completed ==="))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("Task with multiple Context CRD references", func() {
		It("should mount multiple contexts and complete successfully", func() {
			taskName := uniqueName("task-multi")
			contextName1 := uniqueName("intro-context")
			contextName2 := uniqueName("details-context")
			contextName3 := uniqueName("conclusion-context")
			content1 := "# Part 1: Introduction\n\nThis is the introduction."
			content2 := "# Part 2: Details\n\nThese are the details."
			content3 := "# Part 3: Conclusion\n\nThis is the conclusion."
			description := "Review these documents"

			By("Creating Context CRDs")
			ctx1 := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      contextName1,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: content1,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ctx1)).Should(Succeed())

			ctx2 := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      contextName2,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: content2,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ctx2)).Should(Succeed())

			ctx3 := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      contextName3,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: content3,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ctx3)).Should(Succeed())

			By("Creating a Task with multiple Context references")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
					Contexts: []kubetaskv1alpha1.ContextMount{
						{
							Name:      contextName1,
							MountPath: "/workspace/intro.md",
						},
						{
							Name:      contextName2,
							MountPath: "/workspace/details.md",
						},
						{
							Name:      contextName3,
							MountPath: "/workspace/conclusion.md",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Verifying all content parts are in the logs")
			jobName := fmt.Sprintf("%s-job", taskName)
			logs := getPodLogs(ctx, testNS, jobName)
			Expect(logs).Should(ContainSubstring("Part 1: Introduction"))
			Expect(logs).Should(ContainSubstring("Part 2: Details"))
			Expect(logs).Should(ContainSubstring("Part 3: Conclusion"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ctx1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ctx2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ctx3)).Should(Succeed())
		})
	})

	Context("Task with Context from ConfigMap", func() {
		It("should resolve content from ConfigMap and pass to agent", func() {
			taskName := uniqueName("task-cm")
			configMapName := uniqueName("task-content-cm")
			contextName := uniqueName("cm-context")
			configMapContent := "# ConfigMap Content\n\nThis content comes from a ConfigMap.\n\n## Verification\nIf you see this, ConfigMap resolution works!"
			description := "Test ConfigMap context"

			By("Creating ConfigMap with task content")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNS,
				},
				Data: map[string]string{
					"content.md": configMapContent,
				},
			}
			Expect(k8sClient.Create(ctx, cm)).Should(Succeed())

			By("Creating Context CRD referencing ConfigMap")
			context := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      contextName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeConfigMap,
					ConfigMap: &kubetaskv1alpha1.ConfigMapContext{
						Name: configMapName,
						Key:  "content.md",
					},
				},
			}
			Expect(k8sClient.Create(ctx, context)).Should(Succeed())

			By("Creating Task referencing Context")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &description,
					Contexts: []kubetaskv1alpha1.ContextMount{
						{
							Name:      contextName,
							MountPath: "/workspace/guides/content.md",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Verifying ConfigMap content is in the logs")
			jobName := fmt.Sprintf("%s-job", taskName)
			logs := getPodLogs(ctx, testNS, jobName)
			Expect(logs).Should(ContainSubstring("ConfigMap Content"))
			Expect(logs).Should(ContainSubstring("ConfigMap resolution works"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, context)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, cm)).Should(Succeed())
		})
	})

	Context("Task with Agent contexts", func() {
		It("should merge agent contexts with task contexts", func() {
			taskName := uniqueName("task-default-ctx")
			customWSConfigName := uniqueName("ws-default-ctx")
			agentContextName := uniqueName("agent-ctx")
			taskContextName := uniqueName("task-ctx")
			defaultContent := "# Default Guidelines\n\nThese are organization-wide default guidelines."
			taskContextContent := "# Additional Context\n\nThis is additional context from the task."
			taskDescription := "# Specific Task\n\nThis is the specific task to execute."

			By("Creating Agent Context CRD")
			agentContext := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentContextName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: defaultContent,
					},
				},
			}
			Expect(k8sClient.Create(ctx, agentContext)).Should(Succeed())

			By("Creating Task Context CRD")
			taskContext := &kubetaskv1alpha1.Context{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskContextName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.ContextSpec{
					Type: kubetaskv1alpha1.ContextTypeInline,
					Inline: &kubetaskv1alpha1.InlineContext{
						Content: taskContextContent,
					},
				},
			}
			Expect(k8sClient.Create(ctx, taskContext)).Should(Succeed())

			By("Creating Agent with contexts")
			customWSConfig := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      customWSConfigName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					Contexts: []kubetaskv1alpha1.ContextMount{
						{
							Name: agentContextName,
							// No mountPath - should be appended to task.md
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, customWSConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    customWSConfigName,
					Description: &taskDescription,
					Contexts: []kubetaskv1alpha1.ContextMount{
						{
							Name: taskContextName,
							// No mountPath - should be appended to task.md
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Verifying both default and task content are in the logs")
			jobName := fmt.Sprintf("%s-job", taskName)
			logs := getPodLogs(ctx, testNS, jobName)
			Expect(logs).Should(ContainSubstring("Default Guidelines"))
			Expect(logs).Should(ContainSubstring("Specific Task"))
			Expect(logs).Should(ContainSubstring("Additional Context"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, customWSConfig)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agentContext)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, taskContext)).Should(Succeed())
		})
	})

	Context("Task lifecycle transitions", func() {
		It("should properly track phase transitions from Pending to Succeeded", func() {
			taskName := uniqueName("task-lifecycle")
			taskContent := "# Lifecycle Test\n\nSimple task for lifecycle testing."

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &taskContent,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}

			By("Verifying Task transitions to Running")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Verifying StartTime is set")
			runningTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, runningTask)).Should(Succeed())
			Expect(runningTask.Status.StartTime).ShouldNot(BeNil())
			Expect(runningTask.Status.JobName).ShouldNot(BeEmpty())

			By("Verifying Task transitions to Completed")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Verifying CompletionTime is set")
			completedTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, completedTask)).Should(Succeed())
			Expect(completedTask.Status.CompletionTime).ShouldNot(BeNil())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("Task garbage collection", func() {
		It("should clean up Job when Task is deleted (owner reference)", func() {
			taskName := uniqueName("task-gc")
			taskContent := "# GC Test"

			By("Creating and completing Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					AgentRef:    agentName,
					Description: &taskContent,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			jobName := fmt.Sprintf("%s-job", taskName)
			jobKey := types.NamespacedName{Name: jobName, Namespace: testNS}

			By("Waiting for Job to be created")
			Eventually(func() bool {
				job := &batchv1.Job{}
				return k8sClient.Get(ctx, jobKey, job) == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Job has owner reference to Task")
			job := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, jobKey, job)).Should(Succeed())
			Expect(job.OwnerReferences).Should(HaveLen(1))
			Expect(job.OwnerReferences[0].Name).Should(Equal(taskName))
			Expect(job.OwnerReferences[0].Kind).Should(Equal("Task"))

			By("Deleting Task")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())

			By("Verifying Task is deleted")
			Eventually(func() bool {
				task := &kubetaskv1alpha1.Task{}
				err := k8sClient.Get(ctx, taskKey, task)
				return err != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Job is garbage collected")
			Eventually(func() bool {
				job := &batchv1.Job{}
				err := k8sClient.Get(ctx, jobKey, job)
				return err != nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})

// getPodLogs retrieves logs from pods associated with a Job
func getPodLogs(ctx context.Context, namespace, jobName string) string {
	// List pods with the job-name label
	pods := &corev1.PodList{}
	err := k8sClient.List(ctx, pods,
		client.InNamespace(namespace),
		client.MatchingLabels{"job-name": jobName})
	if err != nil || len(pods.Items) == 0 {
		// Try alternative label format
		err = k8sClient.List(ctx, pods,
			client.InNamespace(namespace),
			client.MatchingLabels{"batch.kubernetes.io/job-name": jobName})
		if err != nil || len(pods.Items) == 0 {
			return ""
		}
	}

	var allLogs strings.Builder
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			req := clientset.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
			})
			stream, err := req.Stream(ctx)
			if err != nil {
				continue
			}
			defer stream.Close()

			buf := new(bytes.Buffer)
			_, err = io.Copy(buf, stream)
			if err != nil {
				continue
			}
			allLogs.WriteString(buf.String())
		}
	}

	return allLogs.String()
}
