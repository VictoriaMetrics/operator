package manager

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/VictoriaMetrics/operator/internal/config"
)

func RunDryRunMode(ctx context.Context, mgr ctrl.Manager, baseConfig *config.BaseOperatorConf, controllers map[string]crdController) error {
	setupLog.Info("starting dry run mode on existing objects")

	// Start manager in background to handle caches
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		if err := mgr.Start(ctx); err != nil {
			setupLog.Error(err, "manager stopped")
		}
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		return fmt.Errorf("failed to sync cache")
	}

	c := mgr.GetClient()
	namespaces := baseConfig.WatchNamespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}

	for name, crdCtrl := range controllers {
		crdCtrl.Init(name, c, setupLog, mgr.GetScheme(), baseConfig)
		r, ok := crdCtrl.(reconcile.Reconciler)
		if !ok {
			setupLog.Info("skipping controller, does not implement Reconciler", "name", name)
			continue
		}

		// Retrieve the correct GroupVersionKind from the scheme
		var group, version string
		found := false

		for gvk := range mgr.GetScheme().AllKnownTypes() {
			if gvk.Kind == name {
				group = gvk.Group
				version = gvk.Version
				found = true
				break
			}
		}

		if !found {
			setupLog.Info("skipping controller, unable to find GroupVersionKind", "name", name)
			continue
		}

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   group,
			Version: version,
			Kind:    name + "List",
		})

		// list objects only at watched namespaces, empty namespace means all namespaces
		var items []unstructured.Unstructured
		for _, ns := range namespaces {
			if err := c.List(ctx, list, client.InNamespace(ns)); err != nil {
				setupLog.Error(err, "failed to list objects", "kind", name, "namespace", ns)
				continue
			}
			items = append(items, list.Items...)
		}

		for _, item := range items {
			req := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: item.GetNamespace(),
					Name:      item.GetName(),
				},
			}
			setupLog.Info("reconciling object", "kind", name, "name", item.GetName(), "namespace", item.GetNamespace())
			_, err := r.Reconcile(ctx, req)
			if err != nil {
				setupLog.Error(err, "reconcile failed", "kind", name, "name", item.GetName(), "namespace", item.GetNamespace())
			}
		}
	}

	setupLog.Info("dry run completed successfully")
	return nil
}
