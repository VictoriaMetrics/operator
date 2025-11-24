package e2e

import (
	"context"
	"fmt"
	"time"

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

// createVMClustersAndUpdateTargetRefs creates VMClusters and updates VMUsers with their target references.
func createVMClustersAndUpdateTargetRefs(
	ctx context.Context,
	k8sClient client.Client,
	clusters []vmv1beta1.VMCluster,
	ns string,
	vmUserNames []types.NamespacedName,
) {
	refs := make([]vmv1beta1.TargetRef, len(clusters))
	for i, vmcluster := range clusters {
		createVMClusterAndEnsureOperational(ctx, k8sClient, &vmcluster, ns)

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
	updateVMUserTargetRefs(ctx, k8sClient, vmUserNames, refs)
}

// createVMClusterAndEnsureOperational creates a VMCluster, sets up its deferred cleanup, and waits for it to become operational.
func createVMClusterAndEnsureOperational(ctx context.Context, k8sClient client.Client, vmcluster *vmv1beta1.VMCluster, ns string) {
	Expect(k8sClient.Create(ctx, vmcluster)).To(Succeed())
	localVMCluster := vmcluster
	DeferCleanup(func() {
		Expect(finalize.SafeDelete(ctx, k8sClient, localVMCluster)).To(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: localVMCluster.Name, Namespace: ns}, &vmv1beta1.VMCluster{})
		}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
	})
	Eventually(func() error {
		return expectObjectStatusOperational(ctx, k8sClient, localVMCluster, types.NamespacedName{Name: localVMCluster.Name, Namespace: ns})
	}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
}

// updateVMUserTargetRefs updates VMUser resources with a given set of TargetRefs.
func updateVMUserTargetRefs(ctx context.Context, k8sClient client.Client, vmUserNames []types.NamespacedName, refs []vmv1beta1.TargetRef) {
	for _, vmUserName := range vmUserNames {
		var validVMUser vmv1beta1.VMUser
		Expect(k8sClient.Get(ctx, vmUserName, &validVMUser)).To(Succeed())
		validVMUser.Spec.TargetRefs = refs
		Expect(k8sClient.Update(ctx, &validVMUser)).To(Succeed())
	}
}

// cleanupVMClusters handles the deferred cleanup of VMCluster resources.
func cleanupVMClusters(ctx context.Context, k8sClient client.Client, vmclusters []vmv1beta1.VMCluster) {
	for _, vmcluster := range vmclusters {
		finalize.SafeDelete(ctx, k8sClient, &vmcluster)
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: vmcluster.Name, Namespace: vmcluster.Namespace}, &vmv1beta1.VMCluster{})
		}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
	}
}

func verifyOwnerReferences(ctx context.Context, cr *vmv1alpha1.VMDistributedCluster, vmclusters []vmv1beta1.VMCluster, namespace string) {
	var fetchedVMCluster vmv1beta1.VMCluster
	for _, vmcluster := range vmclusters {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmcluster.Name, Namespace: namespace}, &fetchedVMCluster)).To(Succeed())
		Expect(fetchedVMCluster.GetOwnerReferences()).To(HaveLen(1))
		ownerRef := fetchedVMCluster.GetOwnerReferences()[0]
		Expect(ownerRef.Kind).To(Equal("VMDistributedCluster"))
		Expect(ownerRef.APIVersion).To(Equal("operator.victoriametrics.com/v1alpha1"))
		Expect(ownerRef.Name).To(Equal(cr.Name))
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
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "http://vminsert.monitoring:8089/api/v1/write", // Dummy URL to satisfy validation
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &validVMAgent)).To(Succeed(), "must create managed vm-agent before test")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, types.NamespacedName{Name: validVMAgent.Name, Namespace: namespace})
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
	}

	afterEach := func() {
		Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      namespacedName.Name,
			},
		})).To(Succeed(), "must delete vmdistributedcluster after test")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: namespacedName.Name, Namespace: namespace}, &vmv1beta1.VMCluster{})
		}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

		for _, vmuserName := range validVMUserNames {
			Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: vmuserName.Namespace,
					Name:      vmuserName.Name,
				},
			})).To(Succeed(), "must delete vm-user after test")
			Eventually(func() error {
				return k8sClient.Get(ctx, vmuserName, &vmv1beta1.VMUser{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		}

		// // Clean up VMAgent
		// Expect(finalize.SafeDelete(ctx, k8sClient, &vmv1beta1.VMAgent{
		// 	ObjectMeta: metav1.ObjectMeta{
		// 		Namespace: validVMAgentName.Namespace,
		// 		Name:      validVMAgentName.Name,
		// 	},
		// })).To(Succeed(), "must delete vm-agent after test")
		// Eventually(func() error {
		// 	return k8sClient.Get(ctx, validVMAgentName, &vmv1beta1.VMAgent{})
		// }, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
	}

	Context("create", func() {
		DescribeTable("should create vmdistributedcluster with VMAgent", func(cr *vmv1alpha1.VMDistributedCluster, vmclusters []vmv1beta1.VMCluster, vmagents map[string]*vmv1beta1.VMAgent, verify func(cr *vmv1alpha1.VMDistributedCluster, createdVMClusters []vmv1beta1.VMCluster)) {
			beforeEach()
			DeferCleanup(afterEach)

			createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserNames)
			DeferCleanup(func() {
				cleanupVMClusters(ctx, k8sClient, vmclusters)
			})

			// Create VMAgents
			for _, vmagent := range vmagents {
				Expect(k8sClient.Create(ctx, vmagent)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, types.NamespacedName{Name: vmagent.Name, Namespace: vmagent.Namespace})
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
			}
			DeferCleanup(func() {
				for _, vmagent := range vmagents {
					finalize.SafeDelete(ctx, k8sClient, vmagent)
					Eventually(func() error {
						return k8sClient.Get(ctx, types.NamespacedName{Name: vmagent.Name, Namespace: vmagent.Namespace}, &vmv1beta1.VMAgent{})
					}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
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
				verify(&createdCluster, vmclusters)
			}
		},
			Entry("with single VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "single-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{
								Name: "vmcluster-1",
							},
						},
					}},
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
				}}, func(cr *vmv1alpha1.VMDistributedCluster, createdVMClusters []vmv1beta1.VMCluster) {
				Expect(cr.Status.VMClusterInfo).To(HaveLen(1))
				names := []string{
					cr.Status.VMClusterInfo[0].VMClusterName,
				}
				Expect(names).To(ContainElements("vmcluster-1"))
				verifyOwnerReferences(ctx, cr, createdVMClusters, namespace)
			}),
			Entry("with multiple VMClusters and VMAgent pairs", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "multi-cluster-agent",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
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
					}},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
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
				}}, func(cr *vmv1alpha1.VMDistributedCluster, createdVMClusters []vmv1beta1.VMCluster) {
				Expect(cr.Status.VMClusterInfo).To(HaveLen(2))
				names := []string{
					cr.Status.VMClusterInfo[0].VMClusterName,
					cr.Status.VMClusterInfo[1].VMClusterName,
				}
				Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))
				verifyOwnerReferences(ctx, cr, createdVMClusters, namespace)
			}),
			Entry("with mixed VMClusters (some with VMAgent, some without)", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "mixed-cluster-agent",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
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
					}},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
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
				}}, func(cr *vmv1alpha1.VMDistributedCluster, createdVMClusters []vmv1beta1.VMCluster) {
				Expect(cr.Status.VMClusterInfo).To(HaveLen(2))
				names := []string{
					cr.Status.VMClusterInfo[0].VMClusterName,
					cr.Status.VMClusterInfo[1].VMClusterName,
				}
				Expect(names).To(ContainElements("vmcluster-1", "vmcluster-2"))
				verifyOwnerReferences(ctx, cr, createdVMClusters, namespace)
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
				cleanupVMClusters(ctx, k8sClient, vmclusters)
			})

			createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserNames)

			namespacedName.Name = "distributed-upgrade"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
						{Name: validVMUserNames[1].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{
						VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
							{Ref: &corev1.LocalObjectReference{
								Name: vmCluster1.Name,
							}},
							{Ref: &corev1.LocalObjectReference{
								Name: vmCluster2.Name,
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, vmclusters, namespace)

			// Apply spec update
			Expect(k8sClient.Get(ctx, namespacedName, cr)).To(Succeed())
			cr.Spec.Zones.VMClusters[0].OverrideSpec = &apiextensionsv1.JSON{
				Raw: []byte(fmt.Sprintf(`{"clusterVersion": "%s"}`, updateVersion)),
			}
			cr.Spec.Zones.VMClusters[1].OverrideSpec = &apiextensionsv1.JSON{
				Raw: []byte(fmt.Sprintf(`{"clusterVersion": "%s"}`, updateVersion)),
			}
			Expect(k8sClient.Update(ctx, cr)).To(Succeed())
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
			Eventually(func() error {
				vmCluster1 := &vmv1beta1.VMCluster{}
				vmCluster2 := &vmv1beta1.VMCluster{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: names[0], Namespace: namespace}, vmCluster1)
				if err != nil {
					return err
				}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: names[1], Namespace: namespace}, vmCluster2)
				if err != nil {
					return err
				}
				if vmCluster1.Spec.ClusterVersion != updateVersion || vmCluster2.Spec.ClusterVersion != updateVersion {
					return fmt.Errorf("expected both clusters to have version %s, but got %s and %s", updateVersion, vmCluster1.Spec.ClusterVersion, vmCluster2.Spec.ClusterVersion)
				}
				return nil
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
		})

		It("should skip reconciliation when VMDistributedCluster is paused", func() {
			beforeEach()
			DeferCleanup(afterEach)

			By("creating a VMCluster")
			vmCluster := &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vmcluster-paused",
				},
				Spec: vmv1beta1.VMClusterSpec{
					ClusterVersion: "v1.126.0-cluster",
					VMStorage: &vmv1beta1.VMStorage{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
			}
			vmUserName := types.NamespacedName{
				Namespace: namespace,
				Name:      "vmuser-paused",
			}
			pausedVMUser := vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmUserName.Name,
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMUserSpec{
					TargetRefs: []vmv1beta1.TargetRef{{
						CRD: &vmv1beta1.CRDRef{
							Kind:      "VMCluster/vmselect",
							Name:      "vmcluster-paused",
							Namespace: namespace,
						},
						TargetPathSuffix: "/select/1",
					}},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, &pausedVMUser)).To(Succeed())
				Expect(finalize.SafeDelete(ctx, k8sClient, vmCluster)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, &pausedVMUser)).To(Succeed(), "must create managed vm-user before test")
			createVMClusterAndEnsureOperational(ctx, k8sClient, vmCluster, namespace)

			By("creating a VMDistributedCluster")
			namespacedName.Name = "distributed-paused"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: vmUserName.Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}},
					},
					},
				}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
			verifyOwnerReferences(ctx, cr, []vmv1beta1.VMCluster{*vmCluster}, namespace)

			By("pausing the VMDistributedCluster")
			// Re-fetch the latest VMDistributedCluster object to avoid conflict errors
			Eventually(func() error {
				err := k8sClient.Get(ctx, namespacedName, cr)
				if err != nil {
					return err
				}
				cr.Spec.Paused = true
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).Should(Succeed())

			By("attempting to scale the VMCluster while paused")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: namespace}, vmCluster)).To(Succeed())
			initialReplicas := *vmCluster.Spec.VMStorage.ReplicaCount

			// Re-fetch the latest VMDistributedCluster object to avoid conflict errors
			Eventually(func() error {
				err := k8sClient.Get(ctx, namespacedName, cr)
				if err != nil {
					return err
				}
				cr.Spec.Zones.VMClusters[0].OverrideSpec = &apiextensionsv1.JSON{
					Raw: []byte(fmt.Sprintf(`{"vmstorage":{"replicaCount": %d}}`, initialReplicas+1)),
				}
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).Should(Succeed())

			Consistently(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: namespace}, vmCluster)).To(Succeed())
				return *vmCluster.Spec.VMStorage.ReplicaCount
			}, "10s", "1s").Should(Equal(initialReplicas))

			By("unpausing the VMDistributedCluster")
			// Re-fetch the latest VMDistributedCluster object to avoid conflict errors
			Eventually(func() error {
				err := k8sClient.Get(ctx, namespacedName, cr)
				if err != nil {
					return err
				}
				cr.Spec.Paused = false
				return k8sClient.Update(ctx, cr)
			}, eventualStatefulsetAppReadyTimeout).Should(Succeed())

			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).Should(Succeed())

			By("verifying reconciliation resumes after unpausing")
			Eventually(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: namespace}, vmCluster)).To(Succeed())
				return *vmCluster.Spec.VMStorage.ReplicaCount
			}, eventualDeploymentAppReadyTimeout).Should(Equal(initialReplicas + 1))
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
					Expect(finalize.SafeDelete(ctx, k8sClient, &vmcluster)).To(Succeed())
				}
			})

			createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserNames)

			vmAgents := []vmv1beta1.VMAgent{
				{
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
				{
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
			}
			for _, vmagent := range vmAgents {
				Expect(k8sClient.Create(ctx, &vmagent)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, types.NamespacedName{Name: vmagent.Name, Namespace: vmagent.Namespace})
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
			}
			DeferCleanup(func() {
				for _, vmagent := range vmAgents {
					Expect(finalize.SafeDelete(ctx, k8sClient, &vmagent)).To(Succeed())
				}
			})

			namespacedName.Name = "distributed-agent-upgrade"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
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
					}},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: vmAgents[0].Name},
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
			verifyOwnerReferences(ctx, cr, vmclusters, namespace)

			// Update to update VMAgent
			Eventually(func() error {
				var obj vmv1alpha1.VMDistributedCluster
				if err := k8sClient.Get(ctx, namespacedName, &obj); err != nil {
					return err
				}
				obj.Spec.VMAgent = vmv1alpha1.VMAgentNameAndSpec{Name: vmAgents[1].Name}
				return k8sClient.Update(ctx, &obj)
			}, eventualDeploymentAppReadyTimeout).Should(Succeed())

			// Wait for VMDistributedCluster to become operational after update
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
		})

		It("should successfully create a VMDistributedCluster with inline VMCluster specs", func() {
			beforeEach()
			DeferCleanup(afterEach)

			By("creating a VMDistributedCluster with inline VMCluster specs")
			namespacedName.Name = "inline-vmcluster"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Name: "inline-cluster-1",
							Spec: &vmv1beta1.VMClusterSpec{
								ClusterVersion: "v1.125.0-cluster",
								VMStorage: &vmv1beta1.VMStorage{
									CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](1),
									},
								},
							},
						},
						{
							Name: "inline-cluster-2",
							Spec: &vmv1beta1.VMClusterSpec{
								ClusterVersion: "v1.126.0-cluster",
								VMStorage: &vmv1beta1.VMStorage{
									CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](2),
									},
								},
							},
						},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			var inlineVMClusters []vmv1beta1.VMCluster
			var refs []vmv1beta1.TargetRef
			for _, zone := range cr.Spec.Zones.VMClusters {
				inlineVMClusters = append(inlineVMClusters, vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.Name,
						Namespace: namespace,
					},
				})
				refs = append(refs, vmv1beta1.TargetRef{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "VMCluster/vmselect",
						Name:      zone.Name,
						Namespace: namespace,
					},
					TargetPathSuffix: "/select/1",
				})
			}

			// Update all VMUsers with the target refs
			updateVMUserTargetRefs(ctx, k8sClient, validVMUserNames, refs)
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, inlineVMClusters, namespace)
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})

			By("updating the VMUser to target the inline VMClusters")
			var vmUser vmv1beta1.VMUser
			Expect(k8sClient.Get(ctx, validVMUserNames[0], &vmUser)).To(Succeed())
			vmUser.Spec.TargetRefs = []vmv1beta1.TargetRef{
				{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "VMCluster/vmselect",
						Name:      "inline-cluster-1",
						Namespace: namespace,
					},
					TargetPathSuffix: "/select/1",
				},
				{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "VMCluster/vmselect",
						Name:      "inline-cluster-2",
						Namespace: namespace,
					},
					TargetPathSuffix: "/select/1",
				},
			}
			Expect(k8sClient.Update(ctx, &vmUser)).To(Succeed())
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, &vmUser)).To(Succeed())
			})

			By("waiting for VMDistributedCluster to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			By("verifying that the inline VMClusters are created and operational")
			var vmCluster1 vmv1beta1.VMCluster
			namespacedName := types.NamespacedName{Name: "inline-cluster-1", Namespace: namespace}
			Expect(k8sClient.Get(ctx, namespacedName, &vmCluster1)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmCluster1, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
			Expect(vmCluster1.Spec.ClusterVersion).To(Equal("v1.125.0-cluster"))
			actualReplicaCount := *vmCluster1.Spec.VMStorage.ReplicaCount
			Expect(actualReplicaCount).To(Equal(int32(1)))

			var vmCluster2 vmv1beta1.VMCluster
			namespacedName = types.NamespacedName{Name: "inline-cluster-2", Namespace: namespace}
			Expect(k8sClient.Get(ctx, namespacedName, &vmCluster2)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmCluster2, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).Should(Succeed())
			Expect(vmCluster2.Spec.ClusterVersion).To(Equal("v1.126.0-cluster"))
			actualReplicaCount = *vmCluster2.Spec.VMStorage.ReplicaCount
			Expect(actualReplicaCount).To(Equal(int32(2)))

			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, &vmCluster1)).To(Succeed())
				Expect(finalize.SafeDelete(ctx, k8sClient, &vmCluster2)).To(Succeed())
			})
		})

		It("should successfully create a VMDistributedCluster with VMCluster references and override spec", func() {
			beforeEach()
			DeferCleanup(afterEach)

			By("creating an initial VMCluster")
			initialCluster := &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "referenced-cluster",
				},
				Spec: vmv1beta1.VMClusterSpec{
					ClusterVersion: "v1.126.0-cluster",
					VMStorage: &vmv1beta1.VMStorage{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, initialCluster)).To(Succeed())
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, initialCluster)).To(Succeed())
			})
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, initialCluster, types.NamespacedName{Name: initialCluster.Name, Namespace: namespace})
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

			By("updating the VMUser to target the initial VMCluster")
			var vmUser vmv1beta1.VMUser
			Expect(k8sClient.Get(ctx, validVMUserNames[0], &vmUser)).To(Succeed())
			vmUser.Spec.TargetRefs = []vmv1beta1.TargetRef{
				{
					CRD: &vmv1beta1.CRDRef{
						Kind:      "VMCluster/vmselect",
						Name:      initialCluster.Name,
						Namespace: namespace,
					},
					TargetPathSuffix: "/select/1",
				},
			}
			Expect(k8sClient.Update(ctx, &vmUser)).To(Succeed())

			By("creating a VMDistributedCluster referencing the existing VMCluster with an override spec")
			namespacedName.Name = "ref-override-cluster"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{Name: initialCluster.Name},
							OverrideSpec: &apiextensionsv1.JSON{
								Raw: []byte(`{"retentionPeriod": "10h"}`),
							},
						},
					},
					}},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})

			By("waiting for VMDistributedCluster to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, []vmv1beta1.VMCluster{*initialCluster}, namespace)

			By("verifying that the referenced VMCluster has the override applied")
			var updatedCluster vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: initialCluster.Name, Namespace: namespace}, &updatedCluster)).To(Succeed())
			Expect(updatedCluster.Spec.RetentionPeriod).To(Equal("10h"))
		})
	})

	Context("fail", func() {
		DescribeTable("should fail when creating vmdistributedcluster", func(cr *vmv1alpha1.VMDistributedCluster, vmclusters []vmv1beta1.VMCluster) {
			beforeEach()
			DeferCleanup(func() {
				cleanupVMClusters(ctx, k8sClient, vmclusters)
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
			Entry("with no VMUsers set", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "no-vmusers-set",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 5 * time.Second},
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers:         []corev1.LocalObjectReference{},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{
							Name: "vmcluster-1",
						}},
					}},
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
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: "missing-vmuser"},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{}},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with invalid VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
						{Name: validVMUserNames[1].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{
								Name: "missing-cluster",
							},
						},
					}},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with zone spec but missing name", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "zone-spec-missing-name",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Spec: &vmv1beta1.VMClusterSpec{
								ClusterVersion: "v1.126.0-cluster",
							},
						},
					}},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with zone missing spec and ref", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "zone-missing-spec-ref",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{},
					}},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with zone having both spec and ref", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "zone-both-spec-ref",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{
								Name: "vmcluster-existing",
							},
							Spec: &vmv1beta1.VMClusterSpec{
								ClusterVersion: "v1.126.0-cluster",
							},
						},
					}},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with missing global VMAgent", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-global-vmagent",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: "non-existent-vmagent"},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"}},
					}},
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
			Entry("with missing VMUser", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-vmuser-fail",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: "non-existent-vmuser"},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"}},
					}},
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
			Entry("with missing VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-vmcluster-fail",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{Name: "non-existent-vmcluster"}},
					}},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with invalid OverrideSpec for VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "invalid-override-spec",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{Name: "vmcluster-1"},
							OverrideSpec: &apiextensionsv1.JSON{
								Raw: []byte(`{"invalidField": "invalidValue"}`), // Invalid override spec
							},
						},
					}},
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
			Entry("VMCluster creation fails post-override spec", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vmcluster-create-fail",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
					VMUsers: []corev1.LocalObjectReference{
						{Name: validVMUserNames[0].Name},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Name: "existing-vmcluster-for-failure", // This VMCluster name will conflict with the one created below
							Spec: &vmv1beta1.VMClusterSpec{
								ClusterVersion: "v1.126.0-cluster",
								VMStorage: &vmv1beta1.VMStorage{
									CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](1),
									},
								},
							},
						},
					}},
				},
			}, []vmv1beta1.VMCluster{
				{
					// This VMCluster will be created by createVMClustersAndUpdateTargetRefs before VMDistributedCluster attempts to create one with the same name.
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      "existing-vmcluster-for-failure",
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
			Expect(finalize.SafeDelete(ctx, k8sClient, vmCluster)).To(Succeed())
		})
		createVMClustersAndUpdateTargetRefs(ctx, k8sClient, vmclusters, namespace, validVMUserNames)

		namespacedName.Name = "vmcluster"
		cr := &vmv1alpha1.VMDistributedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      namespacedName.Name,
			},
			Spec: vmv1alpha1.VMDistributedClusterSpec{
				VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: validVMAgentName.Name},
				VMUsers: []corev1.LocalObjectReference{
					{Name: validVMUserNames[0].Name},
					{Name: validVMUserNames[1].Name},
				},
				Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
					{Ref: &corev1.LocalObjectReference{
						Name: vmCluster.Name,
					}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())
		Eventually(func() error {
			return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, types.NamespacedName{Name: namespacedName.Name, Namespace: namespace})
		}, eventualStatefulsetAppReadyTimeout).WithContext(ctx).Should(Succeed())

		Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
		Eventually(func() error {
			err := k8sClient.Get(ctx, namespacedName, &vmv1alpha1.VMDistributedCluster{})
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("want NotFound error, got: %w", err)
		}, eventualDeletionTimeout, 1).WithContext(ctx).Should(Succeed())
	})
})
