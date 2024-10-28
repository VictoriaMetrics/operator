package e2e

import (
	"context"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:dupl
var _ = Describe("test  vmsingle Controller", func() {
	It("must clean up previous test resutls", func() {
		ctx := context.Background()
		// clean up before tests
		Expect(k8sClient.DeleteAllOf(ctx, &vmv1beta1.VMSingle{}, &client.DeleteAllOfOptions{
			ListOptions: client.ListOptions{
				Namespace: namespace,
			},
		})).To(Succeed())
		Eventually(func() bool {
			var unDeletedObjects vmv1beta1.VMSingleList
			Expect(k8sClient.List(ctx, &unDeletedObjects, &client.ListOptions{
				Namespace: namespace,
			})).To(Succeed())
			return len(unDeletedObjects.Items) == 0
		}, eventualDeletionTimeout).Should(BeTrue())

	})

	Context("e2e vmsingle", func() {
		var ctx context.Context
		namespace := "default"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		BeforeEach(func() {
			ctx = context.Background()
		})
		AfterEach(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1beta1.VMSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
			})).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, namespacedName, &vmv1beta1.VMSingle{})
			}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
		})
		Context("crud", func() {
			DescribeTable("should create vmsingle",
				func(name string, cr *vmv1beta1.VMSingle, verify func(*vmv1beta1.VMSingle)) {
					cr.Name = name
					namespacedName.Name = name
					Expect(k8sClient.Create(ctx, cr)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMSingle{}, namespacedName)
					}, eventualDeploymentAppReadyTimeout,
					).Should(Succeed())

					var created vmv1beta1.VMSingle
					Expect(k8sClient.Get(ctx, namespacedName, &created)).To(Succeed())
					verify(&created)
				},
				Entry("with built-in pvc and insert ports", "create-with-pvc-ports",
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
							To(Succeed())
						Expect(createdSvc.Spec.Ports).To(HaveLen(8))
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).To(Succeed())
						Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(1))
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(8))
					}),
				Entry("with empty resources and vmbackup ", "create-wo-resource-w-backup",
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
								AcceptEULA:  true,
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
							To(Succeed())
						Expect(createdSvc.Spec.Ports).To(HaveLen(2))
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).To(Succeed())
						Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(2))
						Expect(createdDeploy.Spec.Template.Spec.Containers[1].VolumeMounts).To(HaveLen(2))
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].Resources).To(Equal(corev1.ResourceRequirements{}))
						Expect(createdDeploy.Spec.Template.Spec.Containers[1].Resources).To(Equal(corev1.ResourceRequirements{}))
						var vss vmv1beta1.VMServiceScrape
						Expect(k8sClient.Get(ctx, createdChildObjects, &vss)).To(Succeed())
						Expect(vss.Spec.Endpoints).To(HaveLen(2))
					}),
				Entry("with strict security", "strict-security",
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
						Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).To(Succeed())
						Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(1))
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext).NotTo(BeNil())
						Expect(createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).NotTo(BeNil())
						Expect(*createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).To(BeTrue())

					}),
			)

			existSingle := &vmv1beta1.VMSingle{
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
			DescribeTable("should update exist vmsingle",
				func(name string, modify func(*vmv1beta1.VMSingle), verify func(*vmv1beta1.VMSingle)) {
					// create and wait ready
					existSingle := existSingle.DeepCopy()
					existSingle.Name = name
					namespacedName.Name = name
					Expect(k8sClient.Create(ctx, existSingle)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMSingle{}, namespacedName)
					}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
					// update and wait ready
					Eventually(func() error {
						var toUpdate vmv1beta1.VMSingle
						Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
						modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusExpanding(ctx, k8sClient, &vmv1beta1.VMSingle{}, namespacedName)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMSingle{}, namespacedName)
					}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
					// verify
					var updated vmv1beta1.VMSingle
					Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
					verify(&updated)
				},
				Entry("add backup app", "add-backup",
					func(cr *vmv1beta1.VMSingle) {
						cr.Spec.Volumes = []corev1.Volume{
							{Name: "backup", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						}
						cr.Spec.VMBackup = &vmv1beta1.VMBackup{
							AcceptEULA:   true,
							Destination:  "fs:///opt/backup",
							VolumeMounts: []corev1.VolumeMount{{Name: "backup", MountPath: "/opt/backup"}},
						}
					},
					func(cr *vmv1beta1.VMSingle) {
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx,
							types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &createdDeploy)).
							To(Succeed())
						Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(2))
						Expect(createdDeploy.Spec.Template.Spec.Containers[1].VolumeMounts).To(HaveLen(2))
					}),
			)
		},
		)
	})
})
