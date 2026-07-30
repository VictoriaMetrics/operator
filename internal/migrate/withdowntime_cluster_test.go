package migrate

import (
	"context"
	"fmt"
	"testing"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// componentLabels mirrors DiscoverComponent's own label-set construction, for building test
// fixtures that only the intended component's discovery call should ever pick up.
func componentLabels(releaseName, component string) map[string]string {
	l := helmLabels(releaseName)
	l["app.kubernetes.io/component"] = component
	return l
}

func simulateVMClusterOperational(ctx context.Context, c client.Client, nsn types.NamespacedName) {
	for {
		var cr vmv1beta1.VMCluster
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

func TestWithDowntimeCluster(t *testing.T) {
	PVCDeletePollInterval = 5 * time.Millisecond
	PVCDeleteTimeout = time.Second
	PVCBindPollInterval = 5 * time.Millisecond
	PVCBindTimeout = time.Second
	TargetReadyPollInterval = 5 * time.Millisecond
	TargetReadyTimeout = time.Second

	release := "myrelease"
	ns := "default"

	// vmstorage: StatefulSet with 2 ordinals, each with its own PV/PVC.
	storageLabels := componentLabels(release, "vmstorage")
	oldStorageSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmstorage", Namespace: ns, Labels: storageLabels},
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
		})
	}

	// vmselect: StatefulSet with 1 ordinal and its own cache PV/PVC.
	selectLabels := componentLabels(release, "vmselect")
	oldSelectSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmselect", Namespace: ns, Labels: selectLabels},
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
	}

	// vminsert: stateless Deployment, no PVC.
	insertLabels := componentLabels(release, "vminsert")
	oldInsertDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vminsert", Namespace: ns, Labels: insertLabels},
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
		WithStatusSubresource(&vmv1beta1.VMCluster{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(objs...).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{Storage: &vmv1beta1.StorageSpec{}},
			VMSelect:  &vmv1beta1.VMSelect{StorageSpec: &vmv1beta1.StorageSpec{}},
			VMInsert:  &vmv1beta1.VMInsert{},
		},
	}

	go simulateVMClusterOperational(ctx, c, types.NamespacedName{Name: release, Namespace: ns})
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-0", Namespace: ns})
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-1", Namespace: ns})
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmselect-cachedir-vmselect-myrelease-0", Namespace: ns})

	components := []ClusterComponentSpec{
		{
			HelmComponentLabel:    "vmstorage",
			TargetWorkloadName:    target.PrefixedName(vmv1beta1.ClusterComponentStorage),
			TargetPVCTemplateName: target.Spec.VMStorage.GetStorageVolumeName(),
			TargetSelectorLabels:  target.SelectorLabels(vmv1beta1.ClusterComponentStorage),
		},
		{
			HelmComponentLabel:    "vmselect",
			TargetWorkloadName:    target.PrefixedName(vmv1beta1.ClusterComponentSelect),
			TargetPVCTemplateName: target.Spec.VMSelect.GetCacheMountVolumeName(),
			TargetSelectorLabels:  target.SelectorLabels(vmv1beta1.ClusterComponentSelect),
		},
		{
			HelmComponentLabel:   "vminsert",
			TargetWorkloadName:   target.PrefixedName(vmv1beta1.ClusterComponentInsert),
			TargetSelectorLabels: target.SelectorLabels(vmv1beta1.ClusterComponentInsert),
		},
	}

	err := WithDowntimeCluster(ctx, c, Options{
		Chart:       ChartVMCluster,
		Strategy:    StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target, components)
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
		}
		return pv, pvc
	}

	t.Run("contiguous ordinals rebind successfully", func(t *testing.T) {
		PVCDeletePollInterval = 5 * time.Millisecond
		PVCDeleteTimeout = time.Second
		PVCBindPollInterval = 5 * time.Millisecond
		PVCBindTimeout = time.Second

		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		pv0, pvc0 := newPVCPVPair("0")
		pv1, pvc1 := newPVCPVPair("1")
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
			WithObjects(pv0, pvc0, pv1, pvc1).
			Build()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	t.Run("non-contiguous ordinals error out", func(t *testing.T) {
		PVCDeletePollInterval = 5 * time.Millisecond
		PVCDeleteTimeout = time.Second
		PVCBindPollInterval = 5 * time.Millisecond
		PVCBindTimeout = time.Second

		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		pv0, pvc0 := newPVCPVPair("0")
		pv2, pvc2 := newPVCPVPair("2")
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
			WithObjects(pv0, pvc0, pv2, pvc2).
			Build()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// ordinal 0 exists and is processed first — it must actually bind, so the walk
		// reaches (and fails on) the gap at ordinal 1, rather than timing out on ordinal 0's
		// own bind-wait first.
		go simulateBind(ctx, c, types.NamespacedName{Name: "template-newsts-0", Namespace: ns})

		err := rebindClusterPVCs(ctx, c, []corev1.PersistentVolumeClaim{*pvc0, *pvc2}, "template", "newsts", ns)
		assert.ErrorContains(t, err, "not contiguous")
	})

	t.Run("PVC without an ordinal suffix errors out", func(t *testing.T) {
		badPVC := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "nodashes", Namespace: ns},
		}
		err := rebindClusterPVCs(context.Background(), nil, []corev1.PersistentVolumeClaim{badPVC}, "template", "newsts", ns)
		assert.ErrorContains(t, err, "ordinal suffix")
	})
}
