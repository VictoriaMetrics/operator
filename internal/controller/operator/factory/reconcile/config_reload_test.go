package reconcile

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestConfigReloaderMetricsURL(t *testing.T) {
	assert.Equal(t, fmt.Sprintf("http://10.0.0.1:%d/metrics", build.ConfigReloaderDefaultPort), configReloaderMetricsURL("10.0.0.1", build.ConfigReloaderDefaultPort))
	// IPv6 must be bracketed, otherwise the port separator is ambiguous with the address itself
	assert.Equal(t, fmt.Sprintf("http://[2001:db8::1]:%d/metrics", build.ConfigReloaderDefaultPort), configReloaderMetricsURL("2001:db8::1", build.ConfigReloaderDefaultPort))
	assert.Equal(t, "http://10.0.0.1:8436/metrics", configReloaderMetricsURL("10.0.0.1", 8436))
}

func TestConfigReloaderPortFromPod(t *testing.T) {
	assert.Equal(t, build.ConfigReloaderDefaultPort, configReloaderPortFromPod(&corev1.Pod{}))

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "config-reloader",
				Ports: []corev1.ContainerPort{{
					Name:          build.ConfigReloaderPortName,
					ContainerPort: 8436,
				}},
			}},
		},
	}
	assert.Equal(t, 8436, configReloaderPortFromPod(pod))
}

func TestWaitForConfigReloadHash_NoPodsIsNoop(t *testing.T) {
	cr := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{Name: "vmauth", Namespace: "default"},
	}
	fclient := k8stools.GetTestClientWithObjects(nil)

	start := time.Now()
	err := WaitForConfigReloadHash(context.Background(), fclient, cr, 42)
	assert.NoError(t, err)
	assert.Less(t, time.Since(start), configReloadWaitInterval, "must return immediately without polling when no pods exist yet")
}

func readyPod(name, namespace, ip string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "config-reloader",
				Ports: []corev1.ContainerPort{{
					Name:          build.ConfigReloaderPortName,
					ContainerPort: int32(build.ConfigReloaderDefaultPort),
				}},
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: ip,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// withConfigReloaderMetricsResponse stubs the sidecar /metrics HTTP response in-process
// (avoids httptest, which is unreliable when loopback is broken in the test environment).
func withConfigReloaderMetricsResponse(t *testing.T, body string) {
	t.Helper()
	origURL := configReloaderMetricsURL
	origClient := configReloadNewHTTPClient
	configReloaderMetricsURL = func(string, int) string { return "http://config-reloader.test/metrics" }
	configReloadNewHTTPClient = func() *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		}
	}
	t.Cleanup(func() {
		configReloaderMetricsURL = origURL
		configReloadNewHTTPClient = origClient
	})
}

func TestWaitForConfigReloadHash(t *testing.T) {
	cr := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{Name: "vmauth", Namespace: "default"},
	}
	sel := cr.SelectorLabels()
	pod := readyPod("vmauth-0", cr.Namespace, "10.0.0.1", sel)

	// exact hash match succeeds immediately.
	t.Run("match succeeds", func(t *testing.T) {
		withConfigReloaderMetricsResponse(t, fmt.Sprintf("configreloader_reload_content_hash{key=\"main\"} %d\n", 42))

		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{pod})
		start := time.Now()
		assert.NoError(t, WaitForConfigReloadHash(context.Background(), fclient, cr, 42))
		assert.Less(t, time.Since(start), configReloadWaitInterval)
	})

	// metric absent entirely (sidecar predates it) - skipped rather than blocking.
	t.Run("absent metric is skipped", func(t *testing.T) {
		withConfigReloaderMetricsResponse(t, "")

		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{pod})
		start := time.Now()
		assert.NoError(t, WaitForConfigReloadHash(context.Background(), fclient, cr, 42))
		assert.Less(t, time.Since(start), configReloadWaitInterval)
	})

	// mismatched hash never satisfies the wait and times out.
	t.Run("mismatch times out", func(t *testing.T) {
		origInterval, origTimeout := configReloadWaitInterval, configReloadWaitTimeout
		configReloadWaitInterval = 5 * time.Millisecond
		configReloadWaitTimeout = 20 * time.Millisecond
		defer func() { configReloadWaitInterval, configReloadWaitTimeout = origInterval, origTimeout }()

		withConfigReloaderMetricsResponse(t, fmt.Sprintf("configreloader_reload_content_hash{key=\"main\"} %d\n", 99))

		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{pod})
		assert.Error(t, WaitForConfigReloadHash(context.Background(), fclient, cr, 42))
	})
}
