package vm

import (
	"context"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
	"github.com/VictoriaMetrics/operator/internal/migrate/migratetest"
)

const adminAddr = "10.0.0.1"

func newSingleNodeFixture(release, ns string) (*corev1.PersistentVolumeClaim, *appsv1.Deployment, *corev1.Service, *vmv1beta1.VMSingle) {
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
	return oldPVC, dep, oldSvc, target
}

func TestNoDowntimeSingleNode(t *testing.T) {
	synctest.Test(t, testNoDowntimeSingleNode)
}

func testNoDowntimeSingleNode(t *testing.T) {
	release := "myrelease"
	ns := "default"

	testHTTPClient := migratetest.HandlerClient(migratetest.AdminHandler(vmv1beta1.VMAgentQueueMetricName))

	oldPVC, dep, oldSvc, target := newSingleNodeFixture(release, ns)

	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1beta1.AddToScheme)).
		WithStatusSubresource(&vmv1beta1.VMSingle{}, &vmv1beta1.VMAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(oldPVC, dep, oldSvc).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} })
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: target.PrefixedName(), Namespace: ns})
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, release+"-migration-snapshot")
	go migratetest.SimulateAgentEndpoints(ctx, t, c, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, adminAddr, 8428, func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} })
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

	var checkDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkDep))
	var checkPVC corev1.PersistentVolumeClaim
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkPVC))

	var createdTarget vmv1beta1.VMSingle
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	var newPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmsingle-" + release, Namespace: ns}, &newPVC))
	require.NotNil(t, newPVC.Spec.DataSource)
	assert.Equal(t, release+"-migration-snapshot", newPVC.Spec.DataSource.Name)

	var agent vmv1beta1.VMAgent
	err = c.Get(ctx, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))

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

	oldPVC, dep, oldSvc, target := newSingleNodeFixture(release, ns)
	staleAgent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      target.PrefixedName() + "-migration-buffer",
			Namespace: ns,
			Labels:    map[string]string{"migrate.victoriametrics.com/buffer-agent": "true"},
		},
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

	err := NoDowntimeSingleNode(ctx, c, migratetest.HandlerClient(migratetest.AdminHandler(vmv1beta1.VMAgentQueueMetricName)), migrate.Options{
		Chart:       migrate.ChartVMSingle,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.Error(t, err)
	assert.ErrorContains(t, err, "isn't configured as a persistent buffer forwarding to")

	var checkDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkDep))
	var checkSvc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkSvc))
	assert.Equal(t, oldSvc.Spec.Selector, checkSvc.Spec.Selector)

	var checkReadsAlias corev1.Service
	err = c.Get(ctx, types.NamespacedName{Name: target.PrefixedName() + "-migration-reads", Namespace: ns}, &checkReadsAlias)
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestSingleNodeEngine_AgentMatches_RejectsUndersizedQueue(t *testing.T) {
	engine := singleNodeEngine()
	want, err := engine.BuildAgent("agent", "default", "http://x", "20Gi", "")
	require.NoError(t, err)

	undersized, err := engine.BuildAgent("agent", "default", "http://x", "1Gi", "")
	require.NoError(t, err)
	assert.False(t, engine.AgentMatches(undersized, want), "an existing buffer with a smaller queue than requested must not be reused — it can fill up and lose writes")

	oversized, err := engine.BuildAgent("agent", "default", "http://x", "50Gi", "")
	require.NoError(t, err)
	assert.True(t, engine.AgentMatches(oversized, want), "an existing buffer with at least the requested queue size is safe to reuse")
}

func TestSingleNodeEngine_AgentMatches_RejectsStaleRemoteWriteAuth(t *testing.T) {
	engine := singleNodeEngine()
	want, err := engine.BuildAgent("agent", "default", "http://x", "20Gi", "")
	require.NoError(t, err)

	stale, err := engine.BuildAgent("agent", "default", "http://x", "20Gi", "")
	require.NoError(t, err)
	stale.Spec.RemoteWrite[0].BasicAuth = &vmv1beta1.BasicAuth{Username: corev1.SecretKeySelector{Key: "leftover"}}
	assert.False(t, engine.AgentMatches(stale, want), "an existing buffer with stale remote-write auth (same URL, different config) must not be reused")
}

func TestSingleNodeEngine_HasStorage_StorageDataPathDisablesSpecStorage(t *testing.T) {
	target := &vmv1beta1.VMSingle{
		Spec: vmv1beta1.VMSingleSpec{
			StorageDataPath: "/custom-data",
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}

	engine := singleNodeEngine()
	assert.False(t, engine.HasStorage(target), "storageDataPath disables spec.storage — VMSingle never mounts the migrated PVC in that mode")
}
