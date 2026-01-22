package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
)

const (
	crustGatherVersionlessBin = "bin/crust-gather"
	crustGatherInvalidPath    = "/tmp/crust-gather.gz"
	filePath                  = "/tmp/crust-gather.tar.gz"
)

// RunCrustGather starts the crust-gather process, which collects Kubernetes resources and pod logs.
func RunCrustGather(ctx context.Context, resourceWaitTimeout time.Duration) error {
	timeBoundContext, cancel := context.WithTimeout(ctx, resourceWaitTimeout)
	defer cancel()

	// Run crust-gather
	// Crust-gather bug: instead of /tmp/crust-gather.gz it creates /tmp/crust-gather.tar.gz
	cmd := exec.CommandContext(timeBoundContext, crustGatherVersionlessBin, "collect", "-f", crustGatherInvalidPath, "-e", "gzip")
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to run crust-gather: %v", err)
	}

	// Attach generated file to the report
	gzipFileContent, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %v", filePath, err)
	}
	baseName := filepath.Base(filePath)
	ginkgo.AddReportEntry(baseName, string(gzipFileContent), ginkgo.ReportEntryVisibilityNever)

	return nil
}
