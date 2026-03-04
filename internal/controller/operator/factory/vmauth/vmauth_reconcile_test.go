package vmauth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
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
		cr     *vmv1beta1.VMAuth
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAuth)
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

		err := CreateOrUpdate(ctx, args.cr, fclient)
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

	name := "example"
	namespace := "default"
	vmauthName := types.NamespacedName{Namespace: namespace, Name: "vmauth-" + name}
	configSecretName := types.NamespacedName{Namespace: namespace, Name: "vmauth-config-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	setupReadyVMAuth := func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAuth) {
		// Create objects first
		assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))

		// clear actions
		c.Actions = nil
	}

	// create vmauth with default config
	f(args{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: objectMeta,
			Spec:       vmv1beta1.VMAuthSpec{},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmauthName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmauthName},
				{Verb: "Get", Kind: "Role", Resource: vmauthName},
				{Verb: "Create", Kind: "Role", Resource: vmauthName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmauthName},
				{Verb: "Create", Kind: "RoleBinding", Resource: vmauthName},
				{Verb: "Get", Kind: "Service", Resource: vmauthName},
				{Verb: "Create", Kind: "Service", Resource: vmauthName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmauthName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmauthName},
				// Config secret
				{Verb: "Get", Kind: "Secret", Resource: configSecretName},
				{Verb: "Create", Kind: "Secret", Resource: configSecretName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
				{Verb: "Create", Kind: "Deployment", Resource: vmauthName},
				{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
			},
		})

	// update vmauth with no changes
	f(args{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAuthSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: setupReadyVMAuth,
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmauthName},
				{Verb: "Get", Kind: "Role", Resource: vmauthName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmauthName},
				{Verb: "Get", Kind: "Service", Resource: vmauthName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmauthName},
				{Verb: "Get", Kind: "Secret", Resource: configSecretName},
				{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
				{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
			},
		})

	// no update on status change
	f(args{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAuthSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAuth) {
			setupReadyVMAuth(ctx, c, cr)
			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		},
	}, want{
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ServiceAccount", Resource: vmauthName},
			{Verb: "Get", Kind: "Role", Resource: vmauthName},
			{Verb: "Get", Kind: "RoleBinding", Resource: vmauthName},
			{Verb: "Get", Kind: "Service", Resource: vmauthName},
			{Verb: "Get", Kind: "VMServiceScrape", Resource: vmauthName},
			{Verb: "Get", Kind: "Secret", Resource: configSecretName},
			{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
			{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
			{Verb: "Get", Kind: "PodDisruptionBudget", Resource: vmauthName},
			{Verb: "Get", Kind: "Ingress", Resource: vmauthName},
			{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: vmauthName},
		},
	})
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	// Create a paused VMAuth CR and test that it is not reconciled
	cr := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmauth",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAuthSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
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

	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	var deploy appsv1.Deployment
	err := fclient.Get(ctx, nsn, &deploy)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))

	// unpause and verify reconciliation
	cr.Spec.Paused = false
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
	err = fclient.Get(ctx, nsn, &deploy)
	assert.NoError(t, err)

	// pause and update replica count
	cr.Spec.Paused = true
	cr.Spec.ReplicaCount = ptr.To(int32(2))
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	// check that replicas count is not updated
	err = fclient.Get(ctx, nsn, &deploy)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), *deploy.Spec.Replicas)
}
