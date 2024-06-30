package e2e

import (
	"fmt"
	"path"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"

	operator "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
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
					},
					)).To(BeNil())
					time.Sleep(time.Second * 8)
					Eventually(func() error {
						err := k8sClient.Get(context.Background(), types.NamespacedName{
							Name:      Name,
							Namespace: Namespace,
						}, &operator.VMAlert{})
						if errors.IsNotFound(err) {
							return nil
						}
						return fmt.Errorf("want NotFound error, got: %w", err)
					}, 60, 1).Should(BeNil())
				})
				It("should create", func() {
					Expect(k8sClient.Create(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: Namespace,
							Name:      Name,
						},
						Spec: operator.VMAlertSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							Notifier:     &operator.VMAlertNotifierSpec{URL: "http://alert-manager-url:9093"},
							Notifiers:    []operator.VMAlertNotifierSpec{{URL: "http://alert-manager-2:9093"}},
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://some-datasource-url:8428",
							},
							ExtraEnvs: []corev1.EnvVar{
								{
									Name:  "external_url",
									Value: "http://external-url.com",
								},
							},
						},
					})).Should(Succeed())
					vmAlert := &operator.VMAlert{}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: Namespace, Name: Name}, vmAlert)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, Namespace, vmAlert.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
				})
				It("should create with remote read and notifier tls", func() {
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
					time.Sleep(time.Second * 8)
					Expect(k8sClient.Create(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: Namespace,
							Name:      Name,
						},
						Spec: operator.VMAlertSpec{
							ReplicaCount: pointer.Int32Ptr(1),
							Notifiers: []operator.VMAlertNotifierSpec{
								{
									URL: "http://alert-manager-url:9093",
									HTTPAuth: operator.HTTPAuth{
										TLSConfig: &operator.TLSConfig{
											CertFile: path.Join(operator.SecretsDir, tlsSecretName, "remote-cert"),
											KeyFile:  path.Join(operator.SecretsDir, tlsSecretName, "remote-key"),
											CAFile:   path.Join(operator.SecretsDir, tlsSecretName, "remote-ca"),
										},
									},
								},
							},
							Secrets: []string{tlsSecretName},
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://some-datasource-url:8428",
								HTTPAuth: operator.HTTPAuth{
									TLSConfig: &operator.TLSConfig{
										CertFile: path.Join(operator.SecretsDir, tlsSecretName, "remote-cert"),
										KeyFile:  path.Join(operator.SecretsDir, tlsSecretName, "remote-key"),
										CAFile:   path.Join(operator.SecretsDir, tlsSecretName, "remote-ca"),
									},
								},
							},
							RemoteRead: &operator.VMAlertRemoteReadSpec{
								URL: "http://some-vmsingle-url",
								HTTPAuth: operator.HTTPAuth{
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
							Notifier: &operator.VMAlertNotifierSpec{URL: "http://some-alertmanager:9093"},
						},
					})).To(BeNil())
					time.Sleep(time.Second * 2)
				})
				JustAfterEach(func() {
					Expect(k8sClient.Delete(context.TODO(), &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
					})).To(BeNil())
					time.Sleep(time.Second * 8)
					Eventually(func() error {
						err := k8sClient.Get(context.Background(), types.NamespacedName{
							Name:      name,
							Namespace: namespace,
						}, &operator.VMAlert{})
						if errors.IsNotFound(err) {
							return nil
						}
						return fmt.Errorf("want NotFound error, got: %w", err)
					}, 60, 1).Should(BeNil())
				})
				It("Should expand vmalert up to 3 replicas with custom prefix", func() {
					vmAlert := &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
						Spec: operator.VMAlertSpec{
							Notifier: &operator.VMAlertNotifierSpec{
								URL: "http://some-notifier-url",
							},
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://vmsingle-url:8428",
							},
						},
					}
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, vmAlert.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, vmAlert)).To(BeNil())
					vmAlert.Spec.ReplicaCount = pointer.Int32Ptr(3)
					vmAlert.Spec.LogLevel = "INFO"
					vmAlert.Spec.ExtraArgs = map[string]string{"http.pathPrefix": "/somenew/prefix"}
					Expect(k8sClient.Update(context.TODO(), vmAlert)).To(BeNil())
					Eventually(func() string {
						return expectPodCount(k8sClient, 3, namespace, vmAlert.SelectorLabels())
					}, 60, 1).Should(BeEmpty())
				})
				It("should update revisionHistoryLimit of vmalert to 3", func() {
					namespacedName := types.NamespacedName{Name: fmt.Sprintf("vmalert-%s", name), Namespace: namespace}
					Eventually(func() int32 {
						return getRevisionHistoryLimit(k8sClient, namespacedName)
					}, 60).Should(Equal(int32(10)))
					vmAlert := &operator.VMAlert{
						ObjectMeta: metav1.ObjectMeta{
							Name:      name,
							Namespace: namespace,
						},
						Spec: operator.VMAlertSpec{
							Notifier: &operator.VMAlertNotifierSpec{
								URL: "http://some-notifier-url",
							},
							Datasource: operator.VMAlertDatasourceSpec{
								URL: "http://vmsingle-url:8428",
							},
						},
					}
					Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, vmAlert)).To(BeNil())
					vmAlert.Spec.RevisionHistoryLimitCount = pointer.Int32Ptr(3)
					Expect(k8sClient.Update(context.TODO(), vmAlert)).To(BeNil())
					Eventually(func() int32 {
						return getRevisionHistoryLimit(k8sClient, namespacedName)
					}, 60).Should(Equal(int32(3)))
				})
			})
		},
		)
	})
})
