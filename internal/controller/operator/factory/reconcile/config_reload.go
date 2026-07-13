package reconcile

import (
	"context"
	"fmt"
	"hash/crc32"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// configReloadWaitable is implemented by CRD types that support verifying a config reload was
// actually picked up by every replica, via their config-reloader sidecar.
type configReloadWaitable interface {
	client.Object
	SelectorLabels() map[string]string
}

var (
	configReloadWaitInterval = 2 * time.Second
	configReloadWaitTimeout  = 60 * time.Second
	configReloadHTTPTimeout  = 5 * time.Second
	configReloaderMetricsURL = func(podIP string) string {
		return fmt.Sprintf("http://%s:%d/metrics", podIP, build.ConfigReloaderDefaultPort)
	}
)

const (
	contentHashMetricName = "configreloader_reload_content_hash"
	mainContentHashKey    = "main"
)

// WaitForConfigReloadHash verifies that every ready replica's config-reloader sidecar has
// confirmed applying content matching hash, an exact CRC32 match rather than a wall-clock
// heuristic. Skipped rather than blocking if a pod's sidecar predates this metric.
func WaitForConfigReloadHash(ctx context.Context, rclient client.Client, cr configReloadWaitable, hash uint32) error {
	httpClient := &http.Client{Timeout: configReloadHTTPTimeout}
	selector := labels.SelectorFromSet(cr.SelectorLabels())
	listOpts := &client.ListOptions{
		LabelSelector: selector,
		Namespace:     cr.GetNamespace(),
	}

	var podList corev1.PodList
	if err := rclient.List(ctx, &podList, listOpts); err != nil {
		return fmt.Errorf("cannot list pods for %s/%s: %w", cr.GetNamespace(), cr.GetName(), err)
	}
	if len(podList.Items) == 0 {
		return nil
	}

	err := wait.PollUntilContextTimeout(ctx, configReloadWaitInterval, configReloadWaitTimeout, true, func(ctx context.Context) (bool, error) {
		var podList corev1.PodList
		if err := rclient.List(ctx, &podList, listOpts); err != nil {
			return false, fmt.Errorf("cannot list pods for %s/%s: %w", cr.GetNamespace(), cr.GetName(), err)
		}
		if len(podList.Items) == 0 {
			return false, nil
		}
		for _, pod := range podList.Items {
			if !pod.DeletionTimestamp.IsZero() || !PodIsReady(&pod, 0) || pod.Status.PodIP == "" {
				return false, nil
			}
			got, found, err := scrapeContentHash(ctx, httpClient, pod.Status.PodIP)
			if err != nil {
				// transient scrape failure - keep polling rather than failing the wait outright
				return false, nil
			}
			if found && got != hash {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("timed out waiting for %s/%s to reload config: %w", cr.GetNamespace(), cr.GetName(), err)
	}
	return nil
}

// HashBytes matches the hash a config-reloader sidecar computes for its watched content.
func HashBytes(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

// scrapeContentHash fetches the config-reloader sidecar's own metrics endpoint on podIP and
// returns the confirmed content hash it currently exposes, if any (a sidecar predating this
// metric reports nothing, distinguished from a genuine mismatch via the found return value).
func scrapeContentHash(ctx context.Context, httpClient *http.Client, podIP string) (uint32, bool, error) {
	values, err := podutil.FetchMetricsValues(ctx, httpClient, configReloaderMetricsURL(podIP), []podutil.MetricQuery{
		{Name: contentHashMetricName, Dimension: "key"},
	})
	if err != nil {
		return 0, false, err
	}
	v, found := values[contentHashMetricName][mainContentHashKey]
	return uint32(v), found, nil
}
