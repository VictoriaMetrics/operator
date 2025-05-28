package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta "github.com/VictoriaMetrics/operator/api/operator/v1beta1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("test vlsingle Controller", func() {

	Context("e2e vlcluster", func() {
		var ctx context.Context
		namespace := "default"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		BeforeEach(func() {
			ctx = context.Background()
		})
		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1.VLCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
			})).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, namespacedName, &vmv1.VLCluster{})
			}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
		})
		baseVLCluster := &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
			},
			Spec: vmv1.VLClusterSpec{
				VLInsert: &vmv1.VLInsert{},
				VLSelect: &vmv1.VLSelect{},
				VLStorage: &vmv1.VLStorage{
					RetentionPeriod: "1",
					CommonApplicationDeploymentParams: vmv1beta.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
				},
			},
		}
		type testStep struct {
			setup  func(*vmv1.VLCluster)
			modify func(*vmv1.VLCluster)
			verify func(*vmv1.VLCluster)
		}

		DescribeTable("should peform update steps",
			func(name string, initCR *vmv1.VLCluster, steps ...testStep) {
				initCR.Name = name
				initCR.Namespace = namespace
				namespacedName.Name = name
				// setup test
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLCluster{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout).Should(Succeed())

				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// perform update
					Eventually(func() error {
						var toUpdate vmv1.VLCluster
						Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
						step.modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLCluster{}, namespacedName)
					}, eventualDeploymentAppReadyTimeout).Should(Succeed())

					var updated vmv1.VLCluster
					Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())

					// verify results
					step.verify(&updated)
				}
			},
			Entry("add and remove annotations with strict security", "manage-annotations",
				baseVLCluster.DeepCopy(),
				testStep{
					modify: func(cr *vmv1.VLCluster) {
						cr.Spec.ManagedMetadata = &vmv1beta.ManagedObjectsMetadata{
							Annotations: map[string]string{
								"added-annotation": "some-value",
							},
						}
						cr.Spec.UseStrictSecurity = ptr.To(true)
					},
					verify: func(cr *vmv1.VLCluster) {
						nsss := []types.NamespacedName{
							{Namespace: namespace, Name: cr.GetVLStorageName()},
						}
						expectedAnnotations := map[string]string{"added-annotation": "some-value"}
						for _, nss := range nsss {
							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.StatefulSet{}, &corev1.Service{}}, expectedAnnotations)
						}
						for _, nss := range nsss {
							sts := &appsv1.StatefulSet{}
							Expect(k8sClient.Get(ctx, nss, sts)).To(Succeed())
							assertStrictSecurity(sts.Spec.Template.Spec)
						}
						nsss = []types.NamespacedName{
							{Namespace: namespace, Name: cr.GetVLInsertName()},
							{Namespace: namespace, Name: cr.GetVLSelectName()},
						}
						for _, nss := range nsss {
							sts := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, nss, sts)).To(Succeed())
							assertStrictSecurity(sts.Spec.Template.Spec)
						}
					},
				},
				testStep{
					modify: func(cr *vmv1.VLCluster) {
						delete(cr.Spec.ManagedMetadata.Annotations, "added-annotation")
					},
					verify: func(cr *vmv1.VLCluster) {
						nsss := []types.NamespacedName{
							{Namespace: namespace, Name: cr.GetVLStorageName()},
						}
						expectedAnnotations := map[string]string{"added-annotation": ""}
						for _, nss := range nsss {
							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.StatefulSet{}, &corev1.Service{}}, expectedAnnotations)
						}
						nsss = []types.NamespacedName{
							{Namespace: namespace, Name: cr.GetVLInsertName()},
							{Namespace: namespace, Name: cr.GetVLSelectName()},
						}
						for _, nss := range nsss {
							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.Deployment{}, &corev1.Service{}}, expectedAnnotations)
						}

					},
				},
			),
			Entry("vlcluster with requests lb", "requests-lb",
				baseVLCluster.DeepCopy(),
				testStep{
					modify: func(cr *vmv1.VLCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = true
					},
					verify: func(cr *vmv1.VLCluster) {

						var dep appsv1.Deployment
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVMAuthLBName(), Namespace: namespace}, &dep)).To(Succeed())

						var svc corev1.Service
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVLSelectName(), Namespace: namespace}, &svc)).To(Succeed())
						Expect(svc.Spec.Selector).To(Equal(cr.VMAuthLBSelectorLabels()))

						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVLInsertName(), Namespace: namespace}, &svc)).To(Succeed())
						Expect(svc.Spec.Selector).To(Equal(cr.VMAuthLBSelectorLabels()))

						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:9481/insert/jsonline?_stream_fields=stream&_time_field=date&_msg_field=log.message", cr.GetVLInsertName(), namespace),
							payload: `{\"log\": {\"level\": \"info\", \"message\": \"hello world\" }, \"date\": \"0\", \"stream\": \"stream1\" }
{ \"log\": { \"level\": \"info\", \"message\": \"hello world\" }, \"date\": \"0\", \"stream\": \"stream2\" }
              `,
							expectedCode: 200,
							method:       "POST",
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL:       fmt.Sprintf("http://%s.%s.svc:9471/select/logsql/query?query=*", cr.GetVLSelectName(), namespace),
							payload:      ``,
							expectedCode: 200,
						})
					},
				},
				testStep{
					modify: func(cr *vmv1.VLCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = false
					},
					verify: func(cr *vmv1.VLCluster) {
						var dep appsv1.Deployment
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVMAuthLBName(), Namespace: namespace}, &dep)).To(MatchError(errors.IsNotFound, "isNotFound"))

						var svc corev1.Service
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVLSelectName(), Namespace: namespace}, &svc)).To(Succeed())
						Expect(svc.Spec.Selector).To(Equal(cr.VLSelectSelectorLabels()))

						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVLInsertName(), Namespace: namespace}, &svc)).To(Succeed())
						Expect(svc.Spec.Selector).To(Equal(cr.VLInsertSelectorLabels()))

						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:9481/insert/jsonline?_stream_fields=stream&_time_field=date&_msg_field=log.message", cr.GetVLInsertName(), namespace),
							payload: `{\"log\": {\"level\": \"info\", \"message\": \"hello world\" }, \"date\": \"0\", \"stream\": \"stream1\" }
{ \"log\": { \"level\": \"info\", \"message\": \"hello world\" }, \"date\": \"0\", \"stream\": \"stream2\" }
              `,
							expectedCode: 200,
							method:       "POST",
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL:       fmt.Sprintf("http://%s.%s.svc:9471/select/logsql/query?query=*", cr.GetVLSelectName(), namespace),
							payload:      ``,
							expectedCode: 200,
						})
					},
				},
			),

			Entry("with syslog tls", "syslog-tls",
				baseVLCluster.DeepCopy(),
				testStep{
					modify: func(cr *vmv1.VLCluster) {
						tlsSecret := corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "syslog-tls",
								Namespace: namespace,
							},
							StringData: map[string]string{
								"TLS_CERT": tlsCert,
								"TLS_KEY":  tlsKey,
							},
						}
						Expect(k8sClient.Create(ctx, &tlsSecret)).To(Succeed())
						DeferCleanup(func(ctx SpecContext) {
							Expect(k8sClient.Delete(ctx, &tlsSecret)).To(Succeed())
						})
						cr.Spec.VLInsert.SyslogSpec = &vmv1.SyslogServerSpec{
							TCPListeners: []*vmv1.SyslogTCPListener{
								{
									ListenPort:   9500,
									StreamFields: vmv1.FieldsListString(`["stream", "log.message"]`),
									TLSConfig: &vmv1.TLSServerConfig{
										CertSecret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecret.Name,
											},
											Key: "TLS_CERT",
										},
										KeySecret: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: tlsSecret.Name,
											},
											Key: "TLS_KEY",
										},
									},
								},
							},
							UDPListeners: []*vmv1.SyslogUDPListener{
								{
									ListenPort:   9500,
									StreamFields: vmv1.FieldsListString(`["stream", "log.message"]`),
								},
							},
						}
					},
					verify: func(cr *vmv1.VLCluster) {
						var svc corev1.Service
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVLInsertName()}, &svc)).To(Succeed())
						Expect(svc.Spec.Ports).To(HaveLen(3))

						var dep appsv1.Deployment
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVLInsertName()}, &dep)).To(Succeed())
						Expect(dep.Spec.Template.Spec.Volumes).To(HaveLen(1))
						Expect(dep.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
					},
				},
			),
		)
	},
	)

})
