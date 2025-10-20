package build

import (
	"iter"
	"sort"
)

type keysSorter[T any] struct {
	target []T
	sorter []string
}

// OrderByKeys orders targets slice according sorter slice sorting result
func OrderByKeys[T any](target []T, sorter []string) {
	if len(target) != len(sorter) {
		panic("BUG: target and sorter names are expected to be equal")
	}
	s := &keysSorter[T]{
		target: target,
		sorter: sorter,
	}
	sort.Sort(s)
}

// Len implements sort.Interface
func (s *keysSorter[T]) Len() int {
	return len(s.sorter)
}

// Less implements sort.Interface
func (s *keysSorter[T]) Less(i, j int) bool {
	return s.sorter[i] < s.sorter[j]
}

// Swap implements sort.Interface
func (s *keysSorter[T]) Swap(i, j int) {
	s.target[i], s.target[j] = s.target[j], s.target[i]
	s.sorter[i], s.sorter[j] = s.sorter[j], s.sorter[i]
}

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
