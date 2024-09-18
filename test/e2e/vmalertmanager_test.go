package e2e

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"

	operator "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
		ctx := context.Background()
		Context("create", func() {
			name := "create-am"
			namespace := "default"
			JustAfterEach(func() {
				Expect(k8sClient.Delete(ctx, &operator.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					}})).To(Succeed())
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
				Expect(k8sClient.Create(ctx, &operator.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					},
					Spec: operator.VMAlertmanagerSpec{
						CommonApplicationDeploymentParams: operator.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				})).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &operator.VMAlertmanager{}, types.NamespacedName{
						Name:      name,
						Namespace: namespace,
					})
				}, clusterReadyTimeout).Should(Succeed())
			})
		})
		Context("update", func() {
			Name := "update-vmalermanager"
			Namespace := "default"
			configSecretName := "vma-conf"
			JustBeforeEach(func() {
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: Namespace,
						Name:      configSecretName,
					},
					StringData: map[string]string{
						"alertmanager.yaml": alertmanagerTestConf,
					}})).To(Succeed())
				time.Sleep(time.Second * 3)
				Expect(k8sClient.Create(ctx, &operator.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      Name,
						Namespace: Namespace,
					},
					Spec: operator.VMAlertmanagerSpec{
						CommonApplicationDeploymentParams: operator.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						ConfigSecret: configSecretName,
					},
				})).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &operator.VMAlertmanager{}, types.NamespacedName{
						Name:      Name,
						Namespace: Namespace,
					})
				}, clusterReadyTimeout).Should(Succeed())

				time.Sleep(time.Second * 3)
			})
			JustAfterEach(func() {
				Expect(k8sClient.Delete(ctx, &operator.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      Name,
						Namespace: Namespace,
					},
					Spec: operator.VMAlertmanagerSpec{
						CommonApplicationDeploymentParams: operator.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				})).To(Succeed())
				Expect(k8sClient.Delete(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configSecretName,
						Namespace: Namespace,
					},
				})).To(Succeed())
				Eventually(func() error {
					err := k8sClient.Get(context.Background(), types.NamespacedName{
						Name:      Name,
						Namespace: namespace,
					}, &operator.VMAlertmanager{})
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("want NotFound error, got: %w", err)
				}, 10, 1).Should(BeNil())
			})
			It("Should expand alertmanager to 2 replicas", func() {
				currVma := &operator.VMAlertmanager{}
				Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: Namespace, Name: Name}, currVma)).To(Succeed())
					currVma.Spec.ReplicaCount = ptr.To[int32](2)
					return k8sClient.Update(ctx, currVma)
				})).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &operator.VMAlertmanager{}, types.NamespacedName{
						Name:      Name,
						Namespace: Namespace,
					})
				}, clusterReadyTimeout).Should(Succeed())

				Eventually(func() string {
					return expectPodCount(k8sClient, 2, Namespace, currVma.SelectorLabels())
				}, clusterReadyTimeout).Should(BeEmpty())
			})
			It("Should switch alertmanager to custom config reloader", func() {
				currVma := &operator.VMAlertmanager{}
				Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: Namespace, Name: Name}, currVma)).To(Succeed())
					currVma.Spec.CommonConfigReloaderParams.UseCustomConfigReloader = ptr.To(true)
					return k8sClient.Update(ctx, currVma)
				})).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusExpanding(ctx, k8sClient, &operator.VMAlertmanager{}, types.NamespacedName{
						Name:      Name,
						Namespace: Namespace,
					})
				}, clusterReadyTimeout).Should(Succeed())

				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &operator.VMAlertmanager{}, types.NamespacedName{
						Name:      Name,
						Namespace: Namespace,
					})
					// TODO investigate why it takes 60 seconds for pod start-up
				}, 120*time.Second).Should(Succeed())
				Expect(expectPodCount(k8sClient, 1, Namespace, currVma.SelectorLabels())).To(BeEmpty())
			})

		})
	})
})
