package alertmanager

import (
	"context"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"testing"

	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
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
					"default/base": &operatorv1beta1.VMAlertmanagerConfig{
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
											SendResolved: pointer.Bool(true),
											From:         "some-sender",
											To:           "some-dst",
											Text:         "some-text",
											TLSConfig: &operatorv1beta1.TLSConfig{
												CertFile: "some_cert_path",
											},
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
					"default/base": &operatorv1beta1.VMAlertmanagerConfig{
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
											SendResolved: pointer.Bool(true),
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
											URL: pointer.String("http://some-wh"),
										},
									},
								},
							},
							Route: &operatorv1beta1.Route{
								Receiver:  "email",
								GroupWait: "1min",
								Routes: []*operatorv1beta1.Route{
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
											SendResolved: pointer.BoolPtr(true),
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
			name: "slack ok",
			args: args{
				ctx: context.Background(),
				baseCfg: []byte(`global:
 time_out: 1min
`),
				amcfgs: map[string]*operatorv1beta1.VMAlertmanagerConfig{
					"default/base": &operatorv1beta1.VMAlertmanagerConfig{
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
											SendResolved: pointer.Bool(true),
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
													Short: pointer.Bool(true),
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
  - send_resolved: true
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
					"default/base": &operatorv1beta1.VMAlertmanagerConfig{
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
											SendResolved: pointer.Bool(true),
											RoutingKey: &v1.SecretKeySelector{
												Key: "some-key",
												LocalObjectReference: v1.LocalObjectReference{
													Name: "some-secret",
												},
											},
											Class:    "some-class",
											Group:    "some-group",
											Severity: "warning",
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
					"default/base": &operatorv1beta1.VMAlertmanagerConfig{
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
											SendResolved: pointer.Bool(true),
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := BuildConfig(tt.args.ctx, testClient, !tt.args.disableNamespaceMatcher, tt.args.baseCfg, tt.args.amcfgs, map[string]string{})
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildConfig() error = %v, wantErr %v", err, tt.wantErr)
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
				"secret-store": &v1.Secret{
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
				"secret-store": &v1.Secret{
					Data: map[string][]byte{
						"cert": []byte("---PEM---"),
						"ca":   []byte("---PEM-CA"),
					},
				},
			}},
			want: `tls_config:
  ca_file: /etc/alertmanager/config/default_secret-store_ca
  cert_file: /etc/alertmanager/config/default_secret-store_cert
  key_file: /etc/mounted_dir/key.pem
  insecure_skip_verify: true
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
					"secret-store": &v1.Secret{
						Data: map[string][]byte{
							"cert": []byte("---PEM---"),
							"key":  []byte("--KEY-PEM--"),
						},
					},
					"secret-bearer": &v1.Secret{
						Data: map[string][]byte{
							"token": []byte("secret-token"),
						},
					},
				},
				configmapCache: map[string]*v1.ConfigMap{
					"cm-store": &v1.ConfigMap{
						Data: map[string]string{
							"ca": "--CA-PEM--",
						},
					},
				},
			},
			want: `tls_config:
  ca_file: /etc/alertmanager/config/default_cm-store_ca
  cert_file: /etc/alertmanager/config/default_secret-store_cert
  key_file: /etc/alertmanager/config/default_secret-store_key
  insecure_skip_verify: true
authorization:
  credentials: secret-token
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := &configBuilder{
				ctx:            context.Background(),
				Client:         k8stools.GetTestClientWithObjects(nil),
				secretCache:    tt.fields.secretCache,
				configmapCache: tt.fields.configmapCache,
				tlsAssets:      map[string]string{},
				currentCR: &operatorv1beta1.VMAlertmanagerConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-am",
						Namespace: "default",
					},
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
