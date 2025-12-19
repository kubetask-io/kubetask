// Copyright Contributors to the KubeTask project

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

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var _ = Describe("Session Persistence E2E Tests", Label(LabelSession), func() {
	var (
		kubeTaskConfig *kubetaskv1alpha1.KubeTaskConfig
		sessionPVC     *corev1.PersistentVolumeClaim
		pvcName        string
	)

	BeforeEach(func() {
		pvcName = uniqueName("session-pvc")

		By("Creating PVC for session persistence")
		sessionPVC = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: testNS,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce, // Kind cluster may not support RWX
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, sessionPVC)).Should(Succeed())

		By("Creating KubeTaskConfig with sessionPVC")
		kubeTaskConfig = &kubetaskv1alpha1.KubeTaskConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: testNS,
			},
			Spec: kubetaskv1alpha1.KubeTaskConfigSpec{
				SessionPVC: &kubetaskv1alpha1.SessionPVCConfig{
					Name:        pvcName,
					StorageSize: "1Gi",
				},
			},
		}
		err := k8sClient.Create(ctx, kubeTaskConfig)
		if err != nil && !isAlreadyExistsGeneric(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterEach(func() {
		// Clean up KubeTaskConfig
		if kubeTaskConfig != nil {
			_ = k8sClient.Delete(ctx, kubeTaskConfig)
		}
		// Clean up PVC
		if sessionPVC != nil {
			_ = k8sClient.Delete(ctx, sessionPVC)
		}
	})

	Context("Task with session persistence enabled", func() {
		It("should add save-session sidecar to Pod", func() {
			agentName := uniqueName("session-agent")
			taskName := uniqueName("task-session")
			taskContent := "# Session Persistence Test"

			By("Creating Agent with humanInTheLoop.persistence enabled")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'Task running' && echo 'test-data' > ${WORKSPACE_DIR}/output.txt && cat ${WORKSPACE_DIR}/task.md"},
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

			By("Waiting for Task to start running")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Verifying Pod has save-session sidecar")
			Eventually(func() bool {
				pods := &corev1.PodList{}
				if err := k8sClient.List(ctx, pods,
					client.InNamespace(testNS),
					client.MatchingLabels{"job-name": jobName}); err != nil || len(pods.Items) == 0 {
					// Try alternative label
					_ = k8sClient.List(ctx, pods,
						client.InNamespace(testNS),
						client.MatchingLabels{"batch.kubernetes.io/job-name": jobName})
					if len(pods.Items) == 0 {
						return false
					}
				}

				// Check for save-session container
				for _, container := range pods.Items[0].Spec.Containers {
					if container.Name == "save-session" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue(), "Pod should have save-session sidecar")

			By("Waiting for Task to complete")
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
		})

		It("should create session Pod when resume annotation is added", func() {
			agentName := uniqueName("resume-agent")
			taskName := uniqueName("task-resume")
			taskContent := "# Resume Session Test"

			By("Creating Agent with humanInTheLoop.persistence enabled")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'Creating output' && echo 'resume-test' > ${WORKSPACE_DIR}/session-data.txt"},
					HumanInTheLoop: &kubetaskv1alpha1.HumanInTheLoop{
						Persistence: &kubetaskv1alpha1.SessionPersistence{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

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

			By("Waiting for Task to complete")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseCompleted))

			By("Adding resume-session annotation")
			completedTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, completedTask)).Should(Succeed())
			if completedTask.Annotations == nil {
				completedTask.Annotations = make(map[string]string)
			}
			completedTask.Annotations["kubetask.io/resume-session"] = "true"
			Expect(k8sClient.Update(ctx, completedTask)).Should(Succeed())

			By("Waiting for SessionStatus to be set")
			Eventually(func() bool {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return false
				}
				return t.Status.SessionStatus != nil
			}, timeout, interval).Should(BeTrue(), "Task should have SessionStatus set")

			By("Verifying session Pod is created")
			Eventually(func() string {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.SessionPodName
			}, timeout, interval).ShouldNot(BeEmpty(), "Task should have SessionPodName set")

			By("Verifying session Pod exists and is running")
			updatedTask := &kubetaskv1alpha1.Task{}
			Expect(k8sClient.Get(ctx, taskKey, updatedTask)).Should(Succeed())
			sessionPodKey := types.NamespacedName{
				Name:      updatedTask.Status.SessionPodName,
				Namespace: testNS,
			}

			Eventually(func() corev1.PodPhase {
				pod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, sessionPodKey, pod); err != nil {
					return ""
				}
				return pod.Status.Phase
			}, timeout, interval).Should(Equal(corev1.PodRunning), "Session Pod should be running")

			By("Cleaning up")
			// Delete session Pod first
			sessionPod := &corev1.Pod{}
			if err := k8sClient.Get(ctx, sessionPodKey, sessionPod); err == nil {
				_ = k8sClient.Delete(ctx, sessionPod)
			}
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("Task without session persistence", func() {
		It("should not add save-session sidecar when persistence is disabled", func() {
			agentName := uniqueName("no-session-agent")
			taskName := uniqueName("task-no-session")
			taskContent := "# No Session Persistence"

			By("Creating Agent without humanInTheLoop")
			agent := &kubetaskv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.AgentSpec{
					AgentImage:         echoImage,
					ServiceAccountName: testServiceAccount,
					WorkspaceDir:       "/workspace",
					Command:            []string{"sh", "-c", "echo 'No persistence' && cat ${WORKSPACE_DIR}/task.md"},
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
					Description: &taskContent,
				},
			}
			Expect(k8sClient.Create(ctx, task)).Should(Succeed())

			taskKey := types.NamespacedName{Name: taskName, Namespace: testNS}
			jobName := fmt.Sprintf("%s-job", taskName)

			By("Waiting for Task to start running")
			Eventually(func() kubetaskv1alpha1.TaskPhase {
				t := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, taskKey, t); err != nil {
					return ""
				}
				return t.Status.Phase
			}, timeout, interval).Should(Equal(kubetaskv1alpha1.TaskPhaseRunning))

			By("Verifying Pod does NOT have save-session sidecar")
			// Give it some time to ensure the pod is created
			time.Sleep(5 * time.Second)

			pods := &corev1.PodList{}
			err := k8sClient.List(ctx, pods,
				client.InNamespace(testNS),
				client.MatchingLabels{"job-name": jobName})
			if err != nil || len(pods.Items) == 0 {
				// Try alternative label
				err = k8sClient.List(ctx, pods,
					client.InNamespace(testNS),
					client.MatchingLabels{"batch.kubernetes.io/job-name": jobName})
				Expect(err).NotTo(HaveOccurred(), "Failed to list pods")
			}
			Expect(len(pods.Items)).Should(BeNumerically(">", 0), "Pod should exist")

			hasSaveSession := false
			for _, container := range pods.Items[0].Spec.Containers {
				if container.Name == "save-session" {
					hasSaveSession = true
					break
				}
			}
			Expect(hasSaveSession).Should(BeFalse(), "Pod should NOT have save-session sidecar")

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, task)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})
})
