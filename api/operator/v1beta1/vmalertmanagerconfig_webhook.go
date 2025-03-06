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
	"context"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var vmAlertmanagerConfigValidator admission.CustomValidator = &VMAlertmanagerConfig{}

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VMAlertmanagerConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-operator-victoriametrics-com-v1beta1-vmalertmanagerconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=operator.victoriametrics.com,resources=vmalertmanagerconfigs,verbs=create;update,versions=v1beta1,name=vvmalertmanagerconfig.kb.io,admissionReviewVersions=v1

// Validate performs logical validation
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

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (*VMAlertmanagerConfig) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*VMAlertmanagerConfig)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", obj)
	}

	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (*VMAlertmanagerConfig) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*VMAlertmanagerConfig)
	if !ok {
		return nil, fmt.Errorf("BUG: unexpected type: %T", newObj)
	}
	if r.Spec.ParsingError != "" {
		return nil, errors.New(r.Spec.ParsingError)
	}

	if err := r.Validate(); err != nil {
		return nil, err
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (*VMAlertmanagerConfig) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

const opsgenieValidTypesRe = `^(team|teams|user|escalation|schedule)$`

var opsgenieTypeMatcher = regexp.MustCompile(opsgenieValidTypesRe)

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

	return nil
}

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
