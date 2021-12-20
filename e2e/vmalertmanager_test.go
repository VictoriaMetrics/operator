package e2e

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"

	operator "github.com/VictoriaMetrics/operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

var (
	alertmanagerTestConf = `
global:
  resolve_timeout: 5m
route:
  group_by: ['job']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  receiver: 'webhook'
receivers:
- name: 'webhook'
  webhook_configs:
  - url: 'http://alertmanagerwh:30500/'`
)

var _ = Describe("e2e vmalertmanager ", func() {
	Context("crud", func() {
		Context("create", func() {
			name := "create-am"
			namespace := "default"
			JustAfterEach(func() {
				Expect(k8sClient.Delete(context.TODO(), &operator.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					}})).To(BeNil())
				Eventually(func() error {
					err := k8sClient.Get(context.Background(), types.NamespacedName{
						Name:      name,
						Namespace: namespace,
					}, &operator.VMAlertmanager{})
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("want NotFound error, got: %w", err)
				}, 60, 1).Should(BeNil())
			})
			It("should create vmalertmanager", func() {
				Expect(k8sClient.Create(context.TODO(), &operator.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					},
					Spec: operator.VMAlertmanagerSpec{
						ReplicaCount: pointer.Int32Ptr(1),
					},
				})).To(BeNil())
			})
		})
		Context("update", func() {
			Name := "update-vmalermanager"
			Namespace := "default"
			configSecretName := "vma-conf"
			JustBeforeEach(func() {
				Expect(k8sClient.Create(context.TODO(), &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: Namespace,
						Name:      configSecretName,
					},
					StringData: map[string]string{
						"alertmanager.yaml": alertmanagerTestConf,
					}})).To(BeNil())
				time.Sleep(time.Second * 3)
				Expect(k8sClient.Create(context.TODO(), &operator.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      Name,
						Namespace: Namespace,
					},
					Spec: operator.VMAlertmanagerSpec{
						ReplicaCount: pointer.Int32Ptr(1),
						ConfigSecret: configSecretName,
					},
				})).To(BeNil())
				time.Sleep(time.Second * 3)
			})
			JustAfterEach(func() {
				Expect(k8sClient.Delete(context.TODO(), &operator.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      Name,
						Namespace: Namespace,
					},
					Spec: operator.VMAlertmanagerSpec{
						ReplicaCount: pointer.Int32Ptr(1),
					},
				})).To(BeNil())
				Expect(k8sClient.Delete(context.TODO(), &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configSecretName,
						Namespace: Namespace,
					},
				}))
				Eventually(func() error {
					err := k8sClient.Get(context.Background(), types.NamespacedName{
						Name:      Name,
						Namespace: namespace,
					}, &operator.VMAlertmanager{})
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("want NotFound error, got: %w", err)
				}, 60, 1).Should(BeNil())
			})
			It("Should expand alertmanager to 2 replicas", func() {
				currVma := &operator.VMAlertmanager{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: Namespace, Name: Name}, currVma)).To(BeNil())
				currVma.Spec.ReplicaCount = pointer.Int32Ptr(2)
				Expect(k8sClient.Update(context.TODO(), currVma)).To(BeNil())
				Eventually(func() string {
					return expectPodCount(k8sClient, 2, Namespace, currVma.SelectorLabels())
				}, 180, 5).Should(BeEmpty())
			})

		})
	})
})
