package vmalert

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
		cr     *vmv1beta1.VMAlert
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlert)
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

	setupReadyVMAlert := func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlert) {
		// Create the object first
		assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c, nil))

		// clear actions
		c.Actions = nil
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
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: setupReadyVMAlert,
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
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAlert) {
			setupReadyVMAlert(ctx, c, cr)

			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		},
	}, want{
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PodDisruptionBudget", Resource: vmalertName},
			{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertName},
			{Verb: "Get", Kind: "Service", Resource: vmalertName},
			{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertName},
			{Verb: "Get", Kind: "Secret", Resource: vmalertName},
			{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
			{Verb: "Get", Kind: "Deployment", Resource: vmalertName},
			{Verb: "Get", Kind: "Deployment", Resource: vmalertName},
		},
	})
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	cr := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmalert",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAlertSpec{
			Datasource: vmv1beta1.VMAlertDatasourceSpec{URL: "http://datasource"},
			Notifier:   &vmv1beta1.VMAlertNotifierSpec{URL: "http://notifier"},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
				Paused:       true,
			},
		},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient, nil))

	var dep appsv1.Deployment
	nsn := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
	err := fclient.Get(ctx, nsn, &dep)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))

	// unpause and verify reconciliation
	cr.Spec.Paused = false
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient, nil))
	err = fclient.Get(ctx, nsn, &dep)
	assert.NoError(t, err)

	// pause and update replica count
	cr.Spec.Paused = true
	cr.Spec.ReplicaCount = ptr.To(int32(2))
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient, nil))

	// check that replicas count is not updated
	err = fclient.Get(ctx, nsn, &dep)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), *dep.Spec.Replicas)
}
