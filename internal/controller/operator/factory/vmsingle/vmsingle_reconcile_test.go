package vmsingle

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
		cr     *vmv1beta1.VMSingle
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMSingle)
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

	name := "example-single"
	namespace := "default"
	vmsingleName := types.NamespacedName{Namespace: namespace, Name: "vmsingle-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	setupReadyVMSingle := func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMSingle) {
		// Create objects first
		assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))

		// clear actions
		c.Actions = nil
	}

	// create vmsingle with default config
	f(args{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: objectMeta,
			Spec:       vmv1beta1.VMSingleSpec{},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Get", Kind: "Service", Resource: vmsingleName},
				{Verb: "Create", Kind: "Service", Resource: vmsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmsingleName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmsingleName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Create", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
			},
		})

	// update vmsingle with no changes
	f(args{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: setupReadyVMSingle,
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Get", Kind: "Service", Resource: vmsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmsingleName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
			},
		})

	// no update on status change
	f(args{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMSingle) {
			setupReadyVMSingle(ctx, c, cr)

			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Get", Kind: "Service", Resource: vmsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
			},
		})
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	// Create a paused VMSingle CR and test that it is not reconciled
	cr := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-single",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMSingleSpec{
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

	var dep appsv1.Deployment
	err := fclient.Get(ctx, nsn, &dep)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))

	// unpause and verify reconciliation
	cr.Spec.Paused = false
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
	err = fclient.Get(ctx, nsn, &dep)
	assert.NoError(t, err)

	// pause and update replica count
	cr.Spec.Paused = true
	cr.Spec.ReplicaCount = ptr.To(int32(2))
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	// check that replicas count is not updated
	err = fclient.Get(ctx, nsn, &dep)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), *dep.Spec.Replicas)
}
