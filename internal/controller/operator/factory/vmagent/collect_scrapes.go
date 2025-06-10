package vmagent

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

func selectScrapeConfig(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMScrapeConfig, error) {
	if cr.Spec.DaemonSetMode {
		return nil, nil
	}

	var selectedConfigs []*vmv1beta1.VMScrapeConfig
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		NamespaceSelector: cr.Spec.ScrapeConfigNamespaceSelector,
		ObjectSelector:    cr.Spec.ScrapeConfigSelector,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMScrapeConfigList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			selectedConfigs = append(selectedConfigs, item)
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, err
	}
	build.OrderByKeys(selectedConfigs, namespacedNames)
	logger.SelectedObjects(ctx, "VMScrapeConfigs", len(namespacedNames), 0, namespacedNames)

	return selectedConfigs, nil
}

func selectPodScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMPodScrape, error) {
	var selectedConfigs []*vmv1beta1.VMPodScrape
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		NamespaceSelector: cr.Spec.PodScrapeNamespaceSelector,
		ObjectSelector:    cr.Spec.PodScrapeSelector,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMPodScrapeList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			selectedConfigs = append(selectedConfigs, item)
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, err
	}

	build.OrderByKeys(selectedConfigs, namespacedNames)
	logger.SelectedObjects(ctx, "VMPodScrapes", len(namespacedNames), 0, namespacedNames)

	return selectedConfigs, nil
}

func selectVMProbes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMProbe, error) {
	if cr.Spec.DaemonSetMode {
		return nil, nil
	}
	var selectedConfigs []*vmv1beta1.VMProbe
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		NamespaceSelector: cr.Spec.ProbeNamespaceSelector,
		ObjectSelector:    cr.Spec.ProbeSelector,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMProbeList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			selectedConfigs = append(selectedConfigs, item)
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, err
	}

	build.OrderByKeys(selectedConfigs, namespacedNames)
	logger.SelectedObjects(ctx, "VMProbes", len(namespacedNames), 0, namespacedNames)
	return selectedConfigs, nil
}

func selectVMNodeScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMNodeScrape, error) {
	if cr.Spec.DaemonSetMode {
		return nil, nil
	}
	if !config.IsClusterWideAccessAllowed() && cr.IsOwnsServiceAccount() {
		logger.WithContext(ctx).Info("cannot use VMNodeScrape at operator in single namespace mode with default permissions." +
			" Create ServiceAccount for VMAgent manually if needed. Skipping config generation for it")
		return nil, nil
	}

	var selectedConfigs []*vmv1beta1.VMNodeScrape
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		NamespaceSelector: cr.Spec.NodeScrapeNamespaceSelector,
		ObjectSelector:    cr.Spec.NodeScrapeSelector,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMNodeScrapeList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			selectedConfigs = append(selectedConfigs, item)
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))

		}
	}); err != nil {
		return nil, err
	}

	build.OrderByKeys(selectedConfigs, namespacedNames)
	logger.SelectedObjects(ctx, "VMNodeScrapes", len(namespacedNames), 0, namespacedNames)

	return selectedConfigs, nil
}

func selectStaticScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMStaticScrape, error) {
	if cr.Spec.DaemonSetMode {
		return nil, nil
	}
	var selectedConfigs []*vmv1beta1.VMStaticScrape
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		NamespaceSelector: cr.Spec.StaticScrapeNamespaceSelector,
		ObjectSelector:    cr.Spec.StaticScrapeSelector,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMStaticScrapeList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			selectedConfigs = append(selectedConfigs, item)
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, err
	}
	build.OrderByKeys(selectedConfigs, namespacedNames)
	logger.SelectedObjects(ctx, "VMStaticScrape", len(namespacedNames), 0, namespacedNames)

	return selectedConfigs, nil
}

func selectServiceScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMServiceScrape, error) {
	if cr.Spec.DaemonSetMode {
		return nil, nil
	}

	var selectedConfigs []*vmv1beta1.VMServiceScrape
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		NamespaceSelector: cr.Spec.ServiceScrapeNamespaceSelector,
		ObjectSelector:    cr.Spec.ServiceScrapeSelector,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMServiceScrapeList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			rclient.Scheme().Default(item)
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			selectedConfigs = append(selectedConfigs, item)
		}
	}); err != nil {
		return nil, err
	}

	build.OrderByKeys(selectedConfigs, namespacedNames)
	logger.SelectedObjects(ctx, "VMServiceScrape", len(namespacedNames), 0, namespacedNames)

	return selectedConfigs, nil
}
