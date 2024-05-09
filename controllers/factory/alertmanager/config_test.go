package alertmanager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/build"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
)

func TestBuildConfig(t *testing.T) {
	type args struct {
		ctx                     context.Context
		disableNamespaceMatcher bool
		baseCfg                 []byte
		amcfgs                  map[string]*operatorv1beta1.VMAlertmanagerConfig
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		want              string
		parseError        string
		wantErr           bool
	}{
		{
			name: "simple ok",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "email",
									EmailConfigs: []operatorv1beta1.EmailConfig{
										{
											SendResolved: ptr.To(true),
											From:         "some-sender",
											To:           "some-dst",
											Text:         "some-text",
											TLSConfig: &operatorv1beta1.TLSConfig{
												CertFile: "some_cert_path",
											},
										},
										{
											SendResolved: ptr.To(true),
											From:         "other-sender",
											To:           "other-dst",
											Text:         "other-text",
											RequireTLS:   ptr.To(false),
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "email",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			want: `global:
  time_out: 1min
route:
  receiver: default-base-email
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-email
    continue: true
receivers:
- name: default-base-email
  email_configs:
  - tls_config:
      cert_file: some_cert_path
    from: some-sender
    text: some-text
    to: some-dst
    send_resolved: true
  - require_tls: false
    from: other-sender
    text: other-text
    to: other-dst
    send_resolved: true
templates: []
`,
		},
		{
			name: "complex with providers",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							InhibitRules: []operatorv1beta1.InhibitRule{
								{
									Equal:          []string{`name = "db"`},
									SourceMatchers: []string{`job != "alertmanager"`},
								},
							},
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "email",
									EmailConfigs: []operatorv1beta1.EmailConfig{
										{
											SendResolved: ptr.To(true),
											From:         "some-sender",
											To:           "some-dst",
											Text:         "some-text",
											TLSConfig: &operatorv1beta1.TLSConfig{
												CertFile: "some_cert_path",
											},
										},
									},
								},
								{
									Name: "webhook",
									WebhookConfigs: []operatorv1beta1.WebhookConfig{
										{
											URL: ptr.To("http://some-wh"),
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "email",
								GroupWait: "1min",
								Routes: []*operatorv1beta1.SubRoute{
									{
										Receiver: "webhook",
									},
								},
							},
						},
					},
					"monitoring/scrape": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "scrape",
							Namespace: "monitoring",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							InhibitRules: []operatorv1beta1.InhibitRule{
								{
									Equal: []string{`name = "scrape"`},
								},
							},
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "global",
									OpsGenieConfigs: []operatorv1beta1.OpsGenieConfig{
										{
											SendResolved: ptr.To(true),
											APIURL:       "https://opsgen",
											Details:      map[string]string{"msg": "critical"},
											Responders: []operatorv1beta1.OpsGenieConfigResponder{
												{
													Name:     "n",
													Username: "f",
													Type:     "some-type",
												},
											},
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver: "global",
							},
						},
					},
				},
			},
			want: `global:
  time_out: 1min
route:
  receiver: default-base-email
  routes:
  - routes:
    - receiver: default-base-webhook
      continue: false
    matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-email
    continue: true
  - matchers:
    - namespace = "monitoring"
    receiver: monitoring-scrape-global
    continue: true
inhibit_rules:
- target_matchers:
  - namespace = "default"
  source_matchers:
  - job != "alertmanager"
  - namespace = "default"
  equal:
  - name = "db"
- target_matchers:
  - namespace = "monitoring"
  source_matchers:
  - namespace = "monitoring"
  equal:
  - name = "scrape"
receivers:
- name: default-base-email
  email_configs:
  - tls_config:
      cert_file: some_cert_path
    from: some-sender
    text: some-text
    to: some-dst
    send_resolved: true
- name: default-base-webhook
  webhook_configs:
  - url: http://some-wh
- name: monitoring-scrape-global
  opsgenie_configs:
  - api_url: https://opsgen
    details:
      msg: critical
    send_resolved: true
    responders:
    - name: "n"
      username: f
      type: some-type
templates: []
`,
		},
		{
			name: "webhook ok",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "webhook",
									WebhookConfigs: []operatorv1beta1.WebhookConfig{
										{
											SendResolved: ptr.To(true),
											URLSecret: &v1.SecretKeySelector{
												Key: "url",
												LocalObjectReference: v1.LocalObjectReference{
													Name: "webhook",
												},
											},
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "webhook",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "webhook",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"url": []byte("https://webhook.example.com"),
					},
				},
			},
			want: `global:
  time_out: 1min
route:
  receiver: default-base-webhook
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-webhook
    continue: true
receivers:
- name: default-base-webhook
  webhook_configs:
  - send_resolved: true
    url: https://webhook.example.com
templates: []
`,
		},
		{
			name: "slack ok",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "slack",
									SlackConfigs: []operatorv1beta1.SlackConfig{
										{
											APIURL: &v1.SecretKeySelector{
												Key: "url",
												LocalObjectReference: v1.LocalObjectReference{
													Name: "slack",
												},
											},
											SendResolved: ptr.To(true),
											Text:         "some-text",
											Title:        "some-title",
											LinkNames:    false,
											ThumbURL:     "some-url",
											Pretext:      "text-1",
											Username:     "some-user",
											Actions: []operatorv1beta1.SlackAction{
												{
													Name: "deny",
													Text: "text-5",
													URL:  "some-url",
													ConfirmField: &operatorv1beta1.SlackConfirmationField{
														Text: "confirmed",
													},
												},
											},
											Fields: []operatorv1beta1.SlackField{
												{
													Short: ptr.To(true),
													Title: "fields",
												},
											},
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "slack",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "slack",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"url": []byte("https://slack.example.com"),
					},
				},
			},
			want: `global:
  time_out: 1min
route:
  receiver: default-base-slack
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-slack
    continue: true
receivers:
- name: default-base-slack
  slack_configs:
  - api_url: https://slack.example.com
    send_resolved: true
    username: some-user
    pretext: text-1
    text: some-text
    title: some-title
    thumb_url: some-url
    actions:
    - name: deny
      text: text-5
      url: some-url
      confirm:
        text: confirmed
    fields:
    - title: fields
      short: true
templates: []
`,
		},
		{
			name: "pagerduty ok",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "pagerduty",
									PagerDutyConfigs: []operatorv1beta1.PagerDutyConfig{
										{
											SendResolved: ptr.To(true),
											RoutingKey: &v1.SecretKeySelector{
												Key: "some-key",
												LocalObjectReference: v1.LocalObjectReference{
													Name: "some-secret",
												},
											},
											Class:    "some-class",
											Group:    "some-group",
											Severity: "warning",
											Images: []operatorv1beta1.ImageConfig{
												{
													Href:   "http://some-href",
													Source: "http://some-source",
													Alt:    "some-alt-text",
												},
											},
											Links: []operatorv1beta1.LinkConfig{
												{
													Href: "http://some-href",
													Text: "some-text",
												},
											},
											Details: map[string]string{
												"alertname":    "alert-name",
												"firing":       "alert-title",
												"instance":     "alert-instance",
												"message":      "alert-message",
												"num_firing":   "1",
												"num_resolved": "0",
												"resolved":     "alert-title",
												"summary":      "alert-summary",
											},
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "pagerduty",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "some-secret", Namespace: "default"},
					Data:       map[string][]byte{"some-key": []byte(`some-value`)},
				},
			},
			want: `global:
  time_out: 1min
route:
  receiver: default-base-pagerduty
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-pagerduty
    continue: true
receivers:
- name: default-base-pagerduty
  pagerduty_configs:
  - routing_key: some-value
    class: some-class
    group: some-group
    severity: warning
    images:
    - href: http://some-href
      source: http://some-source
      alt: some-alt-text
    links:
    - href: http://some-href
      text: some-text
    details:
      alertname: alert-name
      firing: alert-title
      instance: alert-instance
      message: alert-message
      num_firing: "1"
      num_resolved: "0"
      resolved: alert-title
      summary: alert-summary
    send_resolved: true
templates: []
`,
		},
		{
			name: "telegram ok",
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tg-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"token": []byte("some-token"),
					},
				},
			},
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "tg",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "telegram",
									TelegramConfigs: []operatorv1beta1.TelegramConfig{
										{
											SendResolved: ptr.To(true),
											ChatID:       125,
											BotToken: &v1.SecretKeySelector{
												LocalObjectReference: v1.LocalObjectReference{
													Name: "tg-secret",
												},
												Key: "token",
											},
											Message: "some-templated message",
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "telegram",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			want: `global:
  time_out: 1min
route:
  receiver: default-tg-telegram
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-tg-telegram
    continue: true
receivers:
- name: default-tg-telegram
  telegram_configs:
  - bot_token: some-token
    send_resolved: true
    chat_id: 125
    message: some-templated message
templates: []
`,
		},
		{
			name: "slack bad, with invalid api_url",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "slack",
									SlackConfigs: []operatorv1beta1.SlackConfig{
										{
											APIURL: &v1.SecretKeySelector{
												Key: "bad_url",
												LocalObjectReference: v1.LocalObjectReference{
													Name: "slack",
												},
											},
											SendResolved: ptr.To(true),
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "slack",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "slack",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"bad_url": []byte("bad_url"),
					},
				},
			},
			parseError: "invalid URL bad_url in key bad_url from secret slack: unsupported scheme \"\" for URL in object: default/base, will ignore vmalertmanagerconfig base",
			want: `global:
  time_out: 1min
templates: []
`,
		},
		{
			name: "telegram bad, not strict parse",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "tg",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "telegram",
									TelegramConfigs: []operatorv1beta1.TelegramConfig{
										{
											SendResolved: ptr.To(true),
											ChatID:       125,
											BotToken: &v1.SecretKeySelector{
												LocalObjectReference: v1.LocalObjectReference{
													Name: "tg-secret",
												},
												Key: "token",
											},
											Message: "some-templated message",
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "telegram",
								GroupWait: "1min",
							},
						},
					},
					"mon/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "tg",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "telegram",
									TelegramConfigs: []operatorv1beta1.TelegramConfig{
										{
											SendResolved: ptr.To(true),
											ChatID:       125,
											Message:      "some-templated message",
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "telegram",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			parseError: "cannot find secret for VMAlertmanager config: tg, err :secrets \"tg-secret\" not found in object: default/tg, will ignore vmalertmanagerconfig tg",
			want: `global:
  time_out: 1min
route:
  receiver: default-tg-telegram
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-tg-telegram
    continue: true
receivers:
- name: default-tg-telegram
  telegram_configs:
  - send_resolved: true
    chat_id: 125
    message: some-templated message
templates: []
`,
		},
		{
			name: "wrong alertmanagerconfig: with duplicate receiver",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []operatorv1beta1.Receiver{
								{
									Name: "duplicate-receiver",
									EmailConfigs: []operatorv1beta1.EmailConfig{
										{
											SendResolved: ptr.To(true),
											From:         "some-sender",
											To:           "some-dst",
											Text:         "some-text",
											TLSConfig: &operatorv1beta1.TLSConfig{
												CertFile: "some_cert_path",
											},
										},
									},
								},
								{
									Name: "duplicate-receiver",
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "duplicate-receiver",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			parseError: "got duplicate receiver name duplicate-receiver in object default/base, will ignore vmalertmanagerconfig base",
			want: `global:
  time_out: 1min
templates: []
`,
		},
		{
			name: "wrong alertmanagerconfig: with duplicate time interval",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							TimeIntervals: []operatorv1beta1.MuteTimeInterval{
								{
									Name:          "duplicate-interval",
									TimeIntervals: []operatorv1beta1.TimeInterval{{Times: []operatorv1beta1.TimeRange{{StartTime: "00:00", EndTime: "10:00"}}}},
								},
								{
									Name:          "duplicate-interval",
									TimeIntervals: []operatorv1beta1.TimeInterval{{Times: []operatorv1beta1.TimeRange{{StartTime: "08:00", EndTime: "10:00"}}}},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:            "email",
								GroupWait:           "1min",
								ActiveTimeIntervals: []string{"duplicate-interval"},
							},
						},
					},
				},
			},
			parseError: "got duplicate timeInterval name duplicate-interval in object default/base, will ignore vmalertmanagerconfig base",
			want: `global:
  time_out: 1min
templates: []
`,
		},
		{
			name: "wrong alertmanagerconfig: undefined receiver",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Route: &operatorv1beta1.Route{
								Receiver:  "receiver-not-defined",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			parseError: "receiver receiver-not-defined not defined in object default/base, will ignore vmalertmanagerconfig base",
			want: `global:
  time_out: 1min
templates: []
`,
		},
		{
			name: "wrong alertmanagerconfig: undefined timeInterval",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: operatorv1beta1.VMAlertmanagerConfigSpec{
							Route: &operatorv1beta1.Route{
								ActiveTimeIntervals: []string{"interval-not-defined"},
								GroupWait:           "1min",
							},
						},
					},
				},
			},
			parseError: "time_intervals interval-not-defined not defined in object default/base, will ignore vmalertmanagerconfig base",
			want: `global:
  time_out: 1min
templates: []
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := buildConfig(tt.args.ctx, testClient, !tt.args.disableNamespaceMatcher, tt.args.disableNamespaceMatcher, tt.args.baseCfg, tt.args.amcfgs, map[string]string{})
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got.ParseErrors) > 0 {
				assert.Equal(t, tt.parseError, got.ParseErrors[0])
			}
			assert.Equal(t, tt.want, string(got.Data))
		})
	}
}

func TestAddConfigTemplates(t *testing.T) {
	type args struct {
		config    []byte
		templates []string
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		want              string
		wantErr           bool
	}{
		{
			name: "add templates to empty config",
			args: args{
				config:    []byte{},
				templates: []string{"/etc/vm/templates/test/template1.tmpl"},
			},
			want: `templates:
- /etc/vm/templates/test/template1.tmpl
`,
			wantErr: false,
		},
		{
			name: "add templates to config without templates",
			args: args{
				config: []byte(`global:
  resolve_timeout: 5m
route:
  receiver: webhook
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
receivers:
- name: webhook
  webhook_configs:
  - url: http://localhost:30500/
`),
				templates: []string{
					"/etc/vm/templates/test/template1.tmpl",
					"/etc/vm/templates/test/template2.tmpl",
				},
			},
			want: `global:
  resolve_timeout: 5m
route:
  receiver: webhook
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
receivers:
- name: webhook
  webhook_configs:
  - url: http://localhost:30500/
templates:
- /etc/vm/templates/test/template1.tmpl
- /etc/vm/templates/test/template2.tmpl
`,
			wantErr: false,
		},
		{
			name: "add templates to config with templates",
			args: args{
				config: []byte(`global:
  resolve_timeout: 5m
route:
  receiver: webhook
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
receivers:
- name: webhook
  webhook_configs:
  - url: http://localhost:30500/
templates:
- /etc/vm/templates/test/template1.tmpl
- /etc/vm/templates/test/template2.tmpl
`),
				templates: []string{
					"/etc/vm/templates/test/template3.tmpl",
					"/etc/vm/templates/test/template4.tmpl",
					"/etc/vm/templates/test/template0.tmpl",
				},
			},
			want: `global:
  resolve_timeout: 5m
route:
  receiver: webhook
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
receivers:
- name: webhook
  webhook_configs:
  - url: http://localhost:30500/
templates:
- /etc/vm/templates/test/template1.tmpl
- /etc/vm/templates/test/template2.tmpl
- /etc/vm/templates/test/template3.tmpl
- /etc/vm/templates/test/template4.tmpl
- /etc/vm/templates/test/template0.tmpl
`,
			wantErr: false,
		},
		{
			name: "add empty and duplicated templates",
			args: args{
				config: []byte{},
				templates: []string{
					"",
					"/etc/vm/templates/test/template1.tmpl",
					" ",
					"/etc/vm/templates/test/template1.tmpl",
					"\t",
				},
			},
			want: `templates:
- /etc/vm/templates/test/template1.tmpl
`,
			wantErr: false,
		},
		{
			name: "add empty templates list",
			args: args{
				config:    []byte(`test`),
				templates: []string{},
			},
			want:    `test`,
			wantErr: false,
		},
		{
			name: "wrong config",
			args: args{
				config:    []byte(`test`),
				templates: []string{"test"},
			},
			want:    ``,
			wantErr: true,
		},
		{
			name: "add template duplicates without path",
			args: args{
				config: []byte(`global:
  resolve_timeout: 5m
route:
  receiver: webhook
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
receivers:
- name: webhook
  webhook_configs:
  - url: http://localhost:30500/
templates:
- template1.tmpl
- template2.tmpl
- /etc/vm/templates/test/template3.tmpl
`),
				templates: []string{
					"/etc/vm/templates/test/template1.tmpl",
					"/etc/vm/templates/test/template2.tmpl",
					"/etc/vm/templates/test/template0.tmpl",
				},
			},
			want: `global:
  resolve_timeout: 5m
route:
  receiver: webhook
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
receivers:
- name: webhook
  webhook_configs:
  - url: http://localhost:30500/
templates:
- /etc/vm/templates/test/template1.tmpl
- /etc/vm/templates/test/template2.tmpl
- /etc/vm/templates/test/template3.tmpl
- /etc/vm/templates/test/template0.tmpl
`,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := addConfigTemplates(tt.args.config, tt.args.templates)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddConfigTemplates() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func Test_configBuilder_buildHTTPConfig(t *testing.T) {
	type fields struct {
		secretCache    map[string]*v1.Secret
		configmapCache map[string]*v1.ConfigMap
	}
	type args struct {
		httpCfg *operatorv1beta1.HTTPConfig
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "build empty config",
			want: "{}\n",
		},
		{
			name: "with basic auth",
			args: args{
				httpCfg: &operatorv1beta1.HTTPConfig{
					BasicAuth: &operatorv1beta1.BasicAuth{
						Username: v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "secret-store",
							},
							Key: "username",
						},
						PasswordFile: "/etc/vm/secrets/password_file",
					},
				},
			},
			fields: fields{secretCache: map[string]*v1.Secret{
				"secret-store": {
					Data: map[string][]byte{
						"username": []byte("user-1"),
					},
				},
			}},
			want: `basic_auth:
  username: user-1
  password_file: /etc/vm/secrets/password_file
`,
		},
		{
			name: "with tls and bearer",
			args: args{
				httpCfg: &operatorv1beta1.HTTPConfig{
					BearerTokenFile: "/etc/mounted_dir/bearer_file",
					TLSConfig: &operatorv1beta1.TLSConfig{
						InsecureSkipVerify: true,
						Cert: operatorv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "secret-store",
								},
								Key: "cert",
							},
						},
						CA: operatorv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "secret-store",
								},
								Key: "ca",
							},
						},
						KeyFile: "/etc/mounted_dir/key.pem",
					},
				},
			},
			fields: fields{secretCache: map[string]*v1.Secret{
				"secret-store": {
					Data: map[string][]byte{
						"cert": []byte("---PEM---"),
						"ca":   []byte("---PEM-CA"),
					},
				},
			}},
			want: `tls_config:
  ca_file: /etc/alertmanager/config/default_secret-store_ca
  cert_file: /etc/alertmanager/config/default_secret-store_cert
  insecure_skip_verify: true
  key_file: /etc/mounted_dir/key.pem
authorization:
  credentials_file: /etc/mounted_dir/bearer_file
`,
		},
		{
			name: "with tls (configmap) and bearer",
			args: args{
				httpCfg: &operatorv1beta1.HTTPConfig{
					BearerTokenSecret: &v1.SecretKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "secret-bearer",
						},
						Key: "token",
					},
					TLSConfig: &operatorv1beta1.TLSConfig{
						InsecureSkipVerify: true,
						Cert: operatorv1beta1.SecretOrConfigMap{
							Secret: &v1.SecretKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "secret-store",
								},
								Key: "cert",
							},
						},
						CA: operatorv1beta1.SecretOrConfigMap{
							ConfigMap: &v1.ConfigMapKeySelector{
								LocalObjectReference: v1.LocalObjectReference{
									Name: "cm-store",
								},
								Key: "ca",
							},
						},
						KeySecret: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "secret-store",
							},
							Key: "key",
						},
					},
				},
			},
			fields: fields{
				secretCache: map[string]*v1.Secret{
					"secret-store": {
						Data: map[string][]byte{
							"cert": []byte("---PEM---"),
							"key":  []byte("--KEY-PEM--"),
						},
					},
					"secret-bearer": {
						Data: map[string][]byte{
							"token": []byte("secret-token"),
						},
					},
				},
				configmapCache: map[string]*v1.ConfigMap{
					"cm-store": {
						Data: map[string]string{
							"ca": "--CA-PEM--",
						},
					},
				},
			},
			want: `tls_config:
  ca_file: /etc/alertmanager/config/default_cm-store_ca
  cert_file: /etc/alertmanager/config/default_secret-store_cert
  insecure_skip_verify: true
  key_file: /etc/alertmanager/config/default_secret-store_key
authorization:
  credentials: secret-token
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := &configBuilder{
				ConfigBuilder: build.ConfigBuilder{
					Ctx:                context.Background(),
					Client:             k8stools.GetTestClientWithObjects(nil),
					SecretCache:        tt.fields.secretCache,
					ConfigmapCache:     tt.fields.configmapCache,
					TlsAssets:          map[string]string{},
					CurrentCRName:      "test-am",
					CurrentCRNamespace: "default",
				},
			}
			gotYAML, err := cb.buildHTTPConfig(tt.args.httpCfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildHTTPConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			got, err := yaml.Marshal(gotYAML)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildHTTPConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equalf(t, tt.want, string(got), "buildHTTPConfig(%v)", tt.args.httpCfg)
		})
	}
}
