package operator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/converter"
	converterv1alpha1 "github.com/VictoriaMetrics/operator/internal/controller/operator/converter/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
)

var converterLogger = logf.Log.WithName("controller.PrometheusConverter")

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
	sd              *sharedAPIDiscoverer
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
		ctx:      ctx,
		rclient:  rclient,
		baseConf: baseConf,
		sd: &sharedAPIDiscoverer{
			baseClient:       baseClient,
			kindReadyByGroup: map[string]map[string]chan struct{}{},
		},
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

func (c *ConverterController) runInformerWithDiscovery(ctx context.Context, group, kind string, runInformer func(<-chan struct{})) error {
	c.sd.waitForAPIReady(ctx, group, kind)
	runInformer(ctx.Done())
	return nil
}

// Start implements interface.
func (c *ConverterController) Start(ctx context.Context) error {
	var errG errgroup.Group
	converterLogger.Info("starting prometheus converter")
	c.Run(ctx, &errG)
	go func() {
		converterLogger.Info("waiting for prometheus converter to stop")
		err := errG.Wait()
		if err != nil {
			converterLogger.Error(err, "error occurred at prometheus converter")
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
func (c *ConverterController) CreatePrometheusRule(rule any) {
	promRule := rule.(*promv1.PrometheusRule)
	l := converterLogger.WithValues("vmrule", promRule.Name, "namespace", promRule.Namespace)
	cr := converter.ConvertPromRule(promRule, c.baseConf)

	err := c.rclient.Create(context.Background(), cr)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdatePrometheusRule(nil, promRule)
			return
		}
		l.Error(err, "cannot create VMRule from PrometheusRule")
		return
	}
}

// UpdatePrometheusRule updates vmrule
func (c *ConverterController) UpdatePrometheusRule(_old, new any) {
	promRuleNew := new.(*promv1.PrometheusRule)
	l := converterLogger.WithValues("vmrule", promRuleNew.Name, "namespace", promRuleNew.Namespace)
	vmRule := converter.ConvertPromRule(promRuleNew, c.baseConf)
	ctx := context.Background()
	existingVMRule := &vmv1beta1.VMRule{}
	err := c.rclient.Get(ctx, types.NamespacedName{Name: vmRule.Name, Namespace: vmRule.Namespace}, existingVMRule)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, vmRule); err == nil {
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
	metaMergeStrategy := getMetaMergeStrategy(existingVMRule.Annotations)
	vmRule.Annotations = mergeLabelsWithStrategy(existingVMRule.Annotations, vmRule.Annotations, metaMergeStrategy)
	vmRule.Labels = mergeLabelsWithStrategy(existingVMRule.Labels, vmRule.Labels, metaMergeStrategy)

	if equality.Semantic.DeepEqual(vmRule.Spec, existingVMRule.Spec) &&
		isMetaEqual(vmRule, existingVMRule) {
		return
	}
	existingVMRule.Annotations = vmRule.Annotations
	existingVMRule.Labels = vmRule.Labels
	existingVMRule.OwnerReferences = vmRule.OwnerReferences
	existingVMRule.Spec = vmRule.Spec

	err = c.rclient.Update(ctx, existingVMRule)
	if err != nil {
		l.Error(err, "cannot update VMRule")
		return
	}
}

// CreateServiceMonitor converts ServiceMonitor to VMServiceScrape
func (c *ConverterController) CreateServiceMonitor(service any) {
	serviceMon := service.(*promv1.ServiceMonitor)

	l := converterLogger.WithValues("vmservicescrape", serviceMon.Name, "namespace", serviceMon.Namespace)
	vmServiceScrape := converter.ConvertServiceMonitor(serviceMon, c.baseConf)
	err := c.rclient.Create(context.Background(), vmServiceScrape)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateServiceMonitor(nil, serviceMon)
			return
		}
		l.Error(err, "cannot create VMServiceScrape")
		return
	}
}

// UpdateServiceMonitor updates VMServiceMonitor
func (c *ConverterController) UpdateServiceMonitor(_, new any) {
	serviceMonNew := new.(*promv1.ServiceMonitor)
	l := converterLogger.WithValues("vmservicescrape", serviceMonNew.Name, "namespace", serviceMonNew.Namespace)
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
		l.Error(err, "cannot get existing VMServiceScrape")
		return
	}

	if existingVMServiceScrape.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}

	metaMergeStrategy := getMetaMergeStrategy(existingVMServiceScrape.Annotations)
	vmServiceScrape.Annotations = mergeLabelsWithStrategy(existingVMServiceScrape.Annotations, vmServiceScrape.Annotations, metaMergeStrategy)
	vmServiceScrape.Labels = mergeLabelsWithStrategy(existingVMServiceScrape.Labels, vmServiceScrape.Labels, metaMergeStrategy)
	if equality.Semantic.DeepEqual(vmServiceScrape.Spec, existingVMServiceScrape.Spec) &&
		isMetaEqual(vmServiceScrape, existingVMServiceScrape) {
		return
	}
	existingVMServiceScrape.Annotations = vmServiceScrape.Annotations
	existingVMServiceScrape.Labels = vmServiceScrape.Labels
	existingVMServiceScrape.Spec = vmServiceScrape.Spec
	existingVMServiceScrape.OwnerReferences = vmServiceScrape.OwnerReferences

	err = c.rclient.Update(ctx, existingVMServiceScrape)
	if err != nil {
		l.Error(err, "cannot update VMServiceScrape")
		return
	}
}

// CreatePodMonitor converts PodMonitor to VMPodScrape
func (c *ConverterController) CreatePodMonitor(pod any) {
	podMonitor := pod.(*promv1.PodMonitor)
	l := converterLogger.WithValues("vmpodscrape", podMonitor.Name, "namespace", podMonitor.Namespace)
	podScrape := converter.ConvertPodMonitor(podMonitor, c.baseConf)
	err := c.rclient.Create(c.ctx, podScrape)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdatePodMonitor(nil, podMonitor)
			return
		}
		l.Error(err, "cannot create VMPodScrape")
		return
	}
}

// UpdatePodMonitor updates VMPodScrape
func (c *ConverterController) UpdatePodMonitor(_, new any) {
	podMonitorNew := new.(*promv1.PodMonitor)
	l := converterLogger.WithValues("vmpodscrape", podMonitorNew.Name, "namespace", podMonitorNew.Namespace)
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
		l.Error(err, "cannot get existing VMPodScrape")
		return
	}
	if existingVMPodScrape.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}

	mergeStrategy := getMetaMergeStrategy(existingVMPodScrape.Annotations)
	podScrape.Annotations = mergeLabelsWithStrategy(existingVMPodScrape.Annotations, podScrape.Annotations, mergeStrategy)
	podScrape.Labels = mergeLabelsWithStrategy(existingVMPodScrape.Labels, podScrape.Labels, mergeStrategy)
	if equality.Semantic.DeepEqual(podScrape.Spec, existingVMPodScrape.Spec) &&
		isMetaEqual(podScrape, existingVMPodScrape) {
		return
	}
	existingVMPodScrape.Annotations = podScrape.Annotations
	existingVMPodScrape.Labels = podScrape.Labels
	existingVMPodScrape.Spec = podScrape.Spec
	existingVMPodScrape.OwnerReferences = podScrape.OwnerReferences

	err = c.rclient.Update(ctx, existingVMPodScrape)
	if err != nil {
		l.Error(err, "cannot update VMPodScrape")
		return
	}
}

// CreateAlertmanagerConfig converts AlertmanagerConfig to VMAlertmanagerConfig
func (c *ConverterController) CreateAlertmanagerConfig(new any) {
	var vmAMc *vmv1beta1.VMAlertmanagerConfig
	var err error
	switch promAMc := new.(type) {
	case *promv1alpha1.AlertmanagerConfig:
		vmAMc, err = converterv1alpha1.ConvertAlertmanagerConfig(promAMc, c.baseConf)
	default:
		err = fmt.Errorf("BUG: scrape config of type %T is not supported", promAMc)
	}
	if err != nil {
		converterLogger.Error(err, "cannot convert alertmanager config")
		return
	}
	l := converterLogger.WithValues("vmalertmanagerconfig", vmAMc.Name, "namespace", vmAMc.Namespace)
	if err := c.rclient.Create(context.Background(), vmAMc); err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateAlertmanagerConfig(nil, new)
			return
		}
		l.Error(err, "cannot create VMAlertmanagerConfig")
		return
	}
}

// UpdateAlertmanagerConfig updates VMAlertmanagerConfig
func (c *ConverterController) UpdateAlertmanagerConfig(_, new any) {
	var vmAMc *vmv1beta1.VMAlertmanagerConfig
	var err error
	switch promAMc := new.(type) {
	case *promv1alpha1.AlertmanagerConfig:
		vmAMc, err = converterv1alpha1.ConvertAlertmanagerConfig(promAMc, c.baseConf)
	default:
		err = fmt.Errorf("BUG: alertmanager config of type %T is not supported", new)
	}
	if err != nil {
		converterLogger.Error(err, "cannot convert alertmanager config at update")
		return
	}
	l := converterLogger.WithValues("vmalertmanagerconfig", vmAMc.Name, "namespace", vmAMc.Namespace)
	existAlertmanagerConfig := &vmv1beta1.VMAlertmanagerConfig{}
	ctx := context.Background()
	if err := c.rclient.Get(ctx, types.NamespacedName{Name: vmAMc.Name, Namespace: vmAMc.Namespace}, existAlertmanagerConfig); err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, vmAMc); err == nil {
				return
			}
		}
		l.Error(err, "cannot get existing VMAlertmanagerConfig")
		return
	}

	if existAlertmanagerConfig.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}

	metaMergeStrategy := getMetaMergeStrategy(existAlertmanagerConfig.Annotations)
	vmAMc.Annotations = mergeLabelsWithStrategy(existAlertmanagerConfig.Annotations, vmAMc.Annotations, metaMergeStrategy)
	vmAMc.Labels = mergeLabelsWithStrategy(existAlertmanagerConfig.Labels, vmAMc.Labels, metaMergeStrategy)
	if equality.Semantic.DeepEqual(vmAMc.Spec, existAlertmanagerConfig.Spec) &&
		isMetaEqual(vmAMc, existAlertmanagerConfig) {
		return
	}

	existAlertmanagerConfig.Annotations = vmAMc.Annotations
	existAlertmanagerConfig.Labels = vmAMc.Labels
	existAlertmanagerConfig.OwnerReferences = vmAMc.OwnerReferences
	existAlertmanagerConfig.Spec = vmAMc.Spec

	err = c.rclient.Update(ctx, existAlertmanagerConfig)
	if err != nil {
		l.Error(err, "cannot update exist VMAlertmanagerConfig")
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
func (c *ConverterController) CreateProbe(obj any) {
	probe := obj.(*promv1.Probe)
	l := converterLogger.WithValues("vmprobe", probe.Name, "namespace", probe.Namespace)
	vmProbe := converter.ConvertProbe(probe, c.baseConf)
	err := c.rclient.Create(c.ctx, vmProbe)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateProbe(nil, probe)
			return
		}
		l.Error(err, "cannot create VMProbe")
		return
	}
}

// UpdateProbe updates VMProbe
func (c *ConverterController) UpdateProbe(_, new any) {
	probeNew := new.(*promv1.Probe)
	l := converterLogger.WithValues("vmprobe", probeNew.Name, "namespace", probeNew.Namespace)
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
		l.Error(err, "cannot get existing VMProbe")
		return
	}
	if existingVMProbe.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}

	mergeStrategy := getMetaMergeStrategy(existingVMProbe.Annotations)
	vmProbe.Annotations = mergeLabelsWithStrategy(existingVMProbe.Annotations, vmProbe.Annotations, mergeStrategy)
	vmProbe.Labels = mergeLabelsWithStrategy(existingVMProbe.Labels, vmProbe.Labels, mergeStrategy)
	if equality.Semantic.DeepEqual(vmProbe.Spec, existingVMProbe.Spec) &&
		isMetaEqual(vmProbe, existingVMProbe) {
		return
	}

	existingVMProbe.Labels = vmProbe.Labels
	existingVMProbe.Annotations = vmProbe.Annotations
	existingVMProbe.OwnerReferences = vmProbe.OwnerReferences
	existingVMProbe.Spec = vmProbe.Spec
	err = c.rclient.Update(ctx, existingVMProbe)
	if err != nil {
		l.Error(err, "cannot update VMProbe")
		return
	}
}

// CreateScrapeConfig converts ServiceMonitor to VMScrapeConfig
func (c *ConverterController) CreateScrapeConfig(scrapeConfig any) {
	var vmScrapeConfig *vmv1beta1.VMScrapeConfig
	var err error
	switch promScrapeConfig := scrapeConfig.(type) {
	case *promv1alpha1.ScrapeConfig:
		vmScrapeConfig = converterv1alpha1.ConvertScrapeConfig(promScrapeConfig, c.baseConf)
	default:
		err = fmt.Errorf("BUG: scrape config of type %T is not supported", promScrapeConfig)
		converterLogger.Error(err, "cannot parse promscrapeConfig for create")
		return
	}
	l := converterLogger.WithValues("vmscrapeconfig", vmScrapeConfig.Name, "namespace", vmScrapeConfig.Namespace)
	err = c.rclient.Create(context.Background(), vmScrapeConfig)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateScrapeConfig(nil, scrapeConfig)
			return
		}
		l.Error(err, "cannot create vmScrapeConfig")
		return
	}
}

// UpdateScrapeConfig updates VMScrapeConfig
func (c *ConverterController) UpdateScrapeConfig(_, newObj any) {
	var vmScrapeConfig *vmv1beta1.VMScrapeConfig
	var err error
	switch promScrapeConfig := newObj.(type) {
	case *promv1alpha1.ScrapeConfig:
		vmScrapeConfig = converterv1alpha1.ConvertScrapeConfig(promScrapeConfig, c.baseConf)
	default:
		err = fmt.Errorf("BUG: scrape config of type %T is not supported", promScrapeConfig)
		converterLogger.Error(err, "cannot parse promScrapeConfig for update")
		return
	}
	l := converterLogger.WithValues("vmscrapeconfig", vmScrapeConfig.Name, "namespace", vmScrapeConfig.Namespace)
	existingVMScrapeConfig := &vmv1beta1.VMScrapeConfig{}
	ctx := context.Background()
	err = c.rclient.Get(ctx, types.NamespacedName{Name: vmScrapeConfig.Name, Namespace: vmScrapeConfig.Namespace}, existingVMScrapeConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			if err = c.rclient.Create(ctx, vmScrapeConfig); err == nil {
				return
			}
		}
		l.Error(err, "cannot get existing VMScrapeConfig")
		return
	}

	if existingVMScrapeConfig.Annotations[IgnoreConversionLabel] == IgnoreConversion {
		l.Info("syncing for object was disabled by annotation", "annotation", IgnoreConversionLabel)
		return
	}
	metaMergeStrategy := getMetaMergeStrategy(existingVMScrapeConfig.Annotations)
	vmScrapeConfig.Annotations = mergeLabelsWithStrategy(existingVMScrapeConfig.Annotations, vmScrapeConfig.Annotations, metaMergeStrategy)
	vmScrapeConfig.Labels = mergeLabelsWithStrategy(existingVMScrapeConfig.Labels, vmScrapeConfig.Labels, metaMergeStrategy)

	if equality.Semantic.DeepEqual(vmScrapeConfig.Spec, existingVMScrapeConfig.Spec) &&
		isMetaEqual(vmScrapeConfig, existingVMScrapeConfig) {
		return
	}
	existingVMScrapeConfig.Labels = vmScrapeConfig.Labels
	existingVMScrapeConfig.Annotations = vmScrapeConfig.Annotations
	existingVMScrapeConfig.OwnerReferences = vmScrapeConfig.OwnerReferences
	existingVMScrapeConfig.Spec = vmScrapeConfig.Spec
	err = c.rclient.Update(ctx, existingVMScrapeConfig)
	if err != nil {
		l.Error(err, "cannot update VMScrapeConfig")
		return
	}
}

func isMetaEqual(left, right metav1.Object) bool {
	return equality.Semantic.DeepEqual(left.GetLabels(), right.GetLabels()) &&
		equality.Semantic.DeepEqual(left.GetAnnotations(), right.GetAnnotations()) &&
		equality.Semantic.DeepEqual(left.GetOwnerReferences(), right.GetOwnerReferences())
}

// sharedAPIDiscoverer must reduce GET API calls for kubernetes api server
// it will perform 1 single GET request per group
type sharedAPIDiscoverer struct {
	baseClient *kubernetes.Clientset

	mu               sync.Mutex
	kindReadyByGroup map[string]map[string]chan struct{}
}

func (s *sharedAPIDiscoverer) waitForAPIReady(ctx context.Context, group, kind string) {
	l := converterLogger.WithValues("discovery_group", group, "discovery_kind", kind)
	l.Info("waiting for api resource")
	ready := s.subscribeForGroupKind(ctx, group, kind)
	select {
	case <-ready:
		l.Info("object discovered")
	case <-ctx.Done():
	}
}

func (s *sharedAPIDiscoverer) subscribeForGroupKind(ctx context.Context, group, kind string) chan struct{} {
	dst := make(chan struct{})
	s.mu.Lock()
	gp, ok := s.kindReadyByGroup[group]
	if ok {
		if _, ok := gp[kind]; ok {
			panic(fmt.Sprintf("unexpected double subsribe for kind=%q,group=%q", kind, group))
		}
		gp[kind] = dst
	} else {
		gp = map[string]chan struct{}{
			kind: dst,
		}
		s.kindReadyByGroup[group] = gp
		go s.startPollFor(ctx, group)
	}
	s.mu.Unlock()
	return dst
}

func (s *sharedAPIDiscoverer) startPollFor(ctx context.Context, group string) {
	tick := time.NewTicker(time.Second * 5)
	for {
		select {
		case <-tick.C:
			api, err := s.baseClient.ServerResourcesForGroupVersion(group)
			if err != nil {
				if !errors.IsNotFound(err) {
					converterLogger.Error(err, "cannot get server resource for api group version")
				}
				continue
			}
			s.mu.Lock()
			for _, r := range api.APIResources {
				notify, ok := s.kindReadyByGroup[group][r.Kind]
				if ok {
					notify <- struct{}{}
					delete(s.kindReadyByGroup[group], r.Kind)
				}
			}
			l := len(s.kindReadyByGroup[group])
			s.mu.Unlock()
			if l == 0 {
				// no subscribers left
				// exit
				return
			}
		case <-ctx.Done():
			return
		}
	}

}
