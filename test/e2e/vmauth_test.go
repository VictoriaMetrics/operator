package e2e

import (
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operator "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

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
			Expect(k8sClient.DeleteAllOf(ctx, &operator.VLogs{}, &client.DeleteAllOfOptions{
				ListOptions: client.ListOptions{
					Namespace: namespace,
				},
			}))
			Eventually(func() bool {
				var unDeletedVMAuthes operator.VMAuthList
				Expect(k8sClient.List(ctx, &unDeletedVMAuthes, &client.ListOptions{
					Namespace: namespace,
				})).To(BeTrue())
				return len(unDeletedVMAuthes.Items) == 0
			}).Should(Succeed())

		})

		Context("crud", func() {

			JustBeforeEach(func() {
				ctx = context.Background()
			})
			AfterEach(func() {
				Expect(finalize.SafeDelete(ctx, k8sClient, &operator.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      namespacedName.Name,
						Namespace: namespacedName.Namespace,
					},
				})).To(Succeed())
				Eventually(func() error {
					return k8sClient.Get(ctx, namespacedName, &operator.VMAuth{})
				}, 10*time.Second).Should(MatchError(errors.IsNotFound, "IsNotFound"))
			})
			DescribeTable("should create vmauth", func(name string, cr *operator.VMAuth, verify func(cr *operator.VMAuth)) {
				namespacedName.Name = name
				cr.Name = name
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &operator.VMAuth{}, namespacedName)
				}, eventualAppReadyTimeout).Should(Succeed())
				verify(cr)
			},
				Entry("with 1 replica", "replica-1", &operator.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespacedName.Namespace,
					},
					Spec: operator.VMAuthSpec{
						CommonApplicationDeploymentParams: operator.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						UnauthorizedAccessConfig: []operator.UnauthorizedAccessConfigURLMap{
							{
								URLPrefix: []string{"http://localhost:8490"},
								SrcPaths:  []string{"/.*"},
							},
						},
					},
				}, func(cr *operator.VMAuth) {
					Expect(expectPodCount(k8sClient, 1, cr.Namespace, cr.SelectorLabels())).To(BeEmpty())
				}),
				Entry("with strict security and vm config-reloader", "strict-with-reloader", &operator.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespacedName.Namespace,
					},
					Spec: operator.VMAuthSpec{
						CommonDefaultableParams: operator.CommonDefaultableParams{
							UseStrictSecurity:   ptr.To(true),
							UseDefaultResources: ptr.To(false),
						},
						CommonConfigReloaderParams: operator.CommonConfigReloaderParams{
							UseCustomConfigReloader: ptr.To(true),
						},
						CommonApplicationDeploymentParams: operator.CommonApplicationDeploymentParams{
							ReplicaCount: ptr.To[int32](1),
						},
						UnauthorizedAccessConfig: []operator.UnauthorizedAccessConfigURLMap{
							{
								URLPrefix: []string{"http://localhost:8490"},
								SrcPaths:  []string{"/.*"},
							},
						},
					},
				}, func(cr *operator.VMAuth) {
					Expect(expectPodCount(k8sClient, 1, cr.Namespace, cr.SelectorLabels())).To(BeEmpty())
				}),
			)

			existVMAuth := &operator.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
				},
				Spec: operator.VMAuthSpec{
					CommonApplicationDeploymentParams: operator.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To[int32](1),
					},
					UnauthorizedAccessConfig: []operator.UnauthorizedAccessConfigURLMap{
						{
							URLPrefix: []string{"http://localhost:8490"},
							SrcPaths:  []string{"/.*"},
						},
					},
				},
			}
			DescribeTable("should update exist vmauth", func(name string, modify func(*operator.VMAuth), verify func(*operator.VMAuth)) {
				existVMAuth := existVMAuth.DeepCopy()
				existVMAuth.Name = name
				namespacedName.Name = name
				// setup test
				Expect(k8sClient.Create(ctx, existVMAuth)).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &operator.VMAuth{}, namespacedName)
				}, eventualAppReadyTimeout).Should(Succeed())

				// perform update
				Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
					var toUpdate operator.VMAuth
					Expect(k8sClient.Get(ctx, namespacedName, &toUpdate)).To(Succeed())
					modify(&toUpdate)
					return k8sClient.Update(ctx, &toUpdate)
				})).To(Succeed())
				Eventually(func() error {
					return expectObjectStatusExpanding(ctx, k8sClient, &operator.VMAuth{}, namespacedName)
				}, 5*time.Second).Should(Succeed())
				Eventually(func() error {
					return expectObjectStatusOperational(ctx, k8sClient, &operator.VMAuth{}, namespacedName)
				}, eventualAppReadyTimeout).Should(Succeed())
				var updated operator.VMAuth
				Expect(k8sClient.Get(ctx, namespacedName, &updated)).To(Succeed())
				verify(&updated)
			},
				Entry("extend replicas to 2", "update-replicas-2",
					func(cr *operator.VMAuth) {
						cr.Spec.ReplicaCount = ptr.To[int32](2)
					},
					func(cr *operator.VMAuth) {
						Eventually(func() string {
							return expectPodCount(k8sClient, 2, namespace, cr.SelectorLabels())
						}, eventualAppReadyTimeout).Should(BeEmpty())
					}),
				Entry("switch to vm config reloader", "vm-reloader",
					func(cr *operator.VMAuth) {
						cr.Spec.UseCustomConfigReloader = ptr.To(true)
						cr.Spec.UseDefaultResources = ptr.To(false)
					},
					func(cr *operator.VMAuth) {
						Eventually(func() string {
							return expectPodCount(k8sClient, 1, namespace, cr.SelectorLabels())
						}, 5*time.Second).Should(BeEmpty())
					}),
			)

		})
	})
})
