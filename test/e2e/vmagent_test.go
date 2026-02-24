package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
var _ = Describe("test vmagent Controller", Label("vm", "agent", "vmagent"), func() {
	ctx := context.Background()
	Context("e2e vmagent", func() {
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		nsn := types.NamespacedName{
			Namespace: namespace,
		}
		tlsSecretName := "vmagent-remote-tls-certs"

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: nsn.Namespace,
				},
			},
			)).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMAgent{})
		})

		DescribeTable("should create vmagent",
			func(name string, cr *vmv1beta1.VMAgent, setup func(), verify func(*vmv1beta1.VMAgent)) {

				cr.Name = name
				nsn.Name = name
				if setup != nil {
					setup()
				}
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, nsn)
				}, eventualDeploymentAppReadyTimeout,
				).ShouldNot(HaveOccurred())

				var created vmv1beta1.VMAgent
				Expect(k8sClient.Get(ctx, nsn, &created)).ToNot(HaveOccurred())
				verify(&created)

			},
			Entry("with rw stream aggr and relabeling", "stream-aggr", &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
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
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).ToNot(HaveOccurred())
					Expect(dep.Spec.Template.Spec.Volumes).To(HaveLen(6))
					Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(2))
					vmagentCnt := dep.Spec.Template.Spec.Containers[0]
					Expect(vmagentCnt.Name).To(Equal("vmagent"))
					Expect(vmagentCnt.VolumeMounts).To(HaveLen(6))
					Expect(vmagentCnt.Args).To(ContainElements("-remoteWrite.streamAggr.config=,/etc/vm/stream-aggr/RWS_1-CM-STREAM-AGGR-CONF", "-remoteWrite.urlRelabelConfig=/etc/vm/relabeling/url_relabeling-0.yaml,"))
				},
			),
			Entry("with 1 replica", "replica-1", &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
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
				Eventually(func() error {
					return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Labels:    cr.SelectorLabels(),
						},
					}, 1)
				}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())

			}),
			Entry("with statefulMode", "vm-stateful",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1beta1.VMAgentSpec{
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
					Eventually(func() error {
						return expectPodCount(ctx, k8sClient, &appsv1.StatefulSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())
				},
			),
			Entry("with additional service and insert ports", "insert-ports",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
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
					Eventually(func() error {
						return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())

				}),
			Entry("with tls remote target", "remote-tls",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
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
					}()).ToNot(HaveOccurred())
				},
				func(cr *vmv1beta1.VMAgent) {
					Eventually(func() error {
						return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: namespace,
								Labels:    cr.SelectorLabels(),
							},
						}, 1)
					}, eventualDeploymentPodTimeout, 1).ShouldNot(HaveOccurred())
					Expect(finalize.SafeDelete(
						ctx,
						k8sClient,
						&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
							Name:      tlsSecretName,
							Namespace: nsn.Namespace,
						}},
					)).ToNot(HaveOccurred())

				}),
			Entry("with strict security", "strict-sec",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
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
						CommonScrapeParams: vmv1beta1.CommonScrapeParams{
							SelectAllByDefault: true,
						},
					},
				}, nil, func(cr *vmv1beta1.VMAgent) {
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

					// config-reloader must have k8s api access
					vmagentPod := mustGetFirstPod(k8sClient, namespace, cr.SelectorLabels())
					Expect(hasVolumeMount(vmagentPod.Spec.Containers[0].VolumeMounts, "/var/run/secrets/kubernetes.io/serviceaccount")).ToNot(HaveOccurred())

					// vmagent must have k8s api access
					Expect(hasVolume(dep.Spec.Template.Spec.Volumes, "kube-api-access")).ToNot(HaveOccurred())
					cric := pic[0]
					Expect(cric.VolumeMounts).To(HaveLen(2))
					crc := pc[1]
					Expect(crc.Name).To(Equal("config-reloader"))
					Expect(crc.VolumeMounts).To(HaveLen(2))
					vmc := pc[0]
					Expect(vmc.Name).To(Equal("vmagent"))
					Expect(vmc.VolumeMounts).To(HaveLen(5))
					Expect(hasVolumeMount(vmc.VolumeMounts, "/var/run/secrets/kubernetes.io/serviceaccount")).ToNot(HaveOccurred())
				}),
			Entry("by migrating RBAC access", "rbac-migrate",
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
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
					Expect(k8sClient.Create(ctx, crole)).ToNot(HaveOccurred())
					Expect(k8sClient.Create(ctx, croleb)).ToNot(HaveOccurred())
					// check that access not exist with new version naming
					newFormatNss := types.NamespacedName{
						Name:      "monitoring:" + namespace + ":vmagent-" + cr.Name,
						Namespace: namespace,
					}
					waitResourceDeleted(ctx, k8sClient, newFormatNss, &rbacv1.ClusterRole{})
					waitResourceDeleted(ctx, k8sClient, newFormatNss, &rbacv1.ClusterRoleBinding{})
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
					waitResourceDeleted(ctx, k8sClient, prevFormatName, &rbacv1.ClusterRole{})
					waitResourceDeleted(ctx, k8sClient, prevFormatName, &rbacv1.ClusterRoleBinding{})
					Expect(
						k8sClient.Get(ctx,
							newFormatName,
							&rbacv1.ClusterRole{})).ToNot(HaveOccurred())
					Expect(
						k8sClient.Get(ctx,
							newFormatName,
							&rbacv1.ClusterRoleBinding{})).ToNot(HaveOccurred())

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
				nsn.Name = name
				Expect(k8sClient.Create(ctx, initCR)).ToNot(HaveOccurred())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, nsn)
				}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())
				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// update and wait ready
					var toUpdate vmv1beta1.VMAgent
					Expect(k8sClient.Get(ctx, nsn, &toUpdate)).ToNot(HaveOccurred())
					step.modify(&toUpdate)
					Expect(k8sClient.Update(ctx, &toUpdate)).ToNot(HaveOccurred())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, nsn)
					}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())
					// verify
					var updated vmv1beta1.VMAgent
					Expect(k8sClient.Get(ctx, nsn, &updated)).ToNot(HaveOccurred())
					step.verify(&updated)
				}
			},
			Entry("by scaling replicas to 2", "update-replicas-2",
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
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.ReplicaCount = ptr.To[int32](2) },
					verify: func(cr *vmv1beta1.VMAgent) {
						Eventually(func() error {
							return expectPodCount(ctx, k8sClient, &appsv1.ReplicaSet{
								ObjectMeta: metav1.ObjectMeta{
									Namespace: namespace,
									Labels:    cr.SelectorLabels(),
								},
							}, 2)
						}, eventualDeploymentAppReadyTimeout, 1).ShouldNot(HaveOccurred())
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
						Expect(getRevisionHistoryLimit(ctx, k8sClient, types.NamespacedName{
							Namespace: cr.Namespace,
							Name:      cr.PrefixedName(),
						})).To(Equal(int32(11)))
					},
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.RevisionHistoryLimitCount = ptr.To[int32](3) },
					verify: func(cr *vmv1beta1.VMAgent) {
						nsnDeployment := types.NamespacedName{
							Name:      cr.PrefixedName(),
							Namespace: namespace,
						}
						Expect(getRevisionHistoryLimit(ctx, k8sClient, nsnDeployment)).To(Equal(int32(3)))
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
						cr.Spec.IngestOnlyMode = ptr.To(true)
					},
					verify: func(cr *vmv1beta1.VMAgent) {
						var createdSts appsv1.StatefulSet
						Expect(k8sClient.Get(ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      fmt.Sprintf("%s-%d", cr.PrefixedName(), 0),
						}, &createdSts)).ToNot(HaveOccurred())
						Expect(k8sClient.Get(ctx, types.NamespacedName{
							Namespace: namespace,
							Name:      fmt.Sprintf("%s-%d", cr.PrefixedName(), 1),
						}, &createdSts)).ToNot(HaveOccurred())

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
						Expect(k8sClient.Get(ctx, nsn, &appsv1.StatefulSet{})).ToNot(HaveOccurred())
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.Deployment{})
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.StatefulMode = false },
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).ToNot(HaveOccurred())
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.StatefulSet{})
					},
				},
			),
			Entry("by deleting and restoring PodDisruptionBudget and serviceScrape", "pdb-mutations-scrape",
				&vmv1beta1.VMAgent{Spec: vmv1beta1.VMAgentSpec{
					CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{UseDefaultResources: ptr.To(false)},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](2),
					},
					CommonScrapeParams: vmv1beta1.CommonScrapeParams{
						SelectAllByDefault: true,
					},
					PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: &intstr.IntOrString{IntVal: 1}},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{URL: "http://some-vm-single:8428"},
					},
				},
				},
				testStep{
					setup: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).ToNot(HaveOccurred())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).ToNot(HaveOccurred())
					},
					modify: func(cr *vmv1beta1.VMAgent) {
						cr.Spec.PodDisruptionBudget = nil
						cr.Spec.DisableSelfServiceScrape = ptr.To(true)
					},
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						waitResourceDeleted(ctx, k8sClient, nsn, &policyv1.PodDisruptionBudget{})
						waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMServiceScrape{})
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) {
						cr.Spec.PodDisruptionBudget = &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: &intstr.IntOrString{IntVal: 1}}
						cr.Spec.DisableSelfServiceScrape = nil

					},
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).ToNot(HaveOccurred())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).ToNot(HaveOccurred())

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
						Expect(k8sClient.Get(ctx, nsn, &appsv1.DaemonSet{})).ToNot(HaveOccurred())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).ToNot(HaveOccurred())
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.Deployment{})
						waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMServiceScrape{})
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) {
						cr.Spec.StatefulMode = true
						cr.Spec.DaemonSetMode = false
					},
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.StatefulSet{})).ToNot(HaveOccurred())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).ToNot(HaveOccurred())
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.DaemonSet{})
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.Deployment{})
						waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMPodScrape{})
					},
				},
			),
		)

		It("should skip reconciliation when VMAgent is paused", func() {
			ctx := context.Background()
			nsn.Name = "vmagent-paused"
			By("creating a VMAgent")
			initialReplicas := int32(1)
			cr := &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1beta1.VMAgentSpec{
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{URL: "http://localhost:8428/api/v1/write"},
					},
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: &initialReplicas,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			deploymentName := types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, nsn)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			By("pausing the VMAgent")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, nsn, cr); err != nil {
					return err
				}
				cr.Spec.Paused = true
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			By("attempting to scale the VMAgent while paused")
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

			By("unpausing the VMAgent")
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
