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

func setPVCTimeouts(t *testing.T) {
	t.Helper()
	origDeletePoll, origDeleteTimeout := PVCDeletePollInterval, PVCDeleteTimeout
	origBindPoll, origBindTimeout := PVCBindPollInterval, PVCBindTimeout
	origReclaimRestorePoll, origReclaimRestoreTimeout := ReclaimPolicyRestorePollInterval, ReclaimPolicyRestoreTimeout
	origReclaimPropagationPoll, origReclaimPropagationTimeout := ReclaimPolicyPropagationPollInterval, ReclaimPolicyPropagationTimeout
	PVCDeletePollInterval, PVCDeleteTimeout = 10*time.Millisecond, time.Second
	PVCBindPollInterval, PVCBindTimeout = 10*time.Millisecond, time.Second
	ReclaimPolicyRestorePollInterval, ReclaimPolicyRestoreTimeout = 10*time.Millisecond, time.Second
	ReclaimPolicyPropagationPollInterval, ReclaimPolicyPropagationTimeout = 5*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() {
		PVCDeletePollInterval, PVCDeleteTimeout = origDeletePoll, origDeleteTimeout
		PVCBindPollInterval, PVCBindTimeout = origBindPoll, origBindTimeout
		ReclaimPolicyRestorePollInterval, ReclaimPolicyRestoreTimeout = origReclaimRestorePoll, origReclaimRestoreTimeout
		ReclaimPolicyPropagationPollInterval, ReclaimPolicyPropagationTimeout = origReclaimPropagationPoll, origReclaimPropagationTimeout
	})
}

func simulateBind(ctx context.Context, t *testing.T, c client.Client, nsn types.NamespacedName) {
	for {
		var pvc corev1.PersistentVolumeClaim
		if err := c.Get(ctx, nsn, &pvc); err == nil {
			pvc.Status.Phase = corev1.ClaimBound
			if err := c.Status().Update(ctx, &pvc); err != nil && ctx.Err() == nil {
				t.Errorf("simulateBind: cannot mark PVC %s Bound: %v", nsn, err)
			}
			return
		} else if !k8serrors.IsNotFound(err) {
			if ctx.Err() == nil {
				t.Errorf("simulateBind: cannot get PVC %s: %v", nsn, err)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func bindPVCOnCreate(nsn types.NamespacedName) interceptor.Funcs {
	return interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if err := cl.Create(ctx, obj, opts...); err != nil {
				return err
			}
			pvc, ok := obj.(*corev1.PersistentVolumeClaim)
			if !ok || pvc.Name != nsn.Name || pvc.Namespace != nsn.Namespace {
				return nil
			}
			createOpts := &client.CreateOptions{}
			createOpts.ApplyOptions(opts)
			if len(createOpts.DryRun) > 0 {
				return nil
			}
			pvc.Status.Phase = corev1.ClaimBound
			return cl.Status().Update(ctx, pvc)
		},
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

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	targetNsn := types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(bindPVCOnCreate(targetNsn)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got.Name)
	assert.Equal(t, "pv-data", got.Spec.VolumeName)

	var check corev1.PersistentVolumeClaim
	err = c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &check)
	assert.True(t, k8serrors.IsNotFound(err))

	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimPolicy(""), gotPV.Spec.PersistentVolumeReclaimPolicy)

	got2, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got2.Name)
}

func TestRebindPVC_SameIdentityMarksAnnotation(t *testing.T) {
	setPVCTimeouts(t)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			ClaimRef: &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "myrelease-data"},
		},
	}
	sourcePVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease-data", Namespace: "default"},
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
	require.NoError(t, c.Create(ctx, sourcePVC))
	sourcePVC.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, sourcePVC))

	got, err := rebindPVC(ctx, c, sourcePVC, "myrelease-data", "default", nil)
	require.NoError(t, err)
	assert.Equal(t, "default/myrelease-data", got.Annotations[reboundFromAnnotation])
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
	targetNsn := types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(bindPVCOnCreate(targetNsn)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	require.NoError(t, err)

	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimDelete, gotPV.Spec.PersistentVolumeReclaimPolicy)
}

func TestCaptureOriginalReclaimPolicy_ReappliesRetainIfReverted(t *testing.T) {
	setPVCTimeouts(t)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pv-data",
			Annotations: map[string]string{originalReclaimPolicyAnnotation: string(corev1.PersistentVolumeReclaimDelete)},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))

	original, err := captureOriginalReclaimPolicy(ctx, c, pv)
	require.NoError(t, err)
	assert.Equal(t, corev1.PersistentVolumeReclaimDelete, original)

	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimRetain, gotPV.Spec.PersistentVolumeReclaimPolicy)
	assert.Equal(t, string(corev1.PersistentVolumeReclaimDelete), gotPV.Annotations[originalReclaimPolicyAnnotation])
}

func TestCaptureOriginalReclaimPolicy_AlreadyRetainWithAnnotationIsNoop(t *testing.T) {
	setPVCTimeouts(t)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pv-data",
			Annotations: map[string]string{originalReclaimPolicyAnnotation: string(corev1.PersistentVolumeReclaimDelete)},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	rejectUpdate := interceptor.Funcs{
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			return fmt.Errorf("unexpected Update call: %v", obj)
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(rejectUpdate).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))

	original, err := captureOriginalReclaimPolicy(ctx, c, pv)
	require.NoError(t, err)
	assert.Equal(t, corev1.PersistentVolumeReclaimDelete, original)
}

func TestRestorePVReclaimPolicy_RemovesMarkerWhenOriginalIsRetain(t *testing.T) {
	setPVCTimeouts(t)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pv-data",
			Annotations: map[string]string{originalReclaimPolicyAnnotation: string(corev1.PersistentVolumeReclaimRetain)},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))

	require.NoError(t, restorePVReclaimPolicy(ctx, c, "pv-data", corev1.PersistentVolumeReclaimRetain))

	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimRetain, gotPV.Spec.PersistentVolumeReclaimPolicy)
	_, hasMarker := gotPV.Annotations[originalReclaimPolicyAnnotation]
	assert.False(t, hasMarker, "restorePVReclaimPolicy should remove the pending-restore marker even when the original policy is Retain")
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
	targetNsn := types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(bindPVCOnCreate(targetNsn)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	require.NoError(t, err)
	require.NotNil(t, got.Spec.VolumeMode)
	assert.Equal(t, corev1.PersistentVolumeBlock, *got.Spec.VolumeMode)
}

func TestRebindPVC_PreservesPVStorageClassWhenSourceHasNone(t *testing.T) {
	setPVCTimeouts(t)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:         corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			StorageClassName: "manual",
			ClaimRef:         &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "old-release-data"},
		},
	}
	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "old-release-data", Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:       "pv-data",
			StorageClassName: nil,
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	targetNsn := types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(bindPVCOnCreate(targetNsn)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	require.NoError(t, err)
	require.NotNil(t, got.Spec.StorageClassName)
	assert.Equal(t, "manual", *got.Spec.StorageClassName)
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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	assert.Error(t, err)

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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	assert.Error(t, err)
}

func TestRebindPVC_ClaimRefMismatchIsAnError(t *testing.T) {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	assert.Error(t, err)

	var checkPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &checkPVC))
	var checkPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &checkPV))
	assert.Empty(t, checkPV.Spec.PersistentVolumeReclaimPolicy)
}

func TestRebindPVC_ResumesAfterClaimRefRepointedButTargetMissing(t *testing.T) {
	setPVCTimeouts(t)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			ClaimRef: &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "vmsingle-newrelease"},
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
	targetNsn := types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(bindPVCOnCreate(targetNsn)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got.Name)
	assert.Equal(t, "pv-data", got.Spec.VolumeName)

	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimPolicy(""), gotPV.Spec.PersistentVolumeReclaimPolicy)
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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	assert.Error(t, err)

	var checkPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &checkPVC))
	var checkPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &checkPV))
	require.NotNil(t, checkPV.Spec.ClaimRef)
	assert.Equal(t, "old-release-data", checkPV.Spec.ClaimRef.Name)
	assert.Empty(t, checkPV.Spec.PersistentVolumeReclaimPolicy)
}

func TestRebindPVC_AppliesPVCLabels(t *testing.T) {
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

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	targetNsn := types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(bindPVCOnCreate(targetNsn)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	wantLabels := map[string]string{"app.kubernetes.io/name": "vmstorage"}
	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", wantLabels)
	require.NoError(t, err)
	assert.Equal(t, wantLabels, got.Labels)
}

func TestRebindPVC_MismatchedReboundFromAnnotationIsAnError(t *testing.T) {
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
	unrelatedTarget := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vmsingle-newrelease", Namespace: "default",
			Annotations: map[string]string{reboundFromAnnotation: "default/some-other-release-data"},
		},
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
	require.NoError(t, c.Create(ctx, unrelatedTarget))
	unrelatedTarget.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, unrelatedTarget))

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	assert.ErrorContains(t, err, "some-other-release-data")
}

func TestRebindPVC_BoundTargetWithoutMarkerAndMismatchedClaimRefIsAnError(t *testing.T) {
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
	unmarkedTarget := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-newrelease", Namespace: "default"},
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
	require.NoError(t, c.Create(ctx, unmarkedTarget))
	unmarkedTarget.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, unmarkedTarget))

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	assert.ErrorContains(t, err, "claimRef doesn't point back to it")

	var checkPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &checkPVC))
}

func TestRebindPVC_PendingTargetWithMismatchedAnnotationIsAnErrorBeforeAnyDeletion(t *testing.T) {
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
	pendingTarget := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-newrelease", Namespace: "default"},
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
	require.NoError(t, c.Create(ctx, pendingTarget))

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	assert.Error(t, err)

	var checkSource corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &checkSource))
	var checkPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &checkPV))
	require.NotNil(t, checkPV.Spec.ClaimRef)
	assert.Equal(t, "old-release-data", checkPV.Spec.ClaimRef.Name)
}

func bindOnClaimRefRepointed(targetNsn types.NamespacedName) interceptor.Funcs {
	return interceptor.Funcs{
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if err := cl.Update(ctx, obj, opts...); err != nil {
				return err
			}
			pv, ok := obj.(*corev1.PersistentVolume)
			if !ok || pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.Namespace != targetNsn.Namespace || pv.Spec.ClaimRef.Name != targetNsn.Name {
				return nil
			}
			var pvc corev1.PersistentVolumeClaim
			if err := cl.Get(ctx, targetNsn, &pvc); err != nil {
				return nil
			}
			pvc.Status.Phase = corev1.ClaimBound
			return cl.Status().Update(ctx, &pvc)
		},
	}
}

func TestRebindPVC_ResumesAfterTargetCreatedButSourceNotYetDeleted(t *testing.T) {
	setPVCTimeouts(t)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pv-data",
			Annotations: map[string]string{originalReclaimPolicyAnnotation: string(corev1.PersistentVolumeReclaimDelete)},
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
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
	targetPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vmsingle-newrelease", Namespace: "default",
			Annotations: map[string]string{reboundFromAnnotation: "default/old-release-data"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
		},
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	targetNsn := types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(bindOnClaimRefRepointed(targetNsn)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	require.NoError(t, c.Status().Update(ctx, oldPVC))
	require.NoError(t, c.Create(ctx, targetPVC))

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default", nil)
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got.Name)
	assert.Equal(t, corev1.ClaimBound, got.Status.Phase)

	var checkSource corev1.PersistentVolumeClaim
	err = c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &checkSource)
	assert.True(t, k8serrors.IsNotFound(err))

	var checkPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &checkPV))
	require.NotNil(t, checkPV.Spec.ClaimRef)
	assert.Equal(t, "vmsingle-newrelease", checkPV.Spec.ClaimRef.Name)
}
