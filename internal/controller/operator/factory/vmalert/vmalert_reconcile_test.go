package vmalert

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {

	type args struct {
		cr     *vmv1beta1.VMAlert
		preRun func(c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlert)
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

		err := CreateOrUpdate(ctx, args.cr, fclient, nil)
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

	vmalertName := types.NamespacedName{Namespace: "default", Name: "vmalert-vmalert"}
	tlsAssetsName := types.NamespacedName{Namespace: "default", Name: "tls-assets-vmalert-vmalert"}
	objectMeta := metav1.ObjectMeta{Name: "vmalert", Namespace: "default"}

	defaultCR := &vmv1beta1.VMAlert{
		ObjectMeta: objectMeta,
		Spec: vmv1beta1.VMAlertSpec{
			Datasource: vmv1beta1.VMAlertDatasourceSpec{URL: "http://datasource"},
			Notifier:   &vmv1beta1.VMAlertNotifierSpec{URL: "http://notifier"},
		},
	}

	// create vmalert with default config
	f(args{
		cr: defaultCR.DeepCopy(),
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmalertName},
				{Verb: "Get", Kind: "Service", Resource: vmalertName},
				{Verb: "Create", Kind: "Service", Resource: vmalertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmalertName},
				// Secrets
				{Verb: "Get", Kind: "Secret", Resource: vmalertName},
				{Verb: "Create", Kind: "Secret", Resource: vmalertName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Create", Kind: "Secret", Resource: tlsAssetsName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmalertName},
				{Verb: "Create", Kind: "Deployment", Resource: vmalertName},
				{Verb: "Get", Kind: "Deployment", Resource: vmalertName},
			},
		})

	// update vmalert with no changes
	f(args{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAlertSpec{
				Datasource: vmv1beta1.VMAlertDatasourceSpec{URL: "http://datasource"},
				Notifier:   &vmv1beta1.VMAlertNotifierSpec{URL: "http://notifier"},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlert) {
			ctx := context.TODO()
			// Create objects first
			_ = CreateOrUpdate(ctx, cr, c, nil)

			// clear actions
			c.Actions = nil
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertName},
				{Verb: "Get", Kind: "Service", Resource: vmalertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertName},
				// Secrets
				{Verb: "Get", Kind: "Secret", Resource: vmalertName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmalertName},
				{Verb: "Get", Kind: "Deployment", Resource: vmalertName},
			},
		})

	// no update on status change
	f(args{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAlertSpec{
				Datasource: vmv1beta1.VMAlertDatasourceSpec{URL: "http://datasource"},
				Notifier:   &vmv1beta1.VMAlertNotifierSpec{URL: "http://notifier"},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlert) {
			ctx := context.TODO()
			// Create objects first
			_ = CreateOrUpdate(ctx, cr, c, nil)

			// clear actions
			c.Actions = nil

			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = &cr.Spec
		},
	}, want{
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertName},
			{Verb: "Get", Kind: "Service", Resource: vmalertName},
			// TODO: bug?
			{Verb: "Update", Kind: "Service", Resource: vmalertName},
			{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertName},
			{Verb: "Get", Kind: "Secret", Resource: vmalertName},
			{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
			{Verb: "Get", Kind: "Deployment", Resource: vmalertName},
			{Verb: "Get", Kind: "Deployment", Resource: vmalertName},
			{Verb: "Get", Kind: "PodDisruptionBudget", Resource: vmalertName},
			{Verb: "Get", Kind: "Ingress", Resource: vmalertName},
			{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: vmalertName},
		},
	})
}
