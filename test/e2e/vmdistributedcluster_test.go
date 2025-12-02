package e2e

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
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

// createVMClusterAndEnsureOperational creates a VMCluster, sets up its deferred cleanup, and waits for it to become operational.
func createVMClusterAndEnsureOperational(ctx context.Context, k8sClient client.Client, vmcluster *vmv1beta1.VMCluster, ns string) {
	DeferCleanup(func() {
		Expect(finalize.SafeDelete(ctx, k8sClient, vmcluster)).To(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: vmcluster.Name, Namespace: ns}, &vmv1beta1.VMCluster{})
		}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
	})
	Expect(k8sClient.Create(ctx, vmcluster)).To(Succeed())
	Eventually(func() error {
		return expectObjectStatusOperational(ctx, k8sClient, vmcluster, types.NamespacedName{Name: vmcluster.Name, Namespace: ns})
	}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())
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

	os.Setenv("E2E_TEST", "true")

	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	namespacedName := types.NamespacedName{
		Namespace: namespace,
	}
	existingVMAgentName := "existing-vmagent"
	existingVMAuthName := "existing-vmauth"

	beforeEach := func() {
		ctx = context.Background()

		// Create predefined VMAgent
		vmAgent := &vmv1beta1.VMAgent{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      existingVMAgentName,
		}, vmAgent)
		if k8serrors.IsNotFound(err) {
			vmAgent = &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      existingVMAgentName,
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMAgentSpec{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{
							URL: "http://victoria-metrics-single:8428/api/v1/write",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, vmAgent)).To(Succeed(), "must create managed vm-agent before test")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, types.NamespacedName{Name: existingVMAgentName, Namespace: namespace})
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
	}

	Context("create", func() {
		It("should successfully create a VMDistributedCluster with inline VMAgent spec", func() {
			beforeEach()

			By("creating a VMDistributedCluster with inline VMAgent spec")
			namespacedName.Name = "distributed-cluster-with-inline-vmagent"
			vmAgentName := "inline-vmagent"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 1 * time.Millisecond},
					ZoneUpdatePause: &metav1.Duration{Duration: 0 * time.Second},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{
						Name: vmAgentName,
						Spec: &vmv1alpha1.CustomVMAgentSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](2),
							},
						},
					},
					VMAuth: vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Name: "inline-cluster-1",
							Spec: &vmv1beta1.VMClusterSpec{
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
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

			var vmagent vmv1beta1.VMAgent
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: vmAgentName, Namespace: namespace}, &vmagent)
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAgent{}, types.NamespacedName{Name: vmAgentName, Namespace: namespace})
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())
			Expect(vmagent.Spec.ReplicaCount).To(Equal(ptr.To[int32](2)))
			Expect(vmagent.OwnerReferences).To(HaveLen(1))
			Expect(vmagent.OwnerReferences[0].Name).To(Equal(cr.Name))
			Expect(vmagent.OwnerReferences[0].Kind).To(Equal("VMDistributedCluster"))
			Expect(vmagent.OwnerReferences[0].UID).To(Equal(cr.UID))
		})

		It("should successfully create a VMDistributedCluster with inline VMCluster specs", func() {
			beforeEach()

			By("creating a VMDistributedCluster with inline VMCluster specs")
			namespacedName.Name = "inline-vmcluster"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:          vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
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
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			var inlineVMClusters []vmv1beta1.VMCluster
			for _, zone := range cr.Spec.Zones.VMClusters {
				inlineVMClusters = append(inlineVMClusters, vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      zone.Name,
						Namespace: namespace,
					},
				})
			}
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, inlineVMClusters, namespace)

			By("waiting for VMDistributedCluster to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

			By("verifying that the inline VMClusters are created and operational")
			var vmCluster1 vmv1beta1.VMCluster
			namespacedName := types.NamespacedName{Name: "inline-cluster-1", Namespace: namespace}
			Expect(k8sClient.Get(ctx, namespacedName, &vmCluster1)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmCluster1, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())
			Expect(vmCluster1.Spec.ClusterVersion).To(Equal("v1.125.0-cluster"))
			actualReplicaCount := *vmCluster1.Spec.VMStorage.ReplicaCount
			Expect(actualReplicaCount).To(Equal(int32(1)))

			var vmCluster2 vmv1beta1.VMCluster
			namespacedName = types.NamespacedName{Name: "inline-cluster-2", Namespace: namespace}
			Expect(k8sClient.Get(ctx, namespacedName, &vmCluster2)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmCluster2, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())
			Expect(vmCluster2.Spec.ClusterVersion).To(Equal("v1.126.0-cluster"))
			actualReplicaCount = *vmCluster2.Spec.VMStorage.ReplicaCount
			Expect(actualReplicaCount).To(Equal(int32(2)))

			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, &vmCluster1)).To(Succeed())
				Expect(finalize.SafeDelete(ctx, k8sClient, &vmCluster2)).To(Succeed())
			})
		})

		It("should successfully create a VMDistributedCluster with inline VMAuth spec", func() {
			beforeEach()

			By("creating a VMCluster")
			vmCluster := &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vmcluster-inline-vmauth",
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
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, vmCluster)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, vmCluster)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, vmCluster, types.NamespacedName{Name: vmCluster.Name, Namespace: namespace})
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

			By("creating a VMDistributedCluster with inline VMAuth spec")
			inlineVMAuthName := "inline-vmauth-proxy"
			namespacedName.Name = "inline-vmauth-cluster"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth: vmv1alpha1.VMAuthNameAndSpec{
						Name: inlineVMAuthName,
						Spec: &vmv1beta1.VMAuthLoadBalancerSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								Port: "8417",
								Image: vmv1beta1.Image{
									Repository: "docker.io/victoriametrics/vmauth",
									Tag:        "latest",
								},
							},
						},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}},
					}},
				},
			}

			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			By("waiting for VMDistributedCluster to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

			By("verifying that the inline VMAuth is created and operational")
			var createdVMAuthLBDeployment appsv1.Deployment
			vmAuthNsn := types.NamespacedName{Name: cr.PrefixedName(vmv1beta1.ClusterComponentBalancer), Namespace: namespace}
			Expect(k8sClient.Get(ctx, vmAuthNsn, &createdVMAuthLBDeployment)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1beta1.VMAuth{}, vmAuthNsn)
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())

			By("verifying VMAuth has correct owner reference")
			Expect(createdVMAuthLBDeployment.GetOwnerReferences()).To(HaveLen(1))
			ownerRef := createdVMAuthLBDeployment.GetOwnerReferences()[0]
			Expect(ownerRef.Kind).To(Equal("VMDistributedCluster"))
			Expect(ownerRef.Name).To(Equal(cr.Name))
		})

		It("should successfully create a VMDistributedCluster with VMCluster references and override spec", func() {
			beforeEach()

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
			createVMClusterAndEnsureOperational(ctx, k8sClient, initialCluster, namespace)

			By("creating a VMDistributedCluster referencing the existing VMCluster with an override spec")
			namespacedName.Name = "ref-override-cluster"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 1 * time.Millisecond},
					ZoneUpdatePause: &metav1.Duration{Duration: 0 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:          vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{Name: initialCluster.Name},
							OverrideSpec: &apiextensionsv1.JSON{
								Raw: []byte(`{"retentionPeriod": "10h"}`),
							},
						},
					}},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			By("waiting for VMDistributedCluster to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, []vmv1beta1.VMCluster{*initialCluster}, namespace)

			By("verifying that the referenced VMCluster has the override applied")
			var updatedCluster vmv1beta1.VMCluster
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: initialCluster.Name, Namespace: namespace}, &updatedCluster)).To(Succeed())
			Expect(updatedCluster.Spec.RetentionPeriod).To(Equal("10h"))
		})

		It("should handle rolling updates with VMAgent configuration changes", func() {
			beforeEach()

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
			createVMClusterAndEnsureOperational(ctx, k8sClient, vmCluster1, namespace)

			namespacedName.Name = "distributed-agent-upgrade"
			vmAgentName := "global-vmagent"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ZoneUpdatePause: &metav1.Duration{Duration: 2 * time.Second},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{
							Ref: &corev1.LocalObjectReference{
								Name: vmCluster1.Name,
							},
						},
					}},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: vmAgentName},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())
			verifyOwnerReferences(ctx, cr, []vmv1beta1.VMCluster{*vmCluster1}, namespace)

			// Update VMAgent
			Eventually(func() error {
				var obj vmv1alpha1.VMDistributedCluster
				if err := k8sClient.Get(ctx, namespacedName, &obj); err != nil {
					return err
				}
				obj.Spec.VMAgent = vmv1alpha1.VMAgentNameAndSpec{Name: vmAgentName}
				return k8sClient.Update(ctx, &obj)
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())

			// Wait for VMDistributedCluster to become operational after update
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())
		})

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
			createVMClusterAndEnsureOperational(ctx, k8sClient, vmCluster1, namespace)
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
			createVMClusterAndEnsureOperational(ctx, k8sClient, vmCluster2, namespace)
			vmclusters := []vmv1beta1.VMCluster{*vmCluster1, *vmCluster2}

			namespacedName.Name = "distributed-upgrade"
			vmAgentName := existingVMAgentName
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 1 * time.Millisecond},
					ZoneUpdatePause: &metav1.Duration{Duration: 0 * time.Second},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{
						Name: vmAgentName,
						Spec: &vmv1alpha1.CustomVMAgentSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
					VMAuth: vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
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
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())
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
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

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
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())
		})

		It("should skip reconciliation when VMDistributedCluster is paused", func() {
			beforeEach()

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
			createVMClusterAndEnsureOperational(ctx, k8sClient, vmCluster, namespace)

			By("creating a VMDistributedCluster")
			namespacedName.Name = "distributed-paused"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					ReadyDeadline:   &metav1.Duration{Duration: 1 * time.Millisecond},
					ZoneUpdatePause: &metav1.Duration{Duration: 0 * time.Second},
					VMAgent:         vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:          vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
						{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}},
					},
					},
				}}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())
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
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())

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
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())

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
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())

			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName)
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())

			By("verifying reconciliation resumes after unpausing")
			Eventually(func() int32 {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmCluster.Name, Namespace: namespace}, vmCluster)).To(Succeed())
				return *vmCluster.Spec.VMStorage.ReplicaCount
			}, eventualDeploymentAppReadyTimeout).Should(Equal(initialReplicas + 1))
		})
	})

	Context("fail", func() {
		DescribeTable("should fail when creating vmdistributedcluster", func(cr *vmv1alpha1.VMDistributedCluster, vmclusters []vmv1beta1.VMCluster) {
			beforeEach()

			for _, vmcluster := range vmclusters {
				createVMClusterAndEnsureOperational(ctx, k8sClient, &vmcluster, namespace)
			}

			namespacedName.Name = cr.Name
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, namespacedName, vmv1beta1.UpdateStatusFailed)
			}, eventualVMDistributedClusterExpandingTimeout).Should(Succeed())
		},
			Entry("with invalid VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "missing-cluster",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
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
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
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
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
					Zones:   vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{{}}},
				},
			}, []vmv1beta1.VMCluster{}),
			Entry("with zone having both spec and ref", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "zone-both-spec-ref",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
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
			Entry("with invalid OverrideSpec for VMCluster", &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "invalid-override-spec",
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
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
		)
	})

	Context("delete", func() {

		It("should delete VMDistributedCluster and remove it from the cluster", func() {
			beforeEach()

			vmCluster := &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vmcluster-1",
				},
			}
			createVMClusterAndEnsureOperational(ctx, k8sClient, vmCluster, namespace)

			namespacedName.Name = "vmdistributedcluster-remove"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      namespacedName.Name,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
					Zones:   vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}}},
				},
			}
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, types.NamespacedName{Name: namespacedName.Name, Namespace: namespace})
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

			Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			Eventually(func() error {
				err := k8sClient.Get(ctx, namespacedName, &vmv1alpha1.VMDistributedCluster{})
				if k8serrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("want NotFound error, got: %w", err)
			}, eventualDeletionTimeout, 1).WithContext(ctx).Should(Succeed())
		})

		It("should remove vmagent created by vmdistributedcluster", func() {
			beforeEach()

			vmCluster := &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vmcluster-1",
				},
			}
			createVMClusterAndEnsureOperational(ctx, k8sClient, vmCluster, namespace)

			namespacedName.Name = "vmdistributedcluster-agent-remove"
			vmagentName := namespacedName.Name + "-agent"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAuth: vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{
						Name: vmagentName,
						Spec: &vmv1alpha1.CustomVMAgentSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
					Zones: vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}}},
				},
			}

			By("creating the VMDistributedCluster")
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			By("waiting for it to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, types.NamespacedName{Name: namespacedName.Name, Namespace: namespace})
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

			By("ensuring VMAgent was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmagentName, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})).To(Succeed())

			By("deleting the VMDistributedCluster")
			Expect(k8sClient.Delete(ctx, cr)).To(Succeed())

			By("ensuring VMAgent is eventually removed")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: vmagentName, Namespace: cr.Namespace}, &vmv1beta1.VMAgent{})
				return k8serrors.IsNotFound(err)
			}, eventualDeletionTimeout).Should(BeTrue())
		})

		It("should remove vmauth created by vmdistributedcluster", func() {
			beforeEach()

			vmCluster := &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "vmcluster-1",
				},
			}
			createVMClusterAndEnsureOperational(ctx, k8sClient, vmCluster, namespace)

			namespacedName.Name = "vmdistributedcluster-vmauth-remove"
			vmauthName := namespacedName.Name + "-vmauth"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespace,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAuth: vmv1alpha1.VMAuthNameAndSpec{
						Name: vmauthName,
						Spec: &vmv1beta1.VMAuthLoadBalancerSpec{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								Port: "8417",
								Image: vmv1beta1.Image{
									Repository: "docker.io/victoriametrics/vmauth",
									Tag:        "latest",
								},
							},
						},
					},
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					Zones:   vmv1alpha1.ZoneSpec{VMClusters: []vmv1alpha1.VMClusterRefOrSpec{{Ref: &corev1.LocalObjectReference{Name: vmCluster.Name}}}},
				},
			}

			By("creating the VMDistributedCluster")
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})

			By("waiting for it to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, types.NamespacedName{Name: namespacedName.Name, Namespace: namespace})
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

			By("ensuring VMAuth was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmauthName, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})).To(Succeed())

			By("deleting the VMDistributedCluster")
			Expect(k8sClient.Delete(ctx, cr)).To(Succeed())

			By("ensuring VMAuth is eventually removed")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: vmauthName, Namespace: cr.Namespace}, &vmv1beta1.VMAuth{})
				return k8serrors.IsNotFound(err)
			}, eventualDeletionTimeout).Should(BeTrue())
		})

		It("should remove vmclusters created by vmdistributedcluster", func() {
			beforeEach()

			namespacedName.Name = "vmdistributed-vmcluster-removal"
			vmc1Name := namespacedName.Name + "-c1"
			vmc2Name := namespacedName.Name + "-c2"
			cr := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespace,
				},
				Spec: vmv1alpha1.VMDistributedClusterSpec{
					VMAgent: vmv1alpha1.VMAgentNameAndSpec{Name: existingVMAgentName},
					VMAuth:  vmv1alpha1.VMAuthNameAndSpec{Name: existingVMAuthName},
					Zones: vmv1alpha1.ZoneSpec{
						VMClusters: []vmv1alpha1.VMClusterRefOrSpec{
							{
								Name: vmc1Name,
								Spec: &vmv1beta1.VMClusterSpec{
									VMSelect: &vmv1beta1.VMSelect{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](1),
									}},
									VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](1),
									}},
									VMStorage: &vmv1beta1.VMStorage{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](1),
									}},
								},
							},
							{
								Name: vmc2Name,
								Spec: &vmv1beta1.VMClusterSpec{
									VMSelect: &vmv1beta1.VMSelect{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](1),
									}},
									VMInsert: &vmv1beta1.VMInsert{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](1),
									}},
									VMStorage: &vmv1beta1.VMStorage{CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
										ReplicaCount: ptr.To[int32](1),
									}},
								},
							},
						},
					},
				},
			}

			By("creating the VMDistributedCluster")
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, cr)).To(Succeed())
			})

			By("waiting for it to become operational")
			Eventually(func() error {
				return expectObjectStatusOperational(ctx, k8sClient, &vmv1alpha1.VMDistributedCluster{}, types.NamespacedName{Name: namespacedName.Name, Namespace: namespace})
			}, eventualVMDistributedClusterExpandingTimeout).WithContext(ctx).Should(Succeed())

			vmclusters := []string{vmc1Name, vmc2Name}
			By("ensuring VMClusters were created")
			for _, vmclusterName := range vmclusters {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vmclusterName, Namespace: namespace}, &vmv1beta1.VMCluster{})).To(Succeed())
			}

			By("deleting the VMDistributedCluster")
			Expect(k8sClient.Delete(ctx, cr)).To(Succeed())

			By("ensuring VMClusters are eventually removed")
			for _, vmclusterName := range vmclusters {
				Eventually(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: vmclusterName, Namespace: namespace}, &vmv1beta1.VMCluster{})
					return k8serrors.IsNotFound(err)
				}, eventualDeletionTimeout).Should(BeTrue())
			}
		})
	})
})
