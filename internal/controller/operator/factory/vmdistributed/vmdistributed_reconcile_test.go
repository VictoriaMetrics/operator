package vmdistributed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr                *vmv1alpha1.VMDistributed
		predefinedObjects []runtime.Object
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	f := func(args args, want want) {
		t.Helper()

		// Use local scheme to avoid global scheme pollution
		s := runtime.NewScheme()
		_ = scheme.AddToScheme(s)
		_ = vmv1alpha1.AddToScheme(s)
		_ = vmv1beta1.AddToScheme(s)
		build.AddDefaults(s)
		s.Default(args.cr)

		var actions []k8stools.ClientAction
		objInterceptors := k8stools.GetInterceptorsWithObjects()
		actionInterceptor := k8stools.NewActionRecordingInterceptor(&actions, &objInterceptors)

		fclient := fake.NewClientBuilder().
			WithScheme(s).
			WithStatusSubresource(
				&vmv1alpha1.VMDistributed{},
				&vmv1beta1.VMCluster{},
				&vmv1beta1.VMAgent{},
				&vmv1beta1.VMAuth{},
			).
			WithRuntimeObjects(args.predefinedObjects...).
			WithInterceptorFuncs(actionInterceptor).
			Build()

		ctx := context.TODO()
		err := CreateOrUpdate(ctx, args.cr, fclient)
		if want.err != nil {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}

		if !assert.Equal(t, len(want.actions), len(actions)) {
			for i, action := range actions {
				t.Logf("Action %d: %s %s %s", i, action.Verb, action.Kind, action.Resource)
			}
		}

		for i, action := range want.actions {
			if i >= len(actions) {
				break
			}
			assert.Equal(t, action.Verb, actions[i].Verb, "idx %d verb", i)
			assert.Equal(t, action.Kind, actions[i].Kind, "idx %d kind", i)
			assert.Equal(t, action.Resource, actions[i].Resource, "idx %d resource", i)
		}
	}

	name := "test-dist"
	namespace := "default"
	zoneName := "zone-1"
	vmClusterName := types.NamespacedName{Namespace: namespace, Name: "test-dist-zone-1"}
	vmAgentName := types.NamespacedName{Namespace: namespace, Name: "test-dist-zone-1"}
	vmAuthLBName := types.NamespacedName{Namespace: namespace, Name: name}

	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	f(args{
		cr: &vmv1alpha1.VMDistributed{
			ObjectMeta: objectMeta,
			Spec: vmv1alpha1.VMDistributedSpec{
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						Name: zoneName,
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Spec: vmv1beta1.VMClusterSpec{
								RetentionPeriod: "1",
								VMStorage: &vmv1beta1.VMStorage{
									CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To(int32(1)),
									},
								},
								VMSelect: &vmv1beta1.VMSelect{
									CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To(int32(1)),
									},
								},
								VMInsert: &vmv1beta1.VMInsert{
									CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To(int32(1)),
									},
								},
							},
						},
						VMAgent: vmv1alpha1.VMDistributedZoneAgent{
							Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
								PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{},
							},
						},
					},
				},
				VMAuth: vmv1alpha1.VMDistributedAuth{
					Spec: vmv1beta1.VMAuthSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To(int32(1)),
						},
					},
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// getZones
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

				// reconcile VMCluster
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Create", Kind: "VMCluster", Resource: vmClusterName},
				{Verb: "Get", Kind: "VMCluster", Resource: vmClusterName},

				// reconcile VMAgent
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},
				{Verb: "Create", Kind: "VMAgent", Resource: vmAgentName},
				{Verb: "Get", Kind: "VMAgent", Resource: vmAgentName},

				// reconcile VMAuth
				{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
				{Verb: "Create", Kind: "VMAuth", Resource: vmAuthLBName},
				{Verb: "Get", Kind: "VMAuth", Resource: vmAuthLBName},
			},
		})
}
