package vmsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr                *vmv1beta1.VMSingle
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
		_ = vmv1beta1.AddToScheme(s)
		build.AddDefaults(s)
		s.Default(args.cr)

		var actions []k8stools.ClientAction
		objInterceptors := k8stools.GetInterceptorsWithObjects()
		actionInterceptor := k8stools.NewActionRecordingInterceptor(&actions, &objInterceptors)

		fclient := fake.NewClientBuilder().
			WithScheme(s).
			WithStatusSubresource(&vmv1beta1.VMSingle{}).
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

	name := "example-single"
	namespace := "default"
	vmsingleName := types.NamespacedName{Namespace: namespace, Name: "vmsingle-" + name}
	tlsAssetName := types.NamespacedName{Namespace: namespace, Name: "tls-assets-vmsingle-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	childObjectMeta := metav1.ObjectMeta{Name: vmsingleName.Name, Namespace: namespace}

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

	// update vmsingle
	f(args{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ServiceAccount{ObjectMeta: childObjectMeta},
			&rbacv1.Role{ObjectMeta: childObjectMeta},
			&rbacv1.RoleBinding{ObjectMeta: childObjectMeta},
			&corev1.Service{
				ObjectMeta: childObjectMeta,
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.1",
					Selector: map[string]string{
						"app.kubernetes.io/name":      "vmsingle",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   "TCP",
							Port:       8428,
							TargetPort: intstr.Parse("8428"),
						},
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: childObjectMeta},
			&corev1.Secret{ObjectMeta: childObjectMeta},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tlsAssetName.Name, Namespace: namespace}},
			&appsv1.Deployment{
				ObjectMeta: childObjectMeta,
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vmsingle",
							"app.kubernetes.io/instance":  name,
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":      "vmsingle",
								"app.kubernetes.io/instance":  name,
								"app.kubernetes.io/component": "monitoring",
								"managed-by":                  "vm-operator",
							},
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas:           1,
					ReadyReplicas:      1,
					UpdatedReplicas:    1,
					ObservedGeneration: 1,
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Update", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Get", Kind: "Service", Resource: vmsingleName},
				{Verb: "Update", Kind: "Service", Resource: vmsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmsingleName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vmsingleName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Update", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
			},
		})
}
