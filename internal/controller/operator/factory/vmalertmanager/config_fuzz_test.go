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
			fuzztest.New(data).Fuzz(&cr)
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

func FuzzBuildReceiver(f *testing.F) {
	f.Add([]byte(`{"name":"empty"}`))
	for _, field := range []string{
		"opsgenie_configs", "email_configs", "slack_configs", "pagerduty_configs", "pushover_configs",
		"victorops_configs", "wechat_configs", "mattermost_configs", "webhook_configs", "telegram_configs",
		"msteams_configs", "discord_configs", "sns_configs", "webex_configs", "jira_configs",
		"rocketchat_configs", "incidentio_configs", "msteamsv2_configs",
	} {
		f.Add([]byte(fmt.Sprintf(`{"name":"receiver","%s":[{}]}`, field)))
	}
	f.Add([]byte(`{"name":"hook","webhook_configs":[{"url":"https://example.org","http_config":{"follow_redirects":true}}]}`))
	f.Add([]byte(`{"name":"slack","slack_configs":[{"api_url":"https://example.org","fields":[{"title":"site","value":"prod"}]}]}`))
	f.Add([]byte(`{"name":"mattermost","mattermost_configs":[{"attachments":[null]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var r vmv1beta1.Receiver
		if err := json.Unmarshal(data, &r); err != nil {
			fuzztest.New(data).Fuzz(&r)
		}
		cr := &vmv1beta1.VMAlertmanagerConfig{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "default"}}
		am := fuzzAlertmanager()
		scheme := runtime.NewScheme()
		if err := corev1.AddToScheme(scheme); err != nil {
			t.Fatal(err)
		}
		client := fake.NewClientBuilder().WithScheme(scheme).Build()
		assertStableBuild(t, func() (any, error) {
			ac := getAssetsCache(context.Background(), client, am)
			result := &receiver{Name: buildCRPrefixedName(cr, r.Name)}
			err := buildReceiver(result, cr, r, &globalConfig{}, ac)
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
			fuzztest.New(data).Fuzz(&intervals)
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
			fuzztest.New(data).Fuzz(&rule)
		}
		assertStableBuild(t, func() (any, error) { return buildInhibitRule("default", rule, enforceNamespace), nil })
	})
}
