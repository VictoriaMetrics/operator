package migrate

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// aliasOwnershipLabel marks a Service as one CreateAliasService itself created, so a
// same-named Service that merely happens to match on selector/ports is never mistaken for one.
const aliasOwnershipLabel = "migrate.victoriametrics.com/alias"

// CreateAliasService creates a Service mirroring ports under selector, giving NoDowntime a
// stable path to a backend that survives a later selector change on the client-facing
// Service (which would otherwise end up resolving back to itself post-cutover). Carries no
// Helm labels, so Discover/DiscoverComponent never pick it up.
func CreateAliasService(ctx context.Context, c client.Client, name, namespace string, ports []corev1.ServicePort, selector map[string]string) (*corev1.Service, error) {
	aliasPorts := make([]corev1.ServicePort, len(ports))
	for i, p := range ports {
		p.NodePort = 0
		aliasPorts[i] = p
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{aliasOwnershipLabel: "true"},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selector,
			Ports:    aliasPorts,
		},
	}
	if err := c.Create(ctx, svc); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("cannot create alias Service %s/%s: %w", namespace, name, err)
		}
		var existing corev1.Service
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &existing); err != nil {
			return nil, fmt.Errorf("cannot re-fetch existing alias Service %s/%s: %w", namespace, name, err)
		}
		if existing.Labels[aliasOwnershipLabel] != "true" {
			return nil, fmt.Errorf("a Service %s/%s already exists but wasn't created by this tool — "+
				"refusing to reuse an unrelated Service as the migration alias; delete it manually before retrying", namespace, name)
		}
		if existing.Spec.Type != corev1.ServiceTypeClusterIP {
			return nil, fmt.Errorf("existing alias Service %s/%s has type %q, expected %q — refusing to reuse it",
				namespace, name, existing.Spec.Type, corev1.ServiceTypeClusterIP)
		}
		if !reflect.DeepEqual(existing.Spec.Selector, selector) || !servicePortsEqual(existing.Spec.Ports, aliasPorts) {
			return nil, fmt.Errorf("a Service %s/%s already exists but its selector/ports don't match the backend this migration expects — "+
				"it may be left over from a previous migration attempt against a different backend; "+
				"delete it manually before retrying, since routing buffered writes through it would send them to the wrong place", namespace, name)
		}
		svc = &existing
	}
	return svc, nil
}

// servicePortsEqual compares the fields that determine where traffic actually goes, ignoring
// NodePort, which the API server assigns and this alias (always ClusterIP) never sets.
func servicePortsEqual(a, b []corev1.ServicePort) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Port != b[i].Port || a[i].TargetPort != b[i].TargetPort ||
			a[i].Protocol != b[i].Protocol || !appProtocolEqual(a[i].AppProtocol, b[i].AppProtocol) {
			return false
		}
	}
	return true
}

func appProtocolEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
