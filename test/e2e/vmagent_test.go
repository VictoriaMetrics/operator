package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("vmagent", Label("vm", "agent"), func() {
	tlsSecretName := "vmagent-remote-tls-certs"
	Context("crud", func() {
		var ctx context.Context
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		BeforeEach(func() {
			ctx = context.Background()
		})
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1beta1.VMAgent{
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
				}, &vmv1beta1.VMAgent{})
			}, eventualDeletionTimeout, 1).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		})

		DescribeTable("should create vmagent",
			func(name string, cr *vmv1beta1.VMAgent, setup func(), verify func(*vmv1beta1.VMAgent)) {

				cr.Name = name
				namespacedName.Name = name
				if setup != nil {
					setup()
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout,
				).Should(Succeed())

				var created vmv1beta1.VMAgent
				Expect(k8sClient.Get(ctx, namespacedName, &created)).To(Succeed())
				verify(&created)

			},
			Entry("with rw stream aggr and relabeling", "stream-aggr", &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1beta1.VMAgentSpec{
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{

							URL: "http://localhost:8429/api/v1/write",
							InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
								{
									SourceLabels: []string{"job"},
									Action:       "drop",
								},
							},
						},
						{
							URL: "http://localhost:8428/api/v1/write",
							StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
								KeepInput: true,
								Rules: []vmv1beta1.StreamAggrRule{
									{
										By:       []string{"verb", "le"},
										Interval: "1m",
										Match:    vmv1beta1.StringOrArray{"apiserver_request_duration_seconds_bucket"},
										Outputs:  []string{"total"},
									},
								},
							},
						},
					},
				},
			},
				nil,
				func(cr *vmv1beta1.VMAgent) {
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).To(Succeed())
					Expect(dep.Spec.Template.Spec.Volumes).To(HaveLen(6))
					Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(2))
					vmagentCnt := dep.Spec.Template.Spec.Containers[1]
					Expect(vmagentCnt.Name).To(Equal("vmagent"))
					Expect(vmagentCnt.VolumeMounts).To(HaveLen(6))
					Expect(vmagentCnt.Args).To(ContainElements("-remoteWrite.streamAggr.config=,/etc/vm/stream-aggr/RWS_1-CM-STREAM-AGGR-CONF", "-remoteWrite.urlRelabelConfig=/etc/vm/relabeling/url_relabeling-0.yaml,"))
				},
			),
			Entry("with 1 replica", "replica-1", &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1beta1.VMAgentSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{URL: "http://localhost:8428"},
					},
				},
			}, nil, func(cr *vmv1beta1.VMAgent) {
				Eventually(func() string {
					return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
				}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())

			}),
			Entry("with vm config-reloader and statefulMode", "vm-reloader-stateful",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
							UseVMConfigReloader: ptr.To(true),
						},
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							UseDefaultResources: ptr.To(false),
						},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount:                        ptr.To[int32](1),
							DisableAutomountServiceAccountToken: true,
						},
						StatefulMode: true,
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://localhost:8428"},
						},
					},
				}, nil, func(cr *vmv1beta1.VMAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
				},
			),
			Entry("with additional service and insert ports", "insert-ports",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://localhost:8428"},
						},
						InsertPorts: &vmv1beta1.InsertPorts{
							GraphitePort: "8111",
							OpenTSDBPort: "8112",
						},
						ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
							EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
								Name: "vmagent-extra-service",
							},
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeNodePort,
							},
						},
					},
				}, nil, func(cr *vmv1beta1.VMAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())

				}),
			Entry("with tls remote target", "remote-tls",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
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
				func(cr *vmv1beta1.VMAgent) {
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
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							UseStrictSecurity: ptr.To(true),
						},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount:                        ptr.To[int32](1),
							DisableAutomountServiceAccountToken: true,
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://localhost:8428"},
						},
						SelectAllByDefault: true,
					},
				}, nil, func(cr *vmv1beta1.VMAgent) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
					var dep appsv1.Deployment
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).To(Succeed())
					// assert security
					Expect(dep.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.SecurityContext.RunAsUser).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(2))
					Expect(dep.Spec.Template.Spec.InitContainers).To(HaveLen(1))
					pc := dep.Spec.Template.Spec.Containers
					pic := dep.Spec.Template.Spec.InitContainers
					Expect(pc[0].SecurityContext).NotTo(BeNil())
					Expect(pc[1].SecurityContext).NotTo(BeNil())
					Expect(pic[0].SecurityContext).NotTo(BeNil())
					Expect(pc[0].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(pc[1].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(pic[0].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Volumes).To(HaveLen(5))

					// assert k8s api access

					// config-reloader must not have k8s api access
					vmagentPod := mustGetFirstPod(k8sClient, namespace, cr.SelectorLabels())
					Expect(hasVolumeMount(vmagentPod.Spec.Containers[0].VolumeMounts, "/var/run/secrets/kubernetes.io/serviceaccount")).NotTo(Succeed())

					// vmagent must have k8s api access
					Expect(hasVolume(dep.Spec.Template.Spec.Volumes, "kube-api-access")).To(Succeed())
					cric := pic[0]
					Expect(cric.VolumeMounts).To(HaveLen(2))
					crc := pc[0]
					Expect(crc.Name).To(Equal("config-reloader"))
					Expect(crc.VolumeMounts).To(HaveLen(2))
					vmc := pc[1]
					Expect(vmc.Name).To(Equal("vmagent"))
					Expect(vmc.VolumeMounts).To(HaveLen(5))
					Expect(hasVolumeMount(vmc.VolumeMounts, "/var/run/secrets/kubernetes.io/serviceaccount")).To(Succeed())
				}),
			Entry("by migrating RBAC access", "rbac-migrate",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{UseDefaultResources: ptr.To(false)},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://some-vm-single:8428"},
						},
					},
				},
				func() {
					cr := vmv1beta1.VMAgent{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rbac-migrate",
							Namespace: namespace,
						},
					}
					roleMeta := metav1.ObjectMeta{
						Name:       "monitoring:vmagent-cluster-access-" + cr.Name,
						Namespace:  namespace,
						Finalizers: []string{vmv1beta1.FinalizerName},
					}
					crole := &rbacv1.ClusterRole{ObjectMeta: roleMeta, Rules: []rbacv1.PolicyRule{
						{APIGroups: []string{""}, Verbs: []string{"GET"}, Resources: []string{"pods"}},
					}}
					croleb := &rbacv1.ClusterRoleBinding{
						ObjectMeta: roleMeta,
						Subjects: []rbacv1.Subject{
							{
								Kind:      rbacv1.ServiceAccountKind,
								Name:      cr.GetServiceAccountName(),
								Namespace: cr.GetNamespace(),
							},
						},
						RoleRef: rbacv1.RoleRef{
							APIGroup: rbacv1.GroupName,
							Name:     cr.GetClusterRoleName(),
							Kind:     "ClusterRole",
						},
					}
					Expect(k8sClient.Create(ctx, crole)).To(Succeed())
					Expect(k8sClient.Create(ctx, croleb)).To(Succeed())
					// check that access not exist with new version naming
					newFormatNss := types.NamespacedName{
						Name:      "monitoring:" + namespace + ":vmagent-" + cr.Name,
						Namespace: namespace,
					}
					Expect(
						k8sClient.Get(ctx,
							newFormatNss,
							&rbacv1.ClusterRole{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					Expect(
						k8sClient.Get(ctx,
							newFormatNss,
							&rbacv1.ClusterRoleBinding{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))

				},
				func(cr *vmv1beta1.VMAgent) {
					prevFormatName := types.NamespacedName{
						Name:      "monitoring:vmagent-cluster-access-" + cr.Name,
						Namespace: namespace,
					}

					newFormatName := types.NamespacedName{
						Name:      "monitoring:" + namespace + ":vmagent-" + cr.Name,
						Namespace: namespace,
					}
					Expect(
						k8sClient.Get(ctx,
							prevFormatName,
							&rbacv1.ClusterRole{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					Expect(
						k8sClient.Get(ctx,
							prevFormatName,
							&rbacv1.ClusterRoleBinding{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					Expect(
						k8sClient.Get(ctx,
							newFormatName,
							&rbacv1.ClusterRole{})).To(Succeed())
					Expect(
						k8sClient.Get(ctx,
							newFormatName,
							&rbacv1.ClusterRoleBinding{})).To(Succeed())

				},
			),
		)
		type testStep struct {
			setup  func(*vmv1beta1.VMAgent)
			modify func(*vmv1beta1.VMAgent)
			verify func(*vmv1beta1.VMAgent)
		}
		DescribeTable("should update exist vmagent",
			func(name string, initCR *vmv1beta1.VMAgent, steps ...testStep) {
				// create and wait ready
				initCR.Name = name
				initCR.Namespace = namespace
				namespacedName.Name = name
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// update and wait ready
					Eventually(func() error {
						var toUpdate vmv1beta1.VMAgent
						Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
						step.modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, namespacedName)
					}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
					// verify
					var updated vmv1beta1.VMAgent
					Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
					step.verify(&updated)
				}
			},
			Entry("by scaling replicas to to 3", "update-replicas-3",
				&vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://some-vm-single:8428"},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.ReplicaCount = ptr.To[int32](3) },
					verify: func(cr *vmv1beta1.VMAgent) {
						Eventually(func() string {
							return expectPodCount(k8sClient, 3, namespace, cr.SelectorLabels())
						}, eventualDeploymentAppReadyTimeout, 1).Should(BeEmpty())
					},
				},
			),
			Entry("by changing revisionHistoryLimit to 3", "update-revision",
				&vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount:              ptr.To[int32](1),
							RevisionHistoryLimitCount: ptr.To[int32](11),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://some-vm-single:8428"},
						},
					},
				},
				testStep{
					setup: func(cr *vmv1beta1.VMAgent) {
						Expect(getRevisionHistoryLimit(k8sClient, types.NamespacedName{
							Namespace: cr.Namespace,
							Name:      cr.PrefixedName(),
						})).To(Equal(int32(11)))
					},
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.RevisionHistoryLimitCount = ptr.To[int32](3) },
					verify: func(cr *vmv1beta1.VMAgent) {
						namespacedNameDeployment := types.NamespacedName{
							Name:      cr.PrefixedName(),
							Namespace: namespace,
						}
						Expect(getRevisionHistoryLimit(k8sClient, namespacedNameDeployment)).To(Equal(int32(3)))
					},
				},
			),
			Entry("by switching to statefulMode with shard", "stateful-shard",
				&vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://some-vm-single:8428"},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) {
						cr.Spec.ReplicaCount = ptr.To[int32](1)
						cr.Spec.ShardCount = ptr.To(2)
						cr.Spec.StatefulMode = true
						cr.Spec.IngestOnlyMode = true
					},
					verify: func(cr *vmv1beta1.VMAgent) {
						var createdSts appsv1.StatefulSet
						Expect(k8sClient.Get(ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      fmt.Sprintf("%s-%d", cr.PrefixedName(), 0),
						}, &createdSts)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      fmt.Sprintf("%s-%d", cr.PrefixedName(), 1),
						}, &createdSts)).To(Succeed())

					},
				},
			),

			Entry("by transition into statefulMode and back", "stateful-transition",
				&vmv1beta1.VMAgent{Spec: vmv1beta1.VMAgentSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{URL: "http://some-vm-single:8428"},
					},
				},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.StatefulMode = true },
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.StatefulSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.StatefulMode = false },
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &appsv1.StatefulSet{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					},
				},
			),
			Entry("by deleting and restoring PodDisruptionBudget and serviceScrape", "pdb-mutations-scrape",
				&vmv1beta1.VMAgent{Spec: vmv1beta1.VMAgentSpec{
					CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{UseDefaultResources: ptr.To(false)},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](2),
					},
					SelectAllByDefault:  true,
					PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: &intstr.IntOrString{IntVal: 1}},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{URL: "http://some-vm-single:8428"},
					},
				},
				},
				testStep{
					setup: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).To(Succeed())
					},
					modify: func(cr *vmv1beta1.VMAgent) {
						cr.Spec.PodDisruptionBudget = nil
						cr.Spec.DisableSelfServiceScrape = ptr.To(true)
					},
					verify: func(cr *vmv1beta1.VMAgent) {
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
					modify: func(cr *vmv1beta1.VMAgent) {
						cr.Spec.PodDisruptionBudget = &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: &intstr.IntOrString{IntVal: 1}}
						cr.Spec.DisableSelfServiceScrape = nil

					},
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).To(Succeed())

					},
				},
			),
			Entry("by transition into daemonSet and back", "daemonset-transition",
				&vmv1beta1.VMAgent{Spec: vmv1beta1.VMAgentSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{URL: "http://some-vm-single:8428"},
					},
				},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.DaemonSetMode = true },
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.DaemonSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).To(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) {
						cr.Spec.StatefulMode = true
						cr.Spec.DaemonSetMode = false
					},
					verify: func(cr *vmv1beta1.VMAgent) {
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
