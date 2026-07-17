package vm

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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
	"github.com/VictoriaMetrics/operator/internal/migrate/migratetest"
)

const adminAddr = "10.0.0.1"

func adminHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			fmt.Fprintf(w, "%s{url=\"/tmp/q\"} 0\n", vmv1beta1.VMAgentQueueMetricName)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

// TestNoDowntimeSingleNode exercises the full flow against a fake client + an in-process stand-in
// for both the old VictoriaMetrics storage's admin API and the buffer VMAgent's /metrics
// endpoint. Everything except the CSI driver itself (simulated here by manually flipping
// VolumeSnapshot.status.readyToUse, since no real snapshot controller runs against a fake
// client) is exercised end-to-end.
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
			Ports:    []corev1.ServicePort{{Name: "http", Port: 8428}},
		},
	}

	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1beta1.AddToScheme)).
		WithStatusSubresource(&vmv1beta1.VMSingle{}, &vmv1beta1.VMAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(oldPVC, dep, oldSvc).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMSingleSpec{
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}

	// Simulate: buffer agent's Service/EndpointSlice appearing (so drain-poll can discover
	// it), the snapshot becoming ready, the new PVC binding, and the target CR going
	// Operational — all things a real cluster's controllers would do on their own.
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} })
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: target.PrefixedName(), Namespace: ns})
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, release+"-migration-snapshot")
	go simulateAgentEndpoints(ctx, t, c, ns, target.PrefixedName()+"-migration-buffer")
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} })
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(), adminAddr, 8428)

	err := NoDowntimeSingleNode(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVMSingle,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	// old Deployment and PVC were never touched.
	var checkDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkDep))
	var checkPVC corev1.PersistentVolumeClaim
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkPVC))

	// target CR exists.
	var createdTarget vmv1beta1.VMSingle
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	// new PVC exists, sourced from the snapshot.
	var newPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmsingle-" + release, Namespace: ns}, &newPVC))
	require.NotNil(t, newPVC.Spec.DataSource)
	assert.Equal(t, release+"-migration-snapshot", newPVC.Spec.DataSource.Name)

	// buffer agent was deleted at the end.
	var agent vmv1beta1.VMAgent
	err = c.Get(ctx, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))

	// final Service selector points at the target's pods.
	var svc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &svc))
	assert.Equal(t, target.SelectorLabels(), svc.Spec.Selector)
}

func TestNoDowntimeSingleNode_StaleBufferAgentIsAnError(t *testing.T) {
	synctest.Test(t, testNoDowntimeSingleNode_StaleBufferAgentIsAnError)
}

func testNoDowntimeSingleNode_StaleBufferAgentIsAnError(t *testing.T) {
	release := "myrelease"
	ns := "default"
	labels := migratetest.HelmLabels(release)

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
			Ports:    []corev1.ServicePort{{Name: "http", Port: 8428}},
		},
	}
	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMSingleSpec{
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}
	// left over from an unrelated prior attempt: same deterministic name, different target.
	staleAgent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://unrelated.default.svc:8428/api/v1/write"}},
		},
	}

	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1beta1.AddToScheme)).
		WithStatusSubresource(&vmv1beta1.VMSingle{}, &vmv1beta1.VMAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(oldPVC, dep, oldSvc, staleAgent).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	err := NoDowntimeSingleNode(ctx, c, http.DefaultClient, migrate.Options{
		Chart:       migrate.ChartVMSingle,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	assert.Error(t, err)

	// old Deployment/PVC/Service untouched; the failure happens before the release Service is
	// ever cut over.
	var checkDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkDep))
	var checkSvc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkSvc))
	assert.Equal(t, oldSvc.Spec.Selector, checkSvc.Spec.Selector)

	// the read alias Service, created before the agent-creation attempt, is left behind on
	// this failure path — not cleaned up automatically.
	var checkReadsAlias corev1.Service
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: target.PrefixedName() + "-migration-reads", Namespace: ns}, &checkReadsAlias))
}

// simulateAgentEndpoints waits for the buffer VMAgent CR to be created, then publishes an
// EndpointSlice for it at adminAddr, so WaitQueueDrained's discovery can find something to poll.
func simulateAgentEndpoints(ctx context.Context, t *testing.T, c client.Client, namespace, agentName string) {
	for {
		var agent vmv1beta1.VMAgent
		if err := c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: namespace}, &agent); err == nil {
			migratetest.SimulateServiceEndpoints(ctx, t, c, namespace, agent.PrefixedName(), adminAddr, 8428)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}
