package controllers

import (
	"context"
	"fmt"
	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/converter"
	"github.com/VictoriaMetrics/operator/internal/conf"
	v1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/coreos/prometheus-operator/pkg/client/versioned"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// ConverterController - watches for prometheus objects
// and create VictoriaMetrics objects
type ConverterController struct {
	promClient versioned.Interface
	vclient    client.Client
	ruleInf    cache.SharedInformer
	podInf     cache.SharedInformer
	serviceInf cache.SharedInformer
}

// NewConverterController builder for vmprometheusconverter service
func NewConverterController(promCl versioned.Interface, vclient client.Client) *ConverterController {
	c := &ConverterController{
		promClient: promCl,
		vclient:    vclient,
	}
	c.ruleInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return promCl.MonitoringV1().PrometheusRules("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return promCl.MonitoringV1().PrometheusRules("").Watch(context.TODO(), options)
			},
		},
		&v1.PrometheusRule{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	c.ruleInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreatePrometheusRule,
		UpdateFunc: c.UpdatePrometheusRule,
	})
	c.podInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return promCl.MonitoringV1().PodMonitors("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return promCl.MonitoringV1().PodMonitors("").Watch(context.TODO(), options)
			},
		},
		&v1.PodMonitor{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	c.podInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreatePodMonitor,
		UpdateFunc: c.UpdatePodMonitor,
	})
	c.serviceInf = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return promCl.MonitoringV1().ServiceMonitors("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return promCl.MonitoringV1().ServiceMonitors("").Watch(context.TODO(), options)
			},
		},
		&v1.ServiceMonitor{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)
	c.serviceInf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.CreateServiceMonitor,
		UpdateFunc: c.UpdateServiceMonitor,
	})
	return c
}

func waitForAPIResource(ctx context.Context, client discovery.DiscoveryInterface, apiGroupVersion string, kind string) error {
	l := log.WithValues("group", apiGroupVersion, "kind", kind)
	l.Info("waiting for api resource")
	tick := time.NewTicker(time.Second * 10)
	for {
		select {
		case <-tick.C:
			_, apiLists, err := client.ServerGroupsAndResources()
			if err != nil {
				l.Error(err, "cannot get  server resource")
				//				return err
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
			l.Info("api resource doesnt exist, waiting for it")

		case <-ctx.Done():
			l.Info("context was canceled")
			return nil
		}
	}

}

func (c *ConverterController) runInformerWithDiscovery(ctx context.Context, group, kind string, runInformer func(<-chan struct{})) error {
	err := waitForAPIResource(ctx, c.promClient.Discovery(), group, kind)
	if err != nil {
		return fmt.Errorf("error wait for %s, err: %w", kind, err)
	}
	runInformer(ctx.Done())
	return nil
}

// Run - starts vmprometheusconverter with background discovery process for each prometheus api object
func (c *ConverterController) Run(ctx context.Context, group *errgroup.Group, cfg *conf.BaseOperatorConf) {

	if cfg.EnabledPrometheusConverter.ServiceScrape {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, v1.SchemeGroupVersion.String(), v1.ServiceMonitorsKind, c.serviceInf.Run)
		})

	}
	if cfg.EnabledPrometheusConverter.PodMonitor {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, v1.SchemeGroupVersion.String(), v1.PodMonitorsKind, c.podInf.Run)
		})

	}
	if cfg.EnabledPrometheusConverter.PrometheusRule {
		group.Go(func() error {
			return c.runInformerWithDiscovery(ctx, v1.SchemeGroupVersion.String(), v1.PrometheusRuleKind, c.ruleInf.Run)
		})

	}
}

// CreatePrometheusRule converts prometheus rule to vmrule
func (c *ConverterController) CreatePrometheusRule(rule interface{}) {
	promRule := rule.(*v1.PrometheusRule)
	l := log.WithValues("kind", "alertRule", "name", promRule.Name, "ns", promRule.Namespace)
	l.Info("syncing prom rule with VMRule")
	cr := converter.ConvertPromRule(promRule)

	err := c.vclient.Create(context.Background(), cr)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			l.Info("AlertRule already exists")
			return
		}
		l.Error(err, "cannot create AlertRule from Prometheusrule")
		return
	}
	l.Info("AlertRule was created")
}

// UpdatePrometheusRule updates vmrule
func (c *ConverterController) UpdatePrometheusRule(old, new interface{}) {
	promRuleNew := new.(*v1.PrometheusRule)
	l := log.WithValues("kind", "VMRule", "name", promRuleNew.Name, "ns", promRuleNew.Namespace)
	l.Info("updating VMRule")
	VMRule := converter.ConvertPromRule(promRuleNew)
	ctx := context.Background()
	existingVMRule := &v1beta1.VMRule{}
	err := c.vclient.Get(ctx, types.NamespacedName{Name: VMRule.Name, Namespace: VMRule.Namespace}, existingVMRule)
	if err != nil {
		l.Error(err, "cannot get existing VMRule")
		return
	}

	existingVMRule.Spec = VMRule.Spec
	err = c.vclient.Update(ctx, existingVMRule)
	if err != nil {
		l.Error(err, "cannot update VMRule")
		return
	}
	l.Info("VMRule was updated")

}

// CreateServiceMonitor converts ServiceMonitor to VMServiceScrape
func (c *ConverterController) CreateServiceMonitor(service interface{}) {
	serviceMon := service.(*v1.ServiceMonitor)
	l := log.WithValues("kind", "vmServiceScrape", "name", serviceMon.Name, "ns", serviceMon.Namespace)
	l.Info("syncing vmServiceScrape")
	vmServiceScrape := converter.ConvertServiceMonitor(serviceMon)
	err := c.vclient.Create(context.Background(), vmServiceScrape)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			l.Info("vmServiceScrape exists")
			return
		}
		l.Error(err, "cannot create vmServiceScrape")
		return
	}
	l.Info("vmServiceScrape was created")
}

// UpdateServiceMonitor updates VMServiceMonitor
func (c *ConverterController) UpdateServiceMonitor(old, new interface{}) {
	serviceMonNew := new.(*v1.ServiceMonitor)
	l := log.WithValues("kind", "vmServiceScrape", "name", serviceMonNew.Name, "ns", serviceMonNew.Namespace)
	l.Info("updating vmServiceScrape")
	vmServiceScrape := converter.ConvertServiceMonitor(serviceMonNew)
	existingVMServiceScrape := &v1beta1.VMServiceScrape{}
	ctx := context.Background()
	err := c.vclient.Get(ctx, types.NamespacedName{Name: vmServiceScrape.Name, Namespace: vmServiceScrape.Namespace}, existingVMServiceScrape)
	if err != nil {
		l.Error(err, "cannot get existing vmServiceScrape")
		return
	}
	existingVMServiceScrape.Spec = vmServiceScrape.Spec

	err = c.vclient.Update(ctx, existingVMServiceScrape)
	if err != nil {
		l.Error(err, "cannot update")
		return
	}
	l.Info("vmServiceScrape was updated")
}

// CreatePodMonitor converts PodMonitor to VMPodScrape
func (c *ConverterController) CreatePodMonitor(pod interface{}) {
	podMonitor := pod.(*v1.PodMonitor)
	l := log.WithValues("kind", "podScrape", "name", podMonitor.Name, "ns", podMonitor.Namespace)
	l.Info("syncing podScrape")
	podScrape := converter.ConvertPodMonitor(podMonitor)
	err := c.vclient.Create(context.TODO(), podScrape)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			l.Info("podScrape already exists")
			return
		}
		l.Error(err, "cannot create podScrape")
		return
	}
	log.Info("podScrape was created")

}

// UpdatePodMonitor updates VMPodScrape
func (c *ConverterController) UpdatePodMonitor(old, new interface{}) {
	podMonitorNew := new.(*v1.PodMonitor)
	l := log.WithValues("kind", "podScrape", "name", podMonitorNew.Name, "ns", podMonitorNew.Namespace)
	podScrape := converter.ConvertPodMonitor(podMonitorNew)
	ctx := context.Background()
	existingVMPodScrape := &v1beta1.VMPodScrape{}
	err := c.vclient.Get(ctx, types.NamespacedName{Name: podScrape.Name, Namespace: podScrape.Namespace}, existingVMPodScrape)
	if err != nil {
		l.Error(err, "cannot get existing alertRule")
		return
	}
	existingVMPodScrape.Spec = podScrape.Spec
	err = c.vclient.Update(ctx, existingVMPodScrape)
	if err != nil {
		l.Error(err, "cannot update podScrape")
		return
	}
	l.Info("podScrape was updated")

}
