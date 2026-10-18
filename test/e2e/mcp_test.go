package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

// callMCPTool opens a session with the MCP server over Streamable HTTP transport via Kubernetes API server proxy,
// calls the given tool and returns the text content of its result
func callMCPTool(ctx context.Context, svcURL, tool string, args map[string]any) (string, error) {
	proxyURL, err := buildServiceProxyURL(&k8sCfg, svcURL+"/mcp")
	if err != nil {
		return "", err
	}
	hc, err := rest.HTTPClientFor(&k8sCfg)
	if err != nil {
		return "", err
	}
	hc.Timeout = 10 * time.Second
	post := func(sessionID string, msg map[string]any) (http.Header, []byte, error) {
		data, err := json.Marshal(msg)
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL, bytes.NewReader(data))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if sessionID != "" {
			req.Header.Set("Mcp-Session-Id", sessionID)
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("unexpected status code for %s request: %d, body: %s", msg["method"], resp.StatusCode, body)
		}
		return resp.Header, body, nil
	}

	headers, _, err := post("", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "vm-operator-e2e", "version": "v1"},
		},
	})
	if err != nil {
		return "", err
	}
	sessionID := headers.Get("Mcp-Session-Id")
	if sessionID == "" {
		return "", fmt.Errorf("MCP server didn't return session id")
	}
	_, body, err := post(sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": args},
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("cannot parse tools/call response %q: %w", body, err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("tools/call failed: %s", resp.Error.Message)
	}
	var texts []string
	for _, c := range resp.Result.Content {
		texts = append(texts, c.Text)
	}
	text := strings.Join(texts, "\n")
	if resp.Result.IsError {
		return "", fmt.Errorf("tool %q returned error: %s", tool, text)
	}
	return text, nil
}

// expectMCPChildObjectsDeleted checks that objects created for the MCP server are garbage collected after CR removal
func expectMCPChildObjectsDeleted(ctx context.Context, nsn types.NamespacedName) {
	GinkgoHelper()
	Eventually(func() error {
		for _, obj := range []client.Object{&appsv1.Deployment{}, &corev1.Service{}} {
			if err := k8sClient.Get(ctx, nsn, obj); !k8serrors.IsNotFound(err) {
				return fmt.Errorf("%T %s must be deleted, got err: %v", obj, nsn, err)
			}
		}
		return nil
	}, eventualDeletionTimeout).ShouldNot(HaveOccurred())
}

//nolint:dupl,lll
var _ = Describe("test mcp Controllers", Label("mcp"), func() {
	var ctx context.Context
	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	// the documentation tool loads an index of the built-in documentation at start,
	// which slows down the MCP server start and takes most of its memory, so it's disabled in tests
	disabledTools := []string{"documentation"}
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("32Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should run VMMCP for VMSingle", Label("vm", "vmmcp"), func() {
		backend := &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "mcp-backend"},
			Spec: vmv1beta1.VMSingleSpec{
				RetentionPeriod: "1",
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
		}
		backendNSN := types.NamespacedName{Namespace: namespace, Name: backend.Name}
		DeferCleanup(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, backend)).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, backendNSN, &vmv1beta1.VMSingleList{})
		})
		expectStatusAfterAction(ctx, &vmv1beta1.VMSingleList{}, backendNSN, eventualDeploymentAppReadyTimeout, func() {
			Expect(k8sClient.Create(ctx, backend)).ToNot(HaveOccurred())
		}, vmv1beta1.UpdateStatusOperational)

		cr := &vmv1.VMMCP{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "metrics"},
			Spec: vmv1.VMMCPSpec{
				Entrypoint:   backend.AsURL(false),
				InstanceType: vmv1.VMMCPInstanceTypeSingle,
				CommonMCPSpec: vmv1.CommonMCPSpec{
					DisabledTools: disabledTools,
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						UseStrictSecurity: ptr.To(true),
						Resources:         resources,
					},
				},
			},
		}
		nsn := types.NamespacedName{Namespace: namespace, Name: cr.Name}
		DeferCleanup(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, nsn, &vmv1.VMMCPList{})
		})
		expectStatusAfterAction(ctx, &vmv1.VMMCPList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
		}, vmv1beta1.UpdateStatusOperational)

		childNSN := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, childNSN, &dep)).ToNot(HaveOccurred())
		Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(dep.Spec.Template.Spec.Containers[0].SecurityContext).NotTo(BeNil())
		Expect(dep.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).To(Equal(ptr.To(true)))
		Expect(k8sClient.Get(ctx, childNSN, &vmv1beta1.VMServiceScrape{})).ToNot(HaveOccurred())

		By("querying VMSingle with MCP query tool")
		Eventually(func() (string, error) {
			return callMCPTool(ctx, cr.AsURL(false), "query", map[string]any{"query": "vm_app_version"})
		}, eventualDeploymentAppReadyTimeout).Should(ContainSubstring(`"status":"success"`))

		By("updating MCP server settings")
		expectStatusAfterAction(ctx, &vmv1.VMMCPList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
			Eventually(func() error {
				var toUpdate vmv1.VMMCP
				if err := k8sClient.Get(ctx, nsn, &toUpdate); err != nil {
					return err
				}
				toUpdate.Spec.LogLevel = "debug"
				toUpdate.Spec.PodDisruptionBudget = &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
					MaxUnavailable: ptr.To(intstr.FromInt32(1)),
				}
				return k8sClient.Update(ctx, &toUpdate)
			}, eventualDeploymentAppReadyTimeout).ShouldNot(HaveOccurred())
		}, vmv1beta1.UpdateStatusOperational)
		Expect(k8sClient.Get(ctx, childNSN, &dep)).ToNot(HaveOccurred())
		Expect(dep.Spec.Template.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{Name: "MCP_LOG_LEVEL", Value: "debug"}))
		Expect(k8sClient.Get(ctx, childNSN, &policyv1.PodDisruptionBudget{})).ToNot(HaveOccurred())

		By("deleting VMMCP")
		Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
		waitResourceDeleted(ctx, nsn, &vmv1.VMMCPList{})
		expectMCPChildObjectsDeleted(ctx, childNSN)
	})

})
