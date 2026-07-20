package vl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/VictoriaMetrics/operator/internal/migrate"
)

func TestNewBufferAgent(t *testing.T) {
	agent, err := newBufferAgent("myrelease-migration-buffer", "default", "http://old-svc.default.svc:9428/", "20Gi")
	require.NoError(t, err)

	assert.Equal(t, "myrelease-migration-buffer", agent.Name)
	assert.Equal(t, "default", agent.Namespace)
	require.Len(t, agent.Spec.RemoteWrite, 1)
	assert.Equal(t, "http://old-svc.default.svc:9428/", agent.Spec.RemoteWrite[0].URL)
	require.NotNil(t, agent.Spec.Storage)
	assert.Equal(t, resource.MustParse("20Gi"), agent.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage])
}

func TestNewBufferAgent_DefaultBufferSize(t *testing.T) {
	agent, err := newBufferAgent("agent", "default", "http://x", "")
	require.NoError(t, err)
	assert.Equal(t, resource.MustParse(migrate.DefaultAgentBufferSize), agent.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage])
}

func TestNewBufferAgent_InvalidBufferSize(t *testing.T) {
	_, err := newBufferAgent("agent", "default", "http://x", "not-a-size")
	assert.Error(t, err)
}
