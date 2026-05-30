package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestVMAnomaly_DefaultStatusFields(t *testing.T) {
	f := func(cr *VMAnomaly, wantShards int32) {
		t.Helper()
		var status VMAnomalyStatus
		cr.DefaultStatusFields(&status)
		assert.Equal(t, wantShards, status.Shards)
	}

	// no shardCount set: must report 1 so VPA scale subresource gets a non-zero statusReplicasPath
	f(&VMAnomaly{Spec: VMAnomalySpec{}}, 1)

	// shardCount explicitly set
	f(&VMAnomaly{Spec: VMAnomalySpec{ShardCount: ptr.To(int32(3))}}, 3)

	// shardCount=1 explicitly
	f(&VMAnomaly{Spec: VMAnomalySpec{ShardCount: ptr.To(int32(1))}}, 1)
}
