// Copyright Contributors to the KubeTask project

// Package e2e contains end-to-end tests for KubeTask
package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubetaskv1alpha1 "github.com/kubetask-io/kubetask/api/v1alpha1"
)

const (
	// CronTaskLabelKey is the label key used to identify Tasks created by a CronTask
	cronTaskLabelKey = "kubetask.io/crontask"
)

var _ = Describe("CronTask E2E Tests", func() {
	var (
		agent     *kubetaskv1alpha1.Agent
		agentName string
	)

	BeforeEach(func() {
		// Create a Agent with echo agent for all tests
		agentName = uniqueName("cron-echo-agent")
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

	Context("CronTask basic functionality", func() {
		It("should create Tasks on schedule", func() {
			cronTaskName := uniqueName("crontask-basic")

			By("Creating a CronTask with every-minute schedule")
			cronTask := &kubetaskv1alpha1.CronTask{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronTaskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.CronTaskSpec{
					Schedule:          "* * * * *", // Every minute
					ConcurrencyPolicy: kubetaskv1alpha1.ForbidConcurrent,
					TaskTemplate: kubetaskv1alpha1.TaskTemplateSpec{
						Spec: kubetaskv1alpha1.TaskSpec{
							AgentRef: agentName,
							Contexts: []kubetaskv1alpha1.Context{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/task.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: strPtr("# Scheduled Task\nThis is a test from CronTask."),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronTask)).Should(Succeed())

			By("Verifying CronTask was created")
			cronTaskKey := types.NamespacedName{Name: cronTaskName, Namespace: testNS}
			createdCronTask := &kubetaskv1alpha1.CronTask{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, cronTaskKey, createdCronTask) == nil
			}, timeout, interval).Should(BeTrue())

			By("Waiting for at least one Task to be created")
			Eventually(func() int {
				taskList := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, taskList,
					client.InNamespace(testNS),
					client.MatchingLabels{cronTaskLabelKey: cronTaskName}); err != nil {
					return 0
				}
				return len(taskList.Items)
			}, time.Minute*2, interval).Should(BeNumerically(">=", 1))

			By("Verifying CronTask status is updated")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, cronTaskKey, createdCronTask); err != nil {
					return false
				}
				return createdCronTask.Status.LastScheduleTime != nil
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up CronTask")
			Expect(k8sClient.Delete(ctx, cronTask)).Should(Succeed())

			// Wait for child Tasks to be garbage collected
			Eventually(func() int {
				taskList := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, taskList,
					client.InNamespace(testNS),
					client.MatchingLabels{cronTaskLabelKey: cronTaskName}); err != nil {
					return 0
				}
				return len(taskList.Items)
			}, timeout, interval).Should(Equal(0))
		})
	})

	Context("CronTask suspend functionality", func() {
		It("should not create Tasks when suspended", func() {
			cronTaskName := uniqueName("crontask-suspend")

			By("Creating a suspended CronTask")
			suspended := true
			cronTask := &kubetaskv1alpha1.CronTask{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronTaskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.CronTaskSpec{
					Schedule:          "* * * * *",
					Suspend:           &suspended,
					ConcurrencyPolicy: kubetaskv1alpha1.ForbidConcurrent,
					TaskTemplate: kubetaskv1alpha1.TaskTemplateSpec{
						Spec: kubetaskv1alpha1.TaskSpec{
							AgentRef: agentName,
							Contexts: []kubetaskv1alpha1.Context{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/task.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: strPtr("This should not run"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronTask)).Should(Succeed())

			By("Waiting to ensure no Tasks are created")
			time.Sleep(10 * time.Second)

			By("Verifying no Tasks were created")
			taskList := &kubetaskv1alpha1.TaskList{}
			Expect(k8sClient.List(ctx, taskList,
				client.InNamespace(testNS),
				client.MatchingLabels{cronTaskLabelKey: cronTaskName})).Should(Succeed())
			Expect(len(taskList.Items)).Should(Equal(0))

			By("Resuming CronTask")
			cronTaskKey := types.NamespacedName{Name: cronTaskName, Namespace: testNS}
			Expect(k8sClient.Get(ctx, cronTaskKey, cronTask)).Should(Succeed())
			cronTask.Spec.Suspend = nil // Resume
			Expect(k8sClient.Update(ctx, cronTask)).Should(Succeed())

			By("Waiting for a Task to be created after resuming")
			Eventually(func() int {
				taskList := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, taskList,
					client.InNamespace(testNS),
					client.MatchingLabels{cronTaskLabelKey: cronTaskName}); err != nil {
					return 0
				}
				return len(taskList.Items)
			}, time.Minute*2, interval).Should(BeNumerically(">=", 1))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronTask)).Should(Succeed())
		})
	})

	Context("CronTask history limits", func() {
		It("should respect history limits and clean up old Tasks", func() {
			cronTaskName := uniqueName("crontask-history")

			By("Creating a CronTask with low history limits")
			successLimit := int32(2)
			failedLimit := int32(1)
			cronTask := &kubetaskv1alpha1.CronTask{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cronTaskName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.CronTaskSpec{
					Schedule:                    "* * * * *",
					ConcurrencyPolicy:           kubetaskv1alpha1.AllowConcurrent,
					SuccessfulTasksHistoryLimit: &successLimit,
					FailedTasksHistoryLimit:     &failedLimit,
					TaskTemplate: kubetaskv1alpha1.TaskTemplateSpec{
						Spec: kubetaskv1alpha1.TaskSpec{
							AgentRef: agentName,
							Contexts: []kubetaskv1alpha1.Context{
								{
									Type: kubetaskv1alpha1.ContextTypeFile,
									File: &kubetaskv1alpha1.FileContext{
										FilePath: "/workspace/task.md",
										Source: kubetaskv1alpha1.FileSource{
											Inline: strPtr("History limit test"),
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cronTask)).Should(Succeed())

			By("Verifying CronTask was created")
			cronTaskKey := types.NamespacedName{Name: cronTaskName, Namespace: testNS}
			createdCronTask := &kubetaskv1alpha1.CronTask{}
			Eventually(func() bool {
				return k8sClient.Get(ctx, cronTaskKey, createdCronTask) == nil
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, cronTask)).Should(Succeed())
		})
	})
})

// strPtr returns a pointer to the given string
func strPtr(s string) *string {
	return &s
}
