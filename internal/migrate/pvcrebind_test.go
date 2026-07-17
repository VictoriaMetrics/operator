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
	oldPVC.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)
	assert.Equal(t, "vmsingle-newrelease", got.Name)
	assert.Equal(t, "pv-data", got.Spec.VolumeName)

	var check corev1.PersistentVolumeClaim
	err = c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &check)
	assert.True(t, k8serrors.IsNotFound(err))

	var gotPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &gotPV))
	assert.Equal(t, corev1.PersistentVolumeReclaimPolicy(""), gotPV.Spec.PersistentVolumeReclaimPolicy)

	got2, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
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

	got, err := rebindPVC(ctx, c, sourcePVC, "myrelease-data", "default")
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
	oldPVC.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, oldPVC))

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	require.NoError(t, err)

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
	targetNsn := types.NamespacedName{Name: "vmsingle-newrelease", Namespace: "default"}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.PersistentVolumeClaim{}).
		WithInterceptorFuncs(bindPVCOnCreate(targetNsn)).Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Create(ctx, pv))
	require.NoError(t, c.Create(ctx, oldPVC))
	oldPVC.Status.Phase = corev1.ClaimBound
	require.NoError(t, c.Status().Update(ctx, oldPVC))

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

	got, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
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

	_, err := rebindPVC(ctx, c, oldPVC, "vmsingle-newrelease", "default")
	assert.Error(t, err)

	var checkPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "old-release-data", Namespace: "default"}, &checkPVC))
	var checkPV corev1.PersistentVolume
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "pv-data"}, &checkPV))
	require.NotNil(t, checkPV.Spec.ClaimRef)
	assert.Equal(t, "old-release-data", checkPV.Spec.ClaimRef.Name)
	assert.Empty(t, checkPV.Spec.PersistentVolumeReclaimPolicy)
}
