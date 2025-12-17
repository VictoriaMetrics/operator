package vmscrapes

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

type ParsedObjects struct {
	APIServerConfig      *vmv1beta1.APIServerConfig
	Namespace            string
	ExternalLabels       map[string]string
	MustUseNodeSelector  bool
	HasClusterWideAccess bool
	serviceScrapes       *build.ChildObjects[*vmv1beta1.VMServiceScrape]
	podScrapes           *build.ChildObjects[*vmv1beta1.VMPodScrape]
	staticScrapes        *build.ChildObjects[*vmv1beta1.VMStaticScrape]
	nodeScrapes          *build.ChildObjects[*vmv1beta1.VMNodeScrape]
	probes               *build.ChildObjects[*vmv1beta1.VMProbe]
	scrapeConfigs        *build.ChildObjects[*vmv1beta1.VMScrapeConfig]
}

func (pos *ParsedObjects) updateMetrics(ctx context.Context) {
	pos.serviceScrapes.UpdateMetrics(ctx)
	pos.podScrapes.UpdateMetrics(ctx)
	pos.staticScrapes.UpdateMetrics(ctx)
	pos.nodeScrapes.UpdateMetrics(ctx)
	pos.probes.UpdateMetrics(ctx)
	pos.scrapeConfigs.UpdateMetrics(ctx)
}

func (pos *ParsedObjects) Init(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
	if err := pos.selectPodScrapes(ctx, rclient, sp); err != nil {
		return fmt.Errorf("selecting PodScrapes failed: %w", err)
	}
	if err := pos.selectServiceScrapes(ctx, rclient, sp); err != nil {
		return fmt.Errorf("selecting ServiceScrapes failed: %w", err)
	}
	if err := pos.selectProbes(ctx, rclient, sp); err != nil {
		return fmt.Errorf("selecting VMProbes failed: %w", err)
	}
	if err := pos.selectNodeScrapes(ctx, rclient, sp); err != nil {
		return fmt.Errorf("selecting VMNodeScrapes failed: %w", err)
	}
	if err := pos.selectStaticScrapes(ctx, rclient, sp); err != nil {
		return fmt.Errorf("selecting VMStaticScrapes failed: %w", err)
	}
	if err := pos.selectScrapeConfigs(ctx, rclient, sp); err != nil {
		return fmt.Errorf("selecting ScrapeConfigs failed: %w", err)
	}
	return nil
}

func (pos *ParsedObjects) selectScrapeConfigs(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
	var selectedConfigs []*vmv1beta1.VMScrapeConfig
	var nsn []string
	if !pos.MustUseNodeSelector {
		opts := &k8stools.SelectorOpts{
			SelectAll:         sp.SelectAllByDefault,
			NamespaceSelector: sp.ScrapeConfigNamespaceSelector,
			ObjectSelector:    sp.ScrapeConfigSelector,
			DefaultNamespace:  pos.Namespace,
		}
		if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMScrapeConfigList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				selectedConfigs = append(selectedConfigs, item)
				nsn = append(nsn, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
			return err
		}
	}
	pos.scrapeConfigs = build.NewChildObjects("vmscrapeconfig", selectedConfigs, nsn)
	return nil
}

func (pos *ParsedObjects) selectPodScrapes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
	var selectedConfigs []*vmv1beta1.VMPodScrape
	var nsn []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         sp.SelectAllByDefault,
		NamespaceSelector: sp.PodScrapeNamespaceSelector,
		ObjectSelector:    sp.PodScrapeSelector,
		DefaultNamespace:  pos.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMPodScrapeList) {
		for i := range list.Items {
			item := &list.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			selectedConfigs = append(selectedConfigs, item)
			nsn = append(nsn, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return err
	}
	pos.podScrapes = build.NewChildObjects("vmpodscrape", selectedConfigs, nsn)
	return nil
}

func (pos *ParsedObjects) selectProbes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
	var selectedConfigs []*vmv1beta1.VMProbe
	var nsn []string
	if !pos.MustUseNodeSelector {
		opts := &k8stools.SelectorOpts{
			SelectAll:         sp.SelectAllByDefault,
			NamespaceSelector: sp.ProbeNamespaceSelector,
			ObjectSelector:    sp.ProbeSelector,
			DefaultNamespace:  pos.Namespace,
		}
		if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMProbeList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				selectedConfigs = append(selectedConfigs, item)
				nsn = append(nsn, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
			return err
		}
	}
	pos.probes = build.NewChildObjects("vmprobe", selectedConfigs, nsn)
	return nil
}

func (pos *ParsedObjects) selectNodeScrapes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
	var selectedConfigs []*vmv1beta1.VMNodeScrape
	var nsn []string
	if !pos.MustUseNodeSelector {
		if pos.HasClusterWideAccess {
			opts := &k8stools.SelectorOpts{
				SelectAll:         sp.SelectAllByDefault,
				NamespaceSelector: sp.NodeScrapeNamespaceSelector,
				ObjectSelector:    sp.NodeScrapeSelector,
				DefaultNamespace:  pos.Namespace,
			}
			if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMNodeScrapeList) {
				for i := range list.Items {
					item := &list.Items[i]
					if !item.DeletionTimestamp.IsZero() {
						continue
					}
					selectedConfigs = append(selectedConfigs, item)
					nsn = append(nsn, fmt.Sprintf("%s/%s", item.Namespace, item.Name))

				}
			}); err != nil {
				return err
			}
		} else {
			logger.WithContext(ctx).Info("cannot use VMNodeScrape at operator in single namespace mode with default permissions." +
				" Create ServiceAccount manually if needed. Skipping config generation for it")
		}
	}
	pos.nodeScrapes = build.NewChildObjects("vmnodescrape", selectedConfigs, nsn)
	return nil
}

func (pos *ParsedObjects) selectStaticScrapes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
	var selectedConfigs []*vmv1beta1.VMStaticScrape
	var nsn []string
	if !pos.MustUseNodeSelector {
		opts := &k8stools.SelectorOpts{
			SelectAll:         sp.SelectAllByDefault,
			NamespaceSelector: sp.StaticScrapeNamespaceSelector,
			ObjectSelector:    sp.StaticScrapeSelector,
			DefaultNamespace:  pos.Namespace,
		}
		if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMStaticScrapeList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				selectedConfigs = append(selectedConfigs, item)
				nsn = append(nsn, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
			return err
		}
	}
	pos.staticScrapes = build.NewChildObjects("vmstaticscrape", selectedConfigs, nsn)
	return nil
}

func (pos *ParsedObjects) selectServiceScrapes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
	var selectedConfigs []*vmv1beta1.VMServiceScrape
	var nsn []string
	if !pos.MustUseNodeSelector {
		opts := &k8stools.SelectorOpts{
			SelectAll:         sp.SelectAllByDefault,
			NamespaceSelector: sp.ServiceScrapeNamespaceSelector,
			ObjectSelector:    sp.ServiceScrapeSelector,
			DefaultNamespace:  pos.Namespace,
		}
		if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMServiceScrapeList) {
			for i := range list.Items {
				item := &list.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				rclient.Scheme().Default(item)
				nsn = append(nsn, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
				selectedConfigs = append(selectedConfigs, item)
			}
		}); err != nil {
			return err
		}
	}
	pos.serviceScrapes = build.NewChildObjects("vmservicescrape", selectedConfigs, nsn)
	return nil
}
