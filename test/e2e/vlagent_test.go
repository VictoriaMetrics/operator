package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

//nolint:dupl,lll
var _ = Describe("test vlagent Controller", Label("vl", "agent", "vlagent"), func() {
	ctx := context.Background()
	Context("e2e vlagent", func() {
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		nsn := types.NamespacedName{
			Namespace: namespace,
		}
		tlsSecretName := "vlagent-remote-tls-certs"

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: nsn.Namespace,
				},
			})).To(Succeed())
			waitResourceDeleted(ctx, k8sClient, nsn, &vmv1.VLAgent{})
		})

		DescribeTable("should create vlagent",
			func(name string, cr *vmv1.VLAgent, setup func(), verify func(*vmv1.VLAgent)) {

				cr.Name = name
				nsn.Name = name
				if setup != nil {
					setup()
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLAgent{}, nsn)
				}, eventualDeploymentAppReadyTimeout,
				).Should(Succeed())

				var created vmv1.VLAgent
				Expect(k8sClient.Get(ctx, nsn, &created)).To(Succeed())
				verify(&created)

			},
			Entry("with 1 replica, rw headers and rw-settings", "replica-1-rw", &vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1.VLAgentSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
						{URL: "http://localhost:9428/internal/insert"},
						{URL: "http://localhost:9481/internal/insert", Headers: []string{"Authorization: Bearer Insecure", "Basic: Insecure Token"}},
					},
					RemoteWriteSettings: &vmv1.VLAgentRemoteWriteSettings{
						MaxBlockSize:  ptr.To(vmv1beta1.BytesString(`15MB`)),
						FlushInterval: ptr.To("2s"),
						TmpDataPath:   ptr.To("/tmp/custom-path"),
					},
				},
			}, nil, func(cr *vmv1.VLAgent) {
				Eventually(func() string {
					return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
				}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
				var sts appsv1.StatefulSet
				Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &sts)).To(Succeed())
				Expect(sts.Spec.VolumeClaimTemplates).To(BeEmpty())
				Expect(sts.Spec.Template.Spec.Volumes).To(BeEmpty())
				Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
				cnt := sts.Spec.Template.Spec.Containers[0]
				Expect(cnt.VolumeMounts).To(BeEmpty())
				Expect(cnt.Args).To(ContainElements("-remoteWrite.tmpDataPath=/tmp/custom-path", "-remoteWrite.maxBlockSize=15MB"))
			}),
			Entry("with additional service and insert ports", "insert-ports",
				&vmv1.VLAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VLAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
							{URL: "http://localhost:9428"},
						},
						ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
							EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
								Name: "vlagent-extra-service",
							},
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeNodePort,
							},
						},
					},
				}, nil, func(cr *vmv1.VLAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())

				}),
			Entry("with persistent storage and tls remote target", "remote-tls",
				&vmv1.VLAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VLAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
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
						RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
							{URL: "http://localhost:9428"},
							{
								URL: "http://localhost:9425",
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
					}()).To(Succeed())
					DeferCleanup(func(ctx SpecContext) {
						Expect(k8sClient.Delete(ctx, tlsSecret)).To(Succeed())
					})
				},
				func(cr *vmv1.VLAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())

					var sts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &sts)).To(Succeed())
					Expect(sts.Spec.VolumeClaimTemplates).To(HaveLen(1))
					Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
					Expect(sts.Spec.Template.Spec.Volumes).To(HaveLen(1))
					Expect(sts.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(2))
				}),
			Entry("with remote target oauth2 and bearer token", "remote-oauth2-bearer",
				&vmv1.VLAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VLAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
							{
								URL: "http://localhost:9428",
								BearerTokenSecret: &corev1.SecretKeySelector{
									Key: "token",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "bearer-vlagent",
									},
								},
								SendTimeout:  ptr.To("30s"),
								MaxDiskUsage: ptr.To(vmv1beta1.BytesString(`10GB`)),
							},
							{
								URL: "http://localhost:9425",
								OAuth2: &vmv1.OAuth2{
									TokenURL:       "http://oauth2.example.com",
									Scopes:         []string{"scope-1", "scope-2"},
									EndpointParams: map[string]string{"query": "value", "foo": "baz"},
									ClientIDSecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "oauth2-vlagent",
										},
										Key: "client-id",
									},
									ClientSecret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "oauth2-vlagent",
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
							Name:      "oauth2-vlagent",
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
					}()).To(Succeed())
					DeferCleanup(func(ctx SpecContext) {
						Expect(k8sClient.Delete(ctx, oauth2Secret)).To(Succeed())
					})
					bearerSecret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "bearer-vlagent",
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
					}()).To(Succeed())
					DeferCleanup(func(ctx SpecContext) {
						Expect(k8sClient.Delete(ctx, bearerSecret)).To(Succeed())
					})
				},
				func(cr *vmv1.VLAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
					var sts appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &sts)).To(Succeed())
					Expect(sts.Spec.Template.Spec.Volumes).To(HaveLen(3))
					Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
					cnt := sts.Spec.Template.Spec.Containers[0]
					Expect(cnt.VolumeMounts).To(HaveLen(3))
					Expect(cnt.Args).To(ContainElements(
						"-remoteWrite.oauth2.clientID=,/etc/vl/remote-write-assets/oauth2-vlagent/client-id",
						"-remoteWrite.oauth2.scopes=,scope-1;scope-2",
						"-remoteWrite.oauth2.clientSecretFile=,/etc/vl/remote-write-assets/oauth2-vlagent/client-secret",
						"-remoteWrite.oauth2.tokenUrl=,http://oauth2.example.com",
						"-remoteWrite.oauth2.endpointParams=,'{\"foo\":\"baz\",\"query\":\"value\"}'",
					))
					Expect(cnt.Args).To(ContainElements("-remoteWrite.bearerTokenFile=/etc/vl/remote-write-assets/bearer-vlagent/token,"))
				}),

			Entry("with strict security", "strict-sec",
				&vmv1.VLAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VLAgentSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							UseStrictSecurity: ptr.To(true),
						},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount:                        ptr.To[int32](1),
							DisableAutomountServiceAccountToken: true,
						},
						RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
							{URL: "http://localhost:9428"},
						},
					},
				}, nil, func(cr *vmv1.VLAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
					var dep appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).To(Succeed())
					// assert security
					Expect(dep.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.SecurityContext.RunAsUser).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
					pc := dep.Spec.Template.Spec.Containers
					Expect(pc[0].SecurityContext).NotTo(BeNil())
					Expect(pc[0].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Volumes).To(HaveLen(1))

					// vlagent must not have k8s api access
					Expect(hasVolume(dep.Spec.Template.Spec.Volumes, "kube-api-access")).NotTo(Succeed())
					vmc := pc[0]
					Expect(vmc.Name).To(Equal("vlagent"))
					Expect(vmc.VolumeMounts).To(HaveLen(1))
					Expect(hasVolumeMount(vmc.VolumeMounts, "/var/run/secrets/kubernetes.io/serviceaccount")).NotTo(Succeed())
				},
			),
		)
		type testStep struct {
			setup  func(*vmv1.VLAgent)
			modify func(*vmv1.VLAgent)
			verify func(*vmv1.VLAgent)
		}
		DescribeTable("should update exist vlagent",
			func(name string, initCR *vmv1.VLAgent, steps ...testStep) {
				// create and wait ready
				initCR.Name = name
				initCR.Namespace = namespace
				nsn.Name = name
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLAgent{}, nsn)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// update and wait ready
					Eventually(func() error {
						var toUpdate vmv1.VLAgent
						Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
						step.modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLAgent{}, nsn)
					}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
					// verify
					var updated vmv1.VLAgent
					Expect(k8sClient.Get(ctx, nsn, &updated)).To(Succeed())
					step.verify(&updated)
				}
			},
			Entry("by scaling replicas to to 3", "update-replicas-3",
				&vmv1.VLAgent{
					Spec: vmv1.VLAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
							{URL: "http://some-vl-single:9428"},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1.VLAgent) { cr.Spec.ReplicaCount = ptr.To[int32](3) },
					verify: func(cr *vmv1.VLAgent) {
						Eventually(func() string {
							return expectPodCount(k8sClient, 3, namespace, cr.SelectorLabels())
						}, eventualDeploymentAppReadyTimeout, 1).Should(BeEmpty())
					},
				},
			),
			Entry("by deleting and restoring PodDisruptionBudget and podScrape", "pdb-mutations-scrape",
				&vmv1.VLAgent{Spec: vmv1.VLAgentSpec{
					CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{UseDefaultResources: ptr.To(false)},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](2),
					},
					PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: &intstr.IntOrString{IntVal: 1}},
					RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
						{URL: "http://some-vl-single:9428"},
					},
				},
				},
				testStep{
					setup: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).To(Succeed())
					},
					modify: func(cr *vmv1.VLAgent) {
						cr.Spec.PodDisruptionBudget = nil
						cr.Spec.DisableSelfServiceScrape = ptr.To(true)
					},
					verify: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						waitResourceDeleted(ctx, k8sClient, nsn, &policyv1.PodDisruptionBudget{})
						waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMPodScrape{})
					},
				},
				testStep{
					modify: func(cr *vmv1.VLAgent) {
						cr.Spec.PodDisruptionBudget = &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: &intstr.IntOrString{IntVal: 1}}
						cr.Spec.DisableSelfServiceScrape = nil

					},
					verify: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).To(Succeed())

					},
				},
			),
		)
	})
})
