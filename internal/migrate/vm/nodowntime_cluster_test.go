package vm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
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

// TestNoDowntimeCluster exercises the full VMCluster NoDowntime flow: vminsert is buffered
// and cut over, vmselect is cut over directly (nothing to buffer), and vmstorage's per-ordinal
// PVCs are snapshotted with no Service involved at all.
func TestNoDowntimeCluster(t *testing.T) {
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

	adminServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			fmt.Fprintln(w, `vm_persistentqueue_bytes_pending{path="/tmp/q"} 0`)
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

	insertLabels := componentLabels(release, "vminsert")
	oldInsertDep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "old-vminsert", Namespace: ns, Labels: insertLabels}}
	oldInsertSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vminsert", Namespace: ns, Labels: insertLabels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "old-vminsert"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 8480}},
		},
	}

	selectLabels := componentLabels(release, "vmselect")
	oldSelectSTS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "old-vmselect", Namespace: ns, Labels: selectLabels}}
	oldSelectSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vmselect", Namespace: ns, Labels: selectLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vmselect"}},
	}

	storageLabels := componentLabels(release, "vmstorage")
	oldStorageSTS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "old-vmstorage", Namespace: ns, Labels: storageLabels}}
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

	objs := []client.Object{oldInsertDep, oldInsertSvc, oldSelectSTS, oldSelectSvc, oldStorageSTS, oldStorageSvc}
	for _, pv := range storagePVs {
		objs = append(objs, pv)
	}
	for _, pvc := range storagePVCs {
		objs = append(objs, pvc)
	}
	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithStatusSubresource(&vmv1beta1.VMCluster{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(objs...).
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

	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	go simulateVMClusterOperational(ctx, c, types.NamespacedName{Name: release, Namespace: ns})
	go simulateVolumeSnapshotReady(ctx, c, ns, "data-old-vmstorage-0-migration-snapshot")
	go simulateVolumeSnapshotReady(ctx, c, ns, "data-old-vmstorage-1-migration-snapshot")
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-0", Namespace: ns})
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmstorage-db-vmstorage-myrelease-1", Namespace: ns})
	go simulateAgentEndpoints(ctx, c, ns, agentName, adminServer.URL)

	initialWriteURL := captureInitialAgentWriteURL(ctx, c, types.NamespacedName{Name: agentName, Namespace: ns})

	err := NoDowntimeCluster(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVMCluster,
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

	// old workloads and PVCs were never touched.
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

	// buffer agent and alias Service were deleted at the end.
	var agent vmv1beta1.VMAgent
	err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))
	var aliasSvc corev1.Service
	err = c.Get(ctx, types.NamespacedName{Name: target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-source", Namespace: ns}, &aliasSvc)
	assert.True(t, k8serrors.IsNotFound(err))

	// final Service selectors point at the target's pods.
	var svcInsert, svcSelect corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vminsert", Namespace: ns}, &svcInsert))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentInsert), svcInsert.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vmselect", Namespace: ns}, &svcSelect))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentSelect), svcSelect.Spec.Selector)
}
