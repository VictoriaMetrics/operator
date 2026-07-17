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

// insertRemoteWriteURL builds the old vminsert's remote_write URL, assuming the default path -
// a bare Service carries no ExtraArgs, so a non-default --http.pathPrefix on the old vminsert
// isn't reflected here (writes fail and queue instead, then flush once repointed at the target).
func insertRemoteWriteURL(svc *corev1.Service) (string, error) {
	base, err := migrate.ServiceBaseURL(svc, "http", "http")
	if err != nil {
		return "", err
	}
	return base + "/insert/multitenant/prometheus/api/v1/write", nil
}
