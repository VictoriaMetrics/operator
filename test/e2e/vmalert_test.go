package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl
var _ = Describe("test vmalert Controller", Label("vm", "alert"), func() {
	ctx := context.Background()

	Context("e2e vmalert", func() {
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		nsn := types.NamespacedName{
			Namespace: namespace,
		}
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: nsn.Namespace,
				},
			},
			)).To(Succeed())
			waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMAlert{})
		})
		tlsSecretName := "vmalert-remote-tls"
		DescribeTable("should create vmalert",
			func(name string, cr *vmv1beta1.VMAlert, setup func(), verify func(*vmv1beta1.VMAlert)) {

				cr.Name = name
				nsn.Name = name
				if setup != nil {
					setup()
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlert{}, nsn)
				}, eventualDeploymentAppReadyTimeout,
				).Should(Succeed())

				var created vmv1beta1.VMAlert
				Expect(k8sClient.Get(ctx, nsn, &created)).To(Succeed())
				verify(&created)

			},
			Entry("with extra env and read source", "with-extra-env",
				&vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsn.Namespace,
					},
					Spec: vmv1beta1.VMAlertSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
							ExtraEnvs: []corev1.EnvVar{
								{
									Name:  "external_url",
									Value: "http://external-url.com",
								},
							},
						},
						Notifier:  &vmv1beta1.VMAlertNotifierSpec{URL: "http://alert-manager-url:9093"},
						Notifiers: []vmv1beta1.VMAlertNotifierSpec{{URL: "http://alert-manager-2:9093"}},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-datasource-url:8428",
						},
					},
				},
				nil,
				func(cr *vmv1beta1.VMAlert) {
					Eventually(func() error {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(Succeed())

				},
			),
			Entry("with remote read and notifier tls", "with-remote-read-notifier-tls",
				&vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsn.Namespace,
					},
					Spec: vmv1beta1.VMAlertSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
							Secrets:      []string{tlsSecretName},
						},
						Notifiers: []vmv1beta1.VMAlertNotifierSpec{
							{
								URL: "http://alert-manager-url:9093",
								HTTPAuth: vmv1beta1.HTTPAuth{
									TLSConfig: &vmv1beta1.TLSConfig{
										CA: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: tlsSecretName,
												},
												Key: "remote-ca",
											},
										},
										Cert: vmv1beta1.SecretOrConfigMap{
											Secret: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: tlsSecretName,
												},
												Key: "remote-cert",
											},
										},
										KeySecret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecretName,
											},
											Key: "remote-key",
										},
									},
								},
							},
						},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-datasource-url:8428",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecretName,
											},
											Key: "remote-ca",
										},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecretName,
											},
											Key: "remote-cert",
										},
									},
									KeySecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: tlsSecretName,
										},
										Key: "remote-key",
									},
								},
							},
						},
						RemoteRead: &vmv1beta1.VMAlertRemoteReadSpec{
							URL: "http://some-vmsingle-url",
							HTTPAuth: vmv1beta1.HTTPAuth{
								TLSConfig: &vmv1beta1.TLSConfig{
									CA: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecretName,
											},
											Key: "remote-ca",
										},
									},
									Cert: vmv1beta1.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecretName,
											},
											Key: "remote-cert",
										},
									},
									KeySecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: tlsSecretName,
										},
										Key: "remote-key",
									},
								},
							},
						},
					},
				},
				func() {
					tlsSecret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tlsSecretName,
							Namespace: nsn.Namespace,
						},
						StringData: map[string]string{
							"remote-ca":   tlsCA,
							"remote-cert": tlsCert,
							"remote-key":  tlsKey,
						},
					}
					Expect(func() error {
						if err := k8sClient.Create(ctx, tlsSecret); err != nil &&
							!k8serrors.IsAlreadyExists(err) {
							return err
						}
						return nil
					}()).To(Succeed())
				},
				func(cr *vmv1beta1.VMAlert) {
					Eventually(func() error {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(Succeed())
					Expect(finalize.SafeDelete(ctx, k8sClient, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tlsSecretName,
							Namespace: nsn.Namespace,
						},
					})).To(Succeed())

				},
			),
			Entry("with strict security", "strict-security",
				&vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsn.Namespace,
					},
					Spec: vmv1beta1.VMAlertSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							UseStrictSecurity: ptr.To(true),
						},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount:                        ptr.To[int32](1),
							DisableAutomountServiceAccountToken: true,
						},
						SelectAllByDefault: true,
						Notifier:           &vmv1beta1.VMAlertNotifierSpec{URL: "http://alert-manager-url:9093"},
						Notifiers:          []vmv1beta1.VMAlertNotifierSpec{{URL: "http://alert-manager-2:9093"}},
						Datasource: vmv1beta1.VMAlertDatasourceSpec{
							URL: "http://some-datasource-url:8428",
						},
					},
				},
				nil,
				func(cr *vmv1beta1.VMAlert) {
					Eventually(func() error {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(Succeed())
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).To(Succeed())
					// assert security
					Expect(dep.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.SecurityContext.RunAsUser).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(2))
					pc := dep.Spec.Template.Spec.Containers
					Expect(pc[0].SecurityContext).NotTo(BeNil())
					Expect(pc[1].SecurityContext).NotTo(BeNil())
					Expect(pc[0].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(pc[1].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())

					// assert k8s api access
					// vmalert must not have any api access, it doesn't watch for secret changes
					// operator changes pod annotation and triggers config reload
					saTokenMount := "/var/run/secrets/kubernetes.io/serviceaccount"
					vmalertPod := mustGetFirstPod(k8sClient, namespace, cr.SelectorLabels())
					Expect(hasVolumeMount(vmalertPod.Spec.Containers[0].VolumeMounts, saTokenMount)).NotTo(Succeed())
					Expect(hasVolumeMount(vmalertPod.Spec.Containers[1].VolumeMounts, saTokenMount)).NotTo(Succeed())
					Expect(hasVolume(dep.Spec.Template.Spec.Volumes, "kube-api-access")).NotTo(Succeed())
				},
			),
		)
		existObject := &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: nsn.Namespace,
			},
			Spec: vmv1beta1.VMAlertSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To[int32](1),
				},
				Datasource: vmv1beta1.VMAlertDatasourceSpec{
					URL: "http://localhost:8428",
				},
				Notifier: &vmv1beta1.VMAlertNotifierSpec{
					URL: "http://localhost:9093",
				},
			},
		}
		DescribeTable("should update exist vmalert",
			func(name string, modify func(*vmv1beta1.VMAlert), verify func(*vmv1beta1.VMAlert)) {
				// create and wait ready
				existObject := existObject.DeepCopy()
				existObject.Name = name
				nsn.Name = name
				Expect(k8sClient.Create(ctx, existObject)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlert{}, nsn)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				// update and wait ready
				var toUpdate vmv1beta1.VMAlert
				Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
				modify(&toUpdate)
				Expect(k8sClient.Update(ctx, &toUpdate)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlert{}, nsn)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				// verify
				var updated vmv1beta1.VMAlert
				Expect(k8sClient.Get(ctx, nsn, &updated)).To(Succeed())
				verify(&updated)
			},
			Entry("by expand up to 3 replicas with custom prefix", "replica-3-prefix",
				func(cr *vmv1beta1.VMAlert) {
					cr.Spec.ReplicaCount = ptr.To[int32](3)
					cr.Spec.LogLevel = "INFO"
					cr.Spec.ExtraArgs = map[string]string{"http.pathPrefix": "/somenew/prefix"}
				},
				func(cr *vmv1beta1.VMAlert) {
					Eventually(func() error {
						return expectPodCount(k8sClient, 3, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout).Should(Succeed())
				}),
			Entry("by updating revisionHistoryLimit to 3", "historylimit-3",
				func(cr *vmv1beta1.VMAlert) {
					cr.Spec.RevisionHistoryLimitCount = ptr.To[int32](3)
				},
				func(cr *vmv1beta1.VMAlert) {
					Eventually(func() error {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout).Should(Succeed())
					Expect(getRevisionHistoryLimit(k8sClient, types.NamespacedName{
						Name:      cr.PrefixedName(),
						Namespace: nsn.Namespace,
					})).To(Equal(int32(3)))
				}),
		)
	})
})
