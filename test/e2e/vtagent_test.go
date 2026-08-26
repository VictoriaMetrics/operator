package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("test vtagent Controller", Label("vt", "agent", "vtagent"), func() {
	ctx := context.Background()
	Context("e2e vtagent", func() {
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		nsn := types.NamespacedName{
			Namespace: namespace,
		}
		tlsSecretName := "vtagent-remote-tls-certs"

		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1.VTAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: nsn.Namespace,
				},
			})).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, nsn, &vmv1.VTAgentList{})
		})

		DescribeTable("should create vtagent",
			func(name string, cr *vmv1.VTAgent, setup func(), verify func(*vmv1.VTAgent)) {

				cr.Name = name
				nsn.Name = name
				if setup != nil {
					setup()
				}
				expectStatusAfterAction(ctx, &vmv1.VTAgentList{}, nsn, eventualDeploymentAppReadyTimeout, func() {
					Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
				}, vmv1beta1.UpdateStatusOperational)

				var created vmv1.VTAgent
				Expect(k8sClient.Get(ctx, nsn, &created)).ToNot(HaveOccurred())
				verify(&created)

			},
			Entry("with 1 replica and rw-settings", "replica-1-rw", &vmv1.VTAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1.VTAgentSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
						{URL: "http://localhost:10428/insert/native"},
						{URL: "http://localhost:10430/insert/native", Format: ptr.To("jsonline")},
					},
					TmpDataPath: ptr.To("/tmp/custom-path"),
					RemoteWriteSettings: &vmv1.VTAgentRemoteWriteSettings{
						MaxBlockSize:  ptr.To(vmv1beta1.BytesString(`15MB`)),
						FlushInterval: ptr.To("2s"),
					},
				},
			}, nil, func(cr *vmv1.VTAgent) {
				Eventually(func() error {
					return expectPodCount(ctx, k8sClient, &appsv1.StatefulSet{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Labels:    cr.SelectorLabels(),
						},
					}, 1)
				}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())
				var sts appsv1.StatefulSet
				Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &sts)).ToNot(HaveOccurred())
				Expect(sts.Spec.VolumeClaimTemplates).To(BeEmpty())
				Expect(sts.Spec.Template.Spec.Volumes).To(BeEmpty())
				Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
				cnt := sts.Spec.Template.Spec.Containers[0]
				Expect(cnt.VolumeMounts).To(BeEmpty())
				Expect(cnt.Args).To(ContainElements("-tmpDataPath=/tmp/custom-path", "-remoteWrite.maxBlockSize=15MB", "-remoteWrite.format=,jsonline"))
			}),
			Entry("with persistent storage and tls remote target", "remote-tls",
				&vmv1.VTAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VTAgentSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
						},
						Storage: &vmv1beta1.StorageSpec{
							VolumeClaimTemplate: vmv1beta1.EmbeddedPersistentVolumeClaim{
								Spec: corev1.PersistentVolumeClaimSpec{
									Resources: corev1.VolumeResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: resource.MustParse("10G"),
										},
									},
								},
							},
						},
						RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
							{URL: "http://localhost:10428/insert/native"},
							{
								URL: "http://localhost:10425/insert/native",
								TLSConfig: &vmv1.TLSConfig{
									CASecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: tlsSecretName,
										},
										Key: "remote-ca",
									},
									CertSecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: tlsSecretName,
										},
										Key: "remote-cert",
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
							Namespace: namespace,
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
					DeferCleanup(func(ctx SpecContext) {
						Expect(k8sClient.Delete(ctx, tlsSecret)).ToNot(HaveOccurred())
					})
				},
				func(cr *vmv1.VTAgent) {
					Eventually(func() error {
						return expectPodCount(ctx, k8sClient, &appsv1.StatefulSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())

					var sts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &sts)).ToNot(HaveOccurred())
					Expect(sts.Spec.VolumeClaimTemplates).To(HaveLen(1))
					Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
					Expect(sts.Spec.Template.Spec.Volumes).To(HaveLen(1))
					Expect(sts.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(2))
				}),
			Entry("with remote target oauth2 and bearer token", "remote-oauth2-bearer",
				&vmv1.VTAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VTAgentSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1.VTAgentRemoteWriteSpec{
							{
								URL: "http://localhost:10428/insert/native",
								BearerTokenSecret: &corev1.SecretKeySelector{
									Key: "token",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "bearer-vtagent",
									},
								},
								SendTimeout:  ptr.To("30s"),
								MaxDiskUsage: ptr.To(vmv1beta1.BytesString(`10GB`)),
							},
							{
								URL: "http://localhost:10425/insert/native",
								OAuth2: &vmv1.OAuth2{
									TokenURL:       "http://oauth2.example.com",
									Scopes:         []string{"scope-1", "scope-2"},
									EndpointParams: map[string]string{"query": "value", "foo": "baz"},
									ClientIDSecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "oauth2-vtagent",
										},
										Key: "client-id",
									},
									ClientSecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "oauth2-vtagent",
										},
										Key: "client-secret",
									},
								},
							},
						},
					},
				},
				func() {

					oauth2Secret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "oauth2-vtagent",
							Namespace: namespace,
						},
						StringData: map[string]string{
							"client-id":     "some-id",
							"client-secret": "some-secret",
						},
					}
					Expect(func() error {
						if err := k8sClient.Create(ctx, oauth2Secret); err != nil &&
							!k8serrors.IsAlreadyExists(err) {
							return err
						}
						return nil
					}()).ToNot(HaveOccurred())
					DeferCleanup(func(ctx SpecContext) {
						Expect(k8sClient.Delete(ctx, oauth2Secret)).ToNot(HaveOccurred())
					})
					bearerSecret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "bearer-vtagent",
							Namespace: namespace,
						},
						StringData: map[string]string{
							"token": "some-token",
						},
					}
					Expect(func() error {
						if err := k8sClient.Create(ctx, bearerSecret); err != nil &&
							!k8serrors.IsAlreadyExists(err) {
							return err
						}
						return nil
					}()).ToNot(HaveOccurred())
					DeferCleanup(func(ctx SpecContext) {
						Expect(k8sClient.Delete(ctx, bearerSecret)).ToNot(HaveOccurred())
					})
				},
				func(cr *vmv1.VTAgent) {
					Eventually(func() error {
						return expectPodCount(ctx, k8sClient, &appsv1.StatefulSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())
					var sts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &sts)).ToNot(HaveOccurred())
					Expect(sts.Spec.Template.Spec.Volumes).To(HaveLen(3))
					Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
					cnt := sts.Spec.Template.Spec.Containers[0]
					Expect(cnt.VolumeMounts).To(HaveLen(3))
					Expect(cnt.Args).To(ContainElements(
						"-remoteWrite.oauth2.clientID=,/etc/vt/remote-write-assets/oauth2-vtagent/client-id",
						"-remoteWrite.oauth2.scopes=,scope-1;scope-2",
						"-remoteWrite.oauth2.clientSecretFile=,/etc/vt/remote-write-assets/oauth2-vtagent/client-secret",
						"-remoteWrite.oauth2.tokenUrl=,http://oauth2.example.com",
						"-remoteWrite.oauth2.endpointParams=,'{\"foo\":\"baz\",\"query\":\"value\"}'",
					))
					Expect(cnt.Args).To(ContainElements("-remoteWrite.bearerTokenFile=/etc/vt/remote-write-assets/bearer-vtagent/token,"))
				}),
		)
	})
})
