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
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

func createVMClustersAndUpdateTargetRefs(
	ctx context.Context,
	k8sClient client.Client,
	clusters []vmv1beta1.VMCluster,
	ns string,
	vmUserName types.NamespacedName,
) []vmv1beta1.TargetRef {
	refs := make([]vmv1beta1.TargetRef, len(clusters))
	for i, vmcluster := range clusters {
		Expect(k8sClient.Create(ctx, &vmcluster)).To(Succeed())
		Eventually(func() error {
			return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMCluster{}, types.NamespacedName{Name: vmcluster.Name, Namespace: ns})
		}, eventualStatefulsetAppReadyTimeout).Should(Succeed())

		refs[i] = vmv1beta1.TargetRef{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      vmcluster.Name,
				Namespace: ns,
			},
			TargetPathSuffix: "/select/1",
		}
	}

	var validVMUser vmv1beta1.VMUser
	Expect(k8sClient.Get(ctx, vmUserName, &validVMUser)).To(Succeed())
	validVMUser.Spec.TargetRefs = refs
	Expect(k8sClient.Update(ctx, &validVMUser)).To(Succeed())
	return refs
}

var _ = Describe("e2e vmdistributedcluster", Label("vm", "vmdistributedcluster"), func() {
	var ctx context.Context

	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	namespacedName := types.NamespacedName{
		Namespace: namespace,
	}
	validVMUserName := types.NamespacedName{Name: "valid-vm-user", Namespace: namespace}
	invalidVMUserName := types.NamespacedName{Name: "invalid-vm-user", Namespace: namespace}

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

		var validVMUser vmv1beta1.VMUser
		var invalidVMUser vmv1beta1.VMUser

		err := k8sClient.Get(ctx, validVMUserName, &validVMUser)
		if k8serrors.IsNotFound(err) {
			validVMUser = vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "valid-vm-user",
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMUserSpec{
					TargetRefs: []vmv1beta1.TargetRef{},
				},
			}
			Expect(k8sClient.Create(ctx, &validVMUser)).To(Succeed(), "must create managed vm-user before test")
		} else {
			Expect(err).ToNot(HaveOccurred())
		}

		err = k8sClient.Get(ctx, invalidVMUserName, &invalidVMUser)
		if k8serrors.IsNotFound(err) {
			invalidVMUser = vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-vm-user",
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMUserSpec{
					TargetRefs: []vmv1beta1.TargetRef{},
				},
			}
			Expect(k8sClient.Create(ctx, &invalidVMUser)).To(Succeed(), "must create unmanaged vm-user before test")
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

		var vmUser vmv1beta1.VMUser
		for _, vmuserName := range []types.NamespacedName{validVMUserName, validVMUserName} {
			err := k8sClient.Get(ctx, vmuserName, &vmUser)
			if k8serrors.IsNotFound(err) {
				continue
			}
			Expect(err).To(Succeed(), "must get vm-user after test")
			Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmUser)).To(Succeed(), "must delete vm-user after test")
			Eventually(func() error {
				return k8sClient.Get(ctx, namespacedName, &vmv1beta1.VMUser{})
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

			createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserName)

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
					VMClusters: []vmv1alpha1.VMClusterAgentPair{
						{LocalObjectReference: corev1.LocalObjectReference{
							Name: "vmcluster-1",
						}},
					},
					VMUser: corev1.LocalObjectReference{Name: validVMUserName.Name},
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
				Expect(len(cr.Status.VMClusterInfo)).To(Equal(1))
				Expect(cr.Status.VMClusterInfo[0].VMClusterName).To(Equal("vmcluster-1"))
			}),
			Entry("with multiple VMClusters", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "multi-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []vmv1alpha1.VMClusterAgentPair{
						{LocalObjectReference: corev1.LocalObjectReference{
							Name: "vmcluster-1",
						}},
						{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "vmcluster-2",
							},
						},
					},
					VMUser: corev1.LocalObjectReference{Name: validVMUserName.Name},
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
				Expect(len(cr.Status.VMClusterInfo)).To(Equal(2))
				names := []string{
					cr.Status.VMClusterInfo[0].VMClusterName,
					cr.Status.VMClusterInfo[1].VMClusterName,
				}
				Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))
			}),
		)
		It("should wait for VMCluster upgrade completion", func() {
			beforeEach()
			DeferCleanup(afterEach)

			initialVersion := "v1.126.0-cluster"
			updateVersion := "v1.127.0-cluster"

			vmCluster1 := &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vmcluster-1",
				},
				Spec: vmv1beta1.VMClusterSpec{
					ClusterVersion: initialVersion,
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
					ClusterVersion: initialVersion,
					VMStorage: &vmv1beta1.VMStorage{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
			}
			vmclusters := []vmv1beta1.VMCluster{*vmCluster1, *vmCluster2}
			DeferCleanup(func() {
				for _, vmcluster := range vmclusters {
					Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmcluster)).To(Succeed())
				}
			})

			createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserName)

			namespacedName.Name = "distributed-upgrade"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []vmv1alpha1.VMClusterAgentPair{
						{LocalObjectReference: corev1.LocalObjectReference{
							Name: vmCluster1.Name,
						}},
						{LocalObjectReference: corev1.LocalObjectReference{
							Name: vmCluster2.Name,
						}},
					},
					VMUser: corev1.LocalObjectReference{Name: validVMUserName.Name},
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
				obj.Spec.ClusterVersion = updateVersion
				return k8sClient.Update(ctx, &obj)
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())

			// Wait for VMDistributedCluster to become operational after its own upgrade
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			// Verify VMDistributedCluster status reflects both clusters are upgraded/operational
			var upgradedCluster vmv1alpha1.VMDistributedCluster
			Expect(k8sClient.Get(ctx, namespacedName, &upgradedCluster)).To(Succeed())
			Expect(len(upgradedCluster.Status.VMClusterInfo)).To(Equal(2))
			names := []string{
				upgradedCluster.Status.VMClusterInfo[0].VMClusterName,
				upgradedCluster.Status.VMClusterInfo[1].VMClusterName,
			}
			Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))

			// Verify both clusters have desired version set
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster1.Name, Namespace: namespace}, vmCluster1)).To(Succeed())
			Expect(vmCluster1.Spec.ClusterVersion).To(Equal(updateVersion))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster2.Name, Namespace: namespace}, vmCluster2)).To(Succeed())
			Expect(vmCluster2.Spec.ClusterVersion).To(Equal(updateVersion))
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
			Entry("with no VMUser set", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "no-vmuser-set",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []vmv1alpha1.VMClusterAgentPair{},
					VMUser:     corev1.LocalObjectReference{Name: "missing-vmuser"},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with invalid VMUser", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "invalid-vmuser",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []vmv1alpha1.VMClusterAgentPair{
						{LocalObjectReference: corev1.LocalObjectReference{
							Name: "vmcluster-1",
						}},
					},
					VMUser: corev1.LocalObjectReference{Name: invalidVMUserName.Name},
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
			}),
			Entry("with invalid VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMClusters: []vmv1alpha1.VMClusterAgentPair{
						{LocalObjectReference: corev1.LocalObjectReference{
							Name: "vmcluster-missing",
						}},
					},
					VMUser: corev1.LocalObjectReference{Name: validVMUserName.Name},
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
		vmclusters := []vmv1beta1.VMCluster{*vmCluster}

		DeferCleanup(func() {
			Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, vmCluster)).To(Succeed())
		})
		createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserName)

		namespacedName.Name = "vmcluster"
		cr := &vmv1alpha1.VMDistributedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      namespacedName.Name,
			},
			Spec: vmv1alpha1.VMDistributedClusterSpec{
				VMClusters: []vmv1alpha1.VMClusterAgentPair{
					{LocalObjectReference: corev1.LocalObjectReference{
						Name: vmCluster.Name,
					}},
				},
				VMUser: corev1.LocalObjectReference{Name: validVMUserName.Name},
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
