package e2e

import (
	"context"
	"fmt"
	"time"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

const (
	interval = 15 * time.Second
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
							ReplicaCount: ptr.To[int32](1),
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("128Mi"),
									corev1.ResourceCPU:    resource.MustParse("50m"),
								},
							},
						},
						VMSelect: &v1beta1vm.VMSelect{
							ReplicaCount: ptr.To[int32](1),
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
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				}, vmCluster)).To(Succeed())
				Eventually(func() string {
					return expectPodCount(k8sClient, 1, namespace, vmCluster.VMStorageSelectorLabels())
				}, 300, 10).Should(BeEmpty(), "vmCluster must have ready storage pod")
				Eventually(func() string {
					return expectPodCount(k8sClient, 1, namespace, vmCluster.VMSelectSelectorLabels())
				}, 300, 10).Should(BeEmpty(), "vmCluster must have ready select pod")
			})
			It("should expand vmCluster storage and select up to 2 replicas", func() {
				vmCluster := &v1beta1vm.VMCluster{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				}, vmCluster)).To(Succeed())
				vmCluster.Spec.VMSelect.ReplicaCount = ptr.To[int32](2)
				vmCluster.Spec.VMStorage.ReplicaCount = ptr.To[int32](2)
				Expect(k8sClient.Update(context.TODO(), vmCluster)).To(Succeed())
				Eventually(func() string {
					return expectPodCount(k8sClient, 2, namespace, vmCluster.VMStorageSelectorLabels())
				}, 40, 1).Should(BeEmpty())
				Eventually(func() string {
					return expectPodCount(k8sClient, 2, namespace, vmCluster.VMSelectSelectorLabels())
				}, 40, 1).Should(BeEmpty())

				By("vmInsert expand, expect to have 2 vmInserts")
				vmCluster = &v1beta1vm.VMCluster{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				}, vmCluster)).To(Succeed())
				vmCluster.Spec.VMInsert = &v1beta1vm.VMInsert{
					ReplicaCount: ptr.To[int32](2),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
							corev1.ResourceCPU:    resource.MustParse("50m"),
						},
					},
				}
				Expect(k8sClient.Update(context.TODO(), vmCluster)).To(Succeed())
				Eventually(func() string {
					return expectPodCount(k8sClient, 2, namespace, vmCluster.VMInsertSelectorLabels())
				}, 70, 1).Should(BeEmpty())

				By("vmInsert revisionHistoryLimit update to 2")
				vmCluster = &v1beta1vm.VMCluster{}
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				}, vmCluster)).To(Succeed())
				namespacedName := types.NamespacedName{Name: fmt.Sprintf("vminsert-%s", name), Namespace: namespace}
				Eventually(func() int32 {
					return getRevisionHistoryLimit(k8sClient, namespacedName)
				}, 60).Should(Equal(int32(10)))
				vmCluster.Spec.VMInsert = &v1beta1vm.VMInsert{
					ReplicaCount:              ptr.To[int32](2),
					RevisionHistoryLimitCount: ptr.To[int32](2),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
							corev1.ResourceCPU:    resource.MustParse("50m"),
						},
					},
				}
				Expect(k8sClient.Update(context.TODO(), vmCluster)).To(Succeed())
				Eventually(func() int32 {
					return getRevisionHistoryLimit(k8sClient, namespacedName)
				}, 60).Should(Equal(int32(2)))
			})
			It("Should set pause status and skip other update operation", func() {
				vmCluster := &v1beta1vm.VMCluster{}
				key := types.NamespacedName{Name: name, Namespace: namespace}
				Expect(k8sClient.Get(context.TODO(), key, vmCluster)).To(Succeed())

				By("Set spec.Paused to true")
				vmCluster.Spec.Paused = true
				Expect(k8sClient.Update(context.TODO(), vmCluster)).To(Succeed())
				Eventually(func() bool {
					if err := k8sClient.Get(context.TODO(), key, vmCluster); err != nil {
						return false
					}
					return vmCluster.Paused() && vmCluster.Status.UpdateStatus == v1beta1vm.UpdateStatusPaused
				}, 1*time.Minute, interval).Should(BeTrue())

				By("update replicas after cluster paused")
				vmCluster.Spec.VMSelect.ReplicaCount = ptr.To[int32](2)
				Expect(k8sClient.Update(context.TODO(), vmCluster)).To(Succeed())
				// simply sleep 1 minute to ensure reconciliation performed.
				time.Sleep(1 * time.Minute)
				Expect(expectPodCount(k8sClient, 1, namespace, vmCluster.VMSelectPodLabels())).To(BeEmpty())
			})

		})
	})
})
