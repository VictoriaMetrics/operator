package vt

import (
	"context"
	"testing"
	"testing/synctest"
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
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
	"github.com/VictoriaMetrics/operator/internal/migrate/migratetest"
)

const adminAddr = "10.0.0.1"

func TestNoDowntimeSingleNode(t *testing.T) {
	synctest.Test(t, testNoDowntimeSingleNode)
}

func testNoDowntimeSingleNode(t *testing.T) {
	release := "myrelease"
	ns := "default"
	labels := migratetest.HelmLabels(release)

	testHTTPClient := migratetest.HandlerClient(migratetest.AdminHandler(vmv1.VTAgentQueueMetricName))

	oldPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
			},
			VolumeName: "pv-data",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": release}}},
	}
	oldSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": release},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 10428}},
		},
	}

	var initialWriteURL string
	c := fake.NewClientBuilder().WithScheme(migratetest.Scheme(vmv1.AddToScheme)).
		WithStatusSubresource(&vmv1.VTSingle{}, &vmv1.VTAgent{}, &corev1.PersistentVolumeClaim{}).
		WithObjects(oldPVC, dep, oldSvc).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if agent, ok := obj.(*vmv1.VTAgent); ok && initialWriteURL == "" && len(agent.Spec.RemoteWrite) > 0 {
					initialWriteURL = agent.Spec.RemoteWrite[0].URL
				}
				return cl.Create(ctx, obj, opts...)
			},
		}).
		Build()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	target := &vmv1.VTSingle{
		ObjectMeta: metav1.ObjectMeta{Name: release, Namespace: ns},
		Spec: vmv1.VTSingleSpec{
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}

	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: release, Namespace: ns}, func() *vmv1.VTSingle { return &vmv1.VTSingle{} })
	go migratetest.SimulateBind(ctx, t, c, types.NamespacedName{Name: target.PrefixedName(), Namespace: ns})
	go migratetest.SimulateVolumeSnapshotReady(ctx, t, c, ns, release+"-migration-snapshot")
	go migratetest.SimulateAgentEndpoints(ctx, t, c, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, adminAddr, 10428, func() *vmv1.VTAgent { return &vmv1.VTAgent{} })
	go migratetest.SimulateOperational(ctx, t, c, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, func() *vmv1.VTAgent { return &vmv1.VTAgent{} })
	migratetest.SimulateServiceEndpoints(ctx, t, c, ns, target.PrefixedName(), adminAddr, 10428)

	err := NoDowntimeSingleNode(ctx, c, testHTTPClient, migrate.Options{
		Chart:       migrate.ChartVTSingle,
		Strategy:    migrate.StrategyNoDowntime,
		Namespace:   ns,
		ReleaseName: release,
		Yes:         true,
	}, target)
	require.NoError(t, err)

	assert.Equal(t, bufferAgentWriteURL(target.GetRemoteWriteURL(), ""), initialWriteURL)

	var checkDep appsv1.Deployment
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkDep))
	var checkPVC corev1.PersistentVolumeClaim
	assert.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &checkPVC))

	var createdTarget vmv1.VTSingle
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &createdTarget))

	var newPVC corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "vtsingle-" + release, Namespace: ns}, &newPVC))
	require.NotNil(t, newPVC.Spec.DataSource)
	assert.Equal(t, release+"-migration-snapshot", newPVC.Spec.DataSource.Name)

	var agent vmv1.VTAgent
	err = c.Get(ctx, types.NamespacedName{Name: target.PrefixedName() + "-migration-buffer", Namespace: ns}, &agent)
	assert.True(t, k8serrors.IsNotFound(err))

	var svc corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: release, Namespace: ns}, &svc))
	assert.Equal(t, target.SelectorLabels(), svc.Spec.Selector)
}

func TestSingleNodeEngine_AgentMatches_ComparesTransformedURL(t *testing.T) {
	engine := singleNodeEngine()
	target := &vmv1.VTSingle{ObjectMeta: metav1.ObjectMeta{Name: "vtsingle-x", Namespace: "default"}}
	writeURL := engine.BuildAgentWriteURL(target, "http://vtsingle-x.default.svc:10428/insert/native")
	want, err := engine.BuildAgent("agent", "default", writeURL, "10Gi", "")
	require.NoError(t, err)

	existing := want.DeepCopy()
	assert.True(t, engine.AgentMatches(existing, want), "an agent built the same way as want must match, so resuming isn't blocked")

	stillUntransformed := want.DeepCopy()
	stillUntransformed.Spec.RemoteWrite[0].URL = "http://vtsingle-x.default.svc:10428/insert/native"
	assert.False(t, engine.AgentMatches(stillUntransformed, want), "an agent still pointed at the untransformed URL must not be treated as a match")
}

func TestSingleNodeEngine_AgentMatches_RejectsUndersizedQueue(t *testing.T) {
	engine := singleNodeEngine()
	want, err := engine.BuildAgent("agent", "default", "http://x", "20Gi", "")
	require.NoError(t, err)

	undersized, err := engine.BuildAgent("agent", "default", "http://x", "1Gi", "")
	require.NoError(t, err)
	assert.False(t, engine.AgentMatches(undersized, want), "an existing buffer with a smaller queue than requested must not be reused — it can fill up and lose writes")

	oversized, err := engine.BuildAgent("agent", "default", "http://x", "50Gi", "")
	require.NoError(t, err)
	assert.True(t, engine.AgentMatches(oversized, want), "an existing buffer with at least the requested queue size is safe to reuse")
}

func TestSingleNodeEngine_HasStorage_StorageDataPathDisablesSpecStorage(t *testing.T) {
	target := &vmv1.VTSingle{
		Spec: vmv1.VTSingleSpec{
			StorageDataPath: "/custom-data",
			Storage: &corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("10Gi")},
				},
			},
		},
	}

	engine := singleNodeEngine()
	assert.False(t, engine.HasStorage(target), "storageDataPath disables spec.storage — VTSingle never mounts the migrated PVC in that mode")
}
