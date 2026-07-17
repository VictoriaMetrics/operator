package vm

import (
	"context"
	"fmt"
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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
	"github.com/VictoriaMetrics/operator/internal/migrate/migratetest"
)

// componentLabels mirrors discoverComponent's own label-set construction, for building test
// fixtures that only the intended component's discovery call should ever pick up.
func componentLabels(releaseName, component string) map[string]string {
	l := migratetest.HelmLabels(releaseName)
	l["app.kubernetes.io/component"] = component
	return l
}

func TestClusterEngine(t *testing.T) {
	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{Storage: &vmv1beta1.StorageSpec{}},
			VMSelect:  &vmv1beta1.VMSelect{},
			VMInsert:  &vmv1beta1.VMInsert{},
		},
	}

	engine := clusterEngine()
	assert.Equal(t, "vm", engine.ComponentPrefix)

	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentStorage))
	assert.Equal(t, "vmstorage-db", engine.PVCTemplateName(target, vmv1beta1.ClusterComponentStorage))

	// vmselect has no StorageSpec configured on the target — no PVC template name.
	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentSelect))
	assert.Empty(t, engine.PVCTemplateName(target, vmv1beta1.ClusterComponentSelect))

	// vminsert never has persistent storage.
	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentInsert))
	assert.Empty(t, engine.PVCTemplateName(target, vmv1beta1.ClusterComponentInsert))
}

func TestClusterEngine_SelectCacheVolume(t *testing.T) {
	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMSelect: &vmv1beta1.VMSelect{StorageSpec: &vmv1beta1.StorageSpec{}},
		},
	}

	engine := clusterEngine()
	assert.Equal(t, "vmselect-cachedir", engine.PVCTemplateName(target, vmv1beta1.ClusterComponentSelect))
}

func TestClusterEngine_StorageReplicaCountHPA(t *testing.T) {
	engine := clusterEngine()

	withHPA := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{HPA: &vmv1beta1.EmbeddedHPA{MinReplicas: ptr.To(int32(3))}},
		},
	}
	assert.Equal(t, int32(3), engine.StorageReplicaCount(withHPA))

	withHPADefault := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{HPA: &vmv1beta1.EmbeddedHPA{}},
		},
	}
	assert.Equal(t, int32(1), engine.StorageReplicaCount(withHPADefault))

	explicitReplicaCountWins := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(5))},
				HPA:              &vmv1beta1.EmbeddedHPA{MinReplicas: ptr.To(int32(3))},
			},
		},
	}
	assert.Equal(t, int32(5), engine.StorageReplicaCount(explicitReplicaCountWins))
}

func TestClusterEngine_DisabledComponent(t *testing.T) {
	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{Storage: &vmv1beta1.StorageSpec{}},
			VMInsert:  &vmv1beta1.VMInsert{},
		},
	}

	engine := clusterEngine()
	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentStorage))
	assert.False(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentSelect))
	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentInsert))
}

// TestNoDowntimeCluster exercises the full VMCluster NoDowntime flow: vminsert is buffered
// and cut over, vmselect is cut over directly (nothing to buffer), and vmstorage's per-ordinal
// PVCs are snapshotted with no Service involved at all.
func TestNoDowntimeCluster(t *testing.T) {
	synctest.Test(t, testNoDowntimeCluster)
}

func testNoDowntimeCluster(t *testing.T) {
	release := "myrelease"
	ns := "default"

	testHTTPClient := migratetest.HandlerClient(migratetest.AdminHandler(vmv1beta1.VMAgentQueueMetricName))

	insertLabels := componentLabels(release, "vminsert")
	oldInsertDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vminsert", Namespace: ns, Labels: insertLabels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vminsert"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "old-vminsert"}}},
		},
	}
	oldInsertSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vminsert", Namespace: ns, Labels: insertLabels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "old-vminsert"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 8428}},
		},
	}

	selectLabels := componentLabels(release, "vmselect")
	oldSelectSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmselect", Namespace: ns, Labels: selectLabels},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vmselect"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "old-vmselect"}}},
		},
	}
	oldSelectSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmselect", Namespace: ns, Labels: selectLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vmselect"}},
	}

	storageLabels := componentLabels(release, "vmstorage")
	oldStorageSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmstorage", Namespace: ns, Labels: storageLabels},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vmstorage"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "old-vmstorage"}}},
		},
	}
	oldStorageSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmstorage", Namespace: ns, Labels: storageLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vmstorage"}},
	}
	var storagePVs []*corev1.PersistentVolume
	var storagePVCs []*corev1.PersistentVolumeClaim
	for i := range 2 {
		pvName := fmt.Sprintf("pv-storage-%d", i)
		pvcName := fmt.Sprintf("data-old-vmstorage-%d", i)
		storagePVs = append(storagePVs, &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: pvName},
			Spec: corev1.PersistentVolumeSpec{
				Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: pvcName},
			},
		})
		storagePVCs = append(storagePVCs, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: ns, Labels: storageLabels},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				VolumeName:  pvName,
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		})
	}

	objs := []client.Object{oldInsertDep, oldInsertSvc, oldSelectSTS, oldSelectSvc, oldStorageSTS, oldStorageSvc}
	for _, pv := range storagePVs {
		objs = append(objs, pv)
	}
	for _, pvc := range storagePVCs {
		objs = append(objs, pvc)
	}
	var initialWriteURL string
	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1beta1.AddToScheme)).
		WithStatusSubresource(&vmv1beta1.VMCluster{}, &vmv1beta1.VMAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(objs...).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if agent, ok := obj.(*vmv1beta1.VMAgent); ok && initialWriteURL == "" && len(agent.Spec.RemoteWrite) > 0 {
					initialWriteURL = agent.Spec.RemoteWrite[0].URL
				}
				return cl.Create(ctx, obj, opts...)
			},
		}).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{
				Storage:          &vmv1beta1.StorageSpec{},
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
			},
			VMSelect: &vmv1beta1.VMSelect{},
			VMInsert: &vmv1beta1.VMInsert{},
		},
	}

	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1beta1.VMCluster { return &vmv1beta1.VMCluster{} })
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: agentName, Namespace: ns}, func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} })
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, "data-old-vmstorage-0-migration-snapshot")
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, "data-old-vmstorage-1-migration-snapshot")
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-0", Namespace: ns})
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-1", Namespace: ns})
	go migratetest.SimulateAgentEndpoints(ctx, t, c, types.NamespacedName{Name: agentName, Namespace: ns}, adminAddr, 8428, func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} })
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentInsert), adminAddr, 8428)
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentSelect), adminAddr, 8428)

	err := NoDowntimeCluster(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVMCluster,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	assert.Equal(t, target.GetRemoteWriteURL(), initialWriteURL)

	// old insert/select/storage and all PVCs were never touched.
	var checkDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vminsert", Namespace: ns}, &checkDep))
	var checkSelectSTS, checkStorageSTS appsv1.StatefulSet
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vmselect", Namespace: ns}, &checkSelectSTS))
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vmstorage", Namespace: ns}, &checkStorageSTS))
	var checkPVC0, checkPVC1 corev1.PersistentVolumeClaim
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "data-old-vmstorage-0", Namespace: ns}, &checkPVC0))
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "data-old-vmstorage-1", Namespace: ns}, &checkPVC1))

	// target CR exists.
	var createdTarget vmv1beta1.VMCluster
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	// new per-ordinal storage PVCs exist, sourced from the snapshots.
	var newPVC0, newPVC1 corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-0", Namespace: ns}, &newPVC0))
	require.NotNil(t, newPVC0.Spec.DataSource)
	assert.Equal(t, "data-old-vmstorage-0-migration-snapshot", newPVC0.Spec.DataSource.Name)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-1", Namespace: ns}, &newPVC1))
	require.NotNil(t, newPVC1.Spec.DataSource)
	assert.Equal(t, "data-old-vmstorage-1-migration-snapshot", newPVC1.Spec.DataSource.Name)

	// buffer agent was deleted at the end.
	var agent vmv1beta1.VMAgent
	err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))

	// final Service selectors point at the target's pods.
	var svcInsert, svcSelect corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vminsert", Namespace: ns}, &svcInsert))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentInsert), svcInsert.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vmselect", Namespace: ns}, &svcSelect))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentSelect), svcSelect.Spec.Selector)
}
