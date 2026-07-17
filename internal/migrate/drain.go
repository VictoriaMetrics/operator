package migrate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// Polling interval/timeout are vars, not consts, so tests can shrink them.
var (
	DrainPollInterval = 5 * time.Second
	DrainTimeout      = 6 * time.Hour

	EndpointsReadyPollInterval = 2 * time.Second
	EndpointsReadyTimeout      = 2 * time.Minute

	WriteQuiesceGracePeriod = 5 * time.Second
)

// WaitEndpointsReady polls until serviceName has at least one ready endpoint address behind
// portName. A CR reporting Operational only guarantees its pods are Ready, not that the
// EndpointSlice controller has reflected that yet, so a Service redirected to it immediately
// after can still briefly have zero ready endpoints.
func WaitEndpointsReady(ctx context.Context, c client.Client, namespace, serviceName, portName string) error {
	err := wait.PollUntilContextTimeout(ctx, EndpointsReadyPollInterval, EndpointsReadyTimeout, true, func(ctx context.Context) (bool, error) {
		addrs, err := podutil.DiscoverEndpointAddrs(ctx, c, namespace, serviceName, portName, "http", "/")
		if err != nil {
			return false, err
		}
		return len(addrs) > 0, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for %s/%s to have a ready endpoint: %w", namespace, serviceName, err)
	}
	return nil
}

// queueMetricsSource is the shape shared by VMAgent and VLAgent needed to scrape their own
// /metrics endpoint.
type queueMetricsSource interface {
	GetName() string
	GetNamespace() string
	PrefixedName() string
	ProbeScheme() string
	GetMetricsPath() string
}

// WaitQueueDrained polls agent's persistent-queue metric across all its pods until every
// pending-bytes value is at or below thresholdBytes.
func WaitQueueDrained(ctx context.Context, c client.Client, httpClient *http.Client, agent queueMetricsSource, metricName string, thresholdBytes float64) error {
	var metricQueries = []podutil.MetricQuery{
		{Name: metricName, Dimension: "url"},
	}
	err := wait.PollUntilContextTimeout(ctx, DrainPollInterval, DrainTimeout, true, func(ctx context.Context) (bool, error) {
		addrs, err := podutil.DiscoverEndpointAddrs(ctx, c, agent.GetNamespace(), agent.PrefixedName(), "http", agent.ProbeScheme(), agent.GetMetricsPath())
		if err != nil {
			return false, err
		}
		if len(addrs) == 0 {
			// transient: agent pods may not be Ready yet, keep polling.
			return false, nil
		}
		for addr := range addrs {
			metrics, err := podutil.FetchMetricsValues(ctx, httpClient, addr, metricQueries)
			if err != nil {
				var urlErr *url.Error
				if errors.As(err, &urlErr) {
					// transient: connection-level failure, agent may not be accepting
					// connections yet.
					return false, nil
				}
				return false, fmt.Errorf("scraping %s: %w", addr, err)
			}
			samples := metrics[metricName]
			if len(samples) == 0 {
				return false, nil
			}
			for _, v := range samples {
				if v > thresholdBytes {
					return false, nil
				}
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for %s/%s persistent queue to drain: %w", agent.GetNamespace(), agent.GetName(), err)
	}
	return nil
}
