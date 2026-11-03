package v1beta1

import (
	"encoding/json"
	"testing"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
)

type validatorKind uint8

const (
	vmAgentKind validatorKind = iota
	vmAlertKind
	vmAlertmanagerKind
	vmAlertmanagerConfigKind
	vmAuthKind
	vmClusterKind
	vmNodeScrapeKind
	vmPodScrapeKind
	vmProbeKind
	vmRuleKind
	vmScrapeConfigKind
	vmServiceScrapeKind
	vmSingleKind
	vmStaticScrapeKind
	vmUserKind
)

type fuzzValidator interface {
	Validate() error
}

func fuzzValidatorFor(selector byte) fuzzValidator {
	validators := [...]fuzzValidator{
		&VMAgent{}, &VMAlert{}, &VMAlertmanager{}, &VMAlertmanagerConfig{},
		&VMAuth{}, &VMCluster{}, &VMNodeScrape{}, &VMPodScrape{},
		&VMProbe{}, &VMRule{}, &VMScrapeConfig{}, &VMServiceScrape{},
		&VMSingle{}, &VMStaticScrape{}, &VMUser{},
	}
	return validators[int(selector)%len(validators)]
}

func FuzzValidateCRs(f *testing.F) {
	for _, seed := range []struct {
		kind validatorKind
		data string
	}{
		{vmAgentKind, `{}`},
		{vmClusterKind, `{"spec":{"vmstorage":{"replicaCount":2,"maintenanceSelectNodeIDs":[0,1]}}}`},
		{vmClusterKind, `{"spec":{"vmstorage":{"hpa":{"minReplicas":1,"maxReplicas":101}}}}`},
		{vmProbeKind, `{"spec":{"targets":{"kubernetes":[null]}}}`},
		{vmAgentKind, `{"spec":{"remoteWrite":[{"url":"http://localhost:8428/api/v1/write"}]}}`},
		{vmUserKind, `{"spec":{"targetRefs":[{"static":{"url":"http://localhost:8428"},"paths":["/api/v1/.*"]}]}}`},
		{vmClusterKind, "\r\r\r\r\r"},
		{vmAlertmanagerConfigKind, `{"spec":{"route":{"receiver":"main","routes":[{"receiver":"main","matchers":["job=api"]}]},"receivers":[{"name":"main"}]}}`},
		{vmAlertmanagerConfigKind, `{"spec":{"receivers":[{"name":"main","webhook_configs":[{"url":"http://example.com"}]}]}}`},
		{vmAlertmanagerConfigKind, `{"spec":{"receivers":[{"name":"main","jira_configs":[{"api_url":"http://example.com","project":"ops","issue_type":"Bug"}]}]}}`},
		{vmAlertmanagerConfigKind, `{"spec":{"receivers":[{"name":"main","slack_configs":[{"api_url":{"name":"slack-webhook","key":"url"}}]}]}}`},
		{vmAlertmanagerConfigKind, `{"spec":{"time_intervals":[{"name":"office","time_intervals":[{"times":[{"start_time":"09:00","end_time":"17:00"}]}]}]}}`},
	} {
		f.Add(append([]byte{byte(seed.kind)}, seed.data...))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		parsed := fuzzValidatorFor(data[0])
		if err := json.Unmarshal(data[1:], parsed); err == nil {
			_ = parsed.Validate()
		}
		cr := fuzzValidatorFor(data[0])
		fuzztest.New(data[1:]).MaxDepth(8).Fill(cr)
		_ = cr.Validate()
	})
}
