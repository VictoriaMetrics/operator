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

var _ = Describe("VLDistributed Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-vldistributed-resource"

		ctx := context.Background()

		nsn := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		vld := &vmv1alpha1.VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nsn.Name,
				Namespace: nsn.Namespace,
			},
			Spec: vmv1alpha1.VLDistributedSpec{
				VMAuth: vmv1alpha1.VLDistributedAuth{
					Name: "test",
				},
				Zones: []vmv1alpha1.VLDistributedZone{
					{
						Name: "test",
						VLCluster: vmv1alpha1.VLDistributedZoneCluster{
							Name: "test",
						},
						VLAgent: vmv1alpha1.VLDistributedZoneAgent{
							Name: "test",
						},
					},
				},
			},
		}
		BeforeEach(func() {
			By("creating the custom resource for the Kind VLDistributed")
			if err := k8sClient.Get(ctx, nsn, &vmv1alpha1.VLDistributed{}); err != nil {
				Expect(err).Should(MatchError(k8serrors.IsNotFound, "IsNotFound"))
				Expect(k8sClient.Create(ctx, vld)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &vmv1alpha1.VLDistributed{}
			err := k8sClient.Get(ctx, nsn, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance VLDistributed")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VLDistributedReconciler{
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
