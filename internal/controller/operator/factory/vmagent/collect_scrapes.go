package vmagent

import (
	"context"
	"fmt"
	"sort"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func selectScrapeConfig(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMScrapeConfig, error) {
	var scrapeConfigsCombined []*vmv1beta1.VMScrapeConfig
	var namespacedNames []string

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ScrapeConfigNamespaceSelector, cr.Spec.ScrapeConfigSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMScrapeConfigList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				scrapeConfigsCombined = append(scrapeConfigsCombined, item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
		return nil, err
	}
	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMScrapeConfig]{target: scrapeConfigsCombined, sorter: namespacedNames})
	logger.SelectedObjects(ctx, "VMScrapeConfigs", len(namespacedNames), 0, namespacedNames)

	return scrapeConfigsCombined, nil
}

func selectPodScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMPodScrape, error) {
	var podScrapesCombined []*vmv1beta1.VMPodScrape
	var namespacedNames []string

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.PodScrapeNamespaceSelector, cr.Spec.PodScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMPodScrapeList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				podScrapesCombined = append(podScrapesCombined, item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
		return nil, err
	}

	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMPodScrape]{target: podScrapesCombined, sorter: namespacedNames})
	logger.SelectedObjects(ctx, "VMPodScrapes", len(namespacedNames), 0, namespacedNames)

	return podScrapesCombined, nil
}

func selectVMProbes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMProbe, error) {
	var probesCombined []*vmv1beta1.VMProbe
	var namespacedNames []string
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ProbeNamespaceSelector, cr.Spec.ProbeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMProbeList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				probesCombined = append(probesCombined, item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
		return nil, err
	}

	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMProbe]{target: probesCombined, sorter: namespacedNames})
	logger.SelectedObjects(ctx, "VMProbes", len(namespacedNames), 0, namespacedNames)
	return probesCombined, nil
}

func selectVMNodeScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMNodeScrape, error) {
	if !config.IsClusterWideAccessAllowed() && cr.IsOwnsServiceAccount() {
		logger.WithContext(ctx).Info("cannot use VMNodeScrape at operator in single namespace mode with default permissions." +
			" Create ServiceAccount for VMAgent manually if needed. Skipping config generation for it")
		return nil, nil
	}

	var nodesCombined []*vmv1beta1.VMNodeScrape
	var namespacedNames []string

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient,
		cr.Spec.NodeScrapeNamespaceSelector, cr.Spec.NodeScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault, func(list *vmv1beta1.VMNodeScrapeList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				nodesCombined = append(nodesCombined, item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))

			}
		}); err != nil {
		return nil, err
	}

	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMNodeScrape]{target: nodesCombined, sorter: namespacedNames})
	logger.SelectedObjects(ctx, "VMNodeScrapes", len(namespacedNames), 0, namespacedNames)

	return nodesCombined, nil
}

func selectStaticScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMStaticScrape, error) {
	var staticScrapesCombined []*vmv1beta1.VMStaticScrape
	var namespacedNames []string
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.StaticScrapeNamespaceSelector, cr.Spec.StaticScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMStaticScrapeList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				staticScrapesCombined = append(staticScrapesCombined, item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
		return nil, err
	}
	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMStaticScrape]{target: staticScrapesCombined, sorter: namespacedNames})
	logger.SelectedObjects(ctx, "VMStaticScrape", len(namespacedNames), 0, namespacedNames)

	return staticScrapesCombined, nil
}

func selectServiceScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMServiceScrape, error) {
	var servScrapesCombined []*vmv1beta1.VMServiceScrape
	var namespacedNames []string
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ServiceScrapeNamespaceSelector, cr.Spec.ServiceScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMServiceScrapeList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				rclient.Scheme().Default(item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
				servScrapesCombined = append(servScrapesCombined, item)
			}
		}); err != nil {
		return nil, err
	}

	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMServiceScrape]{sorter: namespacedNames, target: servScrapesCombined})
	logger.SelectedObjects(ctx, "VMServiceScrape", len(namespacedNames), 0, namespacedNames)

	return servScrapesCombined, nil
}

type namespacedNameSorter[T any] struct {
	target []T
	sorter []string
}

func (nn *namespacedNameSorter[T]) Len() int {
	return len(nn.sorter)
}

func (nn *namespacedNameSorter[T]) Less(i, j int) bool {
	return nn.sorter[i] < nn.sorter[j]
}

func (nn *namespacedNameSorter[T]) Swap(i, j int) {
	nn.target[i], nn.target[j] = nn.target[j], nn.target[i]
	nn.sorter[i], nn.sorter[j] = nn.sorter[j], nn.sorter[i]
}
