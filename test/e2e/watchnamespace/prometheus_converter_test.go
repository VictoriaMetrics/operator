package watchnamespace

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

var _ = Describe("VM Operator", func() {
	var namespace string

	processIdxSuffix := fmt.Sprintf("-%d", GinkgoParallelProcess())
	JustBeforeEach(func() {
		CreateObjects(
			&monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-monitor" + processIdxSuffix,
					Namespace: namespace,
				},
				Spec: monitoringv1.ServiceMonitorSpec{
					Endpoints: []monitoringv1.Endpoint{{
						Port: "9999",
					}},
				},
			},
			&monitoringv1.PodMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-monitor" + processIdxSuffix,
					Namespace: namespace,
				},
				Spec: monitoringv1.PodMonitorSpec{
					PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{{
						Port: ptr.To("9999"),
					}},
				},
			},
			&monitoringv1.Probe{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-probe" + processIdxSuffix,
					Namespace: namespace,
				},
			},
			&monitoringv1.PrometheusRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-prometheus-rule" + processIdxSuffix,
					Namespace: namespace,
				},
			},
		)
	})

	AfterEach(func() {
		DeleteAllObjectsOf(namespace,
			&monitoringv1.ServiceMonitorList{},
			&vmv1beta1.VMServiceScrapeList{},
			&monitoringv1.PodMonitorList{},
			&vmv1beta1.VMPodScrapeList{},
			&monitoringv1.ProbeList{},
			&vmv1beta1.VMProbeList{},
			&monitoringv1.PrometheusRuleList{},
			&vmv1beta1.VMRuleList{},
		)
	})

	vmObjectListProtos := []client.ObjectList{
		&vmv1beta1.VMServiceScrapeList{},
		&vmv1beta1.VMPodScrapeList{},
		&vmv1beta1.VMProbeList{},
		&vmv1beta1.VMRuleList{},
	}

	Context("when Prometheus operator objects are inside WATCH_NAMESPACE", func() {
		BeforeEach(func() {
			namespace = includedNamespace
		})

		It("should convert Prometheus operator objects to VictoriaMetrics operator objects", func() {
			Eventually(func() []client.Object {
				return ListObjectsInNamespace(namespace, vmObjectListProtos)
			}, 60, 1).ShouldNot(BeEmpty())
		})
	})

	Context("when Prometheus operator objects are outside WATCH_NAMESPACE", func() {
		BeforeEach(func() {
			namespace = excludedNamespace
		})

		It("should NOT convert Prometheus operator objects to to VictoriaMetrics operator objects", func() {
			Consistently(ListObjectsInNamespace, 10, 1).WithArguments(namespace, vmObjectListProtos).Should(BeEmpty())
		})
	})
})
