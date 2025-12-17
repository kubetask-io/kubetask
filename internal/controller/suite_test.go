// Copyright Contributors to the KubeTask project

//go:build integration

// This file uses the "integration" build tag to separate envtest-based tests from unit tests.
// This is the standard pattern in the Kubernetes ecosystem (used by kubebuilder, controller-runtime,
// and most operator projects) because it allows tests to remain close to the code they test while
// still enabling separate execution:
//   - `go test ./...` runs only unit tests (no build tag)
//   - `go test -tags=integration ./...` runs integration tests (requires envtest binaries)
//
// Alternative approaches like placing tests in a separate directory (e.g., test/integration/)
// would separate tests from the code they test, making maintenance harder.

// Package controller implements Kubernetes controllers for KubeTask resources
package controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
	scheme    *runtime.Scheme
	fakeClock *FakeClock
)

// FakeClock is a mock clock for testing time-sensitive operations
type FakeClock struct {
	now time.Time
}

// Now returns the fake current time
func (f *FakeClock) Now() time.Time {
	return f.now
}

// SetTime sets the fake current time
func (f *FakeClock) SetTime(t time.Time) {
	f.now = t
}

// Advance advances the fake clock by the given duration
func (f *FakeClock) Advance(d time.Duration) {
	f.now = f.now.Add(d)
}

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "deploy", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme = runtime.NewScheme()
	err = kubetaskv1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	err = corev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	err = batchv1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Start controllers
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&TaskReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	// Initialize fake clock for CronWorkflow tests
	// Set initial time to a minute boundary to ensure predictable scheduling
	fakeClock = &FakeClock{now: time.Now().Truncate(time.Minute)}

	err = (&WorkflowReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&WorkflowRunReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	err = (&CronWorkflowReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
		Clock:  fakeClock,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	// Create default Agent for tests that don't specify one
	By("Creating default Agent")
	defaultAgent := &kubetaskv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "default",
		},
		Spec: kubetaskv1alpha1.AgentSpec{
			ServiceAccountName: "test-agent",
			WorkspaceDir:       "/workspace",
			Command:            []string{"sh", "-c", "echo 'default agent'"},
		},
	}
	Expect(k8sClient.Create(ctx, defaultAgent)).Should(Succeed())
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// stringPtr returns a pointer to the given string value
func stringPtr(s string) *string {
	return &s
}
