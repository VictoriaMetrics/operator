package build

import (
	"fmt"
	"iter"
	"strconv"

	policyv1 "k8s.io/api/policy/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	shardNumPlaceholder = "%SHARD_NUM%"
	shardLabelName      = "shard-num"
)

type shardOpts interface {
	builderOpts
	PodLabels() map[string]string
	IsSharded() bool
}

// ShardNumIter iterates over shardCount in order defined in backward
func ShardNumIter(backward bool, shardCount int32) iter.Seq[int32] {
	if backward {
		return func(yield func(int32) bool) {
			for shardCount > 0 {
				shardCount--
				num := shardCount
				if !yield(num) {
					return
				}
			}
		}
	}
	return func(yield func(int32) bool) {
		for i := int32(0); i < shardCount; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

// ShardName adds shard suffix to a name if shards count is > 1
func ShardName(cr shardOpts) string {
	name := cr.PrefixedName()
	if cr.IsSharded() {
		name = fmt.Sprintf("%s-%s", name, shardNumPlaceholder)
	}
	return name
}

// ShardSelectorLabels adds shard label pair if sharding enabled
func ShardSelectorLabels(cr shardOpts) map[string]string {
	labels := cr.SelectorLabels()
	if cr.IsSharded() {
		labels[shardLabelName] = shardNumPlaceholder
	}
	return labels
}

// ShardPodLabels adds shard label pair if sharding enabled
func ShardPodLabels(cr shardOpts) map[string]string {
	labels := cr.PodLabels()
	if cr.IsSharded() {
		labels[shardLabelName] = shardNumPlaceholder
	}
	return labels
}

// RenderShard replaces resource's shard number placeholder with a given shard number
func RenderShard[T any](resource *T, num int32) (*T, error) {
	placeholders := map[string]string{
		shardNumPlaceholder: strconv.FormatInt(int64(num), 32),
	}
	return k8stools.RenderPlaceholders(resource, placeholders)
}

// ShardPodDisruptionBudget creates object for given CRD and shard num
func ShardPodDisruptionBudget(cr shardOpts, spec *vmv1beta1.EmbeddedPodDisruptionBudgetSpec, num int32) *policyv1.PodDisruptionBudget {
	pdb := PodDisruptionBudget(cr, spec)
	if cr.IsSharded() {
		pdb.Name = fmt.Sprintf("%s-%d", pdb.Name, num)
		pdb.Spec.Selector.MatchLabels[shardLabelName] = strconv.FormatInt(int64(num), 32)
	}
	return pdb
}
