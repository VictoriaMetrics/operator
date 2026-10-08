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
	"errors"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	vmreconcile "github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

var _ = Describe("VMAgent Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		vmagent := &vmv1beta1.VMAgent{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind VMAgent")
			err := k8sClient.Get(ctx, typeNamespacedName, vmagent)
			if err != nil && k8serrors.IsNotFound(err) {
				resource := &vmv1beta1.VMAgent{
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
			resource := &vmv1beta1.VMAgent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance VMAgent")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VMAgentReconciler{
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

func TestVMAgent_Reconcile_AgentSync_Managed(t *testing.T) {
	g := NewWithT(t)
	managed := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managed",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			CommonScrapeParams: vmv1beta1.CommonScrapeParams{
				SelectAllByDefault: true,
			},
		},
	}

	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{managed})
	r := &VMAgentReconciler{
		Client:       fclient,
		BaseConf:     config.MustGetBaseConfig(),
		Log:          ctrl.Log.WithName("test"),
		OriginScheme: fclient.Scheme(),
	}

	// start with locked agent reconcile
	locked := true
	agentSync.Lock()
	defer func() {
		if locked {
			agentSync.Unlock()
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

	// reconcile completes when agentSync is unlocked
	locked = false
	agentSync.Unlock()
	g.Eventually(doneCh, "5s").Should(BeClosed())
}

func TestVMAgent_Reconcile_AgentSync_Unmanaged(t *testing.T) {
	g := NewWithT(t)
	unmanaged := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unmanaged",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			CommonScrapeParams: vmv1beta1.CommonScrapeParams{
				IngestOnlyMode: ptr.To(true),
			},
		},
	}

	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{unmanaged})
	r := &VMAgentReconciler{
		Client:       fclient,
		BaseConf:     config.MustGetBaseConfig(),
		Log:          ctrl.Log.WithName("test"),
		OriginScheme: fclient.Scheme(),
	}

	// Start with locked agent reconcile
	agentSync.Lock()
	defer agentSync.Unlock()

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

func TestVMAgent_Reconcile_SkipsUnselectedNamespaces(t *testing.T) {
	const agentNamespace = "agent-ns"
	const cleanupNamespace = "operator-cleanup-vmagent-cleanup"
	vmagent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "vmagent", Namespace: agentNamespace},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
		},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{vmagent})
	baseConf := *config.MustGetBaseConfig()
	baseConf.WatchNamespaces = []string{agentNamespace, cleanupNamespace}
	reconciler := &VMAgentReconciler{}
	reconciler.Init("vmagent", fclient, logr.Discard(), scheme.Scheme, &baseConf)

	_, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: vmagent.Name, Namespace: vmagent.Namespace}})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, obj := range []client.Object{&rbacv1.Role{}, &rbacv1.RoleBinding{}} {
		err := fclient.Get(context.Background(), types.NamespacedName{Name: vmagent.GetRBACName(), Namespace: cleanupNamespace}, obj)
		if !k8serrors.IsNotFound(err) {
			t.Fatalf("unexpected %T in cleanup namespace: %v", obj, err)
		}
	}
}

func TestVMAgent_Reconcile_UsesReconcilerWatchNamespaces(t *testing.T) {
	globalCfg := config.MustGetBaseConfig()
	previousCfg := *globalCfg
	defer func() { *globalCfg = previousCfg }()
	globalCfg.WatchNamespaces = []string{"global-ns"}

	vmagent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "vmagent", Namespace: "agent-ns"},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
		},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{vmagent})
	baseConf := *config.MustGetBaseConfig()
	baseConf.WatchNamespaces = []string{vmagent.Namespace}
	reconciler := &VMAgentReconciler{}
	reconciler.Init("vmagent", fclient, logr.Discard(), scheme.Scheme, &baseConf)

	if _, err := reconciler.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: vmagent.Name, Namespace: vmagent.Namespace}}); err != nil {
		t.Fatal(err)
	}

	for _, obj := range []struct {
		kind string
		obj  client.Object
	}{
		{kind: "Role", obj: &rbacv1.Role{}},
		{kind: "RoleBinding", obj: &rbacv1.RoleBinding{}},
	} {
		if err := fclient.Get(context.Background(), types.NamespacedName{Name: vmagent.GetRBACName(), Namespace: vmagent.Namespace}, obj.obj); err != nil {
			t.Errorf("get %s in vmagent namespace: %v", obj.kind, err)
		}
	}
}

func TestVMAgent_Reconcile_DeleteReleasesAppliedCondition(t *testing.T) {
	ctx := context.Background()
	now := metav1.Now()
	vmagent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "vmagent",
			Namespace:         "ns",
			Finalizers:        []string{vmv1beta1.FinalizerName},
			DeletionTimestamp: &now,
		},
		Spec: vmv1beta1.VMAgentSpec{
			SelectAllByDefault: true,
			RemoteWrite:        []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
		},
	}
	ss := &vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{Name: "ss", Namespace: "ns"}}
	ps := &vmv1beta1.VMPodScrape{ObjectMeta: metav1.ObjectMeta{Name: "ps", Namespace: "ns"}}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{vmagent, ss, ps})

	// simulate a prior reconcile that selected both scrape objects
	parent := "vmagent.ns.vmagent"
	if err := vmreconcile.StatusForChildObjects(ctx, fclient, parent, []*vmv1beta1.VMServiceScrape{ss}); err != nil {
		t.Fatal(err)
	}
	if err := vmreconcile.StatusForChildObjects(ctx, fclient, parent, []*vmv1beta1.VMPodScrape{ps}); err != nil {
		t.Fatal(err)
	}
	var gotSS vmv1beta1.VMServiceScrape
	var gotPS vmv1beta1.VMPodScrape
	nsn := func(name string) types.NamespacedName { return types.NamespacedName{Namespace: "ns", Name: name} }
	if err := fclient.Get(ctx, nsn("ss"), &gotSS); err != nil || len(gotSS.Status.Conditions) == 0 {
		t.Fatalf("precondition: VMServiceScrape must carry the Applied condition: %v", err)
	}
	if err := fclient.Get(ctx, nsn("ps"), &gotPS); err != nil || len(gotPS.Status.Conditions) == 0 {
		t.Fatalf("precondition: VMPodScrape must carry the Applied condition: %v", err)
	}

	reconciler := &VMAgentReconciler{}
	reconciler.Init("vmagent", fclient, logr.Discard(), scheme.Scheme, config.MustGetBaseConfig())
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nsn("vmagent")}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if err := fclient.Get(ctx, nsn("ss"), &gotSS); err != nil {
		t.Fatal(err)
	}
	if len(gotSS.Status.Conditions) != 0 {
		t.Errorf("VMServiceScrape condition must be released on VMAgent delete, got %v", gotSS.Status.Conditions)
	}
	if err := fclient.Get(ctx, nsn("ps"), &gotPS); err != nil {
		t.Fatal(err)
	}
	if len(gotPS.Status.Conditions) != 0 {
		t.Errorf("VMPodScrape condition must be released on VMAgent delete, got %v", gotPS.Status.Conditions)
	}
	var gotAgent vmv1beta1.VMAgent
	if err := fclient.Get(ctx, nsn("vmagent"), &gotAgent); err != nil {
		// removing the last finalizer of a deleting object deletes it, so NotFound is the success case
		if !k8serrors.IsNotFound(err) {
			t.Fatal(err)
		}
	} else if len(gotAgent.Finalizers) != 0 {
		t.Errorf("finalizer must be removed, got %v", gotAgent.Finalizers)
	}
}

func TestVMAgent_Reconcile_DeleteKeepsFinalizerOnReleaseError(t *testing.T) {
	ctx := context.Background()
	now := metav1.Now()
	vmagent := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "vmagent",
			Namespace:         "ns",
			Finalizers:        []string{vmv1beta1.FinalizerName},
			DeletionTimestamp: &now,
		},
		Spec: vmv1beta1.VMAgentSpec{
			SelectAllByDefault: true,
			RemoteWrite:        []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
		},
	}
	fns := k8stools.GetInterceptorsWithObjects(nil)
	fns.List = func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		if _, ok := list.(*vmv1beta1.VMServiceScrapeList); ok {
			return errors.New("transient list error")
		}
		return c.List(ctx, list, opts...)
	}
	fclient := k8stools.GetTestClientWithObjectsAndInterceptors([]runtime.Object{vmagent}, fns)

	reconciler := &VMAgentReconciler{}
	reconciler.Init("vmagent", fclient, logr.Discard(), scheme.Scheme, config.MustGetBaseConfig())
	nsn := types.NamespacedName{Namespace: "ns", Name: "vmagent"}
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nsn}); err == nil {
		t.Fatal("reconcile must return the release error")
	}
	var got vmv1beta1.VMAgent
	if err := fclient.Get(ctx, nsn, &got); err != nil {
		t.Fatalf("vmagent must still exist while the release fails: %v", err)
	}
	if len(got.Finalizers) == 0 {
		t.Error("finalizer must be kept when the release fails")
	}
}
