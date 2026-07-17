package vt

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

func componentLabels(component string) map[string]string {
	l := migratetest.HelmLabels("myrelease")
	l["app.kubernetes.io/component"] = component
	return l
}

type vtClusterFixture struct {
	oldInsertDep, oldSelectDep *appsv1.Deployment
	oldInsertSvc, oldSelectSvc *corev1.Service
	oldStorageSTS              *appsv1.StatefulSet
	oldStorageSvc              *corev1.Service
	storagePVs                 []*corev1.PersistentVolume
	storagePVCs                []*corev1.PersistentVolumeClaim
	target                     *vmv1.VTCluster
}

func newVTClusterFixture(release, ns string) vtClusterFixture {
	insertLabels := componentLabels("vtinsert")
	oldInsertDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vtinsert", Namespace: ns, Labels: insertLabels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vtinsert"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "old-vtinsert"}}},
		},
	}
	oldInsertSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vtinsert", Namespace: ns, Labels: insertLabels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "old-vtinsert"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 10481}},
		},
	}

	selectLabels := componentLabels("vtselect")
	oldSelectDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vtselect", Namespace: ns, Labels: selectLabels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vtselect"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "old-vtselect"}}},
		},
	}
	oldSelectSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vtselect", Namespace: ns, Labels: selectLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vtselect"}},
	}

	storageLabels := componentLabels("vtstorage")
	oldStorageSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vtstorage", Namespace: ns, Labels: storageLabels},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vtstorage"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "old-vtstorage"}}},
		},
	}
	oldStorageSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vtstorage", Namespace: ns, Labels: storageLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vtstorage"}},
	}
	var storagePVs []*corev1.PersistentVolume
	var storagePVCs []*corev1.PersistentVolumeClaim
	for i := range 2 {
		pvName := fmt.Sprintf("pv-storage-%d", i)
		pvcName := fmt.Sprintf("data-old-vtstorage-%d", i)
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

	target := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1.VTClusterSpec{
			Storage: &vmv1.VTStorage{
				Storage:          &vmv1beta1.StorageSpec{},
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
			},
			Select: &vmv1.VTSelect{},
			Insert: &vmv1.VTInsert{},
		},
	}

	return vtClusterFixture{
		oldInsertDep:  oldInsertDep,
		oldInsertSvc:  oldInsertSvc,
		oldSelectDep:  oldSelectDep,
		oldSelectSvc:  oldSelectSvc,
		oldStorageSTS: oldStorageSTS,
		oldStorageSvc: oldStorageSvc,
		storagePVs:    storagePVs,
		storagePVCs:   storagePVCs,
		target:        target,
	}
}

func (f vtClusterFixture) objects() []client.Object {
	objs := []client.Object{f.oldInsertDep, f.oldInsertSvc, f.oldSelectDep, f.oldSelectSvc, f.oldStorageSTS, f.oldStorageSvc}
	for _, pv := range f.storagePVs {
		objs = append(objs, pv)
	}
	for _, pvc := range f.storagePVCs {
		objs = append(objs, pvc)
	}
	return objs
}

func TestClusterEngine(t *testing.T) {
	target := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1.VTClusterSpec{
			Storage: &vmv1.VTStorage{Storage: &vmv1beta1.StorageSpec{}},
			Select:  &vmv1.VTSelect{},
			Insert:  &vmv1.VTInsert{},
		},
	}

	engine := clusterEngine()
	assert.Equal(t, "vt", engine.ComponentPrefix)

	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentStorage))
	assert.Equal(t, "vtstorage-db", engine.PVCTemplateName(target, vmv1beta1.ClusterComponentStorage))

	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentSelect))
	assert.Empty(t, engine.PVCTemplateName(target, vmv1beta1.ClusterComponentSelect))

	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentInsert))
	assert.Empty(t, engine.PVCTemplateName(target, vmv1beta1.ClusterComponentInsert))
}

func TestClusterEngine_BuildAgentWriteURL_UsesTargetOwnPathPrefix(t *testing.T) {
	target := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1.VTClusterSpec{
			Insert: &vmv1.VTInsert{CommonAppsParams: vmv1beta1.CommonAppsParams{ExtraArgs: map[string]string{"http.pathPrefix": "/target-prefix"}}},
		},
	}

	engine := clusterEngine()
	got := engine.BuildAgentWriteURL(target, "http://vtinsert-myrelease.default.svc:10481/insert/native")
	assert.Equal(t, "http://vtinsert-myrelease.default.svc:10481/target-prefix/internal/insert", got)
}

func TestClusterEngine_DisabledComponent(t *testing.T) {
	target := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1.VTClusterSpec{
			Storage: &vmv1.VTStorage{Storage: &vmv1beta1.StorageSpec{}},
			Insert:  &vmv1.VTInsert{},
		},
	}

	engine := clusterEngine()
	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentStorage))
	assert.False(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentSelect))
	assert.True(t, engine.ComponentEnabled(target, vmv1beta1.ClusterComponentInsert))
}

func TestNoDowntimeCluster(t *testing.T) {
	synctest.Test(t, testNoDowntimeCluster)
}

func testNoDowntimeCluster(t *testing.T) {
	release := "myrelease"
	ns := "default"

	testHTTPClient := migratetest.HandlerClient(migratetest.AdminHandler(vmv1.VTAgentQueueMetricName))

	f := newVTClusterFixture(release, ns)
	target := f.target

	var initialWriteURL string
	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1.AddToScheme)).
		WithStatusSubresource(&vmv1.VTCluster{}, &vmv1.VTAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(f.objects()...).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if agent, ok := obj.(*vmv1.VTAgent); ok && initialWriteURL == "" && len(agent.Spec.RemoteWrite) > 0 {
					initialWriteURL = agent.Spec.RemoteWrite[0].URL
				}
				return cl.Create(ctx, obj, opts...)
			},
		}).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1.VTCluster { return &vmv1.VTCluster{} })
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: agentName, Namespace: ns}, func() *vmv1.VTAgent { return &vmv1.VTAgent{} })
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, "data-old-vtstorage-0-migration-snapshot")
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, "data-old-vtstorage-1-migration-snapshot")
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: "vtstorage-db-vtstorage-myrelease-0", Namespace: ns})
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: "vtstorage-db-vtstorage-myrelease-1", Namespace: ns})
	go migratetest.SimulateAgentEndpoints(ctx, t, c, types.NamespacedName{Name: agentName, Namespace: ns}, adminAddr, 10481, func() *vmv1.VTAgent { return &vmv1.VTAgent{} })
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentInsert), adminAddr, 10481)
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentSelect), adminAddr, 10481)

	err := NoDowntimeCluster(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVTCluster,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	assert.Equal(t, bufferAgentWriteURL(target.GetRemoteWriteURL(), ""), initialWriteURL)

	var checkInsertDep, checkSelectDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vtinsert", Namespace: ns}, &checkInsertDep))
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vtselect", Namespace: ns}, &checkSelectDep))
	var checkStorageSTS appsv1.StatefulSet
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vtstorage", Namespace: ns}, &checkStorageSTS))
	var checkPVC0, checkPVC1 corev1.PersistentVolumeClaim
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "data-old-vtstorage-0", Namespace: ns}, &checkPVC0))
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: "data-old-vtstorage-1", Namespace: ns}, &checkPVC1))

	var createdTarget vmv1.VTCluster
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	var newPVC0, newPVC1 corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vtstorage-db-vtstorage-myrelease-0", Namespace: ns}, &newPVC0))
	require.NotNil(t, newPVC0.Spec.DataSource)
	assert.Equal(t, "data-old-vtstorage-0-migration-snapshot", newPVC0.Spec.DataSource.Name)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vtstorage-db-vtstorage-myrelease-1", Namespace: ns}, &newPVC1))
	require.NotNil(t, newPVC1.Spec.DataSource)
	assert.Equal(t, "data-old-vtstorage-1-migration-snapshot", newPVC1.Spec.DataSource.Name)

	var agent vmv1.VTAgent
	err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))

	var svcInsert, svcSelect corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vtinsert", Namespace: ns}, &svcInsert))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentInsert), svcInsert.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vtselect", Namespace: ns}, &svcSelect))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentSelect), svcSelect.Spec.Selector)
}

func TestWithDowntimeCluster(t *testing.T) {
	synctest.Test(t, testWithDowntimeCluster)
}

func testWithDowntimeCluster(t *testing.T) {
	release := "myrelease"
	ns := "default"

	f := newVTClusterFixture(release, ns)
	target := f.target

	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1.AddToScheme)).
		WithStatusSubresource(&vmv1.VTCluster{}, &vmv1.VTAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(f.objects()...).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1.VTCluster { return &vmv1.VTCluster{} })
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: "vtstorage-db-vtstorage-myrelease-0", Namespace: ns})
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: "vtstorage-db-vtstorage-myrelease-1", Namespace: ns})
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: agentName, Namespace: ns}, func() *vmv1.VTAgent { return &vmv1.VTAgent{} })
	go migratetest.SimulateAgentEndpoints(ctx, t, c, types.NamespacedName{Name: agentName, Namespace: ns}, adminAddr, 10481, func() *vmv1.VTAgent { return &vmv1.VTAgent{} })
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentSelect), adminAddr, 10481)
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentInsert), adminAddr, 10481)

	testHTTPClient := migratetest.HandlerClient(migratetest.AdminHandler(vmv1.VTAgentQueueMetricName))
	err := WithDowntimeCluster(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVTCluster,
		Strategy:    migrate.StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	var checkDep appsv1.Deployment
	assert.True(t, k8serrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "old-vtinsert", Namespace: ns}, &checkDep)))
	assert.True(t, k8serrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "old-vtselect", Namespace: ns}, &checkDep)))
	var checkSTS appsv1.StatefulSet
	assert.True(t, k8serrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "old-vtstorage", Namespace: ns}, &checkSTS)))

	var newPVC0, newPVC1 corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vtstorage-db-vtstorage-myrelease-0", Namespace: ns}, &newPVC0))
	assert.Equal(t, "pv-storage-0", newPVC0.Spec.VolumeName)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vtstorage-db-vtstorage-myrelease-1", Namespace: ns}, &newPVC1))
	assert.Equal(t, "pv-storage-1", newPVC1.Spec.VolumeName)

	var createdTarget vmv1.VTCluster
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	var agent vmv1.VTAgent
	err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))

	var svcInsert, svcSelect, svcStorage corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vtinsert", Namespace: ns}, &svcInsert))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentInsert), svcInsert.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vtselect", Namespace: ns}, &svcSelect))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentSelect), svcSelect.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vtstorage", Namespace: ns}, &svcStorage))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentStorage), svcStorage.Spec.Selector)
}
