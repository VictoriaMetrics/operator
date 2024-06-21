package vmagent

import (
	"context"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func selectScrapeConfig(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (map[string]*vmv1beta1.VMScrapeConfig, error) {
	res := make(map[string]*vmv1beta1.VMScrapeConfig)
	var scrapeConfigsCombined []vmv1beta1.VMScrapeConfig

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.NodeScrapeNamespaceSelector, cr.Spec.ScrapeConfigSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMScrapeConfigList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				scrapeConfigsCombined = append(scrapeConfigsCombined, item)
			}
		}); err != nil {
		return nil, err
	}

	for _, scrapeConf := range scrapeConfigsCombined {
		sc := scrapeConf.DeepCopy()
		res[scrapeConf.Namespace+"/"+scrapeConf.Name] = sc
	}
	scrapeConfigs := make([]string, 0)
	for key := range res {
		scrapeConfigs = append(scrapeConfigs, key)
	}

	logger.WithContext(ctx).Info("selected scrapeConfigs", "scrapeConfigs", strings.Join(scrapeConfigs, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func selectPodScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (map[string]*vmv1beta1.VMPodScrape, error) {
	res := make(map[string]*vmv1beta1.VMPodScrape)

	var podScrapesCombined []vmv1beta1.VMPodScrape

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.PodScrapeNamespaceSelector, cr.Spec.PodScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMPodScrapeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				podScrapesCombined = append(podScrapesCombined, item)
			}
		}); err != nil {
		return nil, err
	}

	for _, podScrape := range podScrapesCombined {
		pm := podScrape.DeepCopy()
		res[podScrape.Namespace+"/"+podScrape.Name] = pm
	}
	podScrapes := make([]string, 0, len(res))
	for key := range res {
		podScrapes = append(podScrapes, key)
	}

	logger.WithContext(ctx).Info("selected PodScrapes", "podscrapes", strings.Join(podScrapes, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func selectVMProbes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (map[string]*vmv1beta1.VMProbe, error) {
	res := make(map[string]*vmv1beta1.VMProbe)
	var probesCombined []vmv1beta1.VMProbe
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ProbeNamespaceSelector, cr.Spec.ProbeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMProbeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				probesCombined = append(probesCombined, item)
			}
		}); err != nil {
		return nil, err
	}

	for _, probe := range probesCombined {
		pm := probe.DeepCopy()
		res[probe.Namespace+"/"+probe.Name] = pm
	}
	probesList := make([]string, 0)
	for key := range res {
		probesList = append(probesList, key)
	}

	logger.WithContext(ctx).Info("selected VMProbes", "vmProbes", strings.Join(probesList, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func selectVMNodeScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (map[string]*vmv1beta1.VMNodeScrape, error) {
	l := logger.WithContext(ctx).WithValues("vmagent", cr.Name)
	if !config.IsClusterWideAccessAllowed() && cr.IsOwnsServiceAccount() {
		l.Info("cannot use VMNodeScrape at operator in single namespace mode with default permissions. Create ServiceAccount for VMAgent manually if needed. Skipping config generation for it")
		return nil, nil
	}

	res := make(map[string]*vmv1beta1.VMNodeScrape)

	var nodesCombined []vmv1beta1.VMNodeScrape

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient,
		cr.Spec.NodeScrapeNamespaceSelector, cr.Spec.NodeScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault, func(list *vmv1beta1.VMNodeScrapeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				nodesCombined = append(nodesCombined, item)
			}
		}); err != nil {
		return nil, err
	}

	for _, node := range nodesCombined {
		pm := node.DeepCopy()
		res[node.Namespace+"/"+node.Name] = pm
	}
	nodesList := make([]string, 0)
	for key := range res {
		nodesList = append(nodesList, key)
	}

	l.Info("selected VMNodeScrapes", "VMNodeScrapes", strings.Join(nodesList, ","))

	return res, nil
}

func selectStaticScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (map[string]*vmv1beta1.VMStaticScrape, error) {
	res := make(map[string]*vmv1beta1.VMStaticScrape)
	var staticScrapesCombined []vmv1beta1.VMStaticScrape

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.StaticScrapeNamespaceSelector, cr.Spec.StaticScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMStaticScrapeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				staticScrapesCombined = append(staticScrapesCombined, item)
			}
		}); err != nil {
		return nil, err
	}

	for _, staticScrape := range staticScrapesCombined {
		pm := staticScrape.DeepCopy()
		res[staticScrape.Namespace+"/"+staticScrape.Name] = pm
	}
	staticScrapes := make([]string, 0)
	for key := range res {
		staticScrapes = append(staticScrapes, key)
	}

	logger.WithContext(ctx).Info("selected StaticScrapes", "staticScrapes", strings.Join(staticScrapes, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func selectServiceScrapes(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (map[string]*vmv1beta1.VMServiceScrape, error) {
	res := make(map[string]*vmv1beta1.VMServiceScrape)

	var servScrapesCombined []vmv1beta1.VMServiceScrape

	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ServiceScrapeNamespaceSelector, cr.Spec.ServiceScrapeSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(list *vmv1beta1.VMServiceScrapeList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				servScrapesCombined = append(servScrapesCombined, item)
			}
		}); err != nil {
		return nil, err
	}

	for _, servScrape := range servScrapesCombined {
		m := servScrape.DeepCopy()
		res[servScrape.Namespace+"/"+servScrape.Name] = m
	}

	// filter out all service scrapes that access
	// the file system.
	// TODO this restriction applies only ServiceScrape, make it global to any other objects
	if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
		for namespaceAndName, sm := range res {
			for _, endpoint := range sm.Spec.Endpoints {
				if err := testForArbitraryFSAccess(endpoint); err != nil {
					delete(res, namespaceAndName)
					logger.WithContext(ctx).Info("skipping vmservicescrape",
						"error", err.Error(),
						"vmservicescrape", namespaceAndName,
						"namespace", cr.Namespace,
						"vmagent", cr.Name,
					)
				}
			}
		}
	}

	serviceScrapes := make([]string, 0, len(res))
	for k := range res {
		serviceScrapes = append(serviceScrapes, k)
	}
	logger.WithContext(ctx).Info("selected ServiceScrapes", "servicescrapes", strings.Join(serviceScrapes, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}
