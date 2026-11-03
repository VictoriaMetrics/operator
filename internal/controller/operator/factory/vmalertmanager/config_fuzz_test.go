package vmalertmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func assertStableBuild(t *testing.T, render func() (any, error)) {
	t.Helper()
	first, err := render()
	second, nextErr := render()
	if (err == nil) != (nextErr == nil) || err != nil && err.Error() != nextErr.Error() {
		t.Fatalf("non-idempotent build errors: %v != %v", err, nextErr)
	}
	if err != nil {
		return
	}
	a, err := yaml.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := yaml.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("non-idempotent build: %s != %s", a, b)
	}
}

func fuzzAlertmanager() *vmv1beta1.VMAlertmanager {
	return &vmv1beta1.VMAlertmanager{ObjectMeta: metav1.ObjectMeta{Name: "am", Namespace: "default"}}
}

func FuzzBuildRoute(f *testing.F) {
	for _, seed := range []string{
		`{"spec":{"route":{"receiver":"main"}}}`,
		`{"spec":{"route":{"receiver":"main","matchers":["job=api"],"group_by":["job"],"routes":[{"receiver":"child","matchers":["severity=critical"]}]}}}`,
		`{"spec":{"route":{"receiver":"main","routes":[{"receiver":"child","routes":[{"receiver":"leaf"}]}],"active_time_intervals":["office"]}}}`,
	} {
		f.Add([]byte(seed), false)
		f.Add([]byte(seed), true)
	}
	f.Fuzz(func(t *testing.T, data []byte, disableEnforcement bool) {
		var cr vmv1beta1.VMAlertmanagerConfig
		if err := json.Unmarshal(data, &cr); err != nil {
			fuzztest.New(data).Fill(&cr)
		}
		if cr.Spec.Route == nil {
			return
		}
		cr.Name, cr.Namespace = "config", "default"
		am := fuzzAlertmanager()
		am.Spec.DisableNamespaceMatcher = disableEnforcement
		am.Spec.DisableRouteContinueEnforce = disableEnforcement
		assertStableBuild(t, func() (any, error) { return buildRoute(&cr, cr.Spec.Route, true, am) })
	})
}

// fuzzGlobal sets every global receiver default, so receivers without own credentials build.
var fuzzGlobal = globalConfig{
	SMTPFrom:          "a@b",
	SMTPSmarthost:     "h:25",
	SlackAPIURL:       "http://s",
	OpsGenieAPIKey:    "k",
	WechatAPISecret:   "s",
	WechatAPICorpID:   "c",
	VictorOpsAPIKey:   "k",
	JiraAPIURL:        "http://j",
	RocketchatAPIURL:  "http://r",
	RocketchatToken:   "t",
	RocketchatTokenID: "i",
}

func FuzzBuildReceiver(f *testing.F) {
	seeds := []string{
		`{"name":"empty"}`,
		`{"name":"hook","webhook_configs":[{"url":"https://example.org","http_config":{"follow_redirects":true}}]}`,
		`{"name":"slack","slack_configs":[{"api_url":"https://example.org","fields":[{"title":"site","value":"prod"}]}]}`,
		`{"name":"mattermost","mattermost_configs":[{"attachments":[null]}]}`,
		`{"name":"r","opsgenie_configs":[{"api_key":{"name":"ref","key":"key"},"apiURL":"http://o","message":"m","tags":"t","details":{"a":"b"},"responders":[{"name":"n","type":"team"}],"actions":"a","update_alerts":true,"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","email_configs":[{"to":"a@b","from":"c@d","smarthost":"h:25","auth_username":"u","auth_password":{"name":"ref","key":"key"},"auth_secret":{"name":"ref","key":"key"},"headers":{"a":"b"},"html":"h","text":"t","require_tls":true,"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}}},"send_resolved":true}]}`,
		`{"name":"r","slack_configs":[{"api_url":{"name":"ref","key":"key"},"channel":"c","fields":[{"title":"t","value":"v","short":true}],"actions":[{"type":"button","text":"t","url":"http://a","confirm":{"text":"t"}}],"mrkdwn_in":["text"],"link_names":true,"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","pagerduty_configs":[{"routing_key":{"name":"ref","key":"key"},"service_key":{"name":"ref","key":"key"},"url":"http://p","images":[{"href":"h","source":"s","alt":"a"}],"links":[{"href":"h","text":"t"}],"details":{"a":"b"},"severity":"critical","send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","pushover_configs":[{"user_key":{"name":"ref","key":"key"},"token":{"name":"ref","key":"key"},"title":"t","priority":"1","retry":"1m","expire":"1h","html":true,"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","victorops_configs":[{"api_key":{"name":"ref","key":"key"},"api_url":"http://v","routing_key":"r","custom_fields":{"a":"b"},"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","wechat_configs":[{"api_secret":{"name":"ref","key":"key"},"api_url":"http://w","corp_id":"c","agent_id":"a","to_user":"u","message_type":"text","send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","mattermost_configs":[{"url_secret":{"name":"ref","key":"key"},"channel":"c","attachments":[{"title":"t","fields":[{"title":"t","value":"v"}]}],"props":{"card":"c"},"priority":{"priority":"urgent","requested_ack":true},"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","webhook_configs":[{"url_secret":{"name":"ref","key":"key"},"max_alerts":5,"timeout":"10s","send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","telegram_configs":[{"api_url":"http://t","bot_token":{"name":"ref","key":"key"},"chat_id":1,"message_thread_id":2,"parse_mode":"HTML","disable_notifications":true,"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","msteams_configs":[{"webhook_url_secret":{"name":"ref","key":"key"},"title":"t","text":"x","send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","discord_configs":[{"webhook_url_secret":{"name":"ref","key":"key"},"title":"t","message":"m","content":"c","username":"u","avatar_url":"http://a","send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","sns_configs":[{"api_url":"http://s","sigv4":{"region":"r","access_key":"a","access_key_selector":{"name":"ref","key":"key"},"secret_key_selector":{"name":"ref","key":"key"},"role_arn":"arn"},"topic_arn":"t","subject":"s","attributes":{"a":"b"},"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","webex_configs":[{"api_url":"http://w","room_id":"r","message":"m","http_config":{"authorization":{"credentials":{"name":"ref","key":"key"}}},"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","jira_configs":[{"api_url":"http://j","project":"p","issue_type":"Bug","labels":["l"],"custom_fields":{"a":"b"},"reopen_duration":"1h","send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","rocketchat_configs":[{"api_url":"http://r","token_id":{"name":"ref","key":"key"},"token":{"name":"ref","key":"key"},"channel":"c","fields":[{"title":"t","value":"v"}],"actions":[{"type":"button","text":"t","url":"http://a"}],"send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","incidentio_configs":[{"url":"http://i","alert_source_token":{"name":"ref","key":"key"},"max_alerts":1,"timeout":"5s","send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
		`{"name":"r","msteamsv2_configs":[{"webhook_url_secret":{"name":"ref","key":"key"},"title":"t","text":"x","send_resolved":true,"http_config":{"basic_auth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}},"tls_config":{"ca":{"secret":{"name":"ref","key":"key"}},"cert":{"configMap":{"name":"ref","key":"key"}},"keySecret":{"name":"ref","key":"key"}}}}]}`,
	}
	for _, field := range []string{
		"opsgenie_configs", "email_configs", "slack_configs", "pagerduty_configs", "pushover_configs",
		"victorops_configs", "wechat_configs", "mattermost_configs", "webhook_configs", "telegram_configs",
		"msteams_configs", "discord_configs", "sns_configs", "webex_configs", "jira_configs",
		"rocketchat_configs", "incidentio_configs", "msteamsv2_configs",
	} {
		seeds = append(seeds, fmt.Sprintf(`{"name":"receiver","%s":[{}]}`, field))
	}
	for _, seed := range seeds {
		f.Add([]byte(seed), false)
		f.Add([]byte(seed), true)
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		f.Fatal(err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(fuzzutil.RefObjects()...).Build()
	f.Fuzz(func(t *testing.T, data []byte, global bool) {
		var r vmv1beta1.Receiver
		if err := json.Unmarshal(data, &r); err != nil {
			fuzztest.New(data).Fill(&r)
		}
		gc := &globalConfig{}
		if global {
			gc = &fuzzGlobal
		}
		cr := &vmv1beta1.VMAlertmanagerConfig{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "default"}}
		am := fuzzAlertmanager()
		assertStableBuild(t, func() (any, error) {
			ac := getAssetsCache(context.Background(), client, am)
			result := &receiver{Name: buildCRPrefixedName(cr, r.Name)}
			err := buildReceiver(result, cr, r, gc, ac)
			return result, err
		})
	})
}

func FuzzBuildTimeIntervals(f *testing.F) {
	for _, seed := range []string{
		`[]`,
		`[{"name":"office","time_intervals":[{"times":[{"start_time":"09:00","end_time":"17:00"}],"weekdays":["monday:friday"]}]}]`,
		`[{"name":"duplicate","time_intervals":[{}]},{"name":"duplicate","time_intervals":[{}]}]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var intervals []vmv1beta1.TimeIntervals
		if err := json.Unmarshal(data, &intervals); err != nil {
			fuzztest.New(data).Fill(&intervals)
		}
		cr := &vmv1beta1.VMAlertmanagerConfig{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "default"}}
		cr.Spec.TimeIntervals = intervals
		assertStableBuild(t, func() (any, error) { return buildGlobalTimeIntervals(cr) })
	})
}

func FuzzBuildInhibitRule(f *testing.F) {
	for _, seed := range []string{`{}`, `{"source_matchers":["service=api"],"target_matchers":["severity=warning"],"equal":["instance"]}`} {
		f.Add([]byte(seed), false)
		f.Add([]byte(seed), true)
	}
	f.Fuzz(func(t *testing.T, data []byte, enforceNamespace bool) {
		var rule vmv1beta1.InhibitRule
		if err := json.Unmarshal(data, &rule); err != nil {
			fuzztest.New(data).Fill(&rule)
		}
		assertStableBuild(t, func() (any, error) { return buildInhibitRule("default", rule, enforceNamespace), nil })
	})
}
