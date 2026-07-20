package migrate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestWaitQueueDrained(t *testing.T) {
	f := func(t *testing.T, body string, wantErr bool) {
		t.Helper()
		DrainPollInterval = 20 * time.Millisecond
		DrainTimeout = 500 * time.Millisecond

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, body)
		}))
		defer ts.Close()
		tsURL, err := url.Parse(ts.URL)
		require.NoError(t, err)
		tsHost, tsPortStr, err := net.SplitHostPort(tsURL.Host)
		require.NoError(t, err)
		tsPort, err := strconv.ParseInt(tsPortStr, 10, 32)
		require.NoError(t, err)

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
				{Addresses: []string{tsHost}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
			},
			Ports: []discoveryv1.EndpointPort{
				{Name: ptr.To("http"), Port: ptr.To(int32(tsPort))},
			},
		}
		c := k8stools.GetTestClientWithObjects([]runtime.Object{endpointSlice})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err = WaitQueueDrained(ctx, c, ts.Client(), agent, "vm_persistentqueue_bytes_pending", 0)
		if wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}

	t.Run("drained", func(t *testing.T) {
		f(t, `vm_persistentqueue_bytes_pending{path="/tmp/q"} 0`, false)
	})
	t.Run("not drained yet", func(t *testing.T) {
		f(t, `vm_persistentqueue_bytes_pending{path="/tmp/q"} 1024`, true)
	})
}
