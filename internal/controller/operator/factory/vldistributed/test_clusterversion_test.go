package vldistributed

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

// TestClusterVersionChange reproduces the exact conditions of Test_CreateOrUpdate_Actions case 4
// to diagnose why 9 actions are produced instead of 13.
func TestClusterVersionChange(t *testing.T) {
	zoneSpec := vmv1alpha1.VLDistributedZoneCluster{
		Spec: vmv1.VLClusterSpec{
			VLStorage: &vmv1.VLStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))}},
			VLSelect:  &vmv1.VLSelect{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))}},
			VLInsert:  &vmv1.VLInsert{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))}},
		},
	}
	vmAuthSpec := vmv1alpha1.VLDistributedAuth{
		Spec: vmv1beta1.VMAuthSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
		},
	}

	cr := &vmv1alpha1.VLDistributed{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{Name: "test-dist", Namespace: "default"},
		Spec: vmv1alpha1.VLDistributedSpec{
			Zones: []vmv1alpha1.VLDistributedZone{{
				Name:      "zone-1",
				VLCluster: zoneSpec,
				VLAgent: vmv1alpha1.VLDistributedZoneAgent{
					Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
						PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
					},
				},
			}},
			VMAuth: vmAuthSpec,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `%s{url="http://vl-insert-test-dist-zone-1.default.svc:9481"} 0`, vlAgentQueueMetricName)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	tsURL, _ := url.Parse(ts.URL)
	tsHost, tsPortStr, _ := net.SplitHostPort(tsURL.Host)
	tsPort, _ := strconv.ParseInt(tsPortStr, 10, 32)

	endpointSlice := discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "random-endpoint-name",
			Namespace: "default",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "vlagent-test-dist-zone-1"},
		},
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{tsHost}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}},
		},
		Ports: []discoveryv1.EndpointPort{{Name: ptr.To("http"), Port: ptr.To[int32](int32(tsPort))}},
	}

	{
		fclient := k8stools.GetTestClientWithOptsActionsAndObjects(
			[]runtime.Object{&endpointSlice},
			&k8stools.ClientOpts{SetCreationTimestamp: true},
		)
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)

		// === preRun ===
		assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), fclient))
		fclient.Actions = nil
		cr.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
		cr.Spec.ZoneCommon.VLCluster.Spec.ClusterVersion = "v1.51.0-cluster"

		// Verify what's stored
		var storedCluster vmv1.VLCluster
		assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-dist-zone-1"}, &storedCluster))
		t.Logf("=== After preRun ===")
		t.Logf("Stored VLCluster ClusterVersion: %q", storedCluster.Spec.ClusterVersion)
		if storedCluster.Spec.VLStorage != nil {
			t.Logf("Stored VLCluster VLStorage.Image.Tag: %q", storedCluster.Spec.VLStorage.Image.Tag)
		}
		t.Logf("Stored VLCluster CreationTimestamp: %v", storedCluster.CreationTimestamp)
		t.Logf("args.cr.Spec.ZoneCommon.VLCluster.Spec.ClusterVersion: %q", cr.Spec.ZoneCommon.VLCluster.Spec.ClusterVersion)
		t.Logf("args.cr.Spec.Zones[0].VLCluster.Spec.ClusterVersion: %q", cr.Spec.Zones[0].VLCluster.Spec.ClusterVersion)
		if cr.Spec.Zones[0].VLCluster.Spec.VLStorage != nil {
			t.Logf("args.cr.Spec.Zones[0].VLCluster.Spec.VLStorage.Image.Tag: %q", cr.Spec.Zones[0].VLCluster.Spec.VLStorage.Image.Tag)
		}

		// Simulate second getZones manually
		zs, err := getZones(ctx, fclient, cr)
		assert.NoError(t, err)
		c := zs.clusterObjects()[0]
		t.Logf("\n=== After main getZones ===")
		t.Logf("New VLCluster ClusterVersion: %q", c.Spec.ClusterVersion)
		if c.Spec.VLStorage != nil {
			t.Logf("New VLCluster VLStorage.Image.Tag: %q", c.Spec.VLStorage.Image.Tag)
		}
		t.Logf("hasChanges[0]: %v", zs.hasChanges[0])
		t.Logf("backends[0].prevAccepts: %v", zs.backends[0].prevAccepts)
		t.Logf("backends[0].obj.CreationTimestamp: %v", zs.backends[0].obj.GetCreationTimestamp())
		t.Logf("vlagents[0].CreationTimestamp: %v (IsZero: %v)", zs.vlagents[0].CreationTimestamp, zs.vlagents[0].CreationTimestamp.IsZero())

		backendCreated := !zs.backends[0].obj.GetCreationTimestamp().Time.IsZero()
		needsLBUpdate := backendCreated && !zs.vlagents[0].CreationTimestamp.IsZero()
		if !zs.hasChanges[0] {
			needsLBUpdate = false
		}
		t.Logf("backendCreated: %v, needsLBUpdate: %v", backendCreated, needsLBUpdate)

		// Now run the full CreateOrUpdate
		fclient.Actions = nil
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		t.Logf("\n=== Main CreateOrUpdate Actions ===")
		for i, a := range fclient.Actions {
			t.Logf("Action %d: %s %s %s", i, a.Verb, a.Kind, a.Resource)
		}
	}
}
