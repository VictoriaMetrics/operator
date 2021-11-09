package alertmanager

import (
	"context"
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
		ctx     context.Context
		baseCfg []byte
		amcfgs  map[string]*operatorv1beta1.VMAlertmanagerConfig
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := BuildConfig(tt.args.ctx, testClient, tt.args.baseCfg, tt.args.amcfgs)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, string(got))

		})
	}
}
