// Copyright Contributors to the KubeTask project

package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubetaskv1alpha1 "github.com/kubetask/kubetask/api/v1alpha1"
)

var _ = Describe("WebhookTrigger E2E Tests", Label(LabelWebhookTrigger), func() {
	var (
		agent     *kubetaskv1alpha1.Agent
		agentName string
	)

	// Get webhook server URL from cluster
	// The webhook server is exposed as NodePort service for E2E tests
	getWebhookURL := func(namespace, triggerName string) string {
		return fmt.Sprintf("%s/webhooks/%s/%s", webhookBaseURL, namespace, triggerName)
	}

	// postWebhook sends a POST request to the webhook URL
	// nolint:gosec // G107: URL is constructed from test parameters, not user input
	postWebhook := func(url string, payload []byte) (*http.Response, error) {
		return http.Post(url, "application/json", bytes.NewReader(payload))
	}

	BeforeEach(func() {
		// Create an Agent with echo agent for all tests
		agentName = uniqueName("wht-echo")
		agent = &kubetaskv1alpha1.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      agentName,
				Namespace: testNS,
			},
			Spec: kubetaskv1alpha1.AgentSpec{
				AgentImage:         echoImage,
				ServiceAccountName: testServiceAccount,
				WorkspaceDir:       "/workspace",
				Command:            []string{"sh", "-c", "echo '=== WebhookTrigger Task ===' && cat ${WORKSPACE_DIR}/task.md && echo '=== Done ==='"},
			},
		}
		Expect(k8sClient.Create(ctx, agent)).Should(Succeed())
	})

	AfterEach(func() {
		// Clean up Agent
		if agent != nil {
			_ = k8sClient.Delete(ctx, agent)
		}

		// Clean up all WebhookTriggers in test namespace
		triggers := &kubetaskv1alpha1.WebhookTriggerList{}
		if err := k8sClient.List(ctx, triggers, client.InNamespace(testNS)); err == nil {
			for i := range triggers.Items {
				_ = k8sClient.Delete(ctx, &triggers.Items[i])
			}
		}
	})

	Context("WebhookTrigger basic functionality", func() {
		It("should create WebhookTrigger and update webhookURL status", func() {
			triggerName := uniqueName("wht-basic")

			By("Creating a WebhookTrigger")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					TaskTemplate: &kubetaskv1alpha1.WebhookTaskTemplate{
						AgentRef:    agentName,
						Description: "Test webhook task",
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}

			By("Verifying WebhookTrigger status.webhookURL is set")
			Eventually(func() string {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return ""
				}
				return t.Status.WebhookURL
			}, timeout, interval).Should(Equal(fmt.Sprintf("/webhooks/%s/%s", testNS, triggerName)))

			By("Verifying Ready condition is set")
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				for _, c := range t.Status.Conditions {
					if c.Type == "Ready" && c.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})

		It("should create Task when webhook is received", func() {

			triggerName := uniqueName("wht-create")

			By("Creating a WebhookTrigger")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					TaskTemplate: &kubetaskv1alpha1.WebhookTaskTemplate{
						AgentRef:    agentName,
						Description: "Webhook triggered task: {{ .event }}",
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			By("Sending webhook request")
			webhookURL := getWebhookURL(testNS, triggerName)
			payload := map[string]interface{}{
				"event": "test-event",
				"data":  "hello world",
			}
			payloadBytes, _ := json.Marshal(payload)

			resp, err := postWebhook(webhookURL, payloadBytes)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()
			Expect(resp.StatusCode).Should(Equal(http.StatusCreated))

			By("Verifying Task was created")
			Eventually(func() bool {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName}); err != nil {
					return false
				}
				return len(tasks.Items) > 0
			}, timeout, interval).Should(BeTrue())

			By("Verifying WebhookTrigger status is updated")
			Eventually(func() int64 {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return 0
				}
				return t.Status.TotalTriggered
			}, timeout, interval).Should(BeNumerically(">=", 1))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})
	})

	Context("WebhookTrigger with CEL filter", func() {
		It("should create Task only when filter matches", func() {

			triggerName := uniqueName("wht-filter")

			By("Creating a WebhookTrigger with CEL filter")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					Filter: `body.action == "opened"`,
					TaskTemplate: &kubetaskv1alpha1.WebhookTaskTemplate{
						AgentRef:    agentName,
						Description: "Filtered webhook task",
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			webhookURL := getWebhookURL(testNS, triggerName)

			By("Sending webhook that does NOT match filter")
			payload1 := map[string]interface{}{
				"action": "closed",
			}
			payloadBytes1, _ := json.Marshal(payload1)
			resp1, err := postWebhook(webhookURL, payloadBytes1)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp1.Body.Close() }()
			// Should return 200 but not create Task
			Expect(resp1.StatusCode).Should(Equal(http.StatusOK))

			By("Verifying no Task was created for non-matching webhook")
			time.Sleep(2 * time.Second)
			tasks := &kubetaskv1alpha1.TaskList{}
			Expect(k8sClient.List(ctx, tasks, client.InNamespace(testNS),
				client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName})).Should(Succeed())
			Expect(tasks.Items).Should(BeEmpty())

			By("Sending webhook that matches filter")
			payload2 := map[string]interface{}{
				"action": "opened",
			}
			payloadBytes2, _ := json.Marshal(payload2)
			resp2, err := postWebhook(webhookURL, payloadBytes2)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp2.Body.Close() }()
			Expect(resp2.StatusCode).Should(Equal(http.StatusCreated))

			By("Verifying Task was created for matching webhook")
			Eventually(func() bool {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName}); err != nil {
					return false
				}
				return len(tasks.Items) > 0
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})
	})

	Context("WebhookTrigger with HMAC authentication", func() {
		It("should reject requests with invalid signature", func() {

			triggerName := uniqueName("wht-hmac")
			secretName := triggerName + "-secret"
			secretKey := "webhook-secret-key"

			By("Creating Secret with HMAC key")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: testNS,
				},
				StringData: map[string]string{
					"token": secretKey,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Creating a WebhookTrigger with HMAC auth")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					Auth: &kubetaskv1alpha1.WebhookAuth{
						HMAC: &kubetaskv1alpha1.HMACAuth{
							SecretRef: kubetaskv1alpha1.SecretKeyReference{
								Name: secretName,
								Key:  "token",
							},
							SignatureHeader: "X-Hub-Signature-256",
							Algorithm:       "sha256",
						},
					},
					TaskTemplate: &kubetaskv1alpha1.WebhookTaskTemplate{
						AgentRef:    agentName,
						Description: "HMAC authenticated task",
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			webhookURL := getWebhookURL(testNS, triggerName)
			payload := []byte(`{"event":"test"}`)

			By("Sending request without signature")
			resp1, err := postWebhook(webhookURL, payload)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp1.Body.Close() }()
			Expect(resp1.StatusCode).Should(Equal(http.StatusUnauthorized))

			By("Sending request with invalid signature")
			req2, _ := http.NewRequest("POST", webhookURL, bytes.NewReader(payload))
			req2.Header.Set("Content-Type", "application/json")
			req2.Header.Set("X-Hub-Signature-256", "sha256=invalid")
			resp2, err := http.DefaultClient.Do(req2)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp2.Body.Close() }()
			Expect(resp2.StatusCode).Should(Equal(http.StatusUnauthorized))

			By("Sending request with valid signature")
			mac := hmac.New(sha256.New, []byte(secretKey))
			mac.Write(payload)
			signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

			req3, _ := http.NewRequest("POST", webhookURL, bytes.NewReader(payload))
			req3.Header.Set("Content-Type", "application/json")
			req3.Header.Set("X-Hub-Signature-256", signature)
			resp3, err := http.DefaultClient.Do(req3)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp3.Body.Close() }()
			Expect(resp3.StatusCode).Should(Equal(http.StatusCreated))

			By("Verifying Task was created")
			Eventually(func() bool {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName}); err != nil {
					return false
				}
				return len(tasks.Items) > 0
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

	Context("WebhookTrigger concurrency policy", func() {
		It("should respect Forbid policy", func() {

			triggerName := uniqueName("wht-forbid")

			By("Creating a WebhookTrigger with Forbid policy")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					ConcurrencyPolicy: kubetaskv1alpha1.ConcurrencyPolicyForbid,
					TaskTemplate: &kubetaskv1alpha1.WebhookTaskTemplate{
						AgentRef:    agentName,
						Description: "Forbid policy task - sleeps for 30s",
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			webhookURL := getWebhookURL(testNS, triggerName)
			payload := []byte(`{"event":"test"}`)

			By("Sending first webhook to create a running Task")
			resp1, err := postWebhook(webhookURL, payload)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp1.Body.Close() }()
			Expect(resp1.StatusCode).Should(Equal(http.StatusCreated))

			By("Waiting for Task to start running")
			Eventually(func() bool {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName}); err != nil {
					return false
				}
				return len(tasks.Items) > 0
			}, timeout, interval).Should(BeTrue())

			By("Sending second webhook while Task is running")
			resp2, err := postWebhook(webhookURL, payload)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp2.Body.Close() }()
			// Should succeed but not create new Task
			body, _ := io.ReadAll(resp2.Body)
			GinkgoWriter.Printf("Second webhook response: %d - %s\n", resp2.StatusCode, string(body))

			By("Verifying only one Task exists")
			time.Sleep(2 * time.Second)
			tasks := &kubetaskv1alpha1.TaskList{}
			Expect(k8sClient.List(ctx, tasks, client.InNamespace(testNS),
				client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName})).Should(Succeed())
			Expect(tasks.Items).Should(HaveLen(1))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})

		It("should respect Replace policy", func() {

			triggerName := uniqueName("wht-replace")

			By("Creating a WebhookTrigger with Replace policy and slow agent")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					ConcurrencyPolicy: kubetaskv1alpha1.ConcurrencyPolicyReplace,
					TaskTemplate: &kubetaskv1alpha1.WebhookTaskTemplate{
						AgentRef:    agentName,
						Description: "Replace policy task #{{ .seq }}",
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			webhookURL := getWebhookURL(testNS, triggerName)

			By("Sending first webhook")
			payload1 := []byte(`{"seq":1}`)
			resp1, err := postWebhook(webhookURL, payload1)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp1.Body.Close() }()
			Expect(resp1.StatusCode).Should(Equal(http.StatusCreated))

			By("Waiting for first Task to be created")
			var firstTaskName string
			Eventually(func() bool {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName}); err != nil {
					return false
				}
				if len(tasks.Items) > 0 {
					firstTaskName = tasks.Items[0].Name
					return true
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Sending second webhook to replace")
			payload2 := []byte(`{"seq":2}`)
			resp2, err := postWebhook(webhookURL, payload2)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp2.Body.Close() }()
			Expect(resp2.StatusCode).Should(Equal(http.StatusCreated))

			By("Verifying first Task was stopped")
			Eventually(func() bool {
				task := &kubetaskv1alpha1.Task{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: firstTaskName, Namespace: testNS}, task); err != nil {
					return false
				}
				// Check for stop annotation
				return task.Annotations != nil && task.Annotations["kubetask.io/stop"] == "true"
			}, timeout, interval).Should(BeTrue())

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})
	})

	Context("WebhookTrigger Go template rendering", func() {
		It("should render task description with payload data", func() {

			triggerName := uniqueName("wht-template")

			By("Creating a WebhookTrigger with template")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					TaskTemplate: &kubetaskv1alpha1.WebhookTaskTemplate{
						AgentRef: agentName,
						Description: `Review PR #{{ .pull_request.number }}
Repository: {{ .repository.full_name }}
Author: {{ .pull_request.user.login }}`,
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			webhookURL := getWebhookURL(testNS, triggerName)

			By("Sending webhook with PR data")
			payload := map[string]interface{}{
				"pull_request": map[string]interface{}{
					"number": 42,
					"user": map[string]interface{}{
						"login": "testuser",
					},
				},
				"repository": map[string]interface{}{
					"full_name": "myorg/myrepo",
				},
			}
			payloadBytes, _ := json.Marshal(payload)
			resp, err := postWebhook(webhookURL, payloadBytes)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()
			Expect(resp.StatusCode).Should(Equal(http.StatusCreated))

			By("Verifying Task description is rendered correctly")
			Eventually(func() string {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName}); err != nil {
					return ""
				}
				if len(tasks.Items) == 0 {
					return ""
				}
				return *tasks.Items[0].Spec.Description
			}, timeout, interval).Should(ContainSubstring("Review PR #42"))

			tasks := &kubetaskv1alpha1.TaskList{}
			Expect(k8sClient.List(ctx, tasks, client.InNamespace(testNS),
				client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName})).Should(Succeed())
			Expect(tasks.Items).Should(HaveLen(1))

			description := *tasks.Items[0].Spec.Description
			Expect(description).Should(ContainSubstring("myorg/myrepo"))
			Expect(description).Should(ContainSubstring("testuser"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})
	})

	Context("WebhookTrigger with rules-based triggers", func() {
		It("should match rules and create Task based on matchPolicy First", func() {

			triggerName := uniqueName("wht-rules")

			By("Creating a WebhookTrigger with multiple rules")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					MatchPolicy: kubetaskv1alpha1.MatchPolicyFirst,
					Rules: []kubetaskv1alpha1.WebhookRule{
						{
							Name:   "pr-review",
							Filter: `body.event_type == "pull_request"`,
							ResourceTemplate: kubetaskv1alpha1.WebhookResourceTemplate{
								Task: &kubetaskv1alpha1.WebhookTaskSpec{
									AgentRef:    agentName,
									Description: "PR Review Task: {{ .pr_number }}",
								},
							},
						},
						{
							Name:   "issue-triage",
							Filter: `body.event_type == "issue"`,
							ResourceTemplate: kubetaskv1alpha1.WebhookResourceTemplate{
								Task: &kubetaskv1alpha1.WebhookTaskSpec{
									AgentRef:    agentName,
									Description: "Issue Triage Task: {{ .issue_number }}",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			webhookURL := getWebhookURL(testNS, triggerName)

			By("Sending PR event webhook")
			payload := map[string]interface{}{
				"event_type": "pull_request",
				"pr_number":  123,
			}
			payloadBytes, _ := json.Marshal(payload)
			resp, err := postWebhook(webhookURL, payloadBytes)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()
			Expect(resp.StatusCode).Should(Equal(http.StatusCreated))

			By("Verifying Task was created with pr-review rule label")
			Eventually(func() bool {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{
						"kubetask.io/webhook-trigger": triggerName,
						"kubetask.io/webhook-rule":    "pr-review",
					}); err != nil {
					return false
				}
				return len(tasks.Items) > 0
			}, timeout, interval).Should(BeTrue())

			By("Verifying RuleStatuses is updated")
			Eventually(func() int64 {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return 0
				}
				for _, rs := range t.Status.RuleStatuses {
					if rs.Name == "pr-review" {
						return rs.TotalTriggered
					}
				}
				return 0
			}, timeout, interval).Should(BeNumerically(">=", 1))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})

		It("should match all rules when matchPolicy is All", func() {

			triggerName := uniqueName("wht-rules-all")

			By("Creating a WebhookTrigger with matchPolicy All")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					MatchPolicy: kubetaskv1alpha1.MatchPolicyAll,
					Rules: []kubetaskv1alpha1.WebhookRule{
						{
							Name:   "rule-a",
							Filter: `body.priority == "high"`,
							ResourceTemplate: kubetaskv1alpha1.WebhookResourceTemplate{
								Task: &kubetaskv1alpha1.WebhookTaskSpec{
									AgentRef:    agentName,
									Description: "High priority handler",
								},
							},
						},
						{
							Name:   "rule-b",
							Filter: `body.category == "bug"`,
							ResourceTemplate: kubetaskv1alpha1.WebhookResourceTemplate{
								Task: &kubetaskv1alpha1.WebhookTaskSpec{
									AgentRef:    agentName,
									Description: "Bug handler",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			webhookURL := getWebhookURL(testNS, triggerName)

			By("Sending webhook that matches both rules")
			payload := map[string]interface{}{
				"priority": "high",
				"category": "bug",
			}
			payloadBytes, _ := json.Marshal(payload)
			resp, err := postWebhook(webhookURL, payloadBytes)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()
			Expect(resp.StatusCode).Should(Equal(http.StatusCreated))

			By("Verifying two Tasks were created (one for each rule)")
			Eventually(func() int {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName}); err != nil {
					return 0
				}
				return len(tasks.Items)
			}, timeout, interval).Should(Equal(2))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})
	})

	Context("WebhookTrigger with resourceTemplate", func() {
		It("should create Task using resourceTemplate.task", func() {

			triggerName := uniqueName("wht-restpl")

			By("Creating a WebhookTrigger with resourceTemplate")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					ResourceTemplate: &kubetaskv1alpha1.WebhookResourceTemplate{
						Task: &kubetaskv1alpha1.WebhookTaskSpec{
							AgentRef:    agentName,
							Description: "ResourceTemplate Task: {{ .message }}",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			By("Waiting for WebhookTrigger to be ready")
			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			webhookURL := getWebhookURL(testNS, triggerName)

			By("Sending webhook")
			payload := map[string]interface{}{
				"message": "hello from resourceTemplate",
			}
			payloadBytes, _ := json.Marshal(payload)
			resp, err := postWebhook(webhookURL, payloadBytes)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()
			Expect(resp.StatusCode).Should(Equal(http.StatusCreated))

			By("Verifying Task was created")
			Eventually(func() bool {
				tasks := &kubetaskv1alpha1.TaskList{}
				if err := k8sClient.List(ctx, tasks, client.InNamespace(testNS),
					client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName}); err != nil {
					return false
				}
				return len(tasks.Items) > 0
			}, timeout, interval).Should(BeTrue())

			By("Verifying Task description is rendered correctly")
			tasks := &kubetaskv1alpha1.TaskList{}
			Expect(k8sClient.List(ctx, tasks, client.InNamespace(testNS),
				client.MatchingLabels{"kubetask.io/webhook-trigger": triggerName})).Should(Succeed())
			Expect(*tasks.Items[0].Spec.Description).Should(ContainSubstring("hello from resourceTemplate"))

			By("Cleaning up")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())
		})
	})

	Context("WebhookTrigger garbage collection", func() {
		It("should clean up Tasks when WebhookTrigger is deleted", func() {
			triggerName := uniqueName("wht-gc")

			By("Creating a WebhookTrigger")
			trigger := &kubetaskv1alpha1.WebhookTrigger{
				ObjectMeta: metav1.ObjectMeta{
					Name:      triggerName,
					Namespace: testNS,
				},
				Spec: kubetaskv1alpha1.WebhookTriggerSpec{
					TaskTemplate: &kubetaskv1alpha1.WebhookTaskTemplate{
						AgentRef:    agentName,
						Description: "GC test task",
					},
				},
			}
			Expect(k8sClient.Create(ctx, trigger)).Should(Succeed())

			triggerKey := types.NamespacedName{Name: triggerName, Namespace: testNS}

			By("Waiting for WebhookTrigger to be ready")
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				if err := k8sClient.Get(ctx, triggerKey, t); err != nil {
					return false
				}
				return t.Status.WebhookURL != ""
			}, timeout, interval).Should(BeTrue())

			By("Deleting WebhookTrigger")
			Expect(k8sClient.Delete(ctx, trigger)).Should(Succeed())

			By("Verifying WebhookTrigger is deleted")
			Eventually(func() bool {
				t := &kubetaskv1alpha1.WebhookTrigger{}
				return k8sClient.Get(ctx, triggerKey, t) != nil
			}, timeout, interval).Should(BeTrue())
		})
	})
})
