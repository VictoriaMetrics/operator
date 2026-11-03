package v1alpha1

import (
	"testing"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

const (
	sc      = `"securityContext":{"runAsUser":1000,"runAsGroup":1000,"fsGroup":1000}`
	invalid = `{"spec":{"zones":[{"name":"a"},{"name":"a"}]}}`
)

func FuzzValidators(f *testing.F) {
	fuzzutil.Validators(f,
		fuzzutil.NewValidator(func() *vmv1alpha1.VMDistributed { return &vmv1alpha1.VMDistributed{} }, &VMDistributedCustomValidator{},
			`{"spec":{"vmauth":{"spec":{`+sc+`}},"zoneCommon":{"vmcluster":{"spec":{"vminsert":{},"vmselect":{},"vmstorage":{`+sc+`}}},"vmagent":{"spec":{`+sc+`}}},"zones":[{"name":"a"},{"name":"b"}]}}`,
			`{"spec":{"backendType":"VMSingle","zoneCommon":{"vmsingle":{"spec":{`+sc+`}}},"zones":[{"name":"a","vmsingle":{"spec":{`+sc+`}}}]}}`,
			invalid),
		fuzzutil.NewValidator(func() *vmv1alpha1.VLDistributed { return &vmv1alpha1.VLDistributed{} }, &VLDistributedCustomValidator{},
			`{"spec":{"zoneCommon":{"vlcluster":{"spec":{"vlinsert":{},"vlselect":{},"vlstorage":{`+sc+`}}},"vlagent":{"spec":{`+sc+`}}},"zones":[{"name":"a"},{"name":"b"}]}}`,
			`{"spec":{"backendType":"VLSingle","zoneCommon":{"vlsingle":{"spec":{`+sc+`}}},"zones":[{"name":"a","vlsingle":{"spec":{`+sc+`}}}]}}`,
			invalid),
	)
}
