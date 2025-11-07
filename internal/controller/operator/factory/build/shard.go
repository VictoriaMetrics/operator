package build

import (
	"fmt"
	"iter"
	"strconv"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	shardNumPlaceholder = "%SHARD_NUM%"
	shardLabelName      = "shard-num"
)

type shardedApp interface {
	PrefixedName() string
	IsSharded() bool
	SelectorLabels() map[string]string
	PodLabels() map[string]string
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
func ShardName(app shardedApp) string {
	name := app.PrefixedName()
	if app.IsSharded() {
		name = fmt.Sprintf("%s-%s", name, shardNumPlaceholder)
	}
	return name
}

// ShardSelectorLabels adds shard label pair if sharding enabled
func ShardSelectorLabels(app shardedApp) map[string]string {
	labels := app.SelectorLabels()
	if app.IsSharded() {
		labels[shardLabelName] = shardNumPlaceholder
	}
	return labels
}

// ShardPodLabels adds shard label pair if sharding enabled
func ShardPodLabels(app shardedApp) map[string]string {
	labels := app.PodLabels()
	if app.IsSharded() {
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
