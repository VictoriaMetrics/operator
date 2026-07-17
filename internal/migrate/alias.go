package migrate

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateAliasService creates a Service mirroring ports under selector, giving NoDowntime a
// stable path to a backend that survives a later selector change on the client-facing
// Service (which would otherwise end up resolving back to itself post-cutover). Carries no
// Helm labels, so Discover/DiscoverComponent never pick it up.
func CreateAliasService(ctx context.Context, c client.Client, name, namespace string, ports []corev1.ServicePort, selector map[string]string) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    ports,
		},
	}
	if err := c.Create(ctx, svc); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("cannot create alias Service %s/%s: %w", namespace, name, err)
		}
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, svc); err != nil {
			return nil, fmt.Errorf("cannot re-fetch existing alias Service %s/%s: %w", namespace, name, err)
		}
	}
	return svc, nil
}
