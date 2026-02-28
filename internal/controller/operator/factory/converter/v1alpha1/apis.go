package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/converter"
)

func convertPromMatcher(promMatcher promv1alpha1.Matcher) string {
	if promMatcher.MatchType == "" {
		promMatcher.MatchType = "="
		if len(promMatcher.Value) > 0 {
			promMatcher.MatchType = "=~"
		}
	}
	return promMatcher.String()
}

func convertPromRoute(promRoute *promv1alpha1.Route) (*vmv1beta1.Route, error) {
	if promRoute == nil {
		return nil, nil
	}
	r := vmv1beta1.Route{
		Receiver:            promRoute.Receiver,
		Continue:            promRoute.Continue,
		GroupBy:             promRoute.GroupBy,
		GroupWait:           string(ptr.Deref(promRoute.GroupWait, "")),
		GroupInterval:       string(ptr.Deref(promRoute.GroupInterval, "")),
		RepeatInterval:      string(ptr.Deref(promRoute.RepeatInterval, "")),
		Matchers:            convertSliceStruct(promRoute.Matchers, convertPromMatcher),
		ActiveTimeIntervals: promRoute.ActiveTimeIntervals,
		MuteTimeIntervals:   promRoute.MuteTimeIntervals,
	}
	for _, route := range promRoute.Routes {
		var promRoute promv1alpha1.Route
		if err := json.Unmarshal(route.Raw, &promRoute); err != nil {
			return nil, fmt.Errorf("cannot parse raw prom route: %s, err: %w", string(route.Raw), err)
		}
		vmRoute, err := convertPromRoute(&promRoute)
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

func convertPromTimeRange(promTR promv1alpha1.TimeRange) vmv1beta1.TimeRange {
	return vmv1beta1.TimeRange{
		StartTime: string(promTR.StartTime),
		EndTime:   string(promTR.EndTime),
	}
}

func convertPromRangeToString[T ~string](s T) string {
	return string(s)
}

func convertPromDayOfMonth(promDoM promv1alpha1.DayOfMonthRange) string {
	return fmt.Sprintf("%d:%d", promDoM.Start, promDoM.End)
}

func convertPromTimeInterval(promTI promv1alpha1.TimeInterval) vmv1beta1.TimeInterval {
	return vmv1beta1.TimeInterval{
		Times:       convertSliceStruct(promTI.Times, convertPromTimeRange),
		Weekdays:    convertSliceStruct(promTI.Weekdays, convertPromRangeToString),
		DaysOfMonth: convertSliceStruct(promTI.DaysOfMonth, convertPromDayOfMonth),
		Months:      convertSliceStruct(promTI.Months, convertPromRangeToString),
		Years:       convertSliceStruct(promTI.Years, convertPromRangeToString),
	}
}

func convertPromMuteTimeInterval(promTI promv1alpha1.MuteTimeInterval) vmv1beta1.TimeIntervals {
	return vmv1beta1.TimeIntervals{
		Name:          promTI.Name,
		TimeIntervals: convertSliceStruct(promTI.TimeIntervals, convertPromTimeInterval),
	}
}

func convertPromInhibitRule(promIR promv1alpha1.InhibitRule) vmv1beta1.InhibitRule {
	return vmv1beta1.InhibitRule{
		TargetMatchers: convertSliceStruct(promIR.TargetMatch, convertPromMatcher),
		SourceMatchers: convertSliceStruct(promIR.SourceMatch, convertPromMatcher),
		Equal:          promIR.Equal,
	}
}

// AlertmanagerConfig creates VMAlertmanagerConfig from prometheus alertmanagerConfig
func AlertmanagerConfig(promAMCfg *promv1alpha1.AlertmanagerConfig, conf *config.BaseOperatorConf) (*vmv1beta1.VMAlertmanagerConfig, error) {
	convertedRoute, err := convertPromRoute(promAMCfg.Spec.Route)
	if err != nil {
		return nil, fmt.Errorf("cannot convert prometheus alertmanager config: %s into vm, err: %w", promAMCfg.Name, err)
	}
	vamc := &vmv1beta1.VMAlertmanagerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        promAMCfg.Name,
			Namespace:   promAMCfg.Namespace,
			Annotations: converter.FilterPrefixes(promAMCfg.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
			Labels:      converter.FilterPrefixes(promAMCfg.Labels, conf.FilterPrometheusConverterLabelPrefixes),
		},
		Spec: vmv1beta1.VMAlertmanagerConfigSpec{
			Route:         convertedRoute,
			Receivers:     convertReceivers(promAMCfg.Spec.Receivers),
			InhibitRules:  convertSliceStruct(promAMCfg.Spec.InhibitRules, convertPromInhibitRule),
			TimeIntervals: convertSliceStruct(promAMCfg.Spec.MuteTimeIntervals, convertPromMuteTimeInterval),
		},
	}
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

// ScrapeConfig creates VMScrapeConfig from prometheus scrapeConfig
func ScrapeConfig(ctx context.Context, src *promv1alpha1.ScrapeConfig, conf *config.BaseOperatorConf) (*vmv1beta1.VMScrapeConfig, error) {
	cs := &vmv1beta1.VMScrapeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:        src.Name,
			Namespace:   src.Namespace,
			Labels:      converter.FilterPrefixes(src.Labels, conf.FilterPrometheusConverterLabelPrefixes),
			Annotations: converter.FilterPrefixes(src.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes),
		},
	}
	data, err := json.Marshal(src.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal prometheus scrapeconfig, name=%s, namespace=%s: %w", src.Name, src.Namespace, err)
	}
	err = json.Unmarshal(data, &cs.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to convert prometheus scrapeconfig, name=%s, namespace=%s: %w", src.Name, src.Namespace, err)
	}
	cs.Labels = converter.FilterPrefixes(src.Labels, conf.FilterPrometheusConverterLabelPrefixes)
	cs.Annotations = converter.FilterPrefixes(src.Annotations, conf.FilterPrometheusConverterAnnotationPrefixes)
	cs.Spec.RelabelConfigs = converter.ConvertRelabelConfig(ctx, src.Spec.RelabelConfigs)
	cs.Spec.MetricRelabelConfigs = converter.ConvertRelabelConfig(ctx, src.Spec.MetricRelabelConfigs)
	cs.Spec.Path = ptr.Deref(src.Spec.MetricsPath, "")
	for i := range cs.Spec.KubernetesSDConfigs {
		sdCfg := &cs.Spec.KubernetesSDConfigs[i]
		sdCfg.Role = strings.ToLower(sdCfg.Role)
		for j := range sdCfg.Selectors {
			selector := &sdCfg.Selectors[j]
			selector.Role = strings.ToLower(selector.Role)
		}
	}
	if src.Spec.ScrapeInterval != nil {
		cs.Spec.ScrapeInterval = string(*src.Spec.ScrapeInterval)
	}

	if src.Spec.EnableCompression != nil {
		cs.Spec.VMScrapeParams = &vmv1beta1.VMScrapeParams{
			DisableCompression: ptr.To(!*src.Spec.EnableCompression),
		}
	}
	if conf.EnabledPrometheusConverterOwnerReferences {
		cs.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         promv1alpha1.SchemeGroupVersion.String(),
				Kind:               promv1alpha1.ScrapeConfigsKind,
				Name:               src.Name,
				UID:                src.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		}
	}
	cs.Annotations = converter.MaybeAddArgoCDIgnoreAnnotations(conf.PrometheusConverterAddArgoCDIgnoreAnnotations, cs.Annotations)
	return cs, nil
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
			MSTeamsV2Configs: make([]vmv1beta1.MSTeamsV2Config, 0, len(promR.MSTeamsV2Configs)),
		}
		for _, obj := range promR.EmailConfigs {
			vo := vmv1beta1.EmailConfig{
				SendResolved: obj.SendResolved,
				RequireTLS:   obj.RequireTLS,
				TLSConfig:    converter.ConvertSafeTLSConfig(obj.TLSConfig),
				To:           ptr.Deref(obj.To, ""),
				From:         ptr.Deref(obj.From, ""),
				Hello:        ptr.Deref(obj.Hello, ""),
				Smarthost:    ptr.Deref(obj.Smarthost, ""),
				AuthUsername: ptr.Deref(obj.AuthUsername, ""),
				AuthPassword: obj.AuthPassword,
				AuthSecret:   obj.AuthSecret,
				AuthIdentity: ptr.Deref(obj.AuthIdentity, ""),
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
				URL:          string(ptr.Deref(obj.URL, "")),
				Client:       ptr.Deref(obj.Client, ""),
				ClientURL:    ptr.Deref(obj.ClientURL, ""),
				Images: convertSliceStruct(obj.PagerDutyImageConfigs, func(s promv1alpha1.PagerDutyImageConfig) vmv1beta1.ImageConfig {
					return vmv1beta1.ImageConfig{
						Href:   ptr.Deref(s.Href, ""),
						Source: ptr.Deref(s.Src, ""),
						Alt:    ptr.Deref(s.Alt, ""),
					}
				}),
				Links: convertSliceStruct(obj.PagerDutyLinkConfigs, func(s promv1alpha1.PagerDutyLinkConfig) vmv1beta1.LinkConfig {
					return vmv1beta1.LinkConfig{
						Href: ptr.Deref(s.Href, ""),
						Text: ptr.Deref(s.Text, ""),
					}
				}),
				Description: ptr.Deref(obj.Description, ""),
				Severity:    ptr.Deref(obj.Severity, ""),
				Class:       ptr.Deref(obj.Class, ""),
				Group:       ptr.Deref(obj.Group, ""),
				Component:   ptr.Deref(obj.Component, ""),
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
				Title:        ptr.Deref(obj.Title, ""),
				Message:      ptr.Deref(obj.Message, ""),
				URL:          obj.URL,
				URLTitle:     ptr.Deref(obj.URLTitle, ""),
				Sound:        ptr.Deref(obj.Sound, ""),
				Priority:     ptr.Deref(obj.Priority, ""),
				Retry:        ptr.Deref(obj.Retry, ""),
				Expire:       ptr.Deref(obj.Expire, ""),
				HTML:         ptr.Deref(obj.HTML, false),
			}
			dst.PushoverConfigs = append(dst.PushoverConfigs, vo)
		}
		for _, obj := range promR.SlackConfigs {
			vo := vmv1beta1.SlackConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				APIURL:       obj.APIURL,
				Channel:      ptr.Deref(obj.Channel, ""),
				Username:     ptr.Deref(obj.Username, ""),
				Color:        ptr.Deref(obj.Color, ""),
				Title:        ptr.Deref(obj.Title, ""),
				TitleLink:    obj.TitleLink,
				Pretext:      ptr.Deref(obj.Pretext, ""),
				Text:         ptr.Deref(obj.Text, ""),
				Fields: convertSliceStruct(obj.Fields, func(s promv1alpha1.SlackField) vmv1beta1.SlackField {
					return vmv1beta1.SlackField{
						Title: s.Title,
						Value: s.Value,
						Short: s.Short,
					}
				}),
				ShortFields: ptr.Deref(obj.ShortFields, false),
				Footer:      ptr.Deref(obj.Footer, ""),
				Fallback:    ptr.Deref(obj.Fallback, ""),
				CallbackID:  ptr.Deref(obj.CallbackID, ""),
				IconEmoji:   ptr.Deref(obj.IconEmoji, ""),
				IconURL:     obj.IconURL,
				ImageURL:    obj.ImageURL,
				ThumbURL:    obj.ThumbURL,
				LinkNames:   ptr.Deref(obj.LinkNames, false),
				MrkdwnIn:    obj.MrkdwnIn,
				Actions: convertSliceStruct(obj.Actions, func(s promv1alpha1.SlackAction) vmv1beta1.SlackAction {
					return vmv1beta1.SlackAction{
						Type:  s.Type,
						Text:  s.Text,
						URL:   s.URL,
						Style: ptr.Deref(s.Style, ""),
						Name:  ptr.Deref(s.Name, ""),
						Value: ptr.Deref(s.Value, ""),
						ConfirmField: func() *vmv1beta1.SlackConfirmationField {
							if s.ConfirmField == nil {
								return nil
							}
							return &vmv1beta1.SlackConfirmationField{
								Text:   s.ConfirmField.Text,
								OkText: ptr.Deref(s.ConfirmField.OkText, ""),
							}
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
				APIURL:       string(ptr.Deref(obj.APIURL, "")),
				Message:      ptr.Deref(obj.Message, ""),
				Description:  ptr.Deref(obj.Description, ""),
				Source:       ptr.Deref(obj.Source, ""),
				Tags:         ptr.Deref(obj.Tags, ""),
				Note:         ptr.Deref(obj.Note, ""),
				Priority:     ptr.Deref(obj.Priority, ""),
				Details:      convertKVToMap(obj.Details),
				Responders: convertSliceStruct(obj.Responders, func(s promv1alpha1.OpsGenieConfigResponder) vmv1beta1.OpsGenieConfigResponder {
					return vmv1beta1.OpsGenieConfigResponder{
						ID:       ptr.Deref(s.ID, ""),
						Name:     ptr.Deref(s.Name, ""),
						Username: ptr.Deref(s.Username, ""),
						Type:     s.Type,
					}
				}),
				Entity:       ptr.Deref(obj.Entity, ""),
				Actions:      ptr.Deref(obj.Actions, ""),
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
				Timeout:      ptr.Deref((*string)(obj.Timeout), ""),
			}
			dst.WebhookConfigs = append(dst.WebhookConfigs, vo)
		}
		for _, obj := range promR.VictorOpsConfigs {
			vo := vmv1beta1.VictorOpsConfig{
				SendResolved:      obj.SendResolved,
				HTTPConfig:        convertHTTPConfig(obj.HTTPConfig),
				APIKey:            obj.APIKey,
				APIURL:            string(ptr.Deref(obj.APIURL, "")),
				RoutingKey:        obj.RoutingKey,
				MessageType:       ptr.Deref(obj.MessageType, ""),
				EntityDisplayName: ptr.Deref(obj.EntityDisplayName, ""),
				StateMessage:      ptr.Deref(obj.StateMessage, ""),
				MonitoringTool:    ptr.Deref(obj.MonitoringTool, ""),
				CustomFields:      convertKVToMap(obj.CustomFields),
			}
			dst.VictorOpsConfigs = append(dst.VictorOpsConfigs, vo)
		}
		for _, obj := range promR.WeChatConfigs {
			vo := vmv1beta1.WeChatConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				APISecret:    obj.APISecret,
				APIURL:       string(ptr.Deref(obj.APIURL, "")),
				CorpID:       ptr.Deref(obj.CorpID, ""),
				AgentID:      ptr.Deref(obj.AgentID, ""),
				ToUser:       ptr.Deref(obj.ToUser, ""),
				ToParty:      ptr.Deref(obj.ToParty, ""),
				ToTag:        ptr.Deref(obj.ToTag, ""),
				Message:      ptr.Deref(obj.Message, ""),
				MessageType:  ptr.Deref(obj.MessageType, ""),
			}
			dst.WeChatConfigs = append(dst.WeChatConfigs, vo)
		}
		for _, obj := range promR.TelegramConfigs {
			vo := vmv1beta1.TelegramConfig{
				SendResolved:         obj.SendResolved,
				HTTPConfig:           convertHTTPConfig(obj.HTTPConfig),
				APIUrl:               string(ptr.Deref(obj.APIURL, "")),
				BotToken:             obj.BotToken,
				ChatID:               int(obj.ChatID),
				Message:              obj.Message,
				DisableNotifications: obj.DisableNotifications,
				ParseMode:            obj.ParseMode,
			}

			if obj.MessageThreadID != nil {
				vo.MessageThreadID = int(*obj.MessageThreadID)
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
				Username:     ptr.Deref(obj.Username, ""),
				Content:      ptr.Deref(obj.Content, ""),
				AvatarURL:    ptr.Deref((*string)(obj.AvatarURL), ""),
			}
			dst.DiscordConfigs = append(dst.DiscordConfigs, vo)
		}
		for _, obj := range promR.SNSConfigs {
			vo := vmv1beta1.SnsConfig{
				SendResolved: obj.SendResolved,
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
				URL:          ptr.Deref(obj.ApiURL, ""),
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
				TopicArn:    ptr.Deref(obj.TopicARN, ""),
				Subject:     ptr.Deref(obj.Subject, ""),
				PhoneNumber: ptr.Deref(obj.PhoneNumber, ""),
				TargetArn:   ptr.Deref(obj.TargetARN, ""),
				Message:     ptr.Deref(obj.Message, ""),
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
		for _, obj := range promR.MSTeamsV2Configs {
			vo := vmv1beta1.MSTeamsV2Config{
				SendResolved: obj.SendResolved,
				URLSecret:    obj.WebhookURL,
				Title:        ptr.Deref(obj.Title, ""),
				Text:         ptr.Deref(obj.Text, ""),
				HTTPConfig:   convertHTTPConfig(obj.HTTPConfig),
			}
			dst.MSTeamsV2Configs = append(dst.MSTeamsV2Configs, vo)
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
		ProxyURL:          ptr.Deref(prom.ProxyURL, ""),
		Authorization:     converter.ConvertAuthorization(prom.Authorization, nil),
		OAuth2:            converter.ConvertOAuth(prom.OAuth2),
	}
	// Add fallback to proxyUrl
	if len(hc.ProxyURL) == 0 && prom.ProxyURLOriginal != nil {
		hc.ProxyURL = *prom.ProxyURLOriginal
	}
	return hc
}
