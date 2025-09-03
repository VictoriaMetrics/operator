package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("test vtcluster Controller", Label("vt", "cluster", "vtcluster"), func() {

	Context("e2e vtcluster", func() {
		var ctx context.Context
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		BeforeEach(func() {
			ctx = context.Background()
		})
		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1.VTCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
			})).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, namespacedName, &vmv1.VTCluster{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		})
		baseVTCluster := &vmv1.VTCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
			},
			Spec: vmv1.VTClusterSpec{
				Insert: &vmv1.VTInsert{},
				Select: &vmv1.VTSelect{},
				Storage: &vmv1.VTStorage{
					RetentionPeriod: "1",
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
				},
			},
		}
		type testStep struct {
			setup  func(*vmv1.VTCluster)
			modify func(*vmv1.VTCluster)
			verify func(*vmv1.VTCluster)
		}

		DescribeTable("should perform update steps",
			func(name string, initCR *vmv1.VTCluster, steps ...testStep) {
				initCR.Name = name
				initCR.Namespace = namespace
				namespacedName.Name = name
				// setup test
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTCluster{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout).Should(Succeed())

				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// perform update
					Eventually(func() error {
						var toUpdate vmv1.VTCluster
						Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
						step.modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTCluster{}, namespacedName)
					}, eventualDeploymentAppReadyTimeout).Should(Succeed())

					var updated vmv1.VTCluster
					Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())

					// verify results
					step.verify(&updated)
				}
			},
			Entry("add and remove annotations with strict security", "manage-annotations",
				baseVTCluster.DeepCopy(),
				testStep{
					modify: func(cr *vmv1.VTCluster) {
						cr.Spec.ManagedMetadata = &vmv1beta1.ManagedObjectsMetadata{
							Annotations: map[string]string{
								"added-annotation": "some-value",
							},
						}
						cr.Spec.UseStrictSecurity = ptr.To(true)
					},
					verify: func(cr *vmv1.VTCluster) {
						nsss := []types.NamespacedName{
							{Namespace: namespace, Name: cr.GetVTStorageName()},
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
							{Namespace: namespace, Name: cr.GetVTInsertName()},
							{Namespace: namespace, Name: cr.GetVTSelectName()},
						}
						for _, nss := range nsss {
							sts := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, nss, sts)).To(Succeed())
							assertStrictSecurity(sts.Spec.Template.Spec)
						}
					},
				},
				testStep{
					modify: func(cr *vmv1.VTCluster) {
						delete(cr.Spec.ManagedMetadata.Annotations, "added-annotation")
					},
					verify: func(cr *vmv1.VTCluster) {
						nsss := []types.NamespacedName{
							{Namespace: namespace, Name: cr.GetVTStorageName()},
						}
						expectedAnnotations := map[string]string{"added-annotation": ""}
						for _, nss := range nsss {
							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.StatefulSet{}, &corev1.Service{}}, expectedAnnotations)
						}
						nsss = []types.NamespacedName{
							{Namespace: namespace, Name: cr.GetVTInsertName()},
							{Namespace: namespace, Name: cr.GetVTSelectName()},
						}
						for _, nss := range nsss {
							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.Deployment{}, &corev1.Service{}}, expectedAnnotations)
						}

					},
				},
			),
			Entry("vtcluster with requests lb", "requests-lb",
				baseVTCluster.DeepCopy(),
				testStep{
					modify: func(cr *vmv1.VTCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = true
					},
					verify: func(cr *vmv1.VTCluster) {

						var dep appsv1.Deployment
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVMAuthLBName(), Namespace: namespace}, &dep)).To(Succeed())

						var svc corev1.Service
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVTSelectName(), Namespace: namespace}, &svc)).To(Succeed())
						Expect(svc.Spec.Selector).To(Equal(cr.VMAuthLBSelectorLabels()))

						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVTInsertName(), Namespace: namespace}, &svc)).To(Succeed())
						Expect(svc.Spec.Selector).To(Equal(cr.VMAuthLBSelectorLabels()))

						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL:       fmt.Sprintf("http://%s.%s.svc:10481/insert/ready", cr.GetVTInsertName(), namespace),
							expectedCode: 200,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL:       fmt.Sprintf("http://%s.%s.svc:10471/select/logsql/query?query=*", cr.GetVTSelectName(), namespace),
							payload:      ``,
							expectedCode: 200,
						})
					},
				},
				testStep{
					modify: func(cr *vmv1.VTCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = false
					},
					verify: func(cr *vmv1.VTCluster) {
						var dep appsv1.Deployment
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVMAuthLBName(), Namespace: namespace}, &dep)).To(MatchError(k8serrors.IsNotFound, "isNotFound"))

						var svc corev1.Service
						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVTSelectName(), Namespace: namespace}, &svc)).To(Succeed())
						Expect(svc.Spec.Selector).To(Equal(cr.VTSelectSelectorLabels()))

						Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetVTInsertName(), Namespace: namespace}, &svc)).To(Succeed())
						Expect(svc.Spec.Selector).To(Equal(cr.VTInsertSelectorLabels()))

						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL:       fmt.Sprintf("http://%s.%s.svc:10481/insert/ready", cr.GetVTInsertName(), namespace),
							expectedCode: 200,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL:       fmt.Sprintf("http://%s.%s.svc:10471/select/logsql/query?query=*", cr.GetVTSelectName(), namespace),
							payload:      ``,
							expectedCode: 200,
						})
					},
				},
			),

			Entry("by upscaling and downscaling components", "scale",
				baseVTCluster.DeepCopy(),
				testStep{
					modify: func(cr *vmv1.VTCluster) {
						By("upscaling vtinsert, removing vtselect", func() {
							cr.Spec.Select = nil
							cr.Spec.Insert.ReplicaCount = ptr.To(int32(3))
							cr.Spec.Storage.ReplicaCount = ptr.To(int32(1))
						})
					},
					verify: func(cr *vmv1.VTCluster) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVTStorageName()}
						sts := &appsv1.StatefulSet{}
						Expect(k8sClient.Get(ctx, nsn, sts)).To(Succeed())
						Expect(*sts.Spec.Replicas).To(Equal(int32(1)))
						nsn = types.NamespacedName{Namespace: namespace, Name: cr.GetVTInsertName()}
						dep := &appsv1.Deployment{}
						Expect(k8sClient.Get(ctx, nsn, dep)).To(Succeed())
						Expect(*dep.Spec.Replicas).To(Equal(int32(3)))

						// vtselect must be removed
						nsn = types.NamespacedName{Namespace: namespace, Name: cr.GetVTSelectName()}
						Eventually(func() error {
							return k8sClient.Get(ctx, nsn, dep)
						}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					},
				},
				testStep{
					modify: func(cr *vmv1.VTCluster) {
						By("upscaling vtselect, removing vtinsert", func() {
							cr.Spec.Select = &vmv1.VTSelect{
								CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To(int32(2)),
								},
							}
							cr.Spec.Insert = nil
							cr.Spec.Storage.ReplicaCount = ptr.To(int32(2))
						})
					},
					verify: func(cr *vmv1.VTCluster) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVTStorageName()}
						sts := &appsv1.StatefulSet{}
						Expect(k8sClient.Get(ctx, nsn, sts)).To(Succeed())
						Expect(*sts.Spec.Replicas).To(Equal(int32(2)))
						nsn = types.NamespacedName{Namespace: namespace, Name: cr.GetVTSelectName()}
						dep := &appsv1.Deployment{}
						Expect(k8sClient.Get(ctx, nsn, dep)).To(Succeed())
						Expect(*dep.Spec.Replicas).To(Equal(int32(2)))
						// vtselect must be removed
						nsn = types.NamespacedName{Namespace: namespace, Name: cr.GetVTInsertName()}
						Eventually(func() error {
							return k8sClient.Get(ctx, nsn, dep)
						}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
					},
				},
				testStep{
					modify: func(cr *vmv1.VTCluster) {
						By("downscaling all components to 0 replicas", func() {
							cr.Spec.Select = &vmv1.VTSelect{
								CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To(int32(0)),
								},
							}
							cr.Spec.Insert = &vmv1.VTInsert{
								CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To(int32(0)),
								},
							}
							cr.Spec.Storage.ReplicaCount = ptr.To(int32(0))
						})
					},
					verify: func(cr *vmv1.VTCluster) {
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVTStorageName()}
						sts := &appsv1.StatefulSet{}
						Expect(k8sClient.Get(ctx, nsn, sts)).To(Succeed())
						Expect(*sts.Spec.Replicas).To(Equal(int32(0)))
						dep := &appsv1.Deployment{}
						nsn = types.NamespacedName{Namespace: namespace, Name: cr.GetVTInsertName()}
						Expect(k8sClient.Get(ctx, nsn, dep)).To(Succeed())
						Expect(*dep.Spec.Replicas).To(Equal(int32(0)))
						nsn = types.NamespacedName{Namespace: namespace, Name: cr.GetVTSelectName()}
						Expect(k8sClient.Get(ctx, nsn, dep)).To(Succeed())
						Expect(*dep.Spec.Replicas).To(Equal(int32(0)))
					},
				},
			),
		)
	},
	)

})
