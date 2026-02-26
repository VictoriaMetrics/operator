package vtsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr                *vmv1.VTSingle
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
		_ = vmv1.AddToScheme(s)
		_ = vmv1beta1.AddToScheme(s)
		build.AddDefaults(s)
		s.Default(args.cr)

		var actions []k8stools.ClientAction
		objInterceptors := k8stools.GetInterceptorsWithObjects()
		actionInterceptor := k8stools.NewActionRecordingInterceptor(&actions, &objInterceptors)

		fclient := fake.NewClientBuilder().
			WithScheme(s).
			WithStatusSubresource(&vmv1.VTSingle{}).
			WithRuntimeObjects(args.predefinedObjects...).
			WithInterceptorFuncs(actionInterceptor).
			Build()

		ctx := context.TODO()
		err := CreateOrUpdate(ctx, fclient, args.cr)
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

	name := "example-vtsingle"
	namespace := "default"
	vtsingleName := types.NamespacedName{Namespace: namespace, Name: "vtsingle-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	childObjectMeta := metav1.ObjectMeta{Name: vtsingleName.Name, Namespace: namespace}

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

	// update vtsingle
	f(args{
		cr: &vmv1.VTSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1.VTSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ServiceAccount{ObjectMeta: childObjectMeta},
			&corev1.Service{
				ObjectMeta: childObjectMeta,
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.1",
					Selector: map[string]string{
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   "TCP",
							Port:       10428,
							TargetPort: intstr.Parse("10428"),
						},
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: childObjectMeta},
			&appsv1.Deployment{
				ObjectMeta: childObjectMeta,
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vtsingle",
							"app.kubernetes.io/instance":  name,
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":      "vtsingle",
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
				{Verb: "Get", Kind: "ServiceAccount", Resource: vtsingleName},
				{Verb: "Update", Kind: "ServiceAccount", Resource: vtsingleName},
				{Verb: "Get", Kind: "Service", Resource: vtsingleName},
				{Verb: "Update", Kind: "Service", Resource: vtsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtsingleName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
				{Verb: "Update", Kind: "Deployment", Resource: vtsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vtsingleName},
			},
		})
}
