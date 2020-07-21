package e2e

import (
	"context"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

var _ = Describe("test  vmsingle Controller", func() {
	Context("e2e vmsingle", func() {

		Context("crud", func() {
			Context("create", func() {
				name := "create-vmsingle"
				namespace := "default"
				AfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &victoriametricsv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						}})).To(Succeed())
				})

				It("should create vmSingle", func() {
					Expect(k8sClient.Create(context.TODO(), &victoriametricsv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: victoriametricsv1beta1.VMSingleSpec{
							ReplicaCount:    pointer.Int32Ptr(1),
							RetentionPeriod: "1",
						},
					})).To(Succeed())
				})
			})
			Context("update", func() {
				name := "update-vmsingle"
				namespace := "default"

				JustBeforeEach(func() {
					Expect(k8sClient.Create(context.TODO(), &victoriametricsv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: victoriametricsv1beta1.VMSingleSpec{
							ReplicaCount:    pointer.Int32Ptr(1),
							RetentionPeriod: "1",
						},
					})).To(Succeed())

				})
				JustAfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &victoriametricsv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						}})).To(Succeed())
				})
				It("should update vmSingle deploy param", func() {
					currVMSingle := &victoriametricsv1beta1.VMSingle{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
						Name:      name,
						Namespace: namespace,
					}, currVMSingle)).To(BeNil())
					currVMSingle.Spec.RetentionPeriod = "3"
					Expect(k8sClient.Update(context.TODO(), currVMSingle)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, currVMSingle.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
				})
			})

		},
		)

	})
})
