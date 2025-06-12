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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("test vmagent Controller", Label("vm", "agent"), func() {
	ctx := context.Background()
	Context("e2e vmagent", func() {
		namespace := "default"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		tlsSecretName := "vmagent-remote-tls-certs"

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
			}, eventualDeletionTimeout, 1).Should(MatchError(errors.IsNotFound, "IsNotFound"))
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
							!errors.IsAlreadyExists(err) {
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
							&rbacv1.ClusterRole{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
					Expect(
						k8sClient.Get(ctx,
							newFormatNss,
							&rbacv1.ClusterRoleBinding{})).To(MatchError(errors.IsNotFound, "IsNotFound"))

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
							&rbacv1.ClusterRole{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
					Expect(
						k8sClient.Get(ctx,
							prevFormatName,
							&rbacv1.ClusterRoleBinding{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
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
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMAgent) { cr.Spec.StatefulMode = false },
					verify: func(cr *vmv1beta1.VMAgent) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &appsv1.StatefulSet{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
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
						}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
						Eventually(func() error {
							return k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})
						}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
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
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMServiceScrape{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
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
						Expect(k8sClient.Get(ctx, nsn, &appsv1.DaemonSet{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
						Expect(k8sClient.Get(ctx, nsn, &appsv1.Deployment{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).To(MatchError(errors.IsNotFound, "IsNotFound"))

					},
				},
			),
		)
	})
})

var (
	tlsCA = `-----BEGIN CERTIFICATE-----
MIIFfTCCA2WgAwIBAgIUBNFQu/Q4hyC6zm6QTBxh63+I/lUwDQYJKoZIhvcNAQEL
BQAwTjELMAkGA1UEBhMCVEUxCzAJBgNVBAgMAlRFMQswCQYDVQQHDAJURTELMAkG
A1UECgwCVEUxCzAJBgNVBAsMAlRFMQswCQYDVQQDDAJURTAeFw0yMDA3MjgwODQ0
MjJaFw0yMTA3MjgwODQ0MjJaME4xCzAJBgNVBAYTAlRFMQswCQYDVQQIDAJURTEL
MAkGA1UEBwwCVEUxCzAJBgNVBAoMAlRFMQswCQYDVQQLDAJURTELMAkGA1UEAwwC
VEUwggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQCpX9JifNTY0lyItiTI
DEDJK1kr4rOKBGIDqfR5GRn8IGwkHicif/FqOreHPpPfwN6nT99QYjp7uZGRtUgL
2T7szu/PcAvOSb7R3QKenB4Xh6f6brlI4qPQdaarGEQbLIqgq4hV7XuYG4pMEb68
G4amTK2WPKaOIFFH72eNxfOe4TTU/J55ItKlGMUK4CwBYQe2bQOFSrvxzDmCqt1d
75HrJTDnWWp8gpPn9yCsTGW4K/4c0nwZPcm2vBcg41ByE4mJxGEdV0XaU7RxLups
eY6fRa10dYFiU9IGZcLbod0neaclQvS0z4EQBR7Xj6MnLrRJ5dpExaksoMLy/1VM
dfV7/tBFNmKf+fi7mKFA/BltEN1XBEhSUgA8FZq/XpS0/7ULWNcQ2AVc8Selosz8
5w6k6R10bsSe2Ttysi2oR2Y+P1JR74G2wz1hxNiEDPEySRoUo1UZSZODZD9smqYc
rR+QEmH+wtDfQR4pXkyZyaG8Y5qXYKNUMaJH8NPvjw8uWttVoGD1IcmF3mZwVwSU
K4aH7BG09qo4/MijseYMcWWXA0vgCBj8sXXgzDwXZyowiwWrpOriHpNUpiiR8NeT
9lFaHiYxuPj5MjZrGWCxXcXgtWaAj9X9B6Kv0jCA8PhYgO0FkCi2cJEOHFJc7FxO
EfjDoFg4wHd4DVp7z9pSBHe4zwIDAQABo1MwUTAdBgNVHQ4EFgQUkGcr4XZ8GC2W
s18CTuvCtM3pwckwHwYDVR0jBBgwFoAUkGcr4XZ8GC2Ws18CTuvCtM3pwckwDwYD
VR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAgEAN+H+244JARmkjYBtZspy
75IAtKH2rYhbXLB57YP8BNYjSST/HGn7kVAhFbEs4YpHbrK2jiXnMjEKBv1ZCqEv
Q+2DpTf4tKz3QBwZ2zph9ET/WfWZI2Magm6q0IEVhdNID595EdeT+RyT5ccyXQ30
+jgLBd4pAWY0xiTlL9udpYxV7MQdG/7Lk/t1sUgKUUQNgX3kuIs9HjCj12o+Dx2y
uYt2TYRk0oWSXR6JBFX3XiLOuQa89olmUDj67AFTQyE0hZcsWvboxYPNT68kPuiA
Gk9XVpbt4fmAs55SfGA31QDkGNqYBBCCxFJIHQ+lleNdR3862wYXCh+Vgat6452Q
VFhoyJp2SDqwqXSgB4zbkTfLF+Wn1uEBfZoFthjC1hD1j9+g7dh+Dbs03RJF6Bvl
K1S9jDNVfP5Mw4+JZnLWqlCjxCMXW96YgnGzi0T82ntP7dBMofj3DjbNeRJwgk7q
0gD55CiufmpTfLT2NUmSUAmG1MTkodXTEX7XRIyiF8eqhL2iefoc6zuh3KRj4oSv
i7TjcCMJ3XEQ31zOPjTd1SkCiBvNS6sh9JC0NFK0i5Ju+53MjNJ734Km4kpXKoaJ
tIP7kt7mbXWdcZQkznBwArupOUWZEMw3gm0J3mLDJ6ifJieHIb/wSsurh2/tMrqB
MY0V6Pd9E788PphPk/eIaI4=
-----END CERTIFICATE-----`
	tlsKey = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDRtQ6WsCmYdwOx
j58F2DmaaVd123IflPRwei9tmuv8kjiN24gT0PBbdL2NX8vPR8szQNUikeCMd5Jy
QenVueLQRG796ujjMz7Srcdl5kiCa+AN82GYZxgD2GmL2wuJvq0cXgzGQHSCv55f
WojNojGAjwr/yZcS7JCE8w3MfyBsRu5s5fqWQjFhF8suSVXSz3eg+PofILrnrN6x
g4uJIpZYEmXXlaL3DV+tIqVsC2tROsDUrEkg2dx1vuNFa7DJSthpLKR3A3bZik7+
1KzSHnIH/rCn+lIjR1ifcZ1RozdCMsaRfDFe8jQmX4hvSvTs66T9q+2jLp4zy/1J
JbeiFUGNAgMBAAECggEAOvQjfclYaDxNFYXCtunqh7ZFmCRxGN/POC+hVbbP0Nlq
fLbSsn9yksNm5m+f5E3Smj4HrQhFkDetO+G70xHG6bXTXh7ECdtGNgQUoljy2Xdq
LYHWVfnljm8wfNi/jaHFGMx32uQT3Q3xf+z7uJN4RyPve6k4h2Fp33ZU0sCKZOWp
qnE9e0vNPg0OZe3cPISiGHInPCjZn8ngkps1QDnO+rt2qKWl1Ph13pXMnMz40+u2
oHNNS9XiYL83GuU4ZcrnEpFoAZA5YvXSO5/r90b0SyAyBYRnHLzGsDsYdHnSwT/X
4T1oCftqUUb1g/x4VujzJGgQJZkwfcrGcADEnOWaAQKBgQDpNSpjwI6eZLRuTcO/
u10QN+8nLWmMxXJa5BRoy/5qCcgf9uvq9GU1x73t6er1pr0mwUB25Q+QGUMq4LAm
D8OumxcPxftvuwZaGW3c8In8pzwCAi01MHG/+2oMHN1WTV2jlzWvQtU75Ykwxs32
8L+7wNqmm38ogWuTS6zsbjOP/QKBgQDmM+m4rONdfhRERPNhs9YFoXLDS08N//Fd
8izi/y7a/Dh315lmRZk6lU7GJSzxQ0WKygd5wz/RjJfYfT0bonjeB4wKeqvV8Ujb
3Xq3GpBoO3WwPFU4DjyX53p/F9cCCgckhNFUGSzOR+8JXG17GoAHu2Iqfpt6RLpk
8wiuVWvE0QKBgB9wRmWqOM/LnbNdEm2Pka01DS2H5rnOiGsOYl36WjLrXKpKfGVx
Sw+j/MvNBBrXvpox5UHiAWYYscBfCAApkeTBDavXsdzPJr0QvonRd5iy5tkSeAu6
mysZdqNpZMFUrrH2GYumA98OQ59qvatzqzVhe1iIj+zi/aCezBIXjSX1AoGBALCz
9Jofi79+QgxNaQz8QDK+RRuHuT0j06Crfq0X+F178dR8GHIaxo3jgj4y1xay7rSk
c6yRpXEynHQ/XiLSSjkUTfjVRQXKWoT6s3HN4D9CNQp8pWWL+BMaSjs4j4AvNmBf
21bUpEILkX78BcXTB6fnvGimGq52ByXqMCWxyDGhAoGALxgtAo9aIDffmmnszy/2
Nu2nxiA+RVYiH80z3vxQI/oewb4FVTAtJ6A1rYGgbAs5icHQ564xnsjAxH6UeGTU
4PSHnceAZ1MxXQirdOYIg9HPhl+0JBs+KixGclTWWWERtcLbv/UtMZzRKNlair05
Berg9dOTyoXzr1BvqKq7PUw=
-----END PRIVATE KEY-----
`
	tlsCert = `-----BEGIN CERTIFICATE-----
MIIEIzCCAgsCFEuh9p7lgotaef4oqeHZhSOVgmKWMA0GCSqGSIb3DQEBCwUAME4x
CzAJBgNVBAYTAlRFMQswCQYDVQQIDAJURTELMAkGA1UEBwwCVEUxCzAJBgNVBAoM
AlRFMQswCQYDVQQLDAJURTELMAkGA1UEAwwCVEUwHhcNMjAwNzI4MDg0NzAwWhcN
MjAwOTI2MDg0NzAwWjBOMQswCQYDVQQGEwJURTELMAkGA1UECAwCVEUxCzAJBgNV
BAcMAlRFMQswCQYDVQQKDAJURTELMAkGA1UECwwCVEUxCzAJBgNVBAMMAlRFMIIB
IjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA0bUOlrApmHcDsY+fBdg5mmlX
ddtyH5T0cHovbZrr/JI4jduIE9DwW3S9jV/Lz0fLM0DVIpHgjHeSckHp1bni0ERu
/ero4zM+0q3HZeZIgmvgDfNhmGcYA9hpi9sLib6tHF4MxkB0gr+eX1qIzaIxgI8K
/8mXEuyQhPMNzH8gbEbubOX6lkIxYRfLLklV0s93oPj6HyC656zesYOLiSKWWBJl
15Wi9w1frSKlbAtrUTrA1KxJINncdb7jRWuwyUrYaSykdwN22YpO/tSs0h5yB/6w
p/pSI0dYn3GdUaM3QjLGkXwxXvI0Jl+Ib0r07Ouk/avtoy6eM8v9SSW3ohVBjQID
AQABMA0GCSqGSIb3DQEBCwUAA4ICAQBpP4Gr2Fw6CIgosP3ZHoDs3N/OtKGUKF9O
LO2/MPPP3O54x1svFeZIGRFtaxrGqFkCs+SM+Ti9gw6KTq7FmLddqiKYy1bcIj2J
Py06m95tAVNtDhMYDtRVjNeEcdJEewprl55KoxU+XXHdJQ+VaTiIpNc8HBSDShb2
AMv9O0zvBCCpiLM/t5QE1d/f+NCZ7MNHRRs/JVq+YhqK6S4nMIbzC8uNJI1rlMxx
xNiSmvW+RybgU3JKBcKb6TujCR79Jt5kT1ZYmGLQd1VhS59/W0O8PGH8j1YXFKhw
yyYVuR6nBfjDMKwiRJNU5GdqKJ1/lKucYJzrP3xWxemnhdERu2DEgYCYydNSCfNo
CdBIh4Y5Mufswj0cYPoylfWy25NuLbqwLmhj9kEq5BMBNLiJwGnIpIdVBd59payr
M7vgLchoksFqutzVhWleAEXg1dJo+GUKT9aep/OWzRSFYqruAILKHgylkftFb2GA
tM4WxCuAsphZoewqBKvTvCdn8fmXFuWEOaZYfT8IvJ4R+7CfUwI6dA5xHRVxO6Yp
DszbHrMGz4tq39kUG1ylOtspMuFhEHo7Qz+bRJFeLYvvV8W414m+zSndBut+thkY
RBKeYvjZEvkpjCK2SQUK3SqipzpJFu5gkr3NcTk6Qd2T3LOAZcGXYLCTxTEW9DIn
8X3nbqwPPg==
-----END CERTIFICATE-----
`
)
