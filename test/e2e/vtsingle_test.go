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
			})).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, k8sClient, nsn, &vmv1.VTSingle{})
		})
		Context("crud", func() {
			DescribeTable("should create",
				func(name string, cr *vmv1.VTSingle, verify func(*vmv1.VTSingle)) {
					cr.Name = name
					nsn.Name = name
					Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
					}, eventualDeploymentAppReadyTimeout,
					).ShouldNot(HaveOccurred())

					var created vmv1.VTSingle
					Expect(k8sClient.Get(ctx, nsn, &created)).ToNot(HaveOccurred())
					verify(&created)
				},
				Entry("with strict security", "strict-security",
					&vmv1.VTSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1.VTSingleSpec{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount:      ptr.To[int32](1),
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
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
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
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount:      ptr.To[int32](1),
								UseStrictSecurity: ptr.To(false),
							},
							RetentionPeriod: "1",
						},
					},
					func(cr *vmv1.VTSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
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
							CommonAppsParams: vmv1beta1.CommonAppsParams{
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
								UseStrictSecurity: ptr.To(false),
							},
							RetentionPeriod: "1",
							StorageDataPath: "/custom-path/internal/dir",
						},
					},
					func(cr *vmv1.VTSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
						ts := createdDeploy.Spec.Template.Spec
						Expect(ts.Containers).To(HaveLen(1))
						Expect(ts.Volumes).To(HaveLen(2))
						Expect(ts.Containers[0].VolumeMounts).To(HaveLen(2))
						Expect(ts.Containers[0].VolumeMounts[0].Name).To(Equal("data"))
						Expect(ts.Containers[0].VolumeMounts[1].Name).To(Equal("unused"))

					}),
				Entry("with UseProxyProtocol", "proxy-protocol",
					&vmv1.VTSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1.VTSingleSpec{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](1),
								ExtraArgs: map[string]string{
									"httpListenAddr.useProxyProtocol": "true",
								},
							},
							RetentionPeriod: "1",
						},
					},
					func(cr *vmv1.VTSingle) {},
				),
			)

			baseVTSingle := &vmv1.VTSingle{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
				},
				Spec: vmv1.VTSingleSpec{
					RetentionPeriod: "10",
					CommonAppsParams: vmv1beta1.CommonAppsParams{
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
					Expect(k8sClient.Create(ctx, initCR)).ToNot(HaveOccurred())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
					}, eventualDeploymentAppReadyTimeout).ShouldNot(HaveOccurred())

					for _, step := range steps {
						if step.setup != nil {
							step.setup(initCR)
						}
						var toUpdate vmv1.VTSingle
						Expect(k8sClient.Get(ctx, nsn, &toUpdate)).ToNot(HaveOccurred())
						step.modify(&toUpdate)
						Expect(k8sClient.Update(ctx, &toUpdate)).ToNot(HaveOccurred())
						Eventually(func() error {
							return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
						}, eventualDeploymentAppReadyTimeout).ShouldNot(HaveOccurred())

						var updated vmv1.VTSingle
						Expect(k8sClient.Get(ctx, nsn, &updated)).ToNot(HaveOccurred())

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
								ToNot(HaveOccurred())
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

		Context("status transitions", func() {
			JustBeforeEach(func() {
				ctx = context.Background()
			})
			It("should reach operational after creation", func() {
				nsn.Name = "vtsingle-status-created"
				cr := &vmv1.VTSingle{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VTSingleSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RetentionPeriod: "1",
					},
				}
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
				By("waiting for operational status after creation")
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
				}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			})

			It("should transition operational→expanding→operational on spec update", func() {
				nsn.Name = "vtsingle-status-update"
				cr := &vmv1.VTSingle{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VTSingleSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RetentionPeriod: "1",
					},
				}
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
				By("waiting for operational status before update")
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
				}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

				By("updating the spec to trigger reconcile")
				Eventually(func() error {
					if err := k8sClient.Get(ctx, nsn, cr); err != nil {
						return err
					}
					cr.Spec.RetentionPeriod = "2"
					return k8sClient.Update(ctx, cr)
				}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

				By("waiting for expanding status")
				Eventually(func() error {
					return expectObjectStatusExpanding(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
				}, eventualExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

				By("waiting for operational status after update")
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
				}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			})

			It("should transition operational→paused when paused", func() {
				nsn.Name = "vtsingle-status-pause"
				cr := &vmv1.VTSingle{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VTSingleSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RetentionPeriod: "1",
					},
				}
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
				By("waiting for operational status before pause")
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
				}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

				By("pausing the VTSingle")
				Eventually(func() error {
					if err := k8sClient.Get(ctx, nsn, cr); err != nil {
						return err
					}
					cr.Spec.Paused = true
					return k8sClient.Update(ctx, cr)
				}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

				By("waiting for paused status")
				Eventually(func() error {
					return expectObjectStatusPaused(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
				}, eventualExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			})

			It("should transition paused→operational when unpaused", func() {
				nsn.Name = "vtsingle-status-unpause"
				cr := &vmv1.VTSingle{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1.VTSingleSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
							Paused:       true,
						},
						RetentionPeriod: "1",
					},
				}
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
				By("waiting for paused status after creation")
				Eventually(func() error {
					return expectObjectStatusPaused(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
				}, eventualExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

				By("unpausing the VTSingle")
				Eventually(func() error {
					if err := k8sClient.Get(ctx, nsn, cr); err != nil {
						return err
					}
					cr.Spec.Paused = false
					return k8sClient.Update(ctx, cr)
				}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

				By("waiting for operational status after unpause")
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VTSingle{}, nsn)
				}, eventualDeploymentAppReadyTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			})
		})
	})
})
