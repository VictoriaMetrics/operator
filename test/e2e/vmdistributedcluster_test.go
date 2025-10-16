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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

var _ = Describe("e2e vmdistributedcluster", Label("vm", "vmdistributedcluster"), func() {
	var ctx context.Context

	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	namespacedName := types.NamespacedName{
		Namespace: namespace,
	}
	managedVMAuthName := types.NamespacedName{Name: "managed-vm-auth", Namespace: namespace}
	unmanagedVMAuthName := types.NamespacedName{Name: "unmanaged-vm-auth", Namespace: namespace}

	// makeClusterReady := func(nsn types.NamespacedName) {
	// 	time.Sleep(100 * time.Millisecond)
	// 	Eventually(func() error {
	// 		var obj vmv1beta1.VMCluster
	// 		if err := k8sClient.Get(ctx, nsn, &obj); err != nil {
	// 			return err
	// 		}
	// 		obj.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
	// 		return k8sClient.Status().Update(ctx, &obj)
	// 	}, 2*time.Second, 50*time.Millisecond).Should(Succeed())
	// }

	beforeEach := func() {
		ctx = context.Background()

		var managedVMAuth vmv1beta1.VMAuth
		var unmanagedVMAuth vmv1beta1.VMAuth

		err := k8sClient.Get(ctx, managedVMAuthName, &managedVMAuth)
		if k8serrors.IsNotFound(err) {
			managedVMAuth = vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managed-vm-auth",
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
				},
			}
			Expect(k8sClient.Create(ctx, &managedVMAuth)).To(Succeed(), "must create managed vm-auth before test")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAuth{}, managedVMAuthName)
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}

		err = k8sClient.Get(ctx, unmanagedVMAuthName, &unmanagedVMAuth)
		if k8serrors.IsNotFound(err) {
			unmanagedVMAuth = vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unmanaged-vm-auth",
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: false,
				},
			}
			Expect(k8sClient.Create(ctx, &unmanagedVMAuth)).To(Succeed(), "must create unmanaged vm-auth before test")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAuth{}, unmanagedVMAuthName)
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
	}

	afterEach := func() {
		Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{
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

		var vmAuth vmv1beta1.VMAuth
		for _, vmauthName := range []types.NamespacedName{managedVMAuthName, managedVMAuthName} {
			err := k8sClient.Get(ctx, vmauthName, &vmAuth)
			if k8serrors.IsNotFound(err) {
				continue
			}
			Expect(err).To(Succeed(), "must get vm-auth after test")
			Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmAuth)).To(Succeed(), "must delete vm-auth after test")
			Eventually(func() error {
				return k8sClient.Get(ctx, namespacedName, &vmv1beta1.VMAuth{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		}
	}

	Context("create", func() {
		DescribeTable("should create vmdistributedcluster", func(cr *vmv1alpha1.VMDistributedCluster, vmclusters []vmv1beta1.VMCluster, verify func(cr *vmv1alpha1.VMDistributedCluster)) {
			beforeEach()
			DeferCleanup(func() {
				for _, vmcluster := range vmclusters {
					finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmcluster)
					Eventually(func() error {
						return k8sClient.Get(ctx, namespacedName, &vmv1beta1.VMCluster{})
					}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
				}
				afterEach()
			})
			for _, vmcluster := range vmclusters {
				Expect(k8sClient.Create(ctx, &vmcluster)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMCluster{}, types.NamespacedName{Name: vmcluster.Name, Namespace: namespace})
				}, eventualDeploymentAppReadyTimeout).Should(Succeed())
			}

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
					VMAuth: corev1.LocalObjectReference{Name: managedVMAuthName.Name},
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
					VMAuth: corev1.LocalObjectReference{Name: managedVMAuthName.Name},
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
						RetentionPeriod: "2",
						VMStorage: &vmv1beta1.VMStorage{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				},
			}, func(cr *vmv1alpha1.VMDistributedCluster) {
				Expect(len(cr.Status.VMClusterGenerations)).To(Equal(2))
				names := []string{
					cr.Status.VMClusterGenerations[0].VMClusterName,
					cr.Status.VMClusterGenerations[1].VMClusterName,
				}
				Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))
			}),
		)
		It("should wait for VMCluster upgrade completion", func() {
			beforeEach()
			DeferCleanup(afterEach)

			vmCluster1 := &vmv1beta1.VMCluster{
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
			}
			vmCluster2 := &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vmcluster-2",
				},
				Spec: vmv1beta1.VMClusterSpec{
					RetentionPeriod: "2",
					VMStorage: &vmv1beta1.VMStorage{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, vmCluster1)).To(Succeed())
			Expect(k8sClient.Create(ctx, vmCluster2)).To(Succeed())
			DeferCleanup(func() {
				Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, vmCluster1)).To(Succeed())
				Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, vmCluster2)).To(Succeed())
			})

			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMCluster{}, types.NamespacedName{Name: vmCluster1.Name, Namespace: namespace})
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMCluster{}, types.NamespacedName{Name: vmCluster2.Name, Namespace: namespace})
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())

			namespacedName.Name = "distributed-upgrade"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []corev1.LocalObjectReference{
						{Name: vmCluster1.Name},
						{Name: vmCluster2.Name},
					},
					VMAuth: corev1.LocalObjectReference{Name: managedVMAuthName.Name},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			// Start upgrade by changing ClusterVersion
			Eventually(func() error {
				var obj vmv1alpha1.VMDistributedCluster
				if err := k8sClient.Get(ctx, namespacedName, &obj); err != nil {
					return err
				}
				obj.Spec.ClusterVersion = "v1.1.0"
				return k8sClient.Update(ctx, &obj)
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())

			// Wait for VMDistributedCluster to become operational after its own upgrade
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			// Verify VMDistributedCluster status reflects both clusters are upgraded/operational
			var upgradedCluster vmv1alpha1.VMDistributedCluster
			Expect(k8sClient.Get(ctx, namespacedName, &upgradedCluster)).To(Succeed())
			Expect(len(upgradedCluster.Status.VMClusterGenerations)).To(Equal(2))
			names := []string{
				upgradedCluster.Status.VMClusterGenerations[0].VMClusterName,
				upgradedCluster.Status.VMClusterGenerations[1].VMClusterName,
			}
			Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))

			// Verify both clusters have desired version set
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster1.Name, Namespace: namespace}, vmCluster1)).To(Succeed())
			Expect(vmCluster1.Spec.ClusterVersion).To(Equal("v1.1.0"))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster2.Name, Namespace: namespace}, vmCluster2)).To(Succeed())
			Expect(vmCluster2.Spec.ClusterVersion).To(Equal("v1.1.0"))
		})
	})

	Context("fail", func() {
		DescribeTable("should fail when creating vmdistributedcluster", func(cr *vmv1alpha1.VMDistributedCluster, vmclusters []vmv1beta1.VMCluster) {
			beforeEach()
			DeferCleanup(func() {
				for _, vmcluster := range vmclusters {
					finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmcluster)
					Eventually(func() error {
						return k8sClient.Get(ctx, namespacedName, &vmv1beta1.VMCluster{})
					}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
				}
				afterEach()
			})
			for _, vmcluster := range vmclusters {
				Expect(k8sClient.Create(ctx, &vmcluster)).To(Succeed())
			}

			namespacedName.Name = cr.Name
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName, vmv1beta1.UpdateStatusFailed)
			}, eventualDeletionTimeout).Should(Succeed())
		},
			Entry("with invalid VMAuth", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "no-vmauth-set",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []corev1.LocalObjectReference{},
					VMAuth:     corev1.LocalObjectReference{Name: "missing-vmauth"},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with unmanaged VMAuth", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "unmanaged-vmauth",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []corev1.LocalObjectReference{},
					VMAuth:     corev1.LocalObjectReference{Name: unmanagedVMAuthName.Name},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with invalid VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []corev1.LocalObjectReference{
						{
							Name: "vmcluster-missing",
						},
					},
					VMAuth: corev1.LocalObjectReference{Name: managedVMAuthName.Name},
				},
			}, []vmv1beta1.VMCluster{}),
		)
	})

	It("should delete VMDistributedCluster and remove it from the cluster", func() {
		beforeEach()
		DeferCleanup(afterEach)

		vmCluster := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "vmcluster-1",
			},
		}
		Expect(k8sClient.Create(ctx, vmCluster)).To(Succeed())
		DeferCleanup(func() {
			Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, vmCluster)).To(Succeed())
		})

		namespacedName.Name = "vmcluster"
		cr := &vmv1alpha1.VMDistributedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      namespacedName.Name,
			},
			Spec: vmv1alpha1.VMDistributedClusterSpec{
				VMClusters: []corev1.LocalObjectReference{
					{
						Name: vmCluster.Name,
					},
				},
				VMAuth: corev1.LocalObjectReference{Name: managedVMAuthName.Name},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		Eventually(func() error {
			return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, types.NamespacedName{Name: namespacedName.Name, Namespace: namespace})
		}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

		Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, cr)).To(Succeed())
		Eventually(func() error {
			err := k8sClient.Get(ctx, namespacedName, &vmv1alpha1.VMDistributedCluster{})
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("want NotFound error, got: %w", err)
		}, eventualDeletionTimeout, 1).WithContext(ctx).Should(Succeed())
	})
})
