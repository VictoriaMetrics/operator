package migrate

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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// componentLabels mirrors discoverComponent's own label-set construction, for building test
// fixtures that only the intended component's discovery call should ever pick up.
func componentLabels(component string) map[string]string {
	l := helmLabels("myrelease")
	l["app.kubernetes.io/component"] = component
	return l
}

// vmClusterEngine mirrors vm.clusterEngine() (unexported there, so it's duplicated here rather
// than exported solely for test convenience).
func vmClusterEngine() ClusterEngine[*vmv1beta1.VMCluster, *vmv1beta1.VMAgent] {
	return ClusterEngine[*vmv1beta1.VMCluster, *vmv1beta1.VMAgent]{
		ComponentPrefix: "vm",
		ComponentEnabled: func(target *vmv1beta1.VMCluster, kind vmv1beta1.ClusterComponent) bool {
			switch kind {
			case vmv1beta1.ClusterComponentStorage:
				return target.Spec.VMStorage != nil
			case vmv1beta1.ClusterComponentSelect:
				return target.Spec.VMSelect != nil
			case vmv1beta1.ClusterComponentInsert:
				return target.Spec.VMInsert != nil
			}
			return false
		},
		PVCTemplateName: func(target *vmv1beta1.VMCluster, kind vmv1beta1.ClusterComponent) string {
			switch kind {
			case vmv1beta1.ClusterComponentStorage:
				if target.Spec.VMStorage != nil && target.Spec.VMStorage.Storage != nil {
					return target.Spec.VMStorage.GetStorageVolumeName()
				}
			case vmv1beta1.ClusterComponentSelect:
				if target.Spec.VMSelect != nil && target.Spec.VMSelect.StorageSpec != nil {
					return target.Spec.VMSelect.GetCacheMountVolumeName()
				}
			}
			return ""
		},
		QueueMetricName: vmv1beta1.VMAgentQueueMetricName,
		BuildAgent: func(name, namespace, writeURL, bufferSize string) (*vmv1beta1.VMAgent, error) {
			size, err := ParseAgentBufferSize(bufferSize)
			if err != nil {
				return nil, err
			}
			return &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: vmv1beta1.VMAgentSpec{
					CommonScrapeParams: vmv1beta1.CommonScrapeParams{IngestOnlyMode: ptr.To(true)},
					StatefulMode:       true,
					StatefulStorage: &vmv1beta1.StorageSpec{
						VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{corev1.ResourceStorage: size},
								},
							},
						},
					},
					RemoteWriteSettings: &vmv1beta1.VMAgentRemoteWriteSettings{UseMultiTenantMode: true},
					RemoteWrite:         []vmv1beta1.VMAgentRemoteWriteSpec{{URL: writeURL}},
				},
			}, nil
		},
		AgentMatches: func(existing *vmv1beta1.VMAgent, wantURL string) bool {
			return len(existing.Spec.RemoteWrite) == 1 && existing.Spec.RemoteWrite[0].URL == wantURL &&
				existing.Spec.RemoteWriteSettings != nil && existing.Spec.RemoteWriteSettings.UseMultiTenantMode &&
				existing.Spec.StatefulMode && existing.Spec.StatefulStorage != nil && existing.Spec.StatefulStorage.EmptyDir == nil &&
				!existing.Spec.StatefulStorage.VolumeClaimTemplate.Spec.Resources.Requests.Storage().IsZero() &&
				existing.Spec.IngestOnlyMode != nil && *existing.Spec.IngestOnlyMode
		},
		StorageReplicaCount: func(target *vmv1beta1.VMCluster) int32 {
			if target.Spec.VMStorage == nil {
				return 1
			}
			if target.Spec.VMStorage.ReplicaCount != nil {
				return *target.Spec.VMStorage.ReplicaCount
			}
			if target.Spec.VMStorage.HPA != nil {
				return target.Spec.VMStorage.HPA.GetMinReplicas()
			}
			return 1
		},
	}
}

func TestWithDowntimeCluster(t *testing.T) {
	synctest.Test(t, testWithDowntimeCluster)
}

func testWithDowntimeCluster(t *testing.T) {
	release := "myrelease"
	ns := "default"

	// vmstorage: StatefulSet with 2 ordinals, each with its own PV/PVC.
	storageLabels := componentLabels("vmstorage")
	oldStorageSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmstorage", Namespace: ns, Labels: storageLabels},
		Spec:       appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vmstorage"}}},
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

	// vmselect: StatefulSet with 1 ordinal and its own cache PV/PVC.
	selectLabels := componentLabels("vmselect")
	oldSelectSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmselect", Namespace: ns, Labels: selectLabels},
		Spec:       appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vmselect"}}},
	}
	oldSelectSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmselect", Namespace: ns, Labels: selectLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vmselect"}},
	}
	selectPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-select-0"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("5Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: "cache-old-vmselect-0"},
		},
	}
	selectPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "cache-old-vmselect-0", Namespace: ns, Labels: selectLabels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-select-0",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}

	// vminsert: stateless Deployment, no PVC.
	insertLabels := componentLabels("vminsert")
	oldInsertDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vminsert", Namespace: ns, Labels: insertLabels},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vminsert"}}},
	}
	oldInsertSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vminsert", Namespace: ns, Labels: insertLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vminsert"}},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	objs := []client.Object{oldStorageSTS, oldStorageSvc, oldSelectSTS, oldSelectSvc, oldInsertDep, oldInsertSvc, selectPV, selectPVC}
	for _, pv := range storagePVs {
		objs = append(objs, pv)
	}
	for _, pvc := range storagePVCs {
		objs = append(objs, pvc)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMCluster{}, &vmv1beta1.VMAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(objs...).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(2))},
				Storage:          &vmv1beta1.StorageSpec{},
			},
			VMSelect: &vmv1beta1.VMSelect{StorageSpec: &vmv1beta1.StorageSpec{}},
			VMInsert: &vmv1beta1.VMInsert{},
		},
	}
	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"

	go simulateOperational(ctx, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1beta1.VMCluster { return &vmv1beta1.VMCluster{} })
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-0", Namespace: ns})
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-1", Namespace: ns})
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmselect-cachedir-vmselect-myrelease-0", Namespace: ns})
	go simulateOperational(ctx, c, types.NamespacedName{Name: agentName, Namespace: ns}, func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} })
	go simulateVMAgentEndpointsReady(ctx, c, ns, agentName)
	simulateTargetEndpointsReady(ctx, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentSelect))
	simulateTargetEndpointsReady(ctx, c, ns, target.PrefixedName(vmv1beta1.ClusterComponentInsert))

	err := Cluster(ctx, c, handlerClient(testAdminHandler()), Options{
		Chart:       ChartVMCluster,
		Strategy:    StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target, vmClusterEngine(), false)
	require.NoError(t, err)

	// old workloads are gone.
	var checkSTS appsv1.StatefulSet
	assert.True(t, k8serrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "old-vmstorage", Namespace: ns}, &checkSTS)))
	assert.True(t, k8serrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "old-vmselect", Namespace: ns}, &checkSTS)))
	var checkDep appsv1.Deployment
	assert.True(t, k8serrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "old-vminsert", Namespace: ns}, &checkDep)))

	// new per-ordinal PVCs exist under the operator's naming, bound to the same PVs.
	var newPVC0, newPVC1, newSelectPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-0", Namespace: ns}, &newPVC0))
	assert.Equal(t, "pv-storage-0", newPVC0.Spec.VolumeName)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-1", Namespace: ns}, &newPVC1))
	assert.Equal(t, "pv-storage-1", newPVC1.Spec.VolumeName)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmselect-cachedir-vmselect-myrelease-0", Namespace: ns}, &newSelectPVC))
	assert.Equal(t, "pv-select-0", newSelectPVC.Spec.VolumeName)

	// target CR was created.
	var createdTarget vmv1beta1.VMCluster
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	// each component's old Service now points at the target's pods.
	var svcStorage, svcSelect, svcInsert corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vmstorage", Namespace: ns}, &svcStorage))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentStorage), svcStorage.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vmselect", Namespace: ns}, &svcSelect))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentSelect), svcSelect.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vminsert", Namespace: ns}, &svcInsert))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentInsert), svcInsert.Spec.Selector)
}

func TestWithDowntimeCluster_PreflightsBeforeAnyDelete(t *testing.T) {
	release := "myrelease"
	ns := "default"

	// vmstorage: valid - StatefulSet with a PVC, target has storage configured for it.
	storageLabels := componentLabels("vmstorage")
	oldStorageSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmstorage", Namespace: ns, Labels: storageLabels},
		Spec:       appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vmstorage"}}},
	}
	storagePV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-storage-0"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: "data-old-vmstorage-0"},
		},
	}
	storagePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data-old-vmstorage-0", Namespace: ns, Labels: storageLabels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-storage-0",
		},
	}

	// vmselect: invalid - has an existing PVC, but its ClusterComponentSpec (built below)
	// leaves TargetPVCTemplateName empty, as if the target CR had no storage configured for
	// it. Discovered after vmstorage, so it must fail before vmstorage is ever touched.
	selectLabels := componentLabels("vmselect")
	oldSelectSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmselect", Namespace: ns, Labels: selectLabels},
		Spec:       appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old-vmselect"}}},
	}
	selectPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-select-0"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("5Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: "cache-old-vmselect-0"},
		},
	}
	selectPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "cache-old-vmselect-0", Namespace: ns, Labels: selectLabels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-select-0",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMCluster{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(oldStorageSTS, storagePV, storagePVC, oldSelectSTS, selectPV, selectPVC).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{Storage: &vmv1beta1.StorageSpec{}},
			VMSelect:  &vmv1beta1.VMSelect{},
			VMInsert:  &vmv1beta1.VMInsert{},
		},
	}

	err := Cluster(ctx, c, http.DefaultClient, Options{
		Chart:       ChartVMCluster,
		Strategy:    StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target, vmClusterEngine(), false)
	assert.Error(t, err)

	// vmstorage must be untouched: preflight for vmselect must fail before any Delete runs
	// (select is discovered right after storage, and insert — enabled but with no fixtures at
	// all — is never reached).
	var checkSTS appsv1.StatefulSet
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vmstorage", Namespace: ns}, &checkSTS))
	var checkPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "data-old-vmstorage-0", Namespace: ns}, &checkPVC))
}

func TestWithDowntimeCluster_DeploymentWithPVCIsAnError(t *testing.T) {
	release := "myrelease"
	ns := "default"

	// vminsert is normally stateless, but if some chart shape puts a PVC on it, migration
	// must refuse rather than silently leave it unrebound and orphaned.
	insertLabels := componentLabels("vminsert")
	oldInsertDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vminsert", Namespace: ns, Labels: insertLabels},
	}
	insertPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-insert-0"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: "data-old-vminsert"},
		},
	}
	insertPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data-old-vminsert", Namespace: ns, Labels: insertLabels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-insert-0",
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMCluster{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(oldInsertDep, insertPV, insertPVC).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec:       vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}},
	}
	err := Cluster(ctx, c, http.DefaultClient, Options{
		Chart:       ChartVMCluster,
		Strategy:    StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target, vmClusterEngine(), false)
	assert.Error(t, err)

	// nothing must have been touched.
	var checkDep appsv1.Deployment
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vminsert", Namespace: ns}, &checkDep))
	var checkPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "data-old-vminsert", Namespace: ns}, &checkPVC))
}

func TestRebindClusterPVCs(t *testing.T) {
	ns := "default"

	newPVCPVPair := func(ordinalSuffix string) (*corev1.PersistentVolume, *corev1.PersistentVolumeClaim) {
		pvName := "pv-" + ordinalSuffix
		pvcName := "data-old-sts-" + ordinalSuffix
		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: pvName},
			Spec: corev1.PersistentVolumeSpec{
				Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: pvcName},
			},
		}
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: pvcName, Namespace: ns},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				VolumeName:  pvName,
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		}
		return pv, pvc
	}

	t.Run("contiguous ordinals rebind successfully", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			scheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(scheme))
			pv0, pvc0 := newPVCPVPair("0")
			pv1, pvc1 := newPVCPVPair("1")
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
				WithObjects(pv0, pvc0, pv1, pvc1).
				Build()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			go simulateBind(ctx, c, types.NamespacedName{Name: "template-newsts-0", Namespace: ns})
			go simulateBind(ctx, c, types.NamespacedName{Name: "template-newsts-1", Namespace: ns})

			err := rebindClusterPVCs(ctx, c, []corev1.PersistentVolumeClaim{*pvc1, *pvc0}, "template", "newsts", ns)
			require.NoError(t, err)

			var newPVC0, newPVC1 corev1.PersistentVolumeClaim
			require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "template-newsts-0", Namespace: ns}, &newPVC0))
			assert.Equal(t, "pv-0", newPVC0.Spec.VolumeName)
			require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "template-newsts-1", Namespace: ns}, &newPVC1))
			assert.Equal(t, "pv-1", newPVC1.Spec.VolumeName)
		})
	})

	t.Run("non-contiguous ordinals error out", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			scheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(scheme))
			pv0, pvc0 := newPVCPVPair("0")
			pv2, pvc2 := newPVCPVPair("2")
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
				WithObjects(pv0, pvc0, pv2, pvc2).
				Build()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			// ordinal 0 exists and is processed first — it must actually bind, so the walk
			// reaches (and fails on) the gap at ordinal 1, rather than timing out on ordinal 0's
			// own bind-wait first.
			go simulateBind(ctx, c, types.NamespacedName{Name: "template-newsts-0", Namespace: ns})

			err := rebindClusterPVCs(ctx, c, []corev1.PersistentVolumeClaim{*pvc0, *pvc2}, "template", "newsts", ns)
			assert.ErrorContains(t, err, "not contiguous")
		})
	})

	t.Run("PVC without an ordinal suffix errors out", func(t *testing.T) {
		badPVC := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "nodashes", Namespace: ns},
		}
		err := rebindClusterPVCs(context.Background(), nil, []corev1.PersistentVolumeClaim{badPVC}, "template", "newsts", ns)
		assert.ErrorContains(t, err, "ordinal suffix")
	})

	t.Run("two PVCs resolving to the same ordinal errors out", func(t *testing.T) {
		// e.g. a StatefulSet with two volumeClaimTemplates ("data" and "cache") both produce
		// a "-0" suffixed PVC for pod 0 - pvcsByOrdinal can't represent more than one PVC per
		// ordinal, so it must reject this rather than silently keeping only one.
		dataPVC := corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data-old-sts-0", Namespace: ns}}
		cachePVC := corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "cache-old-sts-0", Namespace: ns}}
		err := rebindClusterPVCs(context.Background(), nil, []corev1.PersistentVolumeClaim{dataPVC, cachePVC}, "template", "newsts", ns)
		assert.ErrorContains(t, err, "ordinal 0")
	})
}
