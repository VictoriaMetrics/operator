package migrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateAliasService(t *testing.T) {
	ns := "default"
	ports := []corev1.ServicePort{{Name: "http", Port: 8428}}
	selector := map[string]string{"app": "real-old-storage"}

	c := k8stools.GetTestClientWithObjects(nil)
	ctx := context.Background()

	svc, err := CreateAliasService(ctx, c, "myrelease-migration-source", ns, ports, selector)
	require.NoError(t, err)
	assert.Equal(t, selector, svc.Spec.Selector)
	assert.Equal(t, ports, svc.Spec.Ports)
	// deliberately no Helm labels, so a later Discover/DiscoverComponent call never picks
	// this Service up as if it were part of the old release.
	assert.Empty(t, svc.Labels)

	var check corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "myrelease-migration-source", Namespace: ns}, &check))
	assert.Equal(t, selector, check.Spec.Selector)

	// re-running against an already-created alias Service is a no-op, not an error, and
	// returns the existing object (idempotent for re-runs after a partial failure).
	svc2, err := CreateAliasService(ctx, c, "myrelease-migration-source", ns, ports, selector)
	require.NoError(t, err)
	assert.Equal(t, svc.Name, svc2.Name)
}

func TestCreateAliasService_SurvivesClientFacingServiceSelectorChange(t *testing.T) {
	// This is the actual bug this helper fixes: a URL built from a Service name stays valid
	// only as long as that Service's selector doesn't change. The alias Service must be a
	// *different* object from the client-facing Service that gets redirected, so it keeps
	// pointing at the real backend even after the client-facing one no longer does.
	ns := "default"
	realBackendSelector := map[string]string{"app": "real-old-storage"}
	ports := []corev1.ServicePort{{Name: "http", Port: 8428}}

	clientFacingSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: ns},
		Spec:       corev1.ServiceSpec{Selector: realBackendSelector, Ports: ports},
	}

	c := k8stools.GetTestClientWithObjects(nil)
	ctx := context.Background()
	require.NoError(t, c.Create(ctx, clientFacingSvc))

	aliasSvc, err := CreateAliasService(ctx, c, "myrelease-migration-source", ns, clientFacingSvc.Spec.Ports, clientFacingSvc.Spec.Selector)
	require.NoError(t, err)

	// simulate cutover: the client-facing Service now selects the buffer agent instead.
	clientFacingSvc.Spec.Selector = map[string]string{"app": "buffer-agent"}
	require.NoError(t, c.Update(ctx, clientFacingSvc))

	// the alias must be unaffected — it's a separate object.
	var check corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: aliasSvc.Name, Namespace: ns}, &check))
	assert.Equal(t, realBackendSelector, check.Spec.Selector)
}
