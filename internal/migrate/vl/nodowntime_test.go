package vl

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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(vmv1.AddToScheme(s))
	s.AddKnownTypeWithName(migrate.VolumeSnapshotGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(migrate.VolumeSnapshotGVK.GroupVersion().WithKind("VolumeSnapshotList"), &unstructured.UnstructuredList{})
	return s
}

// helmLabels mirrors the parent migrate package's own helper (unexported there, so it's
// duplicated here rather than exported solely for test convenience).
func helmLabels(releaseName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   releaseName,
		"app.kubernetes.io/managed-by": "Helm",
	}
}

// TestNoDowntimeSingleNode mirrors vm's own test, but exercises the victoria-logs-single
// flow: a VLAgent buffer instead of a VMAgent, and VictoriaLogs' own
// remote-write/force-merge/queue-drain conventions.
func TestNoDowntimeSingleNode(t *testing.T) {
	migrate.PVCBindPollInterval = 5 * time.Millisecond
	migrate.PVCBindTimeout = time.Second
	migrate.TargetReadyPollInterval = 5 * time.Millisecond
	migrate.TargetReadyTimeout = time.Second
	migrate.SnapshotPollInterval = 5 * time.Millisecond
	migrate.SnapshotTimeout = time.Second
	migrate.DrainPollInterval = 5 * time.Millisecond
	migrate.DrainTimeout = time.Second

	release := "myrelease"
	ns := "default"
	labels := helmLabels(release)

	// stand-in admin server for both the "old storage" force-merge endpoint and the buffer
	// agent's /metrics queue-drain endpoint; queue is reported empty so drain succeeds
	// immediately.
	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			fmt.Fprintln(w, `vlagent_remotewrite_pending_data_bytes{path="/tmp/q"} 0`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer adminServer.Close()
	testHTTPClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, adminServer.Listener.Addr().String())
			},
		},
	}

	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			},
		},
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
	}
	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": release},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 9428}},
		},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithStatusSubresource(&vmv1.VLSingle{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(oldPVC, dep, oldSvc).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target := &vmv1.VLSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
	}

	go simulateVLSingleOperational(ctx, c, types.NamespacedName{Name: release, Namespace: ns})
	go simulateBind(ctx, c, types.NamespacedName{Name: target.PrefixedName(), Namespace: ns})
	go simulateVolumeSnapshotReady(ctx, c, ns, release+"-migration-snapshot")
	go simulateVLAgentEndpoints(ctx, c, ns, target.PrefixedName()+"-migration-buffer", adminServer.URL)

	// captures the buffer agent's remoteWrite URL as first observed, i.e. before it gets
	// repointed at the target CR later on — this is the URL that must reference the alias
	// Service (not oldSvc directly), the fix for the self-loop bug described in
	// vl/nodowntime.go.
	initialWriteURL := captureInitialAgentWriteURL(ctx, c, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns})

	err := NoDowntimeSingleNode(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVLSingle,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	select {
	case url := <-initialWriteURL:
		assert.Contains(t, url, "-migration-source")
	default:
		t.Fatal("never observed the buffer agent's initial remoteWrite URL")
	}

	// old Deployment and PVC were never touched.
	var checkDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkDep))
	var checkPVC corev1.PersistentVolumeClaim
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkPVC))

	// target CR exists.
	var createdTarget vmv1.VLSingle
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	// new PVC exists, sourced from the snapshot.
	var newPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vlsingle-" + release, Namespace: ns}, &newPVC))
	require.NotNil(t, newPVC.Spec.DataSource)
	assert.Equal(t, release+"-migration-snapshot", newPVC.Spec.DataSource.Name)

	// buffer agent was deleted at the end.
	var agent vmv1.VLAgent
	err = c.Get(ctx, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))

	// final Service selector points at the target's pods.
	var svc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &svc))
	assert.Equal(t, target.SelectorLabels(), svc.Spec.Selector)
}

// simulateBind mimics a real PVC-binding controller, which the fake client doesn't have:
// once nsn's PVC appears, mark it Bound so callers waiting on RebindPVC's bind-poll succeed.
// It exits promptly once ctx is done, rather than leaking past its caller's test function.
func simulateBind(ctx context.Context, c client.Client, nsn types.NamespacedName) {
	for {
		var pvc corev1.PersistentVolumeClaim
		if err := c.Get(ctx, nsn, &pvc); err == nil {
			pvc.Status.Phase = corev1.ClaimBound
			_ = c.Status().Update(ctx, &pvc)
			return
		} else if !k8serrors.IsNotFound(err) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// simulateVLSingleOperational mimics the operator's own reconcile loop marking a
// freshly-created VLSingle Operational, which the fake client won't do on its own.
func simulateVLSingleOperational(ctx context.Context, c client.Client, nsn types.NamespacedName) {
	for {
		var cr vmv1.VLSingle
		if err := c.Get(ctx, nsn, &cr); err == nil {
			cr.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
			_ = c.Status().Update(ctx, &cr)
			return
		} else if !k8serrors.IsNotFound(err) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// simulateVolumeSnapshotReady exits promptly once ctx is done, rather than leaking past its
// caller's test function.
func simulateVolumeSnapshotReady(ctx context.Context, c client.Client, namespace, name string) {
	for {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(migrate.VolumeSnapshotGVK)
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj); err == nil {
			_ = unstructured.SetNestedField(obj.Object, true, "status", "readyToUse")
			_ = c.Update(ctx, obj)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// simulateVLAgentEndpoints waits for the buffer VLAgent CR to be created, then publishes an
// EndpointSlice for it pointing at adminServerURL, so WaitQueueDrained's discovery can find
// something to poll.
func simulateVLAgentEndpoints(ctx context.Context, c client.Client, namespace, agentName, adminServerURL string) {
	u, err := parseHostPort(adminServerURL)
	if err != nil {
		return
	}
	for {
		var agent vmv1.VLAgent
		if err := c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: namespace}, &agent); err == nil {
			es := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      agentName + "-endpoints",
					Namespace: namespace,
					Labels:    map[string]string{discoveryv1.LabelServiceName: agent.PrefixedName()},
				},
				Endpoints: []discoveryv1.Endpoint{
					{Addresses: []string{u.host}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
				},
				Ports: []discoveryv1.EndpointPort{
					{Name: ptr.To("http"), Port: ptr.To(u.port)},
				},
			}
			_ = c.Create(ctx, es)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// captureInitialAgentWriteURL returns a channel that receives the buffer agent's remoteWrite
// URL as soon as it's first observed — i.e. before the caller later repoints it at the target
// CR — so the test can assert on the *original* write target the agent was created with.
func captureInitialAgentWriteURL(ctx context.Context, c client.Client, nsn types.NamespacedName) <-chan string {
	ch := make(chan string, 1)
	go func() {
		for {
			var agent vmv1.VLAgent
			if err := c.Get(ctx, nsn, &agent); err == nil && len(agent.Spec.RemoteWrite) > 0 {
				ch <- agent.Spec.RemoteWrite[0].URL
				return
			}
			select {
			case <-ctx.Done():
				close(ch)
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
	}()
	return ch
}

type hostPort struct {
	host string
	port int32
}

func parseHostPort(rawURL string) (hostPort, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return hostPort{}, err
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return hostPort{}, err
	}
	port, err := strconv.ParseInt(portStr, 10, 32)
	if err != nil {
		return hostPort{}, err
	}
	return hostPort{host: host, port: int32(port)}, nil
}
