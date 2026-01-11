package vmagent

import (
	"context"
	"fmt"
	"reflect"

	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

type parsedObjects struct {
	APIServerConfig          *vmv1beta1.APIServerConfig
	Namespace                string
	ExternalLabels           map[string]string
	MustUseNodeSelector      bool
	HasClusterWideAccess     bool
	IgnoreNamespaceSelectors bool
	serviceScrapes           *build.ChildObjects[*vmv1beta1.VMServiceScrape]
	podScrapes               *build.ChildObjects[*vmv1beta1.VMPodScrape]
	staticScrapes            *build.ChildObjects[*vmv1beta1.VMStaticScrape]
	nodeScrapes              *build.ChildObjects[*vmv1beta1.VMNodeScrape]
	probes                   *build.ChildObjects[*vmv1beta1.VMProbe]
	scrapeConfigs            *build.ChildObjects[*vmv1beta1.VMScrapeConfig]
}

func (pos *parsedObjects) updateMetrics(ctx context.Context) {
	pos.serviceScrapes.UpdateMetrics(ctx)
	pos.podScrapes.UpdateMetrics(ctx)
	pos.staticScrapes.UpdateMetrics(ctx)
	pos.nodeScrapes.UpdateMetrics(ctx)
	pos.probes.UpdateMetrics(ctx)
	pos.scrapeConfigs.UpdateMetrics(ctx)
}

func (pos *parsedObjects) init(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
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

func (pos *parsedObjects) selectScrapeConfigs(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
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

func (pos *parsedObjects) selectPodScrapes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
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

func (pos *parsedObjects) selectProbes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
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

func (pos *parsedObjects) selectNodeScrapes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
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

func (pos *parsedObjects) selectStaticScrapes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
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

func (pos *parsedObjects) selectServiceScrapes(ctx context.Context, rclient client.Client, sp *vmv1beta1.CommonScrapeParams) error {
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

func (pos *parsedObjects) validateObjects(sp *vmv1beta1.CommonScrapeParams) {
	pos.serviceScrapes.ForEachCollectSkipInvalid(func(sc *vmv1beta1.VMServiceScrape) error {
		if sp.ArbitraryFSAccessThroughSMs.Deny {
			for _, ep := range sc.Spec.Endpoints {
				if err := testForArbitraryFSAccess(ep.EndpointAuth); err != nil {
					return err
				}
			}
		}
		if err := validateScrapeClassExists(sc.Spec.ScrapeClassName, sp); err != nil {
			return err
		}
		if !build.MustSkipRuntimeValidation {
			return sc.Validate()
		}
		return nil
	})
	pos.podScrapes.ForEachCollectSkipInvalid(func(sc *vmv1beta1.VMPodScrape) error {
		if sp.ArbitraryFSAccessThroughSMs.Deny {
			for _, ep := range sc.Spec.PodMetricsEndpoints {
				if err := testForArbitraryFSAccess(ep.EndpointAuth); err != nil {
					return err
				}
			}
		}
		if err := validateScrapeClassExists(sc.Spec.ScrapeClassName, sp); err != nil {
			return err
		}
		if !build.MustSkipRuntimeValidation {
			return sc.Validate()
		}
		return nil
	})
	pos.staticScrapes.ForEachCollectSkipInvalid(func(sc *vmv1beta1.VMStaticScrape) error {
		if sp.ArbitraryFSAccessThroughSMs.Deny {
			for _, ep := range sc.Spec.TargetEndpoints {
				if err := testForArbitraryFSAccess(ep.EndpointAuth); err != nil {
					return err
				}
			}
		}
		if err := validateScrapeClassExists(sc.Spec.ScrapeClassName, sp); err != nil {
			return err
		}
		if !build.MustSkipRuntimeValidation {
			return sc.Validate()
		}
		return nil
	})
	pos.nodeScrapes.ForEachCollectSkipInvalid(func(sc *vmv1beta1.VMNodeScrape) error {
		if sp.ArbitraryFSAccessThroughSMs.Deny {
			if err := testForArbitraryFSAccess(sc.Spec.EndpointAuth); err != nil {
				return err
			}
		}
		if err := validateScrapeClassExists(sc.Spec.ScrapeClassName, sp); err != nil {
			return err
		}
		if !build.MustSkipRuntimeValidation {
			return sc.Validate()
		}
		return nil
	})
	pos.probes.ForEachCollectSkipInvalid(func(sc *vmv1beta1.VMProbe) error {
		if sp.ArbitraryFSAccessThroughSMs.Deny {
			if err := testForArbitraryFSAccess(sc.Spec.EndpointAuth); err != nil {
				return err
			}
		}
		if err := validateScrapeClassExists(sc.Spec.ScrapeClassName, sp); err != nil {
			return err
		}
		if !build.MustSkipRuntimeValidation {
			return sc.Validate()
		}
		return nil
	})
	pos.scrapeConfigs.ForEachCollectSkipInvalid(func(sc *vmv1beta1.VMScrapeConfig) error {
		// TODO: @f41gh7 validate per configuration FS access
		if sp.ArbitraryFSAccessThroughSMs.Deny {
			if err := testForArbitraryFSAccess(sc.Spec.EndpointAuth); err != nil {
				return err
			}
		}
		if err := validateScrapeClassExists(sc.Spec.ScrapeClassName, sp); err != nil {
			return err
		}
		if !build.MustSkipRuntimeValidation {
			return sc.Validate()
		}
		return nil
	})
}

// updateStatusesForScrapeObjects updates status of either selected childObject or all child objects
func (pos *parsedObjects) updateStatusesForScrapeObjects(ctx context.Context, rclient client.Client, parentName string, childObject client.Object) error {
	pos.updateMetrics(ctx)
	if childObject != nil && !reflect.ValueOf(childObject).IsNil() {
		// fast path
		switch t := childObject.(type) {
		case *vmv1beta1.VMStaticScrape:
			if o := pos.staticScrapes.Get(t); o != nil {
				return reconcile.StatusForChildObjects(ctx, rclient, parentName, []*vmv1beta1.VMStaticScrape{o})
			}
		case *vmv1beta1.VMProbe:
			if o := pos.probes.Get(t); o != nil {
				return reconcile.StatusForChildObjects(ctx, rclient, parentName, []*vmv1beta1.VMProbe{o})
			}
		case *vmv1beta1.VMScrapeConfig:
			if o := pos.scrapeConfigs.Get(t); o != nil {
				return reconcile.StatusForChildObjects(ctx, rclient, parentName, []*vmv1beta1.VMScrapeConfig{o})
			}
		case *vmv1beta1.VMNodeScrape:
			if o := pos.nodeScrapes.Get(t); o != nil {
				return reconcile.StatusForChildObjects(ctx, rclient, parentName, []*vmv1beta1.VMNodeScrape{o})
			}
		case *vmv1beta1.VMPodScrape:
			if o := pos.podScrapes.Get(t); o != nil {
				return reconcile.StatusForChildObjects(ctx, rclient, parentName, []*vmv1beta1.VMPodScrape{o})
			}
		case *vmv1beta1.VMServiceScrape:
			if o := pos.serviceScrapes.Get(t); o != nil {
				return reconcile.StatusForChildObjects(ctx, rclient, parentName, []*vmv1beta1.VMServiceScrape{o})
			}
		}
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentName, pos.serviceScrapes.All()); err != nil {
		return fmt.Errorf("cannot update statuses for service scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentName, pos.podScrapes.All()); err != nil {
		return fmt.Errorf("cannot update statuses for pod scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentName, pos.nodeScrapes.All()); err != nil {
		return fmt.Errorf("cannot update statuses for node scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentName, pos.probes.All()); err != nil {
		return fmt.Errorf("cannot update statuses for probe scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentName, pos.staticScrapes.All()); err != nil {
		return fmt.Errorf("cannot update statuses for static scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentName, pos.scrapeConfigs.All()); err != nil {
		return fmt.Errorf("cannot update statuses for scrapeconfig scrape objects: %w", err)
	}
	return nil
}

// generateConfig generates yaml scrape configuration from collected scrape objects
func (pos *parsedObjects) generateConfig(ctx context.Context, sp *vmv1beta1.CommonScrapeParams, ac *build.AssetsCache) ([]byte, error) {
	var additionalScrapeConfigs []byte
	if sp.AdditionalScrapeConfigs != nil {
		sc, err := ac.LoadKeyFromSecret(pos.Namespace, sp.AdditionalScrapeConfigs)
		if err != nil {
			return nil, fmt.Errorf("loading additional scrape configs from Secret failed: %w", err)
		}
		additionalScrapeConfigs = []byte(sc)
	}
	cfg := yaml.MapSlice{}
	scrapeInterval := defaultScrapeInterval
	if sp.ScrapeInterval != "" {
		scrapeInterval = sp.ScrapeInterval
	}
	globalItems := yaml.MapSlice{
		{Key: "scrape_interval", Value: scrapeInterval},
		{Key: "external_labels", Value: stringMapToMapSlice(pos.ExternalLabels)},
	}

	if sp.SampleLimit > 0 {
		globalItems = append(globalItems, yaml.MapItem{
			Key:   "sample_limit",
			Value: sp.SampleLimit,
		})
	}

	if sp.ScrapeTimeout != "" {
		globalItems = append(globalItems, yaml.MapItem{
			Key:   "scrape_timeout",
			Value: sp.ScrapeTimeout,
		})
	}

	if len(sp.GlobalScrapeMetricRelabelConfigs) > 0 {
		globalItems = append(globalItems, yaml.MapItem{
			Key:   "metric_relabel_configs",
			Value: sp.GlobalScrapeMetricRelabelConfigs,
		})
	}
	if len(sp.GlobalScrapeRelabelConfigs) > 0 {
		globalItems = append(globalItems, yaml.MapItem{
			Key:   "relabel_configs",
			Value: sp.GlobalScrapeRelabelConfigs,
		})
	}

	cfg = append(cfg, yaml.MapItem{Key: "global", Value: globalItems})

	var scrapeConfigs []yaml.MapSlice
	var err error

	err = pos.serviceScrapes.ForEachCollectSkipNotFound(func(sc *vmv1beta1.VMServiceScrape) error {
		scrapeConfigsLen := len(scrapeConfigs)
		for i, ep := range sc.Spec.Endpoints {
			s, err := generateServiceScrapeConfig(
				ctx,
				sp,
				pos,
				sc,
				ep, i,
				ac,
			)
			if err != nil {
				scrapeConfigs = scrapeConfigs[:scrapeConfigsLen]
				return err
			}
			scrapeConfigs = append(scrapeConfigs, s)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = pos.podScrapes.ForEachCollectSkipNotFound(func(sc *vmv1beta1.VMPodScrape) error {
		scrapeConfigsLen := len(scrapeConfigs)
		for i, ep := range sc.Spec.PodMetricsEndpoints {
			s, err := generatePodScrapeConfig(
				ctx,
				sp,
				pos,
				sc, ep, i,
				ac,
			)
			if err != nil {
				scrapeConfigs = scrapeConfigs[:scrapeConfigsLen]
				return err
			}
			scrapeConfigs = append(scrapeConfigs, s)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = pos.probes.ForEachCollectSkipNotFound(func(sc *vmv1beta1.VMProbe) error {
		s, err := generateProbeConfig(
			ctx,
			sp,
			pos,
			sc,
			ac,
		)
		if err != nil {
			return err
		}
		scrapeConfigs = append(scrapeConfigs, s)
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = pos.nodeScrapes.ForEachCollectSkipNotFound(func(sc *vmv1beta1.VMNodeScrape) error {
		s, err := generateNodeScrapeConfig(
			ctx,
			sp,
			pos,
			sc,
			ac,
		)
		if err != nil {
			return err
		}
		scrapeConfigs = append(scrapeConfigs, s)

		return nil
	})
	if err != nil {
		return nil, err
	}

	err = pos.staticScrapes.ForEachCollectSkipNotFound(func(sc *vmv1beta1.VMStaticScrape) error {
		scrapeConfigsLen := len(scrapeConfigs)
		for i, ep := range sc.Spec.TargetEndpoints {
			s, err := generateStaticScrapeConfig(
				ctx,
				sp,
				sc,
				ep, i,
				ac,
			)
			if err != nil {
				scrapeConfigs = scrapeConfigs[:scrapeConfigsLen]
				return err
			}
			scrapeConfigs = append(scrapeConfigs, s)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = pos.scrapeConfigs.ForEachCollectSkipNotFound(func(sc *vmv1beta1.VMScrapeConfig) error {
		s, err := generateScrapeConfig(
			ctx,
			sp,
			sc,
			ac,
		)
		if err != nil {
			return err
		}
		scrapeConfigs = append(scrapeConfigs, s)

		return nil
	})
	if err != nil {
		return nil, err
	}

	var additionalScrapeConfigsYaml []yaml.MapSlice
	if err := yaml.Unmarshal(additionalScrapeConfigs, &additionalScrapeConfigsYaml); err != nil {
		return nil, fmt.Errorf("unmarshalling additional scrape configs failed: %w", err)
	}

	var inlineScrapeConfigsYaml []yaml.MapSlice
	if len(sp.InlineScrapeConfig) > 0 {
		if err := yaml.Unmarshal([]byte(sp.InlineScrapeConfig), &inlineScrapeConfigsYaml); err != nil {
			return nil, fmt.Errorf("unmarshalling inline additional scrape configs failed: %w", err)
		}
	}
	additionalScrapeConfigsYaml = append(additionalScrapeConfigsYaml, inlineScrapeConfigsYaml...)
	cfg = append(cfg, yaml.MapItem{
		Key:   "scrape_configs",
		Value: append(scrapeConfigs, additionalScrapeConfigsYaml...),
	})

	return yaml.Marshal(cfg)
}
