package v1alpha1

import (
	"encoding/json"
	"testing"
)

func FuzzValidateCRs(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"spec":{"backendType":"VMCluster","zones":[{"name":"east","vmcluster":{"name":"east"}}]}}`,
		`{"spec":{"backendType":"VMSingle","zones":[{"name":"east","vmsingle":{"name":"east"}},{"name":"west","vmsingle":{"name":"west"}}]}}`,
		`{"spec":{"backendType":"VLCluster","zones":[{"name":"east","vlcluster":{"name":"east"}}]}}`,
		`{"spec":{"backendType":"VLSingle","zones":[{"name":"east","vlsingle":{"name":"east"}}]}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		for _, cr := range []interface{ Validate() error }{&VMDistributed{}, &VLDistributed{}} {
			if err := json.Unmarshal(data, cr); err != nil {
				continue
			}
			_ = cr.Validate()
		}
	})
}
