package vmprometheusconverter

import (
	"context"
	"fmt"
	"github.com/VictoriaMetrics/operator/conf"
	vmclient "github.com/VictoriaMetrics/operator/pkg/client/versioned"
	v1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/coreos/prometheus-operator/pkg/client/versioned"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/discovery"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

var log = logf.Log.WithName("vmprometheuscontroller")

// ConvertorController - watches for prometheus objects
// and create VictoriaMetrics objects
type ConvertorController struct {
	promClient versioned.Interface
	vclient    vmclient.Interface
	ruleInf    cache.SharedInformer
	podInf     cache.SharedInformer
	serviceInf cache.SharedInformer
}

func NewConvertorController(promCl versioned.Interface, vclient vmclient.Interface) *ConvertorController {
	c := &ConvertorController{
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

func waitForApiResource(ctx context.Context, client discovery.DiscoveryInterface, apiGroupVersion string, kind string) error {
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

func (c *ConvertorController) runInformerWithDiscovery(ctx context.Context, group, kind string, runInformer func(<-chan struct{})) error {
	err := waitForApiResource(ctx, c.promClient.Discovery(), group, kind)
	if err != nil {
		return fmt.Errorf("error wait for %s, err: %w", kind, err)
	}
	runInformer(ctx.Done())
	return nil
}

func (c *ConvertorController) Run(ctx context.Context, group *errgroup.Group, cfg *conf.BaseOperatorConf) {

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
