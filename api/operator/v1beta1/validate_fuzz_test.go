package v1beta1

import (
	"encoding/json"
	"testing"
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

// FuzzValidateCRs feeds the same mutated spec to every v1beta1 CR validator.
func FuzzValidateCRs(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"spec":{"vmstorage":{"replicaCount":2,"maintenanceSelectNodeIDs":[0,1]}}}`,
		`{"spec":{"remoteWrite":[{"url":"http://localhost:8428/api/v1/write"}]}}`,
		`{"spec":{"targetRefs":[{"static":{"url":"http://localhost:8428"},"paths":["/api/v1/.*"]}]}}`,
		`{"spec":{"route":{"receiver":"main","routes":[{"receiver":"main","matchers":["job=api"]}]},"receivers":[{"name":"main"}]}}`,
		`{"spec":{"receivers":[{"name":"main","webhook_configs":[{"url":"http://example.com"}]}]}}`,
		`{"spec":{"receivers":[{"name":"main","jira_configs":[{"api_url":"http://example.com","project":"ops","issue_type":"Bug"}]}]}}`,
		`{"spec":{"receivers":[{"name":"main","slack_configs":[{"api_url":"https://hooks.example.com"}]}]}}`,
		`{"spec":{"time_intervals":[{"name":"office","time_intervals":[{"times":[{"start_time":"09:00","end_time":"17:00"}]}]}]}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		validators := []fuzzValidator{
			&VMAgent{}, &VMAlert{}, &VMAlertmanager{}, &VMAlertmanagerConfig{},
			&VMAuth{}, &VMCluster{}, &VMNodeScrape{}, &VMPodScrape{},
			&VMProbe{}, &VMRule{}, &VMScrapeConfig{}, &VMServiceScrape{},
			&VMSingle{}, &VMStaticScrape{}, &VMUser{},
		}
		for _, cr := range validators {
			if err := json.Unmarshal(data, cr); err != nil {
				continue
			}
			_ = cr.Validate()
		}
	})
}
