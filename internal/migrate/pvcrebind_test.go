package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
)

// simulateBind mimics a real PVC-binding controller, which the fake client doesn't have:
// once nsn's PVC appears, mark it Bound so callers waiting on RebindPVC's bind-poll succeed.
// It exits promptly once ctx is done, rather than leaking past its caller's test function.
func simulateBind(ctx context.Context, c client.Client, nsn types.NamespacedName) {
	for {
		var pvc corev1.PersistentVolumeClaim
		if err := c.Get(ctx, nsn, &pvc); err == nil {
			pvc.Status.Phase = corev1.ClaimBound
			_ = c.Status().Update(ctx, &pvc)
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

func TestRebindPVC(t *testing.T) {
	PVCDeletePollInterval = 10 * time.Millisecond
	PVCDeleteTimeout = time.Second
	PVCBindPollInterval = 10 * time.Millisecond
	PVCBindTimeout = time.Second

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			ClaimRef: &corev1.ObjectReference{
				Kind:      "PersistentVolumeClaim",
				Namespace: "default",
				Name:      "old-release-data",
			},
		},
	}
	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "old-release-data", Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}

	// A plain fake client (no k8stools interceptor) is used deliberately: that interceptor
	// auto-patches PVC status right after Create, which races simulateBind's own concurrent
	// Status().Update() below — whichever loses surfaces as a spurious Conflict error out of
	// RebindPVC's Create call.
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	// the fake client doesn't auto-populate status on Create; set it explicitly.
	oldPVC.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	// the fake client has no real PVC-binding controller, so simulate one: once the target
	// PVC shows up, mark it Bound.
	go simulateBind(ctx, c, types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"})

	got, err := RebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got.Name)
	assert.Equal(t, "pv-data", got.Spec.VolumeName)

	// old PVC must be gone
	var check corev1.PersistentVolumeClaim
	err = c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &check)
	assert.Error(t, err)

	// PV must survive, now Retain and bound to the new PVC's claim.
	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimRetain, gotPV.Spec.PersistentVolumeReclaimPolicy)

	// re-running against the already-created target PVC is a no-op, not an error.
	got2, err := RebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got2.Name)
}
