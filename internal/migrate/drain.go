package migrate

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// Polling interval/timeout are vars, not consts, so tests can shrink them.
var (
	DrainPollInterval = 5 * time.Second
	DrainTimeout      = 6 * time.Hour
)

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
	err := wait.PollUntilContextTimeout(ctx, DrainPollInterval, DrainTimeout, true, func(ctx context.Context) (bool, error) {
		addrs, err := podutil.DiscoverEndpointAddrs(ctx, c, agent.GetNamespace(), agent.PrefixedName(), "http", agent.ProbeScheme(), agent.GetMetricsPath())
		if err != nil || len(addrs) == 0 {
			// transient: agent pods may not be Ready yet, keep polling.
			return false, nil
		}
		for addr := range addrs {
			values, err := podutil.FetchMetricValues(ctx, httpClient, addr, metricName, "path")
			if err != nil {
				return false, nil
			}
			for _, v := range values {
				if v > thresholdBytes {
					return false, nil
				}
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("timed out waiting for %s/%s persistent queue to drain: %w", agent.GetNamespace(), agent.GetName(), err)
	}
	return nil
}
