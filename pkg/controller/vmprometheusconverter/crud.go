package vmprometheusconverter

import (
	"context"
	v1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *ConvertorController) CreatePrometheusRule(rule interface{}) {
	promRule := rule.(*v1.PrometheusRule)
	l := log.WithValues("kind", "alertRule", "name", promRule.Name, "ns", promRule.Namespace)
	l.Info("syncing prom rule with VMRule")
	cr := convertPromRule(promRule)

	_, err := c.vclient.VictoriametricsV1beta1().VMRules(promRule.Namespace).Create(context.TODO(), cr, metav1.CreateOptions{})
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

func (c *ConvertorController) UpdatePrometheusRule(old, new interface{}) {
	promRuleNew := new.(*v1.PrometheusRule)
	l := log.WithValues("kind", "VMRule", "name", promRuleNew.Name, "ns", promRuleNew.Namespace)
	l.Info("updating VMRule")
	VMRule := convertPromRule(promRuleNew)
	ctx := context.Background()
	existingVMRule, err := c.vclient.VictoriametricsV1beta1().VMRules(VMRule.Namespace).Get(ctx, VMRule.Name, metav1.GetOptions{})
	if err != nil {
		l.Error(err, "cannot get existing VMRule")
		return
	}

	existingVMRule.Spec = VMRule.Spec
	_, err = c.vclient.VictoriametricsV1beta1().VMRules(VMRule.Namespace).Update(ctx, existingVMRule, metav1.UpdateOptions{})
	if err != nil {
		l.Error(err, "cannot update VMRule")
		return
	}
	l.Info("VMRule was updated")

}

func (c *ConvertorController) CreateServiceMonitor(service interface{}) {
	serviceMon := service.(*v1.ServiceMonitor)
	l := log.WithValues("kind", "vmServiceScrape", "name", serviceMon.Name, "ns", serviceMon.Namespace)
	l.Info("syncing vmServiceScrape")
	vmServiceScrape := convertServiceMonitor(serviceMon)
	_, err := c.vclient.VictoriametricsV1beta1().VMServiceScrapes(serviceMon.Namespace).Create(context.TODO(), vmServiceScrape, metav1.CreateOptions{})
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

func (c *ConvertorController) UpdateServiceMonitor(old, new interface{}) {
	serviceMonNew := new.(*v1.ServiceMonitor)
	l := log.WithValues("kind", "vmServiceScrape", "name", serviceMonNew.Name, "ns", serviceMonNew.Namespace)
	l.Info("updating vmServiceScrape")
	vmServiceScrape := convertServiceMonitor(serviceMonNew)
	ctx := context.Background()
	existingVMServiceScrape, err := c.vclient.VictoriametricsV1beta1().VMServiceScrapes(vmServiceScrape.Namespace).Get(ctx, vmServiceScrape.Name, metav1.GetOptions{})
	if err != nil {
		l.Error(err, "cannot get existing vmServiceScrape")
		return
	}
	existingVMServiceScrape.Spec = vmServiceScrape.Spec

	_, err = c.vclient.VictoriametricsV1beta1().VMServiceScrapes(vmServiceScrape.Namespace).Update(ctx, existingVMServiceScrape, metav1.UpdateOptions{})
	if err != nil {
		l.Error(err, "cannot update")
		return
	}
	l.Info("vmServiceScrape was updated")
}

func (c *ConvertorController) CreatePodMonitor(pod interface{}) {
	podMonitor := pod.(*v1.PodMonitor)
	l := log.WithValues("kind", "podScrape", "name", podMonitor.Name, "ns", podMonitor.Namespace)
	l.Info("syncing podScrape")
	podScrape := convertPodMonitor(podMonitor)
	_, err := c.vclient.VictoriametricsV1beta1().VMPodScrapes(podScrape.Namespace).Create(context.TODO(), podScrape, metav1.CreateOptions{})
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
func (c *ConvertorController) UpdatePodMonitor(old, new interface{}) {
	podMonitorNew := new.(*v1.PodMonitor)
	l := log.WithValues("kind", "podScrape", "name", podMonitorNew.Name, "ns", podMonitorNew.Namespace)
	podScrape := convertPodMonitor(podMonitorNew)
	ctx := context.Background()
	existingVMPodScrape, err := c.vclient.VictoriametricsV1beta1().VMPodScrapes(podScrape.Namespace).Get(ctx, podScrape.Name, metav1.GetOptions{})
	if err != nil {
		l.Error(err, "cannot get existing alertRule")
		return
	}
	existingVMPodScrape.Spec = podScrape.Spec
	_, err = c.vclient.VictoriametricsV1beta1().VMPodScrapes(podScrape.Namespace).Update(ctx, existingVMPodScrape, metav1.UpdateOptions{})
	if err != nil {
		l.Error(err, "cannot update podScrape")
		return
	}
	l.Info("podScrape was updated")

}
