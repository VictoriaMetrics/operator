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

	svc, err := createAliasService(ctx, c, "myrelease-migration-source", ns, ports, selector)
	require.NoError(t, err)
	assert.Equal(t, selector, svc.Spec.Selector)
	assert.Equal(t, ports, svc.Spec.Ports)
	assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
	assert.Equal(t, map[string]string{aliasOwnershipLabel: "true"}, svc.Labels)

	var check corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "myrelease-migration-source", Namespace: ns}, &check))
	assert.Equal(t, selector, check.Spec.Selector)

	svc2, err := createAliasService(ctx, c, "myrelease-migration-source", ns, ports, selector)
	require.NoError(t, err)
	assert.Equal(t, svc.Name, svc2.Name)
}

func TestCreateAliasService_ClearsNodePort(t *testing.T) {
	ns := "default"
	ports := []corev1.ServicePort{{Name: "http", Port: 8428, NodePort: 30080}}
	selector := map[string]string{"app": "real-old-storage"}

	c := k8stools.GetTestClientWithObjects(nil)
	ctx := context.Background()

	svc, err := createAliasService(ctx, c, "myrelease-migration-source", ns, ports, selector)
	require.NoError(t, err)
	require.Len(t, svc.Spec.Ports, 1)
	assert.Zero(t, svc.Spec.Ports[0].NodePort)
	assert.Equal(t, int32(30080), ports[0].NodePort)
}

func TestCreateAliasService_MismatchedExistingServiceIsAnError(t *testing.T) {
	ns := "default"
	ports := []corev1.ServicePort{{Name: "http", Port: 8428}}
	selector := map[string]string{"app": "real-old-storage"}

	c := k8stools.GetTestClientWithObjects(nil)
	ctx := context.Background()

	unrelated := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease-migration-source", Namespace: ns},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "something-else-entirely"}, Ports: ports},
	}
	require.NoError(t, c.Create(ctx, unrelated))

	_, err := createAliasService(ctx, c, "myrelease-migration-source", ns, ports, selector)
	assert.Error(t, err)
}

func TestCreateAliasService_UnownedMatchingServiceIsAnError(t *testing.T) {
	ns := "default"
	ports := []corev1.ServicePort{{Name: "http", Port: 8428}}
	selector := map[string]string{"app": "real-old-storage"}

	c := k8stools.GetTestClientWithObjects(nil)
	ctx := context.Background()

	unowned := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease-migration-source", Namespace: ns},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: selector, Ports: ports},
	}
	require.NoError(t, c.Create(ctx, unowned))

	_, err := createAliasService(ctx, c, "myrelease-migration-source", ns, ports, selector)
	assert.Error(t, err)
}

func TestCreateAliasService_WrongTypeExistingServiceIsAnError(t *testing.T) {
	ns := "default"
	ports := []corev1.ServicePort{{Name: "http", Port: 8428}}
	selector := map[string]string{"app": "real-old-storage"}

	c := k8stools.GetTestClientWithObjects(nil)
	ctx := context.Background()

	wrongType := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease-migration-source", Namespace: ns, Labels: map[string]string{aliasOwnershipLabel: "true"}},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Selector: selector, Ports: ports},
	}
	require.NoError(t, c.Create(ctx, wrongType))

	_, err := createAliasService(ctx, c, "myrelease-migration-source", ns, ports, selector)
	assert.Error(t, err)
}

func TestCreateAliasService_SurvivesClientFacingServiceSelectorChange(t *testing.T) {
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

	aliasSvc, err := createAliasService(ctx, c, "myrelease-migration-source", ns, clientFacingSvc.Spec.Ports, clientFacingSvc.Spec.Selector)
	require.NoError(t, err)

	// simulate cutover: the client-facing Service now selects the buffer agent instead.
	clientFacingSvc.Spec.Selector = map[string]string{"app": "buffer-agent"}
	require.NoError(t, c.Update(ctx, clientFacingSvc))

	// the alias must be unaffected — it's a separate object.
	var check corev1.Service
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: aliasSvc.Name, Namespace: ns}, &check))
	assert.Equal(t, realBackendSelector, check.Spec.Selector)
}
