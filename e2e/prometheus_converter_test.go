package e2e

import (
	"context"
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type testCase struct {
	name            string
	source          controllerutil.Object
	targetTpl       controllerutil.Object
	targetValidator func(controllerutil.Object) error
}

var (
	namespace = "default"

	testCases = []testCase{
		testCase{
			name: "ServiceMonitor",
			source: &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-servicemonitor",
				},
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{
						{Port: "8081",
							RelabelConfigs: []*monitoringv1.RelabelConfig{
								{
									Action: "keep",
								},
								{
									Action:       "drop",
									SourceLabels: []string{"__address__"},
								},
							},
						},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{"managed-by": "vm-operator"},
					},
				},
			},
			targetTpl: &victoriametricsv1beta1.VMServiceScrape{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-servicemonitor",
				},
			},
			targetValidator: func(obj controllerutil.Object) error {
				ss := obj.(*victoriametricsv1beta1.VMServiceScrape)
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
		testCase{
			name: "PodMonitor",
			source: &monitoringv1.PodMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-podmonitor",
				},
				Spec: monitoringv1.PodMonitorSpec{
					PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
						{
							Port: "8081",
						},
					},
				},
			},
			targetTpl: &victoriametricsv1beta1.VMPodScrape{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-podmonitor",
				},
			},
			targetValidator: func(obj controllerutil.Object) error {
				ps := obj.(*victoriametricsv1beta1.VMPodScrape)
				if len(ps.Spec.PodMetricsEndpoints) != 1 {
					return fmt.Errorf("unexpected number of endpoints, want 1, got: %d", len(ps.Spec.PodMetricsEndpoints))
				}
				endpoint := ps.Spec.PodMetricsEndpoints[0]
				if endpoint.Port != "8081" {
					return fmt.Errorf("unexpected endpoint port, want 8081, got: %s", endpoint.Port)
				}
				return nil
			},
		},
		testCase{
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
			targetTpl: &victoriametricsv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-prometheusrule",
				},
			},
			targetValidator: func(obj controllerutil.Object) error {
				vr := obj.(*victoriametricsv1beta1.VMRule)
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
		testCase{
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
			targetTpl: &victoriametricsv1beta1.VMProbe{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "e2e-test-probe",
				},
			},
			targetValidator: func(obj controllerutil.Object) error {
				vp := obj.(*victoriametricsv1beta1.VMProbe)
				if vp.Spec.VMProberSpec.URL != "http://example.com/probe" {
					return fmt.Errorf("unexpected prober url, want 'http://example.com/probe', got: %s", vp.Spec.VMProberSpec.URL)
				}
				return nil
			},
		},
	}
)

func getObject(obj controllerutil.Object) (controllerutil.Object, error) {
	obj = obj.DeepCopyObject().(controllerutil.Object)
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, obj)
	return obj, err
}

var _ = Describe("test  prometheusConverter Controller", func() {
	Context("e2e prome converter", func() {
		for _, testCaseIt := range testCases {
			testCase := testCaseIt
			Context(fmt.Sprintf("crud %s", testCase.name), func() {
				AfterEach(func() {
					k8sClient.Delete(context.TODO(), testCase.source) // nolint:errcheck
					Eventually(func() error {
						_, err := getObject(testCase.source)
						if err == nil {
							return fmt.Errorf("Should be deleted")
						}
						return nil
					}, 60, 1).Should(Succeed())

					k8sClient.Delete(context.TODO(), testCase.targetTpl) // nolint:errcheck
					Eventually(func() error {
						_, err := getObject(testCase.targetTpl)
						if err == nil {
							return fmt.Errorf("Should be deleted")
						}
						return nil
					}, 60, 1).Should(Succeed())
				})

				It("Should convert the object", func() {
					source := testCase.source.DeepCopyObject().(controllerutil.Object)

					Expect(k8sClient.Create(context.TODO(), source)).To(Succeed())
					Eventually(func() error {
						target, err := getObject(testCase.targetTpl)
						if err != nil {
							return err
						}
						return testCase.targetValidator(target)
					}, 60, 1).Should(Succeed())
				})

				It("Should update the converted object", func() {
					source := testCase.source.DeepCopyObject().(controllerutil.Object)

					Expect(k8sClient.Create(context.TODO(), source)).To(Succeed())
					Eventually(func() error {
						_, err := getObject(testCase.targetTpl)
						return err
					}, 60, 1).Should(Succeed())

					labels := source.GetLabels()
					if labels == nil {
						labels = make(map[string]string)
					}
					labels["testKey"] = "testValue"
					source.SetLabels(labels)

					Expect(k8sClient.Update(context.TODO(), source)).To(Succeed())
					Eventually(func() error {
						target, err := getObject(testCase.targetTpl)
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
					source := testCase.source.DeepCopyObject().(controllerutil.Object)

					Expect(k8sClient.Create(context.TODO(), source)).To(Succeed())
					Eventually(func() error {
						_, err := getObject(testCase.targetTpl)
						return err
					}, 60, 1).Should(Succeed())

					Expect(func() error {
						target, err := getObject(testCase.targetTpl)
						if err != nil {
							return err
						}
						if target.GetOwnerReferences() == nil {
							return fmt.Errorf("expected owner reference to be non nil, object :%s", target.GetName())
						}
						return nil
					}()).To(Succeed())
					Expect(k8sClient.Delete(context.TODO(), source)).To(Succeed())
					Eventually(func() error {
						_, err := getObject(testCase.targetTpl)
						if err == nil {
							return fmt.Errorf("expected converted %s to disappear, name: %s, converted: %s", testCase.name, source.GetName(), testCase.targetTpl.GetName())
						}
						return nil
					}, 60, 1).Should(Succeed())
				})
			})
		}
	})
})
