package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
var _ = Describe("vlsingle", Label("vl", "single"), func() {
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
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1.VLSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
			})).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, namespacedName, &vmv1.VLSingle{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		})
		DescribeTable("should create",
			func(name string, cr *vmv1.VLSingle, verify func(*vmv1.VLSingle)) {
				cr.Name = name
				namespacedName.Name = name
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLSingle{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout,
				).Should(Succeed())

				var created vmv1.VLSingle
				Expect(k8sClient.Get(ctx, namespacedName, &created)).To(Succeed())
				verify(&created)
			},
			Entry("with strict security", "strict-security",
				&vmv1.VLSingle{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: vmv1.VLSingleSpec{
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
				func(cr *vmv1.VLSingle) {
					createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
					var createdDeploy appsv1.Deployment
					Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).To(Succeed())
					Expect(createdDeploy.Spec.Template.Spec.Containers).To(HaveLen(1))
					Expect(createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext).NotTo(BeNil())
					Expect(createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).NotTo(BeNil())
					Expect(*createdDeploy.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).To(BeTrue())

				}),
			Entry("with data emptyDir", "emptydir",
				&vmv1.VLSingle{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: vmv1.VLSingleSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
							UseStrictSecurity: ptr.To(false),
						},
						RetentionPeriod: "1",
					},
				},
				func(cr *vmv1.VLSingle) {
					createdChildObjects := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
					var createdDeploy appsv1.Deployment
					Expect(k8sClient.Get(ctx, createdChildObjects, &createdDeploy)).To(Succeed())
					ts := createdDeploy.Spec.Template.Spec
					Expect(ts.Containers).To(HaveLen(1))
					Expect(ts.Volumes).To(HaveLen(1))
					Expect(ts.Containers[0].VolumeMounts).To(HaveLen(1))
				}),
			Entry("with external volume", "externalvolume",
				&vmv1.VLSingle{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: vmv1.VLSingleSpec{
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
						Storage:         &corev1.PersistentVolumeClaimSpec{},
					},
				},
				func(cr *vmv1.VLSingle) {
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

		baseVLSingle := &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
			},
			Spec: vmv1.VLSingleSpec{
				RetentionPeriod: "10",
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To[int32](1),
				},
			},
		}
		type testStep struct {
			setup  func(*vmv1.VLSingle)
			modify func(*vmv1.VLSingle)
			verify func(*vmv1.VLSingle)
		}

		DescribeTable("should update exist",
			func(name string, initCR *vmv1.VLSingle, steps ...testStep) {
				initCR.Name = name
				initCR.Namespace = namespace
				namespacedName.Name = name
				// setup test
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLSingle{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout).Should(Succeed())

				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// perform update
					Eventually(func() error {
						var toUpdate vmv1.VLSingle
						Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
						step.modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &vmv1.VLSingle{}, namespacedName)
					}, eventualDeploymentAppReadyTimeout).Should(Succeed())

					var updated vmv1.VLSingle
					Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())

					// verify results
					step.verify(&updated)
				}
			},
			Entry("add and remove annotations", "manage-annotations",
				baseVLSingle.DeepCopy(),
				testStep{
					modify: func(cr *vmv1.VLSingle) {
						cr.Spec.ManagedMetadata = &vmv1beta1.ManagedObjectsMetadata{
							Annotations: map[string]string{
								"added-annotation": "some-value",
							},
						}
					},
					verify: func(cr *vmv1.VLSingle) {
						nss := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}

						expectedAnnotations := map[string]string{"added-annotation": "some-value"}
						assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.Deployment{}, &corev1.ServiceAccount{}, &corev1.Service{}}, expectedAnnotations)
						var createdDeploy appsv1.Deployment
						Expect(k8sClient.Get(ctx, nss, &createdDeploy)).
							To(Succeed())
					},
				},
				testStep{
					modify: func(cr *vmv1.VLSingle) {
						delete(cr.Spec.ManagedMetadata.Annotations, "added-annotation")
					},
					verify: func(cr *vmv1.VLSingle) {
						nss := types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}
						expectedAnnotations := map[string]string{"added-annotation": ""}

						assertAnnotationsOnObjects(ctx, nss, []client.Object{&appsv1.Deployment{}, &corev1.ServiceAccount{}, &corev1.Service{}}, expectedAnnotations)

					},
				},
			),
			Entry("with syslog tls", "syslog-tls",
				baseVLSingle.DeepCopy(),
				testStep{
					modify: func(cr *vmv1.VLSingle) {
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
						cr.Spec.SyslogSpec = &vmv1.SyslogServerSpec{
							TCPListeners: []*vmv1.SyslogTCPListener{
								{
									ListenPort:   9505,
									IgnoreFields: vmv1.FieldsListString(`["ip","id"]`),
								},
								{
									ListenPort:   9500,
									StreamFields: vmv1.FieldsListString(`["stream", "log.message"]`),
									IgnoreFields: vmv1.FieldsListString(`["_id","_container_id"]`),
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
									ListenPort:       9500,
									StreamFields:     vmv1.FieldsListString(`["stream", "log.message"]`),
									CompressMethod:   "zstd",
									DecolorizeFields: vmv1.FieldsListString(`["severity"]`),
								},
							},
						}
					},
					verify: func(cr *vmv1.VLSingle) {
						var svc corev1.Service
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &svc)).To(Succeed())
						Expect(svc.Spec.Ports).To(HaveLen(4))
						var dep appsv1.Deployment
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.PrefixedName()}, &dep)).To(Succeed())
						Expect(dep.Spec.Template.Spec.Volumes).To(HaveLen(2))
						Expect(dep.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(2))
					},
				},
			),
		)
	},
	)
})
