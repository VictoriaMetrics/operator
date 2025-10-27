package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
	vmUserNames []types.NamespacedName,
) {
	refs := make([]vmv1beta1.TargetRef, len(clusters))
	for i, vmcluster := range clusters {
		Expect(k8sClient.Create(ctx, &vmcluster)).To(Succeed())
		Eventually(func() error {
			return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMCluster{}, types.NamespacedName{Name: vmcluster.Name, Namespace: ns})
		}, eventualStatefulsetAppReadyTimeout).Should(Succeed())

		refs[i] = vmv1beta1.TargetRef{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      clusters[i].Name,
				Namespace: ns,
			},
			TargetPathSuffix: "/select/1",
		}
	}

	// Update all VMUsers with the target refs
	for _, vmUserName := range vmUserNames {
		var validVMUser vmv1beta1.VMUser
		Expect(k8sClient.Get(ctx, vmUserName, &validVMUser)).To(Succeed())
		validVMUser.Spec.TargetRefs = refs
		Expect(k8sClient.Update(ctx, &validVMUser)).To(Succeed())
	}
}

var _ = Describe("e2e vmdistributedcluster", Label("vm", "vmdistributedcluster"), func() {
	var ctx context.Context

	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	namespacedName := types.NamespacedName{
		Namespace: namespace,
	}
	validVMUserNames := []types.NamespacedName{
		{Name: "valid-vm-user-1", Namespace: namespace},
		{Name: "valid-vm-user-2", Namespace: namespace},
	}
	invalidVMUserName := types.NamespacedName{Name: "invalid-vm-user", Namespace: namespace}
	validVMAgentName := types.NamespacedName{Name: "valid-vm-agent", Namespace: namespace}

	beforeEach := func() {
		ctx = context.Background()

		var validVMUser vmv1beta1.VMUser
		var invalidVMUser vmv1beta1.VMUser
		var validVMAgent vmv1beta1.VMAgent

		// Create valid VMUsers
		for _, validVMUserName := range validVMUserNames {
			err := k8sClient.Get(ctx, validVMUserName, &validVMUser)
			if k8serrors.IsNotFound(err) {
				validVMUser = vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      validVMUserName.Name,
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
		}

		// Create invalid VMUser
		err := k8sClient.Get(ctx, invalidVMUserName, &invalidVMUser)
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

		// Create valid VMAgent
		err = k8sClient.Get(ctx, validVMAgentName, &validVMAgent)
		if k8serrors.IsNotFound(err) {
			validVMAgent = vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      validVMAgentName.Name,
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMAgentSpec{
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "http://vminsert.monitoring:8089/api/v1/write", // Dummy URL to satisfy validation
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &validVMAgent)).To(Succeed(), "must create managed vm-agent before test")
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
		for _, vmuserName := range validVMUserNames {
			err := k8sClient.Get(ctx, vmuserName, &vmUser)
			if k8serrors.IsNotFound(err) {
				continue
			}
			Expect(err).To(Succeed(), "must get vm-user after test")
			Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmUser)).To(Succeed(), "must delete vm-user after test")
			Eventually(func() error {
				return k8sClient.Get(ctx, vmuserName, &vmv1beta1.VMUser{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		}

		// Clean up VMAgent
		var vmAgent vmv1beta1.VMAgent
		err := k8sClient.Get(ctx, validVMAgentName, &vmAgent)
		if k8serrors.IsNotFound(err) {
			return
		}
		Expect(err).To(Succeed(), "must get vm-agent after test")
		Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmAgent)).To(Succeed(), "must delete vm-agent after test")
		Eventually(func() error {
			return k8sClient.Get(ctx, validVMAgentName, &vmv1beta1.VMAgent{})
		}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
	}

	Context("create", func() {
		DescribeTable("should create vmdistributedcluster with VMAgent", func(cr *vmv1alpha1.VMDistributedCluster, vmclusters []vmv1beta1.VMCluster, vmagents map[string]*vmv1beta1.VMAgent, verify func(cr *vmv1alpha1.VMDistributedCluster)) {
			beforeEach()
			DeferCleanup(func() {
				for _, vmcluster := range vmclusters {
					finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmcluster)
					Eventually(func() error {
						return k8sClient.Get(ctx, namespacedName, &vmv1beta1.VMCluster{})
					}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
				}
				for _, vmagent := range vmagents {
					finalize.SafeDeleteWithFinalizer(ctx, k8sClient, vmagent)
					Eventually(func() error {
						return k8sClient.Get(ctx, namespacedName, &vmv1beta1.VMAgent{})
					}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
				}
				afterEach()
			})

			createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserNames)

			// Create VMAgents
			for _, vmagent := range vmagents {
				Expect(k8sClient.Create(ctx, vmagent)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, types.NamespacedName{Name: vmagent.Name, Namespace: vmagent.Namespace})
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
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
			Entry("with single VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "single-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
						{Name: validVMUserNames[1].Name},
					},
					Zones: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{
								Name: "vmcluster-1",
							},
						},
					},
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
			}, map[string]*vmv1beta1.VMAgent{
				"vmagent-1": {
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      "vmagent-1",
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://localhost:8428/api/v1/write"},
						},
					},
				},
			}, func(cr *vmv1alpha1.VMDistributedCluster) {
				Expect(cr.Status.VMClusterInfo).To(HaveLen(1))
				Expect(cr.Status.VMClusterInfo[0].VMClusterName).To(Equal("vmcluster-1"))
			}),
			Entry("with multiple VMClusters and VMAgent pairs", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "multi-cluster-agent",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					Zones: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{
								Name: "vmcluster-1",
							},
						},
						{
							Ref: &corev1.LocalObjectReference{
								Name: "vmcluster-2",
							},
						},
					},
					VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
						{Name: validVMUserNames[1].Name},
					},
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
			}, map[string]*vmv1beta1.VMAgent{
				"vmagent-1": {
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      "vmagent-1",
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://localhost:8428/api/v1/write"},
						},
					},
				},
				"vmagent-2": {
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      "vmagent-2",
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://localhost:8428/api/v1/write"},
						},
					},
				},
			}, func(cr *vmv1alpha1.VMDistributedCluster) {
				Expect(cr.Status.VMClusterInfo).To(HaveLen(2))
				names := []string{
					cr.Status.VMClusterInfo[0].VMClusterName,
					cr.Status.VMClusterInfo[1].VMClusterName,
				}
				Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))
			}),
			Entry("with mixed VMClusters (some with VMAgent, some without)", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "mixed-cluster-agent",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					Zones: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{
								Name: "vmcluster-1",
							},
						},
						{
							Ref: &corev1.LocalObjectReference{
								Name: "vmcluster-2",
							},
						},
					},
					VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{
							Name: validVMUserNames[0].Name,
						},
						{
							Name: validVMUserNames[1].Name,
						},
					},
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
			}, map[string]*vmv1beta1.VMAgent{
				"vmagent-1": {
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      "vmagent-1",
					},
					Spec: vmv1beta1.VMAgentSpec{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
							{URL: "http://localhost:8428/api/v1/write"},
						},
					},
				},
			}, func(cr *vmv1alpha1.VMDistributedCluster) {
				Expect(cr.Status.VMClusterInfo).To(HaveLen(2))
				names := []string{
					cr.Status.VMClusterInfo[0].VMClusterName,
					cr.Status.VMClusterInfo[1].VMClusterName,
				}
				Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))
			}),
		)

		It("should wait for VMCluster upgrade completion", func() {
			beforeEach()

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

			createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserNames)

			namespacedName.Name = "distributed-upgrade"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
						{Name: validVMUserNames[1].Name},
					},
					Zones: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{
							Name: vmCluster1.Name,
						}},
						{Ref: &corev1.LocalObjectReference{
							Name: vmCluster2.Name,
						}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			// 	// Start upgrade by changing ClusterVersion
			Eventually(func() error {
				var obj vmv1alpha1.VMDistributedCluster
				if err := k8sClient.Get(ctx, namespacedName, &obj); err != nil {
					return err
				}
				// Apply spec update
				obj.Spec.Zones[0].OverrideSpec = &apiextensionsv1.JSON{
					Raw: []byte(fmt.Sprintf(`{"clusterVersion": "%s"}`, updateVersion)),
				}
				return k8sClient.Update(ctx, &obj)
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())

			// Wait for VMDistributedCluster to become operational after its own upgrade
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			// Verify VMDistributedCluster status reflects both clusters are upgraded/operational
			var upgradedCluster vmv1alpha1.VMDistributedCluster
			Expect(k8sClient.Get(ctx, namespacedName, &upgradedCluster)).To(Succeed())
			Expect(upgradedCluster.Status.VMClusterInfo).To(HaveLen(2))
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

		It("should handle rolling updates with VMAgent configuration changes", func() {
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
			vmclusters := []vmv1beta1.VMCluster{*vmCluster1, *vmCluster2}
			DeferCleanup(func() {
				for _, vmcluster := range vmclusters {
					Expect(finalize.SafeDeleteWithFinalizer(ctx, k8sClient, &vmcluster)).To(Succeed())
				}
			})

			createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserNames)

			namespacedName.Name = "distributed-agent-upgrade"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					Zones: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{
								Name: vmCluster1.Name,
							},
						},
						{
							Ref: &corev1.LocalObjectReference{
								Name: vmCluster2.Name,
							},
						},
					},
					VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{
							Name: validVMUserNames[0].Name,
						},
						{
							Name: validVMUserNames[1].Name,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			// Update to add VMAgent to second cluster
			Eventually(func() error {
				var obj vmv1alpha1.VMDistributedCluster
				if err := k8sClient.Get(ctx, namespacedName, &obj); err != nil {
					return err
				}

				return k8sClient.Update(ctx, &obj)
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())

			// Wait for VMDistributedCluster to become operational after update
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			// Verify both clusters are configured correctly
			var updatedCluster vmv1alpha1.VMDistributedCluster
			Expect(k8sClient.Get(ctx, namespacedName, &updatedCluster)).To(Succeed())
			Expect(updatedCluster.Spec.Zones).To(HaveLen(2))

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
			Entry("with no VMAgent set", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "no-vmagent-set",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					Zones:   []vmv1alpha1.VMClusterRefOrSpec{},
					VMAgent: corev1.LocalObjectReference{Name: "missing-vmagent"},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with invalid VMAgent", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "invalid-vmagent",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: corev1.LocalObjectReference{Name: "missing-vmagent"},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
						{Name: validVMUserNames[1].Name},
					},
					Zones: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{
							Name: "vmcluster-1",
						}},
					},
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
			Entry("with no VMUsers set", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "no-vmusers-set",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{},
					Zones: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{
							Name: "vmcluster-1",
						}},
					},
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
			Entry("with invalid VMUser", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "invalid-vmuser",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: "missing-vmuser"},
					},
					Zones: []vmv1alpha1.VMClusterRefOrSpec{},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with invalid VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
						{Name: validVMUserNames[1].Name},
					},
					Zones: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{
							Name: "missing-cluster",
						}},
					},
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
		createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserNames)

		namespacedName.Name = "vmcluster"
		cr := &vmv1alpha1.VMDistributedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      namespacedName.Name,
			},
			Spec: vmv1alpha1.VMDistributedClusterSpec{
				VMAgent: corev1.LocalObjectReference{Name: validVMAgentName.Name},
				VMUsers: []corev1.LocalObjectReference{
					{Name: validVMUserNames[0].Name},
					{Name: validVMUserNames[1].Name},
				},
				Zones: []vmv1alpha1.VMClusterRefOrSpec{
					{Ref: &corev1.LocalObjectReference{
						Name: vmCluster.Name,
					}},
				},
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
