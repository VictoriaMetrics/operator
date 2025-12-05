package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("test vtsingle Controller", Label("vt", "single", "vtsingle"), func() {

	Context("e2e vtsingle", func() {
		var ctx context.Context
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		nsn := types.NamespacedName{
			Namespace: namespace,
		}
		BeforeEach(func() {
			ctx = context.Background()
		})
		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1.VTSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: nsn.Namespace,
				},
			})).To(Succeed())
			waitResourceDeleted(ctx, k8sClient, nsn, &vmv1.VTSingle{})
		})
		Context("crud", func() {
			DescribeTable("should create",
				func(name string, cr *vmv1.VTSingle, verify func(*vmv1.VTSingle)) {
					cr.Name = name
					nsn.Name = name
					Expect(k8sClient.Create(ctx, cr)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
					}, eventualDeploymentAppReadyTimeout,
					).Should(Succeed())

					var created vmv1.VTSingle
					Expect(k8sClient.Get(ctx, nsn, &created)).To(Succeed())
					verify(&created)
				},
				Entry("with strict security", "strict-security",
					&vmv1.VTSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1.VTSingleSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity: ptr.To(true),
							},
							RetentionPeriod: "1",
							Storage: &corev1.PersistentVolumeClaimSpec{
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
					func(cr *vmv1.VTSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).To(Succeed())
						Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(1))
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext).NotTo(BeNil())
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).NotTo(BeNil())
						Expect(*createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).To(BeTrue())

					}),
				Entry("with data emptyDir", "emptydir",
					&vmv1.VTSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1.VTSingleSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity: ptr.To(false),
							},
							RetentionPeriod: "1",
						},
					},
					func(cr *vmv1.VTSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).To(Succeed())
						ts := createdDeploy.Spec.Template.Spec
						Expect(ts.Containers).To(HaveLen(1))
						Expect(ts.Volumes).To(HaveLen(1))
						Expect(ts.Containers[0].VolumeMounts).To(HaveLen(1))
					}),
				Entry("with external volume", "externalvolume",
					&vmv1.VTSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1.VTSingleSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
								Volumes: []corev1.Volume{
									{
										Name: "data",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
									{
										Name: "unused",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "unused",
										MountPath: "/opt/unused/mountpoint",
									},
								},
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity: ptr.To(false),
							},
							RetentionPeriod: "1",
							StorageDataPath: "/custom-path/internal/dir",
						},
					},
					func(cr *vmv1.VTSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).To(Succeed())
						ts := createdDeploy.Spec.Template.Spec
						Expect(ts.Containers).To(HaveLen(1))
						Expect(ts.Volumes).To(HaveLen(2))
						Expect(ts.Containers[0].VolumeMounts).To(HaveLen(2))
						Expect(ts.Containers[0].VolumeMounts[0].Name).To(Equal("data"))
						Expect(ts.Containers[0].VolumeMounts[1].Name).To(Equal("unused"))

					}),
			)

			baseVTSingle := &vmv1.VTSingle{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
				},
				Spec: vmv1.VTSingleSpec{
					RetentionPeriod: "10",
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
				},
			}
			type testStep struct {
				setup  func(*vmv1.VTSingle)
				modify func(*vmv1.VTSingle)
				verify func(*vmv1.VTSingle)
			}

			DescribeTable("should update exist",
				func(name string, initCR *vmv1.VTSingle, steps ...testStep) {
					initCR.Name = name
					initCR.Namespace = namespace
					nsn.Name = name
					// setup test
					Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
					}, eventualDeploymentAppReadyTimeout).Should(Succeed())

					for _, step := range steps {
						if step.setup != nil {
							step.setup(initCR)
						}
						var toUpdate vmv1.VTSingle
						Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
						step.modify(&toUpdate)
						Expect(k8sClient.Update(ctx, &toUpdate)).To(Succeed())
						Eventually(func() error {
							return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
						}, eventualDeploymentAppReadyTimeout).Should(Succeed())

						var updated vmv1.VTSingle
						Expect(k8sClient.Get(ctx, nsn, &updated)).To(Succeed())

						// verify results
						step.verify(&updated)
					}
				},
				Entry("add and remove annotations", "manage-annotations",
					baseVTSingle.DeepCopy(),
					testStep{
						modify: func(cr *vmv1.VTSingle) {
							cr.Spec.ManagedMetadata = &vmv1beta1.ManagedObjectsMetadata{
								Annotations: map[string]string{
									"added-annotation": "some-value",
								},
							}
						},
						verify: func(cr *vmv1.VTSingle) {
							nss := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}

							expectedAnnotations := map[string]string{"added-annotation": "some-value"}
							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.Deployment{}, &corev1.ServiceAccount{}, &corev1.Service{}}, expectedAnnotations)
							var createdDeploy appsv1.Deployment
							Expect(k8sClient.Get(ctx, nss, &createdDeploy)).
								To(Succeed())
						},
					},
					testStep{
						modify: func(cr *vmv1.VTSingle) {
							delete(cr.Spec.ManagedMetadata.Annotations, "added-annotation")
						},
						verify: func(cr *vmv1.VTSingle) {
							nss := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
							expectedAnnotations := map[string]string{"added-annotation": ""}

							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.Deployment{}, &corev1.ServiceAccount{}, &corev1.Service{}}, expectedAnnotations)

						},
					},
				),
			)
		},
		)
	})
})
