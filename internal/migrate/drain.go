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
	DrainPollInterval  = 5 * time.Second
	DrainTimeout       = 6 * time.Hour
	ScrapeTimeout      = 30 * time.Second
	DrainConfirmations = 3

	EndpointsReadyPollInterval = 2 * time.Second
	EndpointsReadyTimeout      = 2 * time.Minute

	WriteQuiesceGracePeriod = 5 * time.Second
)

// waitEndpointsReady polls until serviceName has at least one ready endpoint behind its "http" port.
func waitEndpointsReady(ctx context.Context, c client.Client, namespace, serviceName string) error {
	err := wait.PollUntilContextTimeout(ctx, EndpointsReadyPollInterval, EndpointsReadyTimeout, true, func(ctx context.Context) (bool, error) {
		addrs, err := podutil.DiscoverEndpointAddrs(ctx, c, namespace, serviceName, "http", "http", "/")
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

// waitQueueDrained polls agent's persistent-queue metric across all its pods until every
// pending-bytes value is zero.
func waitQueueDrained(ctx context.Context, c client.Client, httpClient *http.Client, agent queueMetricsSource, metricName string) error {
	var metricQueries = []podutil.MetricQuery{
		{Name: metricName, Dimension: "url"},
	}
	consecutiveTransientFailures := 0
	consecutiveDrained := 0
	err := wait.PollUntilContextTimeout(ctx, DrainPollInterval, DrainTimeout, true, func(ctx context.Context) (bool, error) {
		addrs, err := podutil.DiscoverEndpointAddrs(ctx, c, agent.GetNamespace(), agent.PrefixedName(), "http", agent.ProbeScheme(), agent.GetMetricsPath())
		if err != nil {
			return false, err
		}
		if len(addrs) == 0 {
			// transient: agent pods may not be Ready yet, keep polling.
			consecutiveDrained = 0
			return false, nil
		}
		drained := true
		for addr := range addrs {
			scrapeCtx, cancel := context.WithTimeout(ctx, ScrapeTimeout)
			metrics, err := podutil.FetchMetricsValues(scrapeCtx, httpClient, addr, metricQueries)
			cancel()
			if err != nil {
				var urlErr *url.Error
				var statusErr *podutil.StatusError
				transient := errors.As(err, &urlErr) || (errors.As(err, &statusErr) && retryableScrapeStatus(statusErr.StatusCode))
				if !transient {
					return false, fmt.Errorf("scraping %s: %w", addr, err)
				}
				consecutiveTransientFailures++
				if consecutiveTransientFailures%30 == 1 {
					fmt.Printf("still waiting for %s to accept scrapes (%d attempts so far): %v\n", addr, consecutiveTransientFailures, err)
				}
				consecutiveDrained = 0
				return false, nil
			}
			consecutiveTransientFailures = 0
			samples := metrics[metricName]
			if len(samples) == 0 {
				drained = false
				continue
			}
			for _, v := range samples {
				if v != 0 {
					drained = false
				}
			}
		}
		if !drained {
			consecutiveDrained = 0
			return false, nil
		}
		consecutiveDrained++
		return consecutiveDrained >= DrainConfirmations, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for %s/%s persistent queue to drain: %w", agent.GetNamespace(), agent.GetName(), err)
	}
	return nil
}

func retryableScrapeStatus(code int) bool {
	if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code <= 599
}
