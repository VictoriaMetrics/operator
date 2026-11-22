package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/encoding"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/prompb"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// vmEstimatorTestArgs disables cardinality metrics caching in order to observe estimations right after ingestion
var vmEstimatorTestArgs = map[string]string{
	"cardinalityMetrics.cacheTTL": "0s",
}

// vmEstimatorServiceURL returns base URL of the given vmestimator component service.
// Defaults aren't persisted at the object, so the default port is used if it isn't set.
func vmEstimatorServiceURL(cr *vmv1.VMEstimator, kind vmv1beta1.ClusterComponent, port string) string {
	if port == "" {
		port = "8490"
	}
	return fmt.Sprintf("http://%s.%s.svc:%s", cr.PrefixedName(kind), cr.Namespace, port)
}

// vmEstimatorWriteURL returns remote write URL, which is served by storage nodes
func vmEstimatorWriteURL(cr *vmv1.VMEstimator) string {
	var port string
	if cr.Spec.Storage != nil {
		port = cr.Spec.Storage.Port
	}
	return vmEstimatorServiceURL(cr, vmv1beta1.ClusterComponentStorage, port) + "/cardinality/api/v1/write"
}

// vmEstimatorQueryURL returns URL of cardinality estimations.
// They're merged and exposed by select nodes if select is enabled, otherwise by storage node.
func vmEstimatorQueryURL(cr *vmv1.VMEstimator) string {
	if sel := cr.Spec.Select; sel != nil && ptr.Deref(sel.ReplicaCount, 1) > 0 {
		return vmEstimatorServiceURL(cr, vmv1beta1.ClusterComponentSelect, sel.Port) + "/metrics"
	}
	var port string
	if cr.Spec.Storage != nil {
		port = cr.Spec.Storage.Port
	}
	return vmEstimatorServiceURL(cr, vmv1beta1.ClusterComponentStorage, port) + "/metrics"
}

// sendVMEstimatorSeries sends the given number of unique series to vmestimator via Prometheus remote write API
func sendVMEstimatorSeries(ctx context.Context, writeURL string, seriesCount int) {
	GinkgoHelper()
	var wr prompb.WriteRequest
	now := time.Now().UnixMilli()
	for i := range seriesCount {
		wr.Timeseries = append(wr.Timeseries, prompb.TimeSeries{
			Labels: []prompb.Label{
				{Name: "__name__", Value: "vmestimator_e2e_metric"},
				{Name: "idx", Value: strconv.Itoa(i)},
				{Name: "job", Value: "vmestimator-e2e"},
			},
			Samples: []prompb.Sample{{Value: 1, Timestamp: now}},
		})
	}
	payload := encoding.CompressZSTDLevel(nil, wr.MarshalProtobuf(nil), 1)
	expectHTTPRequestToSucceed(ctx, httpRequestOpts{
		dstURL:       writeURL,
		method:       http.MethodPost,
		payload:      string(payload),
		expectedCode: http.StatusNoContent,
	})
}

// expectGlobalCardinality waits until the global cardinality estimation exposed at queryURL becomes close to want
func expectGlobalCardinality(ctx context.Context, queryURL string, want float64) {
	GinkgoHelper()
	proxyURL, err := buildServiceProxyURL(&k8sCfg, queryURL)
	Expect(err).ToNot(HaveOccurred())
	hc, err := rest.HTTPClientFor(&k8sCfg)
	Expect(err).ToNot(HaveOccurred())
	hc.Timeout = 10 * time.Second
	Eventually(func() (float64, error) {
		values, err := podutil.FetchMetricsValues(ctx, hc, proxyURL, []podutil.MetricQuery{
			{Name: "cardinality_estimate", Dimension: "group_by_keys"},
		})
		if err != nil {
			return 0, err
		}
		v, ok := values["cardinality_estimate"]["__global__"]
		if !ok {
			return 0, fmt.Errorf("cannot find global cardinality estimation at %s", queryURL)
		}
		return v, nil
	}, eventualDeploymentAppReadyTimeout).Should(BeNumerically("~", want, want*0.1))
}

//nolint:dupl,lll
var _ = Describe("test vmestimator Controller", Label("vm", "vmestimator"), func() {
	Context("e2e vmestimator", func() {
		var ctx context.Context
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		nsn := types.NamespacedName{
			Namespace: namespace,
		}
		componentNsn := func(cr *vmv1.VMEstimator, kind vmv1beta1.ClusterComponent) types.NamespacedName {
			return types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(kind)}
		}
		BeforeEach(func() {
			ctx = context.Background()
		})
		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1.VMEstimator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: nsn.Namespace,
				},
			})).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, nsn, &vmv1.VMEstimatorList{})
		})

		DescribeTable("should create",
			func(name string, cr *vmv1.VMEstimator, verify func(*vmv1.VMEstimator)) {
				cr.Name = name
				cr.Namespace = namespace
				nsn.Name = name
				expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualStatefulsetAppReadyTimeout, func() {
					Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
				}, vmv1beta1.UpdateStatusOperational)

				var created vmv1.VMEstimator
				Expect(k8sClient.Get(ctx, nsn, &created)).ToNot(HaveOccurred())
				verify(&created)
			},
			Entry("with single storage node by default", "storage-default",
				&vmv1.VMEstimator{
					Spec: vmv1.VMEstimatorSpec{
						Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}},
						Storage: &vmv1.VMEstimatorStorage{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ExtraArgs: vmEstimatorTestArgs,
							},
						},
					},
				},
				func(cr *vmv1.VMEstimator) {
					var sts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, componentNsn(cr, vmv1beta1.ClusterComponentStorage), &sts)).ToNot(HaveOccurred())
					Expect(sts.Status.ReadyReplicas).To(Equal(int32(1)))
					Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(2))
					Expect(sts.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-config=/etc/vmestimator/config/streams.yaml"))
					Expect(sts.Spec.Template.Spec.Containers[0].Args).ToNot(ContainElement("-cardinalityMetrics.exposeAt=/cardinality/metrics"))
					Expect(k8sClient.Get(ctx, componentNsn(cr, vmv1beta1.ClusterComponentSelect), &appsv1.Deployment{})).
						To(MatchError(k8serrors.IsNotFound, "isNotFound"))

					sendVMEstimatorSeries(ctx, vmEstimatorWriteURL(cr), 100)
					expectGlobalCardinality(ctx, vmEstimatorQueryURL(cr), 100)
				}),
			Entry("with storage nodes and select", "storage-select",
				&vmv1.VMEstimator{
					Spec: vmv1.VMEstimatorSpec{
						Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}},
						Storage: &vmv1.VMEstimatorStorage{
							PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
								MaxUnavailable: ptr.To(intstr.FromInt32(1)),
							},
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](2),
								ExtraArgs:    vmEstimatorTestArgs,
							},
						},
						Select: &vmv1.VMEstimatorSelect{
							HPA: &vmv1beta1.EmbeddedHPA{
								MinReplicas: ptr.To[int32](1),
								MaxReplicas: 2,
							},
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ExtraArgs: vmEstimatorTestArgs,
							},
						},
					},
				},
				func(cr *vmv1.VMEstimator) {
					var sts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, componentNsn(cr, vmv1beta1.ClusterComponentStorage), &sts)).ToNot(HaveOccurred())
					Expect(sts.Status.ReadyReplicas).To(Equal(int32(2)))
					Expect(sts.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-cardinalityMetrics.exposeAt=/cardinality/metrics"))
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, componentNsn(cr, vmv1beta1.ClusterComponentSelect), &dep)).ToNot(HaveOccurred())
					Expect(dep.Status.ReadyReplicas).To(Equal(int32(1)))
					Expect(k8sClient.Get(ctx, componentNsn(cr, vmv1beta1.ClusterComponentStorage), &policyv1.PodDisruptionBudget{})).ToNot(HaveOccurred())
					Expect(k8sClient.Get(ctx, componentNsn(cr, vmv1beta1.ClusterComponentSelect), &autoscalingv2.HorizontalPodAutoscaler{})).ToNot(HaveOccurred())

					// the same series are sent in several requests in order to spread them among storage nodes,
					// select nodes merge estimations of all storage nodes, so duplicates aren't counted
					for range 4 {
						sendVMEstimatorSeries(ctx, vmEstimatorWriteURL(cr), 100)
					}
					expectGlobalCardinality(ctx, vmEstimatorQueryURL(cr), 100)
				}),
		)

		It("should load streams from configmap", func() {
			nsn.Name = "streams-configmap"
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmestimator-e2e-streams",
					Namespace: namespace,
				},
				Data: map[string]string{
					"streams.yaml": "streams:\n  - interval: 10m\n    group_by: [job]\n",
				},
			}
			Expect(k8sClient.Create(ctx, cm)).ToNot(HaveOccurred())
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cm)).ToNot(HaveOccurred())
			})
			cr := &vmv1.VMEstimator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: namespace,
				},
				Spec: vmv1.VMEstimatorSpec{
					StreamsConfigMap: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: cm.Name},
						Key:                  "streams.yaml",
					},
				},
			}
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualStatefulsetAppReadyTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			var generated corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetConfigMapName()}, &generated)).ToNot(HaveOccurred())
			var c struct {
				Streams []vmv1.VMEstimatorStream `yaml:"streams"`
			}
			Expect(yaml.UnmarshalStrict([]byte(generated.Data["streams.yaml"]), &c)).ToNot(HaveOccurred())
			Expect(c.Streams).To(Equal([]vmv1.VMEstimatorStream{
				{Interval: "10m", GroupBy: []string{"job"}},
			}))
		})

		It("should report incorrect spec at status", func() {
			nsn.Name = "incorrect-spec"
			cr := &vmv1.VMEstimator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: namespace,
				},
				Spec: vmv1.VMEstimatorSpec{
					// duplicated streams pass CRD schema validation, but are rejected by the operator
					Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}, {Interval: "5m"}},
				},
			}
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusFailed)

			var got vmv1.VMEstimator
			Expect(k8sClient.Get(ctx, nsn, &got)).ToNot(HaveOccurred())
			Expect(got.Status.Reason).To(ContainSubstring("produce identical time series"))
			Expect(k8sClient.Get(ctx, componentNsn(cr, vmv1beta1.ClusterComponentStorage), &appsv1.StatefulSet{})).
				To(MatchError(k8serrors.IsNotFound, "isNotFound"))
		})

		It("should reload config on streams change without pods restart", func() {
			nsn.Name = "streams-change"
			cr := &vmv1.VMEstimator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: namespace,
				},
				Spec: vmv1.VMEstimatorSpec{
					Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}},
				},
			}
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualStatefulsetAppReadyTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			stsNsn := componentNsn(cr, vmv1beta1.ClusterComponentStorage)
			var sts appsv1.StatefulSet
			Expect(k8sClient.Get(ctx, stsNsn, &sts)).ToNot(HaveOccurred())
			prevGeneration := sts.Generation

			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualStatefulsetAppReadyTimeout, func() {
				Eventually(func() error {
					var toUpdate vmv1.VMEstimator
					if err := k8sClient.Get(ctx, nsn, &toUpdate); err != nil {
						return err
					}
					toUpdate.Spec.Streams = append(toUpdate.Spec.Streams, vmv1.VMEstimatorStream{Interval: "5m", GroupBy: []string{"__name__"}})
					return k8sClient.Update(ctx, &toUpdate)
				}, eventualDeploymentAppReadyTimeout).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			var generated corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetConfigMapName()}, &generated)).ToNot(HaveOccurred())
			Expect(generated.Data["streams.yaml"]).To(ContainSubstring("__name__"))

			// configuration is reloaded by config-reloader, so statefulset isn't updated
			Expect(k8sClient.Get(ctx, stsNsn, &sts)).ToNot(HaveOccurred())
			Expect(sts.Generation).To(Equal(prevGeneration))
			var pods corev1.PodList
			Expect(k8sClient.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabels(cr.SelectorLabels(vmv1beta1.ClusterComponentStorage)))).ToNot(HaveOccurred())
			Expect(pods.Items).To(HaveLen(1))
			Expect(pods.Items[0].Annotations).To(HaveKey("configmap-sync-lastupdate-at"))
		})
	})
})
