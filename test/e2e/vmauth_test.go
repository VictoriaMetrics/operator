package e2e

import (
	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

//nolint:dupl
var _ = Describe("test vmauth Controller", func() {
	Context("e2e ", func() {
		var ctx context.Context
		namespace := "default"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}

		Context("crud", func() {

			JustBeforeEach(func() {
				ctx = context.Background()
			})
			AfterEach(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, &v1beta1vm.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      namespacedName.Name,
						Namespace: namespacedName.Namespace,
					},
				})).To(Succeed())
				Eventually(func() error {
					return k8sClient.Get(ctx, namespacedName, &v1beta1vm.VMAuth{})
				}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
			})
			DescribeTable("should create vmauth", func(name string, cr *v1beta1vm.VMAuth, verify func(cr *v1beta1vm.VMAuth)) {
				namespacedName.Name = name
				cr.Name = name
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAuth{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout).Should(Succeed())
				verify(cr)
			},
				Entry("with 1 replica", "replica-1", &v1beta1vm.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespacedName.Namespace,
					},
					Spec: v1beta1vm.VMAuthSpec{
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
							{
								URLPrefix: []string{"http://localhost:8490"},
								SrcPaths:  []string{"/.*"},
							},
						},
					},
				}, func(cr *v1beta1vm.VMAuth) {
					Expect(expectPodCount(k8sClient, 1, cr.Namespace, cr.SelectorLabels())).To(BeEmpty())
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).To(Succeed())
					ps := dep.Spec.Template.Spec
					reloaderContainer := ps.Containers[1]
					Expect(reloaderContainer.Name).To(Equal("config-reloader"))
					Expect(reloaderContainer.Resources.Limits.Cpu().CmpInt64(0)).To(Equal(0))
					Expect(reloaderContainer.Resources.Limits.Memory().CmpInt64(0)).To(Equal(0))
					Expect(reloaderContainer.Resources.Requests.Cpu()).To(Equal(ptr.To(resource.MustParse("10m"))))
					Expect(reloaderContainer.Resources.Requests.Memory()).To(Equal(ptr.To(resource.MustParse("25Mi"))))
				}),
				Entry("with strict security and vm config-reloader", "strict-with-reloader", &v1beta1vm.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespacedName.Namespace,
					},
					Spec: v1beta1vm.VMAuthSpec{
						CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
							UseStrictSecurity:   ptr.To(true),
							UseDefaultResources: ptr.To(false),
						},
						CommonConfigReloaderParams: v1beta1vm.CommonConfigReloaderParams{
							UseVMConfigReloader: ptr.To(true),
						},
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount:                        ptr.To[int32](1),
							DisableAutomountServiceAccountToken: true,
						},
						UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
							{
								URLPrefix: []string{"http://localhost:8490"},
								SrcPaths:  []string{"/.*"},
							},
						},
					},
				}, func(cr *v1beta1vm.VMAuth) {
					Expect(expectPodCount(k8sClient, 1, cr.Namespace, cr.SelectorLabels())).To(BeEmpty())
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).To(Succeed())
					ps := dep.Spec.Template.Spec
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
					vmauthPod := mustGetFirstPod(k8sClient, namespace, cr.SelectorLabels())
					Expect(hasVolumeMount(vmauthPod.Spec.Containers[0].VolumeMounts, saTokenMount)).NotTo(Succeed())
					Expect(hasVolume(dep.Spec.Template.Spec.Volumes, "kube-api-access")).To(Succeed())
					Expect(hasVolumeMount(ps.Containers[1].VolumeMounts, saTokenMount)).To(Succeed())
					Expect(hasVolumeMount(ps.InitContainers[0].VolumeMounts, saTokenMount)).To(Succeed())
				}),
			)

			type testStep struct {
				setup  func(*v1beta1vm.VMAuth)
				modify func(*v1beta1vm.VMAuth)
				verify func(*v1beta1vm.VMAuth)
			}
			DescribeTable("should update exist vmauth",
				func(name string, initCR *v1beta1vm.VMAuth, steps ...testStep) {

					initCR.Name = name
					initCR.Namespace = namespace
					namespacedName.Name = name
					// setup test
					Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAuth{}, namespacedName)
					}, eventualDeploymentAppReadyTimeout).Should(Succeed())
					for _, step := range steps {
						if step.setup != nil {
							step.setup(initCR)
						}
						// perform update
						Eventually(func() error {
							var toUpdate v1beta1vm.VMAuth
							Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
							step.modify(&toUpdate)
							return k8sClient.Update(ctx, &toUpdate)
						}, eventualExpandingTimeout).Should(Succeed())
						Eventually(func() error {
							return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAuth{}, namespacedName)
						}, eventualDeploymentAppReadyTimeout).Should(Succeed())
						var updated v1beta1vm.VMAuth
						Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
						// verify results
						step.verify(&updated)
					}

				},
				Entry("by changing replicas to 2", "update-replicas-2",
					&v1beta1vm.VMAuth{
						Spec: v1beta1vm.VMAuthSpec{
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
								{
									URLPrefix: []string{"http://localhost:8490"},
									SrcPaths:  []string{"/.*"},
								},
							},
						},
					},
					testStep{
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.ReplicaCount = ptr.To[int32](2)
						},
						verify: func(cr *v1beta1vm.VMAuth) {
							Eventually(func() string {
								return expectPodCount(k8sClient, 2, namespace, cr.SelectorLabels())
							}, eventualDeploymentPodTimeout).Should(BeEmpty())
						},
					},
				),
				Entry("by switching to vm config reloader", "vm-reloader",
					&v1beta1vm.VMAuth{
						Spec: v1beta1vm.VMAuthSpec{
							SelectAllByDefault: true,
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
								{
									URLPrefix: []string{"http://localhost:8490"},
									SrcPaths:  []string{"/.*"},
								},
							},
						},
					},
					testStep{
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.UseVMConfigReloader = ptr.To(true)
							cr.Spec.UseDefaultResources = ptr.To(false)
						},
						verify: func(cr *v1beta1vm.VMAuth) {
							Eventually(func() string {
								return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
							}, eventualDeploymentPodTimeout).Should(BeEmpty())
						},
					},
				),
				Entry("by switching to internal listen port", "vm-internal-listen",
					&v1beta1vm.VMAuth{
						Spec: v1beta1vm.VMAuthSpec{
							SelectAllByDefault: true,
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},
							CommonConfigReloaderParams: v1beta1vm.CommonConfigReloaderParams{
								UseVMConfigReloader: ptr.To(true),
							},

							UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
								{
									URLPrefix: []string{"http://localhost:8490"},
									SrcPaths:  []string{"/.*"},
								},
							},
						},
					},
					testStep{
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.InternalListenPort = "8426"
						},
						verify: func(cr *v1beta1vm.VMAuth) {
							Eventually(func() string {
								return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
							}, eventualDeploymentPodTimeout).Should(BeEmpty())
							pod := mustGetFirstPod(k8sClient, cr.Namespace, cr.SelectorLabels())
							Expect(pod.Spec.Containers).To(HaveLen(2))
							ac := pod.Spec.Containers[0]
							Expect(ac.Ports).To(HaveLen(2))
							Expect(ac.Ports[1].ContainerPort).To(Equal(int32(8426)))
							nsn := types.NamespacedName{
								Namespace: cr.Namespace,
								Name:      cr.PrefixedName(),
							}
							var svc corev1.Service
							Expect(k8sClient.Get(ctx, nsn, &svc)).To(Succeed())
							Expect(svc.Spec.Ports).To(HaveLen(2))
							var vmss v1beta1vm.VMServiceScrape
							Expect(k8sClient.Get(ctx, nsn, &vmss)).To(Succeed())
							Expect(vmss.Spec.Endpoints).To(HaveLen(1))
							ep := vmss.Spec.Endpoints[0]
							Expect(ep.Port).To(Equal("internal"))
						},
					},
				),
				Entry("by removing podDistruptionBudget and keeping exist ingress", "vm-keep-ingress-change-pdb",
					&v1beta1vm.VMAuth{
						Spec: v1beta1vm.VMAuthSpec{
							SelectAllByDefault: true,
							CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](2),
							},
							PodDisruptionBudget: &v1beta1vm.EmbeddedPodDisruptionBudgetSpec{
								MaxUnavailable: &intstr.IntOrString{IntVal: 1},
							},
							UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
								{
									URLPrefix: []string{"http://localhost:8490"},
									SrcPaths:  []string{"/.*"},
								},
							},
						},
					},
					testStep{
						setup: func(cr *v1beta1vm.VMAuth) {
							ing := &networkingv1.Ingress{
								// intentionally use the same prefixed name
								ObjectMeta: metav1.ObjectMeta{
									Name:      cr.PrefixedName(),
									Namespace: cr.Namespace,
								},
								Spec: networkingv1.IngressSpec{
									Rules: []networkingv1.IngressRule{
										{
											Host: "vmauth.example.com",
											IngressRuleValue: networkingv1.IngressRuleValue{
												HTTP: &networkingv1.HTTPIngressRuleValue{
													Paths: []networkingv1.HTTPIngressPath{
														{Path: "/", PathType: ptr.To(networkingv1.PathTypePrefix), Backend: networkingv1.IngressBackend{
															Service: &networkingv1.IngressServiceBackend{
																Name: cr.PrefixedName(),
																Port: networkingv1.ServiceBackendPort{Number: 8427},
															},
														}},
													},
												},
											},
										},
									},
								},
							}
							Expect(k8sClient.Create(ctx, ing)).To(Succeed())
							Expect(k8sClient.Get(ctx,
								types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace},
								&policyv1.PodDisruptionBudget{})).To(Succeed())
						},
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.PodDisruptionBudget = nil
						},
						verify: func(cr *v1beta1vm.VMAuth) {
							Eventually(func() string {
								return expectPodCount(k8sClient, 2, namespace, cr.SelectorLabels())
							}, eventualDeploymentPodTimeout).Should(BeEmpty())
							nsn := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
							Expect(k8sClient.Get(ctx, nsn, &networkingv1.Ingress{})).To(Succeed())
							Eventually(func() error {
								return k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})
							}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
						},
					},
					testStep{
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.PodDisruptionBudget = &v1beta1vm.EmbeddedPodDisruptionBudgetSpec{
								MaxUnavailable: &intstr.IntOrString{IntVal: 1},
							}
						},
						verify: func(cr *v1beta1vm.VMAuth) {
							nsn := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
							Expect(k8sClient.Get(ctx, nsn, &networkingv1.Ingress{})).To(Succeed())
							Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).To(Succeed())
							Expect(k8sClient.Delete(ctx, &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
								Name:      nsn.Name,
								Namespace: nsn.Namespace,
							}})).To(Succeed())
						},
					},
				),
				Entry("by migrating from configSecret to externalConfig.secretRef", "ext-config",
					&v1beta1vm.VMAuth{
						Spec: v1beta1vm.VMAuthSpec{
							SelectAllByDefault: true,
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
								{
									URLPrefix: []string{"http://localhost:8490"},
									SrcPaths:  []string{"/.*"},
								},
							},
						},
					},
					testStep{
						setup: func(v *v1beta1vm.VMAuth) {
							extSecret := &corev1.Secret{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "auth-ext-config",
									Namespace: namespace,
								},
								StringData: map[string]string{
									"config.yaml": `
 unauthorized_user:
  url_map:
  - src_paths:
    - "/.*"
    url_prefix: "http://vmsingle-some-url:8429"`,
								},
							}
							Expect(k8sClient.Create(ctx, extSecret)).To(Succeed())
							DeferCleanup(func(specCtx SpecContext) {
								Expect(k8sClient.Delete(ctx, extSecret)).To(Succeed())
							})
						},
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.ConfigSecret = "auth-ext-config"
						},
						verify: func(cr *v1beta1vm.VMAuth) {
							var dep appsv1.Deployment
							Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).
								To(Succeed())
							Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
							Eventually(func() string {
								return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
							}, eventualDeploymentPodTimeout).Should(BeEmpty())

						},
					},
					testStep{
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.ConfigSecret = ""
							cr.Spec.ExternalConfig.SecretRef = &corev1.SecretKeySelector{
								Key: "config.yaml",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "auth-ext-config",
								},
							}
						},
						verify: func(cr *v1beta1vm.VMAuth) {
							var dep appsv1.Deployment
							Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).
								To(Succeed())
							Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
							Expect(dep.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
							Eventually(func() string {
								return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
							}, eventualDeploymentPodTimeout).Should(BeEmpty())

						},
					},
				),
				Entry("by switching to local config", "local-config",
					&v1beta1vm.VMAuth{
						Spec: v1beta1vm.VMAuthSpec{
							SelectAllByDefault: true,
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
								{
									URLPrefix: []string{"http://localhost:8490"},
									SrcPaths:  []string{"/.*"},
								},
							},
						},
					},
					testStep{
						setup: func(v *v1beta1vm.VMAuth) {
							extSecret := &corev1.Secret{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "local-ext-config",
									Namespace: namespace,
								},
								StringData: map[string]string{
									"vmauth.yaml": `
 unauthorized_user:
  url_map:
  - src_paths:
    - "/.*"
    url_prefix: "http://vmsingle-some-url:8429"`,
								},
							}
							Expect(k8sClient.Create(ctx, extSecret)).To(Succeed())
							DeferCleanup(func(specCtx SpecContext) {
								Expect(k8sClient.Delete(ctx, extSecret)).To(Succeed())
							})
						},
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.ExternalConfig.LocalPath = "/etc/local-config/vmauth.yaml"
							cr.Spec.Volumes = append(cr.Spec.Volumes, corev1.Volume{
								Name: "local-cfg",
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: "local-ext-config",
									},
								},
							})
							cr.Spec.VolumeMounts = append(cr.Spec.VolumeMounts, corev1.VolumeMount{
								Name:      "local-cfg",
								MountPath: "/etc/local-config",
							})
						},
						verify: func(cr *v1beta1vm.VMAuth) {
							var dep appsv1.Deployment
							Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).
								To(Succeed())
							Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
							Eventually(func() string {
								return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
							}, eventualDeploymentPodTimeout).Should(BeEmpty())

						},
					},
				),
				Entry("by switching to proxy-protocol", "proxy-protocol",
					&v1beta1vm.VMAuth{
						Spec: v1beta1vm.VMAuthSpec{
							SelectAllByDefault: true,
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonConfigReloaderParams: v1beta1vm.CommonConfigReloaderParams{
								UseVMConfigReloader: ptr.To(true),
							},
							UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
								{
									URLPrefix: []string{"http://localhost:8490"},
									SrcPaths:  []string{"/.*"},
								},
							},
						},
					},
					testStep{
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.UseProxyProtocol = true
						},
						verify: func(cr *v1beta1vm.VMAuth) {},
					},
					testStep{
						modify: func(cr *v1beta1vm.VMAuth) {
							cr.Spec.UseVMConfigReloader = ptr.To(false)
						},
						verify: func(cr *v1beta1vm.VMAuth) {},
					},
				),
			)
		})
	})
})
