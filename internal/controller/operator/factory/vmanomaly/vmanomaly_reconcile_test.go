package vmanomaly

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1.VMAnomaly
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1.VMAnomaly)
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

	name := "vmanomaly"
	namespace := "default"
	vmanomalyName := types.NamespacedName{Namespace: namespace, Name: "vmanomaly-" + name}
	tlsAssetName := types.NamespacedName{Namespace: namespace, Name: "tls-assets-vmanomaly-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	setupReadyVMAnomaly := func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1.VMAnomaly) {
		// Create objects first
		assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmanomalyName.Name + "-0",
				Namespace: vmanomalyName.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "vmanomaly",
					"app.kubernetes.io/instance":  name,
					"app.kubernetes.io/component": "monitoring",
					"managed-by":                  "vm-operator",
					"controller-revision-hash":    "v1",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "StatefulSet",
						Name:       vmanomalyName.Name,
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

		// clear actions
		c.Actions = nil
	}

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
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1.VMAnomaly) {
			setupReadyVMAnomaly(ctx, c, cr)
			// change spec
			cr.Spec.Image.Tag = "v1.1.1"
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmanomalyName},
				{Verb: "Get", Kind: "VMPodScrape", Resource: vmanomalyName},
				// Secrets
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetName},
				{Verb: "Get", Kind: "Secret", Resource: vmanomalyName},
				// StatefulSet
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Update", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
			},
		})

	// no update on status change
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
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1.VMAnomaly) {
			setupReadyVMAnomaly(ctx, c, cr)
			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmanomalyName},
				{Verb: "Get", Kind: "VMPodScrape", Resource: vmanomalyName},
				// Secrets
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetName},
				{Verb: "Get", Kind: "Secret", Resource: vmanomalyName},
				// StatefulSet
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
			},
		})
}
