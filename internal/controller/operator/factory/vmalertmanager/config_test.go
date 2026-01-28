package vmalertmanager

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func mustRouteToJSON(t *testing.T, r vmv1beta1.SubRoute) apiextensionsv1.JSON {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("unexpected json marshal error: %s", err)
	}
	return apiextensionsv1.JSON{Raw: data}
}

//nolint:gofmt
func TestBuildConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlertmanager
		baseCfg           []byte
		predefinedObjects []runtime.Object
		want              string
		parseError        string
		wantErr           bool
	}
	f := func(o opts) {
		t.Helper()
		testClient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		if o.cr == nil {
			o.cr = &vmv1beta1.VMAlertmanager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertmanagerSpec{
					ConfigNamespaceSelector: &metav1.LabelSelector{},
				},
			}
		}
		ctx := context.TODO()
		ac := getAssetsCache(ctx, testClient, o.cr)
		got, data, err := buildAlertmanagerConfigWithCRDs(ctx, testClient, o.cr, o.baseCfg, ac)
		if (err != nil) != o.wantErr {
			t.Errorf("BuildConfig() error = %v, wantErr %v", err, o.wantErr)
			return
		}
		broken := got.configs.Broken()
		if len(broken) > 0 {
			assert.Equal(t, o.parseError, broken[0].Status.CurrentSyncError)
		}
		assert.Equal(t, o.want, string(data))
	}

	// override the top namespace matcher
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			Spec: vmv1beta1.VMAlertmanagerSpec{
				EnforcedNamespaceLabel:  "alert-namespace",
				ConfigNamespaceSelector: &metav1.LabelSelector{},
			},
		},
		baseCfg: []byte(`global:
 time_out: 1min
 smtp_smarthost: some:443
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
									To:           "some-dst-1",
									Text:         "some-text",
									Smarthost:    "some:443",
									TLSConfig: &vmv1beta1.TLSConfig{
										CertFile: "some_cert_path",
									},
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
		want: `global:
  smtp_smarthost: some:443
  time_out: 1min
route:
  receiver: blackhole
  routes:
  - matchers:
    - alert-namespace = "default"
    group_wait: 1min
    receiver: default-base-email
    continue: true
receivers:
- name: blackhole
- name: default-base-email
  email_configs:
  - tls_config:
      cert_file: some_cert_path
    from: some-sender
    text: some-text
    to: some-dst-1
    smarthost: some:443
    send_resolved: true
templates: []
`,
	})

	// with complex routing and enforced matchers
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				DisableNamespaceMatcher: true,
				EnforcedTopRouteMatchers: []string{
					`env=~{"dev|prod"}`,
					`pod!=""`,
				},
				ConfigNamespaceSelector: &metav1.LabelSelector{},
			},
		},
		baseCfg: []byte(`global:
 time_out: 1min
 smtp_smarthost: some:443
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
									To:           "some-dst-1",
									Text:         "some-text",
									Smarthost:    "some:443",
									TLSConfig: &vmv1beta1.TLSConfig{
										CertFile: "some_cert_path",
									},
								},
							},
						},
						{
							Name: "email-sub-1",
							EmailConfigs: []vmv1beta1.EmailConfig{
								{
									SendResolved: ptr.To(true),
									From:         "some-sender",
									To:           "some-dst-1",
									Text:         "some-text",
									Smarthost:    "some:443",
									TLSConfig: &vmv1beta1.TLSConfig{
										CertFile: "some_cert_path",
									},
								},
							},
						},
					},
					Route: &vmv1beta1.Route{
						Receiver:  "email",
						GroupWait: "1min",
						RawRoutes: []apiextensionsv1.JSON{
							mustRouteToJSON(t, vmv1beta1.SubRoute{
								Receiver:  "email-sub-1",
								GroupWait: "5min",
								Matchers:  []string{"team=prod"},
								RawRoutes: []apiextensionsv1.JSON{
									mustRouteToJSON(t, vmv1beta1.SubRoute{
										Receiver:  "email",
										GroupWait: "10min",
										Matchers:  []string{"pod=dev-env"},
									}),
								},
							}),
						},
					},
				},
			},
		},
		want: `global:
  smtp_smarthost: some:443
  time_out: 1min
route:
  receiver: blackhole
  routes:
  - routes:
    - routes:
      - matchers:
        - pod=dev-env
        group_wait: 10min
        receiver: default-base-email
        continue: false
      matchers:
      - team=prod
      group_wait: 5min
      receiver: default-base-email-sub-1
      continue: false
    matchers:
    - env=~{"dev|prod"}
    - pod!=""
    group_wait: 1min
    receiver: default-base-email
    continue: true
receivers:
- name: blackhole
- name: default-base-email
  email_configs:
  - tls_config:
      cert_file: some_cert_path
    from: some-sender
    text: some-text
    to: some-dst-1
    smarthost: some:443
    send_resolved: true
- name: default-base-email-sub-1
  email_configs:
  - tls_config:
      cert_file: some_cert_path
    from: some-sender
    text: some-text
    to: some-dst-1
    smarthost: some:443
    send_resolved: true
templates: []
`,
	})

	// email section
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
 smtp_smarthost: some:443
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
									To:           "some-dst-1",
									Text:         "some-text",
									Smarthost:    "some:443",
									TLSConfig: &vmv1beta1.TLSConfig{
										CertFile: "some_cert_path",
									},
								},
								{
									SendResolved: ptr.To(true),
									From:         "some-sender",
									To:           "some-dst-2",
									Text:         "some-text",
									Smarthost:    "some:443",
									RequireTLS:   ptr.To(false),
									TLSConfig: &vmv1beta1.TLSConfig{
										CertFile: "some_cert_path",
									},
								},
								{
									SendResolved: ptr.To(true),
									From:         "some-sender",
									To:           "some-dst-3",
									Text:         "some-text",
									Smarthost:    "some:443",
									RequireTLS:   ptr.To(true),
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
		want: `global:
  smtp_smarthost: some:443
  time_out: 1min
route:
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-email
    continue: true
receivers:
- name: blackhole
- name: default-base-email
  email_configs:
  - tls_config:
      cert_file: some_cert_path
    from: some-sender
    text: some-text
    to: some-dst-1
    smarthost: some:443
    send_resolved: true
  - require_tls: false
    tls_config:
      cert_file: some_cert_path
    from: some-sender
    text: some-text
    to: some-dst-2
    smarthost: some:443
    send_resolved: true
  - require_tls: true
    tls_config:
      cert_file: some_cert_path
    from: some-sender
    text: some-text
    to: some-dst-3
    smarthost: some:443
    send_resolved: true
  - require_tls: false
    from: other-sender
    text: other-text
    to: other-dst
    send_resolved: true
templates: []
`,
	})

	// complex with providers
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
 opsgenie_api_key: some-key
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
									TLSConfig:    &vmv1beta1.TLSConfig{},
									From:         "some-sender",
									To:           "some-dst",
									Text:         "some-text",
									Smarthost:    "some:443",
									RequireTLS:   ptr.To(true),
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
						RawRoutes: []apiextensionsv1.JSON{
							mustRouteToJSON(t, vmv1beta1.SubRoute{
								Receiver: "webhook",
							}),
						},
					},
				},
			},
			&vmv1beta1.VMAlertmanagerConfig{
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
											Type:     "team",
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
		want: `global:
  opsgenie_api_key: some-key
  time_out: 1min
route:
  receiver: blackhole
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
- name: blackhole
- name: default-base-email
  email_configs:
  - require_tls: true
    from: some-sender
    text: some-text
    to: some-dst
    smarthost: some:443
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
      type: team
templates: []
`,
	})

	// webhook ok
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-webhook
    continue: true
receivers:
- name: blackhole
- name: default-base-webhook
  webhook_configs:
  - send_resolved: true
    url: https://webhook.example.com
templates: []
`,
	})

	// slack ok
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
											Type: "test",
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
											Value: "test",
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
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-slack
    continue: true
receivers:
- name: blackhole
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
      type: test
      confirm:
        text: confirmed
    fields:
    - value: test
      title: fields
      short: true
templates: []
`,
	})

	// pagerduty ok
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "some-secret", Namespace: "default"},
				Data:       map[string][]byte{"some-key": []byte(`some-value`)},
			},
		},
		want: `global:
  time_out: 1min
route:
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-pagerduty
    continue: true
receivers:
- name: blackhole
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
	})

	// telegram ok
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
`),
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
			&vmv1beta1.VMAlertmanagerConfig{
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
		want: `global:
  time_out: 1min
route:
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-tg-telegram
    continue: true
receivers:
- name: blackhole
- name: default-tg-telegram
  telegram_configs:
  - bot_token: some-token
    send_resolved: true
    chat_id: 125
    message: some-templated message
templates: []
`,
	})

	// slack bad, with invalid api_url
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
		parseError: "invalid URL bad_url in key bad_url from secret slack: unsupported scheme \"\" for URL",
		want: `global:
  time_out: 1min
route:
  receiver: blackhole
receivers:
- name: blackhole
templates: []
`,
	})

	// telegram bad, not strict parse
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
`),
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
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
		parseError: `unable to fetch secret="tg-secret", ns="default": secrets "tg-secret" not found`,
		want: `global:
  time_out: 1min
route:
  receiver: blackhole
receivers:
- name: blackhole
templates: []
`,
	})

	// jira section
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
 smtp_smarthost: some:443
 jira_api_url: "https://jira.cloud"
`),
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "jira-api-access",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"DC_KEY":     []byte(`somekey`),
					"CLOUD_USER": []byte(`username`),
					"CLOUD_PAT":  []byte(`personal-token`),
				},
			},
			&vmv1beta1.VMAlertmanagerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "base",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertmanagerConfigSpec{
					Receivers: []vmv1beta1.Receiver{
						{
							Name: "jira-dc",
							JiraConfigs: []vmv1beta1.JiraConfig{
								{
									SendResolved: ptr.To(true),
									HTTPConfig: &vmv1beta1.HTTPConfig{
										Authorization: &vmv1beta1.Authorization{
											Credentials: &corev1.SecretKeySelector{
												Key: "DC_KEY",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "jira-api-access",
												},
											},
										},
									},
									Project:   "main",
									IssueType: "BUG",
									Summary:   "must be fixed",
									Labels: []string{
										"dev",
									},
									Fields: map[string]apiextensionsv1.JSON{
										"components":        {Raw: []byte(`{"name": "Monitoring"}`)},
										"customfield_10001": {Raw: []byte(`"Random text"`)},
										"customfield_10002": {Raw: []byte(`{"value": "red"}`)},
									},
								},
							},
						},
						{
							Name: "jira-cloud",
							JiraConfigs: []vmv1beta1.JiraConfig{
								{
									SendResolved: ptr.To(true),
									HTTPConfig: &vmv1beta1.HTTPConfig{
										BasicAuth: &vmv1beta1.BasicAuth{
											Username: corev1.SecretKeySelector{
												Key: "CLOUD_USER",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "jira-api-access",
												},
											},
											Password: corev1.SecretKeySelector{
												Key: "CLOUD_PAT",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "jira-api-access",
												},
											},
										},
									},
									Project:   "main",
									IssueType: "BUG",
									Summary:   "must be fixed",
									Labels: []string{
										"dev",
									},
									Fields: map[string]apiextensionsv1.JSON{
										"components":        {Raw: []byte(`{"name": "Monitoring"}`)},
										"customfield_10001": {Raw: []byte(`"Random text"`)},
										"customfield_10002": {Raw: []byte(`{"value": "red"}`)},
									},
								},
							},
						},
					},
					Route: &vmv1beta1.Route{
						Receiver:  "jira-dc",
						GroupWait: "1min",
					},
				},
			},
		},
		want: `global:
  jira_api_url: https://jira.cloud
  smtp_smarthost: some:443
  time_out: 1min
route:
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    group_wait: 1min
    receiver: default-base-jira-dc
    continue: true
receivers:
- name: blackhole
- name: default-base-jira-dc
  jira_configs:
  - http_config:
      authorization:
        credentials: somekey
        type: Bearer
    send_resolved: true
    project: main
    issue_type: BUG
    summary: must be fixed
    labels:
    - dev
    fields:
      components: '{"name":"Monitoring"}'
      customfield_10001: '"Random text"'
      customfield_10002: '{"value":"red"}'
- name: default-base-jira-cloud
  jira_configs:
  - http_config:
      basic_auth:
        username: username
        password: personal-token
    send_resolved: true
    project: main
    issue_type: BUG
    summary: must be fixed
    labels:
    - dev
    fields:
      components: '{"name":"Monitoring"}'
      customfield_10001: '"Random text"'
      customfield_10002: '{"value":"red"}'
templates: []
`,
	})

	// rocketchat section
	f(opts{
		baseCfg: []byte(`global:
 time_out: 1min
 smtp_smarthost: some:443
`),
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rocket-access",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ID":           []byte(`12356`),
					"SECRET_TOKEN": []byte(`token value`),
				},
			},
			&vmv1beta1.VMAlertmanagerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "base",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertmanagerConfigSpec{
					Route: &vmv1beta1.Route{
						Receiver: "rocketchat",
					},
					Receivers: []vmv1beta1.Receiver{
						{
							Name: "rocketchat",
							RocketchatConfigs: []vmv1beta1.RocketchatConfig{
								{
									TokenID: &corev1.SecretKeySelector{
										Key: "ID",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "rocket-access",
										},
									},
									Token: &corev1.SecretKeySelector{
										Key: "SECRET_TOKEN",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "rocket-access",
										},
									},
									Channel: "some-channel",
									Fields: []vmv1beta1.RocketchatAttachmentField{
										{
											Short: ptr.To(true),
											Title: "alert value",
											Value: "1",
										},
									},
									Actions: []vmv1beta1.RocketchatAttachmentAction{
										{
											Type: "action",
											Text: "some text",
											URL:  "https://example.com/action",
											Msg:  "some message",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		want: `global:
  smtp_smarthost: some:443
  time_out: 1min
route:
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    receiver: default-base-rocketchat
    continue: true
receivers:
- name: blackhole
- name: default-base-rocketchat
  rocketchat_configs:
  - token_id: "12356"
    token: token value
    channel: some-channel
    fields:
    - title: alert value
      value: "1"
      short: true
    actions:
    - type: action
      text,omitempty: some text
      url: https://example.com/action
      msg: some message
templates: []
`,
	})

	// incidentio
	f(opts{
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "incidentio-dev",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertmanagerConfigSpec{
					Route: &vmv1beta1.Route{
						Receiver: "incidentio",
					},
					Receivers: []vmv1beta1.Receiver{
						{
							Name: "incidentio",
							IncidentIOConfigs: []vmv1beta1.IncidentIOConfig{
								{
									URL: "http://example.com/",
									AlertSourceToken: &corev1.SecretKeySelector{
										Key: "TOKEN",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "incidentio-access",
										},
									},
								},
							},
						},
					},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "incidentio-access",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"TOKEN": []byte(`token value`),
				},
			},
		},
		want: `route:
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    receiver: default-incidentio-dev-incidentio
    continue: true
receivers:
- name: blackhole
- name: default-incidentio-dev-incidentio
  incidentio_configs:
  - alert_source_token: token value
    url: http://example.com/
templates: []
`,
	})

	// msteamsv2
	f(opts{
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMAlertmanagerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "msteams-dev",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertmanagerConfigSpec{
					Route: &vmv1beta1.Route{
						Receiver: "mstv2",
					},
					Receivers: []vmv1beta1.Receiver{
						{
							Name: "mstv2",
							MSTeamsV2Configs: []vmv1beta1.MSTeamsV2Config{
								{
									URL:   ptr.To("http://example.com/msteams"),
									Title: "some",
									Text:  "some alert text",
									HTTPConfig: &vmv1beta1.HTTPConfig{
										Authorization: &vmv1beta1.Authorization{
											Credentials: &corev1.SecretKeySelector{
												Key: "TOKEN",
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "ms-teams-access",
												},
											},
										},
									},
								},
								{
									URLSecret: &corev1.SecretKeySelector{
										Key: "API_URL",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "ms-teams-access",
										},
									},
									Title: "team 2",
									Text:  "some other text",
								},
							},
						},
					},
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ms-teams-access",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"TOKEN":   []byte(`token value`),
					"API_URL": []byte(`https://example.com/v2/msteamsv2`),
				},
			},
		},
		want: `route:
  receiver: blackhole
  routes:
  - matchers:
    - namespace = "default"
    receiver: default-msteams-dev-mstv2
    continue: true
receivers:
- name: blackhole
- name: default-msteams-dev-mstv2
  msteamsv2_configs:
  - http_config:
      authorization:
        credentials: token value
        type: Bearer
    webhook_url: http://example.com/msteams
    text: some alert text
    title: some
  - webhook_url: https://example.com/v2/msteamsv2
    text: some other text
    title: team 2
templates: []
`,
	})
}

func TestAddConfigTemplates(t *testing.T) {
	type opts struct {
		config    []byte
		templates []string
		want      string
		wantErr   bool
	}

	f := func(o opts) {
		t.Helper()
		got, err := addConfigTemplates(o.config, o.templates)
		if (err != nil) != o.wantErr {
			t.Errorf("AddConfigTemplates() error = %v, wantErr %v", err, o.wantErr)
			return
		}
		assert.Equal(t, o.want, string(got))
	}

	// add templates to empty config
	f(opts{
		templates: []string{"/etc/vm/templates/test/template1.tmpl"},
		want: `templates:
- /etc/vm/templates/test/template1.tmpl
`,
	})

	// add templates to config without templates
	f(opts{
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
	})

	// add templates to config with templates
	f(opts{
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
	})

	// add empty and duplicated templates
	f(opts{
		templates: []string{
			"",
			"/etc/vm/templates/test/template1.tmpl",
			" ",
			"/etc/vm/templates/test/template1.tmpl",
			"\t",
		},
		want: `templates:
- /etc/vm/templates/test/template1.tmpl
`,
	})

	// add empty templates list
	f(opts{
		config:    []byte(`test`),
		templates: []string{},
		want:      `test`,
	})

	// wrong config
	f(opts{
		config:    []byte(`test`),
		templates: []string{"test"},
		wantErr:   true,
	})

	// add template duplicates without path
	f(opts{
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
	})
}

func Test_configBuilder_buildHTTPConfig(t *testing.T) {
	type opts struct {
		httpCfg           *vmv1beta1.HTTPConfig
		predefinedObjects []runtime.Object
		want              string
		wantErr           bool
	}
	f := func(o opts) {
		t.Helper()
		testClient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		cr := &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-am",
				Namespace: "default",
			},
		}
		cb := &configBuilder{
			cache:     getAssetsCache(context.Background(), testClient, cr),
			namespace: cr.Namespace,
		}
		gotYAML, err := cb.buildHTTPConfig(o.httpCfg)
		if (err != nil) != o.wantErr {
			t.Errorf("buildHTTPConfig() error = %v, wantErr %v", err, o.wantErr)
			return
		}
		got, err := yaml.Marshal(gotYAML)
		if (err != nil) != o.wantErr {
			t.Errorf("buildHTTPConfig() error = %v, wantErr %v", err, o.wantErr)
			return
		}
		assert.Equalf(t, o.want, string(got), "buildHTTPConfig(%v)", o.httpCfg)
	}

	// build empty config
	f(opts{
		want: "{}\n",
	})

	// with basic auth
	f(opts{
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
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-store",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("user-1"),
				},
			},
		},
		want: `basic_auth:
  username: user-1
  password_file: /etc/vm/secrets/password_file
`,
	})

	// with tls and bearer
	f(opts{
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
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-store",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"cert": []byte("---PEM---"),
					"ca":   []byte("---PEM-CA"),
				},
			},
		},
		want: `tls_config:
  insecure_skip_verify: true
  ca_file: /etc/alertmanager/tls_assets/default_secret-store_ca
  cert_file: /etc/alertmanager/tls_assets/default_secret-store_cert
  key_file: /etc/mounted_dir/key.pem
authorization:
  credentials_file: /etc/mounted_dir/bearer_file
`,
	})

	// with tls (configmap) and bearer
	f(opts{
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
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-store",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"cert": []byte("---PEM---"),
					"key":  []byte("--KEY-PEM--"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-bearer",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"token": []byte("secret-token"),
				},
			},
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cm-store",
					Namespace: "default",
				},
				Data: map[string]string{
					"ca": "--CA-PEM--",
				},
			},
		},
		want: `tls_config:
  insecure_skip_verify: true
  ca_file: /etc/alertmanager/tls_assets/default_configmap_cm-store_ca
  cert_file: /etc/alertmanager/tls_assets/default_secret-store_cert
  key_file: /etc/alertmanager/tls_assets/default_secret-store_key
authorization:
  credentials: secret-token
`,
	})

	// with oauth2 (configmap)
	f(opts{
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "oauth-store",
				},
				Data: map[string]string{
					"client_id": "client-value",
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-store",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"client-secret": []byte("value"),
				},
			},
		},
		httpCfg: &vmv1beta1.HTTPConfig{
			OAuth2: &vmv1beta1.OAuth2{
				TokenURL: "https://some-oauth2-proxy",
				EndpointParams: map[string]string{
					"param-1": "value1",
					"param-2": "value2",
				},
				Scopes: []string{"org", "team"},
				ClientID: vmv1beta1.SecretOrConfigMap{
					ConfigMap: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "oauth-store",
						},
						Key: "client_id",
					},
				},
				ClientSecret: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "secret-store",
					},
					Key: "client-secret",
				},
			},
		},
		want: `oauth2:
  client_id: client-value
  client_secret: value
  scopes:
  - org
  - team
  endpoint_params:
    param-1: value1
    param-2: value2
  token_url: https://some-oauth2-proxy
`,
	})

	// with oauth2 (secret)
	f(opts{
		httpCfg: &vmv1beta1.HTTPConfig{
			OAuth2: &vmv1beta1.OAuth2{
				TokenURL: "https://some-oauth2-proxy",
				EndpointParams: map[string]string{
					"param-1": "value1",
					"param-2": "value2",
				},
				Scopes: []string{"org", "team"},
				ClientID: vmv1beta1.SecretOrConfigMap{
					Secret: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "secret-store",
						},
						Key: "client-id",
					},
				},
				ClientSecret: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "secret-store",
					},
					Key: "client-secret",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-store",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"client-secret": []byte("value"),
					"client-id":     []byte("client-value"),
				},
			},
		},
		want: `oauth2:
  client_id: client-value
  client_secret: value
  scopes:
  - org
  - team
  endpoint_params:
    param-1: value1
    param-2: value2
  token_url: https://some-oauth2-proxy
`,
	})
}

func Test_UpdateDefaultAMConfig(t *testing.T) {
	type opts struct {
		cr                  *vmv1beta1.VMAlertmanager
		wantErr             bool
		predefinedObjects   []runtime.Object
		secretMustBeMissing bool
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()

		// Create secret with alert manager config
		if err := CreateOrUpdateConfig(ctx, fclient, o.cr, nil); (err != nil) != o.wantErr {
			t.Fatalf("createDefaultAMConfig() error = %v, wantErr %v", err, o.wantErr)
		}
		var amCfgs []*vmv1beta1.VMAlertmanagerConfig
		opts := &k8stools.SelectorOpts{
			SelectAll:         o.cr.Spec.SelectAllByDefault,
			ObjectSelector:    o.cr.Spec.ConfigSelector,
			NamespaceSelector: o.cr.Spec.ConfigNamespaceSelector,
			DefaultNamespace:  o.cr.Namespace,
		}
		assert.NoError(t, k8stools.VisitSelected(ctx, fclient, opts, func(ams *vmv1beta1.VMAlertmanagerConfigList) {
			for i := range ams.Items {
				item := ams.Items[i]
				amCfgs = append(amCfgs, &item)
			}
		}))
		for _, amc := range amCfgs {
			if amc.Status.Reason != "" {
				t.Errorf("unexpected sync error: %s", amc.Status.Reason)
			}
		}

		var createdSecret corev1.Secret
		secretName := o.cr.ConfigSecretName()
		if err := fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: secretName}, &createdSecret); err != nil {
			if k8serrors.IsNotFound(err) && o.secretMustBeMissing {
				return
			}
			t.Fatalf("config for alertmanager not exist, err: %v", err)
		}

		// check secret config after creating
		d, ok := createdSecret.Data[alertmanagerSecretConfigKeyGz]
		if !ok {
			t.Fatalf("config for alertmanager not exist")
		}
		var secretConfig alertmanagerConfig
		if data, err := build.GunzipConfig(d); err != nil {
			t.Fatalf("failed to gunzip config: %s", err)
		} else {
			assert.NoError(t, yaml.Unmarshal(data, &secretConfig))
		}
		var amc vmv1beta1.VMAlertmanagerConfig
		assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: "test-amc"}, &amc))
		// we add blachole as first route by default
		assert.Len(t, secretConfig.Receivers, len(amc.Spec.Receivers)+1)
		assert.Len(t, secretConfig.InhibitRules, len(amc.Spec.InhibitRules))
		assert.Len(t, secretConfig.Route.Routes, 1)
		assert.Len(t, secretConfig.Route.Routes[0], len(amc.Spec.Route.Routes)+2)
		// Update secret with alert manager config
		if err := CreateOrUpdateConfig(ctx, fclient, o.cr, nil); (err != nil) != o.wantErr {
			t.Fatalf("createDefaultAMConfig() error = %v, wantErr %v", err, o.wantErr)
		}

		if err := fclient.Get(ctx, types.NamespacedName{Namespace: o.cr.Namespace, Name: secretName}, &createdSecret); err != nil {
			if k8serrors.IsNotFound(err) && o.secretMustBeMissing {
				return
			}
			t.Fatalf("secret for alertmanager not exist, err: %v", err)
		}

		// check secret config after updating
		d, ok = createdSecret.Data[alertmanagerSecretConfigKeyGz]
		if !ok {
			t.Fatalf("config for alertmanager not exist")
		}
		if data, err := build.GunzipConfig(d); err != nil {
			t.Fatalf("failed to gunzip alertmanager config: %s", err)
		} else {
			assert.NoError(t, yaml.Unmarshal(data, &secretConfig))
		}
		assert.Len(t, secretConfig.Receivers, len(amc.Spec.Receivers)+1)
		assert.Len(t, secretConfig.InhibitRules, len(amc.Spec.InhibitRules))
		assert.Len(t, secretConfig.Route.Routes, 1)
		assert.Len(t, secretConfig.Route.Routes[0], len(amc.Spec.Route.Routes)+2)
	}

	// with alertmanager config support
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-am",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				ConfigSecret:       "vmalertmanager-test-am-config",
				ConfigRawYaml:      "global: {}",
				SelectAllByDefault: true,
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmalertmanager-test-am-config",
					Namespace: "default",
				},
				Data: map[string][]byte{alertmanagerSecretConfigKeyGz: {}},
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
						RawRoutes: []apiextensionsv1.JSON{
							mustRouteToJSON(t, vmv1beta1.SubRoute{Receiver: "blackhole", Matchers: []string{"alertname=\"QuietWeeklyNotifications\""}}),
							mustRouteToJSON(t, vmv1beta1.SubRoute{Receiver: "blackhole", Matchers: []string{"alertname=\"QuietDailyNotifications\""}}),
							mustRouteToJSON(t, vmv1beta1.SubRoute{Receiver: "l2ci_receiver", Matchers: []string{"alert_group=~\"^l2ci.*\""}}),
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
	})
	assert.Nil(t, os.Setenv("WATCH_NAMESPACE", "default"))
}

func TestBuildWebConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlertmanager
		predefinedObjects []runtime.Object
		want              string
		wantErr           bool
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		ac := getAssetsCache(ctx, fclient, o.cr)
		c, err := buildWebServerConfigYAML(o.cr, ac)
		if (err != nil) != o.wantErr {
			t.Fatalf("unexpected error: %q", err)
		}
		assert.Equal(t, o.want, string(c))
	}

	// simple test
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
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
		want: `http_server_config:
  headers:
    h-1: v-1
    h-2: v-2
`,
	})

	// with http2 and tls files
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "web-cfg",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				GossipConfig: &vmv1beta1.AlertmanagerGossipConfig{
					TLSClientConfig: &vmv1beta1.TLSClientConfig{
						CAFile: "/etc/client/client_ca",
						Certs: vmv1beta1.Certs{
							CertFile: "/etc/client/cert.pem",
							KeyFile:  "/etc/client/cert.key",
						},
					},
					TLSServerConfig: &vmv1beta1.TLSServerConfig{
						ClientCAFile: "/etc/server/client_ca",
						Certs: vmv1beta1.Certs{
							CertFile: "/etc/server/cert.pem",
							KeyFile:  "/etc/server/cert.key",
						},
					},
				},
				WebConfig: &vmv1beta1.AlertmanagerWebConfig{
					TLSServerConfig: &vmv1beta1.TLSServerConfig{
						ClientCAFile: "/etc/server/client_ca",
						Certs: vmv1beta1.Certs{
							CertFile: "/etc/server/cert.pem",
							KeyFile:  "/etc/server/cert.key",
						},
					},
					HTTPServerConfig: &vmv1beta1.AlertmanagerHTTPConfig{
						HTTP2:   true,
						Headers: map[string]string{"h-1": "v-1", "h-2": "v-2"},
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
  client_ca_file: /etc/server/client_ca
  cert_file: /etc/server/cert.pem
  key_file: /etc/server/cert.key
`,
	})

	// http2 and tls secrets
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "web-cfg",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				WebConfig: &vmv1beta1.AlertmanagerWebConfig{
					TLSServerConfig: &vmv1beta1.TLSServerConfig{
						ClientCASecretRef: &corev1.SecretKeySelector{
							Key:                  "client_ca",
							LocalObjectReference: corev1.LocalObjectReference{Name: "tls-secret"},
						},
						Certs: vmv1beta1.Certs{
							CertSecretRef: &corev1.SecretKeySelector{
								Key:                  "cert",
								LocalObjectReference: corev1.LocalObjectReference{Name: "tls-secret"},
							},
							KeySecretRef: &corev1.SecretKeySelector{
								Key:                  "key",
								LocalObjectReference: corev1.LocalObjectReference{Name: "tls-secret-key"},
							},
						},
					},
					HTTPServerConfig: &vmv1beta1.AlertmanagerHTTPConfig{
						HTTP2:   true,
						Headers: map[string]string{"h-1": "v-1", "h-2": "v-2"},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test",
					Name:      "tls-secret",
				},
				Data: map[string][]byte{
					"client_ca": []byte(`content`),
					"cert":      []byte(`content`),
				},
			},
			&corev1.Secret{
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
  client_ca_file: /etc/alertmanager/tls_assets/test_tls-secret_client_ca
  cert_file: /etc/alertmanager/tls_assets/test_tls-secret_cert
  key_file: /etc/alertmanager/tls_assets/test_tls-secret-key_key
`,
	})
}

func TestBuildGossipConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlertmanager
		predefinedObjects []runtime.Object
		want              string
		wantErr           bool
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		ac := getAssetsCache(ctx, fclient, o.cr)
		c, err := buildGossipConfigYAML(o.cr, ac)
		if (err != nil) != o.wantErr {
			t.Fatalf("unexpected error: %q", err)
		}
		assert.Equal(t, o.want, string(c))
	}

	// tls secrets
	f(opts{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "web-cfg",
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				GossipConfig: &vmv1beta1.AlertmanagerGossipConfig{
					TLSClientConfig: &vmv1beta1.TLSClientConfig{
						CAFile: "/etc/client/client_ca",
						Certs: vmv1beta1.Certs{
							CertFile: "/etc/client/cert.pem",
							KeyFile:  "/etc/client/cert.key",
						},
					},
					TLSServerConfig: &vmv1beta1.TLSServerConfig{
						ClientCAFile: "/etc/server/client_ca",
						Certs: vmv1beta1.Certs{
							CertFile: "/etc/server/cert.pem",
							KeyFile:  "/etc/server/cert.key",
						},
					},
				},
			},
		},
		want: `tls_server_config:
  client_ca_file: /etc/server/client_ca
  cert_file: /etc/server/cert.pem
  key_file: /etc/server/cert.key
tls_client_config:
  ca_file: /etc/client/client_ca
  cert_file: /etc/client/cert.pem
  key_file: /etc/client/cert.key
`,
	})
}
