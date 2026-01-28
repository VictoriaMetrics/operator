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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

var _ = Describe("VMAlertmanager Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		vmalertmanager := &vmv1beta1.VMAlertmanager{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind VMAlertmanager")
			err := k8sClient.Get(ctx, typeNamespacedName, vmalertmanager)
			if err != nil && k8serrors.IsNotFound(err) {
				resource := &vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &vmv1beta1.VMAlertmanager{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance VMAlertmanager")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VMAlertmanagerReconciler{
				Client:       k8sClient,
				OriginScheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

func TestVMAlertmanager_Reconcile_AgentSync_Managed(t *testing.T) {
	g := NewWithT(t)
	managed := &vmv1beta1.VMAlertmanager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAlertmanagerSpec{
			SelectAllByDefault: true,
		},
	}

	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{managed})
	r := &VMAlertmanagerReconciler{
		Client:       fclient,
		BaseConf:     &config.BaseOperatorConf{},
		Log:          ctrl.Log.WithName("test"),
		OriginScheme: fclient.Scheme(),
	}

	// start with locked alertmanager reconcile
	locked := true
	alertmanagerSync.Lock()
	defer func() {
		if locked {
			alertmanagerSync.Unlock()
		}
	}()
	// Create a channel to monitor reconcile completion
	doneCh := make(chan struct{})
	go func() {
		nsn := types.NamespacedName{Name: managed.Name, Namespace: managed.Namespace}
		_, _ = r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: nsn})
		// Close done channel when reconcile completes
		close(doneCh)
	}()
	// ensure that reconcile is blocked
	g.Consistently(doneCh, "1s").ShouldNot(BeClosed())

	// reconcile completes when alertmanagerSync is unlocked
	locked = false
	alertmanagerSync.Unlock()
	g.Eventually(doneCh, "5s").Should(BeClosed())
}

func TestVMAlertmanager_Reconcile_AgentSync_Unmanaged(t *testing.T) {
	g := NewWithT(t)
	unmanaged := &vmv1beta1.VMAlertmanager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unmanaged",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAlertmanagerSpec{},
	}

	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{unmanaged})
	r := &VMAlertmanagerReconciler{
		Client:       fclient,
		BaseConf:     &config.BaseOperatorConf{},
		Log:          ctrl.Log.WithName("test"),
		OriginScheme: fclient.Scheme(),
	}

	// Start with locked alertmanager reconcile
	alertmanagerSync.Lock()
	defer alertmanagerSync.Unlock()

	// Create a channel to monitor reconcile completion
	doneCh := make(chan struct{})
	go func() {
		nsn := types.NamespacedName{Name: unmanaged.Name, Namespace: unmanaged.Namespace}
		_, _ = r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: nsn})
		// Close done channel when reconcile completes
		close(doneCh)
	}()
	// The channel should be closed immediately - resource is unmanaged
	g.Eventually(doneCh, "5s").Should(BeClosed())
}
