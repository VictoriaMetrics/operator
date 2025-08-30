package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type testCase struct {
	name            string
	source          client.Object
	targetTpl       client.Object
	targetValidator func(client.Object) error
}

var (
	namespace = "default"
	testCases = []testCase{
		{
			name: "PrometheusScrapeConfig-https",
			source: &promv1alpha1.ScrapeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-scrapeconfig",
				},
				Spec: promv1alpha1.ScrapeConfigSpec{
					Scheme: ptr.To("HTTPS"),
					StaticConfigs: []promv1alpha1.StaticConfig{
						{

							Targets: []promv1alpha1.Target{
								"localhost:9100",
							},
						},
					},
				},
			},
			targetTpl: &vmv1beta1.VMScrapeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-scrapeconfig",
				},
			},
			targetValidator: func(obj client.Object) error {
				ss := obj.(*vmv1beta1.VMScrapeConfig)
				if len(ss.Spec.StaticConfigs) != 1 {
					return fmt.Errorf("unexpected len of static config: %d, want 1", len(ss.Spec.StaticConfigs))
				}
				return nil
			},
		},

		{
			name: "PrometheusScrapeConfig",
			source: &promv1alpha1.ScrapeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-scrapeconfig",
				},
				Spec: promv1alpha1.ScrapeConfigSpec{
					StaticConfigs: []promv1alpha1.StaticConfig{
						{
							Targets: []promv1alpha1.Target{
								"http://localhost:9100",
							},
						},
					},
				},
			},
			targetTpl: &vmv1beta1.VMScrapeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-scrapeconfig",
				},
			},
			targetValidator: func(obj client.Object) error {
				ss := obj.(*vmv1beta1.VMScrapeConfig)
				if len(ss.Spec.StaticConfigs) != 1 {
					return fmt.Errorf("unexpected len of static config: %d, want 1", len(ss.Spec.StaticConfigs))
				}
				return nil
			},
		},
		{
			name: "AlertmanagerConfig",
			source: &promv1alpha1.AlertmanagerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-alertmanagerconfig",
				},
				Spec: promv1alpha1.AlertmanagerConfigSpec{
					Route: &promv1alpha1.Route{
						Receiver: "blackhole",
					},
					InhibitRules: []promv1alpha1.InhibitRule{
						{TargetMatch: []promv1alpha1.Matcher{
							{
								Name:  "env",
								Value: "test",
							},
						}},
					},
					Receivers: []promv1alpha1.Receiver{
						{
							Name: "blackhole",
						},
					},
				},
			},
			targetTpl: &vmv1beta1.VMAlertmanagerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-alertmanagerconfig",
				},
			},
			targetValidator: func(obj client.Object) error {
				ss := obj.(*vmv1beta1.VMAlertmanagerConfig)
				if len(ss.Spec.Receivers) != 1 {
					return fmt.Errorf("unexpected number of receivers, want 1, got: %d", len(ss.Spec.Receivers))
				}
				if ss.Spec.Receivers[0].Name != "blackhole" {
					return fmt.Errorf("unexpected name for alertmanagerConfig receiver, want blackhole, got %q",
						ss.Spec.Receivers[0].Name)
				}
				return nil
			},
		},
		{
			name: "ServiceMonitor",
			source: &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-servicemonitor",
				},
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{
						{
							Port: "8081",
							RelabelConfigs: []monitoringv1.RelabelConfig{
								{
									Action: "keep",
								},
								{
									Action:       "drop",
									SourceLabels: []monitoringv1.LabelName{"__address__"},
								},
							},
						},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{"managed-by": "vm-operator"},
					},
				},
			},
			targetTpl: &vmv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-servicemonitor",
				},
			},
			targetValidator: func(obj client.Object) error {
				ss := obj.(*vmv1beta1.VMServiceScrape)
				if len(ss.Spec.Endpoints) != 1 {
					return fmt.Errorf("unexpected number of endpoints, want 1, got: %d", len(ss.Spec.Endpoints))
				}
				endpoint := ss.Spec.Endpoints[0]
				if len(endpoint.RelabelConfigs) != 1 {
					return fmt.Errorf("unexpected relabelConfig for vmservice scrape, want len 1, got endpoint: %v", endpoint)
				}
				return nil
			},
		},
		{
			name: "PodMonitor",
			source: &monitoringv1.PodMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-podmonitor",
				},
				Spec: monitoringv1.PodMonitorSpec{
					PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
						{
							Port: ptr.To("8081"),
						},
					},
				},
			},
			targetTpl: &vmv1beta1.VMPodScrape{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-podmonitor",
				},
			},
			targetValidator: func(obj client.Object) error {
				ps := obj.(*vmv1beta1.VMPodScrape)
				if len(ps.Spec.PodMetricsEndpoints) != 1 {
					return fmt.Errorf("unexpected number of endpoints, want 1, got: %d", len(ps.Spec.PodMetricsEndpoints))
				}
				endpoint := ps.Spec.PodMetricsEndpoints[0]
				if *endpoint.Port != "8081" {
					return fmt.Errorf("unexpected endpoint port, want 8081, got: %s", *endpoint.Port)
				}
				return nil
			},
		},
		{
			name: "PrometheusRule",
			source: &monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-prometheusrule",
				},
				Spec: monitoringv1.PrometheusRuleSpec{
					Groups: []monitoringv1.RuleGroup{
						{
							Name: "groupName",
							Rules: []monitoringv1.Rule{
								{
									Expr:   intstr.FromString("up{job=\"job_name\"}"),
									Record: "job_name_up",
								},
							},
						},
					},
				},
			},
			targetTpl: &vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-prometheusrule",
				},
			},
			targetValidator: func(obj client.Object) error {
				vr := obj.(*vmv1beta1.VMRule)
				if len(vr.Spec.Groups) != 1 {
					return fmt.Errorf("unexpected number of groups, want 1, got: %d", len(vr.Spec.Groups))
				}
				group := vr.Spec.Groups[0]
				if group.Name != "groupName" {
					return fmt.Errorf("unexpected group name, want 'groupName', got: %s", group.Name)
				}
				if len(group.Rules) != 1 {
					return fmt.Errorf("unexpected number of rules, want 1, got: %d", len(group.Rules))
				}
				rule := group.Rules[0]
				if rule.Record != "job_name_up" {
					return fmt.Errorf("unexpected rule Record field, want 'job_name_up', got: %s", rule.Record)
				}
				return nil
			},
		},
		{
			name: "Probe",
			source: &monitoringv1.Probe{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-probe",
				},
				Spec: monitoringv1.ProbeSpec{
					ProberSpec: monitoringv1.ProberSpec{
						URL: "http://example.com/probe",
					},
				},
			},
			targetTpl: &vmv1beta1.VMProbe{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-probe",
				},
			},
			targetValidator: func(obj client.Object) error {
				vp := obj.(*vmv1beta1.VMProbe)
				if vp.Spec.VMProberSpec.URL != "http://example.com/probe" {
					return fmt.Errorf("unexpected prober url, want 'http://example.com/probe', got: %s", vp.Spec.VMProberSpec.URL)
				}
				return nil
			},
		},
	}
)

func getObject(ctx context.Context, obj client.Object) (client.Object, error) {
	obj = obj.DeepCopyObject().(client.Object)
	err := k8sClient.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, obj)
	return obj, err
}

var _ = Describe("test prometheusConverter Controller", func() {
	Context("e2e prome converter", func() {
		for _, testCaseIt := range testCases {
			testCase := testCaseIt
			// adapt test for parallel execution
			// https://onsi.github.io/ginkgo/#patterns-for-parallel-integration-specs
			procSuffix := fmt.Sprintf("-%d", GinkgoParallelProcess())
			testCase.source.SetName(testCase.source.GetName() + procSuffix)
			testCase.targetTpl.SetName(testCase.targetTpl.GetName() + procSuffix)
			ctx := context.Background()
			Context(fmt.Sprintf("crud %s", testCase.name), func() {
				AfterEach(func() {
					k8sClient.Delete(ctx, testCase.source) // nolint:errcheck
					Eventually(func() error {
						_, err := getObject(ctx, testCase.source)
						return err
					}, eventualDeletionTimeout, 1).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

					k8sClient.Delete(ctx, testCase.targetTpl) // nolint:errcheck
					Eventually(func() error {
						_, err := getObject(ctx, testCase.targetTpl)
						if err == nil {
							return fmt.Errorf("Should be deleted")
						}
						return nil
					}, 60, 1).Should(Succeed())
				})

				It("Should convert the object", func() {
					source := testCase.source.DeepCopyObject().(client.Object)

					Expect(k8sClient.Create(ctx, source)).To(Succeed())
					Eventually(func() error {
						target, err := getObject(ctx, testCase.targetTpl)
						if err != nil {
							return err
						}
						return testCase.targetValidator(target)
					}, 60, 1).Should(Succeed())
				})

				It("Should update the converted object", func() {
					source := testCase.source.DeepCopyObject().(client.Object)

					Expect(k8sClient.Create(ctx, source)).To(Succeed())
					Eventually(func() error {
						_, err := getObject(ctx, testCase.targetTpl)
						return err
					}, 60, 1).Should(Succeed())

					labels := source.GetLabels()
					if labels == nil {
						labels = make(map[string]string)
					}
					// Use this hack to trigger update manually for GenerationChange predicate
					// It's not a problem for production workloads
					// Since operator performs period syncs for parent objects
					source.SetGeneration(source.GetGeneration() + 1)
					labels["testKey"] = "testValue"
					source.SetLabels(labels)

					Expect(k8sClient.Update(ctx, source)).To(Succeed())
					Eventually(func() error {
						target, err := getObject(ctx, testCase.targetTpl)
						if err != nil {
							return err
						}
						if target.GetLabels() == nil || target.GetLabels()["testKey"] != "testValue" {
							return fmt.Errorf("unexpected labels, want testKey=testValue, got: %v", target.GetLabels())
						}
						return nil
					}, 60, 1).Should(Succeed())
				})

				It("Should delete the converted object", func() {
					source := testCase.source.DeepCopyObject().(client.Object)

					Expect(k8sClient.Create(ctx, source)).To(Succeed())
					Eventually(func() error {
						_, err := getObject(ctx, testCase.targetTpl)
						return err
					}, 60, 1).Should(Succeed())

					Expect(func() error {
						target, err := getObject(ctx, testCase.targetTpl)
						if err != nil {
							return err
						}
						if target.GetOwnerReferences() == nil {
							return fmt.Errorf("expected owner reference to be non nil, object :%s", target.GetName())
						}
						return nil
					}()).To(Succeed())
					Expect(k8sClient.Delete(ctx, source)).To(Succeed())
					Eventually(func() error {
						_, err := getObject(ctx, testCase.targetTpl)
						return err
					}, eventualDeletionTimeout, 1).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
				})
			})
		}
	})
})
