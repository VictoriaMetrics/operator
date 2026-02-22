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

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmdistributed"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

func genVMClusterSpec(opts ...func(*vmv1beta1.VMClusterSpec)) vmv1beta1.VMClusterSpec {
	commonApplicationDeploymentParams := vmv1beta1.CommonApplicationDeploymentParams{
		ReplicaCount: ptr.To[int32](1),
	}
	noReplicas := vmv1beta1.CommonApplicationDeploymentParams{
		ReplicaCount: ptr.To[int32](0),
	}

	s := vmv1beta1.VMClusterSpec{
		VMSelect: &vmv1beta1.VMSelect{
			CommonApplicationDeploymentParams: commonApplicationDeploymentParams,
		},
		VMInsert: &vmv1beta1.VMInsert{
			CommonApplicationDeploymentParams: noReplicas,
		},
		VMStorage: &vmv1beta1.VMStorage{
			CommonApplicationDeploymentParams: commonApplicationDeploymentParams,
		},
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// createVMClusters creates a VMCluster, sets up its deferred cleanup, and waits for it to become operational.
func createVMClusters(ctx context.Context, wg *sync.WaitGroup, k8sClient client.Client, vmClusters ...*vmv1beta1.VMCluster) {
	for i := range vmClusters {
		vmCluster := vmClusters[i]
		wg.Go(func() {
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, vmCluster)).ToNot(HaveOccurred())
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace}, &vmv1beta1.VMCluster{})
				}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
			})
			Expect(k8sClient.Create(ctx, vmCluster.DeepCopy())).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, vmCluster, types.NamespacedName{Name: vmCluster.Name, Namespace: vmCluster.Namespace})
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
		})
	}
}

func genVMAgentSpec() vmv1beta1.VMAgentSpec {
	return vmv1beta1.VMAgentSpec{
		CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
			ReplicaCount: ptr.To[int32](1),
		},
		RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
			{URL: "http://localhost"},
		},
	}
}

func createVMAgents(ctx context.Context, wg *sync.WaitGroup, k8sClient client.Client, vmAgents ...*vmv1beta1.VMAgent) {
	for i := range vmAgents {
		vmAgent := vmAgents[i]
		wg.Go(func() {
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, vmAgent)).ToNot(HaveOccurred())
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: vmAgent.Name, Namespace: vmAgent.Namespace}, &vmv1beta1.VMAgent{})
				}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
			})
			Expect(k8sClient.Create(ctx, vmAgent)).NotTo(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, vmAgent, types.NamespacedName{Name: vmAgent.Name, Namespace: vmAgent.Namespace})
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
		})
	}
}

func createVMAuth(ctx context.Context, wg *sync.WaitGroup, k8sClient client.Client, name, ns string) {
	By("creating VMAuth")
	wg.Go(func() {
		vmAuth := &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: ns,
				Name:      name,
			},
			Spec: vmv1beta1.VMAuthSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To[int32](1),
				},
				UserSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"vmd-users": name,
					},
				},
			},
		}
		DeferCleanup(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, vmAuth)).ToNot(HaveOccurred())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmAuth.Name, Namespace: ns}, &vmv1beta1.VMAuth{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		})
		Expect(k8sClient.Create(ctx, vmAuth)).NotTo(HaveOccurred())
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, vmAuth)
		}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
	})
}

func verifyOwnerReferences(ctx context.Context, cr *vmv1alpha1.VMDistributed, vmClusters []*vmv1beta1.VMCluster, namespace string) {
	GinkgoHelper()
	var fetchedVMCluster vmv1beta1.VMCluster
	for _, vmCluster := range vmClusters {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: namespace}, &fetchedVMCluster)).ToNot(HaveOccurred())
		Expect(fetchedVMCluster.GetOwnerReferences()).To(HaveLen(1))
		ownerRef := fetchedVMCluster.GetOwnerReferences()[0]
		Expect(ownerRef.Kind).To(Equal("VMDistributed"))
		Expect(ownerRef.APIVersion).To(Equal("operator.victoriametrics.com/v1alpha1"))
		Expect(ownerRef.Name).To(Equal(cr.Name))
	}
}

//nolint:dupl,lll
var _ = Describe("e2e VMDistributed", Label("vm", "vmdistributed"), func() {
	ctx := context.Background()

	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	nsn := types.NamespacedName{
		Namespace: namespace,
	}

	Context("create", func() {
		It("should successfully create a VMDistributed with inline VMAgent spec", func() {
			By("creating 2 VMClusters")
			nsn.Name = "vmd-inline-vmagent"
			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vmClusterSpec := genVMClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       vmClusterSpec,
				}
			}
			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VMDistributed with inline VMAgent spec")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAuth: vmv1alpha1.VMDistributedAuth{
						Name: nsn.Name,
					},
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
						VMAgent: vmv1alpha1.VMDistributedZoneAgent{
							Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
								CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
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
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			for i := range zs {
				z := &zs[i]
				var vmagent vmv1beta1.VMAgent
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: z.VMAgent.Name, Namespace: namespace}, &vmagent)
				}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
				Expect(vmagent.Spec.ReplicaCount).To(Equal(ptr.To[int32](2)))
				Expect(vmagent.OwnerReferences).To(HaveLen(1))
				Expect(vmagent.OwnerReferences[0].Name).To(Equal(cr.Name))
				Expect(vmagent.OwnerReferences[0].Kind).To(Equal("VMDistributed"))
				Expect(vmagent.OwnerReferences[0].UID).To(Equal(cr.UID))
			}

			var vmauth vmv1beta1.VMAuth
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &vmauth)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			Expect(vmauth.OwnerReferences).To(HaveLen(1))
			Expect(vmauth.OwnerReferences[0].Name).To(Equal(cr.Name))
			Expect(vmauth.OwnerReferences[0].Kind).To(Equal("VMDistributed"))
			Expect(vmauth.OwnerReferences[0].UID).To(Equal(cr.UID))
		})

		It("should successfully create a VMDistributed with inline VMCluster specs", func() {
			nsn.Name = "vmd-inline-vmcluster"
			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmClusterFn := []func(*vmv1beta1.VMClusterSpec){
				func(s *vmv1beta1.VMClusterSpec) {
					s.VMSelect.ReplicaCount = ptr.To[int32](1)
				},
				func(s *vmv1beta1.VMClusterSpec) {
					s.VMSelect.ReplicaCount = ptr.To[int32](2)
				},
			}
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				vmClusterSpec := genVMClusterSpec(vmClusterFn[i])
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       vmClusterSpec,
				}
			}
			var wg sync.WaitGroup
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			wg.Wait()

			By("creating a VMDistributed with inline VMCluster specs")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

			var inlineVMClusters []*vmv1beta1.VMCluster
			owner := cr.AsOwner()
			for _, zone := range cr.Spec.Zones {
				inlineVMClusters = append(inlineVMClusters, &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:            zone.VMCluster.Name,
						Namespace:       namespace,
						OwnerReferences: []metav1.OwnerReference{owner},
					},
				})
			}

			By("waiting for VMDistributed to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			verifyOwnerReferences(ctx, cr, inlineVMClusters, namespace)

			By("verifying that the inline VMClusters are created and operational")
			nsn := types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}
			vmCluster1 := &vmv1beta1.VMCluster{}
			Expect(k8sClient.Get(ctx, nsn, vmCluster1)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, vmCluster1, nsn)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			Expect(*vmCluster1.Spec.VMSelect.ReplicaCount).To(Equal(int32(1)))
			Expect(*vmCluster1.Spec.VMInsert.ReplicaCount).To(Equal(int32(0)))
			Expect(*vmCluster1.Spec.VMStorage.ReplicaCount).To(Equal(int32(1)))

			nsn = types.NamespacedName{Name: vmClusters[1].Name, Namespace: namespace}
			vmCluster2 := &vmv1beta1.VMCluster{}
			Expect(k8sClient.Get(ctx, nsn, vmCluster2)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, vmCluster2, nsn)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			Expect(*vmCluster2.Spec.VMSelect.ReplicaCount).To(Equal(int32(2)))
			Expect(*vmCluster2.Spec.VMInsert.ReplicaCount).To(Equal(int32(0)))
			Expect(*vmCluster2.Spec.VMStorage.ReplicaCount).To(Equal(int32(1)))
		})

		It("should successfully create a VMDistributed with inline VMAuth spec", func() {
			By("creating VMClusters")
			nsn.Name = "vmd-inline-vmauth"
			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				vmClusterSpec := genVMClusterSpec()
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       vmClusterSpec,
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}
			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			wg.Wait()

			By("creating a VMDistributed with inline VMAuth spec")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{
						Name: nsn.Name,
						Spec: vmv1beta1.VMAuthSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
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
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

			By("waiting for VMDistributed to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			By("verifying that the inline VMAuth is created and operational")
			var createdVMAuth vmv1beta1.VMAuth
			nsn = types.NamespacedName{Name: nsn.Name, Namespace: namespace}
			Expect(k8sClient.Get(ctx, nsn, &createdVMAuth)).ToNot(HaveOccurred())
			Expect(createdVMAuth.Spec.CommonApplicationDeploymentParams.ReplicaCount).To(HaveValue(Equal(int32(1))))

			By("verifying VMAuth has correct owner reference")
			Expect(createdVMAuth.GetOwnerReferences()).To(HaveLen(1))
			ownerRef := createdVMAuth.GetOwnerReferences()[0]
			Expect(ownerRef.Kind).To(Equal("VMDistributed"))
			Expect(ownerRef.Name).To(Equal(cr.Name))
		})

		It("should successfully create a VMDistributed with VMCluster references and override spec", func() {
			By("creating an initial VMClusters")
			nsn.Name = "vmd-override-clusters"
			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			vmClusterFn := []func(*vmv1beta1.VMClusterSpec){
				func(s *vmv1beta1.VMClusterSpec) {
					s.RetentionPeriod = "1w"
				},
				func(s *vmv1beta1.VMClusterSpec) {
					s.RetentionPeriod = "2w"
				},
			}
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = genVMClusterSpec(vmClusterFn[i])
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       genVMClusterSpec(),
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VMDistributed referencing the existing VMCluster with an override spec")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

			By("waiting for VMDistributed to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			verifyOwnerReferences(ctx, cr, vmClusters, namespace)

			By("verifying that the referenced VMClusters have the override applied")
			var updatedCluster1, updatedCluster2 vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &updatedCluster1)).ToNot(HaveOccurred())
			Expect(updatedCluster1.Spec.RetentionPeriod).To(Equal("1w"))

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[1].Name, Namespace: namespace}, &updatedCluster2)).ToNot(HaveOccurred())
			Expect(updatedCluster2.Spec.RetentionPeriod).To(Equal("2w"))
		})

		It("should apply global overrides before cluster-specific overrides", func() {
			By("creating initial VMClusters")
			nsn.Name = "vmd-global-override"

			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			vmClusterFn := []func(*vmv1beta1.VMClusterSpec){
				func(s *vmv1beta1.VMClusterSpec) {
					s.VMStorage.ReplicaCount = ptr.To[int32](1)
				},
				func(s *vmv1beta1.VMClusterSpec) {
					s.VMStorage.ReplicaCount = ptr.To[int32](2)
				},
			}
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = genVMClusterSpec(vmClusterFn[i])
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec: genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
						s.ClusterVersion = "v1.126.0-cluster"
						s.RetentionPeriod = "30d"
					}),
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VMDistributed with GlobalClusterSpec and cluster-specific overrides")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
						VMCluster: vmv1alpha1.VMDistributedZoneCluster{
							Name: "%ZONE%",
							Spec: genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
								s.RetentionPeriod = "60d"
							}),
						},
					},
					Zones: zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

			By("waiting for VMDistributed to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			verifyOwnerReferences(ctx, cr, vmClusters, namespace)

			By("verifying that both VMClusters have the global override applied")
			var updatedCluster1, updatedCluster2 vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &updatedCluster1)).ToNot(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[1].Name, Namespace: namespace}, &updatedCluster2)).ToNot(HaveOccurred())

			// Global overrides should be applied
			Expect(updatedCluster1.Spec.RetentionPeriod).To(Equal("60d"))
			Expect(updatedCluster2.Spec.RetentionPeriod).To(Equal("60d"))

			// Cluster-specific overrides should be applied after global overrides
			Expect(*updatedCluster1.Spec.VMStorage.ReplicaCount).To(Equal(int32(1)))
			Expect(*updatedCluster2.Spec.VMStorage.ReplicaCount).To(Equal(int32(2)))
		})

		It("should handle rolling updates with VMAgent configuration changes", func() {
			nsn.Name = "vmd-rolling-updates"

			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = genVMClusterSpec()
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       genVMClusterSpec(),
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					Zones: zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			verifyOwnerReferences(ctx, cr, vmClusters, namespace)

			// Update VMAgent
			Eventually(func() error {
				var obj vmv1alpha1.VMDistributed
				if err := k8sClient.Get(ctx, nsn, &obj); err != nil {
					return err
				}
				for i := range obj.Spec.Zones {
					z := &obj.Spec.Zones[i]
					z.VMAgent = vmv1alpha1.VMDistributedZoneAgent{Name: z.Name}
				}
				return k8sClient.Update(ctx, &obj)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())

			// Wait for VMDistributed to become operational after update
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
		})

		It("should wait for VMCluster upgrade completion", func() {
			initialVersion := "v1.126.0-cluster"
			updateVersion := "v1.127.0-cluster"

			nsn.Name = "vmd-wait-for-upgrade"

			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vmClusterSpec := genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
					s.ClusterVersion = initialVersion
				})
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       vmClusterSpec,
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			verifyOwnerReferences(ctx, cr, vmClusters, namespace)

			// Apply spec update
			Expect(k8sClient.Get(ctx, nsn, cr)).ToNot(HaveOccurred())
			cr.Spec.Zones[0].VMCluster.Spec.ClusterVersion = updateVersion
			cr.Spec.Zones[1].VMCluster.Spec.ClusterVersion = updateVersion
			Expect(k8sClient.Update(ctx, cr)).ToNot(HaveOccurred())
			// Wait for VMDistributed to start expanding after its own upgrade
			Eventually(func() error {
				return expectObjectStatusExpanding(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			// Wait for VMDistributed to become operational after its own upgrade
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			// Verify VMDistributed status reflects both clusters are upgraded/operational
			var newVMCluster1, newVMCluster2 vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &newVMCluster1)).ToNot(HaveOccurred())
			Expect(newVMCluster1.Spec.ClusterVersion).To(Equal(updateVersion))
			Expect(newVMCluster1.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[1].Name, Namespace: namespace}, &newVMCluster2)).ToNot(HaveOccurred())
			Expect(newVMCluster2.Spec.ClusterVersion).To(Equal(updateVersion))
			Expect(newVMCluster2.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
		})

		It("should skip reconciliation when VMDistributed is paused", func() {
			By("creating a VMCluster")
			initialVersion := "v1.126.0-cluster"
			nsn.Name = "vmd-skip-when-paused"

			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vmClusterSpec := genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
					s.ClusterVersion = initialVersion
				})
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       vmClusterSpec,
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VMDistributed")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			verifyOwnerReferences(ctx, cr, vmClusters, namespace)

			By("pausing the VMDistributed")
			// Re-fetch the latest VMDistributed object to avoid conflict errors
			Eventually(func() error {
				err := k8sClient.Get(ctx, nsn, cr)
				if err != nil {
					return err
				}
				cr.Spec.Paused = true
				return k8sClient.Update(ctx, cr)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusPaused(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())

			By("attempting to scale the VMCluster while paused")
			var vmCluster1 vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &vmCluster1)).ToNot(HaveOccurred())
			initialReplicas := *vmCluster1.Spec.VMStorage.ReplicaCount

			// Re-fetch the latest VMDistributed object to avoid conflict errors
			Eventually(func() error {
				err := k8sClient.Get(ctx, nsn, cr)
				if err != nil {
					return err
				}
				cr.Spec.Zones[0].VMCluster.Spec = vmv1beta1.VMClusterSpec{
					VMInsert: cr.Spec.Zones[0].VMCluster.Spec.VMInsert,
					VMSelect: cr.Spec.Zones[0].VMCluster.Spec.VMSelect,
					VMStorage: &vmv1beta1.VMStorage{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](initialReplicas + 1),
						},
					},
				}
				return k8sClient.Update(ctx, cr)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())

			Consistently(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &vmCluster1)).ToNot(HaveOccurred())
				return *vmCluster1.Spec.VMStorage.ReplicaCount
			}, "10s", "1s").Should(Equal(initialReplicas))

			By("unpausing the VMDistributed")
			// Re-fetch the latest VMDistributed object to avoid conflict errors
			Eventually(func() error {
				err := k8sClient.Get(ctx, nsn, cr)
				if err != nil {
					return err
				}
				cr.Spec.Paused = false
				return k8sClient.Update(ctx, cr)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())

			Eventually(func() error {
				return expectObjectStatusExpanding(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())

			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())

			By("verifying reconciliation resumes after unpausing")
			Eventually(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &vmCluster1)).ToNot(HaveOccurred())
				return *vmCluster1.Spec.VMStorage.ReplicaCount
			}, eventualDeploymentAppReadyTimeout).Should(Equal(initialReplicas + 1))
		})

		It("should be idempotent when calling CreateOrUpdate multiple times", func() {
			const (
				attempts = 3
			)

			By("creating a VMCluster for idempotency test")
			nsn.Name = "vmd-idempotent"

			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vmClusterSpec := genVMClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       vmClusterSpec,
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VMDistributed with this VMCluster")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())

			// Capture resource versions of key child resources
			var beforeCluster vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &beforeCluster)).ToNot(HaveOccurred())
			clusterRV := beforeCluster.ResourceVersion

			var beforeAgent vmv1beta1.VMAgent
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmAgents[0].Name, Namespace: namespace}, &beforeAgent)).ToNot(HaveOccurred())
			agentRV := beforeAgent.ResourceVersion

			var beforeVMAuth vmv1beta1.VMAuth
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &beforeVMAuth)).ToNot(HaveOccurred())
			authDeploymentSpec := beforeVMAuth.Spec
			// Call CreateOrUpdate multiple times and ensure it succeeds and doesn't modify child resources
			for i := 0; i < attempts; i++ {
				var latestCR vmv1alpha1.VMDistributed
				Expect(k8sClient.Get(ctx, nsn, &latestCR)).ToNot(HaveOccurred())
				// Ensure that TypeMeta is set for ownership check to pass
				latestCR.Kind = "VMDistributed"
				latestCR.APIVersion = vmv1alpha1.GroupVersion.String()
				k8sClient.Scheme().Default(&latestCR)
				Expect(vmdistributed.CreateOrUpdate(ctx, &latestCR, k8sClient)).ToNot(HaveOccurred())
			}

			// Verify resource versions have not changed (no updates performed)
			var afterCluster vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &afterCluster)).ToNot(HaveOccurred())
			Expect(afterCluster.ResourceVersion).To(Equal(clusterRV), "VMCluster resource version should not change")

			var afterAgent vmv1beta1.VMAgent
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmAgents[0].Name, Namespace: namespace}, &afterAgent)).ToNot(HaveOccurred())
			Expect(afterAgent.ResourceVersion).To(Equal(agentRV), "VMAgent resource version should not change")

			var afterVMAuth vmv1beta1.VMAuth
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &afterVMAuth)).ToNot(HaveOccurred())
			Expect(afterVMAuth.Spec).To(Equal(authDeploymentSpec), "VMAuthLB Deployment spec should not change")
		})

		It("should transition from Expanding to Operational without entering Failing state", func() {
			By("creating a VMCluster")
			nsn.Name = "vmd-transition-without-failure"

			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vmClusterSpec := genVMClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       vmClusterSpec,
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			By("creating a VMDistributed")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}

			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})

			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

			By("waiting for Operational status while ensuring no Failing status occurs")
			Eventually(func() error {
				var fetchedCR vmv1alpha1.VMDistributed
				if err := k8sClient.Get(ctx, nsn, &fetchedCR); err != nil {
					return err
				}
				if fetchedCR.Status.UpdateStatus == vmv1beta1.UpdateStatusFailed {
					return StopTrying("VMDistributed entered Failing state")
				}
				if fetchedCR.Status.UpdateStatus != vmv1beta1.UpdateStatusOperational {
					return fmt.Errorf("waiting for Operational status, current: %s", fetchedCR.Status.UpdateStatus)
				}
				return nil
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
		})
	})

	Context("fail", func() {
		DescribeTable("should fail when creating VMDistributed", func(cr *vmv1alpha1.VMDistributed) {
			nsn.Name = cr.Name
			cr.Spec.VMAuth.Name = cr.Name
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn, vmv1beta1.UpdateStatusFailed)
			}, eventualDistributedExpandingTimeout).ShouldNot(HaveOccurred())
		},
			Entry("with invalid VMCluster", &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-cluster",
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							Name: "test",
							VMCluster: vmv1alpha1.VMDistributedZoneCluster{
								Name: "missing-cluster",
							},
						},
					},
				},
			}),
			Entry("with zone spec but missing zone name", &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "zone-spec-missing-name",
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: vmv1alpha1.VMDistributedZoneCluster{
								Spec: genVMClusterSpec(),
							},
						},
					},
				},
			}),
		)
	})

	Context("delete", func() {
		It("should delete VMDistributed and remove it from the cluster", func() {
			nsn.Name = "vmd-remove"

			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vmClusterSpec := genVMClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
			}

			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{
						Name: nsn.Name,
						Spec: vmv1beta1.VMAuthSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
					Zones: zs,
				},
			}
			By("creating the VMDistributed")
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, types.NamespacedName{Name: nsn.Name, Namespace: namespace})
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			By("ensuring VMAuth was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})).ToNot(HaveOccurred())

			By("ensuring VMClusters and VMAgents were created")
			for _, zone := range cr.Spec.Zones {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: zone.VMAgent.Name, Namespace: namespace}, &vmv1beta1.VMAgent{})).ToNot(HaveOccurred())
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: zone.VMCluster.Name, Namespace: namespace}, &vmv1beta1.VMCluster{})).ToNot(HaveOccurred())
			}

			By("deleting the VMDistributed")
			Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())

			By("ensuring VMAuth is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

			By("ensuring VMDistributed is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, nsn, &vmv1alpha1.VMDistributed{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

			By("ensuring VMClusters and VMAgents are eventually removed")
			for _, zone := range cr.Spec.Zones {
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: zone.VMAgent.Name, Namespace: namespace}, &vmv1beta1.VMAgent{})
				}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: zone.VMCluster.Name, Namespace: namespace}, &vmv1beta1.VMCluster{})
				}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
			}
		})

		It("should delete VMDistributed but keep referenced VMClusters", func() {
			nsn.Name = "vmd-keep-referenced"
			By("creating VMClusters to be referenced")

			zonesCount := 2
			zs := make([]vmv1alpha1.VMDistributedZone, zonesCount)
			vmClusters := make([]*vmv1beta1.VMCluster, zonesCount)
			vmAgents := make([]*vmv1beta1.VMAgent, zonesCount)
			for i := range zs {
				objMeta := metav1.ObjectMeta{
					Namespace: namespace,
					Name:      fmt.Sprintf("%s-%d", nsn.Name, i+1),
				}
				vmClusterSpec := genVMClusterSpec()
				zs[i].Name = objMeta.Name
				zs[i].VMCluster.Spec = vmClusterSpec
				zs[i].VMCluster.Name = objMeta.Name
				zs[i].VMAgent.Name = objMeta.Name
				vmClusters[i] = &vmv1beta1.VMCluster{
					ObjectMeta: objMeta,
					Spec:       vmClusterSpec,
				}
				vmAgents[i] = &vmv1beta1.VMAgent{
					ObjectMeta: objMeta,
					Spec:       genVMAgentSpec(),
				}
			}

			var wg sync.WaitGroup
			createVMClusters(ctx, &wg, k8sClient, vmClusters...)
			createVMAgents(ctx, &wg, k8sClient, vmAgents...)
			createVMAuth(ctx, &wg, k8sClient, nsn.Name, namespace)
			wg.Wait()

			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					Retain: true,
					ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
						ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
						UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					},
					VMAuth: vmv1alpha1.VMDistributedAuth{Name: nsn.Name},
					Zones:  zs,
				},
			}
			By("creating the VMDistributed")
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())
			})
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, types.NamespacedName{Name: nsn.Name, Namespace: namespace})
			}, eventualDistributedExpandingTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			By("ensuring VMAgent was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmAgents[0].Name, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})).ToNot(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmAgents[1].Name, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})).ToNot(HaveOccurred())

			By("ensuring VMAuth was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})).ToNot(HaveOccurred())

			By("deleting the VMDistributed")
			Expect(finalize.SafeDelete(ctx, k8sClient, cr)).ToNot(HaveOccurred())

			By("ensuring VMAgent is not removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmAgents[0].Name, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})
			}, eventualDeletionTimeout).WithContext(ctx).ShouldNot(HaveOccurred())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmAgents[1].Name, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})
			}, eventualDeletionTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			By("ensuring VMAuth is not removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})
			}, eventualDeletionTimeout).WithContext(ctx).ShouldNot(HaveOccurred())

			By("ensuring VMDistributed is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, nsn, &vmv1alpha1.VMDistributed{})
			}, eventualDeletionTimeout).WithContext(ctx).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

			By("ensuring VMCluster is not removed")
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[0].Name, Namespace: namespace}, &vmv1beta1.VMCluster{})
			}, "10s", "1s").ShouldNot(HaveOccurred())
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmClusters[1].Name, Namespace: namespace}, &vmv1beta1.VMCluster{})
			}, "10s", "1s").ShouldNot(HaveOccurred())
		})
	})
})
