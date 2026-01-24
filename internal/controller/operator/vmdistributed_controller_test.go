/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package operator

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
)

var _ = Describe("VMDistributed Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		nsn := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		vmd := &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nsn.Name,
				Namespace: nsn.Namespace,
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				VMAgent: vmv1alpha1.VMDistributedAgent{
					Name: "test",
				},
				VMAuth: vmv1alpha1.VMDistributedAuth{
					Name: "test",
				},
				Zones: []vmv1alpha1.VMDistributedZone{
					{
						VMCluster: &vmv1alpha1.VMDistributedCluster{
							Name: "test",
						},
					},
				},
			},
		}
		BeforeEach(func() {
			By("creating the custom resource for the Kind VMDistributed")
			if err := k8sClient.Get(ctx, nsn, &vmv1alpha1.VMDistributed{}); err != nil {
				Expect(err).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
				Expect(k8sClient.Create(ctx, vmd)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &vmv1alpha1.VMDistributed{}
			err := k8sClient.Get(ctx, nsn, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance VMDistributed")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VMDistributedReconciler{
				Client:       k8sClient,
				OriginScheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: nsn,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
