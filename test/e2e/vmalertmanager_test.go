package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
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
var _ = Describe("test vmalertmanager Controller", Label("vm", "alertmanager"), func() {

	Context("e2e vmalertmanager", func() {
		ctx := context.Background()
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}

		// delete test results
		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1beta1.VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
			})).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, namespacedName, &vmv1beta1.VMAlertmanager{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		})
		DescribeTable("should create alertmanager",
			func(name string, cr *vmv1beta1.VMAlertmanager, verify func(*vmv1beta1.VMAlertmanager)) {
				namespacedName.Name = name
				cr.Name = name
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlertmanager{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				var created vmv1beta1.VMAlertmanager
				Expect(k8sClient.Get(ctx, namespacedName, &created)).To(Succeed())
				verify(&created)
			},
			Entry("with 1 replica", "replica-1",
				&vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
				func(cr *vmv1beta1.VMAlertmanager) {
					Expect(expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())).To(BeEmpty())
				},
			),
			Entry("with vm config reloader", "vmreloader-create",
				&vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
							UseVMConfigReloader: ptr.To(true),
						},
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							UseDefaultResources: ptr.To(false),
						},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
				func(cr *vmv1beta1.VMAlertmanager) {
					Expect(expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())).To(BeEmpty())
				},
			),
			Entry("with strict security and vm config reloader", "strict-vmreloader-create",
				&vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
							UseVMConfigReloader: ptr.To(true),
						},
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							UseDefaultResources: ptr.To(false),
							UseStrictSecurity:   ptr.To(true),
						},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount:                        ptr.To[int32](1),
							DisableAutomountServiceAccountToken: true,
						},
					},
				},
				func(cr *vmv1beta1.VMAlertmanager) {
					Expect(expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())).To(BeEmpty())
					var sts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &sts)).To(Succeed())
					ps := sts.Spec.Template.Spec
					Expect(ps.SecurityContext).NotTo(BeNil())
					Expect(ps.SecurityContext.RunAsNonRoot).NotTo(BeNil())
					Expect(ps.Containers).To(HaveLen(2))
					Expect(ps.InitContainers).To(HaveLen(1))
					Expect(ps.Containers[0].SecurityContext).NotTo(BeNil())
					Expect(ps.Containers[1].SecurityContext).NotTo(BeNil())
					Expect(ps.InitContainers[0].SecurityContext).NotTo(BeNil())
					Expect(ps.Containers[0].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(ps.Containers[1].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(ps.InitContainers[0].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())

					// assert k8s api access
					saTokenMount := "/var/run/secrets/kubernetes.io/serviceaccount"
					alertmanagerPod := mustGetFirstPod(k8sClient, namespace, cr.SelectorLabels())
					Expect(hasVolumeMount(alertmanagerPod.Spec.Containers[0].VolumeMounts, saTokenMount)).NotTo(Succeed())
					Expect(hasVolume(sts.Spec.Template.Spec.Volumes, "kube-api-access")).To(Succeed())
					Expect(hasVolumeMount(ps.Containers[1].VolumeMounts, saTokenMount)).To(Succeed())
					Expect(hasVolumeMount(ps.InitContainers[0].VolumeMounts, saTokenMount)).To(Succeed())

				},
			),
		)

		existAlertmanager := &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespacedName.Namespace,
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
		}
		DescribeTable("should update exist alertmanager",
			func(name string, modify func(*vmv1beta1.VMAlertmanager), verify func(*vmv1beta1.VMAlertmanager)) {
				// create and wait ready
				existAlertmanager := existAlertmanager.DeepCopy()
				existAlertmanager.Name = name
				namespacedName.Name = name
				Expect(k8sClient.Create(ctx, existAlertmanager)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlertmanager{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				// update and wait ready
				Eventually(func() error {
					var toUpdate vmv1beta1.VMAlertmanager
					Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
					modify(&toUpdate)
					return k8sClient.Update(ctx, &toUpdate)
				}, eventualExpandingTimeout).Should(Succeed())
				Eventually(func() error {
					return expectObjectStatusExpanding(ctx, k8sClient, &vmv1beta1.VMAlertmanager{}, namespacedName)
				}, eventualExpandingTimeout).Should(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlertmanager{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				// verify
				var updated vmv1beta1.VMAlertmanager
				Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
				verify(&updated)
			},
			Entry("by changing replicas to 2", "update-replica-2",
				func(cr *vmv1beta1.VMAlertmanager) {
					cr.Spec.ReplicaCount = ptr.To[int32](2)
				},
				func(cr *vmv1beta1.VMAlertmanager) {
					Expect(expectPodCount(k8sClient, 2, namespace, cr.SelectorLabels())).To(BeEmpty())
				},
			),
			Entry("by changing default config", "change-config",
				func(cr *vmv1beta1.VMAlertmanager) {
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
						if err := k8sClient.Create(ctx, &dstSecret); err != nil && !k8serrors.IsNotFound(err) {
							return err
						}
						return nil
					}()).To(Succeed())
				},
				func(cr *vmv1beta1.VMAlertmanager) {
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
				func(cr *vmv1beta1.VMAlertmanager) {
					cr.Spec.ReplicaCount = ptr.To[int32](1)
					cr.Spec.UseVMConfigReloader = ptr.To(true)
					cr.Spec.UseDefaultResources = ptr.To(false)
				},
				func(cr *vmv1beta1.VMAlertmanager) {
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
