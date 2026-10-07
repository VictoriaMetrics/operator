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
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

// vmEstimatorTestArgs disables cardinality metrics caching in order to observe estimations right after ingestion
var vmEstimatorTestArgs = map[string]string{
	"cardinalityMetrics.cacheTTL": "0s",
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

// expectGlobalCardinality waits until the global cardinality estimation exposed at metricsURL becomes close to want
func expectGlobalCardinality(ctx context.Context, metricsURL string, want float64) {
	GinkgoHelper()
	proxyURL, err := buildServiceProxyURL(&k8sCfg, metricsURL)
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
			return 0, fmt.Errorf("cannot find global cardinality estimation at %s", metricsURL)
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
			Entry("in single-node mode by default", "single-default",
				&vmv1.VMEstimator{
					Spec: vmv1.VMEstimatorSpec{
						Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}},
					},
				},
				func(cr *vmv1.VMEstimator) {
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1.VMEstimatorComponentSingle)}, &dep)).ToNot(HaveOccurred())
					Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
					Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-config=/etc/vmestimator/config/streams.yaml"))

					var cm corev1.ConfigMap
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetConfigMapName()}, &cm)).ToNot(HaveOccurred())
					Expect(cm.Data).To(HaveKey("streams.yaml"))

					var svs vmv1beta1.VMServiceScrape
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1.VMEstimatorComponentSingle)}, &svs)).ToNot(HaveOccurred())
				}),
			Entry("in single-node mode with estimations", "single-estimations",
				&vmv1.VMEstimator{
					Spec: vmv1.VMEstimatorSpec{
						Streams: []vmv1.VMEstimatorStream{
							{Interval: "5m"},
							{Interval: "5m", GroupBy: []string{"job"}},
						},
						Single: &vmv1.VMEstimatorSingle{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ExtraArgs:         vmEstimatorTestArgs,
								UseStrictSecurity: ptr.To(true),
							},
						},
					},
				},
				func(cr *vmv1.VMEstimator) {
					sendVMEstimatorSeries(ctx, cr.RemoteWriteURL(), 100)
					expectGlobalCardinality(ctx, cr.AsURL(vmv1.VMEstimatorComponentSingle)+"/metrics", 100)
				}),
			Entry("in single-node mode with optional objects", "single-optional",
				&vmv1.VMEstimator{
					Spec: vmv1.VMEstimatorSpec{
						Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}},
						Single: &vmv1.VMEstimatorSingle{
							PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
								MaxUnavailable: ptr.To(intstr.FromInt32(1)),
							},
							NetworkPolicy: &vmv1beta1.EmbeddedNetworkPolicy{
								Ingress: []networkingv1.NetworkPolicyIngressRule{{}},
							},
							ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
								EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "vmestimator-single-optional-extra"},
							},
						},
					},
				},
				func(cr *vmv1.VMEstimator) {
					name := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1.VMEstimatorComponentSingle)}
					Expect(k8sClient.Get(ctx, name, &policyv1.PodDisruptionBudget{})).ToNot(HaveOccurred())
					Expect(k8sClient.Get(ctx, name, &networkingv1.NetworkPolicy{})).ToNot(HaveOccurred())
					// additional service routes requests to vmestimator pods
					expectHTTPRequestToSucceed(ctx, httpRequestOpts{
						dstURL: fmt.Sprintf("http://vmestimator-single-optional-extra.%s.svc:8490/health", namespace),
					})
				}),
			Entry("in cluster mode with estimations", "cluster-estimations",
				&vmv1.VMEstimator{
					Spec: vmv1.VMEstimatorSpec{
						Streams: []vmv1.VMEstimatorStream{
							{Interval: "5m"},
						},
						Storage: &vmv1.VMEstimatorStorage{
							PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
								MaxUnavailable: ptr.To(intstr.FromInt32(1)),
							},
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](2),
							},
						},
						Select: &vmv1.VMEstimatorSelect{
							HPA: &vmv1beta1.EmbeddedHPA{
								MinReplicas: ptr.To[int32](1),
								MaxReplicas: 2,
							},
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](1),
								ExtraArgs:    vmEstimatorTestArgs,
							},
						},
					},
				},
				func(cr *vmv1.VMEstimator) {
					var sts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage)}, &sts)).ToNot(HaveOccurred())
					Expect(sts.Status.ReadyReplicas).To(Equal(int32(2)))
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage)}, &policyv1.PodDisruptionBudget{})).ToNot(HaveOccurred())
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect)}, &autoscalingv2.HorizontalPodAutoscaler{})).ToNot(HaveOccurred())

					var insertSvc corev1.Service
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedInsertName()}, &insertSvc)).ToNot(HaveOccurred())

					// send data via load-balanced insert service in several requests
					// in order to spread it among storage nodes
					for range 4 {
						sendVMEstimatorSeries(ctx, cr.RemoteWriteURL(), 100)
					}
					// select nodes merge estimations from all storage nodes
					expectGlobalCardinality(ctx, cr.AsURL(vmv1beta1.ClusterComponentSelect)+"/metrics", 100)
				}),
		)

		It("should switch between single-node and cluster modes", func() {
			nsn.Name = "switch-mode"
			cr := &vmv1.VMEstimator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: namespace,
				},
				Spec: vmv1.VMEstimatorSpec{
					Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}},
					Single:  &vmv1.VMEstimatorSingle{},
				},
			}
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			singleNsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1.VMEstimatorComponentSingle)}
			Expect(k8sClient.Get(ctx, singleNsn, &appsv1.Deployment{})).ToNot(HaveOccurred())

			By("switching to the cluster mode")
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualStatefulsetAppReadyTimeout, func() {
				Eventually(func() error {
					var toUpdate vmv1.VMEstimator
					if err := k8sClient.Get(ctx, nsn, &toUpdate); err != nil {
						return err
					}
					toUpdate.Spec.Single = nil
					toUpdate.Spec.Storage = &vmv1.VMEstimatorStorage{}
					toUpdate.Spec.Select = &vmv1.VMEstimatorSelect{}
					return k8sClient.Update(ctx, &toUpdate)
				}, eventualDeploymentAppReadyTimeout).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage)}, &appsv1.StatefulSet{})).ToNot(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect)}, &appsv1.Deployment{})).ToNot(HaveOccurred())
			Eventually(func() error {
				return k8sClient.Get(ctx, singleNsn, &appsv1.Deployment{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "isNotFound"))
			Eventually(func() error {
				return k8sClient.Get(ctx, singleNsn, &corev1.Service{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "isNotFound"))

			By("switching back to the single-node mode")
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
				Eventually(func() error {
					var toUpdate vmv1.VMEstimator
					if err := k8sClient.Get(ctx, nsn, &toUpdate); err != nil {
						return err
					}
					toUpdate.Spec.Storage = nil
					toUpdate.Spec.Select = nil
					return k8sClient.Update(ctx, &toUpdate)
				}, eventualDeploymentAppReadyTimeout).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			Expect(k8sClient.Get(ctx, singleNsn, &appsv1.Deployment{})).ToNot(HaveOccurred())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentStorage)}, &appsv1.StatefulSet{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "isNotFound"))
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1beta1.ClusterComponentSelect)}, &appsv1.Deployment{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "isNotFound"))
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedInsertName()}, &corev1.Service{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "isNotFound"))
		})

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
					Streams: []vmv1.VMEstimatorStream{{Interval: "5m"}},
					StreamsConfigMap: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: cm.Name},
						Key:                  "streams.yaml",
					},
				},
			}
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			var generated corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetConfigMapName()}, &generated)).ToNot(HaveOccurred())
			var c struct {
				Streams []vmv1.VMEstimatorStream `yaml:"streams"`
			}
			Expect(yaml.UnmarshalStrict([]byte(generated.Data["streams.yaml"]), &c)).ToNot(HaveOccurred())
			Expect(c.Streams).To(Equal([]vmv1.VMEstimatorStream{
				{Interval: "5m"},
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
					Streams: []vmv1.VMEstimatorStream{{Interval: "5m", ChurnInterval: "10m"}},
				},
			}
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusFailed)

			var got vmv1.VMEstimator
			Expect(k8sClient.Get(ctx, nsn, &got)).ToNot(HaveOccurred())
			Expect(got.Status.Reason).To(ContainSubstring("churnInterval"))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1.VMEstimatorComponentSingle)}, &appsv1.Deployment{})).
				To(MatchError(k8serrors.IsNotFound, "isNotFound"))
		})

		It("should restart pods on streams change", func() {
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
			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			depNsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName(vmv1.VMEstimatorComponentSingle)}
			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, depNsn, &dep)).ToNot(HaveOccurred())
			prevHash := dep.Spec.Template.Annotations["operator.victoriametrics.com/config-hash"]
			Expect(prevHash).ToNot(BeEmpty())

			expectStatusAfterAction(ctx, &vmv1.VMEstimatorList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
				Eventually(func() error {
					var toUpdate vmv1.VMEstimator
					if err := k8sClient.Get(ctx, nsn, &toUpdate); err != nil {
						return err
					}
					toUpdate.Spec.Streams = append(toUpdate.Spec.Streams, vmv1.VMEstimatorStream{Interval: "5m", GroupBy: []string{"__name__"}})
					return k8sClient.Update(ctx, &toUpdate)
				}, eventualDeploymentAppReadyTimeout).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			Expect(k8sClient.Get(ctx, depNsn, &dep)).ToNot(HaveOccurred())
			Expect(dep.Spec.Template.Annotations["operator.victoriametrics.com/config-hash"]).ToNot(Equal(prevHash))
			Expect(dep.Status.UpdatedReplicas).To(Equal(dep.Status.ReadyReplicas))
		})
	})
})
