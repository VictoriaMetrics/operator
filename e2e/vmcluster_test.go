package e2e

import (
	"context"
	"fmt"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

var _ = Describe("e2e vmcluster", func() {
	Context("crud", func() {
		Context("create", func() {
			name := "create-cluster"
			namespace := "default"
			AfterEach(func() {
				Expect(k8sClient.Delete(context.TODO(), &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					},
				})).To(Succeed(), "must delete vmcluster after test")
				Eventually(func() error {
					err := k8sClient.Get(context.Background(), types.NamespacedName{
						Name:      name,
						Namespace: namespace,
					}, &v1beta1vm.VMCluster{})
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("want NotFound error, got: %w", err)
				}, 60, 1).Should(BeNil())
			})
			It("should create vmCluster with empty services", func() {
				Expect(k8sClient.Create(context.TODO(), &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					},
					Spec: v1beta1vm.VMClusterSpec{RetentionPeriod: "1"},
				})).To(Succeed())
			})

		})
		Context("update", func() {
			name := "update-cluster"
			namespace := "default"
			AfterEach(func() {
				Expect(k8sClient.Delete(context.TODO(), &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					},
				})).To(Succeed())
				Eventually(func() error {
					err := k8sClient.Get(context.Background(), types.NamespacedName{
						Name:      name,
						Namespace: namespace,
					}, &v1beta1vm.VMCluster{})
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("want NotFound error, got: %w", err)
				}, 60, 1).Should(BeNil())
			})
			BeforeEach(func() {
				Expect(k8sClient.Create(context.TODO(), &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      name,
					},
					Spec: v1beta1vm.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &v1beta1vm.VMStorage{
							ReplicaCount: pointer.Int32Ptr(1),
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
									corev1.ResourceCPU:    resource.MustParse("50m"),
								},
							},
						},
						VMSelect: &v1beta1vm.VMSelect{
							ReplicaCount: pointer.Int32Ptr(1),
							LogLevel:     "WARN",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
									corev1.ResourceCPU:    resource.MustParse("50m"),
								},
							},
						},
					},
				})).To(Succeed())
				vmCluster := &v1beta1vm.VMCluster{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, vmCluster)).To(Succeed())
				Eventually(func() string {
					return expectPodCount(k8sClient, 1, namespace, vmCluster.VMStorageSelectorLabels())
				}, 30, 1).Should(BeEmpty(), "vmCluster must have ready storage pod")
				Eventually(func() string {
					return expectPodCount(k8sClient, 1, namespace, vmCluster.VMSelectSelectorLabels())
				}, 30, 1).Should(BeEmpty(), "vmCluster must have ready select pod")
			})
			It("should expand vmCluster storage and select up to 2 replicas", func() {
				vmCluster := &v1beta1vm.VMCluster{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, vmCluster)).To(BeNil())
				vmCluster.Spec.VMSelect.ReplicaCount = pointer.Int32Ptr(2)
				vmCluster.Spec.VMStorage.ReplicaCount = pointer.Int32Ptr(2)
				Expect(k8sClient.Update(context.TODO(), vmCluster)).To(BeNil())
				Eventually(func() string {
					return expectPodCount(k8sClient, 2, namespace, vmCluster.VMStorageSelectorLabels())
				}, 40, 1).Should(BeEmpty())
				Eventually(func() string {
					return expectPodCount(k8sClient, 2, namespace, vmCluster.VMSelectSelectorLabels())
				}, 40, 1).Should(BeEmpty())

				By("vmInsert expand, expect to have 2 vmInserts")
				vmCluster = &v1beta1vm.VMCluster{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, vmCluster)).To(BeNil())
				vmCluster.Spec.VMInsert = &v1beta1vm.VMInsert{
					ReplicaCount: pointer.Int32Ptr(2),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
							corev1.ResourceCPU:    resource.MustParse("50m"),
						},
					},
				}
				Expect(k8sClient.Update(context.TODO(), vmCluster)).To(BeNil())
				Eventually(func() string {
					return expectPodCount(k8sClient, 2, namespace, vmCluster.VMInsertSelectorLabels())
				}, 70, 1).Should(BeEmpty())

				By("vmInsert revisionHistoryLimit update to 2")
				vmCluster = &v1beta1vm.VMCluster{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, vmCluster)).To(BeNil())
				namespacedName := types.NamespacedName{Name: fmt.Sprintf("vminsert-%s", name), Namespace: namespace}
				Eventually(func() int32 {
					return getRevisionHistoryLimit(k8sClient, namespacedName)
				}, 60).Should(Equal(int32(10)))
				vmCluster.Spec.VMInsert = &v1beta1vm.VMInsert{
					ReplicaCount:              pointer.Int32Ptr(2),
					RevisionHistoryLimitCount: pointer.Int32Ptr(2),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
							corev1.ResourceCPU:    resource.MustParse("50m"),
						},
					},
				}
				Expect(k8sClient.Update(context.TODO(), vmCluster)).To(BeNil())
				Eventually(func() int32 {
					return getRevisionHistoryLimit(k8sClient, namespacedName)
				}, 60).Should(Equal(int32(2)))
			})
		})
	})
})
