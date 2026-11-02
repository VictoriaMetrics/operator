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
		{vlDistributedKind, `{}`},
		{vlDistributedKind, `{"spec":{"backendType":"VLCluster","zones":[{"name":"east","vlcluster":{"name":"east"}}]}}`},
		{vlDistributedKind, `{"spec":{"backendType":"VLSingle","zones":[{"name":"east","vlsingle":{"name":"east"}}]}}`},
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
		fuzztest.New(data).Fill(cr)
		_ = cr.Validate()
	})
}
