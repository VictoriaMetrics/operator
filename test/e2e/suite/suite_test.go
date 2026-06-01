package suite

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShutdownOperatorProcessWithoutStartedManager(t *testing.T) {
	oldTestEnv := testEnv
	oldCancelManager := cancelManager
	oldStopped := stopped
	t.Cleanup(func() {
		testEnv = oldTestEnv
		cancelManager = oldCancelManager
		stopped = oldStopped
	})

	testEnv = nil
	cancelManager = nil
	stopped = make(chan struct{})

	assert.NotPanics(t, shutdownOperatorProcess)
}
