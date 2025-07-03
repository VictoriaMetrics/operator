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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("test vlagent Controller", Label("vl", "agent"), func() {
	ctx := context.Background()
	Context("e2e vlagent", func() {
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		tlsSecretName := "vlagent-remote-tls-certs"

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1.VLAgent{
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
				}, &vmv1.VLAgent{})
			}, eventualDeletionTimeout, 1).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		})

		DescribeTable("should create vlagent",
			func(name string, cr *vmv1.VLAgent, setup func(), verify func(*vmv1.VLAgent)) {

				cr.Name = name
				namespacedName.Name = name
				if setup != nil {
					setup()
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLAgent{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout,
				).Should(Succeed())

				var created vmv1.VLAgent
				Expect(k8sClient.Get(ctx, namespacedName, &created)).To(Succeed())
				verify(&created)

			},
			Entry("with 1 replica", "replica-1", &vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1.VLAgentSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
						{URL: "http://localhost:8428"},
					},
				},
			}, nil, func(cr *vmv1.VLAgent) {
				Eventually(func() string {
					return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
				}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())

			}),
			Entry("with additional service and insert ports", "insert-ports",
				&vmv1.VLAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1.VLAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
							{URL: "http://localhost:8428"},
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
			Entry("with tls remote target", "remote-tls",
				&vmv1.VLAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1.VLAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
							{URL: "http://localhost:8428"},
							{
								URL: "http://localhost:8425",
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
				},
				func(cr *vmv1.VLAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
					Expect(finalize.SafeDelete(
						ctx,
						k8sClient,
						&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
							Name:      tlsSecretName,
							Namespace: namespacedName.Namespace,
						}},
					)).To(Succeed())

				}),
			Entry("with strict security", "strict-sec",
				&vmv1.VLAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
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
							{URL: "http://localhost:8428"},
						},
					},
				}, nil, func(cr *vmv1.VLAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).To(Succeed())
					// assert security
					Expect(dep.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.SecurityContext.RunAsUser).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
					pc := dep.Spec.Template.Spec.Containers
					Expect(pc[0].SecurityContext).NotTo(BeNil())
					Expect(pc[0].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Volumes).To(HaveLen(4))

					// vlagent must have k8s api access
					Expect(hasVolume(dep.Spec.Template.Spec.Volumes, "kube-api-access")).To(Succeed())
					vmc := pc[0]
					Expect(vmc.Name).To(Equal("vlagent"))
					Expect(vmc.VolumeMounts).To(HaveLen(4))
					Expect(hasVolumeMount(vmc.VolumeMounts, "/var/run/secrets/kubernetes.io/serviceaccount")).To(Succeed())
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
				namespacedName.Name = name
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLAgent{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// update and wait ready
					Eventually(func() error {
						var toUpdate vmv1.VLAgent
						Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
						step.modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLAgent{}, namespacedName)
					}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
					// verify
					var updated vmv1.VLAgent
					Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
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
							{URL: "http://some-vm-single:8428"},
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
			Entry("by changing revisionHistoryLimit to 3", "update-revision",
				&vmv1.VLAgent{
					Spec: vmv1.VLAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount:              ptr.To[int32](1),
							RevisionHistoryLimitCount: ptr.To[int32](11),
						},
						RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
							{URL: "http://some-vm-single:8428"},
						},
					},
				},
				testStep{
					setup: func(cr *vmv1.VLAgent) {
						Expect(getRevisionHistoryLimit(k8sClient, types.NamespacedName{
							Namespace: cr.Namespace,
							Name:      cr.PrefixedName(),
						})).To(Equal(int32(11)))
					},
					modify: func(cr *vmv1.VLAgent) { cr.Spec.RevisionHistoryLimitCount = ptr.To[int32](3) },
					verify: func(cr *vmv1.VLAgent) {
						namespacedNameDeployment := types.NamespacedName{
							Name:      cr.PrefixedName(),
							Namespace: namespace,
						}
						Expect(getRevisionHistoryLimit(k8sClient, namespacedNameDeployment)).To(Equal(int32(3)))
					},
				},
			),

			Entry("by transition into statefulMode and back", "stateful-transition",
				&vmv1.VLAgent{Spec: vmv1.VLAgentSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					Mode: vmv1.DeploymentMode,
					RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
						{URL: "http://some-vm-single:8428"},
					},
				},
				},
				testStep{
					modify: func(cr *vmv1.VLAgent) { cr.Spec.Mode = vmv1.StatefulSetMode },
					verify: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.StatefulSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					},
				},
				testStep{
					modify: func(cr *vmv1.VLAgent) { cr.Spec.Mode = vmv1.DeploymentMode },
					verify: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &appsv1.StatefulSet{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					},
				},
			),
			Entry("by deleting and restoring PodDisruptionBudget and serviceScrape", "pdb-mutations-scrape",
				&vmv1.VLAgent{Spec: vmv1.VLAgentSpec{
					CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{UseDefaultResources: ptr.To(false)},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](2),
					},
					PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: &intstr.IntOrString{IntVal: 1}},
					RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
						{URL: "http://some-vm-single:8428"},
					},
				},
				},
				testStep{
					setup: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).To(Succeed())
					},
					modify: func(cr *vmv1.VLAgent) {
						cr.Spec.PodDisruptionBudget = nil
						cr.Spec.DisableSelfServiceScrape = ptr.To(true)
					},
					verify: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Eventually(func() error {
							return k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})
						}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
						Eventually(func() error {
							return k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})
						}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
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
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).To(Succeed())

					},
				},
			),
			Entry("by transition into daemonSet and back", "daemonset-transition",
				&vmv1.VLAgent{Spec: vmv1.VLAgentSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
						{URL: "http://some-vm-single:8428"},
					},
				},
				},
				testStep{
					modify: func(cr *vmv1.VLAgent) { cr.Spec.Mode = vmv1.DaemonSetMode },
					verify: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.DaemonSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					},
				},
				testStep{
					modify: func(cr *vmv1.VLAgent) {
						cr.Spec.Mode = vmv1.StatefulSetMode
					},
					verify: func(cr *vmv1.VLAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.StatefulSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &appsv1.DaemonSet{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))

					},
				},
			),
		)
	})
})
