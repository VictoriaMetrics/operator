package e2e

import (
	operator "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"path"
)

var _ = Describe("test  vmalert Controller", func() {
	Context("e2e vmalert", func() {

		Context("crud", func() {
			Context("create", func() {

				Name := "vmalert-example"
				Namespace := "default"
				AfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      Name,
							Namespace: Namespace,
						},
					})).To(BeNil())
				})
				It("should create", func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: Namespace,
							Name:      Name,
						},
						Spec: operator.VMAlertSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							Notifier:     operator.VMAlertNotifierSpec{URL: "http://alert-manager-url:9093"},
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://some-datasource-url:8428",
							},
						},
					})).Should(Succeed())
					vmAlert := &operator.VMAlert{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: Namespace, Name: Name}, vmAlert)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, Namespace, vmAlert.SelectorLabels())
					}, 60, 1).Should(BeEmpty())

				})
				It("should create with remote read tls", func() {
					tlsSecretName := "vmalert-remote-tls"
					tlsSecret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tlsSecretName,
							Namespace: Namespace,
						},
						StringData: map[string]string{
							"remote-ca":   tlsCA,
							"remote-cert": tlsCert,
							"remote-key":  tlsKey,
						},
					}
					Expect(k8sClient.Create(context.TODO(), tlsSecret)).To(Succeed())

					Expect(k8sClient.Create(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: Namespace,
							Name:      Name,
						},
						Spec: operator.VMAlertSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							Notifier:     operator.VMAlertNotifierSpec{URL: "http://alert-manager-url:9093"},
							Secrets:      []string{tlsSecretName},
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://some-datasource-url:8428",
								TLSConfig: &operator.TLSConfig{
									CertFile: path.Join(factory.SecretsDir, tlsSecretName, "remote-cert"),
									KeyFile:  path.Join(factory.SecretsDir, tlsSecretName, "remote-key"),
									CAFile:   path.Join(factory.SecretsDir, tlsSecretName, "remote-ca"),
								},
							},
							RemoteRead: &operator.VMAlertRemoteReadSpec{
								URL: "http://some-vmsingle-url",
								TLSConfig: &operator.TLSConfig{
									CA: operator.SecretOrConfigMap{
										Secret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecretName,
											},
											Key: "remote-ca",
										},
									},
									Cert: operator.SecretOrConfigMap{
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
					})).Should(Succeed())
					vmAlert := &operator.VMAlert{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: Namespace, Name: Name}, vmAlert)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, Namespace, vmAlert.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
					Expect(k8sClient.Delete(context.TODO(), tlsSecret)).To(Succeed())

				})
			})
			Context("update", func() {
				name := "update-vmalert"
				namespace := "default"
				JustBeforeEach(func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
						Spec: operator.VMAlertSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://some-vmsingle:8428",
							},
							Notifier: operator.VMAlertNotifierSpec{URL: "http://some-alertmanager:9093"},
						},
					})).To(BeNil())

				})
				JustAfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
					})).To(BeNil())

				})
				It("Should expand vmalert up to 3 replicas", func() {
					vmAlert := &operator.VMAlert{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, vmAlert)).To(BeNil())
					vmAlert.Spec.ReplicaCount = pointer.Int32Ptr(3)
					vmAlert.Spec.LogLevel = "INFO"
					Expect(k8sClient.Update(context.TODO(), vmAlert)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 3, namespace, vmAlert.SelectorLabels())

					}, 60, 1).Should(BeEmpty())

				})
			})

		},
		)

	})
})
