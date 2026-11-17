package v1beta1

import (
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

const (
	sc      = `"securityContext":{"runAsUser":1000,"runAsGroup":1000,"fsGroup":1000}`
	invalid = `{"spec":{"extraArgs":{"a":"b"},"replicaCount":1000}}`
)

func FuzzValidators(f *testing.F) {
	fuzzutil.Validators(f,
		fuzzutil.NewValidator(func() *vmv1beta1.VLogs { return &vmv1beta1.VLogs{} }, &VLogsCustomValidator{},
			`{"spec":{`+sc+`}}`, `{"metadata":{"name":"n"},"spec":{"serviceSpec":{"metadata":{"name":"vlogs-n"}}}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} }, &VMAgentCustomValidator{},
			`{"spec":{"remoteWrite":[{"url":"http://vm"}],`+sc+`}}`, invalid),
		fuzzutil.NewValidator(func() *vmv1beta1.VMAlertmanagerConfig { return &vmv1beta1.VMAlertmanagerConfig{} }, &VMAlertmanagerConfigCustomValidator{},
			`{"spec":{"route":{"receiver":"r"},"receivers":[{"name":"r","webhook_configs":[{"url":"http://wh"}]}]}}`,
			`{"spec":{"route":{"receiver":"missing"}}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMAlertmanager { return &vmv1beta1.VMAlertmanager{} }, &VMAlertmanagerCustomValidator{},
			`{"spec":{`+sc+`}}`, `{"spec":{"configRawYaml":"bad: ["}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMAlert { return &vmv1beta1.VMAlert{} }, &VMAlertCustomValidator{},
			`{"spec":{"datasource":{"url":"http://vm"},"notifier":{"url":"http://am"},`+sc+`}}`, invalid),
		fuzzutil.NewValidator(func() *vmv1beta1.VMAuth { return &vmv1beta1.VMAuth{} }, &VMAuthCustomValidator{},
			`{"spec":{`+sc+`}}`, `{"spec":{"unauthorizedUserAccessSpec":{}}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMCluster { return &vmv1beta1.VMCluster{} }, &VMClusterCustomValidator{},
			`{"spec":{"retentionPeriod":"1","vmstorage":{"vmbackup":{"acceptEULA":true,"destination":"fs:///b"},`+sc+`},"vmselect":{`+sc+`},"vminsert":{`+sc+`}}}`,
			`{"spec":{"vmstorage":{"replicaCount":1000}}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMNodeScrape { return &vmv1beta1.VMNodeScrape{} }, &VMNodeScrapeCustomValidator{},
			`{"spec":{"port":"9100"}}`, `{"spec":{"relabelConfigs":[{"action":"bad"}]}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMPodScrape { return &vmv1beta1.VMPodScrape{} }, &VMPodScrapeCustomValidator{},
			`{"spec":{"podMetricsEndpoints":[{"port":"http"}]}}`, `{"spec":{"selector":{"matchExpressions":[{"key":"a","operator":"bad"}]}}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMProbe { return &vmv1beta1.VMProbe{} }, &VMProbeCustomValidator{},
			`{"spec":{"vmProberSpec":{"url":"http://prober"},"targets":{"staticConfig":{"targets":["a"]}}}}`, `{"spec":{"targets":{"kubernetes":[{}]}}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMRule { return &vmv1beta1.VMRule{} }, &VMRuleCustomValidator{},
			`{"spec":{"groups":[{"name":"g","rules":[{"alert":"a","expr":"up"}]}]}}`, `{"spec":{"groups":[{"name":"g","rules":[{"expr":"up{"}]}]}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMScrapeConfig { return &vmv1beta1.VMScrapeConfig{} }, &VMScrapeConfigCustomValidator{},
			`{"spec":{"staticConfigs":[{"targets":["a"]}]}}`, `{"spec":{"relabelConfigs":[{"action":"bad"}]}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMServiceScrape { return &vmv1beta1.VMServiceScrape{} }, &VMServiceScrapeCustomValidator{},
			`{"spec":{"discoveryRole":"endpointslices","endpoints":[{"port":"http"}]}}`, `{"spec":{"selector":{"matchExpressions":[{"key":"a","operator":"bad"}]}}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} }, &VMSingleCustomValidator{},
			`{"spec":{"license":{"key":"k"},"vmBackup":{"acceptEULA":true,"destination":"fs:///b"},`+sc+`}}`, `{"spec":{"vmBackup":{"destination":"fs:///b"}}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMStaticScrape { return &vmv1beta1.VMStaticScrape{} }, &VMStaticScrapeCustomValidator{},
			`{"spec":{"targetEndpoints":[{"targets":["a"]}]}}`, `{"spec":{"targetEndpoints":[null]}}`),
		fuzzutil.NewValidator(func() *vmv1beta1.VMUser { return &vmv1beta1.VMUser{} }, &VMUserCustomValidator{},
			`{"spec":{"targetRefs":[{"static":{"url":"http://vm"}}]}}`, `{"spec":{"targetRefs":[{}]}}`),
	)
}
