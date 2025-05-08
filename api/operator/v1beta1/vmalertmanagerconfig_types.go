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
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	amcfg "github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/pkg/labels"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VMAlertmanagerConfigSpec defines configuration for VMAlertmanagerConfig
// it must reference only locally defined objects
type VMAlertmanagerConfigSpec struct {
	// Route definition for alertmanager, may include nested routes.
	Route *Route `json:"route"`
	// Receivers defines alert receivers
	Receivers []Receiver `json:"receivers"`
	// InhibitRules will only apply for alerts matching
	// the resource's namespace.
	// +optional
	InhibitRules []InhibitRule `json:"inhibit_rules,omitempty" yaml:"inhibit_rules,omitempty"`

	// TimeIntervals defines named interval for active/mute notifications interval
	// See https://prometheus.io/docs/alerting/latest/configuration/#time_interval
	// +optional
	TimeIntervals []TimeIntervals `json:"time_intervals,omitempty" yaml:"time_intervals,omitempty"`
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
}

// TimeIntervals for alerts
type TimeIntervals struct {
	// Name of interval
	// +required
	Name string `json:"name,omitempty"`
	// TimeIntervals interval configuration
	// +required
	TimeIntervals []TimeInterval `json:"time_intervals" yaml:"time_intervals"`
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
	DaysOfMonth []string `json:"days_of_month,omitempty" yaml:"days_of_month,omitempty"`
	// Months  defines list of calendar months identified by a case-insensitive name (e.g. ‘January’) or numeric 1.
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
	StartTime string `json:"start_time" yaml:"start_time"`
	// EndTime for example HH:MM
	// +required
	EndTime string `json:"end_time" yaml:"end_time"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (r *VMAlertmanagerConfig) GetStatusMetadata() *StatusMetadata {
	return &r.Status.StatusMetadata
}

func (r *VMAlertmanagerConfig) Validate() error {
	if mustSkipValidation(r) {
		return nil
	}
	validateSpec := r.DeepCopy()
	if r.Spec.Route == nil {
		return fmt.Errorf("no routes provided")
	}
	if r.Spec.Route.Receiver == "" {
		return fmt.Errorf("root route reciever cannot be empty")
	}

	for idx, recv := range r.Spec.Receivers {
		if err := validateReceiver(recv); err != nil {
			return fmt.Errorf("receiver at idx=%d is invalid: %w", idx, err)
		}
	}

	if err := parseNestedRoutes(validateSpec.Spec.Route); err != nil {
		return fmt.Errorf("cannot parse nested route for alertmanager config err: %w", err)
	}

	names := map[string]struct{}{}
	for _, rcv := range r.Spec.Receivers {
		if _, ok := names[rcv.Name]; ok {
			return fmt.Errorf("notification config name %q is not unique", rcv.Name)
		}
		names[rcv.Name] = struct{}{}
	}

	if _, ok := names[r.Spec.Route.Receiver]; !ok {
		return fmt.Errorf("receiver=%q for spec root not found at receivers", r.Spec.Route.Receiver)
	}

	tiNames, err := validateTimeIntervals(r.Spec.TimeIntervals)
	if err != nil {
		return err
	}

	for _, ti := range r.Spec.Route.ActiveTimeIntervals {
		if _, ok := tiNames[ti]; !ok {
			return fmt.Errorf("undefined active time interval %q used in root route", ti)
		}
	}
	for _, ti := range r.Spec.Route.MuteTimeIntervals {
		if _, ok := tiNames[ti]; !ok {
			return fmt.Errorf("undefined mute time interval %q used in root route", ti)
		}
	}
	for idx, sr := range r.Spec.Route.Routes {
		if err := checkRouteReceiver(sr, names, tiNames); err != nil {
			return fmt.Errorf("subRoute=%d is not valid: %w", idx, err)
		}
	}

	return nil
}

// VMAlertmanagerConfigStatus defines the observed state of VMAlertmanagerConfig
type VMAlertmanagerConfigStatus struct {
	// ObservedGeneration defines current generation picked by operator for the
	// reconcile
	StatusMetadata                  `json:",inline"`
	LastErrorParentAlertmanagerName string `json:"lastErrorParentAlertmanagerName,omitempty"`
}

// VMAlertmanagerConfig is the Schema for the vmalertmanagerconfigs API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
// +genclient
// +k8s:openapi-gen=true
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
	Routes []*SubRoute `json:"-" yaml:"-"`
	// Child routes.
	// https://prometheus.io/docs/alerting/latest/configuration/#route
	RawRoutes []apiextensionsv1.JSON `json:"routes,omitempty" yaml:"routes,omitempty"`
	// MuteTimeIntervals is a list of interval names that will mute matched alert
	// +optional
	MuteTimeIntervals []string `json:"mute_time_intervals,omitempty" yaml:"mute_time_intervals,omitempty"`
	// ActiveTimeIntervals Times when the route should be active
	// These must match the name at time_intervals
	// +optional
	ActiveTimeIntervals []string `json:"active_time_intervals,omitempty" yaml:"active_time_intervals,omitempty"`
}

// SubRoute alias for Route, its needed to proper use json parsing with raw input
type SubRoute Route

// parseNestedRoutes validates nested routes
func parseNestedRoutes(src *Route) error {
	if src == nil {
		return nil
	}
	for idx, matchers := range src.Matchers {
		if _, err := labels.ParseMatchers(matchers); err != nil {
			return fmt.Errorf("cannot parse matchers=%q idx=%d for route_receiver=%s: %w", matchers, idx, src.Receiver, err)
		}
	}
	for _, nestedRoute := range src.RawRoutes {
		if len(nestedRoute.Raw) == 0 {
			return fmt.Errorf("unexpected empty route")
		}
		var subRoute Route
		decoder := json.NewDecoder(bytes.NewReader(nestedRoute.Raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&subRoute); err != nil {
			return fmt.Errorf("cannot parse json value: %s for nested route, err :%w", string(nestedRoute.Raw), err)
		}
		if err := parseNestedRoutes(&subRoute); err != nil {
			return err
		}
		newRoute := SubRoute(subRoute)
		src.Routes = append(src.Routes, &newRoute)
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMAlertmanagerConfig) UnmarshalJSON(src []byte) error {
	type amcfg VMAlertmanagerConfig
	if err := json.Unmarshal(src, (*amcfg)(r)); err != nil {
		r.Spec.ParsingError = fmt.Sprintf("cannot parse alertmanager config: %s, err: %s", string(src), err)
		return nil
	}

	if err := parseNestedRoutes(r.Spec.Route); err != nil {
		r.Spec.ParsingError = fmt.Sprintf("cannot parse routes for alertmanager config: %s at namespace: %s, err: %s", r.Name, r.Namespace, err)
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
	EmailConfigs []EmailConfig `json:"email_configs,omitempty" yaml:"email_configs,omitempty"`
	// PagerDutyConfigs defines pager duty notification configurations.
	// +optional
	PagerDutyConfigs []PagerDutyConfig `json:"pagerduty_configs,omitempty" yaml:"pagerduty_configs,omitempty"`
	// PushoverConfigs defines push over notification configurations.
	// +optional
	PushoverConfigs []PushoverConfig `json:"pushover_configs,omitempty" yaml:"pushover_configs,omitempty"`
	// SlackConfigs defines slack notification configurations.
	// +optional
	SlackConfigs []SlackConfig `json:"slack_configs,omitempty" yaml:"slack_configs,omitempty"`
	// OpsGenieConfigs defines ops genie notification configurations.
	// +optional
	OpsGenieConfigs []OpsGenieConfig `json:"opsgenie_configs,omitempty" yaml:"opsgenie_configs,omitempty"`
	// WebhookConfigs defines webhook notification configurations.
	// +optional
	WebhookConfigs []WebhookConfig `json:"webhook_configs,omitempty" yaml:"webhook_configs,omitempty"`

	// VictorOpsConfigs defines victor ops notification configurations.
	// +optional
	VictorOpsConfigs []VictorOpsConfig `json:"victorops_configs,omitempty" yaml:"victorops_configs,omitempty"`
	// WeChatConfigs defines wechat notification configurations.
	// +optional
	WeChatConfigs []WeChatConfig `json:"wechat_configs,omitempty" yaml:"wechat_configs,omitempty"`
	// +optional
	TelegramConfigs []TelegramConfig `json:"telegram_configs,omitempty" yaml:"telegram_configs,omitempty"`
	// +optional
	MSTeamsConfigs []MSTeamsConfig `json:"msteams_configs,omitempty" yaml:"msteams_configs,omitempty"`
	// +optional
	DiscordConfigs []DiscordConfig `json:"discord_configs,omitempty" yaml:"discord_configs,omitempty"`
	// +optional
	SNSConfigs []SnsConfig `json:"sns_configs,omitempty" yaml:"sns_configs,omitempty"`
	// +optional
	WebexConfigs []WebexConfig `json:"webex_configs,omitempty" yaml:"webex_configs,omitempty"`
	// +optional
	JiraConfigs []JiraConfig `json:"jira_configs,omitempty" yaml:"jira_configs,omitempty"`
	// +optional
	RocketchatConfigs []RocketchatConfig `json:"rocketchat_configs,omitempty" yaml:"rocketchat_configs,omitempty"`
	// +optional
	MSTeamsV2Configs []MSTeamsV2Config `json:"msteamsv2_configs,omitempty" yaml:"msteamsv2_configs,omitempty"`
}

// TelegramConfig configures notification via telegram
// https://prometheus.io/docs/alerting/latest/configuration/#telegram_config
type TelegramConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// APIUrl the Telegram API URL i.e. https://api.telegram.org.
	// +optional
	APIUrl string `json:"api_url,omitempty" yaml:"api_url,omitempty"`
	// BotToken token for the bot
	// https://core.telegram.org/bots/api
	BotToken *v1.SecretKeySelector `json:"bot_token" yaml:"bot_token"`
	// ChatID is ID of the chat where to send the messages.
	ChatID int `json:"chat_id" yaml:"chat_id"`
	// MessageThreadID defines ID of the message thread where to send the messages.
	// +optional
	MessageThreadID int `json:"message_thread_id,omitempty"`
	// Message is templated message
	// +optional
	Message string `json:"message,omitempty"`
	// DisableNotifications
	// +optional
	DisableNotifications *bool `json:"disable_notifications,omitempty" yaml:"disable_notifications,omitempty"`
	// ParseMode for telegram message,
	// supported values are MarkdownV2, Markdown, Markdown and empty string for plain text.
	// +optional
	ParseMode string `json:"parse_mode,omitempty" yaml:"parse_mode"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

// WebhookConfig configures notifications via a generic receiver supporting the webhook payload.
// See https://prometheus.io/docs/alerting/latest/configuration/#webhook_config
type WebhookConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// URL to send requests to,
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URL *string `json:"url,omitempty"`
	// URLSecret defines secret name and key at the CRD namespace.
	// It must contain the webhook URL.
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URLSecret *v1.SecretKeySelector `json:"url_secret,omitempty" yaml:"url_secret,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
	// Maximum number of alerts to be sent per webhook message. When 0, all alerts are included.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxAlerts int32 `json:"max_alerts,omitempty" yaml:"max_alerts,omitempty"`
	// Timeout is the maximum time allowed to invoke the webhook
	// available since v0.28.0 alertmanager version
	// +kubebuilder:validation:Pattern:="^(0|(([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?)$"
	// +optional
	Timeout string `json:"timeout,omitempty"`
}

// WeChatConfig configures notifications via WeChat.
// See https://prometheus.io/docs/alerting/latest/configuration/#wechat_config
type WeChatConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The secret's key that contains the WeChat API key.
	// The secret needs to be in the same namespace as the AlertmanagerConfig
	// fallback to global alertmanager setting if empty
	// +optional
	APISecret *v1.SecretKeySelector `json:"api_secret,omitempty" yaml:"api_secret,omitempty"`
	// The WeChat API URL.
	// fallback to global alertmanager setting if empty
	// +optional
	APIURL string `json:"api_url,omitempty" yaml:"api_url,omitempty"`
	// The corp id for authentication.
	// fallback to global alertmanager setting if empty
	// +optional
	CorpID string `json:"corp_id,omitempty" yaml:"corp_id,omitempty"`
	// +optional
	AgentID string `json:"agent_id,omitempty" yaml:"agent_id,omitempty"`
	// +optional
	ToUser string `json:"to_user,omitempty" yaml:"to_user,omitempty"`
	// +optional
	ToParty string `json:"to_party,omitempty" yaml:"to_party,omitempty"`
	// +optional
	ToTag string `json:"to_tag,omitempty" yaml:"to_tag,omitempty"`
	// API request data as defined by the WeChat API.
	Message string `json:"message,omitempty"`
	// +optional
	MessageType string `json:"message_type,omitempty" yaml:"message_type,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

// EmailConfig configures notifications via Email.
type EmailConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The email address to send notifications to.
	// +optional
	To string `json:"to,omitempty"`
	// The sender address.
	// fallback to global setting if empty
	// +optional
	From string `json:"from,omitempty"`
	// The hostname to identify to the SMTP server.
	// +optional
	Hello string `json:"hello,omitempty"`
	// The SMTP host through which emails are sent.
	// fallback to global setting if empty
	// +optional
	Smarthost string `json:"smarthost,omitempty"`
	// The username to use for authentication.
	// +optional
	AuthUsername string `json:"auth_username,omitempty" yaml:"auth_username,omitempty"`
	// AuthPassword defines secret name and key at CRD namespace.
	// +optional
	AuthPassword *v1.SecretKeySelector `json:"auth_password,omitempty" yaml:"auth_password,omitempty"`
	// AuthSecret defines secrent name and key at CRD namespace.
	// It must contain the CRAM-MD5 secret.
	// +optional
	AuthSecret *v1.SecretKeySelector `json:"auth_secret,omitempty" yaml:"auth_secret,omitempty"`
	// The identity to use for authentication.
	// +optional
	AuthIdentity string `json:"auth_identity,omitempty" yaml:"auth_identity,omitempty"`
	// Further headers email header key/value pairs. Overrides any headers
	// previously set by the notification implementation.
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	// The HTML body of the email notification.
	// +optional
	HTML string `json:"html,omitempty"`
	// The text body of the email notification.
	// +optional
	Text string `json:"text,omitempty"`
	// The SMTP TLS requirement.
	// Note that Go does not support unencrypted connections to remote SMTP endpoints.
	// +optional
	RequireTLS *bool `json:"require_tls,omitempty" yaml:"require_tls,omitempty"`
	// TLS configuration
	// +optional
	TLSConfig *TLSConfig `json:"tls_config,omitempty" yaml:"tls_config,omitempty"`
}

// VictorOpsConfig configures notifications via VictorOps.
// See https://prometheus.io/docs/alerting/latest/configuration/#victorops_config
type VictorOpsConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The secret's key that contains the API key to use when talking to the VictorOps API.
	// It must be at them same namespace as CRD
	// fallback to global setting if empty
	// +optional
	APIKey *v1.SecretKeySelector `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	// The VictorOps API URL.
	// +optional
	APIURL string `json:"api_url,omitempty" yaml:"api_url,omitempty"`
	// A key used to map the alert to a team.
	RoutingKey string `json:"routing_key" yaml:"routing_key"`
	// Describes the behavior of the alert (CRITICAL, WARNING, INFO).
	// +optional
	MessageType string `json:"message_type,omitempty" yaml:"message_type,omitempty"`
	// Contains summary of the alerted problem.
	// +optional
	EntityDisplayName string `json:"entity_display_name,omitempty" yaml:"entity_display_name,omitempty"`
	// Contains long explanation of the alerted problem.
	// +optional
	StateMessage string `json:"state_message,omitempty" yaml:"state_message,omitempty"`
	// The monitoring tool the state message is from.
	// +optional
	MonitoringTool string `json:"monitoring_tool,omitempty" yaml:"monitoring_tool,omitempty"`
	// The HTTP client's configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
	// Adds optional custom fields
	// https://github.com/prometheus/alertmanager/blob/v0.24.0/config/notifiers.go#L537
	// +optional
	CustomFields map[string]string `json:"custom_fields,omitempty" yaml:"custom_fields,omitempty"`
}

// PushoverConfig configures notifications via Pushover.
// See https://prometheus.io/docs/alerting/latest/configuration/#pushover_config
type PushoverConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The secret's key that contains the recipient user’s user key.
	// It must be at them same namespace as CRD
	UserKey *v1.SecretKeySelector `json:"user_key,omitempty" yaml:"user_key,omitempty"`
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
	URLTitle string `json:"url_title,omitempty" yaml:"url_title,omitempty"`
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
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

// SlackConfig configures notifications via Slack.
// See https://prometheus.io/docs/alerting/latest/configuration/#slack_config
type SlackConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The secret's key that contains the Slack webhook URL.
	// It must be at them same namespace as CRD
	// fallback to global setting if empty
	// +optional
	APIURL *v1.SecretKeySelector `json:"api_url,omitempty" yaml:"api_url,omitempty"`
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
	TitleLink string `json:"title_link,omitempty" yaml:"title_link,omitempty"`
	// +optional
	Pretext string `json:"pretext,omitempty"`
	// +optional
	Text string `json:"text,omitempty"`
	// A list of Slack fields that are sent with each notification.
	// +optional
	Fields []SlackField `json:"fields,omitempty"`
	// +optional
	ShortFields bool `json:"short_fields,omitempty" yaml:"short_fields,omitempty"`
	// +optional
	Footer string `json:"footer,omitempty"`
	// +optional
	Fallback string `json:"fallback,omitempty"`
	// +optional
	CallbackID string `json:"callback_id,omitempty" yaml:"callback_id,omitempty"`
	// +optional
	IconEmoji string `json:"icon_emoji,omitempty" yaml:"icon_emoji,omitempty"`
	// +optional
	IconURL string `json:"icon_url,omitempty" yaml:"icon_url,omitempty"`
	// +optional
	ImageURL string `json:"image_url,omitempty" yaml:"image_url,omitempty"`
	// +optional
	ThumbURL string `json:"thumb_url,omitempty" yaml:"thumb_url,omitempty"`
	// +optional
	LinkNames bool `json:"link_names,omitempty" yaml:"link_names,omitempty"`
	// +optional
	MrkdwnIn []string `json:"mrkdwn_in,omitempty" yaml:"mrkdwn_in,omitempty"`
	// A list of Slack actions that are sent with each notification.
	// +optional
	Actions []SlackAction `json:"actions,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
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
	OkText string `json:"ok_text,omitempty" yaml:"ok_text,omitempty"`
	// +optional
	DismissText string `json:"dismiss_text,omitempty" yaml:"dismiss_text,omitempty"`
}

// OpsGenieConfig configures notifications via OpsGenie.
// See https://prometheus.io/docs/alerting/latest/configuration/#opsgenie_config
type OpsGenieConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The secret's key that contains the OpsGenie API key.
	// It must be at them same namespace as CRD
	// fallback to global setting if empty
	// +optional
	APIKey *v1.SecretKeySelector `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	// The URL to send OpsGenie API requests to.
	// +optional
	APIURL string `json:"apiURL,omitempty" yaml:"apiURL,omitempty"`
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
	// Optional field that can be used to specify which domain alert is related to.
	Entity string `json:"entity,omitempty"`
	// Comma separated list of actions that will be available for the alert.
	Actions string `json:"actions,omitempty"`
	// Whether to update message and description of the alert in OpsGenie if it already exists
	// By default, the alert is never updated in OpsGenie, the new message only appears in activity log.
	UpdateAlerts bool `json:"update_alerts,omitempty" yaml:"update_alerts,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
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
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The secret's key that contains the PagerDuty integration key (when using
	// Events API v2). Either this field or `serviceKey` needs to be defined.
	// It must be at them same namespace as CRD
	// +optional
	RoutingKey *v1.SecretKeySelector `json:"routing_key,omitempty" yaml:"routing_key,omitempty"`
	// The secret's key that contains the PagerDuty service key (when using
	// integration type "Prometheus"). Either this field or `routingKey` needs to
	// be defined.
	// It must be at them same namespace as CRD
	// +optional
	ServiceKey *v1.SecretKeySelector `json:"service_key,omitempty" yaml:"service_key,omitempty"`
	// The URL to send requests to.
	// +optional
	URL string `json:"url,omitempty"`
	// Client identification.
	// +optional
	Client string `json:"client,omitempty"`
	// Backlink to the sender of notification.
	// +optional
	ClientURL string `json:"client_url,omitempty" yaml:"client_url,omitempty"`
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
	Details map[string]string `json:"details,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

// ImageConfig is used to attach images to the incident.
// See https://developer.pagerduty.com/docs/ZG9jOjExMDI5NTgx-send-an-alert-event#the-images-property
// for more information.
type ImageConfig struct {
	// +optional
	Href   string `json:"href,omitempty"`
	Source string `json:"source"`
	// +optional
	Alt string `json:"alt,omitempty"`
}

// LinkConfig is used to attach text links to the incident.
// See https://developer.pagerduty.com/docs/ZG9jOjExMDI5NTgx-send-an-alert-event#the-links-property
// for more information.
type LinkConfig struct {
	Href string `json:"href"`
	Text string `json:"text,omitempty"`
}

type MSTeamsConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The incoming webhook URL
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URL *string `json:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`
	// URLSecret defines secret name and key at the CRD namespace.
	// It must contain the webhook URL.
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URLSecret *v1.SecretKeySelector `json:"webhook_url_secret,omitempty" yaml:"webhook_url_secret,omitempty"`
	// The title of the teams notification.
	// +optional
	Title string `json:"title,omitempty"`
	// The text body of the teams notification.
	// +optional
	Text string `json:"text,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

type DiscordConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The discord webhook URL
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URL *string `json:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`
	// URLSecret defines secret name and key at the CRD namespace.
	// It must contain the webhook URL.
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URLSecret *v1.SecretKeySelector `json:"webhook_url_secret,omitempty" yaml:"webhook_url_secret,omitempty"`
	// The message title template
	// +optional
	Title string `json:"title,omitempty"`
	// The message body template
	// +optional
	Message string `json:"message,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
	// Content defines message content template
	// Available from operator v0.55.0 and alertmanager v0.28.0
	// +kubebuilder:validation:MaxLength:=2000
	// +optional
	Content string `json:"content,omitempty"`
	// Username defines message username
	// Available from operator v0.55.0 and alertmanager v0.28.0
	// +optional
	Username string `json:"username,omitempty" yaml:"username"`
	// AvatarURL defines message avatar URL
	// Available from operator v0.55.0 and alertmanager v0.28.0
	// +optional
	AvatarURL string `json:"avatar_url,omitempty" yaml:"avatar_url,omitempty"`
}

type SnsConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The api URL
	// +optional
	URL string `json:"api_url,omitempty" yaml:"api_url,omitempty"`
	// Configure the AWS Signature Verification 4 signing process
	Sigv4 *Sigv4Config `json:"sigv4,omitempty"`
	// SNS topic ARN, either specify this, phone_number or target_arn
	// +optional
	TopicArn string `json:"topic_arn,omitempty" yaml:"topic_arn,omitempty"`
	// The subject line if message is delivered to an email endpoint.
	// +optional
	Subject string `json:"subject,omitempty"`
	// Phone number if message is delivered via SMS
	// Specify this, topic_arn or target_arn
	PhoneNumber string `json:"phone_number,omitempty" yaml:"phone_number,omitempty"`
	// Mobile platform endpoint ARN if message is delivered via mobile notifications
	// Specify this, topic_arn or phone_number
	// +optional
	TargetArn string `json:"target_arn,omitempty" yaml:"target_arn,omitempty"`
	// The message content of the SNS notification.
	// +optional
	Message string `json:"message,omitempty"`
	// SNS message attributes
	// +optional
	Attributes map[string]string `json:"attributes,omitempty"`
	// HTTP client configuration.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

type Sigv4Config struct {
	// AWS region, if blank the region from the default credentials chain is used
	// +optional
	Region string `json:"region,omitempty"`
	// The AWS API keys. Both access_key and secret_key must be supplied or both must be blank.
	// If blank the environment variables `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` are used.
	// +optional
	AccessKey string `json:"access_key,omitempty" yaml:"access_key,omitempty"`
	// secret key selector to get the keys from a Kubernetes Secret
	// +optional
	AccessKeySelector *v1.SecretKeySelector `json:"access_key_selector,omitempty" yaml:"access_key_selector,omitempty"`
	// secret key selector to get the keys from a Kubernetes Secret
	// +optional
	SecretKey *v1.SecretKeySelector `json:"secret_key_selector,omitempty" yaml:"secret_key_selector,omitempty"`
	// Named AWS profile used to authenticate
	// +optional
	Profile string `json:"profile,omitempty"`
	// AWS Role ARN, an alternative to using AWS API keys
	// +optional
	RoleArn string `json:"role_arn,omitempty" yaml:"role_arn,omitempty"`
}

type WebexConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// The Webex Teams API URL, i.e. https://webexapis.com/v1/messages
	// +optional
	URL *string `json:"api_url,omitempty" yaml:"api_url,omitempty"`
	// The ID of the Webex Teams room where to send the messages
	// +required
	RoomId string `json:"room_id,omitempty" yaml:"room_id,omitempty"`
	// The message body template
	// +optional
	Message string `json:"message,omitempty"`
	// HTTP client configuration. You must use this configuration to supply the bot token as part of the HTTP `Authorization` header.
	// +optional
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

// JiraConfig represent alertmanager's jira_config entry
// https://prometheus.io/docs/alerting/latest/configuration/#jira_config
// available from v0.55.0 operator version
// and v0.28.0 alertmanager version
type JiraConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`

	// The URL to send API requests to. The full API path must be included.
	// Example: https://company.atlassian.net/rest/api/2/
	// +optional
	APIURL *string `json:"api_url,omitempty" yaml:"api_url,omitempty"`

	// The project key where issues are created
	Project string `json:"project" yaml:"project"`
	// Issue summary template
	// +optional
	Summary string `json:"summary,omitempty" yaml:"summary,omitempty"`
	// Issue description template.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Labels to be added to the issue
	Labels []string `json:"labels,omitempty" yaml:"labels,omitempty"`
	// Priority of the issue
	Priority string `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Type of the issue (e.g. Bug)
	IssueType string `json:"issue_type" yaml:"issue_type"`

	// Name of the workflow transition to resolve an issue.
	// The target status must have the category "done".
	ReopenTransition string `json:"reopen_transition,omitempty" yaml:"reopen_transition,omitempty"`
	// Name of the workflow transition to reopen an issue.
	// The target status should not have the category "done".
	ResolveTransition string `json:"resolve_transition,omitempty" yaml:"resolve_transition,omitempty"`
	// If reopen_transition is defined, ignore issues with that resolution.
	WontFixResolution string `json:"wont_fix_resolution,omitempty" yaml:"wont_fix_resolution,omitempty"`

	// If reopen_transition is defined, reopen the issue when it is not older than this value (rounded down to the nearest minute).
	// The resolutiondate field is used to determine the age of the issue.
	// +kubebuilder:validation:Pattern:="^(0|(([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?)$"
	// +optional
	ReopenDuration string `json:"reopen_duration,omitempty" yaml:"reopen_duration,omitempty"`

	// Other issue and custom fields.
	// Jira issue field can have multiple types.
	// Depends on the field type, the values must be provided differently.
	// See https://developer.atlassian.com/server/jira/platform/jira-rest-api-examples/#setting-custom-field-data-for-other-field-types for further examples.
	// +optional
	Fields map[string]apiextensionsv1.JSON `json:"custom_fields,omitempty" yaml:"fields,omitempty"`

	// The HTTP client's configuration. You must use this configuration to supply the personal access token (PAT) as part of the HTTP `Authorization` header.
	// For Jira Cloud, use basic_auth with the email address as the username and the PAT as the password.
	// For Jira Data Center, use the 'authorization' field with 'credentials: <PAT value>'.
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

// RocketchatConfig configures notifications via Rocketchat.
// https://prometheus.io/docs/alerting/latest/configuration/#rocketchat_config
// available from v0.55.0 operator version
// and v0.28.0 alertmanager version
type RocketchatConfig struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`
	// +optional
	APIURL *string `json:"api_url,omitempty" yaml:"api_url,omitempty"`

	// The sender token and token_id
	// See https://docs.rocket.chat/use-rocket.chat/user-guides/user-panel/my-account#personal-access-tokens
	// +optional
	TokenID *v1.SecretKeySelector `yaml:"token_id,omitempty" json:"token_id,omitempty"`
	// +optional
	Token *v1.SecretKeySelector `yaml:"token,omitempty" json:"token,omitempty"`

	// RocketChat channel override, (like #other-channel or @username).
	// +optional
	Channel string `yaml:"channel,omitempty" json:"channel,omitempty"`
	// +optional
	Color string `json:"color,omitempty" yaml:"color,omitempty"`
	// +optional
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// +optional
	TitleLink string `json:"title_link,omitempty" yaml:"title_link,omitempty"`
	// +optional
	Text string `json:"text,omitempty" yaml:"text,omitempty"`
	// +optional
	Fields []RocketchatAttachmentField `json:"fields,omitempty" yaml:"fields,omitempty"`
	// +optional
	ShortFields bool `json:"short_fields,omitempty" yaml:"short_fields,omitempty"`
	// +optional
	Emoji string `json:"emoji,omitempty" yaml:"emoji,omitempty"`
	// +optional
	IconURL string `json:"icon_url,omitempty" yaml:"icon_url,omitempty"`
	// +optional
	ImageURL string `json:"image_url,omitempty" yaml:"image_url,omitempty"`
	// +optional
	ThumbURL string `json:"thumb_url,omitempty" yaml:"thumb_url,omitempty"`
	// +optional
	LinkNames bool `json:"link_names,omitempty" yaml:"link_names"`
	// +optional
	Actions []RocketchatAttachmentAction `json:"actions,omitempty" yaml:"actions,omitempty"`
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

// RocketchatAttachmentField defines API fields
// https://developer.rocket.chat/reference/api/rest-api/endpoints/messaging/chat-endpoints/postmessage#attachment-field-objects
type RocketchatAttachmentField struct {
	// +optional
	Short *bool `json:"short"`
	// +optional
	Title string `json:"title,omitempty"`
	// +optional
	Value string `json:"value,omitempty"`
}

// RocketchatAttachmentAction defines message attachements
// https://github.com/RocketChat/Rocket.Chat.Go.SDK/blob/master/models/message.go
type RocketchatAttachmentAction struct {
	// +optional
	Type string `json:"type,omitempty"`
	// +optional
	Text string `json:"text,omitempty"`
	// +optional
	URL string `json:"url,omitempty"`
	// +optional
	Msg string `json:"msg,omitempty"`
}

// MSTeamsV2Config sends notifications using the new message format with adaptive cards as required by flows.
// https://support.microsoft.com/en-gb/office/create-incoming-webhooks-with-workflows-for-microsoft-teams-8ae491c7-0394-4861-ba59-055e33f75498
// available from v0.55.0 operator version
// and v0.28.0 alertmanager version
type MSTeamsV2Config struct {
	// SendResolved controls notify about resolved alerts.
	// +optional
	SendResolved *bool `json:"send_resolved,omitempty" yaml:"send_resolved,omitempty"`

	// The incoming webhook URL
	// one of `urlSecret` and `url` must be defined.
	// +optional
	URL *string `json:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`
	// URLSecret defines secret name and key at the CRD namespace.
	// It must contain the webhook URL.
	// one of `webhook_url` or `webhook_url_secret` must be defined.
	// +optional
	URLSecret *v1.SecretKeySelector `json:"webhook_url_secret,omitempty" yaml:"webhook_url_secret,omitempty"`

	// Message title template.
	// +optional
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// Message body template.
	// +optional
	Text string `json:"text,omitempty" yaml:"text,omitempty"`

	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	HTTPConfig *HTTPConfig `json:"http_config,omitempty" yaml:"http_config,omitempty"`
}

// HTTPConfig defines a client HTTP configuration for VMAlertmanagerConfig objects
// See https://prometheus.io/docs/alerting/latest/configuration/#http_config
type HTTPConfig struct {
	// BasicAuth for the client.
	// +optional
	BasicAuth *BasicAuth `json:"basic_auth,omitempty" yaml:"basic_auth,omitempty"`
	// The secret's key that contains the bearer token
	// It must be at them same namespace as CRD
	// +optional
	BearerTokenSecret *v1.SecretKeySelector `json:"bearer_token_secret,omitempty" yaml:"bearer_token_secret,omitempty"`
	// BearerTokenFile defines filename for bearer token, it must be mounted to pod.
	// +optional
	BearerTokenFile string `json:"bearer_token_file,omitempty" yaml:"bearer_token_file,omitempty"`
	// TLS configuration for the client.
	// +optional
	TLSConfig *TLSConfig `json:"tls_config,omitempty" yaml:"tls_config,omitempty"`
	// Optional proxy URL.
	// +optional
	ProxyURL string `json:"proxyURL,omitempty" yaml:"proxy_url,omitempty"`
	// Authorization header configuration for the client.
	// This is mutually exclusive with BasicAuth and is only available starting from Alertmanager v0.22+.
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
	// OAuth2 client credentials used to fetch a token for the targets.
	// +optional
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaller interface
func (hc *HTTPConfig) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	type phc HTTPConfig
	if err := decoder.Decode((*phc)(hc)); err != nil {
		return err
	}

	return nil
}

func (hc *HTTPConfig) validate() error {
	if hc == nil {
		return nil
	}

	if (hc.BasicAuth != nil || hc.OAuth2 != nil) && (hc.BearerTokenSecret != nil) {
		return fmt.Errorf("at most one of basicAuth, oauth2, bearerTokenSecret must be configured")
	}

	if hc.Authorization != nil {
		if hc.BearerTokenSecret != nil || len(hc.BearerTokenFile) > 0 {
			return fmt.Errorf("authorization is not compatible with bearer_token_secret and Bearer_token_file")
		}

		if hc.BasicAuth != nil || hc.OAuth2 != nil {
			return fmt.Errorf("at most one of basicAuth, oauth2 & authorization must be configured")
		}

		if err := hc.Authorization.validate(); err != nil {
			return err
		}
	}

	if hc.OAuth2 != nil {
		if hc.BasicAuth != nil {
			return fmt.Errorf("at most one of basicAuth, oauth2 & authorization must be configured")
		}

		if err := hc.OAuth2.validate(); err != nil {
			return err
		}
	}

	if hc.TLSConfig != nil {
		if err := hc.TLSConfig.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (r *VMAlertmanagerConfig) AsKey() string {
	return fmt.Sprintf("%s/%s", r.Namespace, r.Name)
}

// ValidateAlertmanagerConfigSpec verifies that provided raw alertmanger configuration is logically valid
// according to alertmanager config parser
func ValidateAlertmanagerConfigSpec(srcYAML []byte) error {
	var cfgForTest amcfg.Config
	if err := yaml.UnmarshalStrict(srcYAML, &cfgForTest); err != nil {
		return err
	}
	return nil
}

func validateTimeRangeForInterval(tr TimeRange) error {
	if tr.StartTime == "" || tr.EndTime == "" {
		return fmt.Errorf("start and end are required")
	}

	start, err := parseTime(string(tr.StartTime))
	if err != nil {
		return fmt.Errorf("start time invalid: %w", err)
	}

	end, err := parseTime(string(tr.EndTime))
	if err != nil {
		return fmt.Errorf("end time invalid: %w", err)
	}

	if start >= end {
		return fmt.Errorf("start time %d cannot be equal or greater than end time %d", start, end)
	}
	return nil
}

func validateMonths(s string) error {
	startStr, endStr, err := parseRange(s)
	if err != nil {
		return err
	}

	stToInt := func(rs string) (int, error) {
		i, ok := months[strings.ToLower(rs)]
		if !ok {
			return 0, fmt.Errorf("month value=%q is not valid, expect month number of name", rs)
		}
		return i, nil
	}

	start, err := stToInt(startStr)
	if err != nil {
		return fmt.Errorf("failed to parse start month from month range: %w", err)
	}

	end, err := stToInt(endStr)
	if err != nil {
		return fmt.Errorf("failed to parse start month from month range: %w", err)
	}

	if start > end {
		return fmt.Errorf("end month %s is before start month %s", endStr, startStr)
	}
	return nil
}

var daysOfWeek = map[string]int{
	"sunday":    0,
	"monday":    1,
	"tuesday":   2,
	"wednesday": 3,
	"thursday":  4,
	"friday":    5,
	"saturday":  6,
	"0":         0,
	"1":         1,
	"2":         2,
	"3":         3,
	"4":         4,
	"5":         5,
	"6":         6,
}

func validateWeekDays(wds string) error {
	startStr, endStr, err := parseRange(wds)
	if err != nil {
		return err
	}

	stToInt := func(s string) (int, error) {
		s = strings.ToLower(s)
		dw, ok := daysOfWeek[s]
		if !ok {
			return 0, fmt.Errorf("day of week=%q is not valid, expect day name from sunday to saturday or number from 0 to 6", s)
		}
		return dw, nil
	}

	start, err := stToInt(startStr)
	if err != nil {
		return fmt.Errorf("cannot convert startStr: %w", err)
	}
	end, err := stToInt(endStr)
	if err != nil {
		return fmt.Errorf("cannot convert endStr: %w", err)
	}
	if start > end {
		return fmt.Errorf("start week day cannot be before end day")
	}

	return nil
}

func parseRange(s string) (start, end string, err error) {
	if !strings.Contains(s, ":") {
		start, end = s, s
		return
	}

	ranges := strings.Split(s, ":")
	if len(ranges) != 2 {
		err = fmt.Errorf("")
		return
	}
	return ranges[0], ranges[1], nil
}

func validateDoM(s string) error {
	startStr, endStr, err := parseRange(s)
	if err != nil {
		return fmt.Errorf("cannot parse time range for days_of_month=%q: %w", s, err)
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot parse start=%q as integer: %w", startStr, err)
	}
	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil {
		return fmt.Errorf("cannot parse end=%q as integer: %w", endStr, err)
	}

	// Check beginning <= end accounting for negatives day of month indices as well.
	// Months != 31 days can't be addressed here and are clamped, but at least we can catch blatant errors.
	if start == 0 || start < -31 || start > 31 {
		return fmt.Errorf("%d is not a valid day of the month: out of range", start)
	}
	if end == 0 || end < -31 || end > 31 {
		return fmt.Errorf("%d is not a valid day of the month: out of range", end)
	}
	// Restricting here prevents errors where begin > end in longer months but not shorter months.
	if start < 0 && end > 0 {
		return fmt.Errorf("end day must be negative if start day is negative")
	}
	// Check begin <= end. We can't know this for sure when using negative indices,
	// but we can prevent cases where its always invalid (using 28 day minimum length).
	checkBegin := start
	checkEnd := end
	if start < 0 {
		checkBegin = 28 + start
	}
	if end < 0 {
		checkEnd = 28 + end
	}
	if checkBegin > checkEnd {
		return fmt.Errorf("end day %d is always before start day %d", end, start)
	}
	return nil
}

func validateYears(s string) error {
	startStr, endStr, err := parseRange(s)
	if err != nil {
		return err
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return fmt.Errorf("start year cannot be %s parsed: %w", startStr, err)
	}

	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil {
		return fmt.Errorf("end year cannot be %s parsed: %w", endStr, err)
	}

	if start > end {
		return fmt.Errorf("end year %d is before start year %d", end, start)
	}
	return nil
}

var months = map[string]int{
	"january":   1,
	"february":  2,
	"march":     3,
	"april":     4,
	"may":       5,
	"june":      6,
	"july":      7,
	"august":    8,
	"september": 9,
	"october":   10,
	"november":  11,
	"december":  12,
	"1":         1,
	"2":         2,
	"3":         3,
	"4":         4,
	"5":         5,
	"6":         6,
	"7":         7,
	"8":         8,
	"9":         9,
	"10":        10,
	"11":        11,
	"12":        12,
}

var (
	validTime   = "^((([01][0-9])|(2[0-3])):[0-5][0-9])$|(^24:00$)"
	validTimeRE = regexp.MustCompile(validTime)
)

// Converts a string of the form "HH:MM" into the number of minutes elapsed in the day.
func parseTime(in string) (mins int, err error) {
	if !validTimeRE.MatchString(in) {
		return 0, fmt.Errorf("couldn't parse timestamp %s, invalid format", in)
	}
	timestampComponents := strings.Split(in, ":")
	if len(timestampComponents) != 2 {
		return 0, fmt.Errorf("invalid timestamp format: %s", in)
	}
	timeStampHours, err := strconv.Atoi(timestampComponents[0])
	if err != nil {
		return 0, err
	}
	timeStampMinutes, err := strconv.Atoi(timestampComponents[1])
	if err != nil {
		return 0, err
	}
	if timeStampHours < 0 || timeStampHours > 24 || timeStampMinutes < 0 || timeStampMinutes > 60 {
		return 0, fmt.Errorf("timestamp %s out of range", in)
	}
	// Timestamps are stored as minutes elapsed in the day, so multiply hours by 60.
	mins = timeStampHours*60 + timeStampMinutes
	return mins, nil
}

func validateTimeIntervals(timeIntervals []TimeIntervals) (map[string]struct{}, error) {
	timeIntervalNames := make(map[string]struct{}, len(timeIntervals))

	for idx, ti := range timeIntervals {
		if err := validateTimeIntervalsEntry(&ti); err != nil {
			return nil, fmt.Errorf("time interval at idx=%d is invalid: %w", idx, err)
		}
		if _, ok := timeIntervalNames[ti.Name]; ok {
			return nil, fmt.Errorf("time interval at idx=%d is not unique with name=%q", idx, ti.Name)
		}
		timeIntervalNames[ti.Name] = struct{}{}
	}
	return timeIntervalNames, nil
}

const opsgenieValidTypesRe = `^(team|teams|user|escalation|schedule)$`

var opsgenieTypeMatcher = regexp.MustCompile(opsgenieValidTypesRe)

// checkRouteReceiver returns an error if a node in the routing tree
// references a receiver not in the given map.
func checkRouteReceiver(r *SubRoute, receivers map[string]struct{}, tiNames map[string]struct{}) error {
	for _, ti := range r.ActiveTimeIntervals {
		if _, ok := tiNames[ti]; !ok {
			return fmt.Errorf("undefined time interval %q used in route", ti)
		}
	}
	if r.Receiver == "" {
		return nil
	}
	if _, ok := receivers[r.Receiver]; !ok {
		return fmt.Errorf("undefined receiver %q used in route", r.Receiver)
	}
	for idx, sr := range r.Routes {
		if err := checkRouteReceiver(sr, receivers, tiNames); err != nil {
			return fmt.Errorf("nested route=%d: %w", idx, err)
		}
	}

	return nil
}

func validateTimeIntervalsEntry(ti *TimeIntervals) error {
	if ti.Name == "" {
		return fmt.Errorf("empty name field for time interval")
	}

	for i, ti := range ti.TimeIntervals {
		for _, time := range ti.Times {
			if err := validateTimeRangeForInterval(time); err != nil {
				return fmt.Errorf("time range=%q at idx=%d is invalid: %w", time, i, err)
			}
		}
		for _, weekday := range ti.Weekdays {
			if err := validateWeekDays(weekday); err != nil {
				return fmt.Errorf("weekday range=%q at idx=%d is invalid: %w", weekday, i, err)
			}
		}
		for _, dom := range ti.DaysOfMonth {
			if err := validateDoM(dom); err != nil {
				return fmt.Errorf("day of month range=%q at idx=%d is invalid: %w", dom, i, err)
			}
		}
		for _, month := range ti.Months {
			if err := validateMonths(month); err != nil {
				return fmt.Errorf("month range=%q at idx=%d is invalid: %w", month, i, err)
			}
		}
		for _, year := range ti.Years {
			if err := validateYears(year); err != nil {
				return fmt.Errorf("year range=%q at idx=%d is invalid: %w", year, i, err)
			}
		}
	}
	return nil
}

func validateReceiver(recv Receiver) error {
	if recv.Name == "" {
		return fmt.Errorf("name field cannot be empty")
	}

	for idx, cfg := range recv.EmailConfigs {
		if cfg.To == "" {
			return fmt.Errorf("at idx=%d for email_configs required field `to` must be set", idx)
		}

		if cfg.Smarthost != "" {
			_, port, err := net.SplitHostPort(cfg.Smarthost)
			if err != nil {
				return err
			}
			if port == "" {
				return fmt.Errorf("at idx=%d for email_configs smarthost=%q: port cannot be empty", idx, cfg.Smarthost)
			}
		}

		if len(cfg.Headers) > 0 {
			normalizedHeaders := map[string]struct{}{}
			for key := range cfg.Headers {
				normalized := strings.ToLower(key)
				if _, ok := normalizedHeaders[normalized]; ok {
					return fmt.Errorf("at idx=%d for email_configs duplicate header %q", idx, normalized)
				}
				normalizedHeaders[normalized] = struct{}{}
			}
		}
		if cfg.TLSConfig != nil {
			if err := cfg.TLSConfig.Validate(); err != nil {
				return fmt.Errorf("at idx=%d for email_configs invalid tls_config: %w", idx, err)
			}
		}
	}
	for idx, cfg := range recv.PagerDutyConfigs {
		if cfg.URL != "" {
			if _, err := url.Parse(cfg.URL); err != nil {
				return fmt.Errorf("at idx=%d pagerduty_configs invalid url=%q: %w", idx, cfg.URL, err)
			}
		}
		if cfg.RoutingKey == nil && cfg.ServiceKey == nil {
			return fmt.Errorf("at idx=%d pagerduty_configs one of 'routing_key' or 'service_key' must be configured", idx)
		}
		if cfg.RoutingKey != nil && cfg.ServiceKey != nil {
			return fmt.Errorf("at idx=%d pagerduty_configs at most one of 'routing_key' or 'service_key' must be configured", idx)
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for pagerduty_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.PushoverConfigs {
		if cfg.UserKey == nil {
			return fmt.Errorf("at idx=%d pushover_configs required field 'user_key' must be set", idx)
		}

		if cfg.Token == nil {
			return fmt.Errorf("at idx=%d pushover_configs required field 'token' must be set", idx)
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for pushover_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.SlackConfigs {
		for _, sa := range cfg.Actions {
			if sa.Type == "" {
				return fmt.Errorf("at idx=%d required field 'action.type' for slack actions must be set", idx)
			}
			if sa.Text == "" {
				return fmt.Errorf("at idx=%d required field 'action.text' for slack actions must be set", idx)
			}

			if sa.URL == "" && sa.Name == "" {
				return fmt.Errorf("at idx=%d required field 'action.url' or 'action.name' for slack actions must be set", idx)
			}

			if sa.ConfirmField != nil && sa.ConfirmField.Text == "" {
				return fmt.Errorf("at idx=%d required field 'confirm_field.text' for slack actions must be set", idx)
			}

		}

		for _, field := range cfg.Fields {
			if field.Value == "" {
				return fmt.Errorf("at idx=%d required field 'value' for slack fields must be set", idx)
			}
			if field.Title == "" {
				return fmt.Errorf("at idx=%d required field 'title' for slack fields must be set", idx)
			}
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for slack_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.OpsGenieConfigs {
		for _, responder := range cfg.Responders {
			if responder.ID == "" && responder.Name == "" && responder.Username == "" {
				return fmt.Errorf("at idx=%d opsgenie responder must have at least an id, a name or an username defined", idx)
			}

			switch {
			case strings.Contains(responder.Type, "{{"):
				_, err := template.New("").Parse(responder.Type)
				if err != nil {
					return fmt.Errorf("responder %v type is not a valid template: %w", responder, err)
				}
			case opsgenieTypeMatcher.MatchString(responder.Type):
			default:
				return fmt.Errorf("at idx=%d opsgenie_configs responder type=%q doesnt match requirements, want either template or %s", idx, responder.Type, opsgenieTypeMatcher.String())
			}

		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for opsgenie_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.WebhookConfigs {
		if cfg.URL == nil && cfg.URLSecret == nil {
			return fmt.Errorf("at idx=%d of webhook_configs one of 'url' or 'url_secret' must be specified", idx)
		}
		if cfg.URL != nil && cfg.URLSecret != nil {
			return fmt.Errorf("at idx=%d of webhook_configs at most one of 'url' or 'url_secret' must be specified", idx)
		}
		if cfg.URL != nil {
			if _, err := url.Parse(*cfg.URL); err != nil {
				return fmt.Errorf("invalid 'url': %w", err)
			}
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for webhook_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.VictorOpsConfigs {
		// from https://github.com/prometheus/alertmanager/blob/a7f9fdadbecbb7e692d2cd8d3334e3d6de1602e1/config/notifiers.go#L497
		reservedFields := map[string]struct{}{
			"routing_key":         {},
			"message_type":        {},
			"state_message":       {},
			"entity_display_name": {},
			"monitoring_tool":     {},
			"entity_id":           {},
			"entity_state":        {},
		}

		if len(cfg.CustomFields) > 0 {
			for key := range cfg.CustomFields {
				if _, ok := reservedFields[key]; ok {
					return fmt.Errorf("at idx=%d of victorops_configs usage of reserved word %q in custom fields", idx, key)
				}
			}
		}
		if cfg.RoutingKey == "" {
			return fmt.Errorf("at idx=%d of victorops_configs missing 'routing_key' key", idx)
		}

		if cfg.APIURL != "" {
			if _, err := url.Parse(cfg.APIURL); err != nil {
				return fmt.Errorf("at idx=%d of victorops_configs incorrect api_url=%q: %w", idx, cfg.APIURL, err)
			}
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for victorops_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.WeChatConfigs {
		if cfg.APIURL != "" {
			if _, err := url.Parse(cfg.APIURL); err != nil {
				return fmt.Errorf("at idx=%d for wechat_configs incorrect api_url=%q: %w", idx, cfg.APIURL, err)
			}
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for wechat_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.TelegramConfigs {
		if cfg.BotToken == nil {
			return fmt.Errorf("at idx=%d for telegram_configs required field 'bot_token' must be set", idx)
		}
		if cfg.ChatID == 0 {
			return fmt.Errorf("at idx=%d for telegram_configs required field 'chat_id' must be set", idx)
		}

		if cfg.ParseMode != "" &&
			cfg.ParseMode != "Markdown" &&
			cfg.ParseMode != "MarkdownV2" &&
			cfg.ParseMode != "HTML" {
			return fmt.Errorf("at idx=%d unknown parse_mode=%q on telegram_config, must be Markdown, MarkdownV2, HTML or empty string", idx, cfg.ParseMode)
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for telegram_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.MSTeamsConfigs {
		if cfg.URL == nil && cfg.URLSecret == nil {
			return fmt.Errorf("at idx=%d for msteams_configs of webhook_url or webhook_url_secret must be configured", idx)
		}

		if cfg.URL != nil && cfg.URLSecret != nil {
			return fmt.Errorf("at idx=%d for msteams_configs at most one of webhook_url or webhook_url_secret must be configured", idx)
		}
		if cfg.URL != nil {
			if _, err := url.Parse(*cfg.URL); err != nil {
				return fmt.Errorf("at idx=%d for msteams_configs has invalid webhook_url=%q", idx, *cfg.URL)
			}
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for msteams_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.DiscordConfigs {
		if cfg.URL == nil && cfg.URLSecret == nil {
			return fmt.Errorf("at idx=%d for discord_configs of webhook_url or webhook_url_secret must be configured", idx)
		}

		if cfg.URL != nil && cfg.URLSecret != nil {
			return fmt.Errorf("at idx=%d for discord_configs at most one of webhook_url or webhook_url_secret must be configured", idx)
		}
		if cfg.URL != nil {
			if _, err := url.Parse(*cfg.URL); err != nil {
				return fmt.Errorf("at idx=%d for discord_configs has invalid webhook_url=%q", idx, *cfg.URL)
			}
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for discord_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.SNSConfigs {
		if cfg.TargetArn == "" && cfg.TopicArn == "" && cfg.PhoneNumber == "" {
			return fmt.Errorf("at idx=%d for sns_configs one of target_arn, topic_arn or phone_number fields must be set", idx)
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for sns_configs incorrect http_config: %w", idx, err)
		}
	}
	for idx, cfg := range recv.WebexConfigs {
		if cfg.URL != nil && *cfg.URL != "" {
			if _, err := url.Parse(*cfg.URL); err != nil {
				return fmt.Errorf("at idx=%d for webex_configs incorrect url=%q: %w", idx, *cfg.URL, err)
			}
		}
		if cfg.RoomId == "" {
			return fmt.Errorf("at idx=%d for webex_configs missing required field 'room_id'", idx)
		}
		if cfg.HTTPConfig == nil || cfg.HTTPConfig.Authorization == nil {
			return fmt.Errorf("at idx=%d for webex_configs missing http_config.authorization configuration", idx)
		}
		if err := cfg.HTTPConfig.Authorization.validate(); err != nil {
			return fmt.Errorf("at idx=%d for webex_configs incorrect http_config.authorization: %w", idx, err)
		}
		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for webex_configs incorrect http_config: %w", idx, err)
		}
	}

	for idx, cfg := range recv.JiraConfigs {
		if cfg.APIURL != nil && *cfg.APIURL != "" {
			if _, err := url.Parse(*cfg.APIURL); err != nil {
				return fmt.Errorf("at idx=%d for jira_configs incorrect url=%q: %w", idx, *cfg.APIURL, err)
			}
		}
		if cfg.Project == "" {
			return fmt.Errorf("at idx=%d for jira_configs missing required field 'project'", idx)
		}
		if cfg.IssueType == "" {
			return fmt.Errorf("at idx=%d for jira_configs missing required field 'issue_type'", idx)
		}
	}

	for idx, cfg := range recv.RocketchatConfigs {
		if cfg.APIURL != nil && *cfg.APIURL != "" {
			if _, err := url.Parse(*cfg.APIURL); err != nil {
				return fmt.Errorf("at idx=%d for jira_configs incorrect url=%q: %w", idx, *cfg.APIURL, err)
			}
		}
	}

	for idx, cfg := range recv.MSTeamsV2Configs {
		if cfg.URL == nil && cfg.URLSecret == nil {
			return fmt.Errorf("at idx=%d for msteamsv2_configs of webhook_url or webhook_url_secret must be configured", idx)
		}

		if cfg.URL != nil && cfg.URLSecret != nil {
			return fmt.Errorf("at idx=%d for msteamsv2_configs at most one of webhook_url or webhook_url_secret must be configured", idx)
		}
		if cfg.URL != nil {
			if _, err := url.Parse(*cfg.URL); err != nil {
				return fmt.Errorf("at idx=%d for msteamsv2_configs has invalid webhook_url=%q", idx, *cfg.URL)
			}
		}

		if err := cfg.HTTPConfig.validate(); err != nil {
			return fmt.Errorf("at idx=%d for msteamsv2_configs incorrect http_config: %w", idx, err)
		}
	}

	return nil
}

func init() {
	SchemeBuilder.Register(&VMAlertmanagerConfig{}, &VMAlertmanagerConfigList{})
}
