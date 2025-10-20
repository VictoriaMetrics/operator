package build

import (
	"fmt"
	"iter"
	"strconv"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	ShardNumPlaceholder = "%SHARD_NUM%"
	shardLabelName      = "shard-num"
)

// ShardNumIter iterates over shardCount in order defined in backward
func ShardNumIter(backward bool, shardCount int) iter.Seq[*int] {
	if shardCount <= 1 {
		return func(yield func(*int) bool) {
			yield(nil)
		}
	}
	if backward {
		return func(yield func(*int) bool) {
			for shardCount > 0 {
				shardCount--
				num := shardCount
				if !yield(&num) {
					return
				}
			}
		}
	}
	return func(yield func(*int) bool) {
		for i := 0; i < shardCount; i++ {
			if !yield(&i) {
				return
			}
		}
	}
}

func isSharded(shardCount int) bool {
	return shardCount > 1
}

// ShardName adds shard suffix to a name if shards count is > 1
func ShardName(name string, shardCount int) string {
	if isSharded(shardCount) {
		name = fmt.Sprintf("%s-%s", name, ShardNumPlaceholder)
	}
	return name
}

// ShardLabels adds shard label pair if shards count is > 1
func ShardLabels(labels map[string]string, shardCount int) map[string]string {
	if isSharded(shardCount) {
		labels[shardLabelName] = "%SHARD_NUM%"
	}
	return labels
}

// RenderShard renders resource's placeholders with optional shard number placeholder in num is defined
func RenderShard[T any](resource *T, placeholders map[string]string, num *int) (*T, error) {
	if placeholders == nil {
		placeholders = make(map[string]string)
	}
	if num != nil {
		placeholders[ShardNumPlaceholder] = strconv.Itoa(*num)
	}
	return k8stools.RenderPlaceholders(resource, placeholders)
}
