// Copyright Contributors to the KubeOpenCode project

//go:build integration

package controller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kubeopenv1alpha1 "github.com/kubeopencode/kubeopencode/api/v1alpha1"
)

var _ = Describe("AgentController", func() {
	const (
		agentNamespace = "default"
	)

	Context("When creating a Server-mode Agent", func() {
		It("Should create a Deployment and Service", func() {
			agentName := "test-server-agent"

			By("Creating a Server-mode Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: agentNamespace,
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

			By("Expecting a Deployment to be created")
			deploymentName := ServerDeploymentName(agentName)
			Eventually(func() error {
				var deployment appsv1.Deployment
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: agentNamespace,
				}, &deployment)
			}, timeout, interval).Should(Succeed())

			By("Expecting a Service to be created")
			serviceName := ServerServiceName(agentName)
			Eventually(func() error {
				var service corev1.Service
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      serviceName,
					Namespace: agentNamespace,
				}, &service)
			}, timeout, interval).Should(Succeed())

			By("Expecting Agent status to be updated with ServerStatus")
			Eventually(func() bool {
				var updatedAgent kubeopenv1alpha1.Agent
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      agentName,
					Namespace: agentNamespace,
				}, &updatedAgent); err != nil {
					return false
				}
				return updatedAgent.Status.ServerStatus != nil
			}, timeout, interval).Should(BeTrue())

			By("Verifying Deployment has correct labels and selector")
			var deployment appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      deploymentName,
				Namespace: agentNamespace,
			}, &deployment)).Should(Succeed())
			Expect(deployment.Labels["kubeopencode.io/agent"]).To(Equal(agentName))
			Expect(deployment.Spec.Selector.MatchLabels["kubeopencode.io/agent"]).To(Equal(agentName))

			By("Verifying Service has correct selector")
			var service corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      serviceName,
				Namespace: agentNamespace,
			}, &service)).Should(Succeed())
			Expect(service.Spec.Selector["kubeopencode.io/agent"]).To(Equal(agentName))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(4096)))

			By("Cleaning up the Agent")
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When creating a Pod-mode Agent (no serverConfig)", func() {
		It("Should NOT create a Deployment or Service", func() {
			agentName := "test-pod-agent"

			By("Creating a Pod-mode Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: agentNamespace,
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ExecutorImage:      "quay.io/kubeopencode/kubeopencode-agent-devbox:latest",
					WorkspaceDir:       "/workspace",
					ServiceAccountName: "test-agent",
					// No ServerConfig = Pod mode
				},
			}
			Expect(k8sClient.Create(ctx, agent)).Should(Succeed())

			By("Expecting NO Deployment to be created")
			deploymentName := ServerDeploymentName(agentName)
			Consistently(func() error {
				var deployment appsv1.Deployment
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: agentNamespace,
				}, &deployment)
			}, timeout/2, interval).ShouldNot(Succeed())

			By("Expecting NO Service to be created")
			serviceName := ServerServiceName(agentName)
			Consistently(func() error {
				var service corev1.Service
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      serviceName,
					Namespace: agentNamespace,
				}, &service)
			}, timeout/2, interval).ShouldNot(Succeed())

			By("Cleaning up the Agent")
			Expect(k8sClient.Delete(ctx, agent)).Should(Succeed())
		})
	})

	Context("When updating a Server-mode Agent", func() {
		It("Should update the Deployment with new configuration", func() {
			agentName := "test-update-agent"

			By("Creating a Server-mode Agent with initial port")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: agentNamespace,
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

			By("Waiting for Deployment to be created")
			deploymentName := ServerDeploymentName(agentName)
			Eventually(func() error {
				var deployment appsv1.Deployment
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: agentNamespace,
				}, &deployment)
			}, timeout, interval).Should(Succeed())

			By("Updating the Agent with a new port")
			var updatedAgent kubeopenv1alpha1.Agent
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      agentName,
				Namespace: agentNamespace,
			}, &updatedAgent)).Should(Succeed())
			updatedAgent.Spec.ServerConfig.Port = 8080
			Expect(k8sClient.Update(ctx, &updatedAgent)).Should(Succeed())

			By("Expecting the Deployment to be updated with new port")
			Eventually(func() int32 {
				var deployment appsv1.Deployment
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: agentNamespace,
				}, &deployment); err != nil {
					return 0
				}
				if len(deployment.Spec.Template.Spec.Containers) == 0 {
					return 0
				}
				if len(deployment.Spec.Template.Spec.Containers[0].Ports) == 0 {
					return 0
				}
				return deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort
			}, timeout, interval).Should(Equal(int32(8080)))

			By("Cleaning up the Agent")
			Expect(k8sClient.Delete(ctx, &updatedAgent)).Should(Succeed())
		})
	})

	Context("When switching from Server-mode to Pod-mode", func() {
		It("Should clean up Deployment and Service", func() {
			agentName := "test-mode-switch-agent"

			By("Creating a Server-mode Agent")
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName,
					Namespace: agentNamespace,
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

			By("Waiting for Deployment to be created")
			deploymentName := ServerDeploymentName(agentName)
			Eventually(func() error {
				var deployment appsv1.Deployment
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: agentNamespace,
				}, &deployment)
			}, timeout, interval).Should(Succeed())

			By("Switching to Pod-mode by removing serverConfig")
			var updatedAgent kubeopenv1alpha1.Agent
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      agentName,
				Namespace: agentNamespace,
			}, &updatedAgent)).Should(Succeed())
			updatedAgent.Spec.ServerConfig = nil
			Expect(k8sClient.Update(ctx, &updatedAgent)).Should(Succeed())

			By("Expecting the Deployment to be cleaned up")
			Eventually(func() error {
				var deployment appsv1.Deployment
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      deploymentName,
					Namespace: agentNamespace,
				}, &deployment)
			}, timeout, interval).ShouldNot(Succeed())

			By("Expecting the Service to be cleaned up")
			serviceName := ServerServiceName(agentName)
			Eventually(func() error {
				var service corev1.Service
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      serviceName,
					Namespace: agentNamespace,
				}, &service)
			}, timeout, interval).ShouldNot(Succeed())

			By("Cleaning up the Agent")
			Expect(k8sClient.Delete(ctx, &updatedAgent)).Should(Succeed())
		})
	})

	Context("IsServerMode helper function", func() {
		It("Should correctly identify server mode", func() {
			serverAgent := &kubeopenv1alpha1.Agent{
				Spec: kubeopenv1alpha1.AgentSpec{
					ServerConfig: &kubeopenv1alpha1.ServerConfig{
						Port: 4096,
					},
				},
			}
			Expect(IsServerMode(serverAgent)).To(BeTrue())

			podAgent := &kubeopenv1alpha1.Agent{
				Spec: kubeopenv1alpha1.AgentSpec{
					// No ServerConfig
				},
			}
			Expect(IsServerMode(podAgent)).To(BeFalse())
		})
	})

	Context("GetServerPort helper function", func() {
		It("Should return configured port or default", func() {
			By("Agent with configured port")
			agentWithPort := &kubeopenv1alpha1.Agent{
				Spec: kubeopenv1alpha1.AgentSpec{
					ServerConfig: &kubeopenv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}
			Expect(GetServerPort(agentWithPort)).To(Equal(int32(8080)))

			By("Agent with zero port (should use default)")
			agentWithZeroPort := &kubeopenv1alpha1.Agent{
				Spec: kubeopenv1alpha1.AgentSpec{
					ServerConfig: &kubeopenv1alpha1.ServerConfig{
						Port: 0,
					},
				},
			}
			Expect(GetServerPort(agentWithZeroPort)).To(Equal(DefaultServerPort))

			By("Agent without serverConfig (should use default)")
			agentWithoutConfig := &kubeopenv1alpha1.Agent{
				Spec: kubeopenv1alpha1.AgentSpec{},
			}
			Expect(GetServerPort(agentWithoutConfig)).To(Equal(DefaultServerPort))
		})
	})

	Context("ServerURL helper function", func() {
		It("Should generate correct in-cluster URL", func() {
			url := ServerURL("my-agent", "my-namespace", 4096)
			Expect(url).To(Equal("http://my-agent.my-namespace.svc.cluster.local:4096"))
		})
	})

	Context("Naming helper functions", func() {
		It("Should generate correct names", func() {
			Expect(ServerDeploymentName("my-agent")).To(Equal("my-agent-server"))
			Expect(ServerServiceName("my-agent")).To(Equal("my-agent"))
		})
	})
})

var _ = Describe("DeploymentBuilder", func() {
	Context("BuildServerDeployment", func() {
		It("Should return nil for Pod-mode Agent", func() {
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "default",
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					// No ServerConfig
				},
			}
			cfg := agentConfig{
				executorImage: "test-image",
				workspaceDir:  "/workspace",
			}
			sysCfg := systemConfig{}

			deployment := BuildServerDeployment(agent, cfg, sysCfg)
			Expect(deployment).To(BeNil())
		})

		It("Should build correct Deployment for Server-mode Agent", func() {
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server-agent",
					Namespace: "default",
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServerConfig: &kubeopenv1alpha1.ServerConfig{
						Port: 4096,
					},
				},
			}
			cfg := agentConfig{
				executorImage: "test-executor-image",
				agentImage:    "test-agent-image",
				workspaceDir:  "/workspace",
			}
			sysCfg := systemConfig{}

			deployment := BuildServerDeployment(agent, cfg, sysCfg)
			Expect(deployment).NotTo(BeNil())
			Expect(deployment.Name).To(Equal("test-server-agent-server"))
			Expect(deployment.Namespace).To(Equal("default"))
			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(4096)))
		})

		It("Should use default port when not specified", func() {
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-default-port-agent",
					Namespace: "default",
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServerConfig: &kubeopenv1alpha1.ServerConfig{
						// Port not specified, should use default
					},
				},
			}
			cfg := agentConfig{
				executorImage: "test-executor-image",
				agentImage:    "test-agent-image",
				workspaceDir:  "/workspace",
			}
			sysCfg := systemConfig{}

			deployment := BuildServerDeployment(agent, cfg, sysCfg)
			Expect(deployment).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(DefaultServerPort))
		})
	})

	Context("BuildServerService", func() {
		It("Should return nil for Pod-mode Agent", func() {
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-agent",
					Namespace: "default",
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					// No ServerConfig
				},
			}

			service := BuildServerService(agent)
			Expect(service).To(BeNil())
		})

		It("Should build correct Service for Server-mode Agent", func() {
			agent := &kubeopenv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server-agent",
					Namespace: "default",
				},
				Spec: kubeopenv1alpha1.AgentSpec{
					ServerConfig: &kubeopenv1alpha1.ServerConfig{
						Port: 8080,
					},
				},
			}

			service := BuildServerService(agent)
			Expect(service).NotTo(BeNil())
			Expect(service.Name).To(Equal("test-server-agent"))
			Expect(service.Namespace).To(Equal("default"))
			Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(8080)))
			Expect(service.Spec.Selector["kubeopencode.io/agent"]).To(Equal("test-server-agent"))
		})
	})
})

// Helper to generate unique agent names for tests
var agentCounter = 0

func uniqueAgentName(prefix string) string {
	agentCounter++
	return fmt.Sprintf("%s-%d", prefix, agentCounter)
}
