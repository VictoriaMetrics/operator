package utils

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/onsi/ginkgo/v2"

	"github.com/VictoriaMetrics/operator/test/e2e/suite/allure"
)

const (
	reportsDir      = "crust-gather"
	reportsLocation = "/tmp/" + reportsDir
)

// RunCrustGather starts the crust-gather process, which collects Kubernetes resources and pod logs.
func RunCrustGather(ctx context.Context, resourceWaitTimeout time.Duration) error {
	timeBoundContext, cancel := context.WithTimeout(ctx, resourceWaitTimeout)
	defer cancel()

	// Collect crust-gather folder
	crustGatherBin := os.Getenv("CRUST_GATHER_BIN")
	report := ginkgo.CurrentSpecReport()
	reportHash := fmt.Sprintf("%016x", xxhash.Sum64([]byte(report.FullText())))
	reportDir := filepath.Join(reportsLocation, reportHash)
	cmd := exec.CommandContext(timeBoundContext, crustGatherBin, "collect", "-v", "WARN", "-f", reportDir)
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("crust-gather collect failed: %v, stdout: %s, stderr: %s", err, outb.String(), errb.String())
	}

	// Archive crust-gather folder
	archiveName := reportHash + ".tar.gz"
	archivePath := filepath.Join(reportsLocation, archiveName)
	cmd = exec.CommandContext(timeBoundContext, "tar", "-czvf", archiveName, reportHash)
	cmd.Dir = reportsLocation
	outb.Reset()
	errb.Reset()
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("tar command failed: %v, stdout: %s, stderr: %s", err, outb.String(), errb.String())
	}
	content, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", archivePath, err)
	}
	allure.AddAttachment("crust-gather.tar.gz", allure.MimeTypeGZIP, content)
	return nil
}
