package v1

import (
	"testing"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

const (
	sc      = `"securityContext":{"runAsUser":1000,"runAsGroup":1000,"fsGroup":1000}`
	rw      = `"remoteWrite":[{"url":"http://vl"}]`
	license = `"license":{"key":"k"}`
	invalid = `{"spec":{"terminationGracePeriodSeconds":1,"preStopSleepSeconds":5}}`
)

func FuzzValidators(f *testing.F) {
	fuzzutil.Validators(f,
		fuzzutil.NewValidator(func() *vmv1.VLAgent { return &vmv1.VLAgent{} }, &VLAgentCustomValidator{},
			`{"spec":{`+rw+`,`+sc+`}}`, `{"spec":{}}`, `{"spec":{`+rw+`,"k8sCollector":{"enabled":true,"extraFields":"{"}}}`),
		fuzzutil.NewValidator(func() *vmv1.VLCluster { return &vmv1.VLCluster{} }, &VLClusterCustomValidator{},
			`{"spec":{"vlinsert":{`+sc+`},"vlselect":{`+sc+`},"vlstorage":{`+sc+`}}}`, `{"spec":{"vlstorage":{"replicaCount":1000}}}`),
		fuzzutil.NewValidator(func() *vmv1.VLSingle { return &vmv1.VLSingle{} }, &VLSingleCustomValidator{},
			`{"spec":{`+sc+`}}`, invalid),
		fuzzutil.NewValidator(func() *vmv1.VMAnomalyConfig { return &vmv1.VMAnomalyConfig{} }, &VMAnomalyConfigCustomValidator{},
			`{"spec":{"models":{"m":{"class":"zscore","z_threshold":2.5}},"schedulers":{"s":{"class":"periodic","infer_every":"1m","fit_every":"1h","fit_window":"1d"}}}}`,
			`{"spec":{"models":{"m":{"class":"bad"}}}}`, `{"spec":{"models":[1]}}`, `{"spec":{"schedulers":{"s":null}}}`),
		fuzzutil.NewValidator(func() *vmv1.VMAnomaly { return &vmv1.VMAnomaly{} }, &VMAnomalyCustomValidator{},
			`{"spec":{`+license+`,"configRawYaml":"reader: {}",`+sc+`}}`, `{"spec":{"configRawYaml":"reader: {}"}}`, `{"spec":{`+license+`,"vpa":{}}}`),
		fuzzutil.NewValidator(func() *vmv1.VTAgent { return &vmv1.VTAgent{} }, &VTAgentCustomValidator{},
			`{"spec":{`+rw+`,`+sc+`}}`, `{"spec":{}}`),
		fuzzutil.NewValidator(func() *vmv1.VTCluster { return &vmv1.VTCluster{} }, &VTClusterCustomValidator{},
			`{"spec":{"insert":{`+sc+`},"select":{`+sc+`},"storage":{`+sc+`}}}`, `{"spec":{"storage":{"replicaCount":1000}}}`),
		fuzzutil.NewValidator(func() *vmv1.VTSingle { return &vmv1.VTSingle{} }, &VTSingleCustomValidator{},
			`{"spec":{`+sc+`}}`, invalid),
	)
}
