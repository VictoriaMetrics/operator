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
	assert.True(t, k8serrors.IsNotFound(err))

	// PV must survive, bound to the new PVC's claim, and its reclaim policy restored to
	// whatever it was before RebindPVC's temporary switch to Retain (empty in this fixture).
	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimPolicy(""), gotPV.Spec.PersistentVolumeReclaimPolicy)

	// re-running against the already-created target PVC is a no-op, not an error.
	got2, err := RebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got2.Name)
}

func TestRebindPVC_RestoresOriginalReclaimPolicy(t *testing.T) {
	PVCDeletePollInterval = 10 * time.Millisecond
	PVCDeleteTimeout = time.Second
	PVCBindPollInterval = 10 * time.Millisecond
	PVCBindTimeout = time.Second

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			ClaimRef:                      &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "old-release-data"},
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

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	oldPVC.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	go simulateBind(ctx, c, types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"})

	_, err := RebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)

	// the PV's original Delete policy must be restored, not left stuck on Retain.
	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimDelete, gotPV.Spec.PersistentVolumeReclaimPolicy)
}

func TestRebindPVC_CopiesVolumeMode(t *testing.T) {
	PVCDeletePollInterval = 10 * time.Millisecond
	PVCDeleteTimeout = time.Second
	PVCBindPollInterval = 10 * time.Millisecond
	PVCBindTimeout = time.Second

	blockMode := corev1.PersistentVolumeBlock
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			ClaimRef: &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "old-release-data"},
		},
	}
	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "old-release-data", Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
			VolumeMode:  &blockMode,
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	oldPVC.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	go simulateBind(ctx, c, types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"})

	got, err := RebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)
	require.NotNil(t, got.Spec.VolumeMode)
	assert.Equal(t, corev1.PersistentVolumeBlock, *got.Spec.VolumeMode)
}

func TestRebindPVC_CollidingTargetBoundToDifferentVolumeIsAnError(t *testing.T) {
	PVCDeletePollInterval = 10 * time.Millisecond
	PVCDeleteTimeout = time.Second
	PVCBindPollInterval = 10 * time.Millisecond
	PVCBindTimeout = time.Second

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			ClaimRef: &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "old-release-data"},
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
	// an unrelated PVC already occupies the target name, bound to a completely different
	// PersistentVolume - e.g. left over from a prior migration attempt against another source.
	collidingTarget := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-newrelease", Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "some-other-pv",
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	require.NoError(t, c.Create(ctx, collidingTarget))

	_, err := RebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	assert.Error(t, err)

	// the source PVC must be left untouched - refusing to proceed must not delete it.
	var check corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &check))
}

func TestRebindPVC_UnboundSourceIsAnError(t *testing.T) {
	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "old-release-data", Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, oldPVC))

	_, err := RebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	assert.Error(t, err)
}

func TestRebindPVC_TerminatingCollidingTargetIsAnError(t *testing.T) {
	PVCDeletePollInterval = 10 * time.Millisecond
	PVCDeleteTimeout = time.Second
	PVCBindPollInterval = 10 * time.Millisecond
	PVCBindTimeout = time.Second

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			ClaimRef: &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "old-release-data"},
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
	terminatingTarget := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vmsingle-newrelease", Namespace: "default",
			Finalizers: []string{"kubernetes.io/pvc-protection"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	require.NoError(t, c.Create(ctx, terminatingTarget))
	require.NoError(t, c.Delete(ctx, terminatingTarget))

	_, err := RebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	assert.Error(t, err)
}
