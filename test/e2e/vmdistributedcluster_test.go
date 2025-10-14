package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

	Context("create", func() {
		JustBeforeEach(func() {
			ctx = context.Background()
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
		})

		DescribeTable("should create vmdistributedcluster", func(name string, cr *vmv1alpha1.VMDistributedCluster, verify func(cr *vmv1alpha1.VMDistributedCluster)) {
			namespacedName.Name = name
			cr.Name = name
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
			Entry("with a single VMCluster", "single-cluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "single-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []vmv1beta1.VMCluster{
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
					},
				},
			}, func(cr *vmv1alpha1.VMDistributedCluster) {
				// Verify that the status contains expected generations
				Expect(len(cr.Status.VMClusterGenerations)).To(Equal(1))
				Expect(cr.Status.VMClusterGenerations[0].VMClusterName).To(Equal("vmcluster-1"))
			}),
			Entry("with multiple VMClusters", "multi-cluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "multi-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []vmv1beta1.VMCluster{
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
								RetentionPeriod: "2",
								VMStorage: &vmv1beta1.VMStorage{
									CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](2),
									},
								},
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
})
