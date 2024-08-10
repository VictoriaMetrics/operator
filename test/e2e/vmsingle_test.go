package e2e

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("test  vmsingle Controller", func() {
	Context("e2e vmsingle", func() {
		Context("crud", func() {
			Context("create", func() {
				name := "create-vmsingle"
				namespace := "default"
				AfterEach(func() {
					Expect(k8sClient.Delete(context.Background(), &vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
					})).To(Succeed())
					Eventually(func() string {
						err := k8sClient.Get(context.Background(), types.NamespacedName{
							Name:      name,
							Namespace: namespace,
						}, &vmv1beta1.VMSingle{})
						if errors.IsNotFound(err) {
							return ""
						}
						if err == nil {
							err = fmt.Errorf("expected object to be deleted")
						}
						return err.Error()
					}, 20).Should(BeEmpty())
				})

				It("should create vmSingle", func() {
					Expect(k8sClient.Create(context.TODO(), &vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: vmv1beta1.VMSingleSpec{
							ReplicaCount:    ptr.To[int32](1),
							RetentionPeriod: "1",
							InsertPorts: &vmv1beta1.InsertPorts{
								OpenTSDBPort:     "8081",
								OpenTSDBHTTPPort: "8082",
								GraphitePort:     "8083",
								InfluxPort:       "8084",
							},
						},
					})).To(Succeed())
				})
			})
			Context("update", func() {
				name := "update-vmsingle"
				namespace := "default"

				JustBeforeEach(func() {
					Expect(k8sClient.Create(context.TODO(), &vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
						Spec: vmv1beta1.VMSingleSpec{
							ReplicaCount:    ptr.To[int32](1),
							RetentionPeriod: "1",
						},
					})).To(Succeed())
					time.Sleep(time.Second * 2)
				})
				JustAfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      name,
						},
					})).To(Succeed())
					time.Sleep(time.Second * 3)
				})
				It("should update vmSingle deploy param and ports", func() {
					currVMSingle := &vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
					}
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, currVMSingle.SelectorLabels())
					}, 60, 1).Should(BeEmpty())

					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
						Name:      name,
						Namespace: namespace,
					}, currVMSingle)).To(Succeed())
					currVMSingle.Spec.RetentionPeriod = "3"
					currVMSingle.Spec.InsertPorts = &vmv1beta1.InsertPorts{
						OpenTSDBPort: "8115",
					}
					currVMSingle.Spec.ServiceSpec = &vmv1beta1.AdditionalServiceSpec{
						EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
							Name: "vmsingle-node-access",
						},
						Spec: corev1.ServiceSpec{
							Type: corev1.ServiceTypeNodePort,
						},
					}
					Expect(k8sClient.Update(context.TODO(), currVMSingle)).To(Succeed())
					Eventually(func() error {
						svc := &corev1.Service{}
						return k8sClient.Get(context.TODO(), types.NamespacedName{
							Name:      "vmsingle-node-access",
							Namespace: namespace,
						}, svc)
					}, 60, 1).Should(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, currVMSingle.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
				})
			})
		},
		)
	})
})
