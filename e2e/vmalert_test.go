package e2e

import (
	operator "github.com/VictoriaMetrics/operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

var _ = Describe("test  vmalert Controller", func() {
	Context("e2e vmalert", func() {

		Context("crud", func() {
			Context("create", func() {

				Name := "vmalert-example"
				Namespace := "default"
				AfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      Name,
							Namespace: Namespace,
						},
					})).To(BeNil())
				})
				It("should create", func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: Namespace,
							Name:      Name,
						},
						Spec: operator.VMAlertSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							NotifierURL:  "http://alert-manager-url:9093",
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://some-datasource-url:8428",
							},
						},
					})).Should(Succeed())
					vmAlert := &operator.VMAlert{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: Namespace, Name: Name}, vmAlert)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, Namespace, vmAlert.SelectorLabels())
					}, 60, 1).Should(BeEmpty())

				})

			})
			Context("update", func() {
				name := "update-vmalert"
				namespace := "default"
				JustBeforeEach(func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
						Spec: operator.VMAlertSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://some-vmsingle:8428",
							},
							NotifierURL: "http://some-alertmanager:9091",
						},
					})).To(BeNil())

				})
				JustAfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
					})).To(BeNil())

				})
				It("Should expand vmalert up to 3 replicas", func() {
					vmAlert := &operator.VMAlert{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, vmAlert)).To(BeNil())
					vmAlert.Spec.ReplicaCount = pointer.Int32Ptr(3)
					vmAlert.Spec.LogLevel = "INFO"
					Expect(k8sClient.Update(context.TODO(), vmAlert)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 3, namespace, vmAlert.SelectorLabels())

					}, 60, 1).Should(BeEmpty())

				})
			})

		},
		)

	})
})
