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

var _ = Describe("VMAlert Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		vmalert := &vmv1beta1.VMAlert{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind VMAlert")
			err := k8sClient.Get(ctx, typeNamespacedName, vmalert)
			if err != nil && k8serrors.IsNotFound(err) {
				resource := &vmv1beta1.VMAlert{
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
			resource := &vmv1beta1.VMAlert{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance VMAlert")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VMAlertReconciler{
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

func TestVMAlert_Reconcile_DeleteReleasesChildStatus(t *testing.T) {
	g := NewWithT(t)
	parent := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "parent1",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1beta1.VMAlertSpec{
			SelectAllByDefault: true,
		},
	}
	conditionType := "parent1.default.vmalert" + vmv1beta1.ConditionDomainTypeAppliedSuffix
	child := &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "child1",
			Namespace: "default",
		},
		Status: vmv1beta1.VMRuleStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
				Conditions: []vmv1beta1.Condition{
					{Type: conditionType, Status: "True", Reason: vmv1beta1.ConditionParsingReason},
				},
			},
		},
	}

	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{parent, child})
	g.Expect(fclient.Delete(context.TODO(), parent)).To(Succeed())

	r := &VMAlertReconciler{
		Client:       fclient,
		BaseConf:     &config.BaseOperatorConf{},
		Log:          ctrl.Log.WithName("test"),
		OriginScheme: fclient.Scheme(),
	}
	nsn := types.NamespacedName{Name: parent.Name, Namespace: parent.Namespace}
	_, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: nsn})
	g.Expect(err).NotTo(HaveOccurred())

	var got vmv1beta1.VMRule
	g.Expect(fclient.Get(context.TODO(), types.NamespacedName{Name: child.Name, Namespace: child.Namespace}, &got)).To(Succeed())
	for _, c := range got.Status.Conditions {
		g.Expect(c.Type).NotTo(Equal(conditionType))
	}
	g.Expect(got.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusIgnored))
}

func TestVMAlert_Reconcile_AgentSync_Managed(t *testing.T) {
	g := NewWithT(t)
	managed := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAlertSpec{
			SelectAllByDefault: true,
		},
	}

	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{managed})
	r := &VMAlertReconciler{
		Client:       fclient,
		BaseConf:     &config.BaseOperatorConf{},
		Log:          ctrl.Log.WithName("test"),
		OriginScheme: fclient.Scheme(),
	}

	// start with locked alert reconcile
	locked := true
	alertSync.Lock()
	defer func() {
		if locked {
			alertSync.Unlock()
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
	g.Consistently(doneCh, "100ms").ShouldNot(BeClosed())

	// reconcile completes when alertSync is unlocked
	locked = false
	alertSync.Unlock()
	g.Eventually(doneCh, "5s").Should(BeClosed())
}

func TestVMAlert_Reconcile_AgentSync_Unmanaged(t *testing.T) {
	g := NewWithT(t)
	unmanaged := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unmanaged",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAlertSpec{},
	}

	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{unmanaged})
	r := &VMAlertReconciler{
		Client:       fclient,
		BaseConf:     &config.BaseOperatorConf{},
		Log:          ctrl.Log.WithName("test"),
		OriginScheme: fclient.Scheme(),
	}

	// Start with locked alert reconcile
	alertSync.Lock()
	defer alertSync.Unlock()

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
