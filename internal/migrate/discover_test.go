package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestDiscover(t *testing.T) {
	release := "myrelease"
	ns := "default"
	labels := helmLabels(release)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
	}
	// an unrelated PVC in the same namespace without the release labels must be ignored.
	otherPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: ns},
	}

	c := k8stools.GetTestClientWithObjects([]runtime.Object{dep, svc, pvc, otherPVC})
	ctx := context.Background()

	d, err := Discover(ctx, c, ns, release)
	require.NoError(t, err)
	require.NotNil(t, d.Deployment)
	assert.Equal(t, release, d.Deployment.Name)
	require.Len(t, d.Services, 1)
	assert.Equal(t, release, d.Services[0].Name)
	require.Len(t, d.PVCs, 1)
	assert.Equal(t, release, d.PVCs[0].Name)

	got, err := d.SingleNodePVC()
	require.NoError(t, err)
	assert.Equal(t, release, got.Name)
}

func TestDiscover_MultipleDeploymentsIsAnError(t *testing.T) {
	release := "myrelease"
	ns := "default"
	labels := helmLabels(release)

	dep1 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: release + "-a", Namespace: ns, Labels: labels}}
	dep2 := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: release + "-b", Namespace: ns, Labels: labels}}

	c := k8stools.GetTestClientWithObjects([]runtime.Object{dep1, dep2})
	ctx := context.Background()

	_, err := Discover(ctx, c, ns, release)
	assert.Error(t, err)
}

func TestSingleNodePVC_NoneFoundIsAnError(t *testing.T) {
	d := &Discovered{}
	_, err := d.SingleNodePVC()
	assert.Error(t, err)
}

func TestDiscoverComponent(t *testing.T) {
	release := "myrelease"
	ns := "default"
	storageLabels := helmLabels(release)
	storageLabels["app.kubernetes.io/component"] = "vmstorage"
	selectLabels := helmLabels(release)
	selectLabels["app.kubernetes.io/component"] = "vmselect"

	storageSTS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "vmstorage", Namespace: ns, Labels: storageLabels}}
	selectSTS := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "vmselect", Namespace: ns, Labels: selectLabels}}
	storagePVC := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "vmstorage-0", Namespace: ns, Labels: storageLabels}}

	c := k8stools.GetTestClientWithObjects([]runtime.Object{storageSTS, selectSTS, storagePVC})
	ctx := context.Background()

	d, err := DiscoverComponent(ctx, c, ns, release, "vmstorage")
	require.NoError(t, err)
	require.Len(t, d.StatefulSets, 1)
	assert.Equal(t, "vmstorage", d.StatefulSets[0].Name)
	require.Len(t, d.PVCs, 1)
	assert.Equal(t, "vmstorage-0", d.PVCs[0].Name)

	sts, err := d.SingleStatefulSet()
	require.NoError(t, err)
	assert.Equal(t, "vmstorage", sts.Name)

	dSelect, err := DiscoverComponent(ctx, c, ns, release, "vmselect")
	require.NoError(t, err)
	require.Len(t, dSelect.StatefulSets, 1)
	assert.Equal(t, "vmselect", dSelect.StatefulSets[0].Name)
	assert.Empty(t, dSelect.PVCs)
}

func TestSingleStatefulSet_NoneFoundIsAnError(t *testing.T) {
	d := &Discovered{}
	_, err := d.SingleStatefulSet()
	assert.Error(t, err)
}
