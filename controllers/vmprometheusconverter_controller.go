package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/converter"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/config"
	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
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
	ctx         context.Context
	baseClient  *kubernetes.Clientset
	rclient     client.WithWatch
	ruleInf     cache.SharedInformer
	podInf      cache.SharedInformer
	serviceInf  cache.SharedInformer
	amConfigInf cache.SharedInformer
	probeInf    cache.SharedIndexInformer
	baseConf    *config.BaseOperatorConf
}

// NewConverterController builder for vmprometheusconverter service
func NewConverterController(ctx context.Context, baseClient *kubernetes.Clientset, rclient client.WithWatch, baseConf *config.BaseOperatorConf) (*ConverterController, error) {
	c := &ConverterController{
		ctx:        ctx,
		baseClient: baseClient,
		rclient:    rclient,
		baseConf:   baseConf,
	}
	c.ruleInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects v1.PrometheusRuleList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *v1.PrometheusRuleList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list prometheus_rules: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[v1.PrometheusRuleList](ctx, rclient, config.MustGetWatchNamespaces())
			},
		},
		&v1.PrometheusRule{},
		0,
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
				var objects v1.PodMonitorList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *v1.PodMonitorList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list pod_monitors: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[v1.PodMonitorList](ctx, rclient, config.MustGetWatchNamespaces())
			},
		},
		&v1.PodMonitor{},
		0,
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
				var objects v1.ServiceMonitorList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *v1.ServiceMonitorList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list service_monitors: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[v1.ServiceMonitorList](ctx, rclient, config.MustGetWatchNamespaces())
			},
		},
		&v1.ServiceMonitor{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	if _, err := c.serviceInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreateServiceMonitor,
		UpdateFunc: c.UpdateServiceMonitor,
	}); err != nil {
		return nil, fmt.Errorf("cannot add service_monitor handler: %w", err)
	}
	c.amConfigInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects v1alpha1.AlertmanagerConfigList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *v1alpha1.AlertmanagerConfigList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list alertmanager_configs: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[v1alpha1.AlertmanagerConfigList](ctx, rclient, config.MustGetWatchNamespaces())
			},
		},
		&v1alpha1.AlertmanagerConfig{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if _, err := c.amConfigInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreateAlertmanagerConfig,
		UpdateFunc: c.UpdateAlertmanagerConfig,
	}); err != nil {
		return nil, fmt.Errorf("cannot add alertmanager_config handler: %w", err)
	}
	c.probeInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var objects v1.ProbeList
				if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(dst *v1.ProbeList) {
					objects.Items = append(objects.Items, dst.Items...)
				}); err != nil {
					return nil, fmt.Errorf("cannot list probes: %w", err)
				}
				return &objects, nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return k8stools.NewObjectWatcherForNamespaces[v1.ProbeList](ctx, rclient, config.MustGetWatchNamespaces())
			},
		},
		&v1.Probe{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	if _, err := c.probeInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreateProbe,
		UpdateFunc: c.UpdateProbe,
	}); err != nil {
		return nil, fmt.Errorf("cannot add probe handler: %w", err)
	}
	return c, nil
}

func waitForAPIResource(ctx context.Context, client discovery.DiscoveryInterface, apiGroupVersion string, kind string) error {
	l := log.WithValues("group", apiGroupVersion, "kind", kind)
	l.Info("waiting for api resource")
	tick := time.NewTicker(time.Second * 5)
	for {
		select {
		case <-tick.C:
			_, apiLists, err := client.ServerGroupsAndResources()
			if err != nil {
				l.Error(err, "cannot get  server resource")
			}
			for _, apiList := range apiLists {
				if apiList.GroupVersion == apiGroupVersion {
					for _, r := range apiList.APIResources {
						if r.Kind == kind {
							l.Info("api resource is ready")
							return nil
						}
					}
				}
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
			return c.runInformerWithDiscovery(ctx, v1.SchemeGroupVersion.String(), v1.ServiceMonitorsKind, c.serviceInf.Run)
		})
	}
	if c.baseConf.EnabledPrometheusConverter.PodMonitor {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, v1.SchemeGroupVersion.String(), v1.PodMonitorsKind, c.podInf.Run)
		})
	}
	if c.baseConf.EnabledPrometheusConverter.PrometheusRule {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, v1.SchemeGroupVersion.String(), v1.PrometheusRuleKind, c.ruleInf.Run)
		})
	}
	if c.baseConf.EnabledPrometheusConverter.Probe {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, v1.SchemeGroupVersion.String(), v1.ProbesKind, c.probeInf.Run)
		})
	}

	if c.baseConf.EnabledPrometheusConverter.AlertmanagerConfig {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, v1alpha1.SchemeGroupVersion.String(), v1alpha1.AlertmanagerConfigKind, c.amConfigInf.Run)
		})
	}
}

// CreatePrometheusRule converts prometheus rule to vmrule
func (c *ConverterController) CreatePrometheusRule(rule interface{}) {
	promRule := rule.(*v1.PrometheusRule)
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
	promRuleNew := new.(*v1.PrometheusRule)
	l := log.WithValues("kind", "VMRule", "name", promRuleNew.Name, "ns", promRuleNew.Namespace)
	VMRule := converter.ConvertPromRule(promRuleNew, c.baseConf)
	ctx := context.Background()
	existingVMRule := &v1beta1.VMRule{}
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
	serviceMon := service.(*v1.ServiceMonitor)
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
	serviceMonNew := new.(*v1.ServiceMonitor)
	l := log.WithValues("kind", "vmServiceScrape", "name", serviceMonNew.Name, "ns", serviceMonNew.Namespace)
	vmServiceScrape := converter.ConvertServiceMonitor(serviceMonNew, c.baseConf)
	existingVMServiceScrape := &v1beta1.VMServiceScrape{}
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
	podMonitor := pod.(*v1.PodMonitor)
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
	podMonitorNew := new.(*v1.PodMonitor)
	l := log.WithValues("kind", "podScrape", "name", podMonitorNew.Name, "ns", podMonitorNew.Namespace)
	podScrape := converter.ConvertPodMonitor(podMonitorNew, c.baseConf)
	ctx := context.Background()
	existingVMPodScrape := &v1beta1.VMPodScrape{}
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
	promAMc := amc.(*v1alpha1.AlertmanagerConfig)
	l := log.WithValues("kind", "vmAlertmanagerConfig", "name", promAMc.Name, "ns", promAMc.Namespace)
	l.Info("syncing alertmanager config")
	vmAMc, err := converter.ConvertAlertmanagerConfig(promAMc, c.baseConf)
	if err != nil {
		l.Error(err, "cannot convert alertmanager config")
		return
	}
	if err := c.rclient.Create(context.Background(), vmAMc); err != nil {
		if errors.IsAlreadyExists(err) {
			c.UpdateAlertmanagerConfig(nil, promAMc)
			return
		}
		l.Error(err, "cannot create vmServiceScrape")
		return
	}
}

// UpdateAlertmanagerConfig updates VMAlertmanagerConfig
func (c *ConverterController) UpdateAlertmanagerConfig(_, new interface{}) {
	promAMc := new.(*v1alpha1.AlertmanagerConfig)
	l := log.WithValues("kind", "vmAlertmanagerConfig", "name", promAMc.Name, "ns", promAMc.Namespace)
	vmAMc, err := converter.ConvertAlertmanagerConfig(promAMc, c.baseConf)
	if err != nil {
		l.Error(err, "cannot convert alertmanager config at update")
		return
	}
	existAlertmanagerConfig := &v1beta1.VMAlertmanagerConfig{}
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
	probe := obj.(*v1.Probe)
	l := log.WithValues("kind", "vmProbe", "name", probe.Name, "ns", probe.Namespace)
	l.Info("syncing probes")
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
	probeNew := new.(*v1.Probe)
	l := log.WithValues("kind", "vmProbe", "name", probeNew.Name, "ns", probeNew.Namespace)
	vmProbe := converter.ConvertProbe(probeNew, c.baseConf)
	ctx := context.Background()
	existingVMProbe := &v1beta1.VMProbe{}
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
