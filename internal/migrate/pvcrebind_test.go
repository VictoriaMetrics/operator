package migrate

import (
	"context"
	"fmt"
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
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// setPVCTimeouts shrinks the package-level PVC delete/bind poll intervals and timeouts for the
// duration of the test, restoring the originals via t.Cleanup so mutating them doesn't leak
// into other tests in the package.
func setPVCTimeouts(t *testing.T) {
	t.Helper()
	origDeletePoll, origDeleteTimeout := PVCDeletePollInterval, PVCDeleteTimeout
	origBindPoll, origBindTimeout := PVCBindPollInterval, PVCBindTimeout
	PVCDeletePollInterval, PVCDeleteTimeout = 10*time.Millisecond, time.Second
	PVCBindPollInterval, PVCBindTimeout = 10*time.Millisecond, time.Second
	t.Cleanup(func() {
		PVCDeletePollInterval, PVCDeleteTimeout = origDeletePoll, origDeleteTimeout
		PVCBindPollInterval, PVCBindTimeout = origBindPoll, origBindTimeout
	})
}

// simulateBind mimics a real PVC-binding controller, which the fake client doesn't have:
// once nsn's PVC appears, mark it Bound so callers waiting on rebindPVC's bind-poll succeed.
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
	setPVCTimeouts(t)

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
	// rebindPVC's Create call.
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

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got.Name)
	assert.Equal(t, "pv-data", got.Spec.VolumeName)

	// old PVC must be gone
	var check corev1.PersistentVolumeClaim
	err = c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &check)
	assert.True(t, k8serrors.IsNotFound(err))

	// PV must survive, bound to the new PVC's claim, and its reclaim policy restored to
	// whatever it was before rebindPVC's temporary switch to Retain (empty in this fixture).
	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimPolicy(""), gotPV.Spec.PersistentVolumeReclaimPolicy)

	// re-running against the already-created target PVC is a no-op, not an error.
	got2, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got2.Name)
}

func TestRebindPVC_RestoresOriginalReclaimPolicy(t *testing.T) {
	setPVCTimeouts(t)

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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)

	// the PV's original Delete policy must be restored, not left stuck on Retain.
	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimDelete, gotPV.Spec.PersistentVolumeReclaimPolicy)
}

func TestRebindPVC_CopiesVolumeMode(t *testing.T) {
	setPVCTimeouts(t)

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

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)
	require.NotNil(t, got.Spec.VolumeMode)
	assert.Equal(t, corev1.PersistentVolumeBlock, *got.Spec.VolumeMode)
}

func TestRebindPVC_CollidingTargetBoundToDifferentVolumeIsAnError(t *testing.T) {
	setPVCTimeouts(t)

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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	assert.Error(t, err)
}

func TestRebindPVC_TerminatingCollidingTargetIsAnError(t *testing.T) {
	setPVCTimeouts(t)

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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	assert.Error(t, err)
}

func TestRebindPVC_ClaimRefMismatchIsAnError(t *testing.T) {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			// claimRef points at a different PVC than the one we're about to rebind - e.g. an
			// unrelated claim raced to bind this PV between discovery and this call.
			ClaimRef: &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "someone-elses-claim"},
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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	assert.Error(t, err)

	// refusing to proceed must not delete the source PVC or touch the PV's reclaim policy.
	var checkPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &checkPVC))
	var checkPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &checkPV))
	assert.Empty(t, checkPV.Spec.PersistentVolumeReclaimPolicy)
}

func TestRebindPVC_TargetRejectedByDryRunIsAnError(t *testing.T) {
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

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	rejectPVCCreate := interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*corev1.PersistentVolumeClaim); ok && obj.GetName() == "vmsingle-newrelease" {
				return fmt.Errorf("simulated admission rejection")
			}
			return cl.Create(ctx, obj, opts...)
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(rejectPVCCreate).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	assert.Error(t, err)

	// the dry-run rejection must be caught before anything destructive happens: source PVC
	// intact, PV's claimRef and reclaim policy untouched.
	var checkPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &checkPVC))
	var checkPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &checkPV))
	require.NotNil(t, checkPV.Spec.ClaimRef)
	assert.Equal(t, "old-release-data", checkPV.Spec.ClaimRef.Name)
	assert.Empty(t, checkPV.Spec.PersistentVolumeReclaimPolicy)
}
