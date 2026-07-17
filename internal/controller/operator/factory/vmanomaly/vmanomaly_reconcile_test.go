package vmanomaly

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
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
				{Verb: "Get", Kind: "Service", Resource: vmanomalyName},
				{Verb: "Create", Kind: "Service", Resource: vmanomalyName},
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
				{Verb: "Get", Kind: "Service", Resource: vmanomalyName},
				{Verb: "Get", Kind: "VMPodScrape", Resource: vmanomalyName},
				// Secrets
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetName},
				{Verb: "Get", Kind: "Secret", Resource: vmanomalyName},
				// StatefulSet
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Update", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName}, // getLatestStsState
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName}, // patchSTSCurrentRevision
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
				{Verb: "Get", Kind: "Service", Resource: vmanomalyName},
				{Verb: "Get", Kind: "VMPodScrape", Resource: vmanomalyName},
				// Secrets
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetName},
				{Verb: "Get", Kind: "Secret", Resource: vmanomalyName},
				// StatefulSet
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName}, // getLatestStsState
				{Verb: "Get", Kind: "StatefulSet", Resource: vmanomalyName}, // patchSTSCurrentRevision
			},
		})
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	cr := &vmv1.VMAnomaly{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-anomaly",
			Namespace: "default",
		},
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
			CommonAppsParams: vmv1beta1.CommonAppsParams{
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

	var sts appsv1.StatefulSet
	err := fclient.Get(ctx, nsn, &sts)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))

	// unpause and verify reconciliation
	cr.Spec.Paused = false
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
	err = fclient.Get(ctx, nsn, &sts)
	assert.NoError(t, err)

	// pause and update replica count
	cr.Spec.Paused = true
	cr.Spec.ReplicaCount = ptr.To(int32(2))
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

	// check that replicas count is not updated
	err = fclient.Get(ctx, nsn, &sts)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), *sts.Spec.Replicas)
}

func TestCreateOrUpdateVPA_TargetsCR(t *testing.T) {
	cfg := config.MustGetBaseConfig()
	defaultCfg := *cfg
	cfg.VPAAPIEnabled = true
	defer func() { *cfg = defaultCfg }()

	f := func(cr *vmv1.VMAnomaly) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)

		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		var vpa vpav1.VerticalPodAutoscaler
		assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, &vpa))
		assert.Equal(t, &autoscalingv1.CrossVersionObjectReference{
			Name:       cr.Name,
			Kind:       "VMAnomaly",
			APIVersion: "operator.victoriametrics.com/v1",
		}, vpa.Spec.TargetRef)
	}

	baseSpec := func() vmv1.VMAnomalySpec {
		return vmv1.VMAnomalySpec{
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
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
			},
			VPA: &vmv1beta1.EmbeddedVPA{},
		}
	}

	// non-sharded
	nonSharded := baseSpec()
	f(&vmv1.VMAnomaly{
		ObjectMeta: metav1.ObjectMeta{Name: "vpa-vmanomaly", Namespace: "default"},
		Spec:       nonSharded,
	})

	// sharded
	sharded := baseSpec()
	sharded.ShardCount = ptr.To(int32(3))
	f(&vmv1.VMAnomaly{
		ObjectMeta: metav1.ObjectMeta{Name: "vpa-vmanomaly-sharded", Namespace: "default"},
		Spec:       sharded,
	})
}

func TestCreateOrUpdateVPA_RemovedOnDisable(t *testing.T) {
	cfg := config.MustGetBaseConfig()
	defaultCfg := *cfg
	cfg.VPAAPIEnabled = true
	defer func() { *cfg = defaultCfg }()

	cr := &vmv1.VMAnomaly{
		ObjectMeta: metav1.ObjectMeta{Name: "vpa-disable-vmanomaly", Namespace: "default"},
		Spec: vmv1.VMAnomalySpec{
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
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
			},
			VPA: &vmv1beta1.EmbeddedVPA{},
		},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
	vpaNSN := types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}
	var vpa vpav1.VerticalPodAutoscaler
	assert.NoError(t, fclient.Get(ctx, vpaNSN, &vpa))

	// disabling VPA must delete the VPA created under cr.Name, not one under cr.PrefixedName()
	cr.Spec.VPA = nil
	assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
	err := fclient.Get(ctx, vpaNSN, &vpa)
	assert.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err))
}
