package alertmanager

import (
	"context"
	"fmt"
	"sort"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func BuildConfig(ctx context.Context, rclient client.Client, baseCfg []byte, amcfgs map[string]*operatorv1beta1.VMAlertmanagerConfig) ([]byte, error) {
	// fast path.
	if len(amcfgs) == 0 {
		return baseCfg, nil
	}
	var baseYAMlCfg alertmanagerConfig
	if err := yaml.Unmarshal(baseCfg, &baseYAMlCfg); err != nil {
		return nil, fmt.Errorf("cannot parse base cfg :%w", err)
	}
	amConfigIdentifiers := make([]string, len(amcfgs))
	i := 0
	for k := range amcfgs {
		amConfigIdentifiers[i] = k
		i++
	}

	sort.Strings(amConfigIdentifiers)
	var subRoutes []yaml.MapSlice
	for _, posIdx := range amConfigIdentifiers {
		amcKey := amcfgs[posIdx]
		for _, rule := range amcKey.Spec.InhibitRules {
			baseYAMlCfg.InhibitRules = append(baseYAMlCfg.InhibitRules, buildInhibitRule(amcKey.Namespace, rule))
		}
		if amcKey.Spec.Route == nil {
			continue
		}
		subRoutes = append(subRoutes, buildRoute(amcKey, amcKey.Spec.Route, true))
		for _, receiver := range amcKey.Spec.Receivers {
			receiverCfg, err := buildReceiver(ctx, rclient, amcKey, receiver)
			if err != nil {
				return nil, fmt.Errorf("cannot build receiver cfg for: %s, err: %w", amcKey.AsKey(), err)
			}
			baseYAMlCfg.Receivers = append(baseYAMlCfg.Receivers, receiverCfg)
		}
	}
	baseYAMlCfg.Route.Routes = append(baseYAMlCfg.Route.Routes, subRoutes...)
	return yaml.Marshal(baseYAMlCfg)
}

func buildRoute(cr *operatorv1beta1.VMAlertmanagerConfig, cfgRoute *operatorv1beta1.Route, topLevel bool) yaml.MapSlice {
	var r yaml.MapSlice
	continueSetting := cfgRoute.Continue
	// enforce continue and namespace match
	if topLevel {
		continueSetting = true
		cfgRoute.Matchers = append(cfgRoute.Matchers, fmt.Sprintf("namespace = %s", cr.Namespace))
	}

	var nestedRoutes []yaml.MapSlice
	for _, nestedRoute := range cfgRoute.Routes {
		nestedRoutes = append(nestedRoutes, buildRoute(cr, nestedRoute, false))
	}
	if len(nestedRoutes) > 0 {
		r = append(r, yaml.MapItem{Key: "routes", Value: nestedRoutes})
	}
	toYaml := func(key string, src []string) {
		if len(src) > 0 {
			r = append(r, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYaml("matchers", cfgRoute.Matchers)
	toYaml("group_by", cfgRoute.GroupBy)
	toYaml("mute_time_intervals", cfgRoute.MuteTimeIntervals)
	r = append(r,
		yaml.MapItem{Key: "continue", Value: continueSetting},
		yaml.MapItem{Key: "group_interval", Value: cfgRoute.GroupInterval},
		yaml.MapItem{Key: "group_wait", Value: cfgRoute.GroupWait},
		yaml.MapItem{Key: "receiver", Value: buildReceiverName(cr, cfgRoute.Receiver)},
		yaml.MapItem{Key: "repeat_interval", Value: cfgRoute.RepeatInterval},
	)
	return r
}

func buildInhibitRule(namespace string, rule operatorv1beta1.InhibitRule) yaml.MapSlice {
	var r yaml.MapSlice
	namespaceMatch := fmt.Sprintf("namespace = %s", namespace)
	rule.SourceMatchers = append(rule.SourceMatchers, namespaceMatch)
	rule.TargetMatchers = append(rule.TargetMatchers, namespaceMatch)
	toYaml := func(key string, src []string) {
		if len(src) > 0 {
			r = append(r, yaml.MapItem{
				Key:   key,
				Value: src,
			})
		}
	}
	toYaml("target_matchers", rule.TargetMatchers)
	toYaml("source_matchers", rule.SourceMatchers)
	toYaml("equal", rule.Equal)
	return r
}

func buildReceiverName(cr *operatorv1beta1.VMAlertmanagerConfig, name string) string {
	return fmt.Sprintf("%s-%s-%s", cr.Namespace, cr.Name, name)
}

type alertmanagerConfig struct {
	Global       interface{}     `yaml:"global,omitempty" json:"global,omitempty"`
	Route        *route          `yaml:"route,omitempty" json:"route,omitempty"`
	InhibitRules []yaml.MapSlice `yaml:"inhibit_rules,omitempty" json:"inhibit_rules,omitempty"`
	Receivers    []yaml.MapSlice `yaml:"receivers,omitempty" json:"receivers,omitempty"`
	Templates    []string        `yaml:"templates" json:"templates"`
}

type route struct {
	Receiver       string            `yaml:"receiver,omitempty" json:"receiver,omitempty"`
	GroupByStr     []string          `yaml:"group_by,omitempty" json:"group_by,omitempty"`
	Match          map[string]string `yaml:"match,omitempty" json:"match,omitempty"`
	MatchRE        map[string]string `yaml:"match_re,omitempty" json:"match_re,omitempty"`
	Continue       bool              `yaml:"continue,omitempty" json:"continue,omitempty"`
	Routes         []yaml.MapSlice   `yaml:"routes,omitempty" json:"routes,omitempty"`
	GroupWait      string            `yaml:"group_wait,omitempty" json:"group_wait,omitempty"`
	GroupInterval  string            `yaml:"group_interval,omitempty" json:"group_interval,omitempty"`
	RepeatInterval string            `yaml:"repeat_interval,omitempty" json:"repeat_interval,omitempty"`
}

func buildReceiver(ctx context.Context, rclient client.Client, cr *operatorv1beta1.VMAlertmanagerConfig, reciever operatorv1beta1.Receiver) (yaml.MapSlice, error) {
	var r yaml.MapSlice
	r = append(r, yaml.MapItem{
		Key:   "name",
		Value: buildReceiverName(cr, reciever.Name),
	})
	cb := initConfigBuilder(ctx, rclient, cr, reciever)

	if err := cb.buildCfg(); err != nil {
		return nil, err
	}
	return cb.result, nil
}

type configBuilder struct {
	client.Client
	ctx         context.Context
	receiver    operatorv1beta1.Receiver
	currentYaml []yaml.MapSlice
	result      yaml.MapSlice
	err         error
}

func initConfigBuilder(ctx context.Context, rclient client.Client, cr *operatorv1beta1.VMAlertmanagerConfig, reciver operatorv1beta1.Receiver) *configBuilder {
	cb := configBuilder{
		ctx:      ctx,
		Client:   rclient,
		receiver: reciver,
		result: yaml.MapSlice{
			{
				Key:   "name",
				Value: buildReceiverName(cr, reciver.Name),
			},
		},
	}

	return &cb
}

func (cb *configBuilder) buildCfg() error {
	for _, opsGenCfg := range cb.receiver.OpsGenieConfigs {
		if err := cb.buildOpsGenie(opsGenCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("opsgenie_configs")
	return nil
}

func (cb *configBuilder) finalizeSection(name string) {
	if len(cb.currentYaml) > 0 {
		cb.result = append(cb.result, yaml.MapItem{
			Key:   name,
			Value: cb.currentYaml,
		})
	}
	cb.currentYaml = cb.currentYaml[:0]
}

func (cb *configBuilder) buildOpsGenie(og operatorv1beta1.OpsGenieConfig) error {
	var temp yaml.MapSlice
	toYamlString := func(key string, value string) {
		if len(key) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: value})
		}
	}
	toYamlString("source", og.Source)
	toYamlString("description", og.Description)
	toYamlString("message", og.Message)
	toYamlString("tags", og.Tags)
	toYamlString("note", og.Note)
	toYamlString("api_url", og.APIURL)
	toYamlString("priority", og.Priority)
	if og.Details != nil {
		temp = append(temp, yaml.MapItem{Key: "details", Value: og.Details})
	}
	if og.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *og.SendResolved})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func buildHTTPConfig(httpCfg *operatorv1beta1.HTTPConfig, cache map[string]*v1.Secret) (yaml.MapSlice, error) {
	var r yaml.MapSlice
	fetchSecretValue := func(selector *v1.SecretKeySelector) []byte {
		if s, ok := cache[selector.Name]; ok {
			return s.Data[selector.Key]
		}
		return nil
	}
	if httpCfg == nil {
		return nil, nil
	}
	if httpCfg.TLSConfig != nil {
	}
	if httpCfg.BasicAuth != nil {

	}
	if httpCfg.BearerTokenSecret != nil {
		bearer := fetchSecretValue(httpCfg.BearerTokenSecret)
		if bearer == nil {
			return nil, fmt.Errorf("cannot find secret for bearerToken")
		}
		r = append(r, yaml.MapItem{
			Key:   "bearer_token",
			Value: string(bearer),
		})
	}
	return r, nil
}
