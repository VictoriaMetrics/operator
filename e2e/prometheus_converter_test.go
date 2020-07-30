package e2e

import (
	"context"
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

				It("should create prometheusServiceMonitor", func() {
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
			})

		},
		)

	})
})
