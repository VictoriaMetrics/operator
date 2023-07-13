/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VMAlertmanagerConfigSpec defines configuration for VMAlertmanagerConfig
type VMAlertmanagerConfigSpec struct {
	// Route definition for alertmanager, may include nested routes.
	// +optional
	Route *Route `json:"route"`
	// Receivers defines alert receivers.
	// without defined Route, receivers will be skipped.
	// +optional
	Receivers []Receiver `json:"receivers"`
	// InhibitRules will only apply for alerts matching
	// the resource's namespace.
	// +optional
	InhibitRules []InhibitRule `json:"inhibit_rules,omitempty"`

	// MuteTimeInterval - global mute time
	// See https://prometheus.io/docs/alerting/latest/configuration/#mute_time_interval
	// +optional
	MutTimeIntervals []MuteTimeInterval `json:"mute_time_intervals,omitempty"`
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	// TimeIntervals modern config option, use it instead of  mute_time_intervals
	// +optional
	TimeIntervals []MuteTimeInterval `json:"time_intervals,omitempty"`

	ParsingError string `json:"-,omitempty" yaml:"-,omitempty"`
}

// MuteTimeInterval for alerts
type MuteTimeInterval struct {
	// Name of interval
	// +required
	Name string `json:"name,omitempty"`
	// TimeIntervals interval configuration
	// +required
	TimeIntervals []TimeInterval `json:"time_intervals"`
}

// TimeInterval defines intervals of time
type TimeInterval struct {
	// Times defines time range for mute
	// +optional
	Times []TimeRange `json:"times,omitempty"`
	// Weekdays defines list of days of the week, where the week begins on Sunday and ends on Saturday.
	// +optional
	Weekdays []string `json:"weekdays,omitempty"`
	// DayOfMonth defines list of numerical days in the month. Days begin at 1. Negative values are also accepted.
	// for example, ['1:5', '-3:-1']
	// +optional
	DaysOfMonth []string `json:"days_of_month,omitempty"`
	// Months  defines list of calendar months identified by a case-insentive name (e.g. ‘January’) or numeric 1.
	// For example, ['1:3', 'may:august', 'december']
	// +optional
	Months []string `json:"months,omitempty"`
	// Years defines numerical list of years, ranges are accepted.
	// For example, ['2020:2022', '2030']
	// +optional
	Years []string `json:"years,omitempty"`
	// Location in golang time location form, e.g. UTC
	// +optional
	Location string `json:"location,omitempty"`
}

// TimeRange  ranges inclusive of the starting time and exclusive of the end time
type TimeRange struct {
	// StartTime for example  HH:MM
	// +required
	StartTime string `json:"start_time"`
	// EndTime for example HH:MM
	// +required
	EndTime string `json:"end_time"`
}

// VMAlertmanagerConfigStatus defines the observed state of VMAlertmanagerConfig
type VMAlertmanagerConfigStatus struct {
	// ErrorReason describes validation or any other errors.
	ErrorReason string `json:"reason,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +genclient
// +k8s:openapi-gen=true
// VMAlertmanagerConfig is the Schema for the vmalertmanagerconfigs API
type VMAlertmanagerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAlertmanagerConfigSpec   `json:"spec,omitempty"`
	Status VMAlertmanagerConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMAlertmanagerConfigList contains a list of VMAlertmanagerConfig
type VMAlertmanagerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAlertmanagerConfig `json:"items"`
}

// Route defines a node in the routing tree.
type Route struct {
	// Name of the receiver for this route.
	// +required
	Receiver string `json:"receiver"`
	// List of labels to group by.
	// +optional
	GroupBy []string `json:"group_by,omitempty"`
	// How long to wait before sending the initial notification.
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +optional
	GroupWait string `json:"group_wait,omitempty"`
	// How long to wait before sending an updated notification.
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +optional
	GroupInterval string `json:"group_interval,omitempty"`
	// How long to wait before repeating the last notification.
	// +kubebuilder:validation:Pattern:="[0-9]+(ms|s|m|h)"
	// +optional
	RepeatInterval string `json:"repeat_interval,omitempty"`
	// List of matchers that the alert’s labels should match. For the first
	// level route, the operator adds a namespace: "CRD_NS" matcher.
	// https://prometheus.io/docs/alerting/latest/configuration/#matcher
	// +optional
	Matchers []string `json:"matchers,omitempty"`
	// Continue indicating whether an alert should continue matching subsequent
	// sibling nodes. It will always be true for the first-level route if disableRouteContinueEnforce for vmalertmanager not set.
	// +optional
	Continue bool `json:"continue,omitempty"`
	// Child routes.
	// CRD schema doesn't support self-referential types for now (see https://github.com/kubernetes/kubernetes/issues/62872).
	// We expose below RawRoutes as an alternative type to circumvent the limitation, and use Routes in code.
	Routes []*Route `json:"-,omitempty"`
	// Child routes.
	// https://prometheus.io/docs/alerting/latest/configuration/#route
	RawRoutes []apiextensionsv1.JSON `json:"routes,omitempty"`
	// MuteTimeIntervals for alerts
	// +optional
	MuteTimeIntervals []string `json:"mute_time_intervals,omitempty"`
	// ActiveTimeIntervals Times when the route should be active
	// These must match the name at time_intervals
	// +optional
	ActiveTimeIntervals []string `json:"active_time_intervals,omitempty"`
}

func parseNestedRoutes(src *Route) error {
	if src == nil {
		return nil
	}
	for _, nestedRoute := range src.RawRoutes {
		var route Route
		if err := json.Unmarshal(nestedRoute.Raw, &route); err != nil {
			return fmt.Errorf("cannot parse json value: %s for nested route, err :%w", string(nestedRoute.Raw), err)
		}
		if err := parseNestedRoutes(&route); err != nil {
			return err
		}
		src.Routes = append(src.Routes, &route)
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAlertmanagerConfig) UnmarshalJSON(src []byte) error {
	type amcfg VMAlertmanagerConfig
	if err := json.Unmarshal(src, (*amcfg)(cr)); err != nil {
		cr.Spec.ParsingError = fmt.Sprintf("cannot parse alertmanager config: %s, err: %s", string(src), err)
		return nil
	}
	if err := parseNestedRoutes(cr.Spec.Route); err != nil {
		cr.Spec.ParsingError = fmt.Sprintf("cannot parse routes for alertmanager config: %s at namespace: %s, err: %s", cr.Name, cr.Namespace, err)
		return nil
	}
	return nil
}

// InhibitRule defines an inhibition rule that allows to mute alerts when other
// alerts are already firing.
// Note, it doesn't support deprecated alertmanager config options.
// See https://prometheus.io/docs/alerting/latest/configuration/#inhibit_rule
type InhibitRule struct {
	// TargetMatchers defines a list of matchers that have to be fulfilled by the target
	// alerts to be muted.
	// +optional
	TargetMatchers []string `json:"target_matchers,omitempty"`
	// SourceMatchers defines a list of matchers for which one or more alerts have
	// to exist for the inhibition to take effect.
	// +optional
	SourceMatchers []string `json:"source_matchers,omitempty"`

	// Labels that must have an equal value in the source and target alert for
	// the inhibition to take effect.
	// +optional
	Equal []string `json:"equal,omitempty"`
}

// Receiver defines one or more notification integrations.
type Receiver struct {
	// Name of the receiver. Must be unique across all items from the list.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// EmailConfigs defines email notification configurations.
	// +optional
	EmailConfigs []EmailConfig `json:"email_configs,omitempty"`
	// PagerDutyConfigs defines pager duty notification configurations.
	// +optional
	PagerDutyConfigs []PagerDutyConfig `json:"pagerduty_configs,omitempty"`
	// PushoverConfigs defines push over notification configurations.
	// +optional
	PushoverConfigs []PushoverConfig `json:"pushover_configs,omitempty"`
	// SlackConfigs defines slack notification configurations.
	// +optional
	SlackConfigs []SlackConfig `json:"slack_configs,omitempty"`
	// OpsGenieConfigs defines ops genie notification configurations.
	// +optional
	OpsGenieConfigs []OpsGenieConfig `json:"opsgenie_configs,omitempty"`
	// WebhookConfigs defines webhook notification configurations.
	// +optional
	WebhookConfigs []WebhookConfig `json:"webhook_configs,omitempty"`

	// VictorOpsConfigs defines victor ops notification configurations.
	// +optional
	VictorOpsConfigs []VictorOpsConfig `json:"victorops_configs,omitempty"`
	// WeChatConfigs defines wechat notification configurations.
	// +optional
	WeChatConfigs   []WeChatConfig   `json:"wechat_configs,omitempty"`
	TelegramConfigs []TelegramConfig `json:"telegram_configs,omitempty"`
}

type TelegramConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// APIUrl the Telegram API URL i.e. https://api.telegram.org.
	// +optional
	APIUrl string `json:"api_url,omitempty"`
	// BotToken token for the bot
	// https://core.telegram.org/bots/api
	BotToken *v1.SecretKeySelector `json:"bot_token"`
	// ChatID is ID of the chat where to send the messages.
	ChatID int `json:"chat_id"`
	// Message is templated message
	// +optional
	Message string `json:"message,omitempty"`
	// DisableNotifications
	// +optional
	DisableNotifications *bool `json:"disable_notifications,omitempty"`
	// ParseMode for telegram message,
	// supported values are MarkdownV2, Markdown, Markdown and empty string for plain text.
	// +optional
	ParseMode string `json:"parse_mode,omitempty"`
	// HTTP client configuration.
	// +optional
	// HTTPAuth generic auth methods
	HTTPConfig *HTTPAuth `json:"http_config,omitempty"`
}

// WebhookConfig configures notifications via a generic receiver supporting the webhook payload.
// See https://prometheus.io/docs/alerting/latest/configuration/#webhook_config
type WebhookConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// URL to send requests to,
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URL *string `json:"url,omitempty"`
	// URLSecret defines secret name and key at the CRD namespace.
	// It must contain the webhook URL.
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URLSecret *v1.SecretKeySelector `json:"url_secret,omitempty"`
	// HTTP client configuration.
	// +optional
	// HTTPAuth generic auth methods
	HTTPConfig *HTTPAuth `json:"http_config,omitempty"`
	// Maximum number of alerts to be sent per webhook message. When 0, all alerts are included.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxAlerts int32 `json:"max_alerts,omitempty"`
}

// WeChatConfig configures notifications via WeChat.
// See https://prometheus.io/docs/alerting/latest/configuration/#wechat_config
type WeChatConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// The secret's key that contains the WeChat API key.
	// The secret needs to be in the same namespace as the AlertmanagerConfig
	// object and accessible by the Prometheus Operator.
	// +optional
	APISecret *v1.SecretKeySelector `json:"api_secret,omitempty"`
	// The WeChat API URL.
	// +optional
	APIURL string `json:"api_url,omitempty"`
	// The corp id for authentication.
	// +optional
	CorpID string `json:"corp_id,omitempty"`
	// +optional
	AgentID string `json:"agent_id,omitempty"`
	// +optional
	ToUser string `json:"to_user,omitempty"`
	// +optional
	ToParty string `json:"to_party,omitempty"`
	// +optional
	ToTag string `json:"to_tag,omitempty"`
	// API request data as defined by the WeChat API.
	Message string `json:"message,omitempty"`
	// +optional
	MessageType string `json:"message_type,omitempty"`
	// HTTP client configuration.
	// +optional
	// HTTPAuth generic auth methods
	HTTPConfig *HTTPAuth `json:"http_config,omitempty"`
}

// EmailConfig configures notifications via Email.
type EmailConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// The email address to send notifications to.
	// +optional
	To string `json:"to,omitempty"`
	// The sender address.
	// +optional
	From string `json:"from,omitempty"`
	// The hostname to identify to the SMTP server.
	// +optional
	Hello string `json:"hello,omitempty"`
	// The SMTP host through which emails are sent.
	// +optional
	Smarthost string `json:"smarthost,omitempty"`
	// The username to use for authentication.
	// +optional
	AuthUsername string `json:"auth_username,omitempty"`
	// AuthPassword defines secret name and key at CRD namespace.
	// +optional
	AuthPassword *v1.SecretKeySelector `json:"auth_password,omitempty"`
	// AuthSecret defines secrent name and key at CRD namespace.
	// It must contain the CRAM-MD5 secret.
	// +optional
	AuthSecret *v1.SecretKeySelector `json:"auth_secret,omitempty"`
	// The identity to use for authentication.
	// +optional
	AuthIdentity string `json:"auth_identity,omitempty"`
	// Further headers email header key/value pairs. Overrides any headers
	// previously set by the notification implementation.
	Headers EmailConfigHeaders `json:"headers,omitempty"`
	// The HTML body of the email notification.
	// +optional
	HTML string `json:"html,omitempty"`
	// The text body of the email notification.
	// +optional
	Text string `json:"text,omitempty"`
	// The SMTP TLS requirement.
	// Note that Go does not support unencrypted connections to remote SMTP endpoints.
	// +optional
	RequireTLS *bool `json:"require_tls,omitempty"`
	// TLS configuration
	// +optional
	TLSConfig *TLSConfig `json:"tls_config,omitempty"`
}

// EmailConfigHeaders is a map of email headers.
type EmailConfigHeaders map[string]string

// UnmarshalYAML https://github.com/VictoriaMetrics/operator/issues/609
func (r *EmailConfigHeaders) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw any
	if err := unmarshal(&raw); err != nil {
		return fmt.Errorf("cannot unmarshal email config headers: %w", err)
	}
	rawType := reflect.TypeOf(raw)
	switch rawType.Kind() {
	case reflect.Map:
		m := map[string]string{}
		if err := unmarshal(&m); err != nil {
			return err
		}
		*r = m
		return nil
	case reflect.Slice, reflect.Array:
		*r = map[string]string{}
		a := make([]KeyValue, 0)
		if err := unmarshal(&a); err != nil {
			return err
		}
		for _, kv := range a {
			(*r)[kv.Key] = kv.Value
		}
		return nil
	default:
		return &yaml.TypeError{Errors: []string{
			fmt.Sprintf("cannot unmarshal %#v into Go struct field EmailConfig.Headers of type %v", raw, rawType),
		}}
	}
}

// VictorOpsConfig configures notifications via VictorOps.
// See https://prometheus.io/docs/alerting/latest/configuration/#victorops_config
type VictorOpsConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// The secret's key that contains the API key to use when talking to the VictorOps API.
	// It must be at them same namespace as CRD
	// +optional
	APIKey *v1.SecretKeySelector `json:"api_key,omitempty"`
	// The VictorOps API URL.
	// +optional
	APIURL string `json:"api_url,omitempty"`
	// A key used to map the alert to a team.
	// +optional
	RoutingKey string `json:"routing_key"`
	// Describes the behavior of the alert (CRITICAL, WARNING, INFO).
	// +optional
	MessageType string `json:"message_type,omitempty"`
	// Contains summary of the alerted problem.
	// +optional
	EntityDisplayName string `json:"entity_display_name,omitempty"`
	// Contains long explanation of the alerted problem.
	// +optional
	StateMessage string `json:"state_message,omitempty"`
	// The monitoring tool the state message is from.
	// +optional
	MonitoringTool string `json:"monitoring_tool,omitempty"`
	// The HTTP client's configuration.
	// +optional
	HTTPConfig *HTTPAuth `json:"http_config,omitempty"`

	// Adds optional custom fields
	// https://github.com/prometheus/alertmanager/blob/v0.24.0/config/notifiers.go#L537
	// +optional
	CustomFields map[string]string `json:"custom_fields,omitempty"`
}

// PushoverConfig configures notifications via Pushover.
// See https://prometheus.io/docs/alerting/latest/configuration/#pushover_config
type PushoverConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// The secret's key that contains the recipient user’s user key.
	// It must be at them same namespace as CRD
	UserKey *v1.SecretKeySelector `json:"user_key,omitempty"`
	// The secret's key that contains the registered application’s API token, see https://pushover.net/apps.
	// It must be at them same namespace as CRD
	Token *v1.SecretKeySelector `json:"token,omitempty"`
	// Notification title.
	// +optional
	Title string `json:"title,omitempty"`
	// Notification message.
	// +optional
	Message string `json:"message,omitempty"`
	// A supplementary URL shown alongside the message.
	// +optional
	URL string `json:"url,omitempty"`
	// A title for supplementary URL, otherwise just the URL is shown
	// +optional
	URLTitle string `json:"url_title,omitempty"`
	// The name of one of the sounds supported by device clients to override the user's default sound choice
	// +optional
	Sound string `json:"sound,omitempty"`
	// Priority, see https://pushover.net/api#priority
	// +optional
	Priority string `json:"priority,omitempty"`
	// How often the Pushover servers will send the same notification to the user.
	// Must be at least 30 seconds.
	// +optional
	Retry string `json:"retry,omitempty"`
	// How long your notification will continue to be retried for, unless the user
	// acknowledges the notification.
	// +optional
	Expire string `json:"expire,omitempty"`
	// Whether notification message is HTML or plain text.
	// +optional
	HTML bool `json:"html,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPAuth `json:"http_config,omitempty"`
}

// SlackConfig configures notifications via Slack.
// See https://prometheus.io/docs/alerting/latest/configuration/#slack_config
type SlackConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// The secret's key that contains the Slack webhook URL.
	// It must be at them same namespace as CRD
	// +optional
	APIURL *v1.SecretKeySelector `json:"api_url,omitempty"`
	// The channel or user to send notifications to.
	// +optional
	Channel string `json:"channel,omitempty"`
	// +optional
	Username string `json:"username,omitempty"`
	// +optional
	Color string `json:"color,omitempty"`
	// +optional
	Title string `json:"title,omitempty"`
	// +optional
	TitleLink string `json:"title_link,omitempty"`
	// +optional
	Pretext string `json:"pretext,omitempty"`
	// +optional
	Text string `json:"text,omitempty"`
	// A list of Slack fields that are sent with each notification.
	// +optional
	Fields []SlackField `json:"fields,omitempty"`
	// +optional
	ShortFields bool `json:"short_fields,omitempty"`
	// +optional
	Footer string `json:"footer,omitempty"`
	// +optional
	Fallback string `json:"fallback,omitempty"`
	// +optional
	CallbackID string `json:"callback_id,omitempty"`
	// +optional
	IconEmoji string `json:"icon_emoji,omitempty"`
	// +optional
	IconURL string `json:"icon_url,omitempty"`
	// +optional
	ImageURL string `json:"image_url,omitempty"`
	// +optional
	ThumbURL string `json:"thumb_url,omitempty"`
	// +optional
	LinkNames bool `json:"link_names,omitempty"`
	// +optional
	MrkdwnIn []string `json:"mrkdwn_in,omitempty"`
	// A list of Slack actions that are sent with each notification.
	// +optional
	Actions []SlackAction `json:"actions,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPAuth `json:"http_config,omitempty"`
}

// SlackField configures a single Slack field that is sent with each notification.

// See https://api.slack.com/docs/message-attachments#fields for more information.
type SlackField struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Title string `json:"title"`
	// +kubebuilder:validation:MinLength=1
	// +required
	Value string `json:"value"`
	// +optional
	Short *bool `json:"short,omitempty"`
}

// SlackAction configures a single Slack action that is sent with each
// notification.
// See https://api.slack.com/docs/message-attachments#action_fields and
// https://api.slack.com/docs/message-buttons for more information.
type SlackAction struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`
	// +kubebuilder:validation:MinLength=1
	// +required
	Text string `json:"text"`
	// +optional
	URL string `json:"url,omitempty"`
	// +optional
	Style string `json:"style,omitempty"`
	// +optional
	Name string `json:"name,omitempty"`
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ConfirmField *SlackConfirmationField `json:"confirm,omitempty"`
}

// SlackConfirmationField protect users from destructive actions or
// particularly distinguished decisions by asking them to confirm their button
// click one more time.
// See https://api.slack.com/docs/interactive-message-field-guide#confirmation_fields
// for more information.
type SlackConfirmationField struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Text string `json:"text"`
	// +optional
	Title string `json:"title,omitempty"`
	// +optional
	OkText string `json:"ok_text,omitempty"`
	// +optional
	DismissText string `json:"dismiss_text,omitempty"`
}

// OpsGenieConfig configures notifications via OpsGenie.
// See https://prometheus.io/docs/alerting/latest/configuration/#opsgenie_config
type OpsGenieConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// The secret's key that contains the OpsGenie API key.
	// It must be at them same namespace as CRD
	// +optional
	APIKey *v1.SecretKeySelector `json:"api_key,omitempty"`
	// The URL to send OpsGenie API requests to.
	// +optional
	APIURL string `json:"apiURL,omitempty"`
	// Alert text limited to 130 characters.
	// +optional
	Message string `json:"message,omitempty"`
	// Description of the incident.
	// +optional
	Description string `json:"description,omitempty"`
	// Backlink to the sender of the notification.
	// +optional
	Source string `json:"source,omitempty"`
	// Comma separated list of tags attached to the notifications.
	// +optional
	Tags string `json:"tags,omitempty"`
	// Additional alert note.
	// +optional
	Note string `json:"note,omitempty"`
	// Priority level of alert. Possible values are P1, P2, P3, P4, and P5.
	// +optional
	Priority string `json:"priority,omitempty"`
	// A set of arbitrary key/value pairs that provide further detail about the incident.
	// +optional
	Details map[string]string `json:"details,omitempty"`
	// List of responders responsible for notifications.
	// +optional
	Responders []OpsGenieConfigResponder `json:"responders,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPAuth `json:"http_config,omitempty"`
}

// OpsGenieConfigResponder defines a responder to an incident.
// One of `id`, `name` or `username` has to be defined.
type OpsGenieConfigResponder struct {
	// ID of the responder.
	// +optional
	ID string `json:"id,omitempty"`
	// Name of the responder.
	// +optional
	Name string `json:"name,omitempty"`
	// Username of the responder.
	// +optional
	Username string `json:"username,omitempty"`
	// Type of responder.
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`
}

// PagerDutyConfig configures notifications via PagerDuty.
// See https://prometheus.io/docs/alerting/latest/configuration/#pagerduty_config
type PagerDutyConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty"`
	// The secret's key that contains the PagerDuty integration key (when using
	// Events API v2). Either this field or `serviceKey` needs to be defined.
	// It must be at them same namespace as CRD
	// +optional
	RoutingKey *v1.SecretKeySelector `json:"routing_key,omitempty"`
	// The secret's key that contains the PagerDuty service key (when using
	// integration type "Prometheus"). Either this field or `routingKey` needs to
	// be defined.
	// It must be at them same namespace as CRD
	// +optional
	ServiceKey *v1.SecretKeySelector `json:"service_key,omitempty"`
	// The URL to send requests to.
	// +optional
	URL string `json:"url,omitempty"`
	// Client identification.
	// +optional
	Client string `json:"client,omitempty"`
	// Backlink to the sender of notification.
	// +optional
	ClientURL string `json:"client_url,omitempty"`
	// Images to attach to the incident.
	// +optional
	Images []ImageConfig `json:"images,omitempty"`
	// Links to attach to the incident.
	// +optional
	Links []LinkConfig `json:"links,omitempty"`
	// Description of the incident.
	// +optional
	Description string `json:"description,omitempty"`
	// Severity of the incident.
	// +optional
	Severity string `json:"severity,omitempty"`
	// The class/type of the event.
	// +optional
	Class string `json:"class,omitempty"`
	// A cluster or grouping of sources.
	// +optional
	Group string `json:"group,omitempty"`
	// The part or component of the affected system that is broken.
	// +optional
	Component string `json:"component,omitempty"`
	// Arbitrary key/value pairs that provide further detail about the incident.
	// +optional
	Details PagerDutyDetails `json:"details,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPAuth `json:"http_config,omitempty"`
}

// PagerDutyDetails details for config
type PagerDutyDetails map[string]string

// UnmarshalYAML implements interface
func (pdd *PagerDutyDetails) UnmarshalYAML(unmarshal func(interface{}) error) error {
	pddm := make(map[string]string)
	if err := unmarshal(&pddm); err != nil {
		if !strings.Contains(err.Error(), "seq into map") {
			return err
		}
		// fallback to the prometheus-operator data format
		type pagerDutyPromDetails struct {
			Key   string
			Value string
		}
		var promPDD []pagerDutyPromDetails
		if err := unmarshal(&promPDD); err != nil {
			return fmt.Errorf("cannot parse pager duty details :%w", err)
		}
		for _, kv := range promPDD {
			pddm[kv.Key] = kv.Value
		}
	}
	*pdd = pddm
	return nil
}

// ImageConfig is used to attach images to the incident.
// See https://developer.pagerduty.com/docs/ZG9jOjExMDI5NTgx-send-an-alert-event#the-images-property
// for more information.
type ImageConfig struct {
	Href   string `json:"href,omitempty"`
	Source string `json:"source"`
	Alt    string `json:"alt,omitempty"`
}

// LinkConfig is used to attach text links to the incident.
// See https://developer.pagerduty.com/docs/ZG9jOjExMDI5NTgx-send-an-alert-event#the-links-property
// for more information.
type LinkConfig struct {
	Href string `json:"href"`
	Text string `json:"text,omitempty"`
}

func (amc *VMAlertmanagerConfig) AsKey() string {
	return fmt.Sprintf("%s/%s", amc.Namespace, amc.Name)
}

func init() {
	SchemeBuilder.Register(&VMAlertmanagerConfig{}, &VMAlertmanagerConfigList{})
}
