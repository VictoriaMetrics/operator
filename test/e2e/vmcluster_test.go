package e2e

import (
	"context"
	"fmt"

	"github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("e2e vmcluster", func() {
	Context("crud", func() {
		namespace := "default"
		var ctx context.Context
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		It("must clean up previous test resutls", func() {
			ctx = context.Background()
			// clean up before tests
			Expect(k8sClient.DeleteAllOf(ctx, &v1beta1vm.VMCluster{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace: namespace,
				},
			})).To(Succeed())
			Eventually(func() bool {
				var unDeletedObjects v1beta1vm.VMClusterList
				Expect(k8sClient.List(ctx, &unDeletedObjects, &client.ListOptions{
					Namespace: namespace,
				})).To(Succeed())
				return len(unDeletedObjects.Items) == 0
			}, eventualDeletionTimeout).Should(BeTrue())

		})

		Context("create", func() {
			JustBeforeEach(func() {
				ctx = context.Background()
			})
			AfterEach(func() {
				Expect(k8sClient.Delete(ctx, &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
				})).To(Succeed(), "must delete vmcluster after test")
				Eventually(func() error {
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      namespacedName.Name,
						Namespace: namespace,
					}, &v1beta1vm.VMCluster{})
					if errors.IsNotFound(err) {
						return nil
					}
					return fmt.Errorf("want NotFound error, got: %w", err)
				}, eventualDeletionTimeout, 1).Should(BeNil())
			})

			DescribeTable("should create vmcluster", func(name string, cr *v1beta1vm.VMCluster) {
				namespacedName.Name = name
				cr.Name = name
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMCluster{}, namespacedName)
				}, eventualStatefulsetAppReadyTimeout).Should(Succeed())

			},
				Entry("without any componets", "empty", &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: v1beta1vm.VMClusterSpec{RetentionPeriod: "1"},
				}),
				Entry("with all components", "all-services",
					&v1beta1vm.VMCluster{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      namespacedName.Name,
						},
						Spec: v1beta1vm.VMClusterSpec{
							RetentionPeriod: "1",
							VMStorage: &v1beta1vm.VMStorage{
								CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To[int32](1),
								},
							},
							VMSelect: &v1beta1vm.VMSelect{
								CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To[int32](1),
								},
							},
							VMInsert: &v1beta1vm.VMInsert{CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
							},
						},
					},
				),
				Entry("with vmstorage and vmselect", "with-select", &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: v1beta1vm.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &v1beta1vm.VMStorage{
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMSelect: &v1beta1vm.VMSelect{
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
					},
				}),
				Entry("with vmstorage and vminsert", "with-insert", &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
					Spec: v1beta1vm.VMClusterSpec{
						RetentionPeriod: "1",
						VMStorage: &v1beta1vm.VMStorage{
							CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To[int32](1),
							},
						},
						VMInsert: &v1beta1vm.VMInsert{CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						},
					},
				}),
				Entry("with security enable and without default resources", "all-secure",
					&v1beta1vm.VMCluster{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      namespacedName.Name,
						},
						Spec: v1beta1vm.VMClusterSpec{
							RetentionPeriod: "1",
							VMStorage: &v1beta1vm.VMStorage{
								CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
									//								UseStrictSecurity:   ptr.To(true),
									UseDefaultResources: ptr.To(false),
								},
								CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To[int32](1),
								},
							},
							VMSelect: &v1beta1vm.VMSelect{
								CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
									//							UseStrictSecurity:   ptr.To(true),
									UseDefaultResources: ptr.To(false),
								},

								CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To[int32](1),
								},
							},
							VMInsert: &v1beta1vm.VMInsert{
								CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
									//						UseStrictSecurity:   ptr.To(true),
									UseDefaultResources: ptr.To(false),
								},
								CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
									ReplicaCount: ptr.To[int32](1),
								},
							},
						},
					},
				),
			)
		})
		Context("update", func() {
			AfterEach(func() {
				Expect(k8sClient.Delete(ctx, &v1beta1vm.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      namespacedName.Name,
					},
				})).To(Succeed())
				Eventually(func() error {
					return k8sClient.Get(context.Background(), types.NamespacedName{
						Name:      namespacedName.Name,
						Namespace: namespace,
					}, &v1beta1vm.VMCluster{})

				}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "want not found error"))
			})
			clusterForUpdate := &v1beta1vm.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
				},
				Spec: v1beta1vm.VMClusterSpec{
					RetentionPeriod: "1",
					VMStorage: &v1beta1vm.VMStorage{
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
					VMSelect: &v1beta1vm.VMSelect{
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
					},
					VMInsert: &v1beta1vm.VMInsert{CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					},
				},
			}
			DescribeTable("should update exist cluster",
				func(name string, modify func(*v1beta1vm.VMCluster), verify func(*v1beta1vm.VMCluster)) {
					namespacedName.Name = name
					existCluster := clusterForUpdate.DeepCopy()
					existCluster.Name = name
					ctx = context.Background()
					Expect(k8sClient.Create(ctx, existCluster)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, existCluster, namespacedName)
					}, eventualStatefulsetAppReadyTimeout).
						Should(Succeed())
					Expect(expectPodCount(k8sClient, 1, namespace, existCluster.VMStorageSelectorLabels())).To(BeEmpty())
					Expect(expectPodCount(k8sClient, 1, namespace, existCluster.VMSelectSelectorLabels())).To(BeEmpty())
					Expect(expectPodCount(k8sClient, 1, namespace, existCluster.VMInsertSelectorLabels())).To(BeEmpty())

					// update and wait ready
					Eventually(func() error {
						var toUpdate v1beta1.VMCluster
						if err := k8sClient.Get(ctx, namespacedName, &toUpdate); err != nil {
							return err
						}
						modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusExpanding(ctx, k8sClient, existCluster, namespacedName)
					}, eventualStatefulsetAppReadyTimeout).
						Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, existCluster, namespacedName)
					}, eventualStatefulsetAppReadyTimeout).
						Should(Succeed())
						// verify results
					verify(existCluster)
				},
				Entry("update storage and select replicas to 2", "storage-select-r-2", func(cr *v1beta1vm.VMCluster) {
					cr.Spec.VMStorage.ReplicaCount = ptr.To[int32](2)
					cr.Spec.VMSelect.ReplicaCount = ptr.To[int32](2)
				}, func(cr *v1beta1vm.VMCluster) {
					Expect(expectPodCount(k8sClient, 2, namespace, cr.VMStorageSelectorLabels())).To(BeEmpty())
					Expect(expectPodCount(k8sClient, 2, namespace, cr.VMSelectSelectorLabels())).To(BeEmpty())

				}),
				Entry("update storage and insert replicas to 2", "storage-insert-r-2", func(cr *v1beta1vm.VMCluster) {
					cr.Spec.VMStorage.ReplicaCount = ptr.To[int32](2)
					cr.Spec.VMInsert.ReplicaCount = ptr.To[int32](2)
				}, func(cr *v1beta1vm.VMCluster) {
					Expect(expectPodCount(k8sClient, 2, namespace, cr.VMStorageSelectorLabels())).To(BeEmpty())
					Eventually(func() string {
						return expectPodCount(k8sClient, 2, namespace, cr.VMInsertSelectorLabels())
					}, eventualDeploymentPodTimeout).Should(BeEmpty())
				}),
				Entry("update storage revisionHistoryLimit to 2", "storage-revision-2", func(cr *v1beta1vm.VMCluster) {
					cr.Spec.VMStorage.RevisionHistoryLimitCount = ptr.To[int32](2)
				}, func(cr *v1beta1vm.VMCluster) {
					var updatedCluster v1beta1vm.VMCluster
					Expect(k8sClient.Get(ctx, namespacedName, &updatedCluster)).To(Succeed())
					Expect(*updatedCluster.Spec.VMStorage.RevisionHistoryLimitCount).To(Equal(int32(2)))
				}),
				Entry("add clusterNative ports", "storage-native-r-2", func(cr *v1beta1vm.VMCluster) {
					cr.Spec.VMInsert.ClusterNativePort = "8035"
					cr.Spec.VMSelect.ClusterNativePort = "8036"
				}, func(cr *v1beta1vm.VMCluster) {
					var updatedSvc corev1.Service
					Expect(k8sClient.Get(ctx,
						types.NamespacedName{Namespace: namespace, Name: "vmselect-" + cr.Name},
						&updatedSvc)).
						To(Succeed())
					Expect(updatedSvc.Spec.Ports).To(HaveLen(2))
					Expect(updatedSvc.Spec.Ports[1].Port).To(Equal(int32(8036)))
					Expect(k8sClient.Get(ctx,
						types.NamespacedName{Namespace: namespace, Name: "vminsert-" + cr.Name},
						&updatedSvc)).
						To(Succeed())
					Expect(updatedSvc.Spec.Ports).To(HaveLen(2))
					Expect(updatedSvc.Spec.Ports[1].Port).To(Equal(int32(8035)))

				}),
			)
		})
	})
})
