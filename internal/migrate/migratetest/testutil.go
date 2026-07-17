// Package migratetest holds test helpers shared by the migrate engine packages (vm, vl, vt),
// so each one doesn't carry its own copy of the same fake-client simulators.
package migratetest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// Scheme builds the client-go scheme plus migrate's VolumeSnapshot GVK plus whatever
// engine-specific types register adds (e.g. vmv1beta1.AddToScheme, vmv1.AddToScheme).
func Scheme(register ...func(*runtime.Scheme) error) *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	for _, add := range register {
		utilruntime.Must(add(s))
	}
	s.AddKnownTypeWithName(migrate.VolumeSnapshotGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(migrate.VolumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"), &unstructured.UnstructuredList{})
	return s
}

// HelmLabels mirrors the migrate package's own helper (unexported there, so it's duplicated
// here rather than exported solely for test convenience).
func HelmLabels(releaseName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   releaseName,
		"app.kubernetes.io/managed-by": "Helm",
	}
}

// HandlerClient returns an *http.Client that dispatches every request directly to handler
// in-process via httptest.NewRecorder, without any real network I/O. This makes it safe to use
// inside a testing/synctest bubble: a real listening socket's Accept-loop goroutine is never
// "durably blocked" the way synctest requires, so it would stall the bubble's virtual clock
// forever — an in-process RoundTripper has no such goroutine.
func HandlerClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result(), nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// AdminHandler mocks the old workload's admin API and the buffer agent's /metrics endpoint. The
// queueMetricName sample reports nonzero for the first couple of scrapes before draining to
// zero, so tests actually exercise waitQueueDrained's polling loop — and its ordering relative
// to buffer agent deletion — rather than it trivially succeeding on the first scrape.
func AdminHandler(queueMetricName string) http.Handler {
	var scrapes int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			scrapes++
			var queued int
			if scrapes <= 2 {
				queued = 1024
			}
			fmt.Fprintf(w, "%s{url=\"/tmp/q\"} %d\n", queueMetricName, queued)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

// SimulateBind mimics a real PVC-binding controller, which the fake client doesn't have: once
// nsn's PVC appears, mark it Bound so callers waiting on RebindPVC's bind-poll succeed. It exits
// promptly once ctx is done, rather than leaking past its caller's test function. t.Errorf is
// used instead of require/t.Fatal since this runs on its own goroutine, where only the
// FailNow-based helpers are unsafe to call.
func SimulateBind(ctx context.Context, t *testing.T, c client.Client, nsn types.NamespacedName) {
	for {
		var pvc corev1.PersistentVolumeClaim
		if err := c.Get(ctx, nsn, &pvc); err == nil {
			pvc.Status.Phase = corev1.ClaimBound
			if err := c.Status().Update(ctx, &pvc); err != nil {
				t.Errorf("SimulateBind: cannot mark PVC %s bound: %v", nsn, err)
			}
			return
		} else if !k8serrors.IsNotFound(err) {
			t.Errorf("SimulateBind: cannot get PVC %s: %v", nsn, err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// SimulateVolumeSnapshotReady exits promptly once ctx is done, rather than leaking past its
// caller's test function.
func SimulateVolumeSnapshotReady(ctx context.Context, t *testing.T, c client.Client, namespace, name string) {
	nsn := types.NamespacedName{Name: name, Namespace: namespace}
	for {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(migrate.VolumeSnapshotGVK)
		if err := c.Get(ctx, nsn, obj); err == nil {
			if err := unstructured.SetNestedField(obj.Object, true, "status", "readyToUse"); err != nil {
				t.Errorf("SimulateVolumeSnapshotReady: cannot set status on VolumeSnapshot %s: %v", nsn, err)
				return
			}
			if err := c.Update(ctx, obj); err != nil {
				t.Errorf("SimulateVolumeSnapshotReady: cannot update VolumeSnapshot %s: %v", nsn, err)
			}
			return
		} else if !k8serrors.IsNotFound(err) {
			t.Errorf("SimulateVolumeSnapshotReady: cannot get VolumeSnapshot %s: %v", nsn, err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// SimulateServiceEndpoints publishes an EndpointSlice for serviceName with one ready endpoint
// at host:port, so discovery code under test finds something to poll.
func SimulateServiceEndpoints(ctx context.Context, t *testing.T, c client.Client, namespace, serviceName, host string, port int32) {
	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-endpoints",
			Namespace: namespace,
			Labels:    map[string]string{discoveryv1.LabelServiceName: serviceName},
		},
		AddressType: addressType(host),
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{host}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
		},
		Ports: []discoveryv1.EndpointPort{
			{Name: ptr.To("http"), Port: ptr.To(port)},
		},
	}
	if err := c.Create(ctx, es); err != nil {
		t.Errorf("SimulateServiceEndpoints: cannot create EndpointSlice for %s/%s: %v", namespace, serviceName, err)
	}
}

// operationalCR is the shape SimulateOperational needs from a CR.
type operationalCR interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}

// SimulateOperational mimics the operator's own reconcile loop marking a freshly-created CR
// Operational, which the fake client won't do on its own. newObj must return a fresh zero-value
// T each call, e.g. func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} }.
func SimulateOperational[T operationalCR](ctx context.Context, t *testing.T, c client.Client, nsn types.NamespacedName, newObj func() T) {
	for {
		obj := newObj()
		if err := c.Get(ctx, nsn, obj); err == nil {
			status := obj.GetStatusMetadata()
			status.UpdateStatus = vmv1beta1.UpdateStatusOperational
			status.ObservedGeneration = obj.GetGeneration()
			if err := c.Status().Update(ctx, obj); err != nil {
				t.Errorf("SimulateOperational: cannot mark %s Operational: %v", nsn, err)
			}
			return
		} else if !k8serrors.IsNotFound(err) {
			t.Errorf("SimulateOperational: cannot get %s: %v", nsn, err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// prefixedCR is the shape SimulateAgentEndpoints needs from a CR.
type prefixedCR interface {
	client.Object
	PrefixedName() string
}

// SimulateAgentEndpoints waits for the buffer agent CR at nsn to be created, then publishes an
// EndpointSlice for its own Service at host:port, so waitQueueDrained's discovery can find
// something to poll. newObj must return a fresh zero-value T each call.
func SimulateAgentEndpoints[T prefixedCR](ctx context.Context, t *testing.T, c client.Client, nsn types.NamespacedName, host string, port int32, newObj func() T) {
	for {
		obj := newObj()
		if err := c.Get(ctx, nsn, obj); err == nil {
			SimulateServiceEndpoints(ctx, t, c, nsn.Namespace, obj.PrefixedName(), host, port)
			return
		} else if !k8serrors.IsNotFound(err) {
			t.Errorf("SimulateAgentEndpoints: cannot get %s: %v", nsn, err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func addressType(host string) discoveryv1.AddressType {
	ip := net.ParseIP(host)
	switch {
	case ip == nil:
		return discoveryv1.AddressTypeFQDN
	case ip.To4() != nil:
		return discoveryv1.AddressTypeIPv4
	default:
		return discoveryv1.AddressTypeIPv6
	}
}
