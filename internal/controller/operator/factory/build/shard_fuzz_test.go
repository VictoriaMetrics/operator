package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzShardBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"shardCount":3,"num":1}`,
		`{"shardCount":3,"num":2,"backward":true,"pdb":{"minAvailable":1}}`,
		`{"shardCount":1,"pdb":{"maxUnavailable":"50%","selectorLabels":{"app":"custom"}}}`,
		`{"shardCount":0,"num":5}`,
		`{"shardCount":-2,"num":-1,"backward":true}`,
		`{"shardCount":2,"daemonSetMode":true}`,
		`{"shardCount":2,"num":-7,"data":{"k":"%SHARD_NUM%","%SHARD_NUM%":"v"}}`,
		`{"shardCount":4,"pdb":{"selectorLabels":{"shard-num":"x"},"unhealthyPodEvictionPolicy":"AlwaysAllow"}}`,
		`{"shardCount":2147483647,"num":2147483647,"backward":true}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			ShardCount    *int32                                    `json:"shardCount"`
			DaemonSetMode bool                                      `json:"daemonSetMode"`
			Num           int32                                     `json:"num"`
			Backward      bool                                      `json:"backward"`
			PDB           vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"pdb"`
			Data          map[string]string                         `json:"data"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		meta := metav1.ObjectMeta{Name: "agent", Namespace: "default"}
		agent := &vmv1beta1.VMAgent{ObjectMeta: meta}
		agent.Spec.ShardCount = input.ShardCount
		agent.Spec.DaemonSetMode = input.DaemonSetMode
		anomaly := &vmv1.VMAnomaly{ObjectMeta: meta}
		anomaly.Spec.ShardCount = ptr.To(ptr.Deref(input.ShardCount, 0))

		for _, cr := range []shardOpts{agent, anomaly} {
			ShardName(cr)
			ShardSelectorLabels(cr)
			ShardPodLabels(cr)
			ShardPodDisruptionBudget(cr, &input.PDB, input.Num)
		}

		for num := range ShardNumIter(input.Backward, ptr.Deref(input.ShardCount, 0)) {
			RenderShard(&corev1.ConfigMap{Data: input.Data}, num)
			// stop early to cover the yield-false path
			if num == input.Num {
				break
			}
		}
		RenderShard(&corev1.ConfigMap{Data: input.Data}, input.Num)
		RenderShard[corev1.ConfigMap](nil, input.Num)
	})
}
