package v1alpha1

import (
	"encoding/json"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/converter"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
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
		ActiveTimeIntervals: promRoute.ActiveTimeIntervals,
		MuteTimeIntervals:   promRoute.MuteTimeIntervals,
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
			InhibitRules: convertInhibitRules(promAMCfg.Spec.InhibitRules),
		},
	}
	convertedRoute, err := convertRoute(promAMCfg.Spec.Route)
	if err != nil {
		return nil, fmt.Errorf("cannot convert prometheus alertmanager config: %s into vm, err: %w", promAMCfg.Name, err)
	}
	vamc.Spec.Route = convertedRoute
	convertedReceivers := convertReceivers(promAMCfg.Spec.Receivers)

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
	cs.Spec.Path = *promscrapeConfig.Spec.MetricsPath

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

func convertKVToMap(src []promv1alpha1.KeyValue) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))

	for _, kv := range src {
		dst[kv.Key] = kv.Value
	}
	return dst
}

func convertSliceStruct[T, D any](src []T, convert func(T) D) []D {
	if len(src) == 0 {
		return nil
	}
	dst := make([]D, 0, len(src))
	for _, obj := range src {
		dst = append(dst, convert(obj))
	}
	return dst
}

func convertReceivers(promReceivers []promv1alpha1.Receiver) []vmv1beta1.Receiver {
	vmReceivers := make([]vmv1beta1.Receiver, 0, len(promReceivers))

	for _, promR := range promReceivers {
		dst := vmv1beta1.Receiver{
			Name:             promR.Name,
			EmailConfigs:     make([]vmv1beta1.EmailConfig, 0, len(promR.EmailConfigs)),
			PagerDutyConfigs: make([]vmv1beta1.PagerDutyConfig, 0, len(promR.PagerDutyConfigs)),
			PushoverConfigs:  make([]vmv1beta1.PushoverConfig, 0, len(promR.PushoverConfigs)),
			SlackConfigs:     make([]vmv1beta1.SlackConfig, 0, len(promR.SlackConfigs)),
			OpsGenieConfigs:  make([]vmv1beta1.OpsGenieConfig, 0, len(promR.OpsGenieConfigs)),
			WebhookConfigs:   make([]vmv1beta1.WebhookConfig, 0, len(promR.WebhookConfigs)),
			VictorOpsConfigs: make([]vmv1beta1.VictorOpsConfig, 0, len(promR.VictorOpsConfigs)),
			WeChatConfigs:    make([]vmv1beta1.WeChatConfig, 0, len(promR.WeChatConfigs)),
			TelegramConfigs:  make([]vmv1beta1.TelegramConfig, 0, len(promR.TelegramConfigs)),
			MSTeamsConfigs:   make([]vmv1beta1.MSTeamsConfig, 0, len(promR.MSTeamsConfigs)),
			DiscordConfigs:   make([]vmv1beta1.DiscordConfig, 0, len(promR.DiscordConfigs)),
			SNSConfigs:       make([]vmv1beta1.SnsConfig, 0, len(promR.SNSConfigs)),
			WebexConfigs:     make([]vmv1beta1.WebexConfig, 0, len(promR.WebexConfigs)),
		}
		for _, obj := range promR.EmailConfigs {
			vo := vmv1beta1.EmailConfig{
				SendResolved: obj.SendResolved,
				RequireTLS:   obj.RequireTLS,
				TLSConfig:    converter.ConvertSafeTLSConfig(obj.TLSConfig),
				To:           obj.To,
				From:         obj.From,
				Hello:        obj.Hello,
				Smarthost:    obj.Smarthost,
				AuthUsername: obj.AuthUsername,
				AuthPassword: obj.AuthPassword,
				AuthSecret:   obj.AuthSecret,
				AuthIdentity: obj.AuthIdentity,
				Headers:      convertKVToMap(obj.Headers),
				HTML:         ptr.Deref(obj.HTML, ""),
				Text:         ptr.Deref(obj.Text, ""),
			}
			dst.EmailConfigs = append(dst.EmailConfigs, vo)
		}
		for _, obj := range promR.PagerDutyConfigs {
			vo := vmv1beta1.PagerDutyConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				RoutingKey:   obj.RoutingKey,
				ServiceKey:   obj.ServiceKey,
				URL:          obj.URL,
				Client:       obj.Client,
				ClientURL:    obj.ClientURL,
				Images: convertSliceStruct(obj.PagerDutyImageConfigs, func(s promv1alpha1.PagerDutyImageConfig) vmv1beta1.ImageConfig {
					return vmv1beta1.ImageConfig{
						Href:   s.Href,
						Source: s.Src,
						Alt:    s.Alt,
					}
				}),
				Links: convertSliceStruct(obj.PagerDutyLinkConfigs, func(s promv1alpha1.PagerDutyLinkConfig) vmv1beta1.LinkConfig {
					return vmv1beta1.LinkConfig{
						Href: s.Href,
						Text: s.Text,
					}
				}),
				Description: obj.Description,
				Severity:    obj.Severity,
				Class:       obj.Class,
				Group:       obj.Group,
				Component:   obj.Component,
				Details:     convertKVToMap(obj.Details),
			}
			dst.PagerDutyConfigs = append(dst.PagerDutyConfigs, vo)
		}
		for _, obj := range promR.PushoverConfigs {
			vo := vmv1beta1.PushoverConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				UserKey:      obj.UserKey,
				Token:        obj.Token,
				Title:        obj.Title,
				Message:      obj.Message,
				URL:          obj.URL,
				URLTitle:     obj.URLTitle,
				Sound:        obj.Sound,
				Priority:     obj.Priority,
				Retry:        obj.Retry,
				Expire:       obj.Expire,
				HTML:         obj.HTML,
			}
			dst.PushoverConfigs = append(dst.PushoverConfigs, vo)
		}
		for _, obj := range promR.SlackConfigs {
			vo := vmv1beta1.SlackConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				APIURL:       obj.APIURL,
				Channel:      obj.Channel,
				Username:     obj.Username,
				Color:        obj.Color,
				Title:        obj.Title,
				TitleLink:    obj.TitleLink,
				Pretext:      obj.Pretext,
				Text:         obj.Text,
				Fields: convertSliceStruct(obj.Fields, func(s promv1alpha1.SlackField) vmv1beta1.SlackField {
					return vmv1beta1.SlackField{
						Title: s.Title,
						Value: s.Value,
						Short: s.Short,
					}
				}),
				ShortFields: obj.ShortFields,
				Footer:      obj.Footer,
				Fallback:    obj.Fallback,
				CallbackID:  obj.CallbackID,
				IconEmoji:   obj.IconEmoji,
				IconURL:     obj.IconURL,
				ImageURL:    obj.ImageURL,
				ThumbURL:    obj.ThumbURL,
				LinkNames:   obj.LinkNames,
				MrkdwnIn:    obj.MrkdwnIn,
				Actions: convertSliceStruct(obj.Actions, func(s promv1alpha1.SlackAction) vmv1beta1.SlackAction {
					return vmv1beta1.SlackAction{
						Type:  s.Type,
						Text:  s.Text,
						URL:   s.URL,
						Style: s.Style,
						Name:  s.Name,
						Value: s.Value,
						ConfirmField: func() *vmv1beta1.SlackConfirmationField {
							if s.ConfirmField == nil {
								return nil
							}
							return &vmv1beta1.SlackConfirmationField{Text: s.ConfirmField.Text, OkText: s.ConfirmField.OkText}
						}(),
					}
				}),
			}
			dst.SlackConfigs = append(dst.SlackConfigs, vo)
		}
		for _, obj := range promR.OpsGenieConfigs {
			vo := vmv1beta1.OpsGenieConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				APIKey:       obj.APIKey,
				APIURL:       obj.APIURL,
				Message:      obj.Message,
				Description:  obj.Description,
				Source:       obj.Source,
				Tags:         obj.Tags,
				Note:         obj.Note,
				Priority:     obj.Priority,
				Details:      convertKVToMap(obj.Details),
				Responders: convertSliceStruct(obj.Responders, func(s promv1alpha1.OpsGenieConfigResponder) vmv1beta1.OpsGenieConfigResponder {
					return vmv1beta1.OpsGenieConfigResponder{
						ID:       s.ID,
						Name:     s.Name,
						Username: s.Username,
						Type:     s.Type,
					}
				}),
				Entity:       obj.Entity,
				Actions:      obj.Actions,
				UpdateAlerts: ptr.Deref(obj.UpdateAlerts, false),
			}
			dst.OpsGenieConfigs = append(dst.OpsGenieConfigs, vo)
		}

		for _, obj := range promR.WebhookConfigs {
			vo := vmv1beta1.WebhookConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				URL:          obj.URL,
				URLSecret:    obj.URLSecret,
				MaxAlerts:    obj.MaxAlerts,
			}
			dst.WebhookConfigs = append(dst.WebhookConfigs, vo)
		}
		for _, obj := range promR.VictorOpsConfigs {
			vo := vmv1beta1.VictorOpsConfig{
				SendResolved:      obj.SendResolved,
				HTTPConfig:        convertHTTPConfig(obj.HTTPConfig),
				APIKey:            obj.APIKey,
				APIURL:            obj.APIURL,
				RoutingKey:        obj.RoutingKey,
				MessageType:       obj.MessageType,
				EntityDisplayName: obj.EntityDisplayName,
				StateMessage:      obj.StateMessage,
				MonitoringTool:    obj.MonitoringTool,
				CustomFields:      convertKVToMap(obj.CustomFields),
			}
			dst.VictorOpsConfigs = append(dst.VictorOpsConfigs, vo)
		}
		for _, obj := range promR.WeChatConfigs {
			vo := vmv1beta1.WeChatConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				APISecret:    obj.APISecret,
				APIURL:       obj.APIURL,
				CorpID:       obj.CorpID,
				AgentID:      obj.AgentID,
				ToUser:       obj.ToUser,
				ToParty:      obj.ToParty,
				ToTag:        obj.ToTag,
				Message:      obj.Message,
				MessageType:  obj.MessageType,
			}
			dst.WeChatConfigs = append(dst.WeChatConfigs, vo)
		}
		for _, obj := range promR.TelegramConfigs {
			vo := vmv1beta1.TelegramConfig{
				SendResolved:         obj.SendResolved,
				HTTPConfig:           convertHTTPConfig(obj.HTTPConfig),
				APIUrl:               obj.APIURL,
				BotToken:             obj.BotToken,
				ChatID:               int(obj.ChatID),
				Message:              obj.Message,
				DisableNotifications: obj.DisableNotifications,
				ParseMode:            obj.ParseMode,
			}
			dst.TelegramConfigs = append(dst.TelegramConfigs, vo)
		}
		for _, obj := range promR.MSTeamsConfigs {
			vo := vmv1beta1.MSTeamsConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				URLSecret:    &obj.WebhookURL,
				Title:        ptr.Deref(obj.Title, ""),
				Text:         ptr.Deref(obj.Text, ""),
			}
			dst.MSTeamsConfigs = append(dst.MSTeamsConfigs, vo)
		}
		for _, obj := range promR.DiscordConfigs {
			vo := vmv1beta1.DiscordConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				URLSecret:    &obj.APIURL,
				Title:        ptr.Deref(obj.Title, ""),
				Message:      ptr.Deref(obj.Message, ""),
			}
			dst.DiscordConfigs = append(dst.DiscordConfigs, vo)
		}
		for _, obj := range promR.SNSConfigs {
			vo := vmv1beta1.SnsConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				URL:          obj.ApiURL,
				Sigv4: func() *vmv1beta1.Sigv4Config {
					if obj.Sigv4 == nil {
						return nil
					}
					return &vmv1beta1.Sigv4Config{
						Region:            obj.Sigv4.Region,
						AccessKeySelector: obj.Sigv4.AccessKey,
						SecretKey:         obj.Sigv4.SecretKey,
						Profile:           obj.Sigv4.Profile,
						RoleArn:           obj.Sigv4.RoleArn,
					}
				}(),
				TopicArn:    obj.TopicARN,
				Subject:     obj.Subject,
				PhoneNumber: obj.PhoneNumber,
				TargetArn:   obj.TargetARN,
				Message:     obj.Message,
				Attributes:  obj.Attributes,
			}
			dst.SNSConfigs = append(dst.SNSConfigs, vo)
		}
		for _, obj := range promR.WebexConfigs {
			vo := vmv1beta1.WebexConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				URL: func() *string {
					if obj.APIURL == nil {
						return nil
					}
					st := string(*obj.APIURL)
					return &st
				}(),
				RoomId:  obj.RoomID,
				Message: ptr.Deref(obj.Message, ""),
			}
			dst.WebexConfigs = append(dst.WebexConfigs, vo)
		}

		vmReceivers = append(vmReceivers, dst)
	}

	return vmReceivers
}

func convertHTTPConfig(prom *promv1alpha1.HTTPConfig) *vmv1beta1.HTTPConfig {
	if prom == nil {
		return nil
	}
	hc := &vmv1beta1.HTTPConfig{
		BasicAuth:         converter.ConvertBasicAuth(prom.BasicAuth),
		BearerTokenSecret: prom.BearerTokenSecret,
		TLSConfig:         converter.ConvertSafeTLSConfig(prom.TLSConfig),
		ProxyURL:          prom.ProxyURL,
		Authorization:     converter.ConvertAuthorization(prom.Authorization, nil),
		OAuth2:            converter.ConvertOAuth(prom.OAuth2),
	}
	return hc
}
