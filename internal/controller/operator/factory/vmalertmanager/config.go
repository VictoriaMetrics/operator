package vmalertmanager

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

type extraFields map[string]any

func (f *extraFields) MarshalYAML() (any, error) {
	if f == nil {
		return nil, nil
	}
	vs := *f
	dstKeys := make([]string, 0, len(vs))
	for key := range vs {
		dstKeys = append(dstKeys, key)
	}
	slices.Sort(dstKeys)
	var result yaml.MapSlice
	for _, key := range dstKeys {
		result = append(result, yaml.MapItem{Key: key, Value: vs[key]})
	}
	return result, nil
}

// contains only global configuration param for config validation
type globalConfig struct {
	SMTPFrom              string      `yaml:"smtp_from,omitempty"`
	SMTPSmarthost         string      `yaml:"smtp_smarthost,omitempty"`
	SlackAPIURL           string      `yaml:"slack_api_url,omitempty"`
	SlackAPIURLFile       string      `yaml:"slack_api_url_file,omitempty"`
	OpsGenieAPIKey        string      `yaml:"opsgenie_api_key,omitempty"`
	OpsGenieAPIKeyFile    string      `yaml:"opsgenie_api_key_file,omitempty"`
	WechatAPISecret       string      `yaml:"wechat_api_secret,omitempty"`
	WechatAPICorpID       string      `yaml:"wechat_api_corp_id,omitempty"`
	VictorOpsAPIKey       string      `yaml:"victorops_api_key,omitempty"`
	VictorOpsAPIKeyFile   string      `yaml:"victorops_api_key_file,omitempty"`
	JiraAPIURL            string      `yaml:"jira_api_url,omitempty"`
	RocketchatAPIURL      string      `yaml:"rocketchat_api_url,omitempty"`
	RocketchatToken       string      `yaml:"rocketchat_token,omitempty"`
	RocketchatTokenFile   string      `yaml:"rocketchat_token_file,omitempty"`
	RocketchatTokenID     string      `yaml:"rocketchat_token_id,omitempty"`
	RocketchatTokenIDFile string      `yaml:"rocketchat_token_id_file,omitempty"`
	XXX                   extraFields `yaml:",inline"`
}

type amConfig struct {
	Global        *globalConfig   `yaml:"global,omitempty"`
	Route         *route          `yaml:"route,omitempty"`
	InhibitRules  []yaml.MapSlice `yaml:"inhibit_rules,omitempty"`
	Receivers     []receiver      `yaml:"receivers,omitempty"`
	TimeIntervals []yaml.MapSlice `yaml:"time_intervals,omitempty"`
	Templates     []string        `yaml:"templates"`
	TracingConfig yaml.MapSlice   `yaml:"tracing,omitempty"`
}

type receiver struct {
	Name              string          `yaml:"name"`
	SNSConfigs        []yaml.MapSlice `yaml:"sns_configs,omitempty"`
	WebexConfigs      []yaml.MapSlice `yaml:"webex_configs,omitempty"`
	JiraConfigs       []yaml.MapSlice `yaml:"jira_configs,omitempty"`
	RocketchatConfigs []yaml.MapSlice `yaml:"rocketchat_configs,omitempty"`
	IncidentioConfigs []yaml.MapSlice `yaml:"incidentio_configs,omitempty"`
	MSTeamsV2Configs  []yaml.MapSlice `yaml:"msteamsv2_configs,omitempty"`
	VictorOpsConfigs  []yaml.MapSlice `yaml:"victorops_configs,omitempty"`
	WechatConfigs     []yaml.MapSlice `yaml:"wechat_configs,omitempty"`
	WebhookConfigs    []yaml.MapSlice `yaml:"webhook_configs,omitempty"`
	TelegramConfigs   []yaml.MapSlice `yaml:"telegram_configs,omitempty"`
	MSTeamsConfigs    []yaml.MapSlice `yaml:"msteams_configs,omitempty"`
	DiscordConfigs    []yaml.MapSlice `yaml:"discord_configs,omitempty"`
	OpsGenieConfigs   []yaml.MapSlice `yaml:"opsgenie_configs,omitempty"`
	EmailConfigs      []yaml.MapSlice `yaml:"email_configs,omitempty"`
	SlackConfigs      []yaml.MapSlice `yaml:"slack_configs,omitempty"`
	PagerDutyConfigs  []yaml.MapSlice `yaml:"pagerduty_configs,omitempty"`
	PushoverConfigs   []yaml.MapSlice `yaml:"pushover_configs,omitempty"`
	MattermostConfigs []yaml.MapSlice `yaml:"mattermost_configs,omitempty"`
}

type route struct {
	Receiver            string            `yaml:"receiver,omitempty"`
	GroupByStr          []string          `yaml:"group_by,omitempty"`
	Match               map[string]string `yaml:"match,omitempty"`
	MatchRE             map[string]string `yaml:"match_re,omitempty"`
	Continue            bool              `yaml:"continue,omitempty"`
	Routes              []yaml.MapSlice   `yaml:"routes,omitempty"`
	GroupWait           string            `yaml:"group_wait,omitempty"`
	GroupInterval       string            `yaml:"group_interval,omitempty"`
	RepeatInterval      string            `yaml:"repeat_interval,omitempty"`
	MuteTimeIntervals   []string          `yaml:"mute_time_intervals,omitempty"`
	ActiveTimeIntervals []string          `yaml:"active_time_intervals,omitempty"`
}

type parsedObjects struct {
	configs *build.ChildObjects[*vmv1beta1.VMAlertmanagerConfig]
}

func (pos *parsedObjects) buildConfig(cr *vmv1beta1.VMAlertmanager, data []byte, ac *build.AssetsCache) ([]byte, error) {
	if len(pos.configs.All()) == 0 && cr.Spec.TracingConfig == nil {
		return data, nil
	}
	var baseCfg amConfig
	if err := yaml.Unmarshal(data, &baseCfg); err != nil {
		return nil, fmt.Errorf("cannot parse base cfg: %w", err)
	}

	if baseCfg.Route == nil {
		baseCfg.Route = &route{
			Receiver: "blackhole",
		}
		hasBlackhole := slices.ContainsFunc(baseCfg.Receivers, func(r receiver) bool {
			return r.Name == "blackhole"
		})
		// conditionally add blackhole as default route path
		// alertmanager config must have some default route
		if !hasBlackhole {
			baseCfg.Receivers = append(baseCfg.Receivers, receiver{
				Name: "blackhole",
			})
		}
	}

	if len(pos.configs.All()) > 0 {
		var subRoutes []yaml.MapSlice
		var timeIntervals []yaml.MapSlice
		pos.configs.ForEachCollectSkipInvalid(func(cfg *vmv1beta1.VMAlertmanagerConfig) error {
			if !build.MustSkipRuntimeValidation() {
				if err := cfg.Validate(); err != nil {
					return err
				}
			}
			if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
				if err := cfg.ValidateArbitraryFSAccess(); err != nil {
					return err
				}
			}
			var receiverCfgs []receiver
			for _, r := range cfg.Spec.Receivers {
				result := receiver{
					Name: buildCRPrefixedName(cfg, r.Name),
				}
				if err := buildReceiver(&result, cfg, r, baseCfg.Global, ac); err != nil {
					return err
				}
				receiverCfgs = append(receiverCfgs, result)
			}
			mtis, err := buildGlobalTimeIntervals(cfg)
			if err != nil {
				return err
			}
			if cfg.Spec.Route != nil {
				route, err := buildRoute(cfg, cfg.Spec.Route, true, cr)
				if err != nil {
					return err
				}
				subRoutes = append(subRoutes, route)
			}
			baseCfg.Receivers = append(baseCfg.Receivers, receiverCfgs...)
			for _, rule := range cfg.Spec.InhibitRules {
				baseCfg.InhibitRules = append(baseCfg.InhibitRules, buildInhibitRule(cfg.Namespace, rule, !cr.Spec.DisableNamespaceMatcher))
			}
			if len(mtis) > 0 {
				timeIntervals = append(timeIntervals, mtis...)
			}
			return nil
		})
		if len(subRoutes) > 0 {
			baseCfg.Route.Routes = append(baseCfg.Route.Routes, subRoutes...)
		}
		if len(timeIntervals) > 0 {
			baseCfg.TimeIntervals = append(baseCfg.TimeIntervals, timeIntervals...)
		}
	}
	if len(cr.Spec.Templates) > 0 {
		templatePaths := make([]string, 0, len(cr.Spec.Templates))
		for _, template := range cr.Spec.Templates {
			templatePaths = append(templatePaths, path.Join(templatesDir, template.Name, template.Key))
		}
		addConfigTemplates(&baseCfg, templatePaths)
	}
	if cr.Spec.TracingConfig != nil {
		tracingCfg, err := buildTracingConfig(cr, ac)
		if err != nil {
			return nil, err
		}
		baseCfg.TracingConfig = tracingCfg
	}
	data, err := yaml.Marshal(baseCfg)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// addConfigTemplates adds external templates to the given based configuration
func addConfigTemplates(baseCfg *amConfig, templates []string) {
	if len(templates) == 0 {
		return
	}
	templatesSetByIdx := make(map[string]int)
	for idx, v := range baseCfg.Templates {
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
			baseCfg.Templates[idx] = v
			continue
		}
		baseCfg.Templates = append(baseCfg.Templates, v)
		templatesSetByIdx[v] = len(baseCfg.Templates) - 1
	}
}

type rawValue struct {
	items yaml.MapSlice
}

func (r *rawValue) set(k string, v any) {
	switch item := v.(type) {
	case []string:
		if len(item) > 0 {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	case string:
		if len(item) > 0 {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	case []yaml.MapSlice:
		if len(item) > 0 {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	case int:
		if item > 0 {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	case int32:
		if item > 0 {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	case int64:
		if item > 0 {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	case float64:
		if item > 0 {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	case bool:
		if item {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	case map[string]string:
		if len(item) > 0 {
			r.items = append(r.items, yaml.MapItem{Key: k, Value: item})
		}
	default:
		r.items = append(r.items, yaml.MapItem{Key: k, Value: v})
	}
}

func (r *rawValue) reset() {
	r.items = yaml.MapSlice{}
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
		var tiItem rawValue
		for _, ti := range mti.TimeIntervals {
			tiItem.set("days_of_month", ti.DaysOfMonth)
			tiItem.set("weekdays", ti.Weekdays)
			tiItem.set("months", ti.Months)
			tiItem.set("years", ti.Years)
			tiItem.set("location", ti.Location)
			var trss []yaml.MapSlice
			for _, trs := range ti.Times {
				if trs.EndTime != "" && trs.StartTime != "" {
					trss = append(trss, yaml.MapSlice{{Key: "start_time", Value: trs.StartTime}, {Key: "end_time", Value: trs.EndTime}})
				}
			}
			tiItem.set("times", trss)
			if len(tiItem.items) > 0 {
				temp = append(temp, tiItem.items)
			}
			tiItem.reset()
		}
		if len(temp) > 0 {
			r = append(r, yaml.MapSlice{{Key: "name", Value: buildCRPrefixedName(cr, mti.Name)}, {Key: "time_intervals", Value: temp}})
		}
	}
	return r, nil
}

func buildRoute(cr *vmv1beta1.VMAlertmanagerConfig, cfgRoute *vmv1beta1.Route, topLevel bool, alertmanagerCR *vmv1beta1.VMAlertmanager) (yaml.MapSlice, error) {
	var r rawValue
	matchers := cfgRoute.Matchers
	// enforce continue when route is first-level and vmalertmanager disableRouteContinueEnforce filed is not set,
	// otherwise, always inherit from VMAlertmanagerConfig
	continueSetting := cfgRoute.Continue
	if topLevel {
		if !alertmanagerCR.Spec.DisableRouteContinueEnforce {
			continueSetting = true
		}
		if !alertmanagerCR.Spec.DisableNamespaceMatcher {
			if alertmanagerCR.Spec.EnforcedNamespaceLabel != "" {
				matchers = append(matchers, fmt.Sprintf("%s = %q", alertmanagerCR.Spec.EnforcedNamespaceLabel, cr.Namespace))
			} else {
				matchers = append(matchers, fmt.Sprintf("namespace = %q", cr.Namespace))
			}
		}
		if len(alertmanagerCR.Spec.EnforcedTopRouteMatchers) > 0 {
			matchers = append(matchers, alertmanagerCR.Spec.EnforcedTopRouteMatchers...)
		}
	}

	var nestedRoutes []yaml.MapSlice
	for _, nestedRoute := range cfgRoute.Routes {
		// namespace matcher not needed for nested routes
		tmpRoute := vmv1beta1.Route(*nestedRoute)
		route, err := buildRoute(cr, &tmpRoute, false, alertmanagerCR)
		if err != nil {
			return r.items, err
		}
		nestedRoutes = append(nestedRoutes, route)
	}
	addPrefix := func(items []string) []string {
		vs := make([]string, len(items))
		for i := range items {
			vs[i] = buildCRPrefixedName(cr, items[i])
		}
		return vs
	}
	r.set("routes", nestedRoutes)
	r.set("matchers", matchers)
	r.set("group_by", cfgRoute.GroupBy)
	r.set("active_time_intervals", addPrefix(cfgRoute.ActiveTimeIntervals))
	r.set("mute_time_intervals", addPrefix(cfgRoute.MuteTimeIntervals))
	r.set("group_interval", cfgRoute.GroupInterval)
	r.set("group_wait", cfgRoute.GroupWait)
	r.set("repeat_interval", cfgRoute.RepeatInterval)
	r.set("receiver", buildCRPrefixedName(cr, cfgRoute.Receiver))
	r.set("continue", continueSetting)
	return r.items, nil
}

func buildInhibitRule(ns string, rule vmv1beta1.InhibitRule, mustAddNamespaceMatcher bool) yaml.MapSlice {
	var r rawValue
	if mustAddNamespaceMatcher {
		namespaceMatch := fmt.Sprintf("namespace = %q", ns)
		rule.SourceMatchers = append(rule.SourceMatchers, namespaceMatch)
		rule.TargetMatchers = append(rule.TargetMatchers, namespaceMatch)
	}
	r.set("target_matchers", rule.TargetMatchers)
	r.set("source_matchers", rule.SourceMatchers)
	r.set("equal", rule.Equal)
	return r.items
}

func buildCRPrefixedName(cr *vmv1beta1.VMAlertmanagerConfig, name string) string {
	return fmt.Sprintf("%s-%s-%s", cr.Namespace, cr.Name, name)
}

func buildReceiver(
	result *receiver,
	cr *vmv1beta1.VMAlertmanagerConfig,
	r vmv1beta1.Receiver,
	gc *globalConfig,
	ac *build.AssetsCache,
) error {
	for _, c := range r.OpsGenieConfigs {
		o, err := buildOpsGenie(gc, c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.OpsGenieConfigs = append(result.OpsGenieConfigs, o)
	}
	for _, c := range r.EmailConfigs {
		o, err := buildEmail(gc, c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.EmailConfigs = append(result.EmailConfigs, o)
	}
	for _, c := range r.SlackConfigs {
		o, err := buildSlack(gc, c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.SlackConfigs = append(result.SlackConfigs, o)
	}
	for _, c := range r.PagerDutyConfigs {
		o, err := buildPagerDuty(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.PagerDutyConfigs = append(result.PagerDutyConfigs, o)
	}
	for _, c := range r.PushoverConfigs {
		o, err := buildPushover(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.PushoverConfigs = append(result.PushoverConfigs, o)
	}
	for _, c := range r.VictorOpsConfigs {
		o, err := buildVictorOps(gc, c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.VictorOpsConfigs = append(result.VictorOpsConfigs, o)
	}
	for _, c := range r.WechatConfigs {
		o, err := buildWechat(gc, c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.WechatConfigs = append(result.WechatConfigs, o)
	}
	for _, c := range r.MattermostConfigs {
		o, err := buildMattermost(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.MattermostConfigs = append(result.MattermostConfigs, o)
	}
	for _, c := range r.WebhookConfigs {
		o, err := buildWebhook(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.WebhookConfigs = append(result.WebhookConfigs, o)
	}
	for _, c := range r.TelegramConfigs {
		o, err := buildTelegram(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.TelegramConfigs = append(result.TelegramConfigs, o)
	}
	for _, c := range r.MSTeamsConfigs {
		o, err := buildMSTeams(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.MSTeamsConfigs = append(result.MSTeamsConfigs, o)
	}
	for _, c := range r.DiscordConfigs {
		o, err := buildDiscord(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.DiscordConfigs = append(result.DiscordConfigs, o)
	}
	for _, c := range r.SNSConfigs {
		o, err := buildSNS(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.SNSConfigs = append(result.SNSConfigs, o)
	}
	for _, c := range r.WebexConfigs {
		o, err := buildWebex(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.WebexConfigs = append(result.WebexConfigs, o)
	}
	for _, c := range r.JiraConfigs {
		o, err := buildJira(gc, c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.JiraConfigs = append(result.JiraConfigs, o)
	}
	for _, c := range r.RocketchatConfigs {
		o, err := buildRocketchat(gc, c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.RocketchatConfigs = append(result.RocketchatConfigs, o)
	}
	for _, c := range r.IncidentioConfigs {
		o, err := buildIncidentio(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.IncidentioConfigs = append(result.IncidentioConfigs, o)
	}
	for _, c := range r.MSTeamsV2Configs {
		o, err := buildMSTeamsV2(c, cr.Namespace, ac)
		if err != nil {
			return err
		}
		result.MSTeamsV2Configs = append(result.MSTeamsV2Configs, o)
	}
	return nil
}

func buildMSTeams(rc vmv1beta1.MSTeamsConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	if rc.URLSecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.URLSecret)
		if err != nil {
			return nil, err
		}
		if err := parseURL(secret); err != nil {
			return nil, fmt.Errorf("invalid URL %s in key %s from secret %s: %w", secret, rc.URLSecret.Key, rc.URLSecret.Name, err)
		}
		r.set("webhook_url", secret)
	} else if rc.URL != nil {
		r.set("webhook_url", rc.URL)
	}
	r.set("title", rc.Title)
	r.set("text", rc.Text)
	return r.items, nil
}

func buildMattermost(rc vmv1beta1.MattermostConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	r.set("username", rc.Username)
	r.set("channel", rc.Channel)
	r.set("text", rc.Text)
	r.set("icon_url", rc.IconURL)
	r.set("icon_emoji", rc.IconEmoji)
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	var u string
	if rc.URLSecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.URLSecret)
		if err != nil {
			return nil, err
		}
		u = secret
	} else if rc.URL != nil {
		u = *rc.URL
	}
	if len(u) > 0 {
		if err := parseURL(u); err != nil {
			return nil, fmt.Errorf("invalid URL %s: %w", u, err)
		}
		r.set("webhook_url", u)
	}
	var attachments []yaml.MapSlice
	for _, attachment := range rc.Attachments {
		var ar rawValue
		ar.set("fallback", attachment.Fallback)
		ar.set("color", attachment.Color)
		ar.set("pretext", attachment.Pretext)
		ar.set("text", attachment.Text)
		ar.set("author_name", attachment.AuthorName)
		ar.set("author_link", attachment.AuthorLink)
		ar.set("author_icon", attachment.AuthorIcon)
		ar.set("title", attachment.Title)
		ar.set("title_link", attachment.TitleLink)
		ar.set("thumb_url", attachment.ThumbURL)
		ar.set("footer", attachment.Footer)
		ar.set("footer_icon", attachment.FooterIcon)
		ar.set("image_url", attachment.ImageURL)
		var fields []yaml.MapSlice
		for _, field := range attachment.Fields {
			var fr rawValue
			fr.set("title", field.Title)
			fr.set("value", field.Value)
			fr.set("short", field.Short)
			fields = append(fields, fr.items)
		}
		ar.set("fields", fields)
		attachments = append(attachments, ar.items)
	}
	if len(attachments) > 0 {
		r.set("attachments", attachments)
	}
	if rc.Props != nil {
		var pr rawValue
		if rc.Props.Card != nil {
			pr.set("card", rc.Props.Card)
		}
		r.set("props", pr.items)
	}
	if rc.Priority != nil {
		var pr rawValue
		pr.set("priority", rc.Priority.Priority)
		if rc.Priority.RequestedAck != nil {
			pr.set("requested_ack", rc.Priority.RequestedAck)
		}
		if rc.Priority.PersistentNotifications != nil {
			pr.set("persistent_notifications", rc.Priority.PersistentNotifications)
		}
		r.set("priority", pr.items)
	}
	return r.items, nil
}

func buildDiscord(rc vmv1beta1.DiscordConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	var u string
	if rc.URLSecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.URLSecret)
		if err != nil {
			return nil, err
		}
		u = secret
	} else if rc.URL != nil {
		u = *rc.URL
	}
	if len(u) > 0 {
		if err := parseURL(u); err != nil {
			return nil, fmt.Errorf("invalid URL %s: %w", u, err)
		}
		r.set("webhook_url", u)
	}
	r.set("title", rc.Title)
	r.set("message", rc.Message)
	r.set("content", rc.Content)
	r.set("username", rc.Username)
	r.set("avatar_url", rc.AvatarURL)
	return r.items, nil
}

func buildSNS(rc vmv1beta1.SNSConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	r.set("api_url", rc.URL)
	r.set("topic_arn", rc.TopicArn)
	r.set("subject", rc.Subject)
	r.set("phone_number", rc.PhoneNumber)
	r.set("target_arn", rc.TargetArn)
	r.set("message", rc.Message)
	if len(rc.Attributes) > 0 {
		var attributes yaml.MapSlice
		for k, v := range rc.Attributes {
			attributes = append(attributes, yaml.MapItem{Key: k, Value: v})
		}
		r.set("attributes", attributes)
	}
	if rc.Sigv4 != nil {
		var sr rawValue
		sr.set("region", rc.Sigv4.Region)
		sr.set("profile", rc.Sigv4.Profile)
		sr.set("role_arn", rc.Sigv4.RoleArn)
		if rc.Sigv4.AccessKey != "" {
			sr.set("access_key", rc.Sigv4.AccessKey)
		} else if rc.Sigv4.AccessKeySelector != nil {
			secret, err := ac.LoadKeyFromSecret(ns, rc.Sigv4.AccessKeySelector)
			if err != nil {
				return nil, err
			}
			sr.set("access_key", secret)
		}
		if rc.Sigv4.SecretKey != nil {
			secret, err := ac.LoadKeyFromSecret(ns, rc.Sigv4.SecretKey)
			if err != nil {
				return nil, err
			}
			sr.set("secret_key", secret)
		}
		r.set("sigv4", sr.items)
	}
	return r.items, nil
}

func buildWebex(rc vmv1beta1.WebexConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	if rc.URL != nil {
		r.set("api_url", rc.URL)
	}
	r.set("room_id", rc.RoomId)
	r.set("message", rc.Message)
	return r.items, nil
}

func buildJira(gc *globalConfig, rc vmv1beta1.JiraConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if rc.APIURL == nil && (gc == nil || len(gc.JiraAPIURL) == 0) {
		return nil, fmt.Errorf("api_url secret is not defined and no global Jira API URL set")
	}
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	if rc.APIURL != nil {
		r.set("api_url", rc.APIURL)
	}
	r.set("project", rc.Project)
	r.set("issue_type", rc.IssueType)
	r.set("description", rc.Description)
	r.set("priority", rc.Priority)
	r.set("summary", rc.Summary)
	r.set("reopen_transition", rc.ReopenTransition)
	r.set("resolve_transition", rc.ResolveTransition)
	r.set("wont_fix_resolution", rc.WontFixResolution)
	r.set("reopen_duration", rc.ReopenDuration)
	r.set("labels", rc.Labels)
	if len(rc.Fields) > 0 {
		sortableFieldIdxs := make([]string, 0, len(rc.Fields))
		for key := range rc.Fields {
			sortableFieldIdxs = append(sortableFieldIdxs, key)
		}
		slices.Sort(sortableFieldIdxs)
		fields := make(yaml.MapSlice, 0, len(rc.Fields))
		for _, key := range sortableFieldIdxs {
			fields = append(fields, yaml.MapItem{
				Key:   key,
				Value: string(rc.Fields[key].Raw),
			})
		}
		r.set("fields", fields)
	}
	return r.items, nil
}

func buildIncidentio(rc vmv1beta1.IncidentioConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.AlertSourceToken != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.AlertSourceToken)
		if err != nil {
			return nil, err
		}
		r.set("alert_source_token", secret)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	r.set("url", rc.URL)
	r.set("timeout", rc.Timeout)
	r.set("max_alerts", rc.MaxAlerts)
	return r.items, nil
}

func buildRocketchat(gc *globalConfig, rc vmv1beta1.RocketchatConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if rc.TokenID == nil {
		if gc == nil || (len(gc.RocketchatTokenID) == 0 && len(gc.RocketchatTokenIDFile) == 0) {
			return nil, fmt.Errorf("no global Rocketchat TokenID set either inline or in a file")
		}
	}
	if rc.Token == nil {
		if gc == nil || (len(gc.RocketchatToken) == 0 && len(gc.RocketchatTokenFile) == 0) {
			return nil, fmt.Errorf("no global Rocketchat Token set either inline or in a file")
		}
	}

	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	if rc.APIURL != nil {
		r.set("api_url", rc.APIURL)
	}
	if rc.TokenID != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.TokenID)
		if err != nil {
			return nil, err
		}
		r.set("token_id", secret)
	}
	if rc.Token != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.Token)
		if err != nil {
			return nil, err
		}
		r.set("token", secret)
	}
	r.set("channel", rc.Channel)
	r.set("color", rc.Color)
	r.set("title", rc.Title)
	r.set("text", rc.Text)
	r.set("emoji", rc.Emoji)
	r.set("icon_url", rc.IconURL)
	r.set("image_url", rc.ImageURL)
	r.set("thumb_url", rc.ThumbURL)
	r.set("short_fields", rc.ShortFields)
	r.set("link_names", rc.LinkNames)
	if len(rc.Fields) > 0 {
		fields := make([]yaml.MapSlice, 0, len(rc.Fields))
		for _, f := range rc.Fields {
			var fr rawValue
			fr.set("title", f.Title)
			fr.set("value", f.Value)
			if f.Short != nil {
				fr.set("short", *f.Short)
			}
			fields = append(fields, fr.items)
		}
		r.set("fields", fields)
	}
	if len(rc.Actions) > 0 {
		actions := make([]yaml.MapSlice, 0, len(rc.Actions))
		for _, a := range rc.Actions {
			var ar rawValue
			ar.set("type", a.Type)
			ar.set("text,omitempty", a.Text)
			ar.set("url", a.URL)
			ar.set("msg", a.Msg)
			actions = append(actions, ar.items)
		}
		r.set("actions", actions)
	}
	return r.items, nil
}

func buildTelegram(rc vmv1beta1.TelegramConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.BotToken != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.BotToken)
		if err != nil {
			return nil, err
		}
		r.set("bot_token", secret)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	if rc.DisableNotifications != nil {
		r.set("disable_notifications", rc.DisableNotifications)
	}
	r.set("chat_id", rc.ChatID)
	r.set("message_thread_id", rc.MessageThreadID)
	r.set("api_url", rc.APIUrl)
	r.set("message", rc.Message)
	r.set("parse_mode", rc.ParseMode)
	return r.items, nil
}

func buildSlack(gc *globalConfig, rc vmv1beta1.SlackConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if rc.APIURL == nil {
		if gc == nil || (len(gc.SlackAPIURL) == 0 && gc.SlackAPIURLFile == "") {
			return nil, fmt.Errorf("api_url secret is not defined and no global Slack API URL set either inline or in a file")
		}
	}
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.APIURL != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.APIURL)
		if err != nil {
			return nil, err
		}
		if err := parseURL(secret); err != nil {
			return nil, fmt.Errorf("invalid URL %s in key %s from secret %s: %v", secret, rc.APIURL.Key, rc.APIURL.Name, err)
		}
		r.set("api_url", secret)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	r.set("username", rc.Username)
	r.set("channel", rc.Channel)
	r.set("color", rc.Color)
	r.set("fallback", rc.Fallback)
	r.set("footer", rc.Footer)
	r.set("icon_emoji", rc.IconEmoji)
	r.set("icon_url", rc.IconURL)
	r.set("image_url", rc.ImageURL)
	r.set("pretext", rc.Pretext)
	r.set("text", rc.Text)
	r.set("title", rc.Title)
	r.set("title_link", rc.TitleLink)
	r.set("thumb_url", rc.ThumbURL)
	r.set("callback_id", rc.CallbackID)
	r.set("link_names", rc.LinkNames)
	r.set("short_fields", rc.ShortFields)
	if rc.UpdateMessage != nil {
		r.set("update_message", rc.UpdateMessage)
	}
	r.set("mrkdwn_in", rc.MrkdwnIn)
	var actions []yaml.MapSlice
	for _, action := range rc.Actions {
		var ar rawValue
		ar.set("name", action.Name)
		ar.set("value", action.Value)
		ar.set("text", action.Text)
		ar.set("url", action.URL)
		ar.set("type", action.Type)
		ar.set("style", action.Style)
		if action.ConfirmField != nil {
			var cfr rawValue
			cfr.set("text", action.ConfirmField.Text)
			cfr.set("ok_text", action.ConfirmField.OkText)
			cfr.set("dismiss_text", action.ConfirmField.DismissText)
			cfr.set("title", action.ConfirmField.Title)
			ar.set("confirm", cfr.items)
		}
		actions = append(actions, ar.items)
	}
	if len(actions) > 0 {
		r.set("actions", actions)
	}
	var fields []yaml.MapSlice
	for _, field := range rc.Fields {
		var fr rawValue
		fr.set("value", field.Value)
		fr.set("title", field.Title)
		fr.set("short", field.Short)
		fields = append(fields, fr.items)
	}
	if len(fields) > 0 {
		r.set("fields", fields)
	}
	return r.items, nil
}

func buildMSTeamsV2(rc vmv1beta1.MSTeamsV2Config, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if rc.URL == nil && rc.URLSecret == nil {
		return nil, fmt.Errorf("one of required fields 'webhook_url' or 'webhook_url_secret' are not set")
	}

	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	var u string
	if rc.URLSecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.URLSecret)
		if err != nil {
			return nil, err
		}
		u = secret
	} else if rc.URL != nil {
		u = *rc.URL
	}
	if len(u) == 0 {
		return nil, nil
	}
	if err := parseURL(u); err != nil {
		return nil, fmt.Errorf("unexpected webhook_url value=%q: %w", u, err)
	}
	r.set("webhook_url", u)
	r.set("text", rc.Text)
	r.set("title", rc.Title)
	return r.items, nil
}

func buildWebhook(rc vmv1beta1.WebhookConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	var url string
	if rc.URLSecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.URLSecret)
		if err != nil {
			return nil, err
		}
		url = secret
	} else if rc.URL != nil {
		url = *rc.URL
	}

	// no point to add config without url
	if len(url) == 0 {
		return nil, nil
	}
	if err := parseURL(url); err != nil {
		return nil, fmt.Errorf("failed to parse webhook url: %w", err)
	}
	r.set("url", url)
	r.set("max_alerts", rc.MaxAlerts)
	r.set("timeout", rc.Timeout)
	return r.items, nil
}

func buildWechat(gc *globalConfig, rc vmv1beta1.WechatConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if rc.APISecret == nil && (gc == nil || gc.WechatAPISecret == "") {
		return nil, fmt.Errorf("api_secret is not set and no global Wechat ApiSecret set")
	}
	if rc.CorpID == "" && (gc == nil || gc.WechatAPICorpID == "") {
		return nil, fmt.Errorf("cord_id is not set and no global Wechat CorpID set")
	}
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.APISecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.APISecret)
		if err != nil {
			return nil, err
		}
		r.set("api_secret", secret)
	}
	r.set("message", rc.Message)
	r.set("message_type", rc.MessageType)
	r.set("agent_id", rc.AgentID)
	r.set("corp_id", rc.CorpID)
	r.set("to_party", rc.ToParty)
	r.set("to_tag", rc.ToTag)
	r.set("to_user", rc.ToUser)
	if rc.APIURL != "" {
		if err := parseURL(rc.APIURL); err != nil {
			return nil, err
		}
		r.set("api_url", rc.APIURL)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	return r.items, nil
}

func buildVictorOps(gc *globalConfig, rc vmv1beta1.VictorOpsConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if rc.APIKey == nil {
		if gc == nil || (gc.VictorOpsAPIKey == "" && gc.VictorOpsAPIKeyFile == "") {
			return nil, fmt.Errorf("api_key secret is not set and no global VictorOps API Key set")
		}
	}
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.APIKey != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.APIKey)
		if err != nil {
			return nil, err
		}
		r.set("api_key", secret)
	}
	if rc.APIURL != "" {
		if err := parseURL(rc.APIURL); err != nil {
			return nil, err
		}
		r.set("api_url", rc.APIURL)
	}
	r.set("routing_key", rc.RoutingKey)
	r.set("message_type", rc.MessageType)
	r.set("entity_display_name", rc.EntityDisplayName)
	r.set("state_message", rc.StateMessage)
	r.set("monitoring_tool", rc.MonitoringTool)
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	if len(rc.CustomFields) > 0 {
		var cfs rawValue
		var customFieldIDs []string
		for customFieldKey := range rc.CustomFields {
			customFieldIDs = append(customFieldIDs, customFieldKey)
		}
		slices.Sort(customFieldIDs)
		for _, customFieldKey := range customFieldIDs {
			value := rc.CustomFields[customFieldKey]
			cfs.set(customFieldKey, value)
		}
		r.set("custom_fields", cfs.items)
	}
	return r.items, nil
}

func buildPushover(rc vmv1beta1.PushoverConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.UserKey != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.UserKey)
		if err != nil {
			return nil, err
		}
		r.set("user_key", secret)
	}
	if rc.Token != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.Token)
		if err != nil {
			return nil, err
		}
		r.set("token", secret)
	}

	r.set("url", rc.URL)
	r.set("sound", rc.Sound)
	r.set("priority", rc.Priority)
	r.set("message", rc.Message)
	r.set("expire", rc.Expire)
	r.set("retry", rc.Retry)
	r.set("title", rc.Title)
	r.set("url_title", rc.URLTitle)
	r.set("html", rc.HTML)
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	return r.items, nil
}

func buildPagerDuty(rc vmv1beta1.PagerDutyConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	if rc.RoutingKey != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.RoutingKey)
		if err != nil {
			return nil, err
		}
		r.set("routing_key", secret)
	}
	if rc.ServiceKey != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.ServiceKey)
		if err != nil {
			return nil, err
		}
		r.set("service_key", secret)
	}
	if rc.URL != "" {
		if err := parseURL(rc.URL); err != nil {
			return nil, err
		}
		r.set("url", rc.URL)
	}
	r.set("description", rc.Description)
	r.set("client_url", rc.ClientURL)
	r.set("client", rc.Client)
	r.set("class", rc.Class)
	r.set("component", rc.Component)
	r.set("group", rc.Group)
	r.set("severity", rc.Severity)
	var images []yaml.MapSlice
	for _, image := range rc.Images {
		var ir rawValue
		ir.set("href", image.Href)
		ir.set("source", image.Source)
		ir.set("alt", image.Alt)
		images = append(images, ir.items)
	}
	if len(images) > 0 {
		r.set("images", images)
	}
	var links []yaml.MapSlice
	for _, link := range rc.Links {
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
		r.set("links", links)
	}
	detailKeys := make([]string, 0, len(rc.Details))
	for detailKey, value := range rc.Details {
		if len(value) == 0 {
			continue
		}
		detailKeys = append(detailKeys, detailKey)
	}
	slices.Sort(detailKeys)
	if len(detailKeys) > 0 {
		var detailsYaml yaml.MapSlice
		for _, detailKey := range detailKeys {
			detailsYaml = append(detailsYaml, yaml.MapItem{Key: detailKey, Value: rc.Details[detailKey]})
		}
		r.set("details", detailsYaml)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	return r.items, nil
}

func buildEmail(gc *globalConfig, rc vmv1beta1.EmailConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if len(rc.Smarthost) == 0 && (gc == nil || len(gc.SMTPSmarthost) == 0) {
		return nil, fmt.Errorf("required email smarthost is not set at local and global alertmanager config")
	}
	if len(rc.Smarthost) == 0 && (gc == nil || len(gc.SMTPSmarthost) == 0) {
		return nil, fmt.Errorf("required email smarthost is not set at local and global alertmanager config")
	}
	if rc.From == "" && (gc == nil || gc.SMTPFrom == "") {
		return nil, fmt.Errorf("required email from is not set at local and global alertmanager config")
	}

	var r rawValue
	if rc.RequireTLS != nil {
		r.set("require_tls", rc.RequireTLS)
	}
	// add tls config in any case
	// require_tls is true by default and it could be managed via global configuration
	if rc.TLSConfig != nil {
		cfg, err := ac.TLSToYAML(ns, "", rc.TLSConfig)
		if err != nil {
			return nil, err
		}
		if len(cfg) > 0 {
			r.set("tls_config", cfg)
		}
	}
	if rc.AuthPassword != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.AuthPassword)
		if err != nil {
			return nil, err
		}
		r.set("auth_password", secret)
	}
	if rc.AuthSecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.AuthSecret)
		if err != nil {
			return nil, err
		}
		r.set("auth_secret", secret)
	}
	r.set("headers", rc.Headers)
	r.set("from", rc.From)
	r.set("text", rc.Text)
	r.set("to", rc.To)
	r.set("html", rc.HTML)
	r.set("auth_identity", rc.AuthIdentity)
	r.set("auth_username", rc.AuthUsername)
	r.set("hello", rc.Hello)
	r.set("smarthost", rc.Smarthost)
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	return r.items, nil
}

func buildOpsGenie(gc *globalConfig, rc vmv1beta1.OpsGenieConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if rc.APIKey == nil {
		if gc == nil || (gc.OpsGenieAPIKey == "" && gc.OpsGenieAPIKeyFile == "") {
			return nil, fmt.Errorf("api_key secret is not defined and no global OpsGenie API Key set either inline or in a file")
		}
	}
	var r rawValue
	if rc.APIKey != nil {
		secret, err := ac.LoadKeyFromSecret(ns, rc.APIKey)
		if err != nil {
			return nil, err
		}
		r.set("api_key", secret)
	}
	r.set("source", rc.Source)
	r.set("message", rc.Message)
	r.set("description", rc.Description)
	r.set("tags", rc.Tags)
	r.set("note", rc.Note)
	r.set("entity", rc.Entity)
	r.set("Actions", rc.Actions)
	if rc.APIURL != "" {
		if err := parseURL(rc.APIURL); err != nil {
			return nil, err
		}
		r.set("api_url", rc.APIURL)
	}
	r.set("priority", rc.Priority)
	if rc.Details != nil {
		r.set("details", rc.Details)
	}
	if rc.SendResolved != nil {
		r.set("send_resolved", rc.SendResolved)
	}
	r.set("update_alerts", rc.UpdateAlerts)

	var responders []yaml.MapSlice
	for _, responder := range rc.Responders {
		var rr rawValue
		rr.set("name", responder.Name)
		rr.set("username", responder.Username)
		rr.set("id", responder.ID)
		rr.set("type", responder.Type)
		responders = append(responders, rr.items)
	}
	if len(responders) > 0 {
		r.set("responders", responders)
	}
	if rc.HTTPConfig != nil {
		c, err := buildHTTPConfig(rc.HTTPConfig, ns, ac)
		if err != nil {
			return nil, err
		}
		r.set("http_config", c)
	}
	return r.items, nil
}

func buildHTTPConfig(httpCfg *vmv1beta1.HTTPConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if httpCfg == nil {
		return nil, nil
	}
	var r rawValue
	if httpCfg.TLSConfig != nil {
		cfg, err := ac.TLSToYAML(ns, "", httpCfg.TLSConfig)
		if err != nil {
			return nil, err
		}
		if len(cfg) > 0 {
			r.set("tls_config", cfg)
		}
	}
	if httpCfg.Authorization != nil {
		cfg, err := ac.AuthorizationToYAML(ns, httpCfg.Authorization)
		if err != nil {
			return nil, err
		}
		r.items = append(r.items, cfg...)
	}
	if httpCfg.BasicAuth != nil {
		cfg, err := ac.BasicAuthToYAML(ns, httpCfg.BasicAuth)
		if err != nil {
			return nil, err
		}
		r.set("basic_auth", cfg)
	}
	var tokenAuth rawValue
	if httpCfg.BearerTokenSecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, httpCfg.BearerTokenSecret)
		if err != nil {
			return nil, fmt.Errorf("cannot find secret for bearerToken: %w", err)
		}
		tokenAuth.set("credentials", secret)
	}
	if len(httpCfg.BearerTokenFile) > 0 {
		tokenAuth.set("credentials_file", httpCfg.BearerTokenFile)
	}
	if len(tokenAuth.items) > 0 {
		r.set("authorization", tokenAuth.items)
	}
	if httpCfg.FollowRedirects != nil {
		r.set("follow_redirects", *httpCfg.FollowRedirects)
	}
	if httpCfg.OAuth2 != nil {
		cfg, err := ac.OAuth2ToYAML(ns, httpCfg.OAuth2)
		if err != nil {
			return nil, fmt.Errorf("cannot build oauth2 configuration: %w", err)
		}
		r.items = append(r.items, cfg...)
	}
	cfg, err := buildProxyConfig(&httpCfg.ProxyConfig, ns, ac)
	if err != nil {
		return nil, err
	}
	if len(cfg) > 0 {
		r.items = append(r.items, cfg...)
	}
	return r.items, nil
}

func buildProxyConfig(proxyCfg *vmv1beta1.ProxyConfig, ns string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r rawValue
	r.set("proxy_url", proxyCfg.ProxyURL)
	r.set("no_proxy", proxyCfg.NoProxy)
	r.set("proxy_from_environment", proxyCfg.ProxyFromEnvironment)
	if len(proxyCfg.ProxyConnectHeader) > 0 {
		var h yaml.MapSlice
		for k, vs := range proxyCfg.ProxyConnectHeader {
			var secrets []string
			for i, v := range vs {
				secret, err := ac.LoadKeyFromSecret(ns, &v)
				if err != nil {
					return nil, fmt.Errorf("cannot find secret for proxy_connect_header[%q][%d]: %w", k, i, err)
				}
				secrets = append(secrets, secret)
			}
			h = append(h, yaml.MapItem{Key: k, Value: secrets})
		}
		if len(h) > 0 {
			r.items = append(r.items, yaml.MapItem{Key: "proxy_connect_header", Value: h})
		}
	}
	return r.items, nil
}

// builds configuration according to https://prometheus.io/docs/alerting/latest/configuration/#tracing-configuration
func buildTracingConfig(cr *vmv1beta1.VMAlertmanager, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var cfg rawValue
	if cr == nil || cr.Spec.TracingConfig == nil {
		return cfg.items, nil
	}
	tracingCfg := cr.Spec.TracingConfig
	if tracingCfg.TLSConfig != nil {
		if tracingCfg.TLSConfig.CASecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, tracingCfg.TLSConfig.CASecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			tracingCfg.TLSConfig.CAFile = file
		}
		if tracingCfg.TLSConfig.CertSecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, tracingCfg.TLSConfig.CertSecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			tracingCfg.TLSConfig.CertFile = file
		}

		if tracingCfg.TLSConfig.KeySecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, tracingCfg.TLSConfig.KeySecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			tracingCfg.TLSConfig.KeyFile = file
		}

		var r rawValue
		r.set("ca_file", tracingCfg.TLSConfig.CAFile)
		r.set("cert_file", tracingCfg.TLSConfig.CertFile)
		r.set("key_file", tracingCfg.TLSConfig.KeyFile)
		r.set("insecure_skip_verify", tracingCfg.TLSConfig.InsecureSkipVerify)
		r.set("server_name", tracingCfg.TLSConfig.ServerName)
		cfg.items = append(cfg.items, yaml.MapItem{Key: "tls_config", Value: r.items})
	}
	cfg.set("insecure", tracingCfg.Insecure)
	cfg.set("client_type", tracingCfg.ClientType)
	cfg.set("sampling_fraction", tracingCfg.SamplingFraction)
	cfg.set("timeout", tracingCfg.Timeout)
	cfg.set("compression", tracingCfg.Compression)
	cfg.set("http_headers", tracingCfg.Headers)
	cfg.set("endpoint", tracingCfg.Endpoint)
	return cfg.items, nil
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

// builds configuration according to https://prometheus.io/docs/alerting/latest/https/#gossip-traffic
func buildGossipConfigYAML(cr *vmv1beta1.VMAlertmanager, ac *build.AssetsCache) ([]byte, error) {
	if cr.Spec.GossipConfig == nil {
		return nil, nil
	}
	var cfg yaml.MapSlice
	gossipCfg := cr.Spec.GossipConfig
	if gossipCfg.TLSServerConfig != nil {
		if gossipCfg.TLSServerConfig.ClientCASecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, gossipCfg.TLSServerConfig.ClientCASecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret CA value: %w", err)
			}
			gossipCfg.TLSServerConfig.ClientCAFile = file
		}
		if gossipCfg.TLSServerConfig.CertSecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, gossipCfg.TLSServerConfig.CertSecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret CA value: %w", err)
			}
			gossipCfg.TLSServerConfig.CertFile = file
		}

		if gossipCfg.TLSServerConfig.KeySecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, gossipCfg.TLSServerConfig.KeySecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			gossipCfg.TLSServerConfig.KeyFile = file
		}

		var r rawValue
		r.set("client_ca_file", gossipCfg.TLSServerConfig.ClientCAFile)
		r.set("cert_file", gossipCfg.TLSServerConfig.CertFile)
		r.set("key_file", gossipCfg.TLSServerConfig.KeyFile)
		r.set("cipher_suites", gossipCfg.TLSServerConfig.CipherSuites)
		r.set("curve_preferences", gossipCfg.TLSServerConfig.CurvePreferences)
		r.set("client_auth_type", gossipCfg.TLSServerConfig.ClientAuthType)
		if gossipCfg.TLSServerConfig.PreferServerCipherSuites != nil {
			r.set("prefer_server_cipher_suites", *gossipCfg.TLSServerConfig.PreferServerCipherSuites)
		}
		r.set("max_version", gossipCfg.TLSServerConfig.MaxVersion)
		r.set("min_version", gossipCfg.TLSServerConfig.MinVersion)
		cfg = append(cfg, yaml.MapItem{Key: "tls_server_config", Value: r.items})
	}

	if gossipCfg.TLSClientConfig != nil {
		if gossipCfg.TLSClientConfig.CASecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, gossipCfg.TLSClientConfig.CASecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			gossipCfg.TLSClientConfig.CAFile = file
		}
		if gossipCfg.TLSClientConfig.CertSecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, gossipCfg.TLSClientConfig.CertSecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			gossipCfg.TLSClientConfig.CertFile = file
		}

		if gossipCfg.TLSClientConfig.KeySecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, gossipCfg.TLSClientConfig.KeySecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			gossipCfg.TLSClientConfig.KeyFile = file
		}
		var r rawValue
		r.set("ca_file", gossipCfg.TLSClientConfig.CAFile)
		r.set("cert_file", gossipCfg.TLSClientConfig.CertFile)
		r.set("key_file", gossipCfg.TLSClientConfig.KeyFile)
		r.set("insecure_skip_verify", gossipCfg.TLSClientConfig.InsecureSkipVerify)
		r.set("server_name", gossipCfg.TLSClientConfig.ServerName)
		cfg = append(cfg, yaml.MapItem{Key: "tls_client_config", Value: r.items})
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize alertmanager gossip config as yaml: %w", err)
	}
	return data, nil
}

// builds configuration according to https://prometheus.io/docs/alerting/latest/https/#http-traffic
func buildWebServerConfigYAML(cr *vmv1beta1.VMAlertmanager, ac *build.AssetsCache) ([]byte, error) {
	if cr.Spec.WebConfig == nil {
		return nil, nil
	}
	var cfg yaml.MapSlice
	webCfg := cr.Spec.WebConfig
	if webCfg.HTTPServerConfig != nil {
		var wCfg yaml.MapSlice
		if webCfg.HTTPServerConfig.HTTP2 {
			if webCfg.TLSServerConfig == nil {
				return nil, fmt.Errorf("with enabled http2, tls_server_config is required to be set")
			}
			wCfg = append(wCfg, yaml.MapItem{Key: "http2", Value: webCfg.HTTPServerConfig.HTTP2})
		}
		if len(webCfg.HTTPServerConfig.Headers) > 0 {
			wCfg = append(wCfg, yaml.MapItem{Key: "headers", Value: orderedYAMLMap(webCfg.HTTPServerConfig.Headers)})
		}
		cfg = append(cfg, yaml.MapItem{Key: "http_server_config", Value: wCfg})
	}
	if webCfg.TLSServerConfig != nil {
		if webCfg.TLSServerConfig.ClientCASecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, webCfg.TLSServerConfig.ClientCASecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch secret clientCA value: %w", err)
			}
			webCfg.TLSServerConfig.ClientCAFile = file
		}
		if webCfg.TLSServerConfig.CertSecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, webCfg.TLSServerConfig.CertSecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch certSecret value: %w", err)
			}
			webCfg.TLSServerConfig.CertFile = file
		}

		if webCfg.TLSServerConfig.KeySecretRef != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, cr.Namespace, webCfg.TLSServerConfig.KeySecretRef)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch keySecret value: %w", err)
			}
			webCfg.TLSServerConfig.KeyFile = file
		}

		var r rawValue
		r.set("client_ca_file", webCfg.TLSServerConfig.ClientCAFile)
		r.set("cert_file", webCfg.TLSServerConfig.CertFile)
		r.set("key_file", webCfg.TLSServerConfig.KeyFile)
		r.set("cipher_suites", webCfg.TLSServerConfig.CipherSuites)
		r.set("curve_preferences", webCfg.TLSServerConfig.CurvePreferences)
		r.set("client_auth_type", webCfg.TLSServerConfig.ClientAuthType)
		if webCfg.TLSServerConfig.PreferServerCipherSuites != nil {
			r.set("prefer_server_cipher_suites", *webCfg.TLSServerConfig.PreferServerCipherSuites)
		}
		r.set("max_version", webCfg.TLSServerConfig.MaxVersion)
		r.set("min_version", webCfg.TLSServerConfig.MinVersion)
		cfg = append(cfg, yaml.MapItem{Key: "tls_server_config", Value: r.items})
	}

	if len(webCfg.BasicAuthUsers) > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "basic_auth_users", Value: orderedYAMLMap(webCfg.BasicAuthUsers)})
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize alertmanager webconfig as yaml: %w", err)
	}
	return data, nil
}

func orderedYAMLMap(src map[string]string) yaml.MapSlice {
	dstKeys := make([]string, 0, len(src))
	for key := range src {
		dstKeys = append(dstKeys, key)
	}
	slices.Sort(dstKeys)
	var result yaml.MapSlice
	for _, key := range dstKeys {
		result = append(result, yaml.MapItem{Key: key, Value: src[key]})
	}
	return result
}
