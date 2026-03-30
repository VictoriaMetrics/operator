package vmalertmanager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1beta1.VMAlertmanager
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlertmanager)
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	f := func(args args, want want) {
		t.Helper()

		fclient := k8stools.GetTestClientWithActionsAndObjects(nil)
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(args.cr)

		if args.preRun != nil {
			args.preRun(ctx, fclient, args.cr)
		}

		err := CreateOrUpdateAlertManager(ctx, args.cr, fclient)
		if want.err != nil {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}

		if !assert.Equal(t, len(want.actions), len(fclient.Actions)) {
			for i, action := range fclient.Actions {
				t.Logf("Action %d: %s %s %s", i, action.Verb, action.Kind, action.Resource)
			}
		}

		for i, action := range want.actions {
			if i >= len(fclient.Actions) {
				break
			}
			assert.Equal(t, action.Verb, fclient.Actions[i].Verb, "idx %d verb", i)
			assert.Equal(t, action.Kind, fclient.Actions[i].Kind, "idx %d kind", i)
			assert.Equal(t, action.Resource, fclient.Actions[i].Resource, "idx %d resource", i)
		}
	}

	name := "vmalertmanager"
	namespace := "default"
	vmalertmanagerName := types.NamespacedName{Namespace: namespace, Name: "vmalertmanager-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	setupReadyVMAlertmanager := func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlertmanager) {
		// Create objects first
		_ = CreateOrUpdateAlertManager(ctx, cr, c)

		// Create pod for StatefulSet to simulate readiness
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmalertmanagerName.Name + "-0",
				Namespace: vmalertmanagerName.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "vmalertmanager",
					"app.kubernetes.io/instance":  name,
					"app.kubernetes.io/component": "monitoring",
					"managed-by":                  "vm-operator",
					"controller-revision-hash":    "v1",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "StatefulSet",
						Name:       vmalertmanagerName.Name,
						Controller: ptr.To(true),
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
		assert.NoError(t, c.Create(ctx, pod))

		// Update STS status
		sts := &appsv1.StatefulSet{}
		if err := c.Get(ctx, vmalertmanagerName, sts); err == nil {
			sts.Status.CurrentRevision = "v1"
			sts.Status.UpdateRevision = "v1"
			sts.Status.ObservedGeneration = sts.Generation
			sts.Status.Replicas = 1
			sts.Status.ReadyReplicas = 1
			_ = c.Status().Update(ctx, sts)
		}

		// clear actions
		c.Actions = nil
	}

	// create vmalertmanager with default config
	f(args{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: objectMeta,
			Spec:       vmv1beta1.VMAlertmanagerSpec{},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Role", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "Role", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "RoleBinding", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
			},
		})

	// update vmalertmanager with no changes
	f(args{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: setupReadyVMAlertmanager,
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Role", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
			},
		})

	// no update on status change
	f(args{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlertmanager) {
			setupReadyVMAlertmanager(ctx, c, cr)

			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "PodDisruptionBudget", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Role", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
			},
		})
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	cr := &vmv1beta1.VMAlertmanager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmalertmanager",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAlertmanagerSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
				Paused:       true,
			},
		},
	}
	nsn := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	assert.NoError(t, CreateOrUpdateAlertManager(ctx, cr, fclient))

	var sts appsv1.StatefulSet
	err := fclient.Get(ctx, nsn, &sts)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))

	// unpause and verify reconciliation
	cr.Spec.Paused = false
	assert.NoError(t, CreateOrUpdateAlertManager(ctx, cr, fclient))
	err = fclient.Get(ctx, nsn, &sts)
	assert.NoError(t, err)

	// pause and update replica count
	cr.Spec.Paused = true
	cr.Spec.ReplicaCount = ptr.To(int32(2))
	assert.NoError(t, CreateOrUpdateAlertManager(ctx, cr, fclient))

	// check that replicas count is not updated
	err = fclient.Get(ctx, nsn, &sts)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), *sts.Spec.Replicas)
}
