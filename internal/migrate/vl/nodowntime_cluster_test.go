package vl

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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
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

func simulateVLClusterOperational(ctx context.Context, c client.Client, nsn types.NamespacedName) {
	for {
		var cr vmv1.VLCluster
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

// TestNoDowntimeCluster exercises the full VLCluster NoDowntime flow: vlinsert is buffered
// and cut over, vlselect is cut over directly (nothing to buffer), and vlstorage's per-ordinal
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
			fmt.Fprintln(w, `vlagent_remotewrite_pending_data_bytes{path="/tmp/q"} 0`)
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

	insertLabels := componentLabels(release, "vlinsert")
	oldInsertDep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "old-vlinsert", Namespace: ns, Labels: insertLabels}}
	oldInsertSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vlinsert", Namespace: ns, Labels: insertLabels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "old-vlinsert"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 9481}},
		},
	}

	selectLabels := componentLabels(release, "vlselect")
	oldSelectDep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "old-vlselect", Namespace: ns, Labels: selectLabels}}
	oldSelectSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "old-vlselect", Namespace: ns, Labels: selectLabels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "old-vlselect"}},
	}

	storageLabels := componentLabels(release, "vlstorage")
	oldStorageSTS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "old-vlstorage", Namespace: ns, Labels: storageLabels}}
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
	c := fake.NewClientBuilder().WithScheme(testScheme()).
		WithStatusSubresource(&vmv1.VLCluster{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(objs...).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target := &vmv1.VLCluster{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1.VLClusterSpec{
			VLStorage: &vmv1.VLStorage{Storage: &vmv1beta1.StorageSpec{}},
			VLSelect:  &vmv1.VLSelect{},
			VLInsert:  &vmv1.VLInsert{},
		},
	}

	agentName := target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-buffer"
	go simulateVLClusterOperational(ctx, c, types.NamespacedName{Name: release, Namespace: ns})
	go simulateVolumeSnapshotReady(ctx, c, ns, "data-old-vlstorage-0-migration-snapshot")
	go simulateVolumeSnapshotReady(ctx, c, ns, "data-old-vlstorage-1-migration-snapshot")
	go simulateBind(ctx, c, types.NamespacedName{Name: "vlstorage-db-vlstorage-myrelease-0", Namespace: ns})
	go simulateBind(ctx, c, types.NamespacedName{Name: "vlstorage-db-vlstorage-myrelease-1", Namespace: ns})
	go simulateVLAgentEndpoints(ctx, c, ns, agentName, adminServer.URL)

	initialWriteURL := captureInitialAgentWriteURL(ctx, c, types.NamespacedName{Name: agentName, Namespace: ns})

	err := NoDowntimeCluster(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVLCluster,
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

	// buffer agent and alias Service were deleted at the end.
	var agent vmv1.VLAgent
	err = c.Get(ctx, types.NamespacedName{Name: agentName, Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))
	var aliasSvc corev1.Service
	err = c.Get(ctx, types.NamespacedName{Name: target.PrefixedName(vmv1beta1.ClusterComponentInsert) + "-migration-source", Namespace: ns}, &aliasSvc)
	assert.True(t, k8serrors.IsNotFound(err))

	// final Service selectors point at the target's pods.
	var svcInsert, svcSelect corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vlinsert", Namespace: ns}, &svcInsert))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentInsert), svcInsert.Spec.Selector)
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-vlselect", Namespace: ns}, &svcSelect))
	assert.Equal(t, target.SelectorLabels(vmv1beta1.ClusterComponentSelect), svcSelect.Spec.Selector)
}
