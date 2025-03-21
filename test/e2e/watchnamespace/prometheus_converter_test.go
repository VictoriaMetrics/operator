package watchnamespace

import (
	"fmt"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			&v1beta1vm.VMServiceScrapeList{},
			&monitoringv1.PodMonitorList{},
			&v1beta1vm.VMPodScrapeList{},
			&monitoringv1.ProbeList{},
			&v1beta1vm.VMProbeList{},
			&monitoringv1.PrometheusRuleList{},
			&v1beta1vm.VMRuleList{},
		)
	})

	vmObjectListProtos := []client.ObjectList{
		&v1beta1vm.VMServiceScrapeList{},
		&v1beta1vm.VMPodScrapeList{},
		&v1beta1vm.VMProbeList{},
		&v1beta1vm.VMRuleList{},
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
