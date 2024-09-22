package e2e

import (
	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

//nolint:dupl
var _ = Describe("test  vmalertmanager Controller", func() {
	It("must clean up previous test resutls", func() {
		ctx := context.Background()
		// clean up before tests
		Expect(k8sClient.DeleteAllOf(ctx, &v1beta1vm.VMAlertmanager{}, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				Namespace: namespace,
			},
		})).To(Succeed())
		Eventually(func() bool {
			var unDeletedObjects v1beta1vm.VMAlertmanagerList
			Expect(k8sClient.List(ctx, &unDeletedObjects, &client.ListOptions{
				Namespace: namespace,
			})).To(Succeed())
			return len(unDeletedObjects.Items) == 0
		}, eventualDeletionTimeout).Should(BeTrue())

	})
	Context("e2e vmalertmanager", func() {
		ctx := context.Background()
		namespace := "default"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}

		// delete test results
		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &v1beta1vm.VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
			})).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, namespacedName, &v1beta1vm.VMAlertmanager{})
			}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
		})
		DescribeTable("should create alertmanager",
			func(name string, cr *v1beta1vm.VMAlertmanager, verify func(*v1beta1vm.VMAlertmanager)) {
				namespacedName.Name = name
				cr.Name = name
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAlertmanager{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				var created v1beta1vm.VMAlertmanager
				Expect(k8sClient.Get(ctx, namespacedName, &created)).To(Succeed())
				verify(&created)
			},
			Entry("with 1 replica", "replica-1",
				&v1beta1vm.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: v1beta1vm.VMAlertmanagerSpec{
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
				func(cr *v1beta1vm.VMAlertmanager) {
					Expect(expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())).To(BeEmpty())
				},
			),
			Entry("with vm config reloader", "vmreloader-create",
				&v1beta1vm.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: v1beta1vm.VMAlertmanagerSpec{
						CommonConfigReloaderParams: v1beta1vm.CommonConfigReloaderParams{
							UseVMConfigReloader: ptr.To(true),
						},
						CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
							UseDefaultResources: ptr.To(false),
						},
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
				func(cr *v1beta1vm.VMAlertmanager) {
					Expect(expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())).To(BeEmpty())
				},
			),
		)

		existAlertmanager := &v1beta1vm.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespacedName.Namespace,
			},
			Spec: v1beta1vm.VMAlertmanagerSpec{
				CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
		}
		DescribeTable("should update exist alertmanager",
			func(name string, modify func(*v1beta1vm.VMAlertmanager), verify func(*v1beta1vm.VMAlertmanager)) {
				// create and wait ready
				existAlertmanager := existAlertmanager.DeepCopy()
				existAlertmanager.Name = name
				namespacedName.Name = name
				Expect(k8sClient.Create(ctx, existAlertmanager)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAlertmanager{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				// update and wait ready
				Eventually(func() error {
					var toUpdate v1beta1vm.VMAlertmanager
					Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
					modify(&toUpdate)
					return k8sClient.Update(ctx, &toUpdate)
				}, eventualExpandingTimeout).Should(Succeed())
				Eventually(func() error {
					return expectObjectStatusExpanding(ctx, k8sClient, &v1beta1vm.VMAlertmanager{}, namespacedName)
				}, eventualExpandingTimeout).Should(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAlertmanager{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				// verify
				var updated v1beta1vm.VMAlertmanager
				Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
				verify(&updated)
			},
			Entry("by changing replicas to 2", "update-replica-2",
				func(cr *v1beta1vm.VMAlertmanager) {
					cr.Spec.ReplicaCount = ptr.To[int32](2)
				},
				func(cr *v1beta1vm.VMAlertmanager) {
					Expect(expectPodCount(k8sClient, 2, namespace, cr.SelectorLabels())).To(BeEmpty())
				},
			),
			Entry("by changing default config", "change-config",
				func(cr *v1beta1vm.VMAlertmanager) {
					cr.Spec.ReplicaCount = ptr.To[int32](1)
					cr.Spec.ConfigSecret = "non-default-am-config"
					// create secret with config
					Expect(func() error {
						dstSecret := corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{
								Name:      cr.Spec.ConfigSecret,
								Namespace: namespace,
							},
							Data: map[string][]byte{
								"alertmanager.yaml": []byte(alertmanagerTestConf),
							},
						}
						if err := k8sClient.Create(ctx, &dstSecret); err != nil && !errors.IsNotFound(err) {
							return err
						}
						return nil
					}()).To(Succeed())
				},
				func(cr *v1beta1vm.VMAlertmanager) {
					var amCfg corev1.Secret
					Expect(k8sClient.Get(ctx,
						types.NamespacedName{Name: cr.ConfigSecretName(), Namespace: namespace}, &amCfg)).To(Succeed())
					Expect(string(amCfg.Data["alertmanager.yaml"])).To(Equal(alertmanagerTestConf))
					Expect(finalize.SafeDelete(ctx, k8sClient, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      cr.Spec.ConfigSecret,
							Namespace: namespace,
						}})).To(Succeed())
				},
			),

			Entry("by switching to vm config reloader and empty resources", "switch-vm-reloader",
				func(cr *v1beta1vm.VMAlertmanager) {
					cr.Spec.ReplicaCount = ptr.To[int32](1)
					cr.Spec.UseVMConfigReloader = ptr.To(true)
					cr.Spec.UseDefaultResources = ptr.To(false)
				},
				func(cr *v1beta1vm.VMAlertmanager) {
					Expect(expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())).To(BeEmpty())
					var updatedSts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{
						Namespace: namespace,
						Name:      cr.PrefixedName(),
					}, &updatedSts)).To(Succeed())
					Expect(updatedSts.Spec.Template.Spec.Containers).To(HaveLen(2))
					Expect(updatedSts.Spec.Template.Spec.Containers[0].Resources).To(Equal(corev1.ResourceRequirements{}))
					Expect(updatedSts.Spec.Template.Spec.Containers[1].Resources).To(Equal(corev1.ResourceRequirements{}))
				},
			),
		)
	})
})
