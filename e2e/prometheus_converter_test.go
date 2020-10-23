package e2e

import (
	"context"
	"fmt"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("test  prometheusConverter Controller", func() {
	Context("e2e prome converter", func() {

		Context("crud", func() {
			Context("create", func() {
				name := "create-servicemonitor"
				namespace := "default"
				AfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(),
						&monitoringv1.ServiceMonitor{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Name:      name,
							}})).To(Succeed())
					Expect(k8sClient.Delete(context.TODO(),
						&victoriametricsv1beta1.VMServiceScrape{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Name:      name,
							}})).To(Succeed())

				})

				It("should create prometheus ServiceMonitor", func() {
					Expect(k8sClient.Create(context.TODO(), &monitoringv1.ServiceMonitor{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: monitoringv1.ServiceMonitorSpec{
							Endpoints: []monitoringv1.Endpoint{
								{Port: "8081"},
							},
							Selector: metav1.LabelSelector{MatchLabels: map[string]string{"managed-by": "vm-operator"}},
						},
					})).To(Succeed())
					Eventually(func() error {
						return k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, &victoriametricsv1beta1.VMServiceScrape{
							ObjectMeta: metav1.ObjectMeta{
								Name:      name,
								Namespace: namespace,
							},
						})
					}, 60, 1).Should(Succeed())
				})
				It("should create prometheus ServiceMonitor with relabel filter", func() {
					Expect(k8sClient.Create(context.TODO(), &monitoringv1.ServiceMonitor{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: monitoringv1.ServiceMonitorSpec{
							Endpoints: []monitoringv1.Endpoint{
								{
									Port: "8081",
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
							Selector: metav1.LabelSelector{MatchLabels: map[string]string{"managed-by": "vm-operator"}},
						},
					})).To(Succeed())
					Eventually(func() error {
						expectedVMServiceScrape := &victoriametricsv1beta1.VMServiceScrape{
							ObjectMeta: metav1.ObjectMeta{
								Name:      name,
								Namespace: namespace,
							}}
						err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, expectedVMServiceScrape)
						if err != nil {
							return err
						}
						if len(expectedVMServiceScrape.Spec.Endpoints) != 1 {
							return fmt.Errorf("unexpected number of endpoints, want 1, got: %d", len(expectedVMServiceScrape.Spec.Endpoints))
						}
						endpoint := expectedVMServiceScrape.Spec.Endpoints[0]
						if len(endpoint.RelabelConfigs) != 1 {
							return fmt.Errorf("unexpected relabelConfig for vmservice scrape, want len 1, got endpoint: %v", endpoint)
						}
						return nil
					}, 60, 1).Should(Succeed())
				})

			})

		},
		)

	})
})
