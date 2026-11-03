package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
)

type distributedKind uint8

const (
	vmDistributedKind distributedKind = iota
	vlDistributedKind
	distributedKindCount
)

func FuzzValidateCRs(f *testing.F) {
	for _, seed := range []struct {
		kind distributedKind
		data string
	}{
		{vmDistributedKind, `{}`},
		{vmDistributedKind, `{"spec":{"backendType":"VMCluster","zones":[{"name":"east","vmcluster":{"name":"east"}}]}}`},
		{vmDistributedKind, `{"spec":{"backendType":"VMSingle","zones":[{"name":"east","vmsingle":{"name":"east"}},{"name":"west","vmsingle":{"name":"west"}}]}}`},
		{vmDistributedKind, `{"spec":{"backendType":"VMSingle","zoneCommon":{"vmcluster":{"name":"c"}}}}`},
		{vmDistributedKind, `{"spec":{"zoneCommon":{"vmsingle":{"name":"s"}}}}`},
		{vmDistributedKind, `{"spec":{"zones":[{}]}}`},
		{vmDistributedKind, `{"spec":{"zones":[{"name":"a","vmcluster":{"spec":{"vminsert":{},"vmselect":{}}}},{"name":"a"}]}}`},
		{vmDistributedKind, `{"spec":{"backendType":"VMSingle","zones":[{"name":"a","vmcluster":{"name":"c"}}]}}`},
		{vmDistributedKind, `{"spec":{"backendType":"VMSingle","zones":[{"name":"a","vmsingle":{"name":"s"}},{"name":"b","vmsingle":{"name":"s"}}]}}`},
		{vmDistributedKind, `{"spec":{"zones":[{"name":"a","vmsingle":{"name":"s"}}]}}`},
		{vmDistributedKind, `{"spec":{"zoneCommon":{"vmcluster":{"spec":{"vminsert":{},"vmselect":{}}}},"zones":[{"name":"a","vmcluster":{"name":"c"}},{"name":"b","vmcluster":{"name":"c"}}]}}`},
		{vmDistributedKind, `{"spec":{"zones":[{"name":"a"}]}}`},
		{vmDistributedKind, `{"spec":{"zones":[{"name":"a","vmcluster":{"spec":{"vminsert":{}}}}]}}`},
		{vmDistributedKind, `{"spec":{"zoneCommon":{"vmcluster":{"spec":{"vminsert":{},"vmselect":{}}}},"zones":[{"name":"a","vmagent":{"name":"x"}},{"name":"b","vmagent":{"name":"x"}}]}}`},
		{vmDistributedKind, `{"spec":{"zoneCommon":{"vmcluster":{"spec":{"vminsert":{},"vmselect":{}}}},"zones":[{"name":"a","vmagent":{"spec":{"statefulMode":true,"statefulRollingUpdateStrategyBehavior":{"maxUnavailable":"0%"}}}}]}}`},
		{vmDistributedKind, `{"spec":{"zoneCommon":{"vmcluster":{"spec":{"vminsert":{},"vmselect":{}}}},"zones":[{"name":"a","vmagent":{"spec":{"statefulMode":true,"statefulRollingUpdateStrategyBehavior":{"maxUnavailable":1}}}}]}}`},
		{vlDistributedKind, `{}`},
		{vlDistributedKind, `{"spec":{"backendType":"VLCluster","zones":[{"name":"east","vlcluster":{"name":"east"}}]}}`},
		{vlDistributedKind, `{"spec":{"backendType":"VLSingle","zones":[{"name":"east","vlsingle":{"name":"east"}}]}}`},
		{vlDistributedKind, `{"spec":{"backendType":"VLSingle","zoneCommon":{"vlcluster":{"name":"c"}}}}`},
		{vlDistributedKind, `{"spec":{"zoneCommon":{"vlsingle":{"name":"s"}}}}`},
		{vlDistributedKind, `{"spec":{"zones":[{}]}}`},
		{vlDistributedKind, `{"spec":{"zones":[{"name":"a","vlcluster":{"spec":{"vlinsert":{},"vlselect":{}}}},{"name":"a"}]}}`},
		{vlDistributedKind, `{"spec":{"backendType":"VLSingle","zones":[{"name":"a","vlcluster":{"name":"c"}}]}}`},
		{vlDistributedKind, `{"spec":{"backendType":"VLSingle","zones":[{"name":"a","vlsingle":{"name":"s"}},{"name":"b","vlsingle":{"name":"s"}}]}}`},
		{vlDistributedKind, `{"spec":{"zones":[{"name":"a","vlsingle":{"name":"s"}}]}}`},
		{vlDistributedKind, `{"spec":{"zoneCommon":{"vlcluster":{"spec":{"vlinsert":{},"vlselect":{}}}},"zones":[{"name":"a","vlcluster":{"name":"c"}},{"name":"b","vlcluster":{"name":"c"}}]}}`},
		{vlDistributedKind, `{"spec":{"zones":[{"name":"a"}]}}`},
		{vlDistributedKind, `{"spec":{"zones":[{"name":"a","vlcluster":{"spec":{"vlinsert":{}}}}]}}`},
		{vlDistributedKind, `{"spec":{"zoneCommon":{"vlcluster":{"spec":{"vlinsert":{},"vlselect":{}}}},"zones":[{"name":"a","vlagent":{"name":"x"}},{"name":"b","vlagent":{"name":"x"}}]}}`},
	} {
		f.Add(uint8(seed.kind), []byte(seed.data))
	}
	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		var cr interface{ Validate() error } = &VMDistributed{}
		if distributedKind(kind%uint8(distributedKindCount)) == vlDistributedKind {
			cr = &VLDistributed{}
		}
		if err := json.Unmarshal(data, cr); err == nil {
			_ = cr.Validate()
		}
		// Limit depth past unsupported interfaces.
		fuzztest.New(data).MaxDepth(8).Fill(cr)
		_ = cr.Validate()
	})
}
