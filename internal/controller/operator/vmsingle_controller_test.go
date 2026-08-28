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

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/VictoriaMetrics/operator/api/client/versioned/scheme"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

var _ = Describe("VMSingle Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		vmsingle := &vmv1beta1.VMSingle{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind VMSingle")
			err := k8sClient.Get(ctx, typeNamespacedName, vmsingle)
			if err != nil && k8serrors.IsNotFound(err) {
				resource := &vmv1beta1.VMSingle{
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
			resource := &vmv1beta1.VMSingle{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance VMSingle")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VMSingleReconciler{
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

func TestVMSingle_Reconcile_UsesReconcilerWatchNamespaces(t *testing.T) {
	globalCfg := config.MustGetBaseConfig()
	previousCfg := *globalCfg
	defer func() { *globalCfg = previousCfg }()
	globalCfg.WatchNamespaces = []string{"single-ns", "global-ns"}

	ingestOnly := false
	vmsingle := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "vmsingle", Namespace: "single-ns"},
		Spec: vmv1beta1.VMSingleSpec{
			CommonScrapeParams: vmv1beta1.CommonScrapeParams{IngestOnlyMode: &ingestOnly},
		},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{vmsingle})
	baseConf := *config.MustGetBaseConfig()
	baseConf.WatchNamespaces = []string{vmsingle.Namespace}
	reconciler := &VMSingleReconciler{}
	reconciler.Init("vmsingle", fclient, logr.Discard(), scheme.Scheme, &baseConf)
	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: vmsingle.Name, Namespace: vmsingle.Namespace}})
	require.NoError(t, err)

	for _, obj := range []struct {
		kind string
		obj  client.Object
	}{
		{kind: "Role", obj: &rbacv1.Role{}},
		{kind: "RoleBinding", obj: &rbacv1.RoleBinding{}},
	} {
		require.NoErrorf(t, fclient.Get(context.Background(), types.NamespacedName{Name: vmsingle.GetRBACName(), Namespace: vmsingle.Namespace}, obj.obj), "get %s", obj.kind)
	}

	require.True(t, k8serrors.IsNotFound(fclient.Get(context.Background(), types.NamespacedName{Name: vmsingle.GetRBACName(), Namespace: "global-ns"}, &rbacv1.Role{})))
}
