package vl

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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
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
	target := &vmv1.VLCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1.VLClusterSpec{
			VLStorage: &vmv1.VLStorage{Storage: &vmv1beta1.StorageSpec{}},
			VLSelect:  &vmv1.VLSelect{},
			VLInsert:  &vmv1.VLInsert{},
		},
	}

	engine := clusterEngine()
	assert.Equal(t, "vl", engine.ComponentPrefix)

	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentStorage))
	assert.Equal(t, "vlstorage-db", engine.PVCTemplateName(target, vmv1beta1.ClusterComponentStorage))

	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentSelect))
	assert.Empty(t, engine.PVCTemplateName(target, vmv1beta1.ClusterComponentSelect))

	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentInsert))
	assert.Empty(t, engine.PVCTemplateName(target, vmv1beta1.ClusterComponentInsert))
}

func TestClusterEngine_DisabledComponent(t *testing.T) {
	target := &vmv1.VLCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1.VLClusterSpec{
			VLStorage: &vmv1.VLStorage{Storage: &vmv1beta1.StorageSpec{}},
			VLInsert:  &vmv1.VLInsert{},
		},
	}

	engine := clusterEngine()
	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentStorage))
	assert.False(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentSelect))
	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentInsert))
}

// TestNoDowntimeCluster exercises the full VLCluster NoDowntime flow: vlinsert is buffered
// and cut over, vlselect is cut over directly (nothing to buffer), and vlstorage's per-ordinal
// PVCs are snapshotted with no Service involved at all.
func TestNoDowntimeCluster(t *testing.T) {
	synctest.Test(t, testNoDowntimeCluster)
}

func testNoDowntimeCluster(t *testing.T) {
	release := "myrelease"
	ns := "default"

	testHTTPClient := migratetest.HandlerClient(adminHandler())

	insertLabels := componentLabels(release, "vlinsert")
	oldInsertDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vlinsert", Namespace: ns, Labels: insertLabels},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vlinsert"}}},
	}
	oldInsertSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vlinsert", Namespace: ns, Labels: insertLabels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "old-vlinsert"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 9481}},
		},
	}

	selectLabels := componentLabels(release, "vlselect")
	oldSelectDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vlselect", Namespace: ns, Labels: selectLabels},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vlselect"}}},
	}
	oldSelectSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vlselect", Namespace: ns, Labels: selectLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vlselect"}},
	}

	storageLabels := componentLabels(release, "vlstorage")
	oldStorageSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vlstorage", Namespace: ns, Labels: storageLabels},
		Spec:       appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vlstorage"}}},
	}
	oldStorageSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vlstorage", Namespace: ns, Labels: storageLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vlstorage"}},
	}
	var storagePVs []*corev1.PersistentVolume
	var storagePVCs []*corev1.PersistentVolumeClaim
	for i := range 2 {
		pvName := fmt.Sprintf("pv-storage-%d", i)
		pvcName := fmt.Sprintf("data-old-vlstorage-%d", i)
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
		})
	}

	objs := []client.Object{oldInsertDep, oldInsertSvc, oldSelectDep, oldSelectSvc, oldStorageSTS, oldStorageSvc}
	for _, pv := range storagePVs {
		objs = append(objs, pv)
	}
	for _, pvc := range storagePVCs {
		objs = append(objs, pvc)
	}
	var initialWriteURL string
	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1.AddToScheme)).
		WithStatusSubresource(&vmv1.VLCluster{}, &vmv1.VLAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(objs...).
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

	target := &vmv1.VLCluster{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1.VLClusterSpec{
			VLStorage: &vmv1.VLStorage{
				Storage:          &vmv1beta1.StorageSpec{},
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
			},
			VLSelect: &vmv1.VLSelect{},
			VLInsert: &vmv1.VLInsert{},
		},
	}

	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1.VLCluster { return &vmv1.VLCluster{} })
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: agentName, Namespace: ns}, func() *vmv1.VLAgent { return &vmv1.VLAgent{} })
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, "data-old-vlstorage-0-migration-snapshot")
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, "data-old-vlstorage-1-migration-snapshot")
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: "vlstorage-db-vlstorage-myrelease-0", Namespace: ns})
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: "vlstorage-db-vlstorage-myrelease-1", Namespace: ns})
	go simulateVLAgentEndpoints(ctx, t, c, ns, agentName)
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentInsert), adminAddr, 9428)
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentSelect), adminAddr, 9428)

	err := NoDowntimeCluster(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVLCluster,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	assert.Equal(t, target.GetRemoteWriteURL(), initialWriteURL)

	// old insert/select/storage and all PVCs were never touched.
	var checkInsertDep, checkSelectDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vlinsert", Namespace: ns}, &checkInsertDep))
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vlselect", Namespace: ns}, &checkSelectDep))
	var checkStorageSTS appsv1.StatefulSet
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vlstorage", Namespace: ns}, &checkStorageSTS))
	var checkPVC0, checkPVC1 corev1.PersistentVolumeClaim
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "data-old-vlstorage-0", Namespace: ns}, &checkPVC0))
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "data-old-vlstorage-1", Namespace: ns}, &checkPVC1))

	// target CR exists.
	var createdTarget vmv1.VLCluster
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	// new per-ordinal storage PVCs exist, sourced from the snapshots.
	var newPVC0, newPVC1 corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vlstorage-db-vlstorage-myrelease-0", Namespace: ns}, &newPVC0))
	require.NotNil(t, newPVC0.Spec.DataSource)
	assert.Equal(t, "data-old-vlstorage-0-migration-snapshot", newPVC0.Spec.DataSource.Name)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vlstorage-db-vlstorage-myrelease-1", Namespace: ns}, &newPVC1))
	require.NotNil(t, newPVC1.Spec.DataSource)
	assert.Equal(t, "data-old-vlstorage-1-migration-snapshot", newPVC1.Spec.DataSource.Name)

	// buffer agent was deleted at the end.
	var agent vmv1.VLAgent
	err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))

	// final Service selectors point at the target's pods.
	var svcInsert, svcSelect corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vlinsert", Namespace: ns}, &svcInsert))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentInsert), svcInsert.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vlselect", Namespace: ns}, &svcSelect))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentSelect), svcSelect.Spec.Selector)
}
