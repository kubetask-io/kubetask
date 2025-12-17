// Copyright Contributors to the KubeTask project

//go:build integration

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var _ = Describe("Workflow Controller", func() {
	const (
		workflowNamespace = "default"
	)

	Context("When creating a Workflow template", func() {
		It("Should accept and store the Workflow without execution", func() {
			workflowName := uniqueWorkflowName("template-workflow")

			By("Creating a Workflow template with 2 stages")
			workflow := &kubetaskv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowName,
					Namespace: workflowNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowSpec{
					Stages: []kubetaskv1alpha1.WorkflowStage{
						{
							Name: "stage-lint",
							Tasks: []kubetaskv1alpha1.WorkflowTask{
								{
									Name: "lint",
									Spec: kubetaskv1alpha1.TaskSpec{
										Description: stringPtr("Run linting"),
									},
								},
							},
						},
						{
							Name: "stage-test",
							Tasks: []kubetaskv1alpha1.WorkflowTask{
								{
									Name: "test-unit",
									Spec: kubetaskv1alpha1.TaskSpec{
										Description: stringPtr("Run unit tests"),
									},
								},
								{
									Name: "test-e2e",
									Spec: kubetaskv1alpha1.TaskSpec{
										Description: stringPtr("Run e2e tests"),
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflow)).Should(Succeed())

			By("Checking Workflow was created successfully")
			workflowLookupKey := types.NamespacedName{Name: workflowName, Namespace: workflowNamespace}
			createdWorkflow := &kubetaskv1alpha1.Workflow{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, workflowLookupKey, createdWorkflow)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Workflow spec is preserved")
			Expect(createdWorkflow.Spec.Stages).To(HaveLen(2))
			Expect(createdWorkflow.Spec.Stages[0].Name).To(Equal("stage-lint"))
			Expect(createdWorkflow.Spec.Stages[0].Tasks).To(HaveLen(1))
			Expect(createdWorkflow.Spec.Stages[1].Name).To(Equal("stage-test"))
			Expect(createdWorkflow.Spec.Stages[1].Tasks).To(HaveLen(2))

			By("Verifying no Tasks were created (Workflow is template-only)")
			taskList := &kubetaskv1alpha1.TaskList{}
			Consistently(func() int {
				err := k8sClient.List(ctx, taskList)
				if err != nil {
					return -1
				}
				count := 0
				for _, task := range taskList.Items {
					if task.Labels[WorkflowLabelKey] == workflowName {
						count++
					}
				}
				return count
			}, time.Second*2, interval).Should(Equal(0))

			By("Cleaning up Workflow")
			Expect(k8sClient.Delete(ctx, workflow)).Should(Succeed())
		})
	})

	Context("When Workflow is used as a template", func() {
		It("Should be referenceable by WorkflowRun", func() {
			workflowName := uniqueWorkflowName("reusable-template")

			By("Creating a Workflow template")
			workflow := &kubetaskv1alpha1.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workflowName,
					Namespace: workflowNamespace,
				},
				Spec: kubetaskv1alpha1.WorkflowSpec{
					Stages: []kubetaskv1alpha1.WorkflowStage{
						{
							Tasks: []kubetaskv1alpha1.WorkflowTask{
								{
									Name: "task-from-template",
									Spec: kubetaskv1alpha1.TaskSpec{
										Description: stringPtr("Task defined in Workflow template"),
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, workflow)).Should(Succeed())

			By("Verifying Workflow exists and can be retrieved")
			workflowLookupKey := types.NamespacedName{Name: workflowName, Namespace: workflowNamespace}
			createdWorkflow := &kubetaskv1alpha1.Workflow{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, workflowLookupKey, createdWorkflow)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Workflow has correct spec for referencing")
			Expect(createdWorkflow.Spec.Stages).To(HaveLen(1))
			Expect(createdWorkflow.Spec.Stages[0].Tasks[0].Name).To(Equal("task-from-template"))

			By("Cleaning up Workflow")
			Expect(k8sClient.Delete(ctx, workflow)).Should(Succeed())
		})
	})
})

// uniqueWorkflowName generates a unique workflow name for tests
func uniqueWorkflowName(base string) string {
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}

// stringPtr is defined in crontask_controller_test.go
