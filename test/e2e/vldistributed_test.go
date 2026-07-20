package e2e

import (
	"context"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vldistributed"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

func genVLClusterSpec(opts ...func(*vmv1.VLClusterSpec)) vmv1.VLClusterSpec {
	commonAppsParams := vmv1beta1.CommonAppsParams{
		ReplicaCount: ptr.To[int32](1),
	}
	noReplicas := vmv1beta1.CommonAppsParams{
		ReplicaCount: ptr.To[int32](0),
	}

	s := vmv1.VLClusterSpec{
		VLSelect: &vmv1.VLSelect{
			CommonAppsParams: commonAppsParams,
		},
		VLInsert: &vmv1.VLInsert{
			CommonAppsParams: noReplicas,
		},
		VLStorage: &vmv1.VLStorage{
			CommonAppsParams: commonAppsParams,
		},
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// createVLClusters creates a VLCluster, sets up its deferred cleanup, and waits for it to become operational.
func createVLClusters(ctx context.Context, wg *sync.WaitGroup, k8sClient client.Client, vlClusters ...*vmv1.VLCluster) {
	for i := range vlClusters {
		vlCluster := vlClusters[i]
		wg.Go(func() {
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, vlCluster)).ToNot(HaveOccurred())
				waitResourceDeleted(ctx, types.NamespacedName{Name: vlCluster.Name, Namespace: vlCluster.Namespace}, &vmv1.VLClusterList{})
			})
			clusterNsn := types.NamespacedName{Name: vlCluster.Name, Namespace: vlCluster.Namespace}
			expectStatusAfterAction(ctx, &vmv1.VLClusterList{}, clusterNsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, vlCluster.DeepCopy())).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
		})
	}
}

func genVLAgentSpec() vmv1.VLAgentSpec {
	return vmv1.VLAgentSpec{
		CommonAppsParams: vmv1beta1.CommonAppsParams{
			ReplicaCount: ptr.To[int32](1),
		},
		RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
			{URL: "http://localhost"},
		},
	}
}

func createVLAgents(ctx context.Context, wg *sync.WaitGroup, k8sClient client.Client, vlAgents ...*vmv1.VLAgent) {
	for i := range vlAgents {
		vlAgent := vlAgents[i]
		wg.Go(func() {
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, vlAgent)).ToNot(HaveOccurred())
				waitResourceDeleted(ctx, types.NamespacedName{Name: vlAgent.Name, Namespace: vlAgent.Namespace}, &vmv1.VLAgentList{})
			})
			agentNsn := types.NamespacedName{Name: vlAgent.Name, Namespace: vlAgent.Namespace}
			expectStatusAfterAction(ctx, &vmv1.VLAgentList{}, agentNsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, vlAgent)).NotTo(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
		})
	}
}

func verifyVLDistributedOwnerReferences(ctx context.Context, cr *vmv1alpha1.VLDistributed, vlClusters []*vmv1.VLCluster, namespace string) {
	GinkgoHelper()
	var fetchedVLCluster vmv1.VLCluster
	for _, vlCluster := range vlClusters {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlCluster.Name, Namespace: namespace}, &fetchedVLCluster)).ToNot(HaveOccurred())
		Expect(fetchedVLCluster.GetOwnerReferences()).To(HaveLen(1))
		ownerRef := fetchedVLCluster.GetOwnerReferences()[0]
		Expect(ownerRef.Kind).To(Equal("VLDistributed"))
		Expect(ownerRef.APIVersion).To(Equal("operator.victoriametrics.com/v1alpha1"))
		Expect(ownerRef.Name).To(Equal(cr.Name))
	}
}

//nolint:dupl,lll
var _ = Describe("e2e VLDistributed", Label("vl", "vldistributed"), func() {
	ctx := context.Background()

	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	nsn := types.NamespacedName{
		Namespace: namespace,
	}

	Context("create", func() {
		It("should successfully create a VLDistributed with inline VLAgent spec", func() {
			By("creating 2 VLClusters")
			nsn.Name = "vld-inline-vlagent"
			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vlClusterSpec := genVLClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
			}
			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VLDistributed with inline VLAgent spec")
			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					VMAuth: vmv1alpha1.VLDistributedAuth{
						Name: nsn.Name,
					},
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Spec: vmv1alpha1.VLDistributedZoneAgentSpec{
								CommonAppsParams: vmv1beta1.CommonAppsParams{
									ReplicaCount: ptr.To[int32](2),
								},
							},
						},
					},
					Zones: zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			for i := range zs {
				z := &zs[i]
				var vlagent vmv1.VLAgent
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: z.VLAgent.Name, Namespace: namespace}, &vlagent)
				}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
				Expect(vlagent.Spec.ReplicaCount).To(Equal(ptr.To[int32](2)))
				Expect(vlagent.OwnerReferences).To(HaveLen(1))
				Expect(vlagent.OwnerReferences[0].Name).To(Equal(cr.Name))
				Expect(vlagent.OwnerReferences[0].Kind).To(Equal("VLDistributed"))
				Expect(vlagent.OwnerReferences[0].UID).To(Equal(cr.UID))
			}

			var vmauth vmv1beta1.VMAuth
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &vmauth)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			Expect(vmauth.OwnerReferences).To(HaveLen(1))
			Expect(vmauth.OwnerReferences[0].Name).To(Equal(cr.Name))
			Expect(vmauth.OwnerReferences[0].Kind).To(Equal("VLDistributed"))
			Expect(vmauth.OwnerReferences[0].UID).To(Equal(cr.UID))
		})

		It("should successfully create a VLDistributed with inline VLCluster specs", func() {
			nsn.Name = "vld-inline-vlcluster"
			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlClusterFn := []func(*vmv1.VLClusterSpec){
				func(s *vmv1.VLClusterSpec) {
					s.VLSelect.ReplicaCount = ptr.To[int32](1)
				},
				func(s *vmv1.VLClusterSpec) {
					s.VLSelect.ReplicaCount = ptr.To[int32](2)
				},
			}
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				vlClusterSpec := genVLClusterSpec(vlClusterFn[i])
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
			}
			var wg sync.WaitGroup
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			wg.Wait()

			By("creating a VLDistributed with inline VLCluster specs")
			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			var inlineVLClusters []*vmv1.VLCluster
			owner := cr.AsOwner()
			for _, zone := range cr.Spec.Zones {
				inlineVLClusters = append(inlineVLClusters, &vmv1.VLCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:            zone.VLCluster.Name,
						Namespace:       namespace,
						OwnerReferences: []metav1.OwnerReference{owner},
					},
				})
			}
			verifyVLDistributedOwnerReferences(ctx, cr, inlineVLClusters, namespace)

			By("verifying that the inline VLClusters are created and operational")
			clusterNsn := types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}
			Expect(suite.WatchUntilStatusReached(ctx, k8sClient, &vmv1.VLClusterList{}, clusterNsn, eventualDistributedExpandingTimeout,
				vmv1beta1.UpdateStatusOperational, vmv1beta1.UpdateStatusFailed)).ToNot(HaveOccurred())
			vlCluster1 := &vmv1.VLCluster{}
			Expect(k8sClient.Get(ctx, clusterNsn, vlCluster1)).ToNot(HaveOccurred())
			Expect(*vlCluster1.Spec.VLSelect.ReplicaCount).To(Equal(int32(1)))
			Expect(*vlCluster1.Spec.VLInsert.ReplicaCount).To(Equal(int32(0)))
			Expect(*vlCluster1.Spec.VLStorage.ReplicaCount).To(Equal(int32(1)))

			clusterNsn = types.NamespacedName{Name: vlClusters[1].Name, Namespace: namespace}
			Expect(suite.WatchUntilStatusReached(ctx, k8sClient, &vmv1.VLClusterList{}, clusterNsn, eventualDistributedExpandingTimeout,
				vmv1beta1.UpdateStatusOperational, vmv1beta1.UpdateStatusFailed)).ToNot(HaveOccurred())
			vlCluster2 := &vmv1.VLCluster{}
			Expect(k8sClient.Get(ctx, clusterNsn, vlCluster2)).ToNot(HaveOccurred())
			Expect(*vlCluster2.Spec.VLSelect.ReplicaCount).To(Equal(int32(2)))
			Expect(*vlCluster2.Spec.VLInsert.ReplicaCount).To(Equal(int32(0)))
			Expect(*vlCluster2.Spec.VLStorage.ReplicaCount).To(Equal(int32(1)))
		})

		It("should successfully create a VLDistributed with inline VMAuth spec", func() {
			By("creating VLClusters")
			nsn.Name = "vld-inline-vmauth"
			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				vlClusterSpec := genVLClusterSpec()
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
			}
			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			wg.Wait()

			By("creating a VLDistributed with inline VMAuth spec")
			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{
						Name: nsn.Name,
						Spec: vmv1beta1.VMAuthSpec{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
					Zones: zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			By("verifying that the inline VMAuth is created and operational")
			var createdVMAuth vmv1beta1.VMAuth
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &createdVMAuth)).ToNot(HaveOccurred())
			Expect(createdVMAuth.Spec.CommonAppsParams.ReplicaCount).To(HaveValue(Equal(int32(1))))

			By("verifying VMAuth has correct owner reference")
			Expect(createdVMAuth.GetOwnerReferences()).To(HaveLen(1))
			ownerRef := createdVMAuth.GetOwnerReferences()[0]
			Expect(ownerRef.Kind).To(Equal("VLDistributed"))
			Expect(ownerRef.Name).To(Equal(cr.Name))
		})

		It("should apply global overrides before cluster-specific overrides", func() {
			By("creating initial VLClusters")
			nsn.Name = "vld-global-override"

			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			vlClusterFn := []func(*vmv1.VLClusterSpec){
				func(s *vmv1.VLClusterSpec) {
					s.VLStorage.ReplicaCount = ptr.To[int32](1)
				},
				func(s *vmv1.VLClusterSpec) {
					s.VLStorage.ReplicaCount = ptr.To[int32](2)
				},
			}
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = genVLClusterSpec(vlClusterFn[i])
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec: genVLClusterSpec(func(s *vmv1.VLClusterSpec) {
						s.ClusterVersion = "v1.51.0"
						s.VLStorage.RetentionPeriod = "30d"
					}),
				}
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VLDistributed with GlobalClusterSpec and cluster-specific overrides")
			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
						VLCluster: vmv1alpha1.VLDistributedZoneCluster{
							Name: "%ZONE%",
							Spec: genVLClusterSpec(func(s *vmv1.VLClusterSpec) {
								s.VLStorage.RetentionPeriod = "60d"
							}),
						},
					},
					Zones: zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			verifyVLDistributedOwnerReferences(ctx, cr, vlClusters, namespace)

			By("verifying that both VLClusters have the global override applied")
			var updatedCluster1, updatedCluster2 vmv1.VLCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}, &updatedCluster1)).ToNot(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[1].Name, Namespace: namespace}, &updatedCluster2)).ToNot(HaveOccurred())

			// Global overrides should be applied
			Expect(updatedCluster1.Spec.VLStorage.RetentionPeriod).To(Equal("60d"))
			Expect(updatedCluster2.Spec.VLStorage.RetentionPeriod).To(Equal("60d"))

			// Cluster-specific overrides should be applied after global overrides
			Expect(*updatedCluster1.Spec.VLStorage.ReplicaCount).To(Equal(int32(1)))
			Expect(*updatedCluster2.Spec.VLStorage.ReplicaCount).To(Equal(int32(2)))
		})

		It("should handle rolling updates with VLAgent configuration changes", func() {
			nsn.Name = "vld-rolling-updates"

			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = genVLClusterSpec()
				zs[i].VLCluster.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       genVLClusterSpec(),
				}
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					Zones: zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			verifyVLDistributedOwnerReferences(ctx, cr, vlClusters, namespace)

			// Update VLAgent
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Eventually(func() error {
					var obj vmv1alpha1.VLDistributed
					if err := k8sClient.Get(ctx, nsn, &obj); err != nil {
						return err
					}
					for i := range obj.Spec.Zones {
						z := &obj.Spec.Zones[i]
						z.VLAgent = vmv1alpha1.VLDistributedZoneAgent{Name: z.Name}
					}
					return k8sClient.Update(ctx, &obj)
				}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
		})

		It("should wait for VLCluster upgrade completion", func() {
			initialVersion := "v1.50.0"
			updateVersion := "v1.51.0"

			nsn.Name = "vld-wait-for-upgrade"

			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vlClusterSpec := genVLClusterSpec(func(s *vmv1.VLClusterSpec) {
					s.ClusterVersion = initialVersion
				})
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			verifyVLDistributedOwnerReferences(ctx, cr, vlClusters, namespace)

			// Apply spec update and wait for expanding then operational
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Get(ctx, nsn, cr)).ToNot(HaveOccurred())
				cr.Spec.Zones[0].VLCluster.Spec.ClusterVersion = updateVersion
				cr.Spec.Zones[1].VLCluster.Spec.ClusterVersion = updateVersion
				Expect(k8sClient.Update(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusExpanding, vmv1beta1.UpdateStatusOperational)

			// Verify VLDistributed status reflects both clusters are upgraded/operational
			var newVLCluster1, newVLCluster2 vmv1.VLCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}, &newVLCluster1)).ToNot(HaveOccurred())
			Expect(newVLCluster1.Spec.ClusterVersion).To(Equal(updateVersion))
			Expect(newVLCluster1.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[1].Name, Namespace: namespace}, &newVLCluster2)).ToNot(HaveOccurred())
			Expect(newVLCluster2.Spec.ClusterVersion).To(Equal(updateVersion))
			Expect(newVLCluster2.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
		})

		It("should skip reconciliation when VLDistributed is paused", func() {
			By("creating VLClusters")
			initialVersion := "v1.51.0"
			nsn.Name = "vld-skip-when-paused"

			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vlClusterSpec := genVLClusterSpec(func(s *vmv1.VLClusterSpec) {
					s.ClusterVersion = initialVersion
				})
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VLDistributed")
			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)
			verifyVLDistributedOwnerReferences(ctx, cr, vlClusters, namespace)

			By("pausing the VLDistributed")
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Eventually(func() error {
					err := k8sClient.Get(ctx, nsn, cr)
					if err != nil {
						return err
					}
					cr.Spec.Paused = true
					return k8sClient.Update(ctx, cr)
				}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusPaused)

			By("attempting to scale the VLCluster while paused")
			var vlCluster1 vmv1.VLCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}, &vlCluster1)).ToNot(HaveOccurred())
			initialReplicas := *vlCluster1.Spec.VLStorage.ReplicaCount

			Eventually(func() error {
				err := k8sClient.Get(ctx, nsn, cr)
				if err != nil {
					return err
				}
				cr.Spec.Zones[0].VLCluster.Spec = vmv1.VLClusterSpec{
					VLInsert: cr.Spec.Zones[0].VLCluster.Spec.VLInsert,
					VLSelect: cr.Spec.Zones[0].VLCluster.Spec.VLSelect,
					VLStorage: &vmv1.VLStorage{
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](initialReplicas + 1),
						},
					},
				}
				return k8sClient.Update(ctx, cr)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())

			Consistently(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}, &vlCluster1)).ToNot(HaveOccurred())
				return *vlCluster1.Spec.VLStorage.ReplicaCount
			}, "10s", "1s").Should(Equal(initialReplicas))

			By("unpausing the VLDistributed")
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Eventually(func() error {
					err := k8sClient.Get(ctx, nsn, cr)
					if err != nil {
						return err
					}
					cr.Spec.Paused = false
					return k8sClient.Update(ctx, cr)
				}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusExpanding, vmv1beta1.UpdateStatusOperational)

			By("verifying reconciliation resumes after unpausing")
			Eventually(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}, &vlCluster1)).ToNot(HaveOccurred())
				return *vlCluster1.Spec.VLStorage.ReplicaCount
			}, eventualDeploymentAppReadyTimeout).Should(Equal(initialReplicas + 1))
		})

		It("should be idempotent when calling CreateOrUpdate multiple times", func() {
			const (
				attempts = 3
			)

			By("creating VLClusters for idempotency test")
			nsn.Name = "vld-idempotent"

			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vlClusterSpec := genVLClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VLDistributed")
			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			// Capture resource versions of key child resources
			var beforeCluster vmv1.VLCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}, &beforeCluster)).ToNot(HaveOccurred())
			clusterRV := beforeCluster.ResourceVersion

			var beforeAgent vmv1.VLAgent
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlAgents[0].Name, Namespace: namespace}, &beforeAgent)).ToNot(HaveOccurred())
			agentRV := beforeAgent.ResourceVersion

			var beforeVMAuth vmv1beta1.VMAuth
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &beforeVMAuth)).ToNot(HaveOccurred())
			authDeploymentSpec := beforeVMAuth.Spec

			// Call CreateOrUpdate multiple times and ensure it succeeds and doesn't modify child resources
			for i := 0; i < attempts; i++ {
				var latestCR vmv1alpha1.VLDistributed
				Expect(k8sClient.Get(ctx, nsn, &latestCR)).ToNot(HaveOccurred())
				latestCR.Kind = "VLDistributed"
				latestCR.APIVersion = vmv1alpha1.SchemeGroupVersion.String()
				k8sClient.Scheme().Default(&latestCR)
				Expect(vldistributed.CreateOrUpdate(ctx, &latestCR, k8sClient)).ToNot(HaveOccurred())
			}

			// Verify resource versions have not changed (no updates performed)
			var afterCluster vmv1.VLCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}, &afterCluster)).ToNot(HaveOccurred())
			Expect(afterCluster.ResourceVersion).To(Equal(clusterRV), "VLCluster resource version should not change")

			var afterAgent vmv1.VLAgent
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlAgents[0].Name, Namespace: namespace}, &afterAgent)).ToNot(HaveOccurred())
			Expect(afterAgent.ResourceVersion).To(Equal(agentRV), "VLAgent resource version should not change")

			var afterVMAuth vmv1beta1.VMAuth
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &afterVMAuth)).ToNot(HaveOccurred())
			Expect(afterVMAuth.Spec).To(Equal(authDeploymentSpec), "VMAuthLB Deployment spec should not change")
		})

		It("should transition from Expanding to Operational without entering Failing state", func() {
			By("creating VLClusters")
			nsn.Name = "vld-transition-without-failure"

			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vlClusterSpec := genVLClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VLDistributed")
			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}

			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})

			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

			By("waiting for Operational status while ensuring no Failing status occurs")
			Eventually(func() error {
				var fetchedCR vmv1alpha1.VLDistributed
				if err := k8sClient.Get(ctx, nsn, &fetchedCR); err != nil {
					return err
				}
				if fetchedCR.Status.UpdateStatus == vmv1beta1.UpdateStatusFailed {
					return StopTrying("VLDistributed entered Failing state")
				}
				if fetchedCR.Status.UpdateStatus != vmv1beta1.UpdateStatusOperational {
					return fmt.Errorf("waiting for Operational status, current: %s", fetchedCR.Status.UpdateStatus)
				}
				return nil
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
		})

		It("should correctly migrate zones between traffic modes", func() {
			nsn.Name = "vld-traffic-mode"

			zonesCount := 2
			clusterSpec := vmv1.VLClusterSpec{
				VLSelect:  &vmv1.VLSelect{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To[int32](1)}},
				VLInsert:  &vmv1.VLInsert{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To[int32](1)}},
				VLStorage: &vmv1.VLStorage{CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To[int32](1)}},
			}

			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLCluster.Spec = clusterSpec
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{ObjectMeta: objMeta, Spec: clusterSpec}
				vlAgents[i] = &vmv1.VLAgent{ObjectMeta: objMeta, Spec: genVLAgentSpec()}
			}

			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating VLDistributed with both zones in read-write mode")
			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: nsn.Name},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			zone0NSN := types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}
			zone1NSN := types.NamespacedName{Name: vlClusters[1].Name, Namespace: namespace}

			getInsertReplicas := func(name types.NamespacedName) int32 {
				GinkgoHelper()
				var c vmv1.VLCluster
				Expect(k8sClient.Get(ctx, name, &c)).ToNot(HaveOccurred())
				if c.Spec.VLInsert == nil {
					return -1
				}
				return ptr.Deref(c.Spec.VLInsert.ReplicaCount, -1)
			}

			By("verifying both zones start with VLInsert enabled")
			Expect(getInsertReplicas(zone0NSN)).To(BeEquivalentTo(1))
			Expect(getInsertReplicas(zone1NSN)).To(BeEquivalentTo(1))

			By("switching zone 0 to read-only mode")
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Eventually(func() error {
					var obj vmv1alpha1.VLDistributed
					if err := k8sClient.Get(ctx, nsn, &obj); err != nil {
						return err
					}
					obj.Spec.Zones[0].TrafficMode = vmv1alpha1.VLDistributedTrafficModeReadOnly
					return k8sClient.Update(ctx, &obj)
				}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			By("verifying zone 0 VLInsert is disabled and zone 1 is unchanged")
			Expect(getInsertReplicas(zone0NSN)).To(BeEquivalentTo(0))
			Expect(getInsertReplicas(zone1NSN)).To(BeEquivalentTo(1))

			By("switching zone 0 back to read-write mode")
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Eventually(func() error {
					var obj vmv1alpha1.VLDistributed
					if err := k8sClient.Get(ctx, nsn, &obj); err != nil {
						return err
					}
					obj.Spec.Zones[0].TrafficMode = vmv1alpha1.VLDistributedTrafficModeReadWrite
					return k8sClient.Update(ctx, &obj)
				}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			By("verifying zone 0 VLInsert is re-enabled")
			Expect(getInsertReplicas(zone0NSN)).To(BeEquivalentTo(1))
		})
	})

	Context("fail", func() {
		DescribeTable("should fail when creating VLDistributed", func(cr *vmv1alpha1.VLDistributed) {
			nsn.Name = cr.Name
			cr.Spec.VMAuth.Name = cr.Name
			if err := k8sClient.Create(ctx, cr); err != nil {
				Expect(k8serrors.IsInvalid(err)).To(BeTrue())
				return
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(suite.WatchUntilStatusReached(ctx, k8sClient, &vmv1alpha1.VLDistributedList{}, nsn,
				eventualDistributedExpandingTimeout, vmv1beta1.UpdateStatusFailed)).ToNot(HaveOccurred())
		},
			Entry("with invalid VLCluster", &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vld-missing-cluster",
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					Zones: []vmv1alpha1.VLDistributedZone{
						{
							Name: "test",
							VLCluster: vmv1alpha1.VLDistributedZoneCluster{
								Name: "missing-cluster",
							},
						},
					},
				},
			}),
			Entry("with zone spec but missing zone name", &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vld-zone-spec-missing-name",
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					Zones: []vmv1alpha1.VLDistributedZone{
						{
							VLCluster: vmv1alpha1.VLDistributedZoneCluster{
								Spec: genVLClusterSpec(),
							},
						},
					},
				},
			}),
		)
	})

	Context("delete", func() {
		It("should delete VLDistributed and remove it from the cluster", func() {
			nsn.Name = "vld-remove"

			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vlClusterSpec := genVLClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
			}

			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{
						Name: nsn.Name,
						Spec: vmv1beta1.VMAuthSpec{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
					Zones: zs,
				},
			}
			By("creating the VLDistributed")
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			By("ensuring VMAuth was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})).ToNot(HaveOccurred())

			By("ensuring VLClusters and VLAgents were created")
			for _, zone := range cr.Spec.Zones {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: zone.VLAgent.Name, Namespace: namespace}, &vmv1.VLAgent{})).ToNot(HaveOccurred())
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: zone.VLCluster.Name, Namespace: namespace}, &vmv1.VLCluster{})).ToNot(HaveOccurred())
			}

			By("deleting the VLDistributed")
			Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())

			By("ensuring VMAuth is eventually removed")
			waitResourceDeleted(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuthList{})

			By("ensuring VLDistributed is eventually removed")
			waitResourceDeleted(ctx, nsn, &vmv1alpha1.VLDistributedList{})

			By("ensuring VLClusters and VLAgents are eventually removed")
			for _, zone := range cr.Spec.Zones {
				waitResourceDeleted(ctx, types.NamespacedName{Name: zone.VLAgent.Name, Namespace: namespace}, &vmv1.VLAgentList{})
				waitResourceDeleted(ctx, types.NamespacedName{Name: zone.VLCluster.Name, Namespace: namespace}, &vmv1.VLClusterList{})
			}
		})

		It("should delete VLDistributed but keep referenced VLClusters", func() {
			nsn.Name = "vld-keep-referenced"
			By("creating VLClusters to be referenced")

			zonesCount := 2
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			vlAgents := make([]*vmv1.VLAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vlClusterSpec := genVLClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
				vlAgents[i] = &vmv1.VLAgent{
					ObjectMeta: objMeta,
					Spec:       genVLAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			createVLAgents(ctx, &wg, k8sClient, vlAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					Retain: true,
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VLDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			By("creating the VLDistributed")
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			By("ensuring VLAgent was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlAgents[0].Name, Namespace: cr.Namespace}, &vmv1.VLAgent{})).ToNot(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vlAgents[1].Name, Namespace: cr.Namespace}, &vmv1.VLAgent{})).ToNot(HaveOccurred())

			By("ensuring VMAuth was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})).ToNot(HaveOccurred())

			By("deleting the VLDistributed")
			Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())

			By("ensuring VLAgent is not removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vlAgents[0].Name, Namespace: cr.Namespace}, &vmv1.VLAgent{})
			}, eventualDeletionTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vlAgents[1].Name, Namespace: cr.Namespace}, &vmv1.VLAgent{})
			}, eventualDeletionTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			By("ensuring VMAuth is not removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})
			}, eventualDeletionTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			By("ensuring VLDistributed is eventually removed")
			waitResourceDeleted(ctx, nsn, &vmv1alpha1.VLDistributedList{})

			By("ensuring VLCluster is not removed")
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[0].Name, Namespace: namespace}, &vmv1.VLCluster{})
			}, "10s", "1s").ShouldNot(HaveOccurred())
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vlClusters[1].Name, Namespace: namespace}, &vmv1.VLCluster{})
			}, "10s", "1s").ShouldNot(HaveOccurred())
		})

		It("should successfully create a VLDistributed with disabled VMAuth", func() {
			nsn.Name = "vld-disabled-vmauth"
			zonesCount := 1
			zs := make([]vmv1alpha1.VLDistributedZone, zonesCount)
			vlClusters := make([]*vmv1.VLCluster, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vlClusterSpec := genVLClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VLCluster.Spec = vlClusterSpec
				zs[i].VLCluster.Name = objMeta.Name
				zs[i].VLAgent.Name = objMeta.Name
				vlClusters[i] = &vmv1.VLCluster{
					ObjectMeta: objMeta,
					Spec:       vlClusterSpec,
				}
			}
			var wg sync.WaitGroup
			createVLClusters(ctx, &wg, k8sClient, vlClusters...)
			wg.Wait()

			cr := &vmv1alpha1.VLDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VLDistributedSpec{
					VMAuth: vmv1alpha1.VLDistributedAuth{
						Name:    nsn.Name,
						Enabled: ptr.To(false),
					},
					ZoneCommon: vmv1alpha1.VLDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					Zones: zs,
				},
			}
			By("creating the VLDistributed")
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			expectStatusAfterAction(ctx, &vmv1alpha1.VLDistributedList{}, nsn, eventualDistributedExpandingTimeout, func() {
				Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			}, vmv1beta1.UpdateStatusOperational)

			By("ensuring VMAuth was not created")
			err := k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})
})
