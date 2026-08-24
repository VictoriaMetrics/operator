package vlagent

import (
	"os"
	"testing"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestMain(m *testing.M) {
	// Native SleepAction is supported from k8s 1.30 (feature gate is off by default in 1.29); set it globally for all tests in this package.
	k8stools.ServerMajorVersion = 1
	k8stools.ServerMinorVersion = 30
	os.Exit(m.Run())
}
