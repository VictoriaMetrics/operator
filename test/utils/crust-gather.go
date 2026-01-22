package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
)

const (
	reportLocation = "/tmp/crust-gather"
	tarGzFileName  = reportLocation + ".tar.gz"
)

// RunCrustGather starts the crust-gather process, which collects Kubernetes resources and pod logs.
func RunCrustGather(ctx context.Context, resourceWaitTimeout time.Duration) error {
	timeBoundContext, cancel := context.WithTimeout(ctx, resourceWaitTimeout)
	defer cancel()

	// Collect crust-gather folder
	crustGatherBin := os.Getenv("CRUST_GATHER_BIN")
	cmd := exec.CommandContext(timeBoundContext, crustGatherBin, "collect", "-f", reportLocation)
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("crust-gather collect failed: %v, stdout: %s, stderr: %s", err, outb.String(), errb.String())
	}

	// Archive crust-gather folder
	cmd = exec.CommandContext(timeBoundContext, "tar", "-czvf", tarGzFileName, reportLocation)
	outb.Reset()
	errb.Reset()
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("tar command failed: %v, stdout: %s, stderr: %s", err, outb.String(), errb.String())
	}

	// Add crust-gather.tar.gz to report
	tarGzFileContent, err := os.ReadFile(tarGzFileName)
	if err != nil {
		return fmt.Errorf("failed to read %s: %v", tarGzFileName, err)
	} else {
		baseName := filepath.Base(tarGzFileName)
		ginkgo.AddReportEntry(baseName, string(tarGzFileContent), ginkgo.ReportEntryVisibilityNever)
	}

	return nil
}
