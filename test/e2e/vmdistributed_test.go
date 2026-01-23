package e2e

import (
	"context"
	"fmt"
	"os"
	"time"

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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmdistributed"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

var (
	expectedVMAgentToBeRemoved = false
	expectedVMAuthToBeRemoved  = false
)

func genVMClusterSpec(opts ...func(*vmv1beta1.VMClusterSpec)) *vmv1beta1.VMClusterSpec {
	commonApplicationDeploymentParams := vmv1beta1.CommonApplicationDeploymentParams{
		ReplicaCount: ptr.To[int32](1),
	}
	noReplicas := vmv1beta1.CommonApplicationDeploymentParams{
		ReplicaCount: ptr.To[int32](0),
	}

	s := &vmv1beta1.VMClusterSpec{
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
		opt(s)
	}
	return s
}

// createVMClustersAndEnsureOperational creates a VMCluster, sets up its deferred cleanup, and waits for it to become operational.
func createVMClustersAndEnsureOperational(ctx context.Context, k8sClient client.Client, ns string, vmclusters ...*vmv1beta1.VMCluster) {
	for _, vmcluster := range vmclusters {
		DeferCleanup(func() {
			Expect(finalize.SafeDelete(ctx, k8sClient, vmcluster)).To(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmcluster.Name, Namespace: ns}, &vmv1beta1.VMCluster{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
		})
		Expect(k8sClient.Create(ctx, vmcluster.DeepCopy())).To(Succeed())
		Eventually(func() error {
			return expectObjectStatusOperational(ctx, k8sClient, vmcluster, types.NamespacedName{Name: vmcluster.Name, Namespace: ns})
		}, eventualDistributedExpandingTimeout).Should(Succeed())
	}
}

func createVMUser(ctx context.Context, k8sClient client.Client, name, namespace string, vmclusters []*vmv1beta1.VMCluster) {
	targetRefs := []vmv1beta1.TargetRef{}
	for _, vmCluster := range vmclusters {
		targetRefs = append(targetRefs, vmv1beta1.TargetRef{
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Namespace: namespace,
				Name:      vmCluster.Name,
			},
		})
	}

	By("creating VMUser for VMAuth cache")
	vmUser := &vmv1beta1.VMUser{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"vmd-users": name,
			},
		},
		Spec: vmv1beta1.VMUserSpec{
			Username:   ptr.To(name),
			TargetRefs: targetRefs,
		},
	}
	DeferCleanup(func() {
		Expect(k8sClient.Delete(ctx, vmUser)).NotTo(HaveOccurred())
	})
	Expect(k8sClient.Create(ctx, vmUser)).NotTo(HaveOccurred())
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: vmUser.Name, Namespace: vmUser.Namespace}, vmUser)
	}, eventualDistributedExpandingTimeout).Should(Succeed())
}

func createVMAuth(ctx context.Context, k8sClient client.Client, name, namespace string) {
	By("creating VMAuth")
	vmAuth := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
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
	// Reset expectedVMAuthToBeRemoved so that it would not leak into other tests
	DeferCleanup(func() {
		expectedVMAuthToBeRemoved = false
	})
	DeferCleanup(func() {
		err := k8sClient.Delete(ctx, vmAuth)
		if expectedVMAuthToBeRemoved {
			Expect(k8serrors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("expected NotFound error for %s, but got %v", vmAuth.Name, err))
		} else {
			Expect(err).NotTo(HaveOccurred())
		}
	})
	Expect(k8sClient.Create(ctx, vmAuth)).NotTo(HaveOccurred())
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, vmAuth)
	}, eventualDistributedExpandingTimeout).Should(Succeed())
}

func createVMAgent(ctx context.Context, k8sClient client.Client, name, namespace string) {
	By("creating VMAgent")
	vmAgent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: vmv1beta1.VMAgentSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To[int32](1),
			},
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: "http://localhost"},
			},
		},
	}
	// Reset expectedVMAuthToBeRemoved so that it would not leak into other tests
	DeferCleanup(func() {
		expectedVMAgentToBeRemoved = false
	})
	DeferCleanup(func() {
		err := k8sClient.Delete(ctx, vmAgent)
		if expectedVMAgentToBeRemoved {
			Expect(k8serrors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("expected NotFound error for %s, but got %v", vmAgent.Name, err))
		} else {
			Expect(err).NotTo(HaveOccurred())
		}
	})
	Expect(k8sClient.Create(ctx, vmAgent)).NotTo(HaveOccurred())
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, vmAgent)
	}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())
}

func verifyOwnerReferences(ctx context.Context, cr *vmv1alpha1.VMDistributed, vmclusters []*vmv1beta1.VMCluster, namespace string) {
	var fetchedVMCluster vmv1beta1.VMCluster
	for _, vmcluster := range vmclusters {
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmcluster.Name, Namespace: namespace}, &fetchedVMCluster)).To(Succeed())
		Expect(fetchedVMCluster.GetOwnerReferences()).To(HaveLen(1))
		ownerRef := fetchedVMCluster.GetOwnerReferences()[0]
		Expect(ownerRef.Kind).To(Equal("VMDistributed"))
		Expect(ownerRef.APIVersion).To(Equal("operator.victoriametrics.com/v1alpha1"))
		Expect(ownerRef.Name).To(Equal(cr.Name))
	}
}

var _ = Describe("e2e VMDistributed", Label("vm", "vmdistributed"), func() {
	ctx := context.Background()

	// This env var is required to make vmagent metrics checker ignore errors
	// as the test runs outside of the cluster and has no access to pod metrics
	os.Setenv("E2E_TEST", "true")

	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	nsn := types.NamespacedName{
		Namespace: namespace,
	}

	Context("create", func() {
		It("should successfully create a VMDistributed with inline VMAgent spec", func() {
			By("creating 2 VMClusters")
			nsn.Name = "vmd-inline-vmagent"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)

			By("creating a VMDistributed with inline VMAgent spec")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{
						Name: nsn.Name,
						Spec: &vmv1alpha1.VMDistributedAgentSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](2),
							},
						},
					},
					VMAuth: vmv1alpha1.VMAuthNameAndSpec{
						Name: nsn.Name,
					},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[0].Name},
							},
						},
						{

							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[1].Name},
							},
						},
					},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())

			var vmagent vmv1beta1.VMAgent
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &vmagent)
			}, eventualDistributedExpandingTimeout).Should(Succeed())
			Expect(vmagent.Spec.ReplicaCount).To(Equal(ptr.To[int32](2)))
			Expect(vmagent.OwnerReferences).To(HaveLen(1))
			Expect(vmagent.OwnerReferences[0].Name).To(Equal(cr.Name))
			Expect(vmagent.OwnerReferences[0].Kind).To(Equal("VMDistributed"))
			Expect(vmagent.OwnerReferences[0].UID).To(Equal(cr.UID))

			var vmauth vmv1beta1.VMAuth
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &vmauth)
			}, eventualDistributedExpandingTimeout).Should(Succeed())
			Expect(vmauth.OwnerReferences).To(HaveLen(1))
			Expect(vmauth.OwnerReferences[0].Name).To(Equal(cr.Name))
			Expect(vmauth.OwnerReferences[0].Kind).To(Equal("VMDistributed"))
			Expect(vmauth.OwnerReferences[0].UID).To(Equal(cr.UID))
		})

		It("should successfully create a VMDistributed with inline VMCluster specs", func() {
			nsn.Name = "vmd-inline-vmcluster"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
				},
			}
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			By("creating a VMDistributed with inline VMCluster specs")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:               vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Name: vmclusters[0].Name,
								Spec: genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
									s.VMSelect.ReplicaCount = ptr.To[int32](2)
								}),
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Name: vmclusters[1].Name,
								Spec: genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
									s.VMSelect.ReplicaCount = ptr.To[int32](3)
								}),
							},
						},
					},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			var inlineVMClusters []*vmv1beta1.VMCluster
			for _, zone := range cr.Spec.Zones {
				inlineVMClusters = append(inlineVMClusters, &vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.VMCluster.Name,
						Namespace: namespace,
					},
				})
			}

			By("waiting for VMDistributed to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, inlineVMClusters, namespace)

			By("verifying that the inline VMClusters are created and operational")
			nsn := types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}
			vmCluster1 := &vmv1beta1.VMCluster{}
			Expect(k8sClient.Get(ctx, nsn, vmCluster1)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, vmCluster1, nsn)
			}, eventualDistributedExpandingTimeout).Should(Succeed())
			Expect(*vmCluster1.Spec.VMSelect.ReplicaCount).To(Equal(int32(2)))
			Expect(*vmCluster1.Spec.VMInsert.ReplicaCount).To(Equal(int32(0)))
			Expect(*vmCluster1.Spec.VMStorage.ReplicaCount).To(Equal(int32(1)))

			nsn = types.NamespacedName{Name: vmclusters[1].Name, Namespace: namespace}
			vmCluster2 := &vmv1beta1.VMCluster{}
			Expect(k8sClient.Get(ctx, nsn, vmCluster2)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, vmCluster2, nsn)
			}, eventualDistributedExpandingTimeout).Should(Succeed())
			Expect(*vmCluster2.Spec.VMSelect.ReplicaCount).To(Equal(int32(3)))
			Expect(*vmCluster2.Spec.VMInsert.ReplicaCount).To(Equal(int32(0)))
			Expect(*vmCluster2.Spec.VMStorage.ReplicaCount).To(Equal(int32(1)))
		})

		It("should successfully create a VMDistributed with inline VMAuth spec", func() {
			By("creating VMClusters")
			nsn.Name = "vmd-inline-vmauth"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			By("creating a VMDistributed with inline VMAuth spec")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth: vmv1alpha1.VMAuthNameAndSpec{
						Name: nsn.Name,
						Spec: &vmv1beta1.VMAuthSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[0].Name},
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[1].Name},
							},
						},
					},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			By("waiting for VMDistributed to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())

			By("verifying that the inline VMAuth is created and operational")
			var createdVMAuth vmv1beta1.VMAuth
			nsn = types.NamespacedName{Name: nsn.Name, Namespace: namespace}
			Expect(k8sClient.Get(ctx, nsn, &createdVMAuth)).To(Succeed())
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
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			By("creating a VMDistributed referencing the existing VMCluster with an override spec")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:               vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[0].Name},
								Spec: &vmv1beta1.VMClusterSpec{
									RetentionPeriod: "1w",
								},
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[1].Name},
								Spec: &vmv1beta1.VMClusterSpec{
									RetentionPeriod: "2w",
								},
							},
						},
					},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			By("waiting for VMDistributed to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, vmclusters, namespace)

			By("verifying that the referenced VMClusters have the override applied")
			var updatedCluster1, updatedCluster2 vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &updatedCluster1)).To(Succeed())
			Expect(updatedCluster1.Spec.RetentionPeriod).To(Equal("1w"))

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[1].Name, Namespace: namespace}, &updatedCluster2)).To(Succeed())
			Expect(updatedCluster2.Spec.RetentionPeriod).To(Equal("2w"))
		})

		It("should apply global overrides before cluster-specific overrides", func() {
			By("creating initial VMClusters")
			nsn.Name = "vmd-global-override"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
						s.ClusterVersion = "v1.126.0-cluster"
						s.RetentionPeriod = "30d"
					}),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
						s.ClusterVersion = "v1.126.0-cluster"
						s.RetentionPeriod = "30d"
					}),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			By("creating a VMDistributed with GlobalClusterSpec and cluster-specific overrides")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:               vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
					CommonZone: vmv1alpha1.VMDistributedZone{
						VMCluster: &vmv1alpha1.VMClusterObjOrRef{
							Ref: &corev1.LocalObjectReference{Name: "%ZONE%"},
							Spec: &vmv1beta1.VMClusterSpec{
								RetentionPeriod: "60d",
							},
						},
					},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							Name: vmclusters[0].Name,
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Spec: &vmv1beta1.VMClusterSpec{
									VMStorage: &vmv1beta1.VMStorage{
										CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
											ReplicaCount: ptr.To[int32](2),
										},
									},
								},
							},
						},
						{
							Name: vmclusters[1].Name,
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Spec: &vmv1beta1.VMClusterSpec{
									VMStorage: &vmv1beta1.VMStorage{
										CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
											ReplicaCount: ptr.To[int32](3),
										},
									},
								},
							},
						},
					},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			By("waiting for VMDistributed to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, vmclusters, namespace)

			By("verifying that both VMClusters have the global override applied")
			var updatedCluster1, updatedCluster2 vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &updatedCluster1)).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[1].Name, Namespace: namespace}, &updatedCluster2)).To(Succeed())

			// Global overrides should be applied
			Expect(updatedCluster1.Spec.RetentionPeriod).To(Equal("60d"))
			Expect(updatedCluster2.Spec.RetentionPeriod).To(Equal("60d"))

			// Cluster-specific overrides should be applied after global overrides
			Expect(*updatedCluster1.Spec.VMStorage.ReplicaCount).To(Equal(int32(2)))
			Expect(*updatedCluster2.Spec.VMStorage.ReplicaCount).To(Equal(int32(3)))
		})

		It("should handle rolling updates with VMAgent configuration changes", func() {
			nsn.Name = "vmd-rolling-updates"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{
									Name: vmclusters[0].Name,
								},
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{
									Name: vmclusters[1].Name,
								},
							},
						},
					},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, vmclusters, namespace)

			// Update VMAgent
			Eventually(func() error {
				var obj vmv1alpha1.VMDistributed
				if err := k8sClient.Get(ctx, nsn, &obj); err != nil {
					return err
				}
				obj.Spec.VMAgent = vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name}
				return k8sClient.Update(ctx, &obj)
			}, eventualDistributedExpandingTimeout).Should(Succeed())

			// Wait for VMDistributed to become operational after update
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())
		})

		It("should wait for VMCluster upgrade completion", func() {
			initialVersion := "v1.126.0-cluster"
			updateVersion := "v1.127.0-cluster"

			nsn.Name = "vmd-wait-for-upgrade"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
						s.ClusterVersion = initialVersion
					}),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
						s.ClusterVersion = initialVersion
					}),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:               vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{
									Name: vmclusters[0].Name,
								},
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{
									Name: vmclusters[1].Name,
								},
							},
						},
					},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, vmclusters, namespace)

			// Apply spec update
			Expect(k8sClient.Get(ctx, nsn, cr)).To(Succeed())
			cr.Spec.Zones[0].VMCluster.Spec = &vmv1beta1.VMClusterSpec{
				ClusterVersion: updateVersion,
			}
			cr.Spec.Zones[1].VMCluster.Spec = &vmv1beta1.VMClusterSpec{
				ClusterVersion: updateVersion,
			}
			Expect(k8sClient.Update(ctx, cr)).To(Succeed())
			// Wait for VMDistributed to start expanding after its own upgrade
			Eventually(func() error {
				return expectObjectStatusExpanding(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())

			// Wait for VMDistributed to become operational after its own upgrade
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())

			// Verify VMDistributed status reflects both clusters are upgraded/operational
			var newVMCluster1, newVMCluster2 vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &newVMCluster1)).To(Succeed())
			Expect(newVMCluster1.Spec.ClusterVersion).To(Equal(updateVersion))
			Expect(newVMCluster1.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[1].Name, Namespace: namespace}, &newVMCluster2)).To(Succeed())
			Expect(newVMCluster2.Spec.ClusterVersion).To(Equal(updateVersion))
			Expect(newVMCluster2.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
		})

		It("should skip reconciliation when VMDistributed is paused", func() {
			By("creating a VMCluster")
			initialVersion := "v1.126.0-cluster"
			nsn.Name = "vmd-skip-when-paused"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
						s.ClusterVersion = initialVersion
					}),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(func(s *vmv1beta1.VMClusterSpec) {
						s.ClusterVersion = initialVersion
					}),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			By("creating a VMDistributed")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:               vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[0].Name},
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[1].Name},
							},
						},
					},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).Should(Succeed())
			verifyOwnerReferences(ctx, cr, vmclusters, namespace)

			By("pausing the VMDistributed")
			// Re-fetch the latest VMDistributed object to avoid conflict errors
			Eventually(func() error {
				err := k8sClient.Get(ctx, nsn, cr)
				if err != nil {
					return err
				}
				cr.Spec.Paused = true
				return k8sClient.Update(ctx, cr)
			}, eventualDistributedExpandingTimeout).Should(Succeed())

			By("attempting to scale the VMCluster while paused")
			var vmcluster1 vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &vmcluster1)).To(Succeed())
			initialReplicas := *vmcluster1.Spec.VMStorage.ReplicaCount

			// Re-fetch the latest VMDistributed object to avoid conflict errors
			Eventually(func() error {
				err := k8sClient.Get(ctx, nsn, cr)
				if err != nil {
					return err
				}
				cr.Spec.Zones[0].VMCluster.Spec = &vmv1beta1.VMClusterSpec{
					VMStorage: &vmv1beta1.VMStorage{
						CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](initialReplicas + 1),
						},
					},
				}
				return k8sClient.Update(ctx, cr)
			}, eventualDistributedExpandingTimeout).Should(Succeed())

			Consistently(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &vmcluster1)).To(Succeed())
				return *vmcluster1.Spec.VMStorage.ReplicaCount
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
			}, eventualDistributedExpandingTimeout).Should(Succeed())

			Eventually(func() error {
				return expectObjectStatusExpanding(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).Should(Succeed())

			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).Should(Succeed())

			By("verifying reconciliation resumes after unpausing")
			Eventually(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &vmcluster1)).To(Succeed())
				return *vmcluster1.Spec.VMStorage.ReplicaCount
			}, eventualDeploymentAppReadyTimeout).Should(Equal(initialReplicas + 1))
		})

		It("should be idempotent when calling CreateOrUpdate multiple times", func() {
			const (
				attempts    = 3
				httpTimeout = 1 * time.Second
			)

			By("creating a VMCluster for idempotency test")
			nsn.Name = "vmd-idempotent"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			By("creating a VMDistributed with this VMCluster")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:               vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[0].Name},
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[1].Name},
							},
						},
					},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn)
			}, eventualDistributedExpandingTimeout).Should(Succeed())

			// Capture resource versions of key child resources
			var beforeCluster vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &beforeCluster)).To(Succeed())
			clusterRV := beforeCluster.ResourceVersion

			var beforeAgent vmv1beta1.VMAgent
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &beforeAgent)).To(Succeed())
			agentRV := beforeAgent.ResourceVersion

			var beforeVMAuth vmv1beta1.VMAuth
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &beforeVMAuth)).To(Succeed())
			authDeploymentSpec := beforeVMAuth.Spec
			// Call CreateOrUpdate multiple times and ensure it succeeds and doesn't modify child resources
			for i := 0; i < attempts; i++ {
				var latestCR vmv1alpha1.VMDistributed
				Expect(k8sClient.Get(ctx, nsn, &latestCR)).To(Succeed())
				// Ensure that TypeMeta is set for ownership check to pass
				latestCR.Kind = "VMDistributed"
				latestCR.APIVersion = vmv1alpha1.GroupVersion.String()
				k8sClient.Scheme().Default(&latestCR)
				Expect(vmdistributed.CreateOrUpdate(ctx, &latestCR, k8sClient, httpTimeout)).To(Succeed())
			}

			// Verify resource versions have not changed (no updates performed)
			var afterCluster vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &afterCluster)).To(Succeed())
			Expect(afterCluster.ResourceVersion).To(Equal(clusterRV), "VMCluster resource version should not change")

			var afterAgent vmv1beta1.VMAgent
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &afterAgent)).To(Succeed())
			Expect(afterAgent.ResourceVersion).To(Equal(agentRV), "VMAgent resource version should not change")

			var afterVMAuth vmv1beta1.VMAuth
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: namespace}, &afterVMAuth)).To(Succeed())
			Expect(afterVMAuth.Spec).To(Equal(authDeploymentSpec), "VMAuthLB Deployment spec should not change")
		})

		It("should transition from Expanding to Operational without entering Failing state", func() {
			By("creating a VMCluster")
			nsn.Name = "vmd-transition-without-failure"
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			By("creating a VMDistributed")
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:               vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[0].Name},
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[1].Name},
							},
						},
					},
				},
			}

			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})

			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

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
			}, eventualDistributedExpandingTimeout, "1s").Should(Succeed())
		})
	})

	Context("fail", func() {
		DescribeTable("should fail when creating VMDistributed", func(cr *vmv1alpha1.VMDistributed) {
			nsn.Name = cr.Name
			cr.Spec.VMAgent.Name = cr.Name
			cr.Spec.VMAuth.Name = cr.Name
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, nsn, vmv1beta1.UpdateStatusFailed)
			}, eventualDistributedExpandingTimeout).Should(Succeed())
		},
			Entry("with invalid VMCluster", &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-cluster",
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 10 * time.Second},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{
									Name: "missing-cluster",
								},
							},
						},
					},
				},
			}),
			Entry("with zone spec but missing name", &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "zone-spec-missing-name",
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 10 * time.Second},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Spec: genVMClusterSpec(),
							},
						},
					},
				},
			}),
			Entry("with zone missing spec and ref", &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "zone-missing-spec-ref",
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 10 * time.Second},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{},
						},
					},
				},
			}),
			Entry("with zone having both spec and ref", &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "zone-both-spec-ref",
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 10 * time.Second},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{
									Name: "vmcluster-existing",
								},
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
			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{
						Name: nsn.Name,
						Spec: &vmv1alpha1.VMDistributedAgentSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
					VMAuth: vmv1alpha1.VMAuthNameAndSpec{
						Name: nsn.Name,
						Spec: &vmv1beta1.VMAuthSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Name: nsn.Name + "-1",
								Spec: genVMClusterSpec(),
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Name: nsn.Name + "-2",
								Spec: genVMClusterSpec(),
							},
						},
					},
				},
			}
			By("creating the VMDistributed")
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, types.NamespacedName{Name: nsn.Name, Namespace: namespace})
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())

			By("ensuring VMAgent was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})).To(Succeed())

			By("ensuring VMAuth was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})).To(Succeed())

			By("ensuring VMClusters were created")
			for _, zone := range cr.Spec.Zones {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: zone.VMCluster.Name, Namespace: namespace}, &vmv1beta1.VMCluster{})).To(Succeed())
			}

			By("deleting the VMDistributed")
			Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())

			By("ensuring VMAgent is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

			By("ensuring VMAuth is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

			By("ensuring VMDistributed is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, nsn, &vmv1alpha1.VMDistributed{})
			}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

			By("ensuring VMClusters are eventually removed")
			for _, zone := range cr.Spec.Zones {
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: zone.VMCluster.Name, Namespace: namespace}, &vmv1beta1.VMCluster{})
				}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
			}
		})

		It("should delete VMDistributed but keep referenced VMClusters", func() {
			nsn.Name = "vmd-keep-referenced"
			By("creating VMClusters to be referenced")
			vmclusters := []*vmv1beta1.VMCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-1",
					},
					Spec: *genVMClusterSpec(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      nsn.Name + "-2",
					},
					Spec: *genVMClusterSpec(),
				},
			}
			createVMClustersAndEnsureOperational(ctx, k8sClient, namespace, vmclusters...)
			createVMUser(ctx, k8sClient, nsn.Name, namespace, vmclusters)
			createVMAuth(ctx, k8sClient, nsn.Name, namespace)
			createVMAgent(ctx, k8sClient, nsn.Name, namespace)

			cr := &vmv1alpha1.VMDistributed{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      nsn.Name,
				},
				Spec: vmv1alpha1.VMDistributedSpec{
					VMAgentFlushDeadline: &metav1.Duration{Duration: 1 * time.Second},
					ZoneUpdatePause:      &metav1.Duration{Duration: 1 * time.Second},
					ReadyDeadline:        &metav1.Duration{Duration: 1 * time.Minute},
					VMAgent:              vmv1alpha1.VMAgentNameAndSpec{Name: nsn.Name},
					VMAuth:               vmv1alpha1.VMAuthNameAndSpec{Name: nsn.Name},
					Zones: []vmv1alpha1.VMDistributedZone{
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[0].Name},
							},
						},
						{
							VMCluster: &vmv1alpha1.VMClusterObjOrRef{
								Ref: &corev1.LocalObjectReference{Name: vmclusters[1].Name},
							},
						},
					},
				},
			}
			By("creating the VMDistributed")
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributed{}, types.NamespacedName{Name: nsn.Name, Namespace: namespace})
			}, eventualDistributedExpandingTimeout).WithContext(ctx).Should(Succeed())

			By("ensuring VMAgent was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})).To(Succeed())

			By("ensuring VMAuth was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})).To(Succeed())

			By("deleting the VMDistributed")
			Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())

			By("ensuring VMAgent is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})
			}, eventualDeletionTimeout).WithContext(ctx).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

			By("ensuring VMAuth is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: nsn.Name, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})
			}, eventualDeletionTimeout).WithContext(ctx).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))

			By("ensuring VMDistributed is eventually removed")
			Eventually(func() error {
				return k8sClient.Get(ctx, nsn, &vmv1alpha1.VMDistributed{})
			}, eventualDeletionTimeout).WithContext(ctx).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
			expectedVMAgentToBeRemoved = true
			expectedVMAuthToBeRemoved = true

			By("ensuring referenced VMCluster is not removed")
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[0].Name, Namespace: namespace}, &vmv1beta1.VMCluster{})
			}, "10s", "1s").Should(Succeed())
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmclusters[1].Name, Namespace: namespace}, &vmv1beta1.VMCluster{})
			}, "10s", "1s").Should(Succeed())
		})
	})
})
