package vm

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/internal/migrate"
)

// remoteWriteURL builds the VictoriaMetrics remote_write ingestion URL for a Service.
func remoteWriteURL(svc *corev1.Service) (string, error) {
	base, err := migrate.ServiceBaseURL(svc, "http", "http")
	if err != nil {
		return "", err
	}
	return base + "/api/v1/write", nil
}

// insertRemoteWriteURL builds the VMInsert remote_write URL, matching the default path
// VMCluster.GetRemoteWriteURL() uses for the target cluster's own vminsert.
func insertRemoteWriteURL(svc *corev1.Service) (string, error) {
	base, err := migrate.ServiceBaseURL(svc, "http", "http")
	if err != nil {
		return "", err
	}
	return base + "/insert/multitenant/prometheus/api/v1/write", nil
}
