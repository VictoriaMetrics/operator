package reconcile

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

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
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: ip,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

func withConfigReloaderMetricsURL(t *testing.T, ts *httptest.Server) {
	t.Helper()
	orig := configReloaderMetricsURL
	u, err := url.Parse(ts.URL)
	assert.NoError(t, err)
	configReloaderMetricsURL = func(string) string { return "http://" + u.Host + "/metrics" }
	t.Cleanup(func() { configReloaderMetricsURL = orig })
}

func TestWaitForConfigReloadHash(t *testing.T) {
	cr := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{Name: "vmauth", Namespace: "default"},
	}
	labels := cr.SelectorLabels()
	pod := readyPod("vmauth-0", cr.Namespace, "10.0.0.1", labels)

	// exact hash match succeeds immediately.
	t.Run("match succeeds", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, "configreloader_reload_content_hash{key=\"main\"} %d\n", 42)
		}))
		defer ts.Close()
		withConfigReloaderMetricsURL(t, ts)

		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{pod})
		start := time.Now()
		assert.NoError(t, WaitForConfigReloadHash(context.Background(), fclient, cr, 42))
		assert.Less(t, time.Since(start), configReloadWaitInterval)
	})

	// metric absent entirely (sidecar predates it) - skipped rather than blocking.
	t.Run("absent metric is skipped", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		defer ts.Close()
		withConfigReloaderMetricsURL(t, ts)

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

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, "configreloader_reload_content_hash{key=\"main\"} %d\n", 99)
		}))
		defer ts.Close()
		withConfigReloaderMetricsURL(t, ts)

		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{pod})
		assert.Error(t, WaitForConfigReloadHash(context.Background(), fclient, cr, 42))
	})
}
