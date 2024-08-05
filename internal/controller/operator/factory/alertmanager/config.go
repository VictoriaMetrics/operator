package alertmanager

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type parsedConfig struct {
	data         []byte
	amcfgs       []*vmv1beta1.VMAlertmanagerConfig
	brokenAMCfgs []*vmv1beta1.VMAlertmanagerConfig
}

func buildConfig(ctx context.Context, rclient client.Client, mustAddNamespaceMatcher, disableRouteContinueEnforce bool, baseCfg []byte, amcfgs []*vmv1beta1.VMAlertmanagerConfig, tlsAssets map[string]string) (*parsedConfig, error) {
	// fast path.
	if len(amcfgs) == 0 {
		return &parsedConfig{data: baseCfg}, nil
	}
	var globalConfigOpts globalAlertmanagerConfig
	if err := yaml.Unmarshal(baseCfg, &globalConfigOpts); err != nil {
		return nil, fmt.Errorf("cannot parse global config options: %w", err)
	}
	var baseYAMlCfg alertmanagerConfig
	if err := yaml.Unmarshal(baseCfg, &baseYAMlCfg); err != nil {
		return nil, fmt.Errorf("cannot parse base cfg :%w", err)
	}

	if baseYAMlCfg.Route == nil {
		baseYAMlCfg.Route = &route{
			Receiver: "blackhole",
		}
		var isBlacholeDefined bool
		for _, recv := range baseYAMlCfg.Receivers {
			var recvName string
			for _, entry := range recv {
				if entry.Key == "name" {
					s, ok := entry.Value.(string)
					if !ok {
						return nil, fmt.Errorf("incorrect base configuration=%q, expected receiver name=%v to be a string", string(baseCfg), entry.Value)
					}
					recvName = s
					break
				}
			}
			if recvName == "blackhole" {
				isBlacholeDefined = true
				break
			}
		}
		if !isBlacholeDefined {
			baseYAMlCfg.Receivers = append(baseYAMlCfg.Receivers, yaml.MapSlice{
				{
					Key:   "name",
					Value: "blackhole",
				},
			})
		}
	}
	sort.Slice(amcfgs, func(i, j int) bool {
		return amcfgs[i].AsKey() < amcfgs[j].AsKey()
	})
	var subRoutes []yaml.MapSlice
	var timeIntervals []yaml.MapSlice
	secretCache := make(map[string]*corev1.Secret)
	configmapCache := make(map[string]*corev1.ConfigMap)
	var result parsedConfig

	var cnt int
OUTER:
	for _, amcKey := range amcfgs {
		if amcKey.Spec.Route == nil {
			amcKey.Status.CurrentSyncError = "spec.route cannot be empty"
			result.brokenAMCfgs = append(result.brokenAMCfgs, amcKey)
			continue
		}
		var receiverCfgs []yaml.MapSlice
		for _, receiver := range amcKey.Spec.Receivers {
			receiverCfg, err := buildReceiver(ctx, rclient, amcKey, receiver, &globalConfigOpts, secretCache, configmapCache, tlsAssets)
			if err != nil {
				// skip broken configs
				result.brokenAMCfgs = append(result.brokenAMCfgs, amcKey)
				amcKey.Status.CurrentSyncError = err.Error()
				continue OUTER
			}
			if len(receiverCfg) > 0 {
				receiverCfgs = append(receiverCfgs, receiverCfg)
			}
		}

		mtis, err := buildGlobalTimeIntervals(amcKey)
		if err != nil {
			result.brokenAMCfgs = append(result.brokenAMCfgs, amcKey)
			amcKey.Status.CurrentSyncError = err.Error()
			continue
		}

		route, err := buildRoute(amcKey, amcKey.Spec.Route, true, disableRouteContinueEnforce, mustAddNamespaceMatcher)
		if err != nil {
			result.brokenAMCfgs = append(result.brokenAMCfgs, amcKey)
			amcKey.Status.CurrentSyncError = err.Error()
			continue
		}

		baseYAMlCfg.Receivers = append(baseYAMlCfg.Receivers, receiverCfgs...)
		for _, rule := range amcKey.Spec.InhibitRules {
			baseYAMlCfg.InhibitRules = append(baseYAMlCfg.InhibitRules, buildInhibitRule(amcKey.Namespace, rule, mustAddNamespaceMatcher))
		}
		if len(mtis) > 0 {
			timeIntervals = append(timeIntervals, mtis...)
		}
		subRoutes = append(subRoutes, route)
		amcfgs[cnt] = amcKey
		cnt++
	}
	amcfgs = amcfgs[:cnt]

	if len(subRoutes) > 0 {
		baseYAMlCfg.Route.Routes = append(baseYAMlCfg.Route.Routes, subRoutes...)
	}
	if len(timeIntervals) > 0 {
		baseYAMlCfg.TimeIntervals = append(baseYAMlCfg.TimeIntervals, timeIntervals...)
	}

	data, err := yaml.Marshal(baseYAMlCfg)
	if err != nil {
		return nil, err
	}
	result.amcfgs = amcfgs
	result.data = data
	return &result, nil
}

// addConfigTemplates adds external templates to the given based configuration
func addConfigTemplates(baseCfg []byte, templates []string) ([]byte, error) {
	if len(templates) == 0 {
		return baseCfg, nil
	}
	var baseYAMlCfg alertmanagerConfig
	if err := yaml.Unmarshal(baseCfg, &baseYAMlCfg); err != nil {
		return nil, fmt.Errorf("cannot parse base cfg :%w", err)
	}
	templatesSetByIdx := make(map[string]int)
	for idx, v := range baseYAMlCfg.Templates {
		templatesSetByIdx[v] = idx
	}
	for _, v := range templates {
		if len(strings.TrimSpace(v)) == 0 {
			continue
		}
		if _, ok := templatesSetByIdx[v]; ok {
			continue
		}
		// override value with correct path
		if idx, ok := templatesSetByIdx[path.Base(v)]; ok {
			baseYAMlCfg.Templates[idx] = v
			continue
		}
		baseYAMlCfg.Templates = append(baseYAMlCfg.Templates, v)
		templatesSetByIdx[v] = len(baseYAMlCfg.Templates) - 1
	}
	return yaml.Marshal(baseYAMlCfg)
}

func buildGlobalTimeIntervals(cr *vmv1beta1.VMAlertmanagerConfig) ([]yaml.MapSlice, error) {
	var r []yaml.MapSlice
	timeIntervalNameList := map[string]struct{}{}
	tis := cr.Spec.TimeIntervals
	for _, mti := range tis {
		if _, ok := timeIntervalNameList[mti.Name]; ok {
			return r, fmt.Errorf("got duplicate timeInterval name %s", mti.Name)
		}
		timeIntervalNameList[mti.Name] = struct{}{}
		if len(mti.TimeIntervals) == 0 {
			continue
		}
		var temp []yaml.MapSlice
		var tiItem yaml.MapSlice
		toYaml := func(key string, src []string) {
			if len(src) > 0 {
				tiItem = append(tiItem, yaml.MapItem{Key: key, Value: src})
			}
		}
		for _, ti := range mti.TimeIntervals {
			tiItem = yaml.MapSlice{}
			toYaml("days_of_month", ti.DaysOfMonth)
			toYaml("weekdays", ti.Weekdays)
			toYaml("months", ti.Months)
			toYaml("years", ti.Years)
			if len(ti.Location) > 0 {
				tiItem = append(tiItem, yaml.MapItem{Key: "location", Value: ti.Location})
			}

			var trss []yaml.MapSlice
			for _, trs := range ti.Times {
				if trs.EndTime != "" && trs.StartTime != "" {
					trss = append(trss, yaml.MapSlice{{Key: "start_time", Value: trs.StartTime}, {Key: "end_time", Value: trs.EndTime}})
				}
			}
			if len(trss) > 0 {
				tiItem = append(tiItem, yaml.MapItem{Key: "times", Value: trss})
			}
			if len(tiItem) > 0 {
				temp = append(temp, tiItem)
			}
		}
		if len(temp) > 0 {
			r = append(r, yaml.MapSlice{{Key: "name", Value: buildCRPrefixedName(cr, mti.Name)}, {Key: "time_intervals", Value: temp}})
		}
	}
	return r, nil
}

func buildRoute(cr *vmv1beta1.VMAlertmanagerConfig, cfgRoute *vmv1beta1.Route, topLevel, disableRouteContinueEnforce, mustAddNamespaceMatcher bool) (yaml.MapSlice, error) {
	var r yaml.MapSlice
	matchers := cfgRoute.Matchers
	// enforce continue when route is first-level and vmalertmanager disableRouteContinueEnforce filed is not set,
	// otherwise, always inherit from VMAlertmanagerConfig
	continueSetting := cfgRoute.Continue
	if topLevel && !disableRouteContinueEnforce {
		continueSetting = true
	}
	if mustAddNamespaceMatcher {
		matchers = append(matchers, fmt.Sprintf("namespace = %q", cr.Namespace))
	}

	var nestedRoutes []yaml.MapSlice
	for _, nestedRoute := range cfgRoute.Routes {
		// namespace matcher not needed for nested routes
		tmpRoute := vmv1beta1.Route(*nestedRoute)
		route, err := buildRoute(cr, &tmpRoute, false, false, false)
		if err != nil {
			return r, err
		}
		nestedRoutes = append(nestedRoutes, route)
	}
	if len(nestedRoutes) > 0 {
		r = append(r, yaml.MapItem{Key: "routes", Value: nestedRoutes})
	}
	toYaml := func(key string, src []string) {
		if len(src) > 0 {
			r = append(r, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYamlString := func(key string, src string) {
		if len(src) > 0 {
			r = append(r, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYamlTimeIntervals := func(key string, src []string) {
		if len(src) > 0 {
			tis := make([]string, 0, len(src))
			for _, ti := range src {
				tis = append(tis, buildCRPrefixedName(cr, ti))
			}
			r = append(r, yaml.MapItem{Key: key, Value: tis})
		}
	}

	toYaml("matchers", matchers)
	toYaml("group_by", cfgRoute.GroupBy)
	toYamlTimeIntervals("active_time_intervals", cfgRoute.ActiveTimeIntervals)
	toYamlTimeIntervals("mute_time_intervals", cfgRoute.MuteTimeIntervals)

	toYamlString("group_interval", cfgRoute.GroupInterval)
	toYamlString("group_wait", cfgRoute.GroupWait)
	toYamlString("repeat_interval", cfgRoute.RepeatInterval)
	if len(cfgRoute.Receiver) > 0 {
		r = append(r, yaml.MapItem{Key: "receiver", Value: buildCRPrefixedName(cr, cfgRoute.Receiver)})
	}
	r = append(r, yaml.MapItem{Key: "continue", Value: continueSetting})
	return r, nil
}

func buildInhibitRule(namespace string, rule vmv1beta1.InhibitRule, mustAddNamespaceMatcher bool) yaml.MapSlice {
	var r yaml.MapSlice
	if mustAddNamespaceMatcher {
		namespaceMatch := fmt.Sprintf("namespace = %q", namespace)
		rule.SourceMatchers = append(rule.SourceMatchers, namespaceMatch)
		rule.TargetMatchers = append(rule.TargetMatchers, namespaceMatch)
	}
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

func buildCRPrefixedName(cr *vmv1beta1.VMAlertmanagerConfig, name string) string {
	return fmt.Sprintf("%s-%s-%s", cr.Namespace, cr.Name, name)
}

// contains only global configuration param for config validation
type globalAlertmanagerConfig struct {
	Global struct {
		SMTPFrom            string `yaml:"smtp_from,omitempty" json:"smtp_from,omitempty"`
		SMTPSmarthost       string `yaml:"smtp_smarthost,omitempty" json:"smtp_smarthost,omitempty"`
		SlackAPIURL         string `yaml:"slack_api_url,omitempty" json:"slack_api_url,omitempty"`
		SlackAPIURLFile     string `yaml:"slack_api_url_file,omitempty" json:"slack_api_url_file,omitempty"`
		OpsGenieAPIKey      string `yaml:"opsgenie_api_key,omitempty" json:"opsgenie_api_key,omitempty"`
		OpsGenieAPIKeyFile  string `yaml:"opsgenie_api_key_file,omitempty" json:"opsgenie_api_key_file,omitempty"`
		WeChatAPISecret     string `yaml:"wechat_api_secret,omitempty" json:"wechat_api_secret,omitempty"`
		WeChatAPICorpID     string `yaml:"wechat_api_corp_id,omitempty" json:"wechat_api_corp_id,omitempty"`
		VictorOpsAPIKey     string `yaml:"victorops_api_key,omitempty" json:"victorops_api_key,omitempty"`
		VictorOpsAPIKeyFile string `yaml:"victorops_api_key_file,omitempty" json:"victorops_api_key_file,omitempty"`
	} `yaml:"global,omitempty"`
}

type alertmanagerConfig struct {
	Global        interface{}     `yaml:"global,omitempty" json:"global,omitempty"`
	Route         *route          `yaml:"route,omitempty" json:"route,omitempty"`
	InhibitRules  []yaml.MapSlice `yaml:"inhibit_rules,omitempty" json:"inhibit_rules,omitempty"`
	Receivers     []yaml.MapSlice `yaml:"receivers,omitempty" json:"receivers,omitempty"`
	TimeIntervals []yaml.MapSlice `yaml:"time_intervals,omitempty" json:"time_intervals"`
	Templates     []string        `yaml:"templates" json:"templates"`
}

type route struct {
	Receiver            string            `yaml:"receiver,omitempty" json:"receiver,omitempty"`
	GroupByStr          []string          `yaml:"group_by,omitempty" json:"group_by,omitempty"`
	Match               map[string]string `yaml:"match,omitempty" json:"match,omitempty"`
	MatchRE             map[string]string `yaml:"match_re,omitempty" json:"match_re,omitempty"`
	Continue            bool              `yaml:"continue,omitempty" json:"continue,omitempty"`
	Routes              []yaml.MapSlice   `yaml:"routes,omitempty" json:"routes,omitempty"`
	GroupWait           string            `yaml:"group_wait,omitempty" json:"group_wait,omitempty"`
	GroupInterval       string            `yaml:"group_interval,omitempty" json:"group_interval,omitempty"`
	RepeatInterval      string            `yaml:"repeat_interval,omitempty" json:"repeat_interval,omitempty"`
	MuteTimeIntervals   []string          `yaml:"mute_time_intervals,omitempty" json:"mute_time_intervals,omitempty"`
	ActiveTimeIntervals []string          `yaml:"active_time_intervals,omitempty" json:"active_time_intervals,omitempty"`
}

func buildReceiver(
	ctx context.Context,
	rclient client.Client,
	cr *vmv1beta1.VMAlertmanagerConfig,
	receiver vmv1beta1.Receiver,
	globalCfg *globalAlertmanagerConfig,
	cache map[string]*corev1.Secret,
	configmapCache map[string]*corev1.ConfigMap,
	tlsAssets map[string]string,
) (yaml.MapSlice, error) {
	cb := initConfigBuilder(ctx, rclient, cr, receiver.Name, globalCfg, cache, configmapCache, tlsAssets)
	cb.result = yaml.MapSlice{
		{
			Key:   "name",
			Value: buildCRPrefixedName(cr, receiver.Name),
		},
	}
	if err := cb.buildCfg(receiver); err != nil {
		return nil, err
	}
	return cb.result, nil
}

type configBuilder struct {
	build.TLSConfigBuilder
	globalConfig *globalAlertmanagerConfig
	currentYaml  []yaml.MapSlice
	result       yaml.MapSlice
}

func initConfigBuilder(
	ctx context.Context,
	rclient client.Client,
	cr *vmv1beta1.VMAlertmanagerConfig,
	receiver string,
	globalCfg *globalAlertmanagerConfig,
	cache map[string]*corev1.Secret,
	configmapCache map[string]*corev1.ConfigMap,
	tlsAssets map[string]string,
) *configBuilder {
	cb := configBuilder{
		TLSConfigBuilder: build.TLSConfigBuilder{
			Ctx:                ctx,
			Client:             rclient,
			CurrentCRName:      cr.Name,
			CurrentCRNamespace: cr.Namespace,
			SecretCache:        cache,
			ConfigmapCache:     configmapCache,
			TLSAssets:          tlsAssets,
		},
		globalConfig: globalCfg,
		result: yaml.MapSlice{
			{
				Key:   "name",
				Value: buildCRPrefixedName(cr, receiver),
			},
		},
	}

	return &cb
}

func (cb *configBuilder) buildCfg(receiver vmv1beta1.Receiver) error {
	for _, opsGenCfg := range receiver.OpsGenieConfigs {
		if err := cb.buildOpsGenie(opsGenCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("opsgenie_configs")

	for _, emailCfg := range receiver.EmailConfigs {
		if err := cb.buildEmail(emailCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("email_configs")

	for _, slackCfg := range receiver.SlackConfigs {
		if err := cb.buildSlack(slackCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("slack_configs")

	for _, pgCfg := range receiver.PagerDutyConfigs {
		if err := cb.buildPagerDuty(pgCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("pagerduty_configs")

	for _, poCfg := range receiver.PushoverConfigs {
		if err := cb.buildPushOver(poCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("pushover_configs")

	for _, voCfg := range receiver.VictorOpsConfigs {
		if err := cb.buildVictorOps(voCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("victorops_configs")

	for _, wcCfg := range receiver.WeChatConfigs {
		if err := cb.buildWeeChat(wcCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("wechat_configs")

	for _, whCfg := range receiver.WebhookConfigs {
		if err := cb.buildWebhook(whCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("webhook_configs")
	for _, tgCfg := range receiver.TelegramConfigs {
		if err := cb.buildTelegram(tgCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("telegram_configs")
	for _, mcCfg := range receiver.MSTeamsConfigs {
		if err := cb.buildTeams(mcCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("msteams_configs")
	for _, mcCfg := range receiver.DiscordConfigs {
		if err := cb.buildDiscord(mcCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("discord_configs")
	for _, snsCfg := range receiver.SNSConfigs {
		if err := cb.buildSNS(snsCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("sns_configs")
	for _, webexCfg := range receiver.WebexConfigs {
		if err := cb.buildWebex(webexCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("webex_configs")
	return nil
}

func (cb *configBuilder) finalizeSection(name string) {
	if len(cb.currentYaml) > 0 {
		cb.result = append(cb.result, yaml.MapItem{
			Key:   name,
			Value: cb.currentYaml,
		})
		cb.currentYaml = make([]yaml.MapSlice, 0, len(cb.currentYaml))
	}
}

func (cb *configBuilder) buildTeams(ms vmv1beta1.MSTeamsConfig) error {
	var temp yaml.MapSlice
	if ms.HTTPConfig != nil {
		c, err := cb.buildHTTPConfig(ms.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: c})
	}
	if ms.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *ms.SendResolved})
	}

	if ms.URLSecret != nil {
		s, err := cb.fetchSecretValue(ms.URLSecret)
		if err != nil {
			return err
		}
		if err := parseURL(string(s)); err != nil {
			return fmt.Errorf("invalid URL %s in key %s from secret %s: %v", string(s), ms.URLSecret.Key, ms.URLSecret.Name, err)
		}
		temp = append(temp, yaml.MapItem{Key: "webhook_url", Value: string(s)})
	} else if ms.URL != nil {
		temp = append(temp, yaml.MapItem{Key: "webhook_url", Value: ms.URL})
	}
	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYaml("title", ms.Title)
	toYaml("text", ms.Text)

	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildDiscord(dc vmv1beta1.DiscordConfig) error {
	var temp yaml.MapSlice
	if dc.HTTPConfig != nil {
		c, err := cb.buildHTTPConfig(dc.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: c})
	}
	if dc.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *dc.SendResolved})
	}

	if dc.URLSecret != nil {
		s, err := cb.fetchSecretValue(dc.URLSecret)
		if err != nil {
			return err
		}
		if err := parseURL(string(s)); err != nil {
			return fmt.Errorf("invalid URL %s in key %s from secret %s: %v", string(s), dc.URLSecret.Key, dc.URLSecret.Name, err)
		}
		temp = append(temp, yaml.MapItem{Key: "webhook_url", Value: string(s)})
	} else if dc.URL != nil {
		temp = append(temp, yaml.MapItem{Key: "webhook_url", Value: dc.URL})
	}
	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYaml("title", dc.Title)
	toYaml("message", dc.Message)

	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildSNS(sns vmv1beta1.SnsConfig) error {
	var temp yaml.MapSlice
	if sns.HTTPConfig != nil {
		c, err := cb.buildHTTPConfig(sns.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: c})
	}
	if sns.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *sns.SendResolved})
	}
	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYaml("api_url", sns.URL)
	toYaml("topic_arn", sns.TopicArn)
	toYaml("subject", sns.Subject)
	toYaml("phone_number", sns.PhoneNumber)
	toYaml("target_arn", sns.TargetArn)
	toYaml("message", sns.Message)
	if len(sns.Attributes) > 0 {
		var attributes yaml.MapSlice
		for k, v := range sns.Attributes {
			attributes = append(attributes, yaml.MapItem{Key: k, Value: v})
		}
		temp = append(temp, yaml.MapItem{Key: "attributes", Value: attributes})
	}
	if sns.Sigv4 != nil {
		var sigv4 yaml.MapSlice
		toYamlSig := func(key string, src string) {
			if len(src) > 0 {
				sigv4 = append(sigv4, yaml.MapItem{Key: key, Value: src})
			}
		}
		toYamlSig("region", sns.Sigv4.Region)
		toYamlSig("profile", sns.Sigv4.Profile)
		toYamlSig("role_arn", sns.Sigv4.RoleArn)
		if sns.Sigv4.AccessKey != "" {
			toYamlSig("access_key", sns.Sigv4.AccessKey)
		} else if sns.Sigv4.AccessKeySelector != nil {
			s, err := cb.fetchSecretValue(sns.Sigv4.AccessKeySelector)
			if err != nil {
				return err
			}
			toYamlSig("access_key", string(s))
		}
		if sns.Sigv4.SecretKey != nil {
			s, err := cb.fetchSecretValue(sns.Sigv4.SecretKey)
			if err != nil {
				return err
			}
			toYamlSig("secret_key", string(s))
		}
		temp = append(temp, yaml.MapItem{Key: "sigv4", Value: sigv4})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildWebex(web vmv1beta1.WebexConfig) error {
	var temp yaml.MapSlice
	if web.HTTPConfig != nil {
		c, err := cb.buildHTTPConfig(web.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: c})
	}
	if web.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *web.SendResolved})
	}
	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	if web.URL != nil {
		toYaml("api_url", *web.URL)
	}
	toYaml("room_id", web.RoomId)
	toYaml("message", web.Message)
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildTelegram(tg vmv1beta1.TelegramConfig) error {
	var temp yaml.MapSlice

	if tg.HTTPConfig != nil {
		c, err := cb.buildHTTPConfig(tg.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: c})
	}
	if tg.BotToken != nil {
		s, err := cb.fetchSecretValue(tg.BotToken)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "bot_token", Value: string(s)})
	}
	if tg.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *tg.SendResolved})
	}
	if tg.DisableNotifications != nil {
		temp = append(temp, yaml.MapItem{Key: "disable_notifications", Value: *tg.DisableNotifications})
	}
	temp = append(temp, yaml.MapItem{Key: "chat_id", Value: tg.ChatID})

	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYaml("api_url", tg.APIUrl)
	toYaml("message", tg.Message)
	toYaml("parse_mode", tg.ParseMode)

	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildSlack(slack vmv1beta1.SlackConfig) error {
	var temp yaml.MapSlice
	if slack.HTTPConfig != nil {
		c, err := cb.buildHTTPConfig(slack.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: c})
	}
	if slack.APIURL == nil && cb.globalConfig.Global.SlackAPIURL == "" && cb.globalConfig.Global.SlackAPIURLFile == "" {
		return fmt.Errorf("api_url secret is not defined and no global Slack API URL set either inline or in a file")
	}
	if slack.APIURL != nil {
		s, err := cb.fetchSecretValue(slack.APIURL)
		if err != nil {
			return err
		}
		if err := parseURL(string(s)); err != nil {
			return fmt.Errorf("invalid URL %s in key %s from secret %s: %v", string(s), slack.APIURL.Key, slack.APIURL.Name, err)
		}
		temp = append(temp, yaml.MapItem{Key: "api_url", Value: string(s)})
	}
	if slack.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *slack.SendResolved})
	}
	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYaml("username", slack.Username)
	toYaml("channel", slack.Channel)
	toYaml("color", slack.Color)
	toYaml("fallback", slack.Fallback)
	toYaml("footer", slack.Footer)
	toYaml("icon_emoji", slack.IconEmoji)
	toYaml("icon_url", slack.IconURL)
	toYaml("image_url", slack.ImageURL)
	toYaml("pretext", slack.Pretext)
	toYaml("text", slack.Text)
	toYaml("title", slack.Title)
	toYaml("title_link", slack.TitleLink)
	toYaml("thumb_url", slack.ThumbURL)
	toYaml("callback_id", slack.CallbackID)
	if slack.LinkNames {
		temp = append(temp, yaml.MapItem{Key: "link_names", Value: slack.LinkNames})
	}
	if slack.ShortFields {
		temp = append(temp, yaml.MapItem{Key: "short_fields", Value: slack.ShortFields})
	}
	if len(slack.MrkdwnIn) > 0 {
		temp = append(temp, yaml.MapItem{Key: "mrkdwn_in", Value: slack.MrkdwnIn})
	}
	var actions []yaml.MapSlice
	for _, action := range slack.Actions {
		var actionYAML yaml.MapSlice
		toActionYAML := func(key, src string) {
			if len(src) > 0 {
				actionYAML = append(actionYAML, yaml.MapItem{Key: key, Value: src})
			}
		}
		toActionYAML("name", action.Name)
		toActionYAML("value", action.Value)
		toActionYAML("text", action.Text)
		toActionYAML("url", action.URL)
		toActionYAML("type", action.Type)
		toActionYAML("style", action.Style)

		if action.ConfirmField != nil {
			var confirmYAML yaml.MapSlice
			toConfirm := func(key, src string) {
				if len(src) > 0 {
					confirmYAML = append(confirmYAML, yaml.MapItem{Key: key, Value: src})
				}
			}
			toConfirm("text", action.ConfirmField.Text)
			toConfirm("ok_text", action.ConfirmField.OkText)
			toConfirm("dismiss_text", action.ConfirmField.DismissText)
			toConfirm("title", action.ConfirmField.Title)
			actionYAML = append(actionYAML, yaml.MapItem{Key: "confirm", Value: confirmYAML})
		}
		actions = append(actions, actionYAML)
	}
	if len(actions) > 0 {
		temp = append(temp, yaml.MapItem{Key: "actions", Value: actions})
	}
	var fields []yaml.MapSlice
	for _, field := range slack.Fields {
		var fieldYAML yaml.MapSlice
		if len(field.Value) > 0 {
			fieldYAML = append(fieldYAML, yaml.MapItem{Key: "value", Value: field.Value})
		}
		if len(field.Title) > 0 {
			fieldYAML = append(fieldYAML, yaml.MapItem{Key: "title", Value: field.Title})
		}

		if field.Short != nil {
			fieldYAML = append(fieldYAML, yaml.MapItem{Key: "short", Value: *field.Short})
		}
		fields = append(fields, fieldYAML)
	}
	if len(fields) > 0 {
		temp = append(temp, yaml.MapItem{Key: "fields", Value: fields})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildWebhook(wh vmv1beta1.WebhookConfig) error {
	var temp yaml.MapSlice
	if wh.HTTPConfig != nil {
		h, err := cb.buildHTTPConfig(wh.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: h})
	}
	if wh.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *wh.SendResolved})
	}
	var url string

	if wh.URL != nil {
		url = *wh.URL
	} else if wh.URLSecret != nil {
		s, err := cb.fetchSecretValue(wh.URLSecret)
		if err != nil {
			return err
		}
		url = string(s)
	}

	// no point to add config without url
	if url == "" {
		return nil
	}
	if err := parseURL(url); err != nil {
		return fmt.Errorf("failed to parse webhook url: %w", err)
	}

	temp = append(temp, yaml.MapItem{Key: "url", Value: url})
	if wh.MaxAlerts != 0 {
		temp = append(temp, yaml.MapItem{Key: "max_alerts", Value: wh.MaxAlerts})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildWeeChat(wc vmv1beta1.WeChatConfig) error {
	if wc.APISecret == nil && cb.globalConfig.Global.WeChatAPISecret == "" {
		return fmt.Errorf("api_secret is not set and no global Wechat ApiSecret set")
	}
	if wc.CorpID == "" && cb.globalConfig.Global.WeChatAPICorpID == "" {
		return fmt.Errorf("cord_id is not set and no global Wechat CorpID set")
	}
	var temp yaml.MapSlice
	if wc.HTTPConfig != nil {
		h, err := cb.buildHTTPConfig(wc.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: h})
	}
	if wc.APISecret != nil {
		s, err := cb.fetchSecretValue(wc.APISecret)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "api_secret", Value: string(s)})
	}
	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYaml("message", wc.Message)
	toYaml("message_type", wc.MessageType)
	toYaml("agent_id", wc.AgentID)
	toYaml("corp_id", wc.CorpID)
	toYaml("to_party", wc.ToParty)
	toYaml("to_tag", wc.ToTag)
	toYaml("to_user", wc.ToUser)
	toYaml("api_url", wc.APIURL)
	if wc.APIURL != "" {
		err := parseURL(wc.APIURL)
		if err != nil {
			return err
		}
	}
	if wc.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *wc.SendResolved})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildVictorOps(vo vmv1beta1.VictorOpsConfig) error {
	if vo.APIKey == nil && cb.globalConfig.Global.VictorOpsAPIKey == "" && cb.globalConfig.Global.VictorOpsAPIKeyFile == "" {
		return fmt.Errorf("api_key secret is not set and no global VictorOps API Key set")
	}
	var temp yaml.MapSlice
	if vo.HTTPConfig != nil {
		h, err := cb.buildHTTPConfig(vo.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: h})
	}
	if vo.APIKey != nil {
		s, err := cb.fetchSecretValue(vo.APIKey)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "api_key", Value: string(s)})
	}
	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYaml("api_url", vo.APIURL)
	if vo.APIURL != "" {
		err := parseURL(vo.APIURL)
		if err != nil {
			return err
		}
	}
	toYaml("routing_key", vo.RoutingKey)
	toYaml("message_type", vo.MessageType)
	toYaml("entity_display_name", vo.EntityDisplayName)
	toYaml("state_message", vo.StateMessage)
	toYaml("monitoring_tool", vo.MonitoringTool)
	if vo.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *vo.SendResolved})
	}
	if len(vo.CustomFields) > 0 {
		var cfs yaml.MapSlice
		var customFieldIDs []string
		for customFieldKey := range vo.CustomFields {
			customFieldIDs = append(customFieldIDs, customFieldKey)
		}
		sort.Strings(customFieldIDs)
		for _, customFieldKey := range customFieldIDs {
			value := vo.CustomFields[customFieldKey]
			cfs = append(cfs, yaml.MapItem{Key: customFieldKey, Value: value})
		}
		temp = append(temp, yaml.MapItem{Key: "custom_fields", Value: cfs})
	}

	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildPushOver(po vmv1beta1.PushoverConfig) error {
	var temp yaml.MapSlice
	toYaml := func(key, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	if po.HTTPConfig != nil {
		h, err := cb.buildHTTPConfig(po.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: h})
	}
	if po.UserKey != nil {
		s, err := cb.fetchSecretValue(po.UserKey)
		if err != nil {
			return err
		}
		toYaml("user_key", string(s))
	}
	if po.Token != nil {
		s, err := cb.fetchSecretValue(po.Token)
		if err != nil {
			return err
		}
		toYaml("token", string(s))
	}

	toYaml("url", po.URL)
	toYaml("sound", po.Sound)
	toYaml("priority", po.Priority)
	toYaml("message", po.Message)
	toYaml("expire", po.Expire)
	toYaml("retry", po.Retry)
	toYaml("title", po.Title)
	toYaml("url_title", po.URLTitle)
	temp = append(temp, yaml.MapItem{Key: "html", Value: po.HTML})
	if po.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *po.SendResolved})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildPagerDuty(pd vmv1beta1.PagerDutyConfig) error {
	var temp yaml.MapSlice
	toYaml := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	if pd.HTTPConfig != nil {
		h, err := cb.buildHTTPConfig(pd.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: h})
	}
	if pd.RoutingKey != nil {
		s, err := cb.fetchSecretValue(pd.RoutingKey)
		if err != nil {
			return err
		}
		toYaml("routing_key", string(s))
	}
	if pd.ServiceKey != nil {
		s, err := cb.fetchSecretValue(pd.ServiceKey)
		if err != nil {
			return err
		}
		toYaml("service_key", string(s))
	}
	toYaml("url", pd.URL)
	if pd.URL != "" {
		err := parseURL(pd.URL)
		if err != nil {
			return err
		}
	}
	toYaml("description", pd.Description)
	toYaml("client_url", pd.ClientURL)
	toYaml("client", pd.Client)
	toYaml("class", pd.Class)
	toYaml("component", pd.Component)
	toYaml("group", pd.Group)
	toYaml("severity", pd.Severity)
	var images []yaml.MapSlice
	for _, image := range pd.Images {
		var imageYAML yaml.MapSlice
		if len(image.Href) > 0 {
			imageYAML = append(imageYAML, yaml.MapItem{Key: "href", Value: image.Href})
		}
		if len(image.Source) > 0 {
			imageYAML = append(imageYAML, yaml.MapItem{Key: "source", Value: image.Source})
		}
		if len(image.Alt) > 0 {
			imageYAML = append(imageYAML, yaml.MapItem{Key: "alt", Value: image.Alt})
		}
		images = append(images, imageYAML)
	}
	if len(images) > 0 {
		temp = append(temp, yaml.MapItem{Key: "images", Value: images})
	}
	var links []yaml.MapSlice
	for _, link := range pd.Links {
		var linkYAML yaml.MapSlice
		if len(link.Href) > 0 {
			linkYAML = append(linkYAML, yaml.MapItem{Key: "href", Value: link.Href})
		}
		if len(link.Text) > 0 {
			linkYAML = append(linkYAML, yaml.MapItem{Key: "text", Value: link.Text})
		}
		links = append(links, linkYAML)
	}
	if len(links) > 0 {
		temp = append(temp, yaml.MapItem{Key: "links", Value: links})
	}
	detailKeys := make([]string, 0, len(pd.Details))
	for detailKey, value := range pd.Details {
		if len(value) == 0 {
			continue
		}
		detailKeys = append(detailKeys, detailKey)
	}
	sort.Strings(detailKeys)
	if len(detailKeys) > 0 {
		var detailsYaml yaml.MapSlice
		for _, detailKey := range detailKeys {
			detailsYaml = append(detailsYaml, yaml.MapItem{Key: detailKey, Value: pd.Details[detailKey]})
		}
		temp = append(temp, yaml.MapItem{Key: "details", Value: detailsYaml})
	}
	if pd.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *pd.SendResolved})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildEmail(email vmv1beta1.EmailConfig) error {
	var temp yaml.MapSlice
	if email.RequireTLS != nil {
		temp = append(temp, yaml.MapItem{Key: "require_tls", Value: *email.RequireTLS})
	}
	if email.Smarthost == "" && cb.globalConfig.Global.SMTPSmarthost == "" {
		return fmt.Errorf("required email smarthost is not set at local and global alertmanager config")
	}
	if email.From == "" && cb.globalConfig.Global.SMTPFrom == "" {
		return fmt.Errorf("required email from is not set at local and global alertmanager config")
	}

	// skip tls_config if require_tls is false
	if email.RequireTLS != nil && *email.RequireTLS && email.TLSConfig != nil {
		s, err := cb.BuildTLSConfig(email.TLSConfig, tlsAssetsDir)
		if err != nil {
			return err
		}
		if len(s) > 0 {
			temp = append(temp, yaml.MapItem{Key: "tls_config", Value: s})
		}
	}

	if email.AuthPassword != nil {
		p, err := cb.fetchSecretValue(email.AuthPassword)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "auth_password", Value: string(p)})
	}
	if email.AuthSecret != nil {
		s, err := cb.fetchSecretValue(email.AuthSecret)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "auth_secret", Value: string(s)})
	}
	if len(email.Headers) > 0 {
		temp = append(temp, yaml.MapItem{Key: "headers", Value: email.Headers})
	}
	toYamlString := func(key string, src string) {
		if len(src) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: src})
		}
	}
	toYamlString("from", email.From)
	toYamlString("text", email.Text)
	toYamlString("to", email.To)
	toYamlString("html", email.HTML)
	toYamlString("auth_identity", email.AuthIdentity)
	toYamlString("auth_username", email.AuthUsername)
	toYamlString("hello", email.Hello)
	toYamlString("smarthost", email.Smarthost)
	if email.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *email.SendResolved})
	}

	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildOpsGenie(og vmv1beta1.OpsGenieConfig) error {
	if og.APIKey == nil && cb.globalConfig.Global.OpsGenieAPIKey == "" && cb.globalConfig.Global.OpsGenieAPIKeyFile == "" {
		return fmt.Errorf("api_key secret is not defined and no global OpsGenie API Key set either inline or in a file")
	}
	var temp yaml.MapSlice
	if og.APIKey != nil {
		s, err := cb.fetchSecretValue(og.APIKey)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "api_key", Value: string(s)})
	}
	toYamlString := func(key string, value string) {
		if len(value) > 0 {
			temp = append(temp, yaml.MapItem{Key: key, Value: value})
		}
	}
	toYamlString("source", og.Source)
	toYamlString("description", og.Description)
	toYamlString("message", og.Message)
	toYamlString("tags", og.Tags)
	toYamlString("note", og.Note)
	toYamlString("api_url", og.APIURL)
	toYamlString("entity", og.Entity)
	toYamlString("Actions", og.Actions)

	if og.APIURL != "" {
		err := parseURL(og.APIURL)
		if err != nil {
			return err
		}
	}
	toYamlString("priority", og.Priority)
	if og.Details != nil {
		temp = append(temp, yaml.MapItem{Key: "details", Value: og.Details})
	}
	if og.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *og.SendResolved})
	}
	if og.UpdateAlerts {
		temp = append(temp, yaml.MapItem{Key: "update_alerts", Value: og.UpdateAlerts})
	}

	var responders []yaml.MapSlice
	for _, responder := range og.Responders {
		var responderYAML yaml.MapSlice
		toResponderYAML := func(key, src string) {
			if len(src) > 0 {
				responderYAML = append(responderYAML, yaml.MapItem{Key: key, Value: src})
			}
		}
		toResponderYAML("name", responder.Name)
		toResponderYAML("username", responder.Username)
		toResponderYAML("id", responder.ID)
		toResponderYAML("type", responder.Type)

		responders = append(responders, responderYAML)
	}
	if len(responders) > 0 {
		temp = append(temp, yaml.MapItem{Key: "responders", Value: responders})
	}

	if og.HTTPConfig != nil {
		yamlHTTP, err := cb.buildHTTPConfig(og.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: yamlHTTP})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) fetchSecretValue(selector *corev1.SecretKeySelector) ([]byte, error) {
	return fetchSecretValue(cb.Ctx, cb.Client, cb.CurrentCRNamespace, selector, cb.SecretCache)
}

func (cb *configBuilder) buildHTTPConfig(httpCfg *vmv1beta1.HTTPConfig) (yaml.MapSlice, error) {
	var r yaml.MapSlice

	if httpCfg == nil {
		return nil, nil
	}
	if httpCfg.TLSConfig != nil {
		tls, err := cb.BuildTLSConfig(httpCfg.TLSConfig, tlsAssetsDir)
		if err != nil {
			return nil, err
		}
		if len(tls) > 0 {
			r = append(r, yaml.MapItem{Key: "tls_config", Value: tls})
		}
	}
	if httpCfg.Authorization != nil {
		au, err := cb.buildAuthorization(httpCfg.Authorization)
		if err != nil {
			return nil, err
		}
		r = append(r, yaml.MapItem{Key: "authorization", Value: au})
	}
	if httpCfg.BasicAuth != nil {
		ba, err := cb.buildBasicAuth(httpCfg.BasicAuth)
		if err != nil {
			return nil, err
		}
		r = append(r, yaml.MapItem{Key: "basic_auth", Value: ba})
	}
	var tokenAuth yaml.MapSlice
	if httpCfg.BearerTokenSecret != nil {
		bearer, err := cb.fetchSecretValue(httpCfg.BearerTokenSecret)
		if err != nil {
			return nil, fmt.Errorf("cannot find secret for bearerToken: %w", err)
		}
		tokenAuth = append(tokenAuth, yaml.MapItem{Key: "credentials", Value: string(bearer)})
	}
	if len(httpCfg.BearerTokenFile) > 0 {
		tokenAuth = append(tokenAuth, yaml.MapItem{Key: "credentials_file", Value: httpCfg.BearerTokenFile})
	}
	if len(tokenAuth) > 0 {
		r = append(r, yaml.MapItem{Key: "authorization", Value: tokenAuth})
	}

	if httpCfg.OAuth2 != nil {
		oauth2, err := cb.buildOAuth2(httpCfg.OAuth2)
		if err != nil {
			return nil, fmt.Errorf("cannot build oauth2 configuration: %w", err)
		}
		r = append(r, yaml.MapItem{Key: "oauth2", Value: oauth2})
	}

	if len(httpCfg.ProxyURL) > 0 {
		r = append(r, yaml.MapItem{Key: "proxy_url", Value: httpCfg.ProxyURL})
	}
	return r, nil
}

func (cb *configBuilder) buildOAuth2(oauth2 *vmv1beta1.OAuth2) (yaml.MapSlice, error) {
	var r yaml.MapSlice

	if oauth2.ClientSecret != nil {
		p, err := cb.fetchSecretValue(oauth2.ClientSecret)
		if err != nil {
			return nil, err
		}
		r = append(r, yaml.MapItem{
			Key:   "client_secret",
			Value: string(p),
		})
	}

	switch {
	case oauth2.ClientID.ConfigMap != nil:
		var cm corev1.ConfigMap
		if err := cb.Get(cb.Ctx, types.NamespacedName{Namespace: cb.CurrentCRNamespace, Name: oauth2.ClientID.ConfigMap.Name}, &cm); err != nil {
			return nil, fmt.Errorf("cannot fetch configmap for oauth2.client_id: %w", err)
		}
		p, ok := cm.Data[oauth2.ClientID.ConfigMap.Key]
		if !ok {
			return nil, fmt.Errorf("cannot find expected key=%s at oauth2.client_id configmap=%s", oauth2.ClientID.ConfigMap.Key, oauth2.ClientID.ConfigMap.Name)
		}
		r = append(r, yaml.MapItem{Key: "client_id", Value: p})

	case oauth2.ClientID.Secret != nil:
		p, err := cb.fetchSecretValue(oauth2.ClientID.Secret)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch secret value for oauth2.client_id: %w", err)
		}
		r = append(r, yaml.MapItem{Key: "client_id", Value: string(p)})
	}
	if len(oauth2.EndpointParams) > 0 {
		r = append(r, yaml.MapItem{Key: "endpoint_params", Value: orderedYAMLMAp(oauth2.EndpointParams)})
	}
	if len(oauth2.Scopes) > 0 {
		r = append(r, yaml.MapItem{Key: "scopes", Value: oauth2.Scopes})
	}
	if len(oauth2.ClientSecretFile) > 0 {
		r = append(r, yaml.MapItem{
			Key:   "client_secret_file",
			Value: oauth2.ClientSecretFile,
		})
	}
	if len(oauth2.TokenURL) > 0 {
		r = append(r, yaml.MapItem{Key: "token_url", Value: oauth2.TokenURL})
	}

	return r, nil
}

func (cb *configBuilder) buildBasicAuth(basicAuth *vmv1beta1.BasicAuth) (yaml.MapSlice, error) {
	var r yaml.MapSlice

	if len(basicAuth.Username.Name) > 0 {
		u, err := cb.fetchSecretValue(&basicAuth.Username)
		if err != nil {
			return nil, err
		}
		r = append(r, yaml.MapItem{
			Key:   "username",
			Value: string(u),
		})
	}

	if len(basicAuth.Password.Name) > 0 {
		p, err := cb.fetchSecretValue(&basicAuth.Password)
		if err != nil {
			return nil, err
		}
		r = append(r, yaml.MapItem{
			Key:   "password",
			Value: string(p),
		})
	}

	if len(basicAuth.PasswordFile) > 0 {
		r = append(r, yaml.MapItem{
			Key:   "password_file",
			Value: basicAuth.PasswordFile,
		})
	}

	return r, nil
}

func (cb *configBuilder) buildAuthorization(authCfg *vmv1beta1.Authorization) (yaml.MapSlice, error) {
	var r yaml.MapSlice

	if len(authCfg.Type) > 0 {
		r = append(r, yaml.MapItem{
			Key:   "type",
			Value: authCfg.Type,
		})
	}
	if authCfg.Credentials != nil {
		hv, err := cb.fetchSecretValue(authCfg.Credentials)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch credentials secret value: %w", err)
		}
		r = append(r, yaml.MapItem{
			Key:   "credentials",
			Value: string(hv),
		})
	}
	if len(authCfg.CredentialsFile) > 0 {
		r = append(r, yaml.MapItem{
			Key:   "credentials_file",
			Value: authCfg.CredentialsFile,
		})
	}

	return r, nil
}

func parseURL(s string) error {
	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q for URL", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("missing host for URL")
	}
	return nil
}

func fetchSecretValue(ctx context.Context, rclient client.Client, ns string, selector *corev1.SecretKeySelector, sm map[string]*corev1.Secret) ([]byte, error) {
	var s corev1.Secret
	if existSecret, ok := sm[selector.Name]; ok {
		s = *existSecret
	} else if err := rclient.Get(ctx, types.NamespacedName{Name: selector.Name, Namespace: ns}, &s); err != nil {
		return nil, fmt.Errorf("cannot find secret=%q to fetch content at ns=%q, err: %w", selector.Name, ns, err)
	}
	if v, ok := s.Data[selector.Key]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("secret key=%q not exists at secret=%q", selector.Key, selector.Name)
}

func secretSelectorToAssetKey(selector *corev1.SecretKeySelector) string {
	return fmt.Sprintf("%s_%s", selector.Name, selector.Key)
}

// builds configuration according to https://prometheus.io/docs/alerting/latest/https/#gossip-traffic
func buildGossipConfigYAML(ctx context.Context, rclient client.Client, vmaCR *vmv1beta1.VMAlertmanager, tlsAssets map[string]string) ([]byte, error) {
	if vmaCR.Spec.GossipConfig == nil {
		return nil, nil
	}
	var cfg yaml.MapSlice
	gossipCfg := vmaCR.Spec.GossipConfig
	if gossipCfg.TLSServerConfig != nil {
		var tlsCfg yaml.MapSlice
		secretMap := make(map[string]*corev1.Secret)
		tlsAssetsServerDir := tlsAssetsDir + "/gossip/server/"
		if gossipCfg.TLSServerConfig.ClientCASecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, gossipCfg.TLSServerConfig.ClientCASecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret CA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(gossipCfg.TLSServerConfig.ClientCASecretRef)
			tlsAssets[assetKey] = string(data)
			gossipCfg.TLSServerConfig.ClientCAFile = tlsAssetsServerDir + assetKey
		}
		if gossipCfg.TLSServerConfig.Certs.CertSecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, gossipCfg.TLSServerConfig.Certs.CertSecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret CA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(gossipCfg.TLSServerConfig.Certs.CertSecretRef)
			tlsAssets[assetKey] = string(data)
			gossipCfg.TLSServerConfig.Certs.CertFile = tlsAssetsServerDir + assetKey

		}

		if gossipCfg.TLSServerConfig.Certs.KeySecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, gossipCfg.TLSServerConfig.Certs.KeySecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(gossipCfg.TLSServerConfig.Certs.KeySecretRef)
			tlsAssets[assetKey] = string(data)
			gossipCfg.TLSServerConfig.Certs.KeyFile = tlsAssetsServerDir + assetKey
		}

		if len(gossipCfg.TLSServerConfig.ClientCAFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "client_ca_file", Value: gossipCfg.TLSServerConfig.ClientCAFile})
		}
		if len(gossipCfg.TLSServerConfig.Certs.CertFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "cert_file", Value: gossipCfg.TLSServerConfig.Certs.CertFile})
		}
		if len(gossipCfg.TLSServerConfig.Certs.KeyFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "key_file", Value: gossipCfg.TLSServerConfig.Certs.KeyFile})
		}
		if len(gossipCfg.TLSServerConfig.CipherSuites) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "cipher_suites", Value: gossipCfg.TLSServerConfig.CipherSuites})
		}
		if len(gossipCfg.TLSServerConfig.CurvePreferences) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "curve_preferences", Value: gossipCfg.TLSServerConfig.CurvePreferences})
		}
		if len(gossipCfg.TLSServerConfig.ClientAuthType) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "client_auth_type", Value: gossipCfg.TLSServerConfig.ClientAuthType})
		}
		if gossipCfg.TLSServerConfig.PreferServerCipherSuites {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "prefer_server_cipher_suites", Value: gossipCfg.TLSServerConfig.PreferServerCipherSuites})
		}
		if len(gossipCfg.TLSServerConfig.MaxVersion) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "max_version", Value: gossipCfg.TLSServerConfig.MaxVersion})
		}
		if len(gossipCfg.TLSServerConfig.MinVersion) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "min_version", Value: gossipCfg.TLSServerConfig.MinVersion})
		}

		cfg = append(cfg, yaml.MapItem{Key: "tls_server_config", Value: tlsCfg})
	}

	if gossipCfg.TLSClientConfig != nil {
		var tlsCfg yaml.MapSlice
		secretMap := make(map[string]*corev1.Secret)
		tlsAssetsClientDir := tlsAssetsDir + "/gossip/client/"
		if gossipCfg.TLSClientConfig.CASecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, gossipCfg.TLSClientConfig.CASecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(gossipCfg.TLSClientConfig.CASecretRef)
			tlsAssets[assetKey] = string(data)
			gossipCfg.TLSClientConfig.CAFile = tlsAssetsClientDir + assetKey
		}
		if gossipCfg.TLSClientConfig.Certs.CertSecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, gossipCfg.TLSClientConfig.Certs.CertSecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(gossipCfg.TLSClientConfig.Certs.CertSecretRef)
			tlsAssets[assetKey] = string(data)
			gossipCfg.TLSClientConfig.Certs.CertFile = tlsAssetsClientDir + assetKey

		}

		if gossipCfg.TLSClientConfig.Certs.KeySecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, gossipCfg.TLSClientConfig.Certs.KeySecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(gossipCfg.TLSClientConfig.Certs.KeySecretRef)
			tlsAssets[assetKey] = string(data)
			gossipCfg.TLSClientConfig.Certs.KeyFile = tlsAssetsClientDir + assetKey
		}

		if len(gossipCfg.TLSClientConfig.CAFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "ca_file", Value: gossipCfg.TLSClientConfig.CAFile})
		}
		if len(gossipCfg.TLSClientConfig.Certs.CertFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "cert_file", Value: gossipCfg.TLSClientConfig.Certs.CertFile})
		}
		if len(gossipCfg.TLSClientConfig.Certs.KeyFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "key_file", Value: gossipCfg.TLSClientConfig.Certs.KeyFile})
		}
		if gossipCfg.TLSClientConfig.InsecureSkipVerify {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "insecure_skip_verify", Value: gossipCfg.TLSClientConfig.InsecureSkipVerify})
		}
		if len(gossipCfg.TLSClientConfig.ServerName) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "server_name", Value: gossipCfg.TLSClientConfig.ServerName})
		}

		cfg = append(cfg, yaml.MapItem{Key: "tls_client_config", Value: tlsCfg})
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize alertmanager gossip config as yaml: %w", err)
	}
	return data, nil
}

// builds configuration according to https://prometheus.io/docs/alerting/latest/https/#http-traffic
func buildWebServerConfigYAML(ctx context.Context, rclient client.Client, vmaCR *vmv1beta1.VMAlertmanager, tlsAssets map[string]string) ([]byte, error) {
	if vmaCR.Spec.WebConfig == nil {
		return nil, nil
	}
	var cfg yaml.MapSlice
	webCfg := vmaCR.Spec.WebConfig
	if webCfg.HTTPServerConfig != nil {
		var wCfg yaml.MapSlice
		if webCfg.HTTPServerConfig.HTTP2 {
			if webCfg.TLSServerConfig == nil {
				return nil, fmt.Errorf("with enabled http2, tls_server_config is required to be set")
			}
			wCfg = append(wCfg, yaml.MapItem{Key: "http2", Value: webCfg.HTTPServerConfig.HTTP2})
		}
		if len(webCfg.HTTPServerConfig.Headers) > 0 {
			wCfg = append(wCfg, yaml.MapItem{Key: "headers", Value: orderedYAMLMAp(webCfg.HTTPServerConfig.Headers)})
		}
		cfg = append(cfg, yaml.MapItem{Key: "http_server_config", Value: wCfg})
	}
	if webCfg.TLSServerConfig != nil {
		var tlsCfg yaml.MapSlice
		secretMap := make(map[string]*corev1.Secret)
		tlsAssetsServerDir := tlsAssetsDir + "/web/server/"
		if webCfg.TLSServerConfig.ClientCASecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, webCfg.TLSServerConfig.ClientCASecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret CA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(webCfg.TLSServerConfig.ClientCASecretRef)
			tlsAssets[assetKey] = string(data)
			webCfg.TLSServerConfig.ClientCAFile = tlsAssetsServerDir + assetKey
		}
		if webCfg.TLSServerConfig.Certs.CertSecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, webCfg.TLSServerConfig.Certs.CertSecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret CA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(webCfg.TLSServerConfig.Certs.CertSecretRef)
			tlsAssets[assetKey] = string(data)
			webCfg.TLSServerConfig.Certs.CertFile = tlsAssetsServerDir + assetKey

		}

		if webCfg.TLSServerConfig.Certs.KeySecretRef != nil {
			data, err := fetchSecretValue(ctx, rclient, vmaCR.Namespace, webCfg.TLSServerConfig.Certs.KeySecretRef, secretMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			assetKey := secretSelectorToAssetKey(webCfg.TLSServerConfig.Certs.KeySecretRef)
			tlsAssets[assetKey] = string(data)
			webCfg.TLSServerConfig.Certs.KeyFile = tlsAssetsServerDir + assetKey
		}

		if len(webCfg.TLSServerConfig.ClientCAFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "client_ca_file", Value: webCfg.TLSServerConfig.ClientCAFile})
		}
		if len(webCfg.TLSServerConfig.Certs.CertFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "cert_file", Value: webCfg.TLSServerConfig.Certs.CertFile})
		}
		if len(webCfg.TLSServerConfig.Certs.KeyFile) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "key_file", Value: webCfg.TLSServerConfig.Certs.KeyFile})
		}
		if len(webCfg.TLSServerConfig.CipherSuites) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "cipher_suites", Value: webCfg.TLSServerConfig.CipherSuites})
		}
		if len(webCfg.TLSServerConfig.CurvePreferences) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "curve_preferences", Value: webCfg.TLSServerConfig.CurvePreferences})
		}
		if len(webCfg.TLSServerConfig.ClientAuthType) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "client_auth_type", Value: webCfg.TLSServerConfig.ClientAuthType})
		}
		if webCfg.TLSServerConfig.PreferServerCipherSuites {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "prefer_server_cipher_suites", Value: webCfg.TLSServerConfig.PreferServerCipherSuites})
		}
		if len(webCfg.TLSServerConfig.MaxVersion) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "max_version", Value: webCfg.TLSServerConfig.MaxVersion})
		}
		if len(webCfg.TLSServerConfig.MinVersion) > 0 {
			tlsCfg = append(tlsCfg, yaml.MapItem{Key: "min_version", Value: webCfg.TLSServerConfig.MinVersion})
		}

		cfg = append(cfg, yaml.MapItem{Key: "tls_server_config", Value: tlsCfg})
	}

	if len(webCfg.BasicAuthUsers) > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "basic_auth_users", Value: orderedYAMLMAp(webCfg.BasicAuthUsers)})
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize alertmanager webconfig as yaml: %w", err)
	}
	return data, nil
}

func orderedYAMLMAp(src map[string]string) yaml.MapSlice {
	dstKeys := make([]string, 0, len(src))
	for key := range src {
		dstKeys = append(dstKeys, key)
	}
	sort.Strings(dstKeys)
	var result yaml.MapSlice
	for _, key := range dstKeys {
		result = append(result, yaml.MapItem{Key: key, Value: src[key]})
	}
	return result
}
