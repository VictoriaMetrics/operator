package dryrun

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/manager"
)

const (
	deploymentQuota corev1.ResourceName = "count/deployments.apps"
	quotaName       string              = "no-deployments"
	operatorSAName  string              = "dry-run-operator"
	webhookName     string              = "dry-run.test.victoriametrics.com"
)

// operatorRules lets the operator read everything and write all it needs, except NetworkPolicies.
var operatorRules = []rbacv1.PolicyRule{
	{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"get", "list", "watch"}},
	{
		APIGroups: []string{"", "apps", "rbac.authorization.k8s.io", vmv1beta1.SchemeGroupVersion.Group},
		Resources: []string{
			"serviceaccounts", "services", "configmaps", "secrets", "events", "persistentvolumeclaims", "deployments",
			"clusterroles", "clusterrolebindings", "roles", "rolebindings",
			"vmsingles", "vmsingles/status", "vmsingles/finalizers", "vmservicescrapes",
		},
		Verbs: []string{"create", "update", "patch", "delete"},
	},
}

// useRestrictedOperatorIdentity runs the operator as a ServiceAccount with operatorRules and returns its user name.
func useRestrictedOperatorIdentity(ctx context.Context) string {
	role := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "dry-run-operator-" + namespace}, Rules: operatorRules}
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: role.Name},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: role.Name},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Namespace: namespace, Name: operatorSAName}},
	}
	for _, obj := range []client.Object{role, binding} {
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed()) })
	}

	user := "system:serviceaccount:" + namespace + ":" + operatorSAName
	orig := manager.GetRESTConfig
	// the admin config impersonates the ServiceAccount, RBAC matches the binding subject by name
	manager.GetRESTConfig = func() *rest.Config {
		cfg := rest.CopyConfig(&k8sCfg)
		cfg.Impersonate = rest.ImpersonationConfig{UserName: user}
		return cfg
	}
	DeferCleanup(func() { manager.GetRESTConfig = orig })
	return user
}

// runDryRunOperator runs the operator in -dry-run mode until it reconciles existing objects and returns its logs.
func runDryRunOperator(ctx context.Context) string {
	// logs are the only place where reconcile errors are reported in dry-run mode
	var logs bytes.Buffer
	GinkgoWriter.TeeTo(&logs)
	os.Args = append(os.Args[:1],
		"--dry-run",
		"--metrics-bind-address", "0",
		"--pprof-addr", "0",
		"--health-probe-bind-address", "0",
	)
	runErr := manager.RunManager(ctx)
	// detach under the writer lock, so late log lines don't race with reading the buffer
	GinkgoWriter.ClearTeeWriters()
	Expect(runErr).ToNot(HaveOccurred())

	return logs.String()
}

// lineOf returns the only log line that mentions all given substrings, or "" if there is none.
func lineOf(logs string, substrs ...string) string {
	var res []string
	for _, l := range strings.Split(logs, "\n") {
		if !slices.ContainsFunc(substrs, func(s string) bool { return !strings.Contains(l, s) }) {
			res = append(res, l)
		}
	}
	ExpectWithOffset(1, len(res)).To(BeNumerically("<=", 1), "want at most one log line with %q, got: %q", substrs, res)
	if len(res) == 0 {
		return ""
	}
	return res[0]
}

func newVMSingle(name string) *vmv1beta1.VMSingle {
	return &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       vmv1beta1.VMSingleSpec{RetentionPeriod: "1"},
	}
}

var _ = Describe("operator in dry-run mode", Ordered, func() {
	var logs string
	ctx := context.Background()
	create, update, remove := newVMSingle("dry-create"), newVMSingle("dry-update"), newVMSingle("dry-remove")
	noNetPol, webhook := newVMSingle("dry-rbac"), newVMSingle("dry-webhook")
	noNetPol.Spec.NetworkPolicy = &vmv1beta1.EmbeddedNetworkPolicy{}
	var operatorUser string
	childKey := func(cr *vmv1beta1.VMSingle) client.ObjectKey {
		return client.ObjectKey{Namespace: namespace, Name: cr.PrefixedName()}
	}
	// svc is an existing Service so that the operator would attempt to update its ports.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      update.PrefixedName(),
			Labels:    update.SelectorLabels(),
		},
		Spec: corev1.ServiceSpec{
			Selector: update.SelectorLabels(),
			Ports:    []corev1.ServicePort{{Name: "http", Port: 1234, TargetPort: intstr.FromInt32(1234)}},
		},
	}
	// role is a cluster-wide object the operator deletes with the VMSingle.
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: remove.GetRBACName(), Labels: remove.SelectorLabels()},
	}

	BeforeAll(func() {
		// Speed up readyness check
		Expect(os.Setenv("VM_APPREADYTIMEOUT", "3s")).To(Succeed())

		// create a quota that rejects Deployment creation
		quota := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: quotaName},
			Spec:       corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{deploymentQuota: resource.MustParse("0")}},
		}
		Expect(k8sClient.Create(ctx, quota)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, quota))).To(Succeed()) })
		Eventually(func() bool {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(quota), quota)).To(Succeed())
			_, ok := quota.Status.Hard[deploymentQuota]
			return ok
		}, 30*time.Second, time.Second).Should(BeTrue())

		// the webhook is unreachable, so the API server must fail the request itself
		hook := &admissionv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "dry-run-" + namespace},
			Webhooks: []admissionv1.ValidatingWebhook{{
				Name:                    webhookName,
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             ptr.To(admissionv1.SideEffectClassNone),
				FailurePolicy:           ptr.To(admissionv1.Fail),
				ClientConfig: admissionv1.WebhookClientConfig{
					Service: &admissionv1.ServiceReference{Namespace: namespace, Name: "no-such-webhook", Path: ptr.To("/validate")},
				},
				ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/instance": webhook.Name}},
				Rules: []admissionv1.RuleWithOperations{{
					Operations: []admissionv1.OperationType{admissionv1.Create},
					Rule:       admissionv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"services"}},
				}},
			}},
		}
		Expect(k8sClient.Create(ctx, hook)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, hook))).To(Succeed()) })

		operatorUser = useRestrictedOperatorIdentity(ctx)

		for _, obj := range []client.Object{create, update, noNetPol, webhook, svc, role} {
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed()) })
		}

		// the finalizer keeps the object after the deletion, as it does under a real operator
		remove.Finalizers = []string{vmv1beta1.FinalizerName}
		Expect(k8sClient.Create(ctx, remove)).To(Succeed())
		DeferCleanup(func() {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(remove), remove)).To(Succeed())
			remove.Finalizers = nil
			Expect(client.IgnoreNotFound(k8sClient.Update(ctx, remove))).To(Succeed())
		})
		Expect(k8sClient.Delete(ctx, remove)).To(Succeed())

		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		logs = runDryRunOperator(runCtx)
	})

	// reconcile errors are only in the logs, so print them all when any spec fails
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			AddReportEntry("operator logs", logs, ReportEntryVisibilityFailureOrVerbose)
		}
	})

	// absent fails unless obj does not exist.
	absent := func(obj client.Object, key client.ObjectKey) {
		err := k8sClient.Get(ctx, key, obj)
		ExpectWithOffset(1, k8serrors.IsNotFound(err)).To(BeTrue(), "%T %s must not exist, got err: %v", obj, key, err)
	}

	Context("create", func() {
		It("should log creation of every child", func() {
			Expect(lineOf(logs, "reconciling object", create.Name)).ToNot(BeEmpty())
			for _, kind := range []string{"ServiceAccount", "Service", "Deployment"} {
				Expect(lineOf(logs, "creating new "+kind+"="+namespace+"/"+create.PrefixedName())).ToNot(BeEmpty(), kind)
				Expect(lineOf(logs, `"msg":"dry-run: Create"`, `"kind":"`+kind+`"`, `"name":"`+create.PrefixedName()+`"`)).ToNot(BeEmpty(), kind)
			}
		})

		It("should report the Deployment rejected by the API server", func() {
			errLine := lineOf(logs, `"msg":"reconcile failed"`, `"name":"`+create.Name+`"`)
			Expect(errLine).To(ContainSubstring("cannot create new Deployment=" + namespace + "/" + create.PrefixedName()))
			Expect(errLine).To(ContainSubstring("exceeded quota: " + quotaName))
		})

		It("should not create children", func() {
			absent(&corev1.ServiceAccount{}, childKey(create))
			absent(&corev1.Service{}, childKey(create))
			absent(&appsv1.Deployment{}, childKey(create))
		})

		It("should not change the VMSingle", func() {
			var got vmv1beta1.VMSingle
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(create), &got)).To(Succeed())
			Expect(got.Finalizers).To(BeEmpty())
			Expect(got.Status.UpdateStatus).To(BeEmpty())
		})
	})

	Context("insufficient RBAC", func() {
		It("should report the permission the operator lacks", func() {
			errLine := lineOf(logs, `"msg":"reconcile failed"`, `"name":"`+noNetPol.Name+`"`)
			Expect(errLine).To(ContainSubstring(`networkpolicies.networking.k8s.io is forbidden: User \"` + operatorUser + `\" cannot create resource \"networkpolicies\"`))
		})

		It("should not create the NetworkPolicy and stop before the Deployment", func() {
			absent(&networkingv1.NetworkPolicy{}, childKey(noNetPol))
			absent(&appsv1.Deployment{}, childKey(noNetPol))
		})
	})

	Context("validating webhook", func() {
		It("should report the rejection by the webhook", func() {
			errLine := lineOf(logs, `"msg":"reconcile failed"`, `"name":"`+webhook.Name+`"`)
			Expect(errLine).To(ContainSubstring("cannot create new Service=" + namespace + "/" + webhook.PrefixedName()))
			Expect(errLine).To(ContainSubstring(`failed calling webhook \"` + webhookName + `\"`))
		})

		It("should not create the Service and stop before the Deployment", func() {
			absent(&corev1.Service{}, childKey(webhook))
			absent(&appsv1.Deployment{}, childKey(webhook))
		})

		It("should not affect other Services", func() {
			Expect(lineOf(logs, `"msg":"dry-run: Create"`, `"kind":"Service"`, `"name":"`+create.PrefixedName()+`"`)).ToNot(BeEmpty())
		})
	})

	Context("update", func() {
		It("should log the Service diff and its update", func() {
			diff := lineOf(logs, "updating Service name="+namespace+"/"+update.PrefixedName())
			Expect(diff).To(ContainSubstring(`"spec.ports[0].port":{"--":1234,"++":8429}`))
			Expect(lineOf(logs, `"msg":"dry-run: Update"`, `"kind":"Service"`, `"name":"`+update.PrefixedName()+`"`)).ToNot(BeEmpty())
		})

		It("should not change the existing Service", func() {
			var got corev1.Service
			Expect(k8sClient.Get(ctx, childKey(update), &got)).To(Succeed())
			Expect(got.ResourceVersion).To(Equal(svc.ResourceVersion))
			Expect(got.Spec.Ports).To(HaveLen(1))
			Expect(got.Spec.Ports[0].Port).To(Equal(int32(1234)))
		})

		It("should not create the other children", func() {
			absent(&corev1.ServiceAccount{}, childKey(update))
			absent(&appsv1.Deployment{}, childKey(update))
		})
	})

	Context("delete", func() {
		It("should log deletion of the ClusterRole without errors", func() {
			Expect(lineOf(logs, `"msg":"dry-run: Delete"`, `"kind":"ClusterRole"`, `"name":"`+role.Name+`"`)).ToNot(BeEmpty())
			Expect(lineOf(logs, `"msg":"reconcile failed"`, `"name":"`+remove.Name+`"`)).To(BeEmpty())
		})

		It("should not delete the ClusterRole", func() {
			var got rbacv1.ClusterRole
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(role), &got)).To(Succeed())
			Expect(got.DeletionTimestamp).To(BeNil())
			Expect(got.UID).To(Equal(role.UID))
		})

		It("should keep the VMSingle with its finalizer", func() {
			var got vmv1beta1.VMSingle
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(remove), &got)).To(Succeed())
			Expect(got.DeletionTimestamp).ToNot(BeNil())
			Expect(got.Finalizers).To(ContainElement(vmv1beta1.FinalizerName))
		})
	})
})
