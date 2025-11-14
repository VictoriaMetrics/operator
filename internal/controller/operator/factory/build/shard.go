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
func ShardNumIter(backward bool, shardCount int) iter.Seq[int] {
	if backward {
		return func(yield func(int) bool) {
			for shardCount > 0 {
				shardCount--
				num := shardCount
				if !yield(num) {
					return
				}
			}
		}
	}
	return func(yield func(int) bool) {
		for i := 0; i < shardCount; i++ {
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
func RenderShard[T any](resource *T, num int) (*T, error) {
	placeholders := map[string]string{
		shardNumPlaceholder: strconv.Itoa(num),
	}
	return k8stools.RenderPlaceholders(resource, placeholders)
}

// ShardPodDisruptionBudget creates object for given CRD and shard num
func ShardPodDisruptionBudget(cr shardOpts, spec *vmv1beta1.EmbeddedPodDisruptionBudgetSpec, num int) *policyv1.PodDisruptionBudget {
	pdb := PodDisruptionBudget(cr, spec)
	if cr.IsSharded() {
		pdb.Name = fmt.Sprintf("%s-%d", pdb.Name, num)
		pdb.Spec.Selector.MatchLabels[shardLabelName] = strconv.Itoa(num)
	}
	return pdb
}
