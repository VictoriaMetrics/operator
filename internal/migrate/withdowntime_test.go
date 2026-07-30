package migrate

import (
	"context"
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

// simulateVMSingleOperational mimics the operator's own reconcile loop marking a
// freshly-created VMSingle Operational, which the fake client won't do on its own. It exits
// promptly once ctx is done, rather than leaking past its caller's test function.
func simulateVMSingleOperational(ctx context.Context, c client.Client, nsn types.NamespacedName) {
	for {
		var cr vmv1beta1.VMSingle
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

func TestWithDowntimeSingleNode(t *testing.T) {
	PVCDeletePollInterval = 5 * time.Millisecond
	PVCDeleteTimeout = time.Second
	PVCBindPollInterval = 5 * time.Millisecond
	PVCBindTimeout = time.Second
	TargetReadyPollInterval = 5 * time.Millisecond
	TargetReadyTimeout = time.Second

	release := "myrelease"
	ns := "default"
	labels := helmLabels(release)

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-data"},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:    corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ClaimRef:    &corev1.ObjectReference{Kind: "PersistentVolumeClaim", Namespace: ns, Name: release},
		},
	}
	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeName:  "pv-data",
		},
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{Name: "data", VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: release},
						}},
						{Name: "config", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: release + "-config"}},
						}},
					},
				},
			},
		},
	}
	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": release}},
	}
	dependentCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: release + "-config", Namespace: ns, Labels: labels},
	}
	// co-labeled but NOT referenced by the pod spec — must survive.
	unrelatedCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: release + "-unrelated", Namespace: ns, Labels: labels},
	}

	// A plain fake client (no k8stools interceptor) is used deliberately: that interceptor
	// auto-patches PVC status right after Create, which races simulateBind's own concurrent
	// Status().Update() below — see pvcrebind_test.go.
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&vmv1beta1.VMSingle{}, &corev1.PersistentVolumeClaim{}).
		WithRuntimeObjects(pv, oldPVC, dep, oldSvc, dependentCM, unrelatedCM).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
	}

	go simulateBind(ctx, c, types.NamespacedName{Name: target.PrefixedName(), Namespace: ns})
	go simulateVMSingleOperational(ctx, c, types.NamespacedName{Name: release, Namespace: ns})

	err := WithDowntimeSingleNode(ctx, c, Options{
		Chart:       ChartVMSingle,
		Strategy:    StrategyWithDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	// old Deployment is gone.
	var checkDep appsv1.Deployment
	err = c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkDep)
	assert.True(t, k8serrors.IsNotFound(err))

	// referenced ConfigMap is gone, unrelated one survives.
	var checkCM corev1.ConfigMap
	err = c.Get(ctx, types.NamespacedName{Name: release + "-config", Namespace: ns}, &checkCM)
	assert.True(t, k8serrors.IsNotFound(err))
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release + "-unrelated", Namespace: ns}, &checkCM))

	// new PVC exists under the operator's naming convention, bound to the same PV.
	var newPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vmsingle-" + release, Namespace: ns}, &newPVC))
	assert.Equal(t, "pv-data", newPVC.Spec.VolumeName)

	// target CR was created.
	var createdTarget vmv1beta1.VMSingle
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	// the release's Service now points at the target's pods.
	var svc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &svc))
	assert.Equal(t, target.SelectorLabels(), svc.Spec.Selector)
}
