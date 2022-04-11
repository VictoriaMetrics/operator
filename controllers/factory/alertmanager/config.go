package alertmanager

import (
	"context"
	"fmt"
	"path"
	"sort"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func BuildConfig(ctx context.Context, rclient client.Client, mustAddNamespaceMatcher bool, baseCfg []byte, amcfgs map[string]*operatorv1beta1.VMAlertmanagerConfig, tlsAssets map[string]string) ([]byte, error) {
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
	var muteIntervals []yaml.MapSlice
	secretCache := make(map[string]*v1.Secret)
	configmapCache := make(map[string]*v1.ConfigMap)
	var firstReceiverName string
	for _, posIdx := range amConfigIdentifiers {
		amcKey := amcfgs[posIdx]
		for _, rule := range amcKey.Spec.InhibitRules {
			baseYAMlCfg.InhibitRules = append(baseYAMlCfg.InhibitRules, buildInhibitRule(amcKey.Namespace, rule))
		}
		mtis := buildMuteTimeInterval(amcKey)
		if len(mtis) > 0 {
			muteIntervals = append(muteIntervals, mtis...)
		}
		if amcKey.Spec.Route == nil {
			// todo add logging.
			continue
		}
		// use first route receiver name as default receiver.
		if len(firstReceiverName) == 0 && len(amcKey.Spec.Route.Receiver) > 0 {
			firstReceiverName = buildReceiverName(amcKey, amcKey.Spec.Route.Receiver)
		}
		subRoutes = append(subRoutes, buildRoute(amcKey, amcKey.Spec.Route, true, mustAddNamespaceMatcher))
		for _, receiver := range amcKey.Spec.Receivers {
			receiverCfg, err := buildReceiver(ctx, rclient, amcKey, receiver, secretCache, configmapCache, tlsAssets)
			if err != nil {
				return nil, fmt.Errorf("cannot build receiver cfg for: %s, err: %w", amcKey.AsKey(), err)
			}
			if len(receiverCfg) > 0 {
				baseYAMlCfg.Receivers = append(baseYAMlCfg.Receivers, receiverCfg)
			}
		}
	}
	if baseYAMlCfg.Route == nil && len(subRoutes) > 0 {
		baseYAMlCfg.Route = &route{
			Receiver: firstReceiverName,
		}
	}
	if len(subRoutes) > 0 {
		baseYAMlCfg.Route.Routes = append(baseYAMlCfg.Route.Routes, subRoutes...)
	}
	if len(muteIntervals) > 0 {
		baseYAMlCfg.MuteTimeIntervals = append(baseYAMlCfg.MuteTimeIntervals, muteIntervals...)
	}

	return yaml.Marshal(baseYAMlCfg)
}

func buildMuteTimeInterval(cr *operatorv1beta1.VMAlertmanagerConfig) []yaml.MapSlice {
	var r []yaml.MapSlice
	for _, mti := range cr.Spec.MutTimeIntervals {
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
			r = append(r, yaml.MapSlice{{Key: "name", Value: mti.Name}, {Key: "time_intervals", Value: temp}})
		}

	}
	return r
}

func buildRoute(cr *operatorv1beta1.VMAlertmanagerConfig, cfgRoute *operatorv1beta1.Route, topLevel, mustAddNamespaceMatcher bool) yaml.MapSlice {
	var r yaml.MapSlice
	matchers := cfgRoute.Matchers
	continueSetting := cfgRoute.Continue
	// enforce continue and namespace match
	if topLevel {
		continueSetting = true

	}
	if mustAddNamespaceMatcher {
		matchers = append(matchers, fmt.Sprintf("namespace = %q", cr.Namespace))
	}

	var nestedRoutes []yaml.MapSlice
	for _, nestedRoute := range cfgRoute.Routes {
		// namespace matcher not needed for nested routes
		nestedRoutes = append(nestedRoutes, buildRoute(cr, nestedRoute, false, false))
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
	toYaml("matchers", matchers)
	toYaml("group_by", cfgRoute.GroupBy)
	toYaml("mute_time_intervals", cfgRoute.MuteTimeIntervals)
	toYamlString("group_interval", cfgRoute.GroupInterval)
	toYamlString("group_wait", cfgRoute.GroupWait)
	toYamlString("repeat_interval", cfgRoute.RepeatInterval)
	if len(cfgRoute.Receiver) > 0 {
		r = append(r, yaml.MapItem{Key: "receiver", Value: buildReceiverName(cr, cfgRoute.Receiver)})
	}
	r = append(r, yaml.MapItem{Key: "continue", Value: continueSetting})
	return r
}

func buildInhibitRule(namespace string, rule operatorv1beta1.InhibitRule) yaml.MapSlice {
	var r yaml.MapSlice
	namespaceMatch := fmt.Sprintf("namespace = %q", namespace)
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
	Global            interface{}     `yaml:"global,omitempty" json:"global,omitempty"`
	Route             *route          `yaml:"route,omitempty" json:"route,omitempty"`
	InhibitRules      []yaml.MapSlice `yaml:"inhibit_rules,omitempty" json:"inhibit_rules,omitempty"`
	Receivers         []yaml.MapSlice `yaml:"receivers,omitempty" json:"receivers,omitempty"`
	MuteTimeIntervals []yaml.MapSlice `yaml:"mute_time_intervals,omitempty"`
	Templates         []string        `yaml:"templates" json:"templates"`
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

func buildReceiver(
	ctx context.Context,
	rclient client.Client,
	cr *operatorv1beta1.VMAlertmanagerConfig,
	reciever operatorv1beta1.Receiver,
	cache map[string]*v1.Secret,
	configmapCache map[string]*v1.ConfigMap,
	tlsAssets map[string]string,
) (yaml.MapSlice, error) {

	cb := initConfigBuilder(ctx, rclient, cr, reciever, cache, configmapCache, tlsAssets)

	if err := cb.buildCfg(); err != nil {
		return nil, err
	}
	return cb.result, nil
}

type configBuilder struct {
	client.Client
	ctx            context.Context
	receiver       operatorv1beta1.Receiver
	currentCR      *operatorv1beta1.VMAlertmanagerConfig
	currentYaml    []yaml.MapSlice
	result         yaml.MapSlice
	secretCache    map[string]*v1.Secret
	configmapCache map[string]*v1.ConfigMap
	tlsAssets      map[string]string
}

func initConfigBuilder(
	ctx context.Context,
	rclient client.Client,
	cr *operatorv1beta1.VMAlertmanagerConfig,
	receiver operatorv1beta1.Receiver,
	cache map[string]*v1.Secret,
	configmapCache map[string]*v1.ConfigMap,
	tlsAssets map[string]string) *configBuilder {
	cb := configBuilder{
		ctx:       ctx,
		Client:    rclient,
		receiver:  receiver,
		currentCR: cr,
		result: yaml.MapSlice{
			{
				Key:   "name",
				Value: buildReceiverName(cr, receiver.Name),
			},
		},
		secretCache:    cache,
		configmapCache: configmapCache,
		tlsAssets:      tlsAssets,
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

	for _, emailCfg := range cb.receiver.EmailConfigs {
		if err := cb.buildEmail(emailCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("email_configs")

	for _, slackCfg := range cb.receiver.SlackConfigs {
		if err := cb.buildSlack(slackCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("slack_configs")

	for _, pgCfg := range cb.receiver.PagerDutyConfigs {
		if err := cb.buildPagerDuty(pgCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("pagerduty_configs")

	for _, poCfg := range cb.receiver.PushoverConfigs {
		if err := cb.buildPushOver(poCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("pushover_configs")

	for _, voCfg := range cb.receiver.VictorOpsConfigs {
		if err := cb.buildVictorOps(voCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("victorops_configs")

	for _, wcCfg := range cb.receiver.WeChatConfigs {
		if err := cb.buildWeeChat(wcCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("wechat_configs")

	for _, whCfg := range cb.receiver.WebhookConfigs {
		if err := cb.buildWebhook(whCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("webhook_configs")
	for _, tgCfg := range cb.receiver.TelegramConfigs {
		if err := cb.buildTelegram(tgCfg); err != nil {
			return err
		}
	}
	cb.finalizeSection("telegram_configs")
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

func (cb *configBuilder) buildTelegram(tg operatorv1beta1.TelegramConfig) error {
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

func (cb *configBuilder) buildSlack(slack operatorv1beta1.SlackConfig) error {
	var temp yaml.MapSlice
	if slack.HTTPConfig != nil {
		c, err := cb.buildHTTPConfig(slack.HTTPConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "http_config", Value: c})
	}
	if slack.APIURL != nil {
		s, err := cb.fetchSecretValue(slack.APIURL)
		if err != nil {
			return err
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

func (cb *configBuilder) buildWebhook(wh operatorv1beta1.WebhookConfig) error {
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

	temp = append(temp, yaml.MapItem{Key: "url", Value: url})
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildWeeChat(wc operatorv1beta1.WeChatConfig) error {
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
	if wc.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *wc.SendResolved})
	}
	cb.currentYaml = append(cb.currentYaml, temp)
	return nil
}

func (cb *configBuilder) buildVictorOps(vo operatorv1beta1.VictorOpsConfig) error {
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

func (cb *configBuilder) buildPushOver(po operatorv1beta1.PushoverConfig) error {
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

func (cb *configBuilder) buildPagerDuty(pd operatorv1beta1.PagerDutyConfig) error {
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
	toYaml("description", pd.Description)
	toYaml("client_url", pd.ClientURL)
	toYaml("client", pd.Client)
	toYaml("class", pd.Class)
	toYaml("component", pd.Component)
	toYaml("group", pd.Group)
	toYaml("severity", pd.Severity)
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

func (cb *configBuilder) buildEmail(email operatorv1beta1.EmailConfig) error {
	var temp yaml.MapSlice
	if email.TLSConfig != nil {
		s, err := cb.buildTLSConfig(email.TLSConfig)
		if err != nil {
			return err
		}
		temp = append(temp, yaml.MapItem{Key: "tls_config", Value: s})
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

func (cb *configBuilder) buildOpsGenie(og operatorv1beta1.OpsGenieConfig) error {
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
	toYamlString("priority", og.Priority)
	if og.Details != nil {
		temp = append(temp, yaml.MapItem{Key: "details", Value: og.Details})
	}
	if og.SendResolved != nil {
		temp = append(temp, yaml.MapItem{Key: "send_resolved", Value: *og.SendResolved})
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

func (cb *configBuilder) fetchSecretValue(selector *v1.SecretKeySelector) ([]byte, error) {
	var s v1.Secret
	if existSecret, ok := cb.secretCache[selector.Name]; ok {
		s = *existSecret
	} else if err := cb.Client.Get(cb.ctx, types.NamespacedName{Name: selector.Name, Namespace: cb.currentCR.Namespace}, &s); err != nil {
		return nil, fmt.Errorf("cannot find secret for VMAlertmanager config: %s, receiver: %s, err :%w", cb.currentCR.Name, cb.receiver.Name, err)
	}
	if v, ok := s.Data[selector.Key]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("cannot find key : %s at secret: %s", selector.Key, selector.Name)
}

func (cb *configBuilder) buildHTTPConfig(httpCfg *operatorv1beta1.HTTPConfig) (yaml.MapSlice, error) {
	var r yaml.MapSlice

	if httpCfg == nil {
		return nil, nil
	}
	if httpCfg.TLSConfig != nil {
		tls, err := cb.buildTLSConfig(httpCfg.TLSConfig)
		if err != nil {
			return nil, err
		}
		r = append(r, yaml.MapItem{Key: "tls_config", Value: tls})
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

	if len(httpCfg.ProxyURL) > 0 {
		r = append(r, yaml.MapItem{Key: "proxy_url", Value: httpCfg.ProxyURL})
	}
	return r, nil
}

func (cb *configBuilder) buildBasicAuth(basicAuth *operatorv1beta1.BasicAuth) (yaml.MapSlice, error) {
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

func (cb *configBuilder) fetchSecretWithAssets(ss *v1.SecretKeySelector, cs *v1.ConfigMapKeySelector, assetKey string) error {
	var value string
	if ss != nil {
		var s v1.Secret
		if v, ok := cb.secretCache[ss.Name]; ok {
			s = *v
		} else {
			if err := cb.Client.Get(cb.ctx, types.NamespacedName{Namespace: cb.currentCR.Namespace, Name: ss.Name}, &s); err != nil {
				return fmt.Errorf("cannot fetch secret=%q for tlsAsset, err=%w", ss.Name, err)
			}
			cb.secretCache[ss.Name] = &s
		}
		value = string(s.Data[ss.Key])
	}
	if cs != nil {
		var c v1.ConfigMap
		if v, ok := cb.configmapCache[cs.Name]; ok {
			c = *v
		} else {
			if err := cb.Client.Get(cb.ctx, types.NamespacedName{Namespace: cb.currentCR.Namespace, Name: cs.Name}, &c); err != nil {
				return fmt.Errorf("cannot fetch configmap=%q for tlsAssert, err=%w", cs.Name, err)
			}
		}
		value = c.Data[cs.Key]
	}
	if len(value) == 0 {
		return fmt.Errorf("cannot find tlsAsset secret or configmap for key=%q", assetKey)
	}
	cb.tlsAssets[assetKey] = value
	return nil
}

func (cb *configBuilder) buildTLSConfig(tlsCfg *operatorv1beta1.TLSConfig) (yaml.MapSlice, error) {
	var r yaml.MapSlice
	const tlsAssetsDir = "/etc/alertmanager/config"
	pathPrefix := path.Join(tlsAssetsDir, cb.currentCR.Namespace)
	toYamlString := func(key string, src string) {
		if len(src) > 0 {
			r = append(r, yaml.MapItem{Key: key, Value: src})
		}
	}

	// tls part is tricky, secrets must be fetched and mounted to the alertmanager pod
	// it requires TLSAssets to be loaded.
	if tlsCfg.CAFile != "" {
		toYamlString("ca_file", tlsCfg.CAFile)
	} else if tlsCfg.CA.Name() != "" {
		assetKey := tlsCfg.BuildAssetPath(cb.currentCR.Namespace, tlsCfg.CA.Name(), tlsCfg.CA.Key())
		if err := cb.fetchSecretWithAssets(tlsCfg.CA.Secret, tlsCfg.CA.ConfigMap, assetKey); err != nil {
			return nil, fmt.Errorf("cannot fetch ca: %w", err)
		}
		toYamlString("ca_file", tlsCfg.BuildAssetPath(pathPrefix, tlsCfg.CA.Name(), tlsCfg.CA.Key()))
	}

	if tlsCfg.CertFile != "" {
		toYamlString("cert_file", tlsCfg.CertFile)
	} else if tlsCfg.Cert.Name() != "" {
		assetKey := tlsCfg.BuildAssetPath(cb.currentCR.Namespace, tlsCfg.Cert.Name(), tlsCfg.Cert.Key())
		if err := cb.fetchSecretWithAssets(tlsCfg.Cert.Secret, tlsCfg.Cert.ConfigMap, assetKey); err != nil {
			return nil, fmt.Errorf("cannot fetch cert: %w", err)
		}
		toYamlString("cert_file", tlsCfg.BuildAssetPath(pathPrefix, tlsCfg.Cert.Name(), tlsCfg.Cert.Key()))
	}

	if tlsCfg.KeyFile != "" {
		toYamlString("key_file", tlsCfg.KeyFile)
	} else if tlsCfg.KeySecret != nil {
		assetKey := tlsCfg.BuildAssetPath(cb.currentCR.Namespace, tlsCfg.KeySecret.Name, tlsCfg.KeySecret.Key)
		if err := cb.fetchSecretWithAssets(tlsCfg.KeySecret, nil, assetKey); err != nil {
			return nil, fmt.Errorf("cannot fetch keySecret: %w", err)
		}
		toYamlString("key_file", tlsCfg.BuildAssetPath(pathPrefix, tlsCfg.KeySecret.Name, tlsCfg.KeySecret.Key))
	}
	toYamlString("server_name", tlsCfg.ServerName)
	if tlsCfg.InsecureSkipVerify {
		r = append(r, yaml.MapItem{Key: "insecure_skip_verify", Value: tlsCfg.InsecureSkipVerify})
	}
	return r, nil
}
