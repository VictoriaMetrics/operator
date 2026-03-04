package vmanomaly

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
		cr                *vmv1.VMAnomaly
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
			WithStatusSubresource(&vmv1.VMAnomaly{}).
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

	name := "vmanomaly"
	namespace := "default"
	vmanomalyName := types.NamespacedName{Namespace: namespace, Name: "vmanomaly-" + name}
	tlsAssetName := types.NamespacedName{Namespace: namespace, Name: "tls-assets-vmanomaly-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	childObjectMeta := metav1.ObjectMeta{Name: vmanomalyName.Name, Namespace: namespace}

	// create vmanomaly with default config
	f(args{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: objectMeta,
			Spec: vmv1.VMAnomalySpec{
				ConfigRawYaml: `
models:
  M1:
    class: "zscore"
    z_threshold: 2.5
    queries: ["q1"]
    schedulers: ["S1"]
reader:
  queries:
    q1:
      expr: "sum(up)"
schedulers:
  S1:
    class: "periodic"
    infer_every: "1m"
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader-url",
					SamplingPeriod: "1m",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer-url",
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmanomalyName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmanomalyName},
				{Verb: "Get", Kind: "VMPodScrape", Resource: vmanomalyName},
				{Verb: "Create", Kind: "VMPodScrape", Resource: vmanomalyName},
				// Secrets
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetName},
				{Verb: "Create", Kind: "Secret", Resource: tlsAssetName},
				{Verb: "Get", Kind: "Secret", Resource: vmanomalyName},
				{Verb: "Create", Kind: "Secret", Resource: vmanomalyName},
				// StatefulSet
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
			},
		})

	// update vmanomaly
	f(args{
		cr: &vmv1.VMAnomaly{
			ObjectMeta: objectMeta,
			Spec: vmv1.VMAnomalySpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				RollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType,
				ConfigRawYaml: `
models:
  M1:
    class: "zscore"
    z_threshold: 2.5
    queries: ["q1"]
    schedulers: ["S1"]
reader:
  queries:
    q1:
      expr: "sum(up)"
schedulers:
  S1:
    class: "periodic"
    infer_every: "1m"
`,
				Reader: &vmv1.VMAnomalyReadersSpec{
					DatasourceURL:  "http://reader-url",
					SamplingPeriod: "1m",
				},
				Writer: &vmv1.VMAnomalyWritersSpec{
					DatasourceURL: "http://writer-url",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ServiceAccount{ObjectMeta: childObjectMeta},
			&vmv1beta1.VMPodScrape{ObjectMeta: childObjectMeta},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: tlsAssetName.Name}},
			&corev1.Secret{ObjectMeta: childObjectMeta},
			&appsv1.StatefulSet{
				ObjectMeta: childObjectMeta,
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vmanomaly",
							"app.kubernetes.io/instance":  name,
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":      "vmanomaly",
								"app.kubernetes.io/instance":  name,
								"app.kubernetes.io/component": "monitoring",
								"managed-by":                  "vm-operator",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "vmanomaly"},
							},
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
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
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmanomalyName},
				{Verb: "Update", Kind: "ServiceAccount", Resource: vmanomalyName},
				{Verb: "Get", Kind: "VMPodScrape", Resource: vmanomalyName},
				{Verb: "Update", Kind: "VMPodScrape", Resource: vmanomalyName},
				// Secrets
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetName},
				{Verb: "Update", Kind: "Secret", Resource: tlsAssetName},
				{Verb: "Get", Kind: "Secret", Resource: vmanomalyName},
				{Verb: "Update", Kind: "Secret", Resource: vmanomalyName},
				// StatefulSet
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Delete", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
			},
		})
}
