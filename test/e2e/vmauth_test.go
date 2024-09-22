package e2e

import (
	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:dupl
var _ = Describe("test vmauth Controller", func() {
	Context("e2e ", func() {
		var ctx context.Context
		namespace := "default"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
		}
		It("must clean up previous test resutls", func() {
			ctx = context.Background()
			// clean up before tests
			Expect(k8sClient.DeleteAllOf(ctx, &v1beta1vm.VMAuth{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace: namespace,
				},
			})).To(Succeed())
			Eventually(func() bool {
				var unDeletedObjects v1beta1vm.VMAuthList
				Expect(k8sClient.List(ctx, &unDeletedObjects, &client.ListOptions{
					Namespace: namespace,
				})).To(Succeed())
				return len(unDeletedObjects.Items) == 0
			}, eventualDeletionTimeout).Should(BeTrue())

		})

		Context("crud", func() {

			JustBeforeEach(func() {
				ctx = context.Background()
			})
			AfterEach(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, &v1beta1vm.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      namespacedName.Name,
						Namespace: namespacedName.Namespace,
					},
				})).To(Succeed())
				Eventually(func() error {
					return k8sClient.Get(ctx, namespacedName, &v1beta1vm.VMAuth{})
				}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "IsNotFound"))
			})
			DescribeTable("should create vmauth", func(name string, cr *v1beta1vm.VMAuth, verify func(cr *v1beta1vm.VMAuth)) {
				namespacedName.Name = name
				cr.Name = name
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAuth{}, namespacedName)
				}, eventualDeploymentAppReadyTimeout).Should(Succeed())
				verify(cr)
			},
				Entry("with 1 replica", "replica-1", &v1beta1vm.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespacedName.Namespace,
					},
					Spec: v1beta1vm.VMAuthSpec{
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
							{
								URLPrefix: []string{"http://localhost:8490"},
								SrcPaths:  []string{"/.*"},
							},
						},
					},
				}, func(cr *v1beta1vm.VMAuth) {
					Expect(expectPodCount(k8sClient, 1, cr.Namespace, cr.SelectorLabels())).To(BeEmpty())
				}),
				Entry("with strict security and vm config-reloader", "strict-with-reloader", &v1beta1vm.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespacedName.Namespace,
					},
					Spec: v1beta1vm.VMAuthSpec{
						CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
							//						UseStrictSecurity:   ptr.To(true),
							UseDefaultResources: ptr.To(false),
						},
						CommonConfigReloaderParams: v1beta1vm.CommonConfigReloaderParams{
							UseVMConfigReloader: ptr.To(true),
						},
						CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
							{
								URLPrefix: []string{"http://localhost:8490"},
								SrcPaths:  []string{"/.*"},
							},
						},
					},
				}, func(cr *v1beta1vm.VMAuth) {
					Expect(expectPodCount(k8sClient, 1, cr.Namespace, cr.SelectorLabels())).To(BeEmpty())
				}),
			)

			existVMAuth := &v1beta1vm.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
				},
				Spec: v1beta1vm.VMAuthSpec{
					CommonApplicationDeploymentParams: v1beta1vm.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					UnauthorizedAccessConfig: []v1beta1vm.UnauthorizedAccessConfigURLMap{
						{
							URLPrefix: []string{"http://localhost:8490"},
							SrcPaths:  []string{"/.*"},
						},
					},
				},
			}
			DescribeTable("should update exist vmauth",
				func(name string, modify func(*v1beta1vm.VMAuth), verify func(*v1beta1vm.VMAuth)) {
					existVMAuth := existVMAuth.DeepCopy()
					existVMAuth.Name = name
					namespacedName.Name = name
					// setup test
					Expect(k8sClient.Create(ctx, existVMAuth)).To(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAuth{}, namespacedName)
					}, eventualDeploymentAppReadyTimeout).Should(Succeed())

					// perform update
					Eventually(func() error {
						var toUpdate v1beta1vm.VMAuth
						Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
						modify(&toUpdate)
						return k8sClient.Update(ctx, &toUpdate)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusExpanding(ctx, k8sClient, &v1beta1vm.VMAuth{}, namespacedName)
					}, eventualExpandingTimeout).Should(Succeed())
					Eventually(func() error {
						return expectObjectStatusOperational(ctx, k8sClient, &v1beta1vm.VMAuth{}, namespacedName)
					}, eventualDeploymentAppReadyTimeout).Should(Succeed())
					var updated v1beta1vm.VMAuth
					Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
					// verify results
					verify(&updated)
				},
				Entry("extend replicas to 2", "update-replicas-2",
					func(cr *v1beta1vm.VMAuth) {
						cr.Spec.ReplicaCount = ptr.To[int32](2)
					},
					func(cr *v1beta1vm.VMAuth) {
						Eventually(func() string {
							return expectPodCount(k8sClient, 2, namespace, cr.SelectorLabels())
						}, eventualDeploymentPodTimeout).Should(BeEmpty())
					}),
				Entry("switch to vm config reloader", "vm-reloader",
					func(cr *v1beta1vm.VMAuth) {
						cr.Spec.UseVMConfigReloader = ptr.To(true)
						cr.Spec.UseDefaultResources = ptr.To(false)
					},
					func(cr *v1beta1vm.VMAuth) {
						Eventually(func() string {
							return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
						}, eventualDeploymentPodTimeout).Should(BeEmpty())
					}),
			)

		})
	})
})
