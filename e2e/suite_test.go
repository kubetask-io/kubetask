// Copyright Contributors to the KubeTask project

// Package e2e contains end-to-end tests for KubeTask
package e2e

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var (
	k8sClient client.Client
	clientset *kubernetes.Clientset
	ctx       context.Context
	cancel    context.CancelFunc
	scheme    *runtime.Scheme
	testNS    string
	echoImage string
)

const (
	// Timeout for e2e tests (longer than integration tests)
	timeout = time.Minute * 5

	// Interval for polling
	interval = time.Second * 2

	// Default test namespace
	defaultTestNS = "kubetask-e2e-test"

	// Default echo agent image
	defaultEchoImage = "quay.io/kubetask/kubetask-agent-echo:latest"

	// Test ServiceAccount name for e2e tests
	testServiceAccount = "kubetask-e2e-agent"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KubeTask E2E Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	By("Setting up test configuration")

	// Get test namespace from env or use default
	testNS = os.Getenv("E2E_TEST_NAMESPACE")
	if testNS == "" {
		testNS = defaultTestNS
	}

	// Get echo agent image from env or use default
	echoImage = os.Getenv("E2E_ECHO_IMAGE")
	if echoImage == "" {
		echoImage = defaultEchoImage
	}

	By("Connecting to Kubernetes cluster")

	// Use kubeconfig from env or default location
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = clientcmd.RecommendedHomeFile
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		// Try in-cluster config
		config, err = ctrl.GetConfig()
		Expect(err).NotTo(HaveOccurred(), "Failed to get Kubernetes config")
	}
	Expect(config).NotTo(BeNil())

	// Create scheme with all required types
	scheme = runtime.NewScheme()
	err = kubetaskv1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	err = corev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	err = batchv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	// Create controller-runtime client
	k8sClient, err = client.New(config, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Create clientset for pod logs and other operations
	clientset, err = kubernetes.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())

	By("Creating test namespace")
	ns := &corev1.Namespace{}
	ns.Name = testNS
	err = k8sClient.Create(ctx, ns)
	if err != nil && !isAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	By("Creating test ServiceAccount")
	sa := &corev1.ServiceAccount{}
	sa.Name = testServiceAccount
	sa.Namespace = testNS
	err = k8sClient.Create(ctx, sa)
	if err != nil && !isAlreadyExistsGeneric(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	By("Verifying controller is running")
	// Check that the controller deployment exists and is ready
	Eventually(func() bool {
		pods := &corev1.PodList{}
		err := k8sClient.List(ctx, pods, client.InNamespace("kubetask-system"), client.MatchingLabels{
			"app.kubernetes.io/name":      "kubetask",
			"app.kubernetes.io/component": "controller",
		})
		if err != nil {
			return false
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				return true
			}
		}
		return false
	}, timeout, interval).Should(BeTrue(), "Controller should be running")

	GinkgoWriter.Printf("E2E test setup complete. Namespace: %s, Echo Image: %s\n", testNS, echoImage)
})

var _ = AfterSuite(func() {
	By("Cleaning up test namespace")

	// Delete all Tasks in test namespace
	tasks := &kubetaskv1alpha1.TaskList{}
	if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS)); err == nil {
		for _, task := range tasks.Items {
			_ = k8sClient.Delete(ctx, &task)
		}
	}

	// Delete all Agents in test namespace
	agents := &kubetaskv1alpha1.AgentList{}
	if err := k8sClient.List(ctx, agents, client.InNamespace(testNS)); err == nil {
		for _, a := range agents.Items {
			_ = k8sClient.Delete(ctx, &a)
		}
	}

	// Delete all WorkflowRuns in test namespace
	workflowRuns := &kubetaskv1alpha1.WorkflowRunList{}
	if err := k8sClient.List(ctx, workflowRuns, client.InNamespace(testNS)); err == nil {
		for i := range workflowRuns.Items {
			_ = k8sClient.Delete(ctx, &workflowRuns.Items[i])
		}
	}

	// Delete all Workflows in test namespace
	workflows := &kubetaskv1alpha1.WorkflowList{}
	if err := k8sClient.List(ctx, workflows, client.InNamespace(testNS)); err == nil {
		for i := range workflows.Items {
			_ = k8sClient.Delete(ctx, &workflows.Items[i])
		}
	}

	// Wait for resources to be cleaned up
	time.Sleep(5 * time.Second)

	// Delete namespace if it was created by the test
	if testNS == defaultTestNS {
		ns := &corev1.Namespace{}
		ns.Name = testNS
		_ = k8sClient.Delete(ctx, ns)
	}

	cancel()
	GinkgoWriter.Println("E2E test cleanup complete")
})

// isAlreadyExists checks if the error is an "already exists" error for namespace
func isAlreadyExists(err error) bool {
	return err != nil && err.Error() == "namespaces \""+testNS+"\" already exists"
}

// isAlreadyExistsGeneric checks if the error is an "already exists" error
func isAlreadyExistsGeneric(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "already exists")
}

// Helper function to generate unique names for test resources
func uniqueName(prefix string) string {
	return prefix + "-" + time.Now().Format("150405")
}
