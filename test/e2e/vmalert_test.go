package e2e

import (
	"path"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

//nolint:dupl
var _ = Describe("test  vmalert Controller", func() {
	ctx := context.Background()

	Context("e2e vmalert", func() {
		namespace := "default"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &v1beta1vm.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
			},
			)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(context.Background(), types.NamespacedName{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				}, &v1beta1vm.VMAlert{})
			}, eventualDeletionTimeout, 1).Should(MatchError(errors.IsNotFound, "IsNotFound"))
		})
		tlsSecretName := "vmalert-remote-tls"
		DescribeTable("should create vmalert",
			func(name string, cr *v1beta1vm.VMAlert, setup func(), verify func(*v1beta1vm.VMAlert)) {

				cr.Name = name
				namespacedName.Name = name
				if setup != nil {
					setup()
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAlert{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout,
				).Should(Succeed())

				var created v1beta1vm.VMAlert
				Expect(k8sClient.Get(ctx, namespacedName, &created)).To(Succeed())
				verify(&created)

			},
			Entry("with extra env and read source", "with-extra-env",
				&v1beta1vm.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespacedName.Namespace,
					},
					Spec: v1beta1vm.VMAlertSpec{
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
							ExtraEnvs: []corev1.EnvVar{
								{
									Name:  "external_url",
									Value: "http://external-url.com",
								},
							},
						},
						Notifier:  &v1beta1vm.VMAlertNotifierSpec{URL: "http://alert-manager-url:9093"},
						Notifiers: []v1beta1vm.VMAlertNotifierSpec{{URL: "http://alert-manager-2:9093"}},
						Datasource: v1beta1vm.VMAlertDatasourceSpec{
							URL: "http://some-datasource-url:8428",
						},
					},
				},
				nil,
				func(cr *v1beta1vm.VMAlert) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())

				},
			),
			Entry("with remote read and notifier tls", "with-remote-read-notifier-tls",
				&v1beta1vm.VMAlert{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespacedName.Namespace,
					},
					Spec: v1beta1vm.VMAlertSpec{
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
							Secrets:      []string{tlsSecretName},
						},
						Notifiers: []v1beta1vm.VMAlertNotifierSpec{
							{
								URL: "http://alert-manager-url:9093",
								HTTPAuth: v1beta1vm.HTTPAuth{
									TLSConfig: &v1beta1vm.TLSConfig{
										CertFile: path.Join(v1beta1vm.SecretsDir, tlsSecretName, "remote-cert"),
										KeyFile:  path.Join(v1beta1vm.SecretsDir, tlsSecretName, "remote-key"),
										CAFile:   path.Join(v1beta1vm.SecretsDir, tlsSecretName, "remote-ca"),
									},
								},
							},
						},
						Datasource: v1beta1vm.VMAlertDatasourceSpec{
							URL: "http://some-datasource-url:8428",
							HTTPAuth: v1beta1vm.HTTPAuth{
								TLSConfig: &v1beta1vm.TLSConfig{
									CertFile: path.Join(v1beta1vm.SecretsDir, tlsSecretName, "remote-cert"),
									KeyFile:  path.Join(v1beta1vm.SecretsDir, tlsSecretName, "remote-key"),
									CAFile:   path.Join(v1beta1vm.SecretsDir, tlsSecretName, "remote-ca"),
								},
							},
						},
						RemoteRead: &v1beta1vm.VMAlertRemoteReadSpec{
							URL: "http://some-vmsingle-url",
							HTTPAuth: v1beta1vm.HTTPAuth{
								TLSConfig: &v1beta1vm.TLSConfig{
									CA: v1beta1vm.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecretName,
											},
											Key: "remote-ca",
										},
									},
									Cert: v1beta1vm.SecretOrConfigMap{
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
							Namespace: namespacedName.Namespace,
						},
						StringData: map[string]string{
							"remote-ca":   tlsCA,
							"remote-cert": tlsCert,
							"remote-key":  tlsKey,
						},
					}
					Expect(func() error {
						if err := k8sClient.Create(ctx, tlsSecret); err != nil &&
							!errors.IsAlreadyExists(err) {
							return err
						}
						return nil
					}()).To(Succeed())
				},
				func(cr *v1beta1vm.VMAlert) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
					Expect(finalize.SafeDelete(ctx, k8sClient, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tlsSecretName,
							Namespace: namespacedName.Namespace,
						},
					})).To(Succeed())

				},
			),
		)
		existObject := &v1beta1vm.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespacedName.Namespace,
			},
			Spec: v1beta1vm.VMAlertSpec{
				CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To[int32](1),
				},
				Datasource: v1beta1vm.VMAlertDatasourceSpec{
					URL: "http://localhost:8428",
				},
				Notifier: &v1beta1vm.VMAlertNotifierSpec{
					URL: "http://localhost:9093",
				},
			},
		}
		DescribeTable("should update exist vmalert",
			func(name string, modify func(*v1beta1vm.VMAlert), verify func(*v1beta1vm.VMAlert)) {
				// create and wait ready
				existObject := existObject.DeepCopy()
				existObject.Name = name
				namespacedName.Name = name
				Expect(k8sClient.Create(ctx, existObject)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAlert{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				// update and wait ready
				Eventually(func() error {
					var toUpdate v1beta1vm.VMAlert
					Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
					modify(&toUpdate)
					return k8sClient.Update(ctx, &toUpdate)
				}, eventualExpandingTimeout).Should(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAlert{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				// verify
				var updated v1beta1vm.VMAlert
				Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
				verify(&updated)
			},
			Entry("by expand up to 3 replicas with custom prefix", "replica-3-prefix",
				func(cr *v1beta1vm.VMAlert) {
					cr.Spec.ReplicaCount = ptr.To[int32](3)
					cr.Spec.LogLevel = "INFO"
					cr.Spec.ExtraArgs = map[string]string{"http.pathPrefix": "/somenew/prefix"}
				},
				func(cr *v1beta1vm.VMAlert) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 3, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout).Should(BeEmpty())
				}),
			Entry("by updating revisionHistoryLimit to 3", "historylimit-3",
				func(cr *v1beta1vm.VMAlert) {
					cr.Spec.RevisionHistoryLimitCount = ptr.To[int32](3)
				},
				func(cr *v1beta1vm.VMAlert) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout).Should(BeEmpty())
					Expect(getRevisionHistoryLimit(k8sClient, types.NamespacedName{
						Name:      cr.PrefixedName(),
						Namespace: namespacedName.Namespace,
					})).To(Equal(int32(3)))
				}),
			Entry("by switching to vm config-reloader", "vm-reloader",
				func(cr *v1beta1vm.VMAlert) {
					cr.Spec.UseVMConfigReloader = ptr.To(true)
				},
				func(cr *v1beta1vm.VMAlert) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
				}),
		)
	},
	)
})
