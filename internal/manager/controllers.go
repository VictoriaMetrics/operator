package manager

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	authorizationv1 "k8s.io/api/authorization/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

var promCRDControllers = map[schema.GroupVersion]sets.Set[string]{
	promv1.SchemeGroupVersion: sets.New[string](
		"PodMonitor",
		"ServiceMonitor",
		"Probe",
		"PrometheusRule",
	),
	promv1alpha1.SchemeGroupVersion: sets.New[string](
		"ScrapeConfig",
		"AlertmanagerConfig",
	),
}

// resolveControllers returns an error if an enabled controller CRD is missing or the operator has no list and watch access to it.
func resolveControllers(ctx context.Context, l logr.Logger, baseClient *kubernetes.Clientset, bs *config.BaseOperatorConf, disabledControllerNames sets.Set[string]) (map[string]crdController, error) {
	type groupResource struct {
		// group holds Kubernetes resource group name without version.
		group string
		// name is Kubernetes resource name.
		name string
	}
	existGroupResourceByKind := make(map[string]groupResource)
	for groupVersion, kinds := range promCRDControllers {
		api, err := baseClient.ServerResourcesForGroupVersion(groupVersion.String())
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("cannot get server resource for API=%s: %w", groupVersion, err)
			}
		} else {
			for _, r := range api.APIResources {
				if strings.Contains(r.Name, "/") {
					// skip subresources, e.g. servicemonitors/status. They have the same Kind as the main resource
					// and would overwrite it, so the RBAC check would ask for list/watch on the subresource
					continue
				}
				existGroupResourceByKind[r.Kind] = groupResource{group: groupVersion.Group, name: r.Name}
				if _, ok := controllersByName[r.Kind]; ok && kinds.Has(r.Kind) {
					kinds.Delete(r.Kind)
				}
			}
		}
		disabledControllerNames.Insert(kinds.UnsortedList()...)
	}

	for _, gv := range []schema.GroupVersion{vmv1.SchemeGroupVersion, vmv1beta1.SchemeGroupVersion, vmv1alpha1.SchemeGroupVersion} {
		api, err := baseClient.ServerResourcesForGroupVersion(gv.String())
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("cannot get server resource for API=%s: %w", gv, err)
			}
			continue
		}
		for _, ar := range api.APIResources {
			if strings.Contains(ar.Name, "/") {
				// skip subresources
				continue
			}
			existGroupResourceByKind[ar.Kind] = groupResource{group: gv.Group, name: ar.Name}
		}
	}
	controllersToInit := make(map[string]crdController)
	for name, ct := range controllersByName {
		if ct.IsDisabled(bs, disabledControllerNames) {
			disabledControllerNames.Insert(name)
		}
		if disabledControllerNames.Has(name) {
			l.Info("controller disabled either by provided flag or respective CRD is not found", "name", name, "controller.disableReconcileFor", *disableControllerForCRD)
			continue
		}
		gr, ok := existGroupResourceByKind[name]
		if !ok {
			return nil, fmt.Errorf("controller: %s is missing at Kubernetes CRDs, make sure that it's installed or disable controller with -controller.disableReconcileFor= flag", name)
		}
		if err := checkWatchListRBACAccess(ctx, baseClient, bs.WatchNamespaces, gr.group, gr.name); err != nil {
			return nil, fmt.Errorf("operator doesn't have access RBAC access to controller: %s, make sure that operator has correct permissions or disable controller with -controller.disableReconcileFor= flag: %w", name, err)
		}
		controllersToInit[name] = ct
	}
	build.SetDisabledControllers(disabledControllerNames)
	return controllersToInit, nil
}

func initControllers(mgr ctrl.Manager, l logr.Logger, bs *config.BaseOperatorConf, controllersToInit map[string]crdController) error {
	for name, ct := range controllersToInit {
		ct.Init(name, mgr.GetClient(), l, mgr.GetScheme(), bs)
		if err := ct.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("cannot setup controller=%q: %w", name, err)
		}
	}
	return nil
}

func checkWatchListRBACAccess(ctx context.Context, baseClient *kubernetes.Clientset, namespaces []string, group, resource string) error {
	if len(namespaces) == 0 {
		// Empty namespace equals to cluster wide access.
		namespaces = []string{""}
	}
	for _, ns := range namespaces {
		for _, verb := range []string{"watch", "list"} {
			sar := &authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{
					ResourceAttributes: &authorizationv1.ResourceAttributes{
						Namespace: ns,
						Group:     group,
						Verb:      verb,
						Resource:  resource,
					},
				},
			}
			result, err := baseClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
			if err != nil {
				return err
			}
			if !result.Status.Allowed {
				return fmt.Errorf("access denied for namespace=%q,verb=%q,group=%q,resource=%q", ns, verb, group, resource)
			}
		}
	}

	return nil
}
