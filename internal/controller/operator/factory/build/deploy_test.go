package build

import (
	"slices"
	"testing"
)

func TestShardNumIter(t *testing.T) {
	f := func(backward bool, upperBound int) {
		output := slices.Collect(shardNumIter(backward, upperBound))
		if len(output) != upperBound {
			t.Errorf("invalid shardNumIter() items count, want: %d, got: %d", upperBound, len(output))
		}
		var lowerBound int
		if backward {
			lowerBound = upperBound - 1
			upperBound = 0
		} else {
			upperBound--
		}
		if output[0] != lowerBound || output[len(output)-1] != upperBound {
			t.Errorf("invalid shardNumIter() bounds, want: [%d, %d], got: [%d, %d]", lowerBound, upperBound, output[0], output[len(output)-1])
		}
	}
	f(true, 9)
	f(false, 5)
}
