package build

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShardNumIter(t *testing.T) {
	f := func(backward bool, upperBound int32) {
		output := slices.Collect(ShardNumIter(backward, upperBound))
		assert.Len(t, output, int(upperBound), "invalid ShardNumIter() items count")
		var lowerBound int32
		if backward {
			lowerBound = upperBound - 1
			upperBound = 0
		} else {
			upperBound--
		}
		assert.Equal(t, lowerBound, output[0], "invalid ShardNumIter() lower bound")
		assert.Equal(t, upperBound, output[len(output)-1], "invalid ShardNumIter() upper bound")
	}
	f(true, 9)
	f(false, 5)
}
