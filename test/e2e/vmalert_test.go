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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmalert"
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
			)).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMAlert{})
		})
		tlsSecretName := "vmalert-remote-tls"

		It("should be idempotent when calling CreateOrUpdate multiple times", func() {
			const attempts = 3
			nsn.Name = "vmalert-idempotent"
			cr := &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1beta1.VMAlertSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					Datasource: vmv1beta1.VMAlertDatasourceSpec{
						URL: "http://some-datasource-url:8428",
					},
					Notifier: &vmv1beta1.VMAlertNotifierSpec{
						URL: "http://alert-manager-url:9093",
					},
				},
			}

			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlert{}, nsn)
			}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			var alertDep appsv1.Deployment
			alertDepName := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
			Expect(k8sClient.Get(ctx, alertDepName, &alertDep)).ToNot(HaveOccurred())
			alertDepRV := alertDep.ResourceVersion

			for i := 0; i < attempts; i++ {
				var latestCR vmv1beta1.VMAlert
				Expect(k8sClient.Get(ctx, nsn, &latestCR)).ToNot(HaveOccurred())
				latestCR.Kind = "VMAlert"
				latestCR.APIVersion = vmv1beta1.GroupVersion.String()
				k8sClient.Scheme().Default(&latestCR)
				Expect(vmalert.CreateOrUpdate(ctx, &latestCR, k8sClient, []string{})).ToNot(HaveOccurred())
			}

			var afterAlertDep appsv1.Deployment
			Expect(k8sClient.Get(ctx, alertDepName, &afterAlertDep)).ToNot(HaveOccurred())
			Expect(afterAlertDep.ResourceVersion).To(Equal(alertDepRV), "VMAlert Deployment resource version should not change")
		})

		DescribeTable("should create vmalert",
			func(name string, cr *vmv1beta1.VMAlert, setup func(), verify func(*vmv1beta1.VMAlert)) {

				cr.Name = name
				nsn.Name = name
				if setup != nil {
					setup()
				}
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlert{}, nsn)
				}, eventualDeploymentAppReadyTimeout,
				).ShouldNot(HaveOccurred())

				var created vmv1beta1.VMAlert
				Expect(k8sClient.Get(ctx, nsn, &created)).ToNot(HaveOccurred())
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
						return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())

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
					}()).ToNot(HaveOccurred())
				},
				func(cr *vmv1beta1.VMAlert) {
					Eventually(func() error {
						return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())
					Expect(finalize.SafeDelete(ctx, k8sClient, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tlsSecretName,
							Namespace: nsn.Namespace,
						},
					})).ToNot(HaveOccurred())

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
						return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).ToNot(HaveOccurred())
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
					vmalertPod := mustGetFirstPod(ctx, k8sClient, &appsv1.ReplicaSet{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Labels:    cr.SelectorLabels(),
						},
					})
					Expect(hasVolumeMount(vmalertPod.Spec.Containers[0].VolumeMounts, saTokenMount)).To(HaveOccurred())
					Expect(hasVolumeMount(vmalertPod.Spec.Containers[1].VolumeMounts, saTokenMount)).To(HaveOccurred())
					Expect(hasVolume(dep.Spec.Template.Spec.Volumes, "kube-api-access")).To(HaveOccurred())
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
				Expect(k8sClient.Create(ctx, existObject)).ToNot(HaveOccurred())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlert{}, nsn)
				}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())
				// update and wait ready
				var toUpdate vmv1beta1.VMAlert
				Expect(k8sClient.Get(ctx, nsn, &toUpdate)).ToNot(HaveOccurred())
				modify(&toUpdate)
				Expect(k8sClient.Update(ctx, &toUpdate)).ToNot(HaveOccurred())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlert{}, nsn)
				}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())
				// verify
				var updated vmv1beta1.VMAlert
				Expect(k8sClient.Get(ctx, nsn, &updated)).ToNot(HaveOccurred())
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
						return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 3)
					}, eventualDeploymentPodTimeout).ShouldNot(HaveOccurred())
				}),
			Entry("by updating revisionHistoryLimit to 3", "historylimit-3",
				func(cr *vmv1beta1.VMAlert) {
					cr.Spec.RevisionHistoryLimitCount = ptr.To[int32](3)
				},
				func(cr *vmv1beta1.VMAlert) {
					Eventually(func() error {
						return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout).ShouldNot(HaveOccurred())
					Expect(getRevisionHistoryLimit(ctx, k8sClient, types.NamespacedName{
						Name:      cr.PrefixedName(),
						Namespace: nsn.Namespace,
					})).To(Equal(int32(3)))
				}),
		)

		It("should skip reconciliation when VMAlert is paused", func() {
			nsn.Name = "vmalert-paused"
			By("creating a VMAlert")
			initialReplicas := int32(1)
			cr := &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1beta1.VMAlertSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: &initialReplicas,
					},
					Datasource: vmv1beta1.VMAlertDatasourceSpec{
						URL: "http://localhost:8428",
					},
					Notifier: &vmv1beta1.VMAlertNotifierSpec{
						URL: "http://localhost:9093",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			deploymentName := types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAlert{}, nsn)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			By("pausing the VMAlert")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, nsn, cr); err != nil {
					return err
				}
				cr.Spec.Paused = true
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			By("attempting to scale the VMAlert while paused")
			updatedReplicas := int32(2)
			Eventually(func() error {
				if err := k8sClient.Get(ctx, nsn, cr); err != nil {
					return err
				}
				cr.Spec.ReplicaCount = &updatedReplicas
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			Consistently(func() int32 {
				var dep appsv1.Deployment
				Expect(k8sClient.Get(ctx, deploymentName, &dep)).ToNot(HaveOccurred())
				return *dep.Spec.Replicas
			}, "10s", "1s").Should(Equal(initialReplicas))

			By("unpausing the VMAlert")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, nsn, cr); err != nil {
					return err
				}
				cr.Spec.Paused = false
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			Eventually(func() int32 {
				var dep appsv1.Deployment
				Expect(k8sClient.Get(ctx, deploymentName, &dep)).ToNot(HaveOccurred())
				return *dep.Spec.Replicas
			}, eventualStatefulsetAppReadyTimeout).Should(Equal(updatedReplicas))
		})
	})
})
