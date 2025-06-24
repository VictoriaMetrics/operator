package e2e

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

const anomalyConfig = `
reader:
  queries:
    test1:
      expr: node_arp_entries
    test2:
      expr: node_arp_entries
    test3:
      expr: node_arp_entries
    test4:
      expr: node_arp_entries
models:
  model1:
    class: 'zscore'
    z_threshold: 2.5
  model2:
    class: 'auto'
    tuned_class_name: 'zscore'
schedulers:
  scheduler1:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: "1m"
    fit_every: "2m"
    fit_window: "1h"
  scheduler2:
    class: "scheduler.periodic.PeriodicScheduler"
    infer_every: "2m"
    fit_every: "4m"
    fit_window: "1h"
`

var (
	anomalyReadyTimeout  = 120 * time.Second
	anomalyDeleteTimeout = 60 * time.Second
	anomalyExpandTimeout = 240 * time.Second
)

//nolint:dupl,lll
var _ = Describe("test vmanomaly Controller", Label("vm", "anomaly", "enterprise"), Ordered, func() {
	ctx := context.Background()
	namespace := "default"
	anomalyDatasourceURL := fmt.Sprintf("http://vmsingle-anomaly.%s.svc:8428", namespace)
	anomalySingle := vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anomaly",
			Namespace: namespace,
		},
	}
	licenseKey := os.Getenv("LICENSE_KEY")
	BeforeAll(func() {
		if licenseKey == "" {
			Skip("ignoring VMAnomaly tests, license was not found")
		}
		Expect(k8sClient.Create(ctx,
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "license",
					Namespace: namespace,
				},
				StringData: map[string]string{
					"key": licenseKey,
				},
			},
		)).To(Succeed())

		Expect(k8sClient.Create(ctx, &anomalySingle)).To(Succeed())
		Eventually(func() error {
			return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMSingle{}, types.NamespacedName{Name: anomalySingle.Name, Namespace: namespace})
		}, eventualDeploymentAppReadyTimeout,
		).Should(Succeed())

	})
	AfterAll(func() {
		Expect(k8sClient.Delete(ctx,
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "license",
					Namespace: namespace,
				},
			},
		)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &anomalySingle)).To(Succeed())
		Eventually(func() error {
			return k8sClient.Get(context.Background(), types.NamespacedName{Name: anomalySingle.Name, Namespace: namespace}, &vmv1beta1.VMSingle{})
		}, eventualDeletionTimeout, 1).Should(MatchError(errors.IsNotFound, "IsNotFound"))

	})
	Context("e2e vmanomaly", func() {
		namespace := "default"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		tlsSecretName := "vmanomaly-remote-tls-certs"
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx,
				&vmv1.VMAnomaly{
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
				}, &vmv1.VMAnomaly{})
			}, anomalyDeleteTimeout, 1).Should(MatchError(errors.IsNotFound, "IsNotFound"))
		})
		DescribeTable("should create vmanomaly",
			func(name string, cr *vmv1.VMAnomaly, setup func(), verify func(*vmv1.VMAnomaly)) {
				cr.Name = name
				namespacedName.Name = name
				if setup != nil {
					setup()
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VMAnomaly{}, namespacedName)
				}, anomalyReadyTimeout,
				).Should(Succeed())

				var created vmv1.VMAnomaly
				Expect(k8sClient.Get(ctx, namespacedName, &created)).To(Succeed())
				verify(&created)
			},
			Entry("with reader", "custom-reader",
				&vmv1.VMAnomaly{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1.VMAnomalySpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						License: &vmv1beta1.License{
							KeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "license",
								},
								Key: "key",
							},
						},
						ConfigRawYaml: anomalyConfig,
						Reader: &vmv1.VMAnomalyReaderSpec{
							DatasourceURL:  anomalyDatasourceURL,
							QueryRangePath: "/api/v1/query_range",
							SamplingPeriod: "10s",
							VMAnomalyHTTPClientSpec: vmv1.VMAnomalyHTTPClientSpec{
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
						Writer: &vmv1.VMAnomalyWriterSpec{
							DatasourceURL: anomalyDatasourceURL,
						},
					},
				}, func() {
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
				func(cr *vmv1.VMAnomaly) {
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

				},
			),
			Entry("with strict security", "strict-security",
				&vmv1.VMAnomaly{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: vmv1.VMAnomalySpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							UseStrictSecurity: ptr.To(true),
						},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount:                        ptr.To[int32](1),
							DisableAutomountServiceAccountToken: true,
						},
						License: &vmv1beta1.License{
							KeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "license",
								},
								Key: "key",
							},
						},
						ConfigRawYaml: anomalyConfig,
						Reader: &vmv1.VMAnomalyReaderSpec{
							DatasourceURL:  anomalyDatasourceURL,
							QueryRangePath: "/api/v1/query_range",
							SamplingPeriod: "10s",
						},
						Writer: &vmv1.VMAnomalyWriterSpec{
							DatasourceURL: anomalyDatasourceURL,
						},
					},
				}, nil, func(cr *vmv1.VMAnomaly) {
					Eventually(func() string {
						return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
					}, eventualDeploymentPodTimeout, 1).Should(BeEmpty())
					var dep appsv1.StatefulSet
					Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: cr.PrefixedName(), Namespace: namespace,
					}, &dep)).To(Succeed())
					// assert security
					Expect(dep.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.SecurityContext.RunAsUser).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
					pc := dep.Spec.Template.Spec.Containers
					Expect(pc[0].SecurityContext).NotTo(BeNil())
					Expect(pc[0].SecurityContext.AllowPrivilegeEscalation).NotTo(BeNil())
					Expect(dep.Spec.Template.Spec.Volumes).To(HaveLen(4))

					// vmanomaly cannot have k8s api access
					vmc := pc[0]
					Expect(vmc.Name).To(Equal("vmanomaly"))
					Expect(vmc.VolumeMounts).To(HaveLen(4))
					Expect(hasVolumeMount(vmc.VolumeMounts, "/var/run/secrets/kubernetes.io/serviceaccount")).NotTo(Succeed())
				},
			),
		)
		type testStep struct {
			setup  func(*vmv1.VMAnomaly)
			modify func(*vmv1.VMAnomaly)
			verify func(*vmv1.VMAnomaly)
		}
		DescribeTable("should update exist vmanomaly",
			func(name string, initCR *vmv1.VMAnomaly, steps ...testStep) {
				// create and wait ready
				initCR.Name = name
				initCR.Namespace = namespace
				namespacedName.Name = name
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VMAnomaly{}, namespacedName)
				}, anomalyReadyTimeout).Should(Succeed())
				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// update and wait ready
					Eventually(func() error {
						var toUpdate vmv1.VMAnomaly
						Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
						step.modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, anomalyExpandTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VMAnomaly{}, namespacedName)
					}, anomalyExpandTimeout).Should(Succeed())
					// verify
					var updated vmv1.VMAnomaly
					Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
					step.verify(&updated)
				}
			},
			Entry("by switching to shard mode", "shard",
				&vmv1.VMAnomaly{
					Spec: vmv1.VMAnomalySpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						License: &vmv1beta1.License{
							KeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "license",
								},
								Key: "key",
							},
						},
						ConfigRawYaml: anomalyConfig,
						Reader: &vmv1.VMAnomalyReaderSpec{
							DatasourceURL:  anomalyDatasourceURL,
							QueryRangePath: "/api/v1/query_range",
							SamplingPeriod: "10s",
						},
						Writer: &vmv1.VMAnomalyWriterSpec{
							DatasourceURL: anomalyDatasourceURL,
						},
					},
				},
				testStep{
					modify: func(cr *vmv1.VMAnomaly) {
						cr.Spec.ReplicaCount = ptr.To[int32](1)
						cr.Spec.ShardCount = ptr.To(2)
					},
					verify: func(cr *vmv1.VMAnomaly) {
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

			Entry("by deleting and restoring PodDisruptionBudget and podScrape", "pdb-mutations-scrape",
				&vmv1.VMAnomaly{
					Spec: vmv1.VMAnomalySpec{
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{UseDefaultResources: ptr.To(false)},
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](2),
						},
						License: &vmv1beta1.License{
							KeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "license",
								},
								Key: "key",
							},
						},
						PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: &intstr.IntOrString{IntVal: 1}},
						ConfigRawYaml:       anomalyConfig,
						Reader: &vmv1.VMAnomalyReaderSpec{
							DatasourceURL:  anomalyDatasourceURL,
							QueryRangePath: "/api/v1/query_range",
							SamplingPeriod: "10s",
						},
						Writer: &vmv1.VMAnomalyWriterSpec{
							DatasourceURL: anomalyDatasourceURL,
						},
					},
				},
				testStep{
					setup: func(cr *vmv1.VMAnomaly) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).To(Succeed())
					},
					modify: func(cr *vmv1.VMAnomaly) {
						cr.Spec.PodDisruptionBudget = nil
						cr.Spec.DisableSelfServiceScrape = ptr.To(true)
					},
					verify: func(cr *vmv1.VMAnomaly) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Eventually(func() error {
							return k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})
						}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
						Eventually(func() error {
							return k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})
						}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
					},
				},
				testStep{
					modify: func(cr *vmv1.VMAnomaly) {
						cr.Spec.PodDisruptionBudget = &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
							MaxUnavailable: &intstr.IntOrString{IntVal: 1},
						}
						cr.Spec.DisableSelfServiceScrape = nil

					},
					verify: func(cr *vmv1.VMAnomaly) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(k8sClient.Get(ctx, nsn, &policyv1.PodDisruptionBudget{})).To(Succeed())
						Expect(k8sClient.Get(ctx, nsn, &vmv1beta1.VMPodScrape{})).To(Succeed())

					},
				},
			),
		)
	})
})
