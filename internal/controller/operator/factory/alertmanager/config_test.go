package alertmanager

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestBuildConfig(t *testing.T) {
	type args struct {
		ctx                     context.Context
		disableNamespaceMatcher bool
		baseCfg                 []byte
		amcfgs                  map[string]*vmv1beta1.VMAlertmanagerConfig
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "email",
									EmailConfigs: []vmv1beta1.EmailConfig{
										{
											SendResolved: ptr.To(true),
											From:         "some-sender",
											To:           "some-dst",
											Text:         "some-text",
											TLSConfig: &vmv1beta1.TLSConfig{
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
							Route: &vmv1beta1.Route{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							InhibitRules: []vmv1beta1.InhibitRule{
								{
									Equal:          []string{`name = "db"`},
									SourceMatchers: []string{`job != "alertmanager"`},
								},
							},
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "email",
									EmailConfigs: []vmv1beta1.EmailConfig{
										{
											SendResolved: ptr.To(true),
											From:         "some-sender",
											To:           "some-dst",
											Text:         "some-text",
											TLSConfig: &vmv1beta1.TLSConfig{
												CertFile: "some_cert_path",
											},
										},
									},
								},
								{
									Name: "webhook",
									WebhookConfigs: []vmv1beta1.WebhookConfig{
										{
											URL: ptr.To("http://some-wh"),
										},
									},
								},
							},
							Route: &vmv1beta1.Route{
								Receiver:  "email",
								GroupWait: "1min",
								Routes: []*vmv1beta1.SubRoute{
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
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							InhibitRules: []vmv1beta1.InhibitRule{
								{
									Equal: []string{`name = "scrape"`},
								},
							},
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "global",
									OpsGenieConfigs: []vmv1beta1.OpsGenieConfig{
										{
											SendResolved: ptr.To(true),
											APIURL:       "https://opsgen",
											Details:      map[string]string{"msg": "critical"},
											Responders: []vmv1beta1.OpsGenieConfigResponder{
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
							Route: &vmv1beta1.Route{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "webhook",
									WebhookConfigs: []vmv1beta1.WebhookConfig{
										{
											SendResolved: ptr.To(true),
											URLSecret: &corev1.SecretKeySelector{
												Key: "url",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "webhook",
												},
											},
										},
									},
								},
							},
							Route: &vmv1beta1.Route{
								Receiver:  "webhook",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "slack",
									SlackConfigs: []vmv1beta1.SlackConfig{
										{
											APIURL: &corev1.SecretKeySelector{
												Key: "url",
												LocalObjectReference: corev1.LocalObjectReference{
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
											Actions: []vmv1beta1.SlackAction{
												{
													Name: "deny",
													Text: "text-5",
													URL:  "some-url",
													ConfirmField: &vmv1beta1.SlackConfirmationField{
														Text: "confirmed",
													},
												},
											},
											Fields: []vmv1beta1.SlackField{
												{
													Short: ptr.To(true),
													Title: "fields",
												},
											},
										},
									},
								},
							},
							Route: &vmv1beta1.Route{
								Receiver:  "slack",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "pagerduty",
									PagerDutyConfigs: []vmv1beta1.PagerDutyConfig{
										{
											SendResolved: ptr.To(true),
											RoutingKey: &corev1.SecretKeySelector{
												Key: "some-key",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "some-secret",
												},
											},
											Class:    "some-class",
											Group:    "some-group",
											Severity: "warning",
											Images: []vmv1beta1.ImageConfig{
												{
													Href:   "http://some-href",
													Source: "http://some-source",
													Alt:    "some-alt-text",
												},
											},
											Links: []vmv1beta1.LinkConfig{
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
							Route: &vmv1beta1.Route{
								Receiver:  "pagerduty",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
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
				&corev1.Secret{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "tg",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "telegram",
									TelegramConfigs: []vmv1beta1.TelegramConfig{
										{
											SendResolved: ptr.To(true),
											ChatID:       125,
											BotToken: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tg-secret",
												},
												Key: "token",
											},
											Message: "some-templated message",
										},
									},
								},
							},
							Route: &vmv1beta1.Route{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "slack",
									SlackConfigs: []vmv1beta1.SlackConfig{
										{
											APIURL: &corev1.SecretKeySelector{
												Key: "bad_url",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "slack",
												},
											},
											SendResolved: ptr.To(true),
										},
									},
								},
							},
							Route: &vmv1beta1.Route{
								Receiver:  "slack",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "tg",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "telegram",
									TelegramConfigs: []vmv1beta1.TelegramConfig{
										{
											SendResolved: ptr.To(true),
											ChatID:       125,
											BotToken: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "tg-secret",
												},
												Key: "token",
											},
											Message: "some-templated message",
										},
									},
								},
							},
							Route: &vmv1beta1.Route{
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
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "telegram",
									TelegramConfigs: []vmv1beta1.TelegramConfig{
										{
											SendResolved: ptr.To(true),
											ChatID:       125,
											Message:      "some-templated message",
										},
									},
								},
							},
							Route: &vmv1beta1.Route{
								Receiver:  "telegram",
								GroupWait: "1min",
							},
						},
					},
				},
			},
			parseError: `cannot find secret="tg-secret" to fetch content at ns="default", err: secrets "tg-secret" not found in object: default/tg, will ignore vmalertmanagerconfig tg`,
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Receivers: []vmv1beta1.Receiver{
								{
									Name: "duplicate-receiver",
									EmailConfigs: []vmv1beta1.EmailConfig{
										{
											SendResolved: ptr.To(true),
											From:         "some-sender",
											To:           "some-dst",
											Text:         "some-text",
											TLSConfig: &vmv1beta1.TLSConfig{
												CertFile: "some_cert_path",
											},
										},
									},
								},
								{
									Name: "duplicate-receiver",
								},
							},
							Route: &vmv1beta1.Route{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							TimeIntervals: []vmv1beta1.MuteTimeInterval{
								{
									Name:          "duplicate-interval",
									TimeIntervals: []vmv1beta1.TimeInterval{{Times: []vmv1beta1.TimeRange{{StartTime: "00:00", EndTime: "10:00"}}}},
								},
								{
									Name:          "duplicate-interval",
									TimeIntervals: []vmv1beta1.TimeInterval{{Times: []vmv1beta1.TimeRange{{StartTime: "08:00", EndTime: "10:00"}}}},
								},
							},
							Route: &vmv1beta1.Route{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Route: &vmv1beta1.Route{
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
				amcfgs: map[string]*vmv1beta1.VMAlertmanagerConfig{
					"default/base": {
						ObjectMeta: metav1.ObjectMeta{
							Name:      "base",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMAlertmanagerConfigSpec{
							Route: &vmv1beta1.Route{
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
		secretCache    map[string]*corev1.Secret
		configmapCache map[string]*corev1.ConfigMap
	}
	type args struct {
		httpCfg *vmv1beta1.HTTPConfig
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
				httpCfg: &vmv1beta1.HTTPConfig{
					BasicAuth: &vmv1beta1.BasicAuth{
						Username: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "secret-store",
							},
							Key: "username",
						},
						PasswordFile: "/etc/vm/secrets/password_file",
					},
				},
			},
			fields: fields{secretCache: map[string]*corev1.Secret{
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
				httpCfg: &vmv1beta1.HTTPConfig{
					BearerTokenFile: "/etc/mounted_dir/bearer_file",
					TLSConfig: &vmv1beta1.TLSConfig{
						InsecureSkipVerify: true,
						Cert: vmv1beta1.SecretOrConfigMap{
							Secret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "secret-store",
								},
								Key: "cert",
							},
						},
						CA: vmv1beta1.SecretOrConfigMap{
							Secret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "secret-store",
								},
								Key: "ca",
							},
						},
						KeyFile: "/etc/mounted_dir/key.pem",
					},
				},
			},
			fields: fields{secretCache: map[string]*corev1.Secret{
				"secret-store": {
					Data: map[string][]byte{
						"cert": []byte("---PEM---"),
						"ca":   []byte("---PEM-CA"),
					},
				},
			}},
			want: `tls_config:
  ca_file: /etc/alertmanager/tls_assets/default_secret-store_ca
  cert_file: /etc/alertmanager/tls_assets/default_secret-store_cert
  insecure_skip_verify: true
  key_file: /etc/mounted_dir/key.pem
authorization:
  credentials_file: /etc/mounted_dir/bearer_file
`,
		},
		{
			name: "with tls (configmap) and bearer",
			args: args{
				httpCfg: &vmv1beta1.HTTPConfig{
					BearerTokenSecret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "secret-bearer",
						},
						Key: "token",
					},
					TLSConfig: &vmv1beta1.TLSConfig{
						InsecureSkipVerify: true,
						Cert: vmv1beta1.SecretOrConfigMap{
							Secret: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "secret-store",
								},
								Key: "cert",
							},
						},
						CA: vmv1beta1.SecretOrConfigMap{
							ConfigMap: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "cm-store",
								},
								Key: "ca",
							},
						},
						KeySecret: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "secret-store",
							},
							Key: "key",
						},
					},
				},
			},
			fields: fields{
				secretCache: map[string]*corev1.Secret{
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
				configmapCache: map[string]*corev1.ConfigMap{
					"cm-store": {
						Data: map[string]string{
							"ca": "--CA-PEM--",
						},
					},
				},
			},
			want: `tls_config:
  ca_file: /etc/alertmanager/tls_assets/default_cm-store_ca
  cert_file: /etc/alertmanager/tls_assets/default_secret-store_cert
  insecure_skip_verify: true
  key_file: /etc/alertmanager/tls_assets/default_secret-store_key
authorization:
  credentials: secret-token
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := &configBuilder{
				TLSConfigBuilder: build.TLSConfigBuilder{
					Ctx:                context.Background(),
					Client:             k8stools.GetTestClientWithObjects(nil),
					SecretCache:        tt.fields.secretCache,
					ConfigmapCache:     tt.fields.configmapCache,
					TLSAssets:          map[string]string{},
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

func Test_UpdateDefaultAMConfig(t *testing.T) {
	type args struct {
		ctx context.Context
		cr  *vmv1beta1.VMAlertmanager
	}
	tests := []struct {
		name                string
		args                args
		wantErr             bool
		predefinedObjects   []runtime.Object
		secretMustBeMissing bool
	}{
		{
			name: "with alertmanager config support",
			args: args{
				ctx: context.TODO(),
				cr: &vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-am",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						ConfigSecret:            "vmalertmanager-test-am-config",
						ConfigRawYaml:           "global: {}",
						ConfigSelector:          &metav1.LabelSelector{},
						ConfigNamespaceSelector: &metav1.LabelSelector{},
						SelectAllByDefault:      true,
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmalertmanager-test-am-config",
						Namespace: "default",
					},
					Data: map[string][]byte{alertmanagerSecretConfigKey: {}},
				},
				&vmv1beta1.VMAlertmanagerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-amc",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMAlertmanagerConfigSpec{
						InhibitRules: []vmv1beta1.InhibitRule{
							{Equal: []string{"alertname"}, SourceMatchers: []string{"severity=\"critical\""}, TargetMatchers: []string{"severity=\"warning\""}},
							{SourceMatchers: []string{"alertname=\"QuietWeeklyNotifications\""}, TargetMatchers: []string{"alert_group=\"l2ci_weekly\""}},
						},
						Route: &vmv1beta1.Route{
							GroupBy:  []string{"alertname", "l2ci_channel"},
							Receiver: "blackhole",
							Routes: []*vmv1beta1.SubRoute{
								{Receiver: "blackhole", Matchers: []string{"alertname=\"QuietWeeklyNotifications\""}},
								{Receiver: "blackhole", Matchers: []string{"alertname=\"QuietDailyNotifications\""}},
								{Receiver: "l2ci_receiver", Matchers: []string{"alert_group=~\"^l2ci.*\""}},
							},
						},
						Receivers: []vmv1beta1.Receiver{
							{
								Name: "l2ci_receiver",
								WebhookConfigs: []vmv1beta1.WebhookConfig{
									{URL: ptr.To("http://notification_stub_ci1:8080")},
								},
							},
							{Name: "blackhole"},
							{
								Name: "ca_em_receiver",
								WebhookConfigs: []vmv1beta1.WebhookConfig{
									{URL: ptr.To("http://notification_stub_ci2:8080")},
								},
							},
						},
					},
				},
			},
		},
	}
	assert.Nil(t, os.Setenv("WATCH_NAMESPACE", "default"))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			// Create secret with alert manager config
			if err := createDefaultAMConfig(tt.args.ctx, tt.args.cr, fclient); (err != nil) != tt.wantErr {
				t.Fatalf("createDefaultAMConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			var createdSecret corev1.Secret
			secretName := tt.args.cr.ConfigSecretName()
			err := fclient.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: secretName}, &createdSecret)
			if err != nil {
				if errors.IsNotFound(err) && tt.secretMustBeMissing {
					return
				}
				t.Fatalf("config for alertmanager not exist, err: %v", err)
			}

			// check secret config after creating
			d, ok := createdSecret.Data[alertmanagerSecretConfigKey]
			if !ok {
				t.Fatalf("config for alertmanager not exist, err: %v", err)
			}
			var secretConfig alertmanagerConfig
			err = yaml.Unmarshal(d, &secretConfig)
			if err != nil {
				t.Fatalf("could not unmarshall secret config data into structure, err: %v", err)
			}
			var amc vmv1beta1.VMAlertmanagerConfig
			err = fclient.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: "test-amc"}, &amc)
			if err != nil {
				t.Fatalf("could not get alert manager config. Error: %v", err)
			}

			if len(secretConfig.Receivers) != len(amc.Spec.Receivers) {
				t.Fatalf("receivers count is wrong. Expected: %v, actual: %v", len(amc.Spec.Receivers), len(secretConfig.Receivers))
			}

			if len(secretConfig.InhibitRules) != len(amc.Spec.InhibitRules) {
				t.Fatalf("inhibit rules count is wrong. Expected: %v, actual: %v", len(amc.Spec.InhibitRules), len(secretConfig.InhibitRules))
			}

			if !strings.EqualFold(buildCRPrefixedName(&amc, amc.Spec.Route.Receiver), secretConfig.Route.Receiver) {
				t.Fatalf("receiver name is wrong. Expected: %v, actual: %v", buildCRPrefixedName(&amc, amc.Spec.Route.Receiver), secretConfig.Route.Receiver)
			}
			if len(secretConfig.Route.Routes) != 1 {
				t.Fatalf("subroutes count is wrong. Expected: %v, actual: %v", 1, len(secretConfig.Route.Routes))
			}
			if len(secretConfig.Route.Routes[0]) != len(amc.Spec.Route.Routes)+2 { // 2 default routes added
				t.Fatalf("subroutes count is wrong. Expected: %v, actual: %v", len(amc.Spec.Route.Routes), len(secretConfig.Route.Routes))
			}

			// Update secret with alert manager config
			if err = createDefaultAMConfig(tt.args.ctx, tt.args.cr, fclient); (err != nil) != tt.wantErr {
				t.Fatalf("createDefaultAMConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			err = fclient.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.cr.Namespace, Name: secretName}, &createdSecret)
			if err != nil {
				if errors.IsNotFound(err) && tt.secretMustBeMissing {
					return
				}
				t.Fatalf("secret for alertmanager not exist, err: %v", err)
			}

			// check secret config after updating
			d, ok = createdSecret.Data[alertmanagerSecretConfigKey]
			if !ok {
				t.Fatalf("config for alertmanager not exist, err: %v", err)
			}
			err = yaml.Unmarshal(d, &secretConfig)
			if err != nil {
				t.Fatalf("could not unmarshall secret config data into structure, err: %v", err)
			}

			if len(secretConfig.Receivers) != len(amc.Spec.Receivers) {
				t.Fatalf("receivers count is wrong. Expected: %v, actual: %v", len(amc.Spec.Receivers), len(secretConfig.Receivers))
			}

			if len(secretConfig.InhibitRules) != len(amc.Spec.InhibitRules) {
				t.Fatalf("inhibit rules count is wrong. Expected: %v, actual: %v", len(amc.Spec.InhibitRules), len(secretConfig.InhibitRules))
			}

			if !strings.EqualFold(buildCRPrefixedName(&amc, amc.Spec.Route.Receiver), secretConfig.Route.Receiver) {
				t.Fatalf("receiver name is wrong. Expected: %v, actual: %v", buildCRPrefixedName(&amc, amc.Spec.Route.Receiver), secretConfig.Route.Receiver)
			}

			if len(secretConfig.Route.Routes) != 1 {
				t.Fatalf("subroutes count is wrong. Expected: %v, actual: %v", 1, len(secretConfig.Route.Routes))
			}
			if len(secretConfig.Route.Routes[0]) != len(amc.Spec.Route.Routes)+2 { // 2 default routes added
				t.Fatalf("subroutes count is wrong. Expected: %v, actual: %v", len(amc.Spec.Route.Routes), len(secretConfig.Route.Routes))
			}
		})
	}
}

func TestBuildWebConfig(t *testing.T) {
	type args struct {
		ctx   context.Context
		vmaCR vmv1beta1.VMAlertmanager
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		want              string
		wantErr           bool
	}{
		{
			name: "simple test",
			args: args{
				ctx: context.Background(),
				vmaCR: vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test",
						Name:      "web-cfg",
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						WebConfig: &vmv1beta1.AlertmanagerWebConfig{
							HTTPServerConfig: &vmv1beta1.AlertmanagerHTTPConfig{
								Headers: map[string]string{"h-1": "v-1", "h-2": "v-2"},
							},
						},
					},
				},
			},
			want: `http_server_config:
  headers:
    h-1: v-1
    h-2: v-2
`,
		},
		{
			name: "with http2 and tls files",
			args: args{
				ctx: context.Background(),
				vmaCR: vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test",
						Name:      "web-cfg",
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						WebConfig: &vmv1beta1.AlertmanagerWebConfig{
							TLSServerConfig: &vmv1beta1.WebserverTLSConfig{
								ClientCAFile: "/etc/client_ca",
								CertFile:     "/etc/cert.pem",
								KeyFile:      "/etc/cert.key",
							},
							HTTPServerConfig: &vmv1beta1.AlertmanagerHTTPConfig{
								HTTP2:   true,
								Headers: map[string]string{"h-1": "v-1", "h-2": "v-2"},
							},
						},
					},
				},
			},
			want: `http_server_config:
  http2: true
  headers:
    h-1: v-1
    h-2: v-2
tls_server_config:
  client_ca_file: /etc/client_ca
  cert_file: /etc/cert.pem
  key_file: /etc/cert.key
`,
		},
		{
			name: "http2 and tls secrets",
			args: args{
				ctx: context.Background(),
				vmaCR: vmv1beta1.VMAlertmanager{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test",
						Name:      "web-cfg",
					},
					Spec: vmv1beta1.VMAlertmanagerSpec{
						WebConfig: &vmv1beta1.AlertmanagerWebConfig{
							TLSServerConfig: &vmv1beta1.WebserverTLSConfig{
								ClientCASecretRef: &v1.SecretKeySelector{
									Key:                  "client_ca",
									LocalObjectReference: v1.LocalObjectReference{Name: "tls-secret"},
								},
								CertSecretRef: &v1.SecretKeySelector{
									Key:                  "cert",
									LocalObjectReference: v1.LocalObjectReference{Name: "tls-secret"},
								},
								KeySecretRef: &v1.SecretKeySelector{
									Key:                  "key",
									LocalObjectReference: v1.LocalObjectReference{Name: "tls-secret-key"},
								},
							},
							HTTPServerConfig: &vmv1beta1.AlertmanagerHTTPConfig{
								HTTP2:   true,
								Headers: map[string]string{"h-1": "v-1", "h-2": "v-2"},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test",
						Name:      "tls-secret",
					},
					Data: map[string][]byte{
						"client_ca": []byte(`content`),
						"cert":      []byte(`content`),
					},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test",
						Name:      "tls-secret-key",
					},
					Data: map[string][]byte{
						"key": []byte(`content`),
					},
				},
			},
			want: `http_server_config:
  http2: true
  headers:
    h-1: v-1
    h-2: v-2
tls_server_config:
  client_ca_file: /etc/alertmanager/tls_assets/tls-secret_client_ca
  cert_file: /etc/alertmanager/tls_assets/tls-secret_cert
  key_file: /etc/alertmanager/tls_assets/tls-secret-key_key
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			tlsAssets := make(map[string]string)
			cfg, err := buildWebServerConfigYAML(tt.args.ctx, fclient, &tt.args.vmaCR, tlsAssets)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error: %q", err)
			}
			assert.Equal(t, tt.want, string(cfg))
		})
	}
}
