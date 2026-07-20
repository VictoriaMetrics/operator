package vl

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// remoteWriteURL builds the VictoriaLogs remote-write base URL for a Service. Unlike VM's
// /api/v1/write, vlagent's remoteWrite.url is just the bare base URL — vlagent/vlsingle
// resolve the actual ingestion path themselves.
func remoteWriteURL(svc *corev1.Service) (string, error) {
	base, err := migrate.ServiceBaseURL(svc, "http", "http")
	if err != nil {
		return "", err
	}
	return base + "/", nil
}
