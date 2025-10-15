package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

var _ = Describe("e2e vmdistributedcluster", Label("vm", "vmdistributedcluster"), func() {
	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	var ctx context.Context
	namespacedName := types.NamespacedName{
		Namespace: namespace,
	}
	globalVMAuth := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "vm-auth",
		},
		Spec: vmv1beta1.VMAuthSpec{
			SelectAllByDefault: true,
		},
	}

	Context("create", func() {
		JustBeforeEach(func() {
			ctx = context.Background()
			k8sClient.Delete(ctx, globalVMAuth)
			Expect(k8sClient.Create(ctx, globalVMAuth)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
			})).To(Succeed(), "must delete vmdistributedcluster after test")
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      namespacedName.Name,
					Namespace: namespace,
				}, &vmv1alpha1.VMDistributedCluster{})
				if k8serrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("want NotFound error, got: %w", err)
			}, eventualDeletionTimeout, 1).WithContext(ctx).Should(Succeed())
			Expect(k8sClient.Delete(ctx, globalVMAuth)).To(Succeed(), "must delete vm-auth after test")
		})

		DescribeTable("should create vmdistributedcluster", func(cr *vmv1alpha1.VMDistributedCluster, vmclusters []vmv1beta1.VMCluster, verify func(cr *vmv1alpha1.VMDistributedCluster)) {
			for _, vmcluster := range vmclusters {
				Expect(k8sClient.Create(ctx, &vmcluster)).To(Succeed())
			}
			DeferCleanup(func() {
				for _, vmcluster := range vmclusters {
					Expect(k8sClient.Delete(ctx, &vmcluster)).To(Succeed())
				}
			})

			namespacedName.Name = cr.Name
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
			if verify != nil {
				var createdCluster vmv1alpha1.VMDistributedCluster
				Expect(k8sClient.Get(ctx, namespacedName, &createdCluster)).To(Succeed())
				verify(&createdCluster)
			}
		},
			Entry("with a single VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "single-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []corev1.LocalObjectReference{
						{
							Name: "vmcluster-1",
						},
					},
					VMAuth: corev1.LocalObjectReference{Name: globalVMAuth.Name},
				},
			}, []vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      "vmcluster-1",
					},
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
			}, func(cr *vmv1alpha1.VMDistributedCluster) {
				// Verify that the status contains expected generations
				Expect(len(cr.Status.VMClusterGenerations)).To(Equal(1))
				Expect(cr.Status.VMClusterGenerations[0].VMClusterName).To(Equal("vmcluster-1"))
			}),
			Entry("with multiple VMClusters", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "multi-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []corev1.LocalObjectReference{
						{
							Name: "vmcluster-1",
						},
						{
							Name: "vmcluster-2",
						},
					},
					VMAuth: corev1.LocalObjectReference{Name: globalVMAuth.Name},
				},
			}, []vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      "vmcluster-1",
					},
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      "vmcluster-2",
					},
					Spec: vmv1beta1.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
			}, func(cr *vmv1alpha1.VMDistributedCluster) {
				// Verify that the status contains expected generations
				Expect(len(cr.Status.VMClusterGenerations)).To(Equal(2))
				names := []string{
					cr.Status.VMClusterGenerations[0].VMClusterName,
					cr.Status.VMClusterGenerations[1].VMClusterName,
				}
				Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))
			}),
		)
	})

	It("should delete VMDistributedCluster and remove it from the cluster", func() {
		name := "delete-test"
		namespacedName.Name = name
		cr := &vmv1alpha1.VMDistributedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: vmv1alpha1.VMDistributedClusterSpec{
				VMClusters: []corev1.LocalObjectReference{
					{
						Name: "vmcluster-1",
					},
				},
				VMAuth: corev1.LocalObjectReference{Name: globalVMAuth.Name},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		Eventually(func() error {
			return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
		}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		Eventually(func() error {
			err := k8sClient.Get(ctx, namespacedName, &vmv1alpha1.VMDistributedCluster{})
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("want NotFound error, got: %w", err)
		}, eventualDeletionTimeout, 1).WithContext(ctx).Should(Succeed())
	})
})
