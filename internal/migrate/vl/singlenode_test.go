package vl

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
	"github.com/VictoriaMetrics/operator/internal/migrate/migratetest"
)

const adminAddr = "10.0.0.1"

func adminHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			fmt.Fprintf(w, "%s{url=\"/tmp/q\"} 0\n", vmv1.VLAgentQueueMetricName)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

// TestNoDowntimeSingleNode mirrors vm's own test, but exercises the victoria-logs-single
// flow: a VLAgent buffer instead of a VMAgent, and VictoriaLogs' own
// remote-write/force-merge/queue-drain conventions.
func TestNoDowntimeSingleNode(t *testing.T) {
	synctest.Test(t, testNoDowntimeSingleNode)
}

func testNoDowntimeSingleNode(t *testing.T) {
	release := "myrelease"
	ns := "default"
	labels := migratetest.HelmLabels(release)

	testHTTPClient := migratetest.HandlerClient(adminHandler())

	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			},
			VolumeName: "pv-data",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": release}}},
	}
	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": release},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 9428}},
		},
	}

	var initialWriteURL string
	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1.AddToScheme)).
		WithStatusSubresource(&vmv1.VLSingle{}, &vmv1.VLAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(oldPVC, dep, oldSvc).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if agent, ok := obj.(*vmv1.VLAgent); ok && initialWriteURL == "" && len(agent.Spec.RemoteWrite) > 0 {
					initialWriteURL = agent.Spec.RemoteWrite[0].URL
				}
				return cl.Create(ctx, obj, opts...)
			},
		}).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	target := &vmv1.VLSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1.VLSingleSpec{
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}

	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1.VLSingle { return &vmv1.VLSingle{} })
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: target.PrefixedName(), Namespace: ns})
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, release+"-migration-snapshot")
	go simulateVLAgentEndpoints(ctx, t, c, ns, target.PrefixedName()+"-migration-buffer")
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, func() *vmv1.VLAgent { return &vmv1.VLAgent{} })
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(), adminAddr, 9428)

	err := NoDowntimeSingleNode(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVLSingle,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	assert.Equal(t, target.GetRemoteWriteURL(), initialWriteURL)

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

// simulateVLAgentEndpoints waits for the buffer VLAgent CR to be created, then publishes an
// EndpointSlice for it at adminAddr, so WaitQueueDrained's discovery can find something to poll.
func simulateVLAgentEndpoints(ctx context.Context, t *testing.T, c client.Client, namespace, agentName string) {
	for {
		var agent vmv1.VLAgent
		if err := c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: namespace}, &agent); err == nil {
			migratetest.SimulateServiceEndpoints(ctx, t, c, namespace, agent.PrefixedName(), adminAddr, 9428)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}
