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

	kubetaskv1alpha1 "github.com/xuezhaojun/kubetask/api/v1alpha1"
)

var _ = Describe("Task E2E Tests", func() {
	var (
		wsConfig     *kubetaskv1alpha1.WorkspaceConfig
		wsConfigName string
	)

	BeforeEach(func() {
		// Create a WorkspaceConfig with echo agent for all tests
		wsConfigName = uniqueName("echo-ws")
		wsConfig = &kubetaskv1alpha1.WorkspaceConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      wsConfigName,
				Namespace: testNS,
			},
			Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
				AgentImage:         echoImage,
				ServiceAccountName: testServiceAccount,
			},
		}
		Expect(k8sClient.Create(ctx, wsConfig)).Should(Succeed())
	})

	AfterEach(func() {
		// Clean up WorkspaceConfig
		if wsConfig != nil {
			_ = k8sClient.Delete(ctx, wsConfig)
		}
	})

	Context("Task with inline context using echo agent", func() {
		It("should create a Job that echoes task content and complete successfully", func() {
			taskName := uniqueName("task-echo")
			taskContent := "# Hello E2E Test\n\nThis is a test task for the echo agent.\n\n## Expected Output\nThe echo agent should display this content."

			By("Creating a Task with inline content")
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
									Inline: &taskContent,
								},
							},
						},
					},
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

			By("Verifying Task status is Succeeded")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

			By("Verifying pod logs contain the task content")
			logs := getPodLogs(ctx, testNS, jobName)
			Expect(logs).Should(ContainSubstring("=== Task Content ==="))
			Expect(logs).Should(ContainSubstring("Hello E2E Test"))
			Expect(logs).Should(ContainSubstring("=== Task Completed ==="))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("Task with multiple contexts aggregated", func() {
		It("should aggregate multiple contexts into task.md", func() {
			taskName := uniqueName("task-multi")
			content1 := "# Part 1: Introduction\n\nThis is the introduction."
			content2 := "# Part 2: Details\n\nThese are the details."
			content3 := "# Part 3: Conclusion\n\nThis is the conclusion."

			By("Creating a Task with multiple contexts")
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
								FilePath: "/workspace/intro.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &content1,
								},
							},
						},
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/details.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &content2,
								},
							},
						},
						{
							Type: kubetaskv1alpha1.ContextTypeFile,
							File: &kubetaskv1alpha1.FileContext{
								FilePath: "/workspace/conclusion.md",
								Source: kubetaskv1alpha1.FileSource{
									Inline: &content3,
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
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

			By("Verifying all content parts are in the logs")
			jobName := fmt.Sprintf("%s-job", taskName)
			logs := getPodLogs(ctx, testNS, jobName)
			Expect(logs).Should(ContainSubstring("Part 1: Introduction"))
			Expect(logs).Should(ContainSubstring("Part 2: Details"))
			Expect(logs).Should(ContainSubstring("Part 3: Conclusion"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
		})
	})

	Context("Task with context from ConfigMap", func() {
		It("should resolve content from ConfigMap and pass to agent", func() {
			taskName := uniqueName("task-cm")
			configMapName := uniqueName("task-content-cm")
			configMapContent := "# ConfigMap Content\n\nThis content comes from a ConfigMap.\n\n## Verification\nIf you see this, ConfigMap resolution works!"

			By("Creating ConfigMap with task content")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: testNS,
				},
				Data: map[string]string{
					"task.md": configMapContent,
				},
			}
			Expect(k8sClient.Create(ctx, cm)).Should(Succeed())

			By("Creating Task referencing ConfigMap")
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
									ConfigMapKeyRef: &kubetaskv1alpha1.ConfigMapKeySelector{
										Name: configMapName,
										Key:  "task.md",
									},
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
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

			By("Verifying ConfigMap content is in the logs")
			jobName := fmt.Sprintf("%s-job", taskName)
			logs := getPodLogs(ctx, testNS, jobName)
			Expect(logs).Should(ContainSubstring("ConfigMap Content"))
			Expect(logs).Should(ContainSubstring("ConfigMap resolution works"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, cm)).Should(Succeed())
		})
	})

	Context("Task with WorkspaceConfig defaultContexts", func() {
		It("should merge defaultContexts with task contexts", func() {
			taskName := uniqueName("task-default-ctx")
			customWSConfigName := uniqueName("ws-default-ctx")
			defaultContent := "# Default Guidelines\n\nThese are organization-wide default guidelines."
			taskContent := "# Specific Task\n\nThis is the specific task to execute."

			By("Creating WorkspaceConfig with defaultContexts")
			customWSConfig := &kubetaskv1alpha1.WorkspaceConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      customWSConfigName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WorkspaceConfigSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
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
			Expect(k8sClient.Create(ctx, customWSConfig)).Should(Succeed())

			By("Creating Task")
			task := &kubetaskv1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Name:      taskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.TaskSpec{
					WorkspaceConfigRef: customWSConfigName,
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

			By("Waiting for Task to complete")
			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

			By("Verifying both default and task content are in the logs")
			jobName := fmt.Sprintf("%s-job", taskName)
			logs := getPodLogs(ctx, testNS, jobName)
			Expect(logs).Should(ContainSubstring("Default Guidelines"))
			Expect(logs).Should(ContainSubstring("Specific Task"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, customWSConfig)).Should(Succeed())
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

			By("Verifying Task transitions to Succeeded")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				createdTask := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, createdTask); err != nil {
					return ""
				}
				return createdTask.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseSucceeded))

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
