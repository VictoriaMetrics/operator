package vmestimator

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

// withConfig applies the given mutator to the operator config and returns a function, which restores it
func withConfig(mutator func(*config.BaseOperatorConf)) func() {
	cfg := config.MustGetBaseConfig()
	defaultCfg := *cfg
	mutator(cfg)
	return func() {
		*config.MustGetBaseConfig() = defaultCfg
	}
}

func enableVPA(c *config.BaseOperatorConf) {
	c.VPAAPIEnabled = true
}

// createReadyStsPods creates ready pods of the given statefulset,
// since the fake client doesn't create them, but they're required by rolling update of existing statefulsets
func createReadyStsPods(ctx context.Context, t *testing.T, rclient client.Client, nsn types.NamespacedName) {
	t.Helper()
	var sts appsv1.StatefulSet
	require.NoError(t, rclient.Get(ctx, nsn, &sts))
	for i := range ptr.Deref(sts.Spec.Replicas, 1) {
		podLabels := map[string]string{"controller-revision-hash": sts.Status.UpdateRevision}
		for k, v := range sts.Spec.Selector.MatchLabels {
			podLabels[k] = v
		}
		require.NoError(t, rclient.Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", sts.Name, i),
				Namespace: sts.Namespace,
				Labels:    podLabels,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       sts.Name,
					UID:        sts.UID,
					Controller: ptr.To(true),
				}},
			},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			},
		}))
	}
}

func testVPA() *vmv1beta1.EmbeddedVPA {
	return &vmv1beta1.EmbeddedVPA{
		UpdatePolicy: &vpav1.PodUpdatePolicy{UpdateMode: ptr.To(vpav1.UpdateModeInitial)},
		ResourcePolicy: &vpav1.PodResourcePolicy{
			ContainerPolicies: []vpav1.ContainerResourcePolicy{{ContainerName: "vmestimator"}},
		},
	}
}

func parseStreams(t *testing.T, data string) []vmv1.VMEstimatorStream {
	t.Helper()
	var c streamsConfig
	require.NoError(t, yaml.UnmarshalStrict([]byte(data), &c))
	return c.Streams
}

func TestBuildConfig(t *testing.T) {
	type opts struct {
		spec              vmv1.VMEstimatorSpec
		predefinedObjects []runtime.Object
		want              []vmv1.VMEstimatorStream
		wantErr           bool
	}
	f := func(o opts) {
		t.Helper()
		cr := &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       o.spec,
		}
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := build.NewAssetsCache(context.TODO(), fclient, map[build.ResourceKind]*build.ResourceCfg{})
		data, err := buildConfig(cr, ac)
		if o.wantErr {
			assert.Error(t, err)
			return
		}
		require.NoError(t, err)
		assert.Equal(t, o.want, parseStreams(t, string(data)))
	}
	streamsCM := func(data string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "streams", Namespace: "default"},
			Data:       map[string]string{"streams.yaml": data},
		}
	}
	cmRef := &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "streams"},
		Key:                  "streams.yaml",
	}

	// default streams
	f(opts{
		want: defaultStreams,
	})

	// inline streams
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			Streams: []vmv1.VMEstimatorStream{
				{
					Interval:      "15m",
					ChurnInterval: "15m",
					Filter:        `{env!="dev"}`,
					GroupBy:       []string{"job", "__label__"},
					GroupLimit:    100,
					HLLPrecision:  12,
					HLLSparse:     ptr.To(false),
					Labels:        map[string]string{"cluster": "prod"},
				},
			},
		},
		want: []vmv1.VMEstimatorStream{
			{
				Interval:      "15m",
				ChurnInterval: "15m",
				Filter:        `{env!="dev"}`,
				GroupBy:       []string{"job", "__label__"},
				GroupLimit:    100,
				HLLPrecision:  12,
				HLLSparse:     ptr.To(false),
				Labels:        map[string]string{"cluster": "prod"},
			},
		},
	})

	// streams from configmap are appended to inline streams
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			Streams:          []vmv1.VMEstimatorStream{{Interval: "1h"}},
			StreamsConfigMap: cmRef,
		},
		predefinedObjects: []runtime.Object{
			streamsCM(`
streams:
  - interval: '15m'
    churn_interval: '15m'
    group_by: ["job"]
`),
		},
		want: []vmv1.VMEstimatorStream{
			{Interval: "1h"},
			{Interval: "15m", ChurnInterval: "15m", GroupBy: []string{"job"}},
		},
	})

	// configmap with empty streams list disables default streams
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			StreamsConfigMap: cmRef,
		},
		predefinedObjects: []runtime.Object{
			streamsCM(`streams: []`),
		},
		want: []vmv1.VMEstimatorStream{},
	})

	// configmap without streams list
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			StreamsConfigMap: cmRef,
		},
		predefinedObjects: []runtime.Object{
			streamsCM(``),
		},
		wantErr: true,
	})
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			StreamsConfigMap: cmRef,
		},
		predefinedObjects: []runtime.Object{
			streamsCM(`streams:`),
		},
		wantErr: true,
	})

	// negative group limit at configmap
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			StreamsConfigMap: cmRef,
		},
		predefinedObjects: []runtime.Object{
			streamsCM(`
streams:
  - interval: 5m
    group_limit: -1
`),
		},
		wantErr: true,
	})

	// missing configmap
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			StreamsConfigMap: cmRef,
		},
		wantErr: true,
	})

	// unknown field at configmap
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			StreamsConfigMap: cmRef,
		},
		predefinedObjects: []runtime.Object{
			streamsCM(`
streams:
  - interval: 5m
    unknown_field: true
`),
		},
		wantErr: true,
	})

	// incorrect stream at configmap
	f(opts{
		spec: vmv1.VMEstimatorSpec{
			StreamsConfigMap: cmRef,
		},
		predefinedObjects: []runtime.Object{
			streamsCM(`
streams:
  - interval: 5m
    churn_interval: 10m
`),
		},
		wantErr: true,
	})
}

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1.VMEstimator
		predefinedObjects []runtime.Object
		cfgMutator        func(*config.BaseOperatorConf)
		validate          func(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator)
		wantErr           bool
	}
	f := func(o opts) {
		t.Helper()
		if o.cfgMutator != nil {
			defer withConfig(o.cfgMutator)()
		}
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(o.cr)
		ctx := context.TODO()
		synctest.Test(t, func(t *testing.T) {
			err := CreateOrUpdate(ctx, fclient, o.cr)
			if o.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if o.validate != nil {
				o.validate(ctx, fclient, o.cr)
			}
		})
	}
	nsn := func(name string) types.NamespacedName {
		return types.NamespacedName{Name: name, Namespace: "default"}
	}

	// single-node by default
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) {
			var sa corev1.ServiceAccount
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-base"), &sa))
			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vmestimator",
				"app.kubernetes.io/part-of":   "vmestimator",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, sa.Labels)

			var cm corev1.ConfigMap
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-base"), &cm))
			assert.Equal(t, defaultStreams, parseStreams(t, cm.Data[configFileName]))

			var dep appsv1.Deployment
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-single-base"), &dep))
			assert.Equal(t, map[string]string{
				"app.kubernetes.io/name":      "vmestimator-single",
				"app.kubernetes.io/part-of":   "vmestimator",
				"app.kubernetes.io/instance":  "base",
				"app.kubernetes.io/component": "monitoring",
				"managed-by":                  "vm-operator",
			}, dep.Labels)
			assert.Equal(t, cr.SelectorLabels(vmv1.VMEstimatorComponentSingle), dep.Spec.Selector.MatchLabels)
			assert.Equal(t, "vmestimator-base", dep.Spec.Template.Spec.ServiceAccountName)
			assert.Len(t, dep.Spec.Template.Annotations[configHashAnnotation], 64)
			require.Len(t, dep.Spec.Template.Spec.Containers, 1)
			cnt := dep.Spec.Template.Spec.Containers[0]
			assert.Equal(t, "vmestimator", cnt.Name)
			assert.Equal(t, "victoriametrics/vmestimator:v0.1.16", cnt.Image)
			assert.Equal(t, []string{"-config=/etc/vmestimator/config/streams.yaml", "-httpListenAddr=:8490"}, cnt.Args)
			assert.Equal(t, []corev1.VolumeMount{{Name: "config", MountPath: "/etc/vmestimator/config", ReadOnly: true}}, cnt.VolumeMounts)
			require.NotNil(t, cnt.ReadinessProbe)
			assert.Equal(t, "/health", cnt.ReadinessProbe.HTTPGet.Path)
			require.Len(t, dep.Spec.Template.Spec.Volumes, 1)
			assert.Equal(t, "vmestimator-base", dep.Spec.Template.Spec.Volumes[0].ConfigMap.Name)

			var svc corev1.Service
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-single-base"), &svc))
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
			assert.Empty(t, svc.Spec.ClusterIP)
			assert.Equal(t, int32(8490), svc.Spec.Ports[0].Port)

			var svs vmv1beta1.VMServiceScrape
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-single-base"), &svs))
			assert.Equal(t, "/metrics", svs.Spec.Endpoints[0].Path)
		},
	})

	// cluster mode
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				Streams: []vmv1.VMEstimatorStream{{Interval: "15m", GroupBy: []string{"__name__"}}},
				Storage: &vmv1.VMEstimatorStorage{
					LogFormat: "json",
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(2)),
					},
				},
				Select: &vmv1.VMEstimatorSelect{
					LogLevel: "WARN",
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To(int32(2)),
					},
				},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) {
			var cm corev1.ConfigMap
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-base"), &cm))
			assert.Equal(t, cr.Spec.Streams, parseStreams(t, cm.Data[configFileName]))

			var sts appsv1.StatefulSet
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-storage-base"), &sts))
			assert.Equal(t, ptr.To(int32(2)), sts.Spec.Replicas)
			assert.Equal(t, "vmestimator-storage-base", sts.Spec.ServiceName)
			assert.Equal(t, cr.SelectorLabels(vmv1beta1.ClusterComponentStorage), sts.Spec.Selector.MatchLabels)
			assert.Len(t, sts.Spec.Template.Annotations[configHashAnnotation], 64)
			require.Len(t, sts.Spec.Template.Spec.Containers, 1)
			assert.Equal(t, []string{
				"-cardinalityMetrics.exposeAt=/cardinality/metrics",
				"-config=/etc/vmestimator/config/streams.yaml",
				"-httpListenAddr=:8490",
				"-loggerFormat=json",
			}, sts.Spec.Template.Spec.Containers[0].Args)

			var svc corev1.Service
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-storage-base"), &svc))
			assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
			assert.True(t, svc.Spec.PublishNotReadyAddresses)

			var insertSvc corev1.Service
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-storage-base-insert"), &insertSvc))
			assert.Equal(t, corev1.ServiceTypeClusterIP, insertSvc.Spec.Type)
			assert.NotEqual(t, corev1.ClusterIPNone, insertSvc.Spec.ClusterIP)
			assert.False(t, insertSvc.Spec.PublishNotReadyAddresses)
			assert.Equal(t, cr.SelectorLabels(vmv1beta1.ClusterComponentStorage), insertSvc.Spec.Selector)
			assert.Equal(t, "managed", insertSvc.Labels[vmv1beta1.AdditionalServiceLabel])

			var dep appsv1.Deployment
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-select-base"), &dep))
			assert.Equal(t, ptr.To(int32(2)), dep.Spec.Replicas)
			assert.Empty(t, dep.Spec.Template.Annotations[configHashAnnotation])
			assert.Empty(t, dep.Spec.Template.Spec.Volumes)
			require.Len(t, dep.Spec.Template.Spec.Containers, 1)
			assert.Equal(t, []string{
				"-httpListenAddr=:8490",
				"-loggerLevel=WARN",
				"-storageNode=http://vmestimator-storage-base-0.vmestimator-storage-base.default:8490,http://vmestimator-storage-base-1.vmestimator-storage-base.default:8490",
			}, dep.Spec.Template.Spec.Containers[0].Args)

			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-select-base"), &svc))
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)

			// insert service must not be scraped
			var svsList vmv1beta1.VMServiceScrapeList
			require.NoError(t, rclient.List(ctx, &svsList, client.InNamespace("default")))
			var scrapeNames []string
			for _, svs := range svsList.Items {
				scrapeNames = append(scrapeNames, svs.Name)
			}
			assert.ElementsMatch(t, []string{"vmestimator-storage-base", "vmestimator-select-base"}, scrapeNames)

			// single-node must not be created
			err := rclient.Get(ctx, nsn("vmestimator-single-base"), &dep)
			assert.True(t, k8serrors.IsNotFound(err))
		},
	})

	// cluster mode with custom domain, port, path prefix and storage nodes override
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				ClusterDomainName: "cluster.local",
				Storage: &vmv1.VMEstimatorStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						Port:      "8491",
						ExtraArgs: map[string]string{"http.pathPrefix": "/estimator"},
					},
				},
				Select: &vmv1.VMEstimatorSelect{},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) {
			var dep appsv1.Deployment
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-select-base"), &dep))
			assert.Equal(t, []string{
				"-httpListenAddr=:8490",
				"-storageNode=http://vmestimator-storage-base-0.vmestimator-storage-base.default.svc.cluster.local:8491/estimator",
			}, dep.Spec.Template.Spec.Containers[0].Args)
		},
	})

	// single-node with optional objects
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{
					Labels:      map[string]string{"team": "observability"},
					Annotations: map[string]string{"owner": "sre"},
				},
				Single: &vmv1.VMEstimatorSingle{
					PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: ptr.To(intstr.FromInt32(1))},
					NetworkPolicy: &vmv1beta1.EmbeddedNetworkPolicy{
						Ingress: []networkingv1.NetworkPolicyIngressRule{{}},
					},
					VPA: testVPA(),
					ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
						EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "vmestimator-external"},
						Spec:                   corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
					},
					UpdateStrategy: ptr.To(appsv1.RecreateDeploymentStrategyType),
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						UseStrictSecurity: ptr.To(true),
					},
				},
			},
		},
		cfgMutator: enableVPA,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) {
			name := "vmestimator-single-base"
			selector := cr.SelectorLabels(vmv1.VMEstimatorComponentSingle)

			var pdb policyv1.PodDisruptionBudget
			require.NoError(t, rclient.Get(ctx, nsn(name), &pdb))
			assert.Equal(t, selector, pdb.Spec.Selector.MatchLabels)
			assert.Equal(t, ptr.To(intstr.FromInt32(1)), pdb.Spec.MaxUnavailable)

			var np networkingv1.NetworkPolicy
			require.NoError(t, rclient.Get(ctx, nsn(name), &np))
			assert.Equal(t, selector, np.Spec.PodSelector.MatchLabels)

			var vpa vpav1.VerticalPodAutoscaler
			require.NoError(t, rclient.Get(ctx, nsn(name), &vpa))
			assert.Equal(t, "Deployment", vpa.Spec.TargetRef.Kind)
			assert.Equal(t, name, vpa.Spec.TargetRef.Name)

			var svc corev1.Service
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-external"), &svc))
			assert.Equal(t, corev1.ServiceTypeNodePort, svc.Spec.Type)
			assert.Equal(t, selector, svc.Spec.Selector)
			assert.Equal(t, "managed", svc.Labels[vmv1beta1.AdditionalServiceLabel])

			var dep appsv1.Deployment
			require.NoError(t, rclient.Get(ctx, nsn(name), &dep))
			assert.Equal(t, appsv1.RecreateDeploymentStrategyType, dep.Spec.Strategy.Type)
			assert.Equal(t, "observability", dep.Labels["team"])
			assert.Equal(t, "sre", dep.Annotations["owner"])
			sc := dep.Spec.Template.Spec.Containers[0].SecurityContext
			require.NotNil(t, sc)
			assert.True(t, ptr.Deref(sc.RunAsNonRoot, false))

			var cm corev1.ConfigMap
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-base"), &cm))
			assert.Equal(t, "observability", cm.Labels["team"])
			assert.Equal(t, "sre", cm.Annotations["owner"])

			// additional service must not be scraped
			var svs vmv1beta1.VMServiceScrape
			require.NoError(t, rclient.Get(ctx, nsn(name), &svs))
			assert.Contains(t, svs.Spec.Selector.MatchExpressions, metav1.LabelSelectorRequirement{
				Key:      vmv1beta1.AdditionalServiceLabel,
				Operator: metav1.LabelSelectorOpDoesNotExist,
			})
		},
	})

	// cluster mode with optional objects and overridden flags
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				Storage: &vmv1.VMEstimatorStorage{
					PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: ptr.To(intstr.FromInt32(1))},
					NetworkPolicy: &vmv1beta1.EmbeddedNetworkPolicy{
						Ingress: []networkingv1.NetworkPolicyIngressRule{{}},
					},
					VPA: testVPA(),
					ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
						EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "storage-external"},
					},
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ExtraArgs: map[string]string{"cardinalityMetrics.exposeAt": "/metrics"},
					},
				},
				Select: &vmv1.VMEstimatorSelect{
					PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MinAvailable: ptr.To(intstr.FromInt32(1))},
					NetworkPolicy: &vmv1beta1.EmbeddedNetworkPolicy{
						Egress: []networkingv1.NetworkPolicyEgressRule{{}},
					},
					HPA: &vmv1beta1.EmbeddedHPA{
						MinReplicas: ptr.To(int32(1)),
						MaxReplicas: 3,
					},
					VPA: testVPA(),
					ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
						EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "select-external"},
					},
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ExtraArgs: map[string]string{"storageNode": "http://external-storage:8490"},
					},
				},
			},
		},
		cfgMutator: enableVPA,
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) {
			for _, kind := range []vmv1beta1.ClusterComponent{vmv1beta1.ClusterComponentStorage, vmv1beta1.ClusterComponentSelect} {
				name := cr.PrefixedName(kind)
				selector := cr.SelectorLabels(kind)

				var pdb policyv1.PodDisruptionBudget
				require.NoError(t, rclient.Get(ctx, nsn(name), &pdb), "kind=%s", kind)
				assert.Equal(t, selector, pdb.Spec.Selector.MatchLabels, "kind=%s", kind)

				var np networkingv1.NetworkPolicy
				require.NoError(t, rclient.Get(ctx, nsn(name), &np), "kind=%s", kind)
				assert.Equal(t, selector, np.Spec.PodSelector.MatchLabels, "kind=%s", kind)

				var vpa vpav1.VerticalPodAutoscaler
				require.NoError(t, rclient.Get(ctx, nsn(name), &vpa), "kind=%s", kind)
				assert.Equal(t, name, vpa.Spec.TargetRef.Name, "kind=%s", kind)
			}
			var vpa vpav1.VerticalPodAutoscaler
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-storage-base"), &vpa))
			assert.Equal(t, "StatefulSet", vpa.Spec.TargetRef.Kind)

			var hpa autoscalingv2.HorizontalPodAutoscaler
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-select-base"), &hpa))
			assert.Equal(t, "Deployment", hpa.Spec.ScaleTargetRef.Kind)
			assert.Equal(t, "vmestimator-select-base", hpa.Spec.ScaleTargetRef.Name)
			assert.Equal(t, int32(3), hpa.Spec.MaxReplicas)

			var svc corev1.Service
			require.NoError(t, rclient.Get(ctx, nsn("storage-external"), &svc))
			assert.Equal(t, cr.SelectorLabels(vmv1beta1.ClusterComponentStorage), svc.Spec.Selector)
			require.NoError(t, rclient.Get(ctx, nsn("select-external"), &svc))
			assert.Equal(t, cr.SelectorLabels(vmv1beta1.ClusterComponentSelect), svc.Spec.Selector)

			// extraArgs override default flags
			var sts appsv1.StatefulSet
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-storage-base"), &sts))
			assert.Contains(t, sts.Spec.Template.Spec.Containers[0].Args, "-cardinalityMetrics.exposeAt=/metrics")
			assert.NotContains(t, sts.Spec.Template.Spec.Containers[0].Args, "-cardinalityMetrics.exposeAt=/cardinality/metrics")
			var dep appsv1.Deployment
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-select-base"), &dep))
			assert.Equal(t, []string{"-httpListenAddr=:8490", "-storageNode=http://external-storage:8490"}, dep.Spec.Template.Spec.Containers[0].Args)
		},
	})

	// vpa requires VPA API support
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				Single: &vmv1.VMEstimatorSingle{
					VPA: testVPA(),
				},
			},
		},
		wantErr: true,
	})

	// paused object isn't reconciled
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				Paused: true,
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) {
			assert.True(t, k8serrors.IsNotFound(rclient.Get(ctx, nsn("vmestimator-single-base"), &appsv1.Deployment{})))
			assert.True(t, k8serrors.IsNotFound(rclient.Get(ctx, nsn("vmestimator-base"), &corev1.ConfigMap{})))
		},
	})

	// custom service account isn't managed by operator
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				ServiceAccountName: "custom",
				Storage:            &vmv1.VMEstimatorStorage{},
				Select:             &vmv1.VMEstimatorSelect{},
			},
		},
		validate: func(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) {
			assert.True(t, k8serrors.IsNotFound(rclient.Get(ctx, nsn("vmestimator-base"), &corev1.ServiceAccount{})))
			var sts appsv1.StatefulSet
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-storage-base"), &sts))
			assert.Equal(t, "custom", sts.Spec.Template.Spec.ServiceAccountName)
			var dep appsv1.Deployment
			require.NoError(t, rclient.Get(ctx, nsn("vmestimator-select-base"), &dep))
			assert.Equal(t, "custom", dep.Spec.Template.Spec.ServiceAccountName)
		},
	})

	// missing streams configmap
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				StreamsConfigMap: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "missing"},
					Key:                  "streams.yaml",
				},
			},
		},
		wantErr: true,
	})

	// incorrect spec
	f(opts{
		cr: &vmv1.VMEstimator{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
			Spec: vmv1.VMEstimatorSpec{
				Single:  &vmv1.VMEstimatorSingle{},
				Storage: &vmv1.VMEstimatorStorage{},
			},
		},
		wantErr: true,
	})
}

func TestCreateOrUpdate_ConfigChange(t *testing.T) {
	ctx := context.TODO()
	fclient := k8stools.GetTestClientWithObjects(nil)
	build.AddDefaults(fclient.Scheme())
	cr := &vmv1.VMEstimator{
		ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
		Spec: vmv1.VMEstimatorSpec{
			Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}},
		},
	}
	getConfigHash := func() string {
		t.Helper()
		var dep appsv1.Deployment
		require.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: "vmestimator-single-base", Namespace: "default"}, &dep))
		return dep.Spec.Template.Annotations[configHashAnnotation]
	}
	synctest.Test(t, func(t *testing.T) {
		fclient.Scheme().Default(cr)
		require.NoError(t, CreateOrUpdate(ctx, fclient, cr))
		hash := getConfigHash()
		assert.NotEmpty(t, hash)

		// reconcile without changes keeps hash
		cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		require.NoError(t, CreateOrUpdate(ctx, fclient, cr))
		assert.Equal(t, hash, getConfigHash())

		// streams change must trigger pods rollout
		cr.Spec.Streams = append(cr.Spec.Streams, vmv1.VMEstimatorStream{Interval: "5m", GroupBy: []string{"job"}})
		require.NoError(t, CreateOrUpdate(ctx, fclient, cr))
		assert.NotEqual(t, hash, getConfigHash())
	})
}

func TestCreateOrUpdate_ModeSwitch(t *testing.T) {
	ctx := context.TODO()
	fclient := k8stools.GetTestClientWithObjects(nil)
	build.AddDefaults(fclient.Scheme())
	nsn := func(name string) types.NamespacedName {
		return types.NamespacedName{Name: name, Namespace: "default"}
	}
	cr := &vmv1.VMEstimator{
		ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
		Spec: vmv1.VMEstimatorSpec{
			Single: &vmv1.VMEstimatorSingle{
				PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: ptr.To(intstr.FromInt32(1))},
			},
		},
	}
	synctest.Test(t, func(t *testing.T) {
		fclient.Scheme().Default(cr)
		require.NoError(t, CreateOrUpdate(ctx, fclient, cr))
		var dep appsv1.Deployment
		require.NoError(t, fclient.Get(ctx, nsn("vmestimator-single-base"), &dep))

		// switch to the cluster mode
		cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		cr.Spec.Single = nil
		cr.Spec.Storage = &vmv1.VMEstimatorStorage{}
		cr.Spec.Select = &vmv1.VMEstimatorSelect{}
		fclient.Scheme().Default(cr)
		require.NoError(t, CreateOrUpdate(ctx, fclient, cr))

		var sts appsv1.StatefulSet
		require.NoError(t, fclient.Get(ctx, nsn("vmestimator-storage-base"), &sts))
		require.NoError(t, fclient.Get(ctx, nsn("vmestimator-select-base"), &dep))

		// single-node objects must be removed
		assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn("vmestimator-single-base"), &appsv1.Deployment{})))
		assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn("vmestimator-single-base"), &corev1.Service{})))
		assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn("vmestimator-single-base"), &vmv1beta1.VMServiceScrape{})))
		var pdbs policyv1.PodDisruptionBudgetList
		require.NoError(t, fclient.List(ctx, &pdbs, client.InNamespace("default")))
		assert.Empty(t, pdbs.Items)

		// switch back to the single-node mode
		cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		cr.Spec.Storage = nil
		cr.Spec.Select = nil
		fclient.Scheme().Default(cr)
		require.NoError(t, CreateOrUpdate(ctx, fclient, cr))
		require.NoError(t, fclient.Get(ctx, nsn("vmestimator-single-base"), &dep))
		assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn("vmestimator-storage-base"), &appsv1.StatefulSet{})))
		assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn("vmestimator-select-base"), &appsv1.Deployment{})))
		for _, name := range []string{"vmestimator-storage-base", "vmestimator-storage-base-insert", "vmestimator-select-base"} {
			assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn(name), &corev1.Service{})), "service=%s", name)
		}
	})
}

func TestCreateOrUpdate_RemoveOptionalObjects(t *testing.T) {
	defer withConfig(enableVPA)()
	ctx := context.TODO()
	fclient := k8stools.GetTestClientWithObjects(nil)
	build.AddDefaults(fclient.Scheme())
	nsn := func(name string) types.NamespacedName {
		return types.NamespacedName{Name: name, Namespace: "default"}
	}
	pdb := func() *vmv1beta1.EmbeddedPodDisruptionBudgetSpec {
		return &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: ptr.To(intstr.FromInt32(1))}
	}
	np := func() *vmv1beta1.EmbeddedNetworkPolicy {
		return &vmv1beta1.EmbeddedNetworkPolicy{Ingress: []networkingv1.NetworkPolicyIngressRule{{}}}
	}
	svcSpec := func(name string) *vmv1beta1.AdditionalServiceSpec {
		return &vmv1beta1.AdditionalServiceSpec{EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: name}}
	}
	cr := &vmv1.VMEstimator{
		ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
		Spec: vmv1.VMEstimatorSpec{
			Storage: &vmv1.VMEstimatorStorage{
				PodDisruptionBudget: pdb(),
				NetworkPolicy:       np(),
				VPA:                 testVPA(),
				ServiceSpec:         svcSpec("storage-external"),
			},
			Select: &vmv1.VMEstimatorSelect{
				PodDisruptionBudget: pdb(),
				NetworkPolicy:       np(),
				HPA:                 &vmv1beta1.EmbeddedHPA{MinReplicas: ptr.To(int32(1)), MaxReplicas: 3},
				VPA:                 testVPA(),
				ServiceSpec:         svcSpec("select-external"),
			},
		},
	}
	synctest.Test(t, func(t *testing.T) {
		fclient.Scheme().Default(cr)
		require.NoError(t, CreateOrUpdate(ctx, fclient, cr))
		for _, name := range []string{"vmestimator-storage-base", "vmestimator-select-base"} {
			require.NoError(t, fclient.Get(ctx, nsn(name), &policyv1.PodDisruptionBudget{}), name)
			require.NoError(t, fclient.Get(ctx, nsn(name), &networkingv1.NetworkPolicy{}), name)
			require.NoError(t, fclient.Get(ctx, nsn(name), &vpav1.VerticalPodAutoscaler{}), name)
		}
		require.NoError(t, fclient.Get(ctx, nsn("vmestimator-select-base"), &autoscalingv2.HorizontalPodAutoscaler{}))
		require.NoError(t, fclient.Get(ctx, nsn("storage-external"), &corev1.Service{}))
		require.NoError(t, fclient.Get(ctx, nsn("select-external"), &corev1.Service{}))
		createReadyStsPods(ctx, t, fclient, nsn("vmestimator-storage-base"))

		// remove all optional objects
		cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		cr.Spec.Storage = &vmv1.VMEstimatorStorage{}
		cr.Spec.Select = &vmv1.VMEstimatorSelect{}
		fclient.Scheme().Default(cr)
		require.NoError(t, CreateOrUpdate(ctx, fclient, cr))
		for _, name := range []string{"vmestimator-storage-base", "vmestimator-select-base"} {
			assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn(name), &policyv1.PodDisruptionBudget{})), "pdb=%s", name)
			assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn(name), &networkingv1.NetworkPolicy{})), "networkpolicy=%s", name)
			assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn(name), &vpav1.VerticalPodAutoscaler{})), "vpa=%s", name)
		}
		assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn("vmestimator-select-base"), &autoscalingv2.HorizontalPodAutoscaler{})))
		assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn("storage-external"), &corev1.Service{})))
		assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, nsn("select-external"), &corev1.Service{})))

		// default services must be kept
		for _, name := range []string{"vmestimator-storage-base", "vmestimator-storage-base-insert", "vmestimator-select-base"} {
			assert.NoError(t, fclient.Get(ctx, nsn(name), &corev1.Service{}), "service=%s", name)
		}
	})
}
