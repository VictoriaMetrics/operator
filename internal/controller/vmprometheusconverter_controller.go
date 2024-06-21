package controller

import (
	"context"
	"fmt"
	"time"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/converter"
	converterv1alpha1 "github.com/VictoriaMetrics/operator/internal/controller/converter/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/k8stools"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

const (
	// MetaMergeStrategyLabel merge strategy by default prefer prometheus meta labels
	// but with annotation value added to VMObject:
	// annotations:
	//   operator.victoriametrics.com/merge-api-strategy: prefer-victoriametrics
	// metadata from VMObject will be preferred during merge
	MetaMergeStrategyLabel = "operator.victoriametrics.com/merge-meta-strategy"
	// MetaPreferVM - prefers VM object meta values, ignores prometheus
	MetaPreferVM = "prefer-victoriametrics"
	// MetaPreferProm - prefers prometheus
	MetaPreferProm = "prefer-prometheus"
	// MetaMergeLabelsVMPriority merges both label sets
	// its not possible to remove values
	MetaMergeLabelsVMPriority = "merge-victoriametrics-priority"
	// MetaMergeLabelsPromPriority merges both label sets
	// its not possible to remove values
	MetaMergeLabelsPromPriority = "merge-prometheus-priority"

	// IgnoreConversionLabel this annotation disables updating of corresponding VMObject
	// must be added to annotation of VMObject
	// annotations:
	//  operator.victoriametrics.com/ignore-prometheus-updates: enabled
	IgnoreConversionLabel = "operator.victoriametrics.com/ignore-prometheus-updates"
	// IgnoreConversion - disables updates from prometheus api
	IgnoreConversion = "enabled"
)

// ConverterController - watches for prometheus objects
// and create VictoriaMetrics objects
type ConverterController struct {
	ctx             context.Context
	baseClient      *kubernetes.Clientset
	rclient         client.WithWatch
	ruleInf         cache.SharedInformer
	podInf          cache.SharedInformer
	serviceInf      cache.SharedInformer
	amConfigInf     cache.SharedInformer
	probeInf        cache.SharedIndexInformer
	scrapeConfigInf cache.SharedIndexInformer
	baseConf        *config.BaseOperatorConf
}

// NewConverterController builder for vmprometheusconverter service
func NewConverterController(ctx context.Context, baseClient *kubernetes.Clientset, rclient client.WithWatch, resyncPeriod time.Duration, baseConf *config.BaseOperatorConf) (*ConverterController, error) {
	c := &ConverterController{
		ctx:        ctx,
		baseClient: baseClient,
		rclient:    rclient,
		baseConf:   baseConf,
	}

	c.ruleInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects promv1.PrometheusRuleList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *promv1.PrometheusRuleList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list prometheus_rules: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[promv1.PrometheusRuleList](ctx, rclient, "prometheus_rules", config.MustGetWatchNamespaces())
			},
		},
		&promv1.PrometheusRule{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	if _, err := c.ruleInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreatePrometheusRule,
		UpdateFunc: c.UpdatePrometheusRule,
	}); err != nil {
		return nil, fmt.Errorf("cannot add prometheus_rule handler: %w", err)
	}
	c.podInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects promv1.PodMonitorList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *promv1.PodMonitorList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list pod_monitors: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[promv1.PodMonitorList](ctx, rclient, "pod_monitors", config.MustGetWatchNamespaces())
			},
		},
		&promv1.PodMonitor{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	if _, err := c.podInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreatePodMonitor,
		UpdateFunc: c.UpdatePodMonitor,
	}); err != nil {
		return nil, fmt.Errorf("cannot add pod_monitor handler: %w", err)
	}
	c.serviceInf = cache.NewSharedIndexInformer(

		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects promv1.ServiceMonitorList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *promv1.ServiceMonitorList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list service_monitors: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[promv1.ServiceMonitorList](ctx, rclient, "service_monitors", config.MustGetWatchNamespaces())
			},
		},
		&promv1.ServiceMonitor{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	if _, err := c.serviceInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreateServiceMonitor,
		UpdateFunc: c.UpdateServiceMonitor,
	}); err != nil {
		return nil, fmt.Errorf("cannot add service_monitor handler: %w", err)
	}

	amConfigInf := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects promv1alpha1.AlertmanagerConfigList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *promv1alpha1.AlertmanagerConfigList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list alertmanager_configs: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[promv1alpha1.AlertmanagerConfigList](ctx, rclient, "alertmanager_configs", config.MustGetWatchNamespaces())
			},
		},
		&promv1alpha1.AlertmanagerConfig{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	if _, err := amConfigInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreateAlertmanagerConfig,
		UpdateFunc: c.UpdateAlertmanagerConfig,
	}); err != nil {
		return nil, fmt.Errorf("cannot add alertmanager_config handler: %w", err)
	}
	c.amConfigInf = amConfigInf

	c.probeInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects promv1.ProbeList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *promv1.ProbeList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list probes: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[promv1.ProbeList](ctx, rclient, "probes", config.MustGetWatchNamespaces())
			},
		},
		&promv1.Probe{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	if _, err := c.probeInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreateProbe,
		UpdateFunc: c.UpdateProbe,
	}); err != nil {
		return nil, fmt.Errorf("cannot add probe handler: %w", err)
	}
	c.scrapeConfigInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects promv1alpha1.ScrapeConfigList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *promv1alpha1.ScrapeConfigList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list scrapeConfig: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[promv1alpha1.ScrapeConfigList](ctx, rclient, "scrape_configs", config.MustGetWatchNamespaces())
			},
		},
		&promv1alpha1.ScrapeConfig{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	if _, err := c.scrapeConfigInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreateScrapeConfig,
		UpdateFunc: c.UpdateScrapeConfig,
	}); err != nil {
		return nil, fmt.Errorf("cannot add scrapeConfig handler: %w", err)
	}
	return c, nil
}

func waitForAPIResource(ctx context.Context, client discovery.DiscoveryInterface, apiGroupVersion, kind string) error {
	l := log.WithValues("group", apiGroupVersion, "kind", kind)
	l.Info("waiting for api resource")
	tick := time.NewTicker(time.Second * 5)
	for {
		select {
		case <-tick.C:
			api, err := client.ServerResourcesForGroupVersion(apiGroupVersion)
			if err != nil {
				if !errors.IsNotFound(err) {
					l.Error(err, "cannot get server resource for api group version")
				}
				continue
			}
			for _, r := range api.APIResources {
				if r.Kind != kind {
					continue
				}
				l.Info("api resource is ready")
				return nil
			}
		case <-ctx.Done():
			l.Info("context was canceled")
			return nil
		}
	}
}

func (c *ConverterController) runInformerWithDiscovery(ctx context.Context, group, kind string, runInformer func(<-chan struct{})) error {
	err := waitForAPIResource(ctx, c.baseClient, group, kind)
	if err != nil {
		return fmt.Errorf("error wait for %s, err: %w", kind, err)
	}
	runInformer(ctx.Done())
	return nil
}

// Start implements interface.
func (c *ConverterController) Start(ctx context.Context) error {
	var errG errgroup.Group
	log.Info("starting prometheus converter")
	c.Run(ctx, &errG)
	go func() {
		log.Info("waiting for prometheus converter to stop")
		err := errG.Wait()
		if err != nil {
			log.Error(err, "error occured at prometheus converter")
		}
	}()
	return nil
}

// Run - starts vmprometheusconverter with background discovery process for each prometheus api object
func (c *ConverterController) Run(ctx context.Context, group *errgroup.Group) {
	if c.baseConf.EnabledPrometheusConverter.ServiceScrape {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, promv1.SchemeGroupVersion.String(), promv1.ServiceMonitorsKind, c.serviceInf.Run)
		})
	}
	if c.baseConf.EnabledPrometheusConverter.PodMonitor {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, promv1.SchemeGroupVersion.String(), promv1.PodMonitorsKind, c.podInf.Run)
		})
	}
	if c.baseConf.EnabledPrometheusConverter.PrometheusRule {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, promv1.SchemeGroupVersion.String(), promv1.PrometheusRuleKind, c.ruleInf.Run)
		})
	}
	if c.baseConf.EnabledPrometheusConverter.Probe {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, promv1.SchemeGroupVersion.String(), promv1.ProbesKind, c.probeInf.Run)
		})
	}

	if c.baseConf.EnabledPrometheusConverter.AlertmanagerConfig {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, promv1alpha1.SchemeGroupVersion.String(), promv1alpha1.AlertmanagerConfigKind, c.amConfigInf.Run)
		})
	}
	if c.baseConf.EnabledPrometheusConverter.ScrapeConfig {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, promv1alpha1.SchemeGroupVersion.String(), promv1alpha1.ScrapeConfigsKind, c.scrapeConfigInf.Run)
		})
	}
}

// CreatePrometheusRule converts prometheus rule to vmrule
func (c *ConverterController) CreatePrometheusRule(rule interface{}) {
	promRule := rule.(*promv1.PrometheusRule)
	l := log.WithValues("kind", "alertRule", "name", promRule.Name, "ns", promRule.Namespace)
	cr := converter.ConvertPromRule(promRule, c.baseConf)

	err := c.rclient.Create(context.Background(), cr)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdatePrometheusRule(nil, promRule)
			return
		}
		l.Error(err, "cannot create AlertRule from Prometheusrule")
		return
	}
}

// UpdatePrometheusRule updates vmrule
func (c *ConverterController) UpdatePrometheusRule(_old, new interface{}) {
	promRuleNew := new.(*promv1.PrometheusRule)
	l := log.WithValues("kind", "VMRule", "name", promRuleNew.Name, "ns", promRuleNew.Namespace)
	VMRule := converter.ConvertPromRule(promRuleNew, c.baseConf)
	ctx := context.Background()
	existingVMRule := &vmv1beta1.VMRule{}
	err := c.rclient.Get(ctx, types.NamespacedName{Name: VMRule.Name, Namespace: VMRule.Namespace}, existingVMRule)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, VMRule); err == nil {
				return
			}
		}
		l.Error(err, "cannot get existing VMRule")
		return
	}
	if existingVMRule.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}
	existingVMRule.Spec = VMRule.Spec
	metaMergeStrategy := getMetaMergeStrategy(existingVMRule.Annotations)
	existingVMRule.Annotations = mergeLabelsWithStrategy(existingVMRule.Annotations, VMRule.Annotations, metaMergeStrategy)
	existingVMRule.Labels = mergeLabelsWithStrategy(existingVMRule.Labels, VMRule.Labels, metaMergeStrategy)
	existingVMRule.OwnerReferences = VMRule.OwnerReferences

	err = c.rclient.Update(ctx, existingVMRule)
	if err != nil {
		l.Error(err, "cannot update VMRule")
		return
	}
}

// CreateServiceMonitor converts ServiceMonitor to VMServiceScrape
func (c *ConverterController) CreateServiceMonitor(service interface{}) {
	serviceMon := service.(*promv1.ServiceMonitor)
	l := log.WithValues("kind", "vmServiceScrape", "name", serviceMon.Name, "ns", serviceMon.Namespace)
	vmServiceScrape := converter.ConvertServiceMonitor(serviceMon, c.baseConf)
	err := c.rclient.Create(context.Background(), vmServiceScrape)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateServiceMonitor(nil, serviceMon)
			return
		}
		l.Error(err, "cannot create vmServiceScrape")
		return
	}
}

// UpdateServiceMonitor updates VMServiceMonitor
func (c *ConverterController) UpdateServiceMonitor(_, new interface{}) {
	serviceMonNew := new.(*promv1.ServiceMonitor)
	l := log.WithValues("kind", "vmServiceScrape", "name", serviceMonNew.Name, "ns", serviceMonNew.Namespace)
	vmServiceScrape := converter.ConvertServiceMonitor(serviceMonNew, c.baseConf)
	existingVMServiceScrape := &vmv1beta1.VMServiceScrape{}
	ctx := context.Background()
	err := c.rclient.Get(ctx, types.NamespacedName{Name: vmServiceScrape.Name, Namespace: vmServiceScrape.Namespace}, existingVMServiceScrape)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, vmServiceScrape); err == nil {
				return
			}
		}
		l.Error(err, "cannot get existing vmServiceScrape")
		return
	}

	if existingVMServiceScrape.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}
	existingVMServiceScrape.Spec = vmServiceScrape.Spec

	metaMergeStrategy := getMetaMergeStrategy(existingVMServiceScrape.Annotations)
	existingVMServiceScrape.Annotations = mergeLabelsWithStrategy(existingVMServiceScrape.Annotations, vmServiceScrape.Annotations, metaMergeStrategy)
	existingVMServiceScrape.Labels = mergeLabelsWithStrategy(existingVMServiceScrape.Labels, vmServiceScrape.Labels, metaMergeStrategy)
	existingVMServiceScrape.OwnerReferences = vmServiceScrape.OwnerReferences

	err = c.rclient.Update(ctx, existingVMServiceScrape)
	if err != nil {
		l.Error(err, "cannot update")
		return
	}
}

// CreatePodMonitor converts PodMonitor to VMPodScrape
func (c *ConverterController) CreatePodMonitor(pod interface{}) {
	podMonitor := pod.(*promv1.PodMonitor)
	l := log.WithValues("kind", "podScrape", "name", podMonitor.Name, "ns", podMonitor.Namespace)
	podScrape := converter.ConvertPodMonitor(podMonitor, c.baseConf)
	err := c.rclient.Create(c.ctx, podScrape)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdatePodMonitor(nil, podMonitor)
			return
		}
		l.Error(err, "cannot create podScrape")
		return
	}
}

// UpdatePodMonitor updates VMPodScrape
func (c *ConverterController) UpdatePodMonitor(_, new interface{}) {
	podMonitorNew := new.(*promv1.PodMonitor)
	l := log.WithValues("kind", "podScrape", "name", podMonitorNew.Name, "ns", podMonitorNew.Namespace)
	podScrape := converter.ConvertPodMonitor(podMonitorNew, c.baseConf)
	ctx := context.Background()
	existingVMPodScrape := &vmv1beta1.VMPodScrape{}
	err := c.rclient.Get(ctx, types.NamespacedName{Name: podScrape.Name, Namespace: podScrape.Namespace}, existingVMPodScrape)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, podScrape); err == nil {
				return
			}
		}
		l.Error(err, "cannot get existing podMonitor")
		return
	}
	if existingVMPodScrape.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}

	existingVMPodScrape.Spec = podScrape.Spec
	mergeStrategy := getMetaMergeStrategy(existingVMPodScrape.Annotations)
	existingVMPodScrape.Annotations = mergeLabelsWithStrategy(existingVMPodScrape.Annotations, podScrape.Annotations, mergeStrategy)
	existingVMPodScrape.Labels = mergeLabelsWithStrategy(existingVMPodScrape.Labels, podScrape.Labels, mergeStrategy)
	existingVMPodScrape.OwnerReferences = podScrape.OwnerReferences

	err = c.rclient.Update(ctx, existingVMPodScrape)
	if err != nil {
		l.Error(err, "cannot update podScrape")
		return
	}
}

// CreateAlertmanagerConfig converts AlertmanagerConfig to VMAlertmanagerConfig
func (c *ConverterController) CreateAlertmanagerConfig(amc interface{}) {
	var vmAMc *vmv1beta1.VMAlertmanagerConfig
	var err error
	switch promAMc := amc.(type) {
	case *promv1alpha1.AlertmanagerConfig:
		vmAMc, err = converterv1alpha1.ConvertAlertmanagerConfig(promAMc, c.baseConf)
	default:
		err = fmt.Errorf("scrape config of type %t is not supported", promAMc)
	}
	if err != nil {
		log.Error(err, "cannot convert alertmanager config")
		return
	}
	l := log.WithValues("kind", "vmAlertmanagerConfig", "name", vmAMc.Name, "ns", vmAMc.Namespace)
	if err := c.rclient.Create(context.Background(), vmAMc); err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateAlertmanagerConfig(nil, vmAMc)
			return
		}
		l.Error(err, "cannot create vmServiceScrape")
		return
	}
}

// UpdateAlertmanagerConfig updates VMAlertmanagerConfig
func (c *ConverterController) UpdateAlertmanagerConfig(_, new interface{}) {
	var vmAMc *vmv1beta1.VMAlertmanagerConfig
	var err error
	switch promAMc := new.(type) {
	case *promv1alpha1.AlertmanagerConfig:
		vmAMc, err = converterv1alpha1.ConvertAlertmanagerConfig(promAMc, c.baseConf)
	default:
		err = fmt.Errorf("alertmanager config of type %t is not supported", new)
	}
	if err != nil {
		log.Error(err, "cannot convert alertmanager config at update")
		return
	}
	l := log.WithValues("kind", "vmAlertmanagerConfig", "name", vmAMc.Name, "ns", vmAMc.Namespace)
	existAlertmanagerConfig := &vmv1beta1.VMAlertmanagerConfig{}
	ctx := context.Background()
	if err := c.rclient.Get(ctx, types.NamespacedName{Name: vmAMc.Name, Namespace: vmAMc.Namespace}, existAlertmanagerConfig); err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, vmAMc); err == nil {
				return
			}
		}
		l.Error(err, "cannot get existing vmalertmanagerconfig")
		return
	}

	if existAlertmanagerConfig.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}
	existAlertmanagerConfig.Spec = vmAMc.Spec

	metaMergeStrategy := getMetaMergeStrategy(existAlertmanagerConfig.Annotations)
	existAlertmanagerConfig.Annotations = mergeLabelsWithStrategy(existAlertmanagerConfig.Annotations, vmAMc.Annotations, metaMergeStrategy)
	existAlertmanagerConfig.Labels = mergeLabelsWithStrategy(existAlertmanagerConfig.Labels, vmAMc.Labels, metaMergeStrategy)
	existAlertmanagerConfig.OwnerReferences = vmAMc.OwnerReferences

	err = c.rclient.Update(ctx, existAlertmanagerConfig)
	if err != nil {
		l.Error(err, "cannot update exist alertmanager config")
		return
	}
}

// default merge strategy - prefer-prometheus
// old - from vm
// new - from prometheus
// by default new has priority
func mergeLabelsWithStrategy(old, new map[string]string, mergeStrategy string) map[string]string {
	switch mergeStrategy {
	case MetaPreferVM:
		return old
	case MetaPreferProm:
		return new
	case MetaMergeLabelsVMPriority:
		old, new = new, old
	case MetaMergeLabelsPromPriority:
		break
	}
	merged := make(map[string]string)
	for k, v := range old {
		merged[k] = v
	}
	for k, v := range new {
		merged[k] = v
	}
	return merged
}

// helper function - extracts meta merge strategy
// in the future we can introduce another merge strategies
func getMetaMergeStrategy(vmMeta map[string]string) string {
	switch vmMeta[MetaMergeStrategyLabel] {
	case MetaPreferVM:
		return MetaPreferVM
	case MetaMergeLabelsPromPriority:
		return MetaMergeLabelsPromPriority
	case MetaMergeLabelsVMPriority:
		return MetaMergeLabelsVMPriority
	}
	return MetaPreferProm
}

// CreateProbe converts Probe to VMProbe
func (c *ConverterController) CreateProbe(obj interface{}) {
	probe := obj.(*promv1.Probe)
	l := log.WithValues("kind", "vmProbe", "name", probe.Name, "ns", probe.Namespace)
	vmProbe := converter.ConvertProbe(probe, c.baseConf)
	err := c.rclient.Create(c.ctx, vmProbe)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateProbe(nil, probe)
			return
		}
		l.Error(err, "cannot create vmProbe")
		return
	}
}

// UpdateProbe updates VMProbe
func (c *ConverterController) UpdateProbe(_, new interface{}) {
	probeNew := new.(*promv1.Probe)
	l := log.WithValues("kind", "vmProbe", "name", probeNew.Name, "ns", probeNew.Namespace)
	vmProbe := converter.ConvertProbe(probeNew, c.baseConf)
	ctx := context.Background()
	existingVMProbe := &vmv1beta1.VMProbe{}
	err := c.rclient.Get(ctx, types.NamespacedName{Name: vmProbe.Name, Namespace: vmProbe.Namespace}, existingVMProbe)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, vmProbe); err == nil {
				return
			}
		}
		l.Error(err, "cannot get existing vmProbe")
		return
	}
	if existingVMProbe.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}

	mergeStrategy := getMetaMergeStrategy(existingVMProbe.Annotations)
	existingVMProbe.Annotations = mergeLabelsWithStrategy(existingVMProbe.Annotations, probeNew.Annotations, mergeStrategy)
	existingVMProbe.Labels = mergeLabelsWithStrategy(existingVMProbe.Labels, probeNew.Labels, mergeStrategy)
	existingVMProbe.OwnerReferences = vmProbe.OwnerReferences

	existingVMProbe.Spec = vmProbe.Spec
	err = c.rclient.Update(ctx, existingVMProbe)
	if err != nil {
		l.Error(err, "cannot update vmProbe")
		return
	}
}

// CreateScrapeConfig converts ServiceMonitor to VMScrapeConfig
func (c *ConverterController) CreateScrapeConfig(scrapeConfig interface{}) {
	var vmScrapeConfig *vmv1beta1.VMScrapeConfig
	var err error
	switch promScrapeConfig := scrapeConfig.(type) {
	case *promv1alpha1.ScrapeConfig:
		vmScrapeConfig = converterv1alpha1.ConvertScrapeConfig(promScrapeConfig, c.baseConf)
	default:
		err = fmt.Errorf("scrape config of type %t is not supported", promScrapeConfig)
		log.Error(err, "cannot parse vmScrapeConfig")
		return
	}
	l := log.WithValues("kind", "vmScrapeConfig", "name", vmScrapeConfig.Name, "ns", vmScrapeConfig.Namespace)
	err = c.rclient.Create(context.Background(), vmScrapeConfig)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateScrapeConfig(nil, vmScrapeConfig)
			return
		}
		l.Error(err, "cannot create vmScrapeConfig")
		return
	}
}

// UpdateScrapeConfig updates VMScrapeConfig
func (c *ConverterController) UpdateScrapeConfig(_, new interface{}) {
	var vmScrapeConfig *vmv1beta1.VMScrapeConfig
	var err error
	switch promScrapeConfig := new.(type) {
	case *promv1alpha1.ScrapeConfig:
		vmScrapeConfig = converterv1alpha1.ConvertScrapeConfig(promScrapeConfig, c.baseConf)
	default:
		err = fmt.Errorf("scrape config of type %t is not supported", promScrapeConfig)
		log.Error(err, "cannot parse vmScrapeConfig")
		return
	}
	l := log.WithValues("kind", "vmScrapeConfig", "name", vmScrapeConfig.Name, "ns", vmScrapeConfig.Namespace)
	existingVMScrapeConfig := &vmv1beta1.VMScrapeConfig{}
	ctx := context.Background()
	err = c.rclient.Get(ctx, types.NamespacedName{Name: vmScrapeConfig.Name, Namespace: vmScrapeConfig.Namespace}, existingVMScrapeConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, vmScrapeConfig); err == nil {
				return
			}
		}
		l.Error(err, "cannot get existing vmScrapeConfig")
		return
	}

	if existingVMScrapeConfig.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}
	existingVMScrapeConfig.Spec = vmScrapeConfig.Spec

	metaMergeStrategy := getMetaMergeStrategy(existingVMScrapeConfig.Annotations)
	existingVMScrapeConfig.Annotations = mergeLabelsWithStrategy(existingVMScrapeConfig.Annotations, vmScrapeConfig.Annotations, metaMergeStrategy)
	existingVMScrapeConfig.Labels = mergeLabelsWithStrategy(existingVMScrapeConfig.Labels, vmScrapeConfig.Labels, metaMergeStrategy)
	existingVMScrapeConfig.OwnerReferences = vmScrapeConfig.OwnerReferences

	err = c.rclient.Update(ctx, existingVMScrapeConfig)
	if err != nil {
		l.Error(err, "cannot update vmScrapeConfig")
		return
	}
}
