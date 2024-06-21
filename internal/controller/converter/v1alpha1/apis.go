package v1alpha1

import (
	"encoding/json"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/converter"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"gopkg.in/yaml.v2"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithValues("controller", "prometheus.converter")

func convertMatchers(promMatchers []promv1alpha1.Matcher) []string {
	if promMatchers == nil {
		return nil
	}
	r := make([]string, 0, len(promMatchers))
	for _, pm := range promMatchers {
		if len(pm.Value) > 0 && pm.MatchType == "" {
			pm.MatchType = "=~"
		}
		if pm.MatchType == "" {
			pm.MatchType = "="
		}
		r = append(r, pm.String())
	}
	return r
}

func convertRoute(promRoute *promv1alpha1.Route) (*vmv1beta1.Route, error) {
	if promRoute == nil {
		return nil, nil
	}
	r := vmv1beta1.Route{
		Receiver:            promRoute.Receiver,
		Continue:            promRoute.Continue,
		GroupBy:             promRoute.GroupBy,
		GroupWait:           promRoute.GroupWait,
		GroupInterval:       promRoute.GroupInterval,
		RepeatInterval:      promRoute.RepeatInterval,
		Matchers:            convertMatchers(promRoute.Matchers),
		MuteTimeIntervals:   promRoute.MuteTimeIntervals,
		ActiveTimeIntervals: promRoute.ActiveTimeIntervals,
	}
	for _, route := range promRoute.Routes {
		var promRoute promv1alpha1.Route
		if err := json.Unmarshal(route.Raw, &promRoute); err != nil {
			return nil, fmt.Errorf("cannot parse raw prom route: %s, err: %w", string(route.Raw), err)
		}
		vmRoute, err := convertRoute(&promRoute)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(vmRoute)
		if err != nil {
			return nil, fmt.Errorf("cannot serialize vm route for alertmanager config: %w", err)
		}
		r.RawRoutes = append(r.RawRoutes, apiextensionsv1.JSON{Raw: data})
	}
	return &r, nil
}

func convertInhibitRules(promIRs []promv1alpha1.InhibitRule) []vmv1beta1.InhibitRule {
	if promIRs == nil {
		return nil
	}
	vmIRs := make([]vmv1beta1.InhibitRule, 0, len(promIRs))
	for _, promIR := range promIRs {
		ir := vmv1beta1.InhibitRule{
			TargetMatchers: convertMatchers(promIR.TargetMatch),
			SourceMatchers: convertMatchers(promIR.SourceMatch),
			Equal:          promIR.Equal,
		}
		vmIRs = append(vmIRs, ir)
	}
	return vmIRs
}

func convertMuteIntervals(promMIs []promv1alpha1.MuteTimeInterval) []vmv1beta1.MuteTimeInterval {
	if promMIs == nil {
		return nil
	}

	vmMIs := make([]vmv1beta1.MuteTimeInterval, 0, len(promMIs))
	for _, promMI := range promMIs {
		vmMI := vmv1beta1.MuteTimeInterval{
			Name:          promMI.Name,
			TimeIntervals: make([]vmv1beta1.TimeInterval, 0, len(promMI.TimeIntervals)),
		}
		for _, tis := range promMI.TimeIntervals {
			var vmTIs vmv1beta1.TimeInterval
			for _, t := range tis.Times {
				vmTIs.Times = append(vmTIs.Times, vmv1beta1.TimeRange{EndTime: string(t.EndTime), StartTime: string(t.StartTime)})
			}
			for _, dm := range tis.DaysOfMonth {
				vmTIs.DaysOfMonth = append(vmTIs.DaysOfMonth, fmt.Sprintf("%d:%d", dm.Start, dm.End))
			}
			for _, wm := range tis.Weekdays {
				vmTIs.Weekdays = append(vmTIs.Weekdays, string(wm))
			}
			for _, y := range tis.Years {
				vmTIs.Years = append(vmTIs.Years, string(y))
			}
			for _, m := range tis.Months {
				vmTIs.Months = append(vmTIs.Months, string(m))
			}
			vmMI.TimeIntervals = append(vmMI.TimeIntervals, vmTIs)
		}
		vmMIs = append(vmMIs, vmMI)
	}
	return vmMIs
}

func convertReceivers(promReceivers []promv1alpha1.Receiver) ([]vmv1beta1.Receiver, error) {
	// yaml instead of json is used by purpose
	// prometheus-operator objects has different field tags
	marshaledRcvs, err := yaml.Marshal(promReceivers)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize prometheus receivers, err: %w", err)
	}
	var vmReceivers []vmv1beta1.Receiver
	if err := yaml.Unmarshal(marshaledRcvs, &vmReceivers); err != nil {
		return nil, fmt.Errorf("cannot parse serialized prometheus receievers: %s, err: %w", string(marshaledRcvs), err)
	}
	return vmReceivers, nil
}

// ConvertAlertmanagerConfig creates VMAlertmanagerConfig from prometheus alertmanagerConfig
func ConvertAlertmanagerConfig(promAMCfg *promv1alpha1.AlertmanagerConfig, conf *config.BaseOperatorConf) (*vmv1beta1.VMAlertmanagerConfig, error) {
	vamc := &vmv1beta1.VMAlertmanagerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        promAMCfg.Name,
			Namespace:   promAMCfg.Namespace,
			Annotations: converter.FilterPrefixes(promAMCfg.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
			Labels:      converter.FilterPrefixes(promAMCfg.Labels, conf.FilterPrometheusConverterLabelPrefixes),
		},
		Spec: vmv1beta1.VMAlertmanagerConfigSpec{
			InhibitRules:     convertInhibitRules(promAMCfg.Spec.InhibitRules),
			MutTimeIntervals: convertMuteIntervals(promAMCfg.Spec.MuteTimeIntervals),
		},
	}
	convertedRoute, err := convertRoute(promAMCfg.Spec.Route)
	if err != nil {
		return nil, fmt.Errorf("cannot convert prometheus alertmanager config: %s into vm, err: %w", promAMCfg.Name, err)
	}
	vamc.Spec.Route = convertedRoute
	convertedReceivers, err := convertReceivers(promAMCfg.Spec.Receivers)
	if err != nil {
		return nil, fmt.Errorf("cannot convert prometheus alertmanager config: %s into vm, err: %w", promAMCfg.Name, err)
	}
	vamc.Spec.Receivers = convertedReceivers
	if conf.EnabledPrometheusConverterOwnerReferences {
		vamc.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         promv1alpha1.SchemeGroupVersion.String(),
				Kind:               promv1alpha1.AlertmanagerConfigKind,
				Name:               promAMCfg.Name,
				UID:                promAMCfg.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	vamc.Annotations = converter.MaybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, vamc.Annotations)
	return vamc, nil
}

// ConvertScrapeConfig creates VMScrapeConfig from prometheus scrapeConfig
func ConvertScrapeConfig(promscrapeConfig *promv1alpha1.ScrapeConfig, conf *config.BaseOperatorConf) *vmv1beta1.VMScrapeConfig {
	cs := &vmv1beta1.VMScrapeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        promscrapeConfig.Name,
			Namespace:   promscrapeConfig.Namespace,
			Labels:      converter.FilterPrefixes(promscrapeConfig.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: converter.FilterPrefixes(promscrapeConfig.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
	}
	data, err := json.Marshal(promscrapeConfig.Spec)
	if err != nil {
		log.Error(err, "POSSIBLE BUG: failed to marshal prometheus scrapeconfig for converting", "name", promscrapeConfig.Name, "namespace", promscrapeConfig.Namespace)
		return cs
	}
	err = json.Unmarshal(data, &cs.Spec)
	if err != nil {
		log.Error(err, "POSSIBLE BUG: failed to convert prometheus scrapeconfig to VMScrapeConfig", "name", promscrapeConfig.Name, "namespace", promscrapeConfig.Namespace)
		return cs
	}
	cs.Labels = converter.FilterPrefixes(promscrapeConfig.Labels, conf.FilterPrometheusConverterLabelPrefixes)
	cs.Annotations = converter.FilterPrefixes(promscrapeConfig.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes)
	cs.Spec.RelabelConfigs = converter.ConvertRelabelConfig(promscrapeConfig.Spec.RelabelConfigs)
	cs.Spec.MetricRelabelConfigs = converter.ConvertRelabelConfig(promscrapeConfig.Spec.MetricRelabelConfigs)

	if promscrapeConfig.Spec.EnableCompression != nil {
		cs.Spec.VMScrapeParams = &vmv1beta1.VMScrapeParams{
			DisableCompression: ptr.To(!*promscrapeConfig.Spec.EnableCompression),
		}
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         promv1alpha1.SchemeGroupVersion.String(),
				Kind:               promv1alpha1.ScrapeConfigsKind,
				Name:               promscrapeConfig.Name,
				UID:                promscrapeConfig.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cs.Annotations = converter.MaybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
	return cs
}
