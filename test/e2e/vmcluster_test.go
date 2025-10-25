package e2e

import (
	"context"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

//nolint:dupl,lll
var _ = Describe("e2e vmcluster", Label("vm", "cluster"), func() {
	licenseKey := os.Getenv("LICENSE_KEY")
	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	var ctx context.Context
	nsn := types.NamespacedName{
		Namespace: namespace,
	}
	Context("create", func() {
		JustBeforeEach(func() {
			ctx = context.Background()
		})
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
			})).To(Succeed(), "must delete vmcluster after test")
			waitResourceDeleted(ctx, k8sClient, types.NamespacedName{
				Name:      nsn.Name,
				Namespace: namespace,
			}, &vmv1beta1.VMCluster{})
		})

		DescribeTable("should create vmcluster", func(name string, cr *vmv1beta1.VMCluster, verify func(cr *vmv1beta1.VMCluster)) {
			nsn.Name = name
			cr.Name = name
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMCluster{}, nsn)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
			if verify != nil {
				var createdCluster vmv1beta1.VMCluster
				Expect(k8sClient.Get(ctx, nsn, &createdCluster)).To(Succeed())
				verify(&createdCluster)
			}

		},
			Entry("without any components", "empty", &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1beta1.VMClusterSpec{RetentionPeriod: "1"},
			}, nil,
			),
			Entry("with all components", "all-services",
				&vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				nil,
			),
			Entry("with vmstorage and vmselect", "with-select", &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1beta1.VMClusterSpec{
					RetentionPeriod: "1",
					VMStorage: &vmv1beta1.VMStorage{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
					VMSelect: &vmv1beta1.VMSelect{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
			}, nil,
			),
			Entry("with vmstorage and vminsert", "with-insert", &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1beta1.VMClusterSpec{
					RetentionPeriod: "1",
					VMStorage: &vmv1beta1.VMStorage{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
					VMInsert: &vmv1beta1.VMInsert{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
			},
				nil),
			Entry("with security enable and without default resources", "all-secure",
				&vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity:   ptr.To(true),
								UseDefaultResources: ptr.To(false),
							},
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity:   ptr.To(true),
								UseDefaultResources: ptr.To(false),
							},

							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseStrictSecurity:   ptr.To(true),
								UseDefaultResources: ptr.To(false),
							},
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
				func(cr *vmv1beta1.VMCluster) {
					clusterNsnObjects := map[types.NamespacedName]client.Object{
						{Namespace: cr.Namespace, Name: cr.GetVMInsertName()}:  &appsv1.Deployment{},
						{Namespace: cr.Namespace, Name: cr.GetVMStorageName()}: &appsv1.StatefulSet{},
						{Namespace: cr.Namespace, Name: cr.GetVMSelectName()}:  &appsv1.StatefulSet{},
					}
					for nsn, obj := range clusterNsnObjects {
						By(fmt.Sprintf("verifying object with name: %s", nsn))
						Expect(k8sClient.Get(ctx, nsn, obj)).To(Succeed())
						switch t := obj.(type) {
						case *appsv1.Deployment:
							assertStrictSecurity(t.Spec.Template.Spec)
						case *appsv1.StatefulSet:
							assertStrictSecurity(t.Spec.Template.Spec)
						default:
							Fail(fmt.Sprintf("type %T is not expected", t))
						}
					}
				},
			),
			Entry("with external security enable and UseStrictSecurity:false", "all-secure-external",
				&vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name,
					},
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
								SecurityContext: &vmv1beta1.SecurityContext{
									PodSecurityContext: &corev1.PodSecurityContext{
										RunAsNonRoot: ptr.To(true),
										RunAsUser:    ptr.To(int64(65534)),
										RunAsGroup:   ptr.To(int64(65534)),
									},
									ContainerSecurityContext: &vmv1beta1.ContainerSecurityContext{
										Privileged: ptr.To(false),
									},
								},
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},

							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
								SecurityContext: &vmv1beta1.SecurityContext{
									PodSecurityContext: &corev1.PodSecurityContext{
										RunAsNonRoot: ptr.To(true),
										RunAsUser:    ptr.To(int64(65534)),
										RunAsGroup:   ptr.To(int64(65534)),
									},
									ContainerSecurityContext: &vmv1beta1.ContainerSecurityContext{
										Privileged: ptr.To(false),
									},
								},
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
								SecurityContext: &vmv1beta1.SecurityContext{
									PodSecurityContext: &corev1.PodSecurityContext{
										RunAsNonRoot: ptr.To(true),
										RunAsUser:    ptr.To(int64(65534)),
										RunAsGroup:   ptr.To(int64(65534)),
									},
									ContainerSecurityContext: &vmv1beta1.ContainerSecurityContext{
										Privileged: ptr.To(false),
									},
								},
							},
						},
					},
				},
				func(cr *vmv1beta1.VMCluster) {
					clusterNsnObjects := map[types.NamespacedName]client.Object{
						{Namespace: cr.Namespace, Name: cr.GetVMInsertName()}:  &appsv1.Deployment{},
						{Namespace: cr.Namespace, Name: cr.GetVMStorageName()}: &appsv1.StatefulSet{},
						{Namespace: cr.Namespace, Name: cr.GetVMSelectName()}:  &appsv1.StatefulSet{},
					}
					for nsn, obj := range clusterNsnObjects {
						By(fmt.Sprintf("verifying object with name: %s", nsn))
						Expect(k8sClient.Get(ctx, nsn, obj)).To(Succeed())
						switch t := obj.(type) {
						case *appsv1.Deployment:
							assertStrictSecurity(t.Spec.Template.Spec)
						case *appsv1.StatefulSet:
							assertStrictSecurity(t.Spec.Template.Spec)
						default:
							Fail(fmt.Sprintf("type %T is not expected", t))
						}
					}
				},
			),
		)
	})
	Context("update", func() {
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
				)).To(Succeed())
			}
		})
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
			})).To(Succeed())
			waitResourceDeleted(ctx, k8sClient, types.NamespacedName{
				Name:      nsn.Name,
				Namespace: namespace,
			}, &vmv1beta1.VMCluster{})
			if licenseKey != "" {
				Expect(k8sClient.Delete(ctx,
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "license",
							Namespace: namespace,
						},
					},
				)).To(Succeed())
			}
		})

		type testStep struct {
			setup  func(*vmv1beta1.VMCluster)
			modify func(*vmv1beta1.VMCluster)
			verify func(*vmv1beta1.VMCluster)
		}

		DescribeTable("should update exist cluster",
			func(name string, isEnterprise bool, initCR *vmv1beta1.VMCluster, steps ...testStep) {
				if isEnterprise {
					if licenseKey == "" {
						Skip("ignoring VMCluster test, license was not found")
					}
					cfg := config.MustGetBaseConfig()
					tag := cfg.MetricsVersion + "-enterprise-cluster"
					initCR.Spec.VMStorage.Image.Tag = tag
					initCR.Spec.VMSelect.Image.Tag = tag
					initCR.Spec.VMInsert.Image.Tag = tag
					initCR.Spec.License = &vmv1beta1.License{
						KeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "license",
							},
							Key: "key",
						},
					}
				}
				nsn.Name = name
				initCR.Namespace = namespace
				initCR.Name = name
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, initCR, nsn)
				}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
				for _, step := range steps {
					if step.setup != nil {
						step.setup(initCR)
					}
					// update and wait ready
					var toUpdate vmv1beta1.VMCluster
					Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
					step.modify(&toUpdate)
					Expect(k8sClient.Update(ctx, &toUpdate)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusExpanding(ctx, k8sClient, initCR, nsn)
					}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, initCR, nsn)
					}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
					var updated vmv1beta1.VMCluster
					Expect(k8sClient.Get(ctx, nsn, &updated)).To(Succeed())
					step.verify(&updated)
				}
			},
			Entry("by scaling select and storage replicas to 2", "storage-select-r-2", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMStorage.ReplicaCount = ptr.To[int32](2)
						cr.Spec.VMSelect.ReplicaCount = ptr.To[int32](2)
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						Expect(expectPodCount(k8sClient, 2, namespace, cr.VMStorageSelectorLabels())).To(BeEmpty())
						Expect(expectPodCount(k8sClient, 2, namespace, cr.VMSelectSelectorLabels())).To(BeEmpty())
					},
				},
			),
			Entry("by adding vmbackupmanager to vmstorage ", "with-backup", true,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMStorage.InitContainers = []corev1.Container{
							{
								Name:  "create-dir",
								Image: "curlimages/curl:7.85.0",
								Env: []corev1.EnvVar{

									{
										Name: "POD_NAME",
										ValueFrom: &corev1.EnvVarSource{
											FieldRef: &corev1.ObjectFieldSelector{
												FieldPath: "metadata.name",
											},
										},
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "backup",
										MountPath: "/opt/backup-dir",
									},
								},
								Command: []string{"sh", "-c"},
								Args:    []string{"mkdir /opt/backup-dir/$POD_NAME"},
							},
						}
						cr.Spec.VMStorage.Volumes = []corev1.Volume{
							{Name: "backup", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						}
						cr.Spec.VMStorage.VMBackup = &vmv1beta1.VMBackup{
							Destination: "fs:///opt/backup-dir",
							VolumeMounts: []corev1.VolumeMount{
								{Name: "backup", MountPath: "/opt/backup-dir"},
							},
						}

					},
					verify: func(cr *vmv1beta1.VMCluster) {
						nsn := types.NamespacedName{
							Namespace: namespace,
							Name:      cr.GetVMStorageName(),
						}
						var svc corev1.Service
						Expect(k8sClient.Get(ctx, nsn, &svc)).To(Succeed())
						Expect(svc.Spec.Ports).To(HaveLen(4))
						var vss vmv1beta1.VMServiceScrape
						Expect(k8sClient.Get(ctx, nsn, &vss)).To(Succeed())
						Expect(vss.Spec.Endpoints).To(HaveLen(2))
					},
				},
			),
			Entry("by scaling storage and insert replicas to 2", "storage-insert-r-2", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMStorage.ReplicaCount = ptr.To[int32](2)
						cr.Spec.VMInsert.ReplicaCount = ptr.To[int32](2)
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						Expect(expectPodCount(k8sClient, 2, namespace, cr.VMStorageSelectorLabels())).To(BeEmpty())
						Eventually(func() string {
							return expectPodCount(k8sClient, 2, namespace, cr.VMInsertSelectorLabels())
						}, eventualDeploymentPodTimeout).Should(BeEmpty())
					},
				},
			),
			Entry("by changing storage revisionHistoryLimit to 2", "storage-revision-2", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMStorage.RevisionHistoryLimitCount = ptr.To[int32](2)
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						var updatedCluster vmv1beta1.VMCluster
						Expect(k8sClient.Get(ctx, nsn, &updatedCluster)).To(Succeed())
						Expect(*updatedCluster.Spec.VMStorage.RevisionHistoryLimitCount).To(Equal(int32(2)))
					},
				},
			),
			Entry("by adding clusterNative ports", "storage-native-r-2", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMInsert.ClusterNativePort = "8035"
						cr.Spec.VMSelect.ClusterNativePort = "8036"
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						var updatedSvc corev1.Service
						Expect(k8sClient.Get(ctx,
							types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name},
							&updatedSvc)).
							To(Succeed())
						Expect(updatedSvc.Spec.Ports).To(HaveLen(2))
						Expect(updatedSvc.Spec.Ports[1].Port).To(Equal(int32(8036)))
						Expect(k8sClient.Get(ctx,
							types.NamespacedName{Namespace: namespace, Name: "vminsert-" + cr.Name},
							&updatedSvc)).
							To(Succeed())
						Expect(updatedSvc.Spec.Ports).To(HaveLen(2))
						Expect(updatedSvc.Spec.Ports[1].Port).To(Equal(int32(8035)))
					},
				},
			),
			Entry("by deleting select component", "select-delete", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					setup: func(cr *vmv1beta1.VMCluster) {
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name}, &appsv1.StatefulSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name}, &corev1.Service{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name}, &vmv1beta1.VMServiceScrape{})).To(Succeed())
					},
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMSelect = nil
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						nsn := types.NamespacedName{
							Name:      cr.GetVMSelectName(),
							Namespace: namespace,
						}
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.StatefulSet{})
						waitResourceDeleted(ctx, k8sClient, nsn, &corev1.Service{})
						waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMServiceScrape{})
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMSelect = &vmv1beta1.VMSelect{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						}
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name}, &appsv1.StatefulSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name}, &corev1.Service{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name}, &vmv1beta1.VMServiceScrape{})).To(Succeed())
					},
				},
			),
			Entry("by deleting storage and insert components", "storage-insert-delete", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					setup: func(cr *vmv1beta1.VMCluster) {
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + cr.Name}, &appsv1.StatefulSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + cr.Name}, &corev1.Service{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + cr.Name}, &vmv1beta1.VMServiceScrape{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vminsert-" + cr.Name}, &appsv1.Deployment{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vminsert-" + cr.Name}, &corev1.Service{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vminsert-" + cr.Name}, &vmv1beta1.VMServiceScrape{})).To(Succeed())
					},
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMStorage = nil
						cr.Spec.VMInsert = nil
						cr.Spec.VMSelect.ExtraArgs = map[string]string{
							"storageNode": "non-exist-vmstorage:8402",
						}
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						nsn := types.NamespacedName{
							Namespace: namespace,
							Name:      cr.GetVMStorageName(),
						}
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.StatefulSet{})
						waitResourceDeleted(ctx, k8sClient, nsn, &corev1.Service{})
						waitResourceDeleted(ctx, k8sClient, nsn, &vmv1beta1.VMServiceScrape{})

						nsn.Name = cr.GetVMInsertName()
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.Deployment{})
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMStorage = &vmv1beta1.VMStorage{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						}
						cr.Spec.VMInsert = &vmv1beta1.VMInsert{
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								UseDefaultResources: ptr.To(false),
							},
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						}
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + cr.Name}, &appsv1.StatefulSet{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + cr.Name}, &corev1.Service{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + cr.Name}, &vmv1beta1.VMServiceScrape{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vminsert-" + cr.Name}, &appsv1.Deployment{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vminsert-" + cr.Name}, &corev1.Service{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vminsert-" + cr.Name}, &vmv1beta1.VMServiceScrape{})).To(Succeed())
					},
				},
			),
			Entry("by deleting deleting and renaming additional services", "select-additional-svc", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
								EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
									Name: "my-service-name",
								},
								Spec: corev1.ServiceSpec{
									Type: corev1.ServiceTypeClusterIP,
									Ports: []corev1.ServicePort{
										{
											TargetPort: intstr.FromInt(8431),
											Name:       "web-port",
											Port:       8435,
										},
									},
								},
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
								Spec: corev1.ServiceSpec{
									Type: corev1.ServiceTypeClusterIP,
									Ports: []corev1.ServicePort{
										{
											TargetPort: intstr.FromInt(8431),
											Name:       "web-port",
											Port:       8435,
										},
									},
								},
							},
						},
					},
				},
				testStep{
					setup: func(cr *vmv1beta1.VMCluster) {
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name + "-additional-service"}, &corev1.Service{})).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "my-service-name"}, &corev1.Service{})).To(Succeed())
					},
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMSelect.ServiceSpec = nil
						cr.Spec.VMStorage.ServiceSpec.Name = ""
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						nsn := types.NamespacedName{
							Namespace: namespace,
							Name:      cr.GetVMSelectName() + "-additional-service",
						}
						waitResourceDeleted(ctx, k8sClient, nsn, &corev1.Service{})
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + cr.Name + "-additional-service"}, &corev1.Service{})).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{
							Namespace: namespace,
							Name:      "my-service-name",
						}, &corev1.Service{})
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.VMStorage.ServiceSpec.UseAsDefault = true
						cr.Spec.VMSelect.ServiceSpec = &vmv1beta1.AdditionalServiceSpec{
							EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{
								Name: "my-service-name-v2",
							},
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeClusterIP,
								Ports: []corev1.ServicePort{
									{
										TargetPort: intstr.FromInt(8431),
										Name:       "web-port",
										Port:       8436,
									},
								},
							},
						}
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{
							Namespace: namespace,
							Name:      cr.GetVMStorageName() + "-additional-service",
						}, &corev1.Service{})
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "my-service-name-v2"}, &corev1.Service{})).To(Succeed())
						var stSvc corev1.Service
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + cr.Name}, &stSvc)).To(Succeed())
						Expect(stSvc.Spec.Ports).To(HaveLen(4))
					},
				},
			),
			Entry("by adding imagePullSecret", "storage-image-pull-secret", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod:  "1",
						ImagePullSecrets: nil,
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					setup: func(v *vmv1beta1.VMCluster) {
						pullSecret := corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{Name: "test-pull-secret", Namespace: namespace},
							Data: map[string][]byte{
								".dockerconfigjson": []byte(`{"auths":{"test.example.com":{"username":"test","password":"12345","email":"test@example.com","auth":"dGVzdDoxMjM0NQ=="}}}`),
							},
							Type: corev1.SecretTypeDockerConfigJson,
						}
						Expect(k8sClient.Create(ctx, &pullSecret)).To(Succeed())
					},
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
							{Name: "test-pull-secret"},
						}
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						var sts appsv1.StatefulSet
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVMStorageName()}
						Expect(k8sClient.Get(ctx, nsn, &sts)).To(Succeed())
						Expect(sts.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(1))
						Expect(k8sClient.Delete(ctx,
							&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test-pull-secret", Namespace: namespace}})).
							To(Succeed())
					},
				},
			),
			Entry("by switching to vmauth loadbalancer", "with-load-balancing", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
							Enabled: false,
						},
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					setup: func(cr *vmv1beta1.VMCluster) {
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8480/insert/0/prometheus/api/v1/import/prometheus", cr.GetVMInsertName(), namespace),
							payload: `up{bar="baz"} 123
up{baz="bar"} 123
              `,
							expectedCode: 204,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8481/select/0/prometheus/api/v1/query?query=up", cr.GetVMSelectName(), namespace),
						})
					},
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = true
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						By("switching enabling loadbalanacer")
						var lbDep appsv1.Deployment
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVMAuthLBName()}
						Expect(k8sClient.Get(ctx, nsn, &lbDep)).To(Succeed())
						var svc corev1.Service
						Expect(k8sClient.Get(ctx, nsn, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &svc)).To(Succeed())
						var vss vmv1beta1.VMServiceScrape
						Expect(k8sClient.Get(ctx, nsn, &vss)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &vss)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &vss)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{
							Name:      cr.GetVMSelectName(),
							Namespace: namespace,
						}, &vmv1beta1.VMServiceScrape{})
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{
							Name:      cr.GetVMInsertName(),
							Namespace: namespace,
						}, &vmv1beta1.VMServiceScrape{})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8480/insert/0/prometheus/api/v1/import/prometheus", cr.GetVMInsertName(), namespace),
							payload: `up{bar="baz"} 123
up{baz="bar"} 123
              `,
							expectedCode: 204,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8481/select/0/prometheus/api/v1/query?query=up", cr.GetVMSelectName(), namespace),
						})
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = false
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						By("disabling loadbalancer")
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVMAuthLBName()}
						waitResourceDeleted(ctx, k8sClient, nsn, &appsv1.Deployment{})
						var svc corev1.Service
						waitResourceDeleted(ctx, k8sClient, nsn, &svc)
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{
							Namespace: namespace,
							Name:      cr.GetVMInsertLBName(),
						}, &svc)
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &svc)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{
							Namespace: namespace,
							Name:      cr.GetVMSelectLBName(),
						}, &svc)
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &svc)).To(Succeed())
						var vss vmv1beta1.VMServiceScrape
						waitResourceDeleted(ctx, k8sClient, nsn, &vss)
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &vss)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &vss)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &vss)
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &vss)
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8480/insert/0/prometheus/api/v1/import/prometheus", cr.GetVMInsertName(), namespace),
							payload: `up{bar="baz"} 123
up{baz="bar"} 123
              `,
							expectedCode: 204,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8481/select/0/prometheus/api/v1/query?query=up", cr.GetVMSelectName(), namespace),
						})
					},
				},
			),
			Entry("by switching partially to vmauth loadbalanacer", "with-partial-load-balancing", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
							Enabled: false,
						},
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = true
						cr.Spec.RequestsLoadBalancer.DisableInsertBalancing = true
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8480/insert/0/prometheus/api/v1/import/prometheus", cr.GetVMInsertName(), namespace),
							payload: `up{bar="baz"} 123
up{baz="bar"} 123
              `,
							expectedCode: 204,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8481/select/0/prometheus/api/v1/query?query=up", cr.GetVMSelectName(), namespace),
						})
						var lbDep appsv1.Deployment
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVMAuthLBName()}
						Expect(k8sClient.Get(ctx, nsn, &lbDep)).To(Succeed())
						var svc corev1.Service
						Expect(k8sClient.Get(ctx, nsn, &svc)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &svc)
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &svc)).To(Succeed())
						var vss vmv1beta1.VMServiceScrape
						Expect(k8sClient.Get(ctx, nsn, &vss)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &vss)
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &vss)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &vss)
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &vss)).To(Succeed())

					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = true
						cr.Spec.RequestsLoadBalancer.DisableInsertBalancing = false
						cr.Spec.RequestsLoadBalancer.Spec.ReplicaCount = ptr.To[int32](2)
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						var lbDep appsv1.Deployment
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVMAuthLBName()}
						Expect(k8sClient.Get(ctx, nsn, &lbDep)).To(Succeed())
						Expect(*lbDep.Spec.Replicas).To(BeEquivalentTo(int32(2)))
						var svc corev1.Service
						Expect(k8sClient.Get(ctx, nsn, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &svc)).To(Succeed())
						var vss vmv1beta1.VMServiceScrape
						Expect(k8sClient.Get(ctx, nsn, &vss)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &vss)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &vss)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &vss)
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &vss)
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8480/insert/0/prometheus/api/v1/import/prometheus", cr.GetVMInsertName(), namespace),
							payload: `up{bar="baz"} 123
up{baz="bar"} 123
              `,
							expectedCode: 204,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8481/select/0/prometheus/api/v1/query?query=up", cr.GetVMSelectName(), namespace),
						})
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.RequestsLoadBalancer.Enabled = true
						cr.Spec.RequestsLoadBalancer.DisableInsertBalancing = false
						cr.Spec.RequestsLoadBalancer.DisableSelectBalancing = true
						cr.Spec.RequestsLoadBalancer.Spec.ReplicaCount = ptr.To[int32](2)
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						By("disabling select loadbalancing")
						var lbDep appsv1.Deployment
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVMAuthLBName()}
						Expect(k8sClient.Get(ctx, nsn, &lbDep)).To(Succeed())
						Expect(*lbDep.Spec.Replicas).To(BeEquivalentTo(int32(2)))
						var svc corev1.Service
						Expect(k8sClient.Get(ctx, nsn, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &svc)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &svc)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &svc)
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &svc)).To(Succeed())
						var vss vmv1beta1.VMServiceScrape
						Expect(k8sClient.Get(ctx, nsn, &vss)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &vss)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &vss)
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &vss)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &vss)
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8480/insert/0/prometheus/api/v1/import/prometheus", cr.GetVMInsertName(), namespace),
							payload: `up{bar="baz"} 123
up{baz="bar"} 123
              `,
							expectedCode: 204,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8481/select/0/prometheus/api/v1/query?query=up", cr.GetVMSelectName(), namespace),
						})
					},
				},
			),
			Entry("by running with load-balancer and modify vmauth", "with-load-balancing-modify-auth", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
							Enabled: true,
							Spec: vmv1beta1.VMAuthLoadBalancerSpec{
								CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
									Port: "8431",
								},
							},
						},
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](2),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.RequestsLoadBalancer.Spec.ReplicaCount = ptr.To[int32](2)
						cr.Spec.RequestsLoadBalancer.Spec.UseStrictSecurity = ptr.To(true)
						cr.Spec.RequestsLoadBalancer.Spec.AdditionalServiceSpec = &vmv1beta1.AdditionalServiceSpec{
							Spec: corev1.ServiceSpec{
								Type:      corev1.ServiceTypeClusterIP,
								ClusterIP: corev1.ClusterIPNone,
							},
						}
						cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget = &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
							MaxUnavailable: ptr.To(intstr.Parse("1")),
						}
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8480/insert/0/prometheus/api/v1/import/prometheus", cr.GetVMInsertName(), namespace),
							payload: `up{bar="baz"} 123
up{baz="bar"} 123
              `,
							expectedCode: 204,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8481/select/0/prometheus/api/v1/query?query=up", cr.GetVMSelectName(), namespace),
						})
						var lbDep appsv1.Deployment
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVMAuthLBName()}
						Expect(k8sClient.Get(ctx, nsn, &lbDep)).To(Succeed())
						Expect(*lbDep.Spec.Replicas).To(Equal(int32(2)))
						Expect(lbDep.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
						Expect(lbDep.Spec.Template.Spec.SecurityContext.RunAsUser).NotTo(BeNil())
						Expect(lbDep.Spec.Template.Spec.SecurityContext.RunAsGroup).NotTo(BeNil())

						var svc corev1.Service
						Expect(k8sClient.Get(ctx, nsn, &svc)).To(Succeed())
						Expect(svc.Spec.ClusterIP).To(Equal(corev1.ClusterIPNone))
						Expect(svc.Spec.Ports).To(HaveLen(1))
						Expect(svc.Spec.Ports[0].TargetPort).To(Equal(intstr.Parse("8431")))
						var vss vmv1beta1.VMServiceScrape
						Expect(k8sClient.Get(ctx, nsn, &vss)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertLBName()}, &vss)).To(Succeed())
						Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectLBName()}, &vss)).To(Succeed())
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMSelectName()}, &vss)
						waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: cr.GetVMInsertName()}, &vss)
						var pdb policyv1.PodDisruptionBudget
						Expect(k8sClient.Get(ctx, nsn, &pdb)).To(Succeed())
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.RequestsLoadBalancer.Spec.ReplicaCount = ptr.To[int32](1)
						cr.Spec.RequestsLoadBalancer.Spec.UseStrictSecurity = ptr.To(false)
						cr.Spec.RequestsLoadBalancer.Spec.AdditionalServiceSpec = nil
						cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget = nil
						cr.Spec.RequestsLoadBalancer.Spec.Port = ""
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8480/insert/0/prometheus/api/v1/import/prometheus", cr.GetVMInsertName(), namespace),
							payload: `up{bar="baz"} 123
up{baz="bar"} 123
              `,
							expectedCode: 204,
						})
						expectHTTPRequestToSucceed(ctx, cr, httpRequestOpts{
							dstURL: fmt.Sprintf("http://%s.%s.svc:8481/select/0/prometheus/api/v1/query?query=up", cr.GetVMSelectName(), namespace),
						})
						var lbDep appsv1.Deployment
						nsn := types.NamespacedName{Namespace: namespace, Name: cr.GetVMAuthLBName()}
						Expect(k8sClient.Get(ctx, nsn, &lbDep)).To(Succeed())
						Expect(*lbDep.Spec.Replicas).To(Equal(int32(1)))
						Expect(lbDep.Spec.Template.Spec.SecurityContext.RunAsUser).To(BeNil())
						Expect(lbDep.Spec.Template.Spec.SecurityContext.RunAsGroup).To(BeNil())
						var svc corev1.Service
						Expect(k8sClient.Get(ctx, nsn, &svc)).To(Succeed())
						Expect(svc.Spec.ClusterIP).NotTo(Equal(corev1.ClusterIPNone))
						Expect(svc.Spec.Ports[0].TargetPort).To(Equal(intstr.Parse("8427")))
						waitResourceDeleted(ctx, k8sClient, nsn, &policyv1.PodDisruptionBudget{})
					},
				},
			),
			Entry("by changing annotations for created objects", "manage-annotations", false,
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{Enabled: true},
						RetentionPeriod:      "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.ManagedMetadata = &vmv1beta1.ManagedObjectsMetadata{
							// attempt to change selector label should fail
							Labels:      map[string]string{"label-1": "value-1", "label-2": "value-2", "managed-by": "wrong-value"},
							Annotations: map[string]string{"annotation-1": "value-a-1", "annotation-2": "value-a-2"},
						}
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						expectedAnnotations := map[string]string{"annotation-1": "value-a-1", "annotation-2": "value-a-2"}
						expectedLabels := map[string]string{"label-1": "value-1", "label-2": "value-2", "managed-by": "vm-operator"}
						selectN, insertN, storageN, lbName, saName := cr.GetVMSelectName(), cr.GetVMInsertName(), cr.GetVMStorageName(), cr.GetVMAuthLBName(), cr.PrefixedName()
						objectsByNsn := map[types.NamespacedName][]client.Object{
							{Name: selectN}:  {&corev1.Service{}, &appsv1.StatefulSet{}},
							{Name: storageN}: {&corev1.Service{}, &appsv1.StatefulSet{}},
							{Name: insertN}:  {&corev1.Service{}, &appsv1.Deployment{}},
							{Name: lbName}:   {&corev1.Service{}, &appsv1.Deployment{}},
							{Name: saName}:   {&corev1.ServiceAccount{}},
						}
						for nsn, objectsToAssert := range objectsByNsn {
							nsn.Namespace = namespace
							By(nsn.String())
							assertAnnotationsOnObjects(ctx, nsn, objectsToAssert, expectedAnnotations)
							assertLabelsOnObjects(ctx, nsn, objectsToAssert, expectedLabels)
						}
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						cr.Spec.ManagedMetadata = &vmv1beta1.ManagedObjectsMetadata{
							Annotations: map[string]string{"annotation-1": "value-a-1"},
						}
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						expectedAnnotations := map[string]string{"annotation-1": "value-a-1", "annotation-2": ""}
						expectedLabels := map[string]string{"label-1": "", "label-2": "", "managed-by": "vm-operator"}
						selectN, insertN, storageN, lbName, saName := cr.GetVMSelectName(), cr.GetVMInsertName(), cr.GetVMStorageName(), cr.GetVMAuthLBName(), cr.PrefixedName()
						objectsByNsn := map[types.NamespacedName][]client.Object{
							{Name: selectN}:  {&corev1.Service{}, &appsv1.StatefulSet{}},
							{Name: storageN}: {&corev1.Service{}, &appsv1.StatefulSet{}},
							{Name: insertN}:  {&corev1.Service{}, &appsv1.Deployment{}},
							{Name: lbName}:   {&corev1.Service{}, &appsv1.Deployment{}},
							{Name: saName}:   {&corev1.ServiceAccount{}},
						}
						for nsn, objectsToAssert := range objectsByNsn {
							nsn.Namespace = namespace
							By(nsn.String())
							assertAnnotationsOnObjects(ctx, nsn, objectsToAssert, expectedAnnotations)
							assertLabelsOnObjects(ctx, nsn, objectsToAssert, expectedLabels)
						}
					},
				},
			),
		)
	})
	Context("update", func() {
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
			})).To(Succeed())
			waitResourceDeleted(ctx, k8sClient, types.NamespacedName{Namespace: namespace, Name: nsn.Name}, &vmv1beta1.VMCluster{})
		})

		type testStep struct {
			modify func(*vmv1beta1.VMCluster)
			verify func(*vmv1beta1.VMCluster)
		}

		DescribeTable("should respect customized behavior",
			func(name string, initCR *vmv1beta1.VMCluster, steps ...testStep) {
				nsn.Name = name
				initCR.Namespace = namespace
				initCR.Name = name
				ctx = context.Background()
				Expect(k8sClient.Create(ctx, initCR)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, initCR, nsn)
				}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
				for _, step := range steps {
					// update and verify immediately
					var toUpdate vmv1beta1.VMCluster
					Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
					step.modify(&toUpdate)
					Expect(k8sClient.Update(ctx, &toUpdate)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &toUpdate, nsn)
					}, eventualExpandingTimeout).WithContext(ctx).Should(Succeed())
					var updated vmv1beta1.VMCluster
					Expect(k8sClient.Get(ctx, nsn, &updated)).To(Succeed())
					step.verify(&updated)
				}
			},
			Entry("configures vmstorage with MaxUnavailable 2", "maxunavailable-2-integer",
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](5),
							},
							PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
								Labels: map[string]string{"version": "old"},
							},
							RollingUpdateStrategyBehavior: &vmv1beta1.StatefulSetUpdateStrategyBehavior{
								MaxUnavailable: ptr.To(intstr.FromInt32(2)),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						By("triggering a rolling update")
						cr.Spec.VMStorage.PodMetadata.Labels["version"] = "new"
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						By("checking that update process runs as configured")
						Eventually(func() int {
							podList := &corev1.PodList{}
							k8sClient.List(ctx, podList, &client.ListOptions{
								Namespace:     namespace,
								LabelSelector: labels.SelectorFromSet(cr.VMStorageSelectorLabels()),
							})
							podsUpdated := 0
							podsUnavailable := 0
							for _, pod := range podList.Items {
								if pod.Status.Phase == corev1.PodRunning && pod.Labels["version"] == "new" {
									podsUpdated++
								}
								if pod.Status.Phase != corev1.PodRunning {
									podsUnavailable++
								}
							}
							Expect(podsUnavailable).To(BeNumerically("<=", 2), "no more than 2 pods should be unavailable during the update")
							return podsUpdated
						}, eventualStatefulsetAppReadyTimeout).Should(BeNumerically("==", 5))
					},
				},
			),
			Entry("configures vmstorage with MaxUnavailable 100%", "maxunavailable-100-percent",
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](5),
							},
							PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
								Labels: map[string]string{"version": "old"},
							},
							RollingUpdateStrategyBehavior: &vmv1beta1.StatefulSetUpdateStrategyBehavior{
								MaxUnavailable: ptr.To(intstr.FromString("100%")),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						By("triggering a rolling update")
						cr.Spec.VMStorage.PodMetadata.Labels["version"] = "new"
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						By("checking that update process runs as configured")
						Eventually(func() int {
							podList := &corev1.PodList{}
							k8sClient.List(ctx, podList, &client.ListOptions{
								Namespace:     namespace,
								LabelSelector: labels.SelectorFromSet(cr.VMStorageSelectorLabels()),
							})
							podsUpdated := 0
							podsOutdated := 0
							for _, pod := range podList.Items {
								if pod.Status.Phase == corev1.PodRunning && pod.Labels["version"] == "new" {
									podsUpdated++
								}
								if pod.Status.Phase == corev1.PodRunning && pod.Labels["version"] == "old" {
									podsOutdated++
								}
							}
							// consider the update is in progress if at least one pod is updated and running,
							// this is to avoid catching false positives due to async pod evictions/creations
							if podsUpdated >= 1 {
								Expect(podsOutdated).To(BeNumerically("==", 0), "no pods are running with old version when the update is in progress")
							}
							return podsUpdated
						}, eventualStatefulsetAppReadyTimeout).Should(BeNumerically("==", 5))
					},
				},
			),
			Entry("configures vmstorage with MaxUnavailable 100% but limited by a PDB", "maxunavailable-100-percent-pdb",
				&vmv1beta1.VMCluster{
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](5),
							},
							PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
								Labels: map[string]string{"version": "old"},
							},
							RollingUpdateStrategyBehavior: &vmv1beta1.StatefulSetUpdateStrategyBehavior{
								MaxUnavailable: ptr.To(intstr.FromString("100%")),
							},
							PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
								MinAvailable: ptr.To(intstr.FromInt32(4)),
							},
						},
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
				testStep{
					modify: func(cr *vmv1beta1.VMCluster) {
						By("triggering a rolling update")
						cr.Spec.VMStorage.PodMetadata.Labels["version"] = "new"
					},
					verify: func(cr *vmv1beta1.VMCluster) {
						By("checking that update process runs as configured")
						Eventually(func() int {
							podList := &corev1.PodList{}
							k8sClient.List(ctx, podList, &client.ListOptions{
								Namespace:     namespace,
								LabelSelector: labels.SelectorFromSet(cr.VMStorageSelectorLabels()),
							})
							podsUpdated := 0
							for _, pod := range podList.Items {
								if pod.Status.Phase == corev1.PodRunning && pod.Labels["version"] == "new" {
									podsUpdated++
								}
							}
							var pdb policyv1.PodDisruptionBudget
							k8sClient.Get(ctx, types.NamespacedName{
								Namespace: namespace,
								Name:      cr.GetVMStorageName(),
							}, &pdb)
							Expect(pdb.Status.CurrentHealthy).To(BeNumerically(">=", 4), "at least 4 pods should be healthy during the update")
							return podsUpdated
						}, eventualStatefulsetAppReadyTimeout).Should(BeNumerically("==", 5))
					},
				},
			),
		)
	})
})

func assertStrictSecurity(podSpec corev1.PodSpec) {
	Expect(podSpec.SecurityContext).NotTo((BeNil()))
	Expect(podSpec.SecurityContext).NotTo((BeNil()))
	Expect(podSpec.SecurityContext.RunAsNonRoot).NotTo(BeNil())
	Expect(*podSpec.SecurityContext.RunAsNonRoot).To(BeTrue())
	Expect(podSpec.SecurityContext.RunAsUser).NotTo(BeNil())
	Expect(*podSpec.SecurityContext.RunAsUser).To(Equal(int64(65534)))

	Expect(podSpec.Containers).To(HaveLen(1))
	for _, cnt := range podSpec.Containers {
		By(fmt.Sprintf("verifying container name=%q", cnt.Name))
		Expect(cnt.SecurityContext).NotTo(BeNil())
		Expect(cnt.SecurityContext.RunAsNonRoot).NotTo(BeNil())
		Expect(*cnt.SecurityContext.RunAsNonRoot).To(BeTrue())
		Expect(cnt.SecurityContext.RunAsUser).NotTo(BeNil())
		Expect(*cnt.SecurityContext.RunAsUser).To(Equal(int64(65534)))
		Expect(cnt.SecurityContext.Privileged).NotTo(BeNil())
		Expect(*cnt.SecurityContext.Privileged).To(BeFalse())

	}
}
