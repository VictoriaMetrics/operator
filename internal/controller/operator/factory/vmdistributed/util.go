package vmdistributed

import (
	"fmt"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func mergeSpecs[T any](a, b *T, name string) (*T, error) {
	merged, err := k8stools.RenderPlaceholders(a, map[string]string{
		vmv1alpha1.ZonePlaceholder: name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render spec: %w", err)
	}

	// Apply cluster-specific override if it exist
	if err := build.MergeDeep(merged, b, false); err != nil {
		return nil, fmt.Errorf("failed to merge spec: %w", err)
	}
	return merged, nil
}
