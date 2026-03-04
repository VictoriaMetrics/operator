package vtsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1.VTSingle
		preRun func(c *k8stools.ClientWithActions, cr *vmv1.VTSingle)
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
			args.preRun(fclient, args.cr)
		}

		err := CreateOrUpdate(ctx, fclient, args.cr)
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

	name := "example-vtsingle"
	namespace := "default"
	vtsingleName := types.NamespacedName{Namespace: namespace, Name: "vtsingle-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	// create vtsingle with default config
	f(args{
		cr: &vmv1.VTSingle{
			ObjectMeta: objectMeta,
			Spec:       vmv1.VTSingleSpec{},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vtsingleName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vtsingleName},
				{Verb: "Get", Kind: "Service", Resource: vtsingleName},
				{Verb: "Create", Kind: "Service", Resource: vtsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtsingleName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
				{Verb: "Create", Kind: "Deployment", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
			},
		})

	// update vtsingle with no changes
	f(args{
		cr: &vmv1.VTSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1.VTSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(c *k8stools.ClientWithActions, cr *vmv1.VTSingle) {
			ctx := context.TODO()
			// Create objects first
			assert.NoError(t, CreateOrUpdate(ctx, c, cr.DeepCopy()))

			// clear actions
			c.Actions = nil
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vtsingleName},
				{Verb: "Get", Kind: "Service", Resource: vtsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
			},
		})

	// no update on status change
	f(args{
		cr: &vmv1.VTSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1.VTSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(c *k8stools.ClientWithActions, cr *vmv1.VTSingle) {
			ctx := context.TODO()
			// Create objects first
			assert.NoError(t, CreateOrUpdate(ctx, c, cr.DeepCopy()))

			// clear actions
			c.Actions = nil

			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vtsingleName},
				{Verb: "Get", Kind: "Service", Resource: vtsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
			},
		})

	// no update on no change
	f(args{
		cr: &vmv1.VTSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1.VTSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(c *k8stools.ClientWithActions, cr *vmv1.VTSingle) {
			ctx := context.TODO()
			// Create objects first
			assert.NoError(t, CreateOrUpdate(ctx, c, cr.DeepCopy()))

			// clear actions
			c.Actions = nil
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vtsingleName},
				{Verb: "Get", Kind: "Service", Resource: vtsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
			},
		})
}
