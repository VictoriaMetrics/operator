package build

import (
	"slices"
	"testing"
)

func TestShardNumIter(t *testing.T) {
	f := func(backward bool, upperBound int) {
		output := slices.Collect(ShardNumIter(backward, upperBound))
		if upperBound <= 1 {
			if len(output) != 1 || output[0] != nil {
				t.Errorf("invalid ShardNumIter() values, want: [nil]; got: %v", output)
			}
			return
		}
		if upperBound > 1 && len(output) != upperBound {
			t.Errorf("invalid ShardNumIter() items count, want: %d, got: %d", upperBound, len(output))
		}
		var lowerBound int
		if backward {
			lowerBound = upperBound - 1
			upperBound = 0
		} else {
			upperBound--
		}
		if *output[0] != lowerBound || *output[len(output)-1] != upperBound {
			t.Errorf("invalid ShardNumIter() bounds, want: [%d, %d], got: [%d, %d]", lowerBound, upperBound, *output[0], *output[len(output)-1])
		}
	}
	f(true, 9)
	f(false, 5)
	f(false, 1)
	f(true, 0)
}
