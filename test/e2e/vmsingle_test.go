package e2e

import (
	"context"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

//nolint:dupl,lll
var _ = Describe("test vmsingle Controller", Label("vm", "single"), func() {

	licenseKey := os.Getenv("LICENSE_KEY")
	Context("e2e vmsingle", func() {
		var ctx context.Context
		namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
		nsn := types.NamespacedName{
			Namespace: namespace,
		}
		BeforeEach(func() {
			ctx = context.Background()
			if licenseKey != "" {
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
				)).ToNot(HaveOccurred())
			}
		})
		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1beta1.VMSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nsn.Name,
					Namespace: nsn.Namespace,
				},
			})).ToNot(HaveOccurred())
			waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMSingle{})
			if licenseKey != "" {
				Expect(k8sClient.Delete(ctx,
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "license",
							Namespace: namespace,
						},
					},
				)).ToNot(HaveOccurred())
			}
		})
		Context("crud", func() {
			DescribeTable("should create vmsingle",
				func(name string, isEnterprise bool, cr *vmv1beta1.VMSingle, verify func(*vmv1beta1.VMSingle)) {
					if isEnterprise {
						if licenseKey == "" {
							Skip("ignoring VMSingle test, license was not found")
						}
						cfg := config.MustGetBaseConfig()
						cr.Spec.Image.Tag = cfg.MetricsVersion + "-enterprise"
						cr.Spec.License = &vmv1beta1.License{
							KeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "license",
								},
								Key: "key",
							},
						}
					}
					cr.Name = name
					nsn.Name = name
					Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMSingle{}, nsn)
					}, eventualDeploymentAppReadyTimeout,
					).ShouldNot(HaveOccurred())

					var created vmv1beta1.VMSingle
					Expect(k8sClient.Get(ctx, nsn, &created)).ToNot(HaveOccurred())
					verify(&created)
				},
				Entry("with built-in pvc and insert ports", "create-with-pvc-ports", false,
					&vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMSingleSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							RetentionPeriod:      "1",
							RemovePvcAfterDelete: true,
							Storage: &corev1.PersistentVolumeClaimSpec{
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("1Gi"),
									},
								},
							},
							InsertPorts: &vmv1beta1.InsertPorts{
								OpenTSDBPort:     "8081",
								OpenTSDBHTTPPort: "8082",
								GraphitePort:     "8083",
								InfluxPort:       "8084",
							},
						},
					},
					func(cr *vmv1beta1.VMSingle) {
						var createdSvc corev1.Service
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(
							k8sClient.Get(ctx, createdChildObjects, &createdSvc)).
							ToNot(HaveOccurred())
						Expect(createdSvc.Spec.Ports).To(HaveLen(9))
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
						Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(1))
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(8))
					}),
				Entry("with empty resources and vmbackup ", "create-wo-resource-w-backup", true,
					&vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMSingleSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
								Volumes: []corev1.Volume{
									{Name: "backup", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
								},
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},
							VMBackup: &vmv1beta1.VMBackup{
								Destination: "fs:///opt/backup-dir",
								VolumeMounts: []corev1.VolumeMount{
									{Name: "backup", MountPath: "/opt/backup-dir"},
								},
							},
							RetentionPeriod:      "1",
							RemovePvcAfterDelete: true,
							Storage: &corev1.PersistentVolumeClaimSpec{
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
					func(cr *vmv1beta1.VMSingle) {
						var createdSvc corev1.Service
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						Expect(
							k8sClient.Get(ctx, createdChildObjects, &createdSvc)).
							ToNot(HaveOccurred())
						Expect(createdSvc.Spec.Ports).To(HaveLen(3))
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
						Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(2))
						Expect(createdDeploy.Spec.Template.Spec.Containers[1].VolumeMounts).To(HaveLen(3))
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].Resources).To(Equal(corev1.ResourceRequirements{}))
						Expect(createdDeploy.Spec.Template.Spec.Containers[1].Resources).To(Equal(corev1.ResourceRequirements{}))
						var vss vmv1beta1.VMServiceScrape
						Expect(k8sClient.Get(ctx, createdChildObjects, &vss)).ToNot(HaveOccurred())
						Expect(vss.Spec.Endpoints).To(HaveLen(2))
					}),
				Entry("with strict security", "strict-security", false,
					&vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMSingleSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity: ptr.To(true),
							},
							RetentionPeriod:      "1",
							RemovePvcAfterDelete: true,
							Storage: &corev1.PersistentVolumeClaimSpec{
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
					func(cr *vmv1beta1.VMSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
						Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(1))
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext).NotTo(BeNil())
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).NotTo(BeNil())
						Expect(*createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).To(BeTrue())

					}),
				Entry("with storage", "storage", false,
					&vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMSingleSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity: ptr.To(false),
							},
							RetentionPeriod:      "1",
							RemovePvcAfterDelete: true,
							StorageDataPath:      "/tmp/",
							Storage: &corev1.PersistentVolumeClaimSpec{
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceStorage: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
					func(cr *vmv1beta1.VMSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
						ts := createdDeploy.Spec.Template.Spec
						Expect(ts.Containers).To(HaveLen(1))
						Expect(ts.Volumes).To(HaveLen(1))
						Expect(ts.Containers[0].VolumeMounts).To(HaveLen(1))
					}),
				Entry("with empty dir", "emptydir", false,
					&vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMSingleSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity: ptr.To(false),
							},
							RetentionPeriod:      "1",
							RemovePvcAfterDelete: true,
							StorageDataPath:      "/tmp/",
						},
					},
					func(cr *vmv1beta1.VMSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
						ts := createdDeploy.Spec.Template.Spec
						Expect(ts.Containers).To(HaveLen(1))
						Expect(ts.Volumes).To(HaveLen(1))
						Expect(ts.Containers[0].VolumeMounts).To(HaveLen(1))
					}),
				Entry("with external volume", "externalvolume", true,
					&vmv1beta1.VMSingle{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMSingleSpec{
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
										Name: "backup",
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
							RetentionPeriod:      "1",
							RemovePvcAfterDelete: true,
							StorageDataPath:      "/custom-path/internal/dir",
							VMBackup: &vmv1beta1.VMBackup{
								Destination:  "fs:///opt/backup",
								VolumeMounts: []corev1.VolumeMount{{Name: "backup", MountPath: "/opt/backup"}},
							},
						},
					},
					func(cr *vmv1beta1.VMSingle) {
						createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).ToNot(HaveOccurred())
						ts := createdDeploy.Spec.Template.Spec
						Expect(ts.Containers).To(HaveLen(2))
						Expect(ts.Volumes).To(HaveLen(4))
						Expect(ts.Containers[0].VolumeMounts).To(HaveLen(3))
						Expect(ts.Containers[0].VolumeMounts[0].Name).To(Equal("data"))
						Expect(ts.Containers[0].VolumeMounts[1].Name).To(Equal("unused"))
						Expect(ts.Containers[0].VolumeMounts[2].Name).To(Equal("license"))
						Expect(ts.Containers[1].VolumeMounts).To(HaveLen(4))
						Expect(ts.Containers[1].VolumeMounts[0].Name).To(Equal("data"))
						Expect(ts.Containers[1].VolumeMounts[1].Name).To(Equal("unused"))
						Expect(ts.Containers[1].VolumeMounts[2].Name).To(Equal("backup"))
						Expect(ts.Containers[1].VolumeMounts[3].Name).To(Equal("license"))
					}),
			)

			baseSingle := &vmv1beta1.VMSingle{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMSingleSpec{
					RemovePvcAfterDelete: true,
					RetentionPeriod:      "10",
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
				},
			}
			type testStep struct {
				setup  func(*vmv1beta1.VMSingle)
				modify func(*vmv1beta1.VMSingle)
				verify func(*vmv1beta1.VMSingle)
			}

			DescribeTable("should update exist vmsingle",
				func(name string, isEnterprise bool, initCR *vmv1beta1.VMSingle, steps ...testStep) {
					if isEnterprise {
						if licenseKey == "" {
							Skip("ignoring VMSingle test, license was not found")
						}
						cfg := config.MustGetBaseConfig()
						initCR.Spec.Image.Tag = cfg.MetricsVersion + "-enterprise"
						initCR.Spec.License = &vmv1beta1.License{
							KeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "license",
								},
								Key: "key",
							},
						}
					}
					initCR.Name = name
					initCR.Namespace = namespace
					nsn.Name = name
					// setup test
					Expect(k8sClient.Create(ctx, initCR)).ToNot(HaveOccurred())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMSingle{}, nsn)
					}, eventualDeploymentAppReadyTimeout).ShouldNot(HaveOccurred())

					for _, step := range steps {
						if step.setup != nil {
							step.setup(initCR)
						}
						// perform update
						var toUpdate vmv1beta1.VMSingle
						Expect(k8sClient.Get(ctx, nsn, &toUpdate)).ToNot(HaveOccurred())
						step.modify(&toUpdate)
						Expect(k8sClient.Update(ctx, &toUpdate)).ToNot(HaveOccurred())
						Eventually(func() error {
							return expectObjectStatusExpanding(ctx, k8sClient, &vmv1beta1.VMSingle{}, nsn)
						}, eventualExpandingTimeout).ShouldNot(HaveOccurred())
						Eventually(func() error {
							return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMSingle{}, nsn)
						}, eventualDeploymentAppReadyTimeout).ShouldNot(HaveOccurred())

						var updated vmv1beta1.VMSingle
						Expect(k8sClient.Get(ctx, nsn, &updated)).ToNot(HaveOccurred())

						// verify results
						step.verify(&updated)
					}
				},
				Entry("add backup app", "add-backup", true,
					baseSingle.DeepCopy(),
					testStep{
						modify: func(cr *vmv1beta1.VMSingle) {
							cr.Spec.Volumes = []corev1.Volume{
								{Name: "backup", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
							}
							cr.Spec.VMBackup = &vmv1beta1.VMBackup{
								Destination:  "fs:///opt/backup",
								VolumeMounts: []corev1.VolumeMount{{Name: "backup", MountPath: "/opt/backup"}},
							}
						},
						verify: func(cr *vmv1beta1.VMSingle) {
							var createdDeploy appsv1.Deployment
							Expect(k8sClient.Get(ctx,
								types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &createdDeploy)).
								ToNot(HaveOccurred())
							Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(2))
							Expect(createdDeploy.Spec.Template.Spec.Containers[1].VolumeMounts).To(HaveLen(3))

						},
					}),
				Entry("add and remove annotations", "manage-annotations", false,
					baseSingle.DeepCopy(),
					testStep{
						modify: func(cr *vmv1beta1.VMSingle) {
							cr.Spec.ManagedMetadata = &vmv1beta1.ManagedObjectsMetadata{
								Annotations: map[string]string{
									"added-annotation": "some-value",
								},
							}
						},
						verify: func(cr *vmv1beta1.VMSingle) {
							nss := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}

							expectedAnnotations := map[string]string{"added-annotation": "some-value"}
							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.Deployment{}, &corev1.ServiceAccount{}, &corev1.Service{}}, expectedAnnotations)
							var createdDeploy appsv1.Deployment
							Expect(k8sClient.Get(ctx, nss, &createdDeploy)).
								ToNot(HaveOccurred())
						},
					},
					testStep{
						modify: func(cr *vmv1beta1.VMSingle) {
							delete(cr.Spec.ManagedMetadata.Annotations, "added-annotation")
						},
						verify: func(cr *vmv1beta1.VMSingle) {
							nss := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
							expectedAnnotations := map[string]string{"added-annotation": ""}

							assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.Deployment{}, &corev1.ServiceAccount{}, &corev1.Service{}}, expectedAnnotations)

						},
					},
				),
			)
		},
		)

		It("should skip reconciliation when VMSingle is paused", func() {
			nsn.Name = "vmsingle-paused"
			By("creating a VMSingle")
			initialReplicas := int32(1)
			cr := &vmv1beta1.VMSingle{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1beta1.VMSingleSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: &initialReplicas,
					},
					RetentionPeriod: "1",
				},
			}
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMSingle{}, nsn)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			By("pausing the VMSingle")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, nsn, cr); err != nil {
					return err
				}
				cr.Spec.Paused = true
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			By("attempting to scale the VMSingle while paused")
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
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).ToNot(HaveOccurred())
				return *dep.Spec.Replicas
			}, "10s", "1s").Should(Equal(initialReplicas))

			By("unpausing the VMSingle")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, nsn, cr); err != nil {
					return err
				}
				cr.Spec.Paused = false
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).ShouldNot(HaveOccurred())

			Eventually(func() int32 {
				var dep appsv1.Deployment
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: namespace}, &dep)).ToNot(HaveOccurred())
				return *dep.Spec.Replicas
			}, eventualStatefulsetAppReadyTimeout).Should(Equal(updatedReplicas))
		})
	})
})
