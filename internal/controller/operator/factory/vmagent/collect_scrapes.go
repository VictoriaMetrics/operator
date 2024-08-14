package vmagent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func selectScrapeConfig(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMScrapeConfig, error) {
	var scrapeConfigsCombined []*vmv1beta1.VMScrapeConfig
	var namespacedNames []string

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.NodeScrapeNamespaceSelector, cr.Spec.ScrapeConfigSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMScrapeConfigList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				item := item
				scrapeConfigsCombined = append(scrapeConfigsCombined, &item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
		return nil, err
	}
	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMScrapeConfig]{target: scrapeConfigsCombined, sorter: namespacedNames})
	if len(namespacedNames) > 0 {
		logger.WithContext(ctx).Info("selected scrapeConfigs", "scrapeConfigs", strings.Join(namespacedNames, ","), "namespace", cr.Namespace, "vmagent", cr.Name)
	}

	return scrapeConfigsCombined, nil
}

func selectPodScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMPodScrape, error) {
	var podScrapesCombined []*vmv1beta1.VMPodScrape
	var namespacedNames []string

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.PodScrapeNamespaceSelector, cr.Spec.PodScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMPodScrapeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				item := item
				podScrapesCombined = append(podScrapesCombined, &item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
		return nil, err
	}

	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMPodScrape]{target: podScrapesCombined, sorter: namespacedNames})
	if len(namespacedNames) > 0 {
		logger.WithContext(ctx).Info("selected PodScrapes", "podscrapes", strings.Join(namespacedNames, ","), "namespace", cr.Namespace, "vmagent", cr.Name)
	}

	return podScrapesCombined, nil
}

func selectVMProbes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMProbe, error) {
	var probesCombined []*vmv1beta1.VMProbe
	var namespacedNames []string
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ProbeNamespaceSelector, cr.Spec.ProbeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMProbeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				item := item
				probesCombined = append(probesCombined, &item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
		return nil, err
	}

	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMProbe]{target: probesCombined, sorter: namespacedNames})
	if len(namespacedNames) > 0 {
		logger.WithContext(ctx).Info("selected VMProbes", "vmProbes", strings.Join(namespacedNames, ","), "namespace", cr.Namespace, "vmagent", cr.Name)
	}

	return probesCombined, nil
}

func selectVMNodeScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMNodeScrape, error) {
	l := logger.WithContext(ctx).WithValues("vmagent", cr.Name)
	if !config.IsClusterWideAccessAllowed() && cr.IsOwnsServiceAccount() {
		l.Info("cannot use VMNodeScrape at operator in single namespace mode with default permissions. Create ServiceAccount for VMAgent manually if needed. Skipping config generation for it")
		return nil, nil
	}

	var nodesCombined []*vmv1beta1.VMNodeScrape
	var namespacedNames []string

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient,
		cr.Spec.NodeScrapeNamespaceSelector, cr.Spec.NodeScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault, func(list *vmv1beta1.VMNodeScrapeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				item := item
				nodesCombined = append(nodesCombined, &item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))

			}
		}); err != nil {
		return nil, err
	}

	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMNodeScrape]{target: nodesCombined, sorter: namespacedNames})
	if len(namespacedNames) > 0 {
		l.Info("selected VMNodeScrapes", "VMNodeScrapes", strings.Join(namespacedNames, ","))
	}

	return nodesCombined, nil
}

func selectStaticScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMStaticScrape, error) {
	var staticScrapesCombined []*vmv1beta1.VMStaticScrape
	var namespacedNames []string
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.StaticScrapeNamespaceSelector, cr.Spec.StaticScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMStaticScrapeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				item := item
				staticScrapesCombined = append(staticScrapesCombined, &item)
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
		return nil, err
	}
	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMStaticScrape]{target: staticScrapesCombined, sorter: namespacedNames})
	if len(namespacedNames) > 0 {
		logger.WithContext(ctx).Info("selected StaticScrapes", "staticScrapes", strings.Join(namespacedNames, ","), "namespace", cr.Namespace, "vmagent", cr.Name)
	}

	return staticScrapesCombined, nil
}

func selectServiceScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) ([]*vmv1beta1.VMServiceScrape, error) {
	var servScrapesCombined []*vmv1beta1.VMServiceScrape
	var serviceScrapeNamespacedNames []string
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ServiceScrapeNamespaceSelector, cr.Spec.ServiceScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMServiceScrapeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				item := item
				serviceScrapeNamespacedNames = append(serviceScrapeNamespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
				servScrapesCombined = append(servScrapesCombined, &item)
			}
		}); err != nil {
		return nil, err
	}

	// filter out all service scrapes that access
	// the file system.
	// TODO this restriction applies only ServiceScrape, make it global to any other objects
	if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
		var cnt int
	OUTER:
		for idx, sm := range servScrapesCombined {
			for _, endpoint := range sm.Spec.Endpoints {
				if err := testForArbitraryFSAccess(endpoint.EndpointAuth); err != nil {
					logger.WithContext(ctx).Info("skipping vmservicescrape",
						"error", err.Error(),
						"vmservicescrape", serviceScrapeNamespacedNames[idx],
						"namespace", cr.Namespace,
						"vmagent", cr.Name,
					)
					continue OUTER
				}
			}
			servScrapesCombined[cnt] = sm
			serviceScrapeNamespacedNames[cnt] = serviceScrapeNamespacedNames[idx]
			cnt++
		}
		servScrapesCombined = servScrapesCombined[:cnt]
		serviceScrapeNamespacedNames = serviceScrapeNamespacedNames[:cnt]
	}
	sort.Sort(&namespacedNameSorter[*vmv1beta1.VMServiceScrape]{sorter: serviceScrapeNamespacedNames, target: servScrapesCombined})

	if len(serviceScrapeNamespacedNames) > 0 {
		logger.WithContext(ctx).Info("selected ServiceScrapes", "servicescrapes", strings.Join(serviceScrapeNamespacedNames, ","), "namespace", cr.Namespace, "vmagent", cr.Name)
	}

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
