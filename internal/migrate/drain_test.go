package migrate

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

// TestWaitQueueDrained runs under synctest with the package's real (not shrunk) poll
// interval/timeout: synctest's virtual clock fast-forwards through blocked timers, so the test
// completes quickly in wall-clock time without mutating shared package-level vars that other
// tests in the package would otherwise see leak across test boundaries.
func TestWaitQueueDrained(t *testing.T) {
	f := func(t *testing.T, body string, wantErr bool) {
		t.Helper()
		synctest.Test(t, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintln(w, body)
			})

			agent := &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "buffer", Namespace: "default"},
			}
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "buffer-endpoints",
					Namespace: agent.Namespace,
					Labels:    map[string]string{discoveryv1.LabelServiceName: agent.PrefixedName()},
				},
				Endpoints: []discoveryv1.Endpoint{
					{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
				},
				Ports: []discoveryv1.EndpointPort{
					{Name: ptr.To("http"), Port: ptr.To(int32(8429))},
				},
			}
			c := k8stools.GetTestClientWithObjects([]runtime.Object{endpointSlice})

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			err := waitQueueDrained(ctx, c, handlerClient(handler), agent, vmv1beta1.VMAgentQueueMetricName, 0)
			if wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("drained", func(t *testing.T) {
		f(t, fmt.Sprintf(`%s{url="/tmp/q"} 0`, vmv1beta1.VMAgentQueueMetricName), false)
	})
	t.Run("not drained yet", func(t *testing.T) {
		f(t, fmt.Sprintf(`%s{url="/tmp/q"} 1024`, vmv1beta1.VMAgentQueueMetricName), true)
	})
	t.Run("metric absent from scrape is not treated as drained", func(t *testing.T) {
		f(t, `some_other_metric 0`, true)
	})
}
