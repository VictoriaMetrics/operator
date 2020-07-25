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

var _ = Describe("test  vmagent Controller", func() {
	Context("e2e ", func() {
		Context("crud", func() {
			Context("create", func() {
				name := "create-vma"
				namespace := "default"
				AfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
					})).To(BeNil())
				})
				It("should create vmagent", func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: operator.VMAgentSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							RemoteWrite: []operator.VMAgentRemoteWriteSpec{
								{URL: "http://localhost:8428"},
							},
						}})).To(BeNil())
					currVMAgent := &operator.VMAgent{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, currVMAgent)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, currVMAgent.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
				})

			})
			Context("update", func() {
				name := "update-vma"
				namespace := "default"
				JustAfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						}})).To(BeNil())

				})
				JustBeforeEach(func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
						Spec: operator.VMAgentSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							RemoteWrite: []operator.VMAgentRemoteWriteSpec{
								{URL: "http://some-vm-single:8428"},
							},
						},
					})).To(BeNil())

				})
				It("should expand vmagent up to 3 replicas", func() {
					currVMAgent := &operator.VMAgent{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, currVMAgent)).To(BeNil())
					currVMAgent.Spec.ReplicaCount = pointer.Int32Ptr(3)
					Expect(k8sClient.Update(context.TODO(), currVMAgent))
					Eventually(func() string {
						return expectPodCount(k8sClient, 3, namespace, currVMAgent.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
				})
			})
		})

	})
})
