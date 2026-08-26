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

const aliasOwnershipLabel = "migrate.victoriametrics.com/alias"

func createAliasService(ctx context.Context, c client.Client, name, namespace string, source *corev1.Service, selector map[string]string) (*corev1.Service, error) {
	if len(source.Spec.ExternalIPs) > 0 {
		return nil, fmt.Errorf("source Service %s/%s uses externalIPs (%v), which can't be safely mirrored onto a read alias — both Services would claim the same IP with undefined routing; repoint external clients at the target's own Service once ready instead",
			source.Namespace, source.Name, source.Spec.ExternalIPs)
	}
	aliasPorts := make([]corev1.ServicePort, len(source.Spec.Ports))
	for i, p := range source.Spec.Ports {
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
			Type:                          source.Spec.Type,
			ClusterIP:                     headlessClusterIP(source),
			Selector:                      selector,
			Ports:                         aliasPorts,
			SessionAffinity:               source.Spec.SessionAffinity,
			SessionAffinityConfig:         source.Spec.SessionAffinityConfig,
			IPFamilyPolicy:                source.Spec.IPFamilyPolicy,
			IPFamilies:                    source.Spec.IPFamilies,
			InternalTrafficPolicy:         source.Spec.InternalTrafficPolicy,
			PublishNotReadyAddresses:      source.Spec.PublishNotReadyAddresses,
			ExternalTrafficPolicy:         source.Spec.ExternalTrafficPolicy,
			LoadBalancerSourceRanges:      source.Spec.LoadBalancerSourceRanges,
			LoadBalancerClass:             source.Spec.LoadBalancerClass,
			AllocateLoadBalancerNodePorts: source.Spec.AllocateLoadBalancerNodePorts,
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
		if existing.Spec.Type != source.Spec.Type {
			return nil, fmt.Errorf("existing alias Service %s/%s has type %q, expected %q (matching the source Service's own exposure) — refusing to reuse it",
				namespace, name, existing.Spec.Type, source.Spec.Type)
		}
		if (existing.Spec.ClusterIP == corev1.ClusterIPNone) != (source.Spec.ClusterIP == corev1.ClusterIPNone) {
			return nil, fmt.Errorf("existing alias Service %s/%s's headless-ness (clusterIP=%q) doesn't match the source Service's (clusterIP=%q) — refusing to reuse it",
				namespace, name, existing.Spec.ClusterIP, source.Spec.ClusterIP)
		}
		if !reflect.DeepEqual(existing.Spec.Selector, selector) || !servicePortsEqual(existing.Spec.Ports, aliasPorts) {
			return nil, fmt.Errorf("a Service %s/%s already exists but its selector/ports don't match the backend this migration expects — "+
				"it may be left over from a previous migration attempt against a different backend; "+
				"delete it manually before retrying, since routing buffered writes through it would send them to the wrong place", namespace, name)
		}
		if existing.DeletionTimestamp != nil {
			return nil, fmt.Errorf("existing alias Service %s/%s is terminating — wait for it to finish deleting before retrying", namespace, name)
		}
		svc = &existing
	}
	return svc, nil
}

func headlessClusterIP(source *corev1.Service) string {
	if source.Spec.ClusterIP == corev1.ClusterIPNone {
		return corev1.ClusterIPNone
	}
	return ""
}

func servicePortsEqual(a, b []corev1.ServicePort) bool {
	if len(a) != len(b) {
		return false
	}
	byName := make(map[string]*corev1.ServicePort, len(a))
	for i := range a {
		byName[a[i].Name] = &a[i]
	}
	for i := range b {
		other, ok := byName[b[i].Name]
		if !ok {
			return false
		}
		if other.Port != b[i].Port || other.TargetPort != b[i].TargetPort ||
			other.Protocol != b[i].Protocol || !appProtocolEqual(other.AppProtocol, b[i].AppProtocol) {
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
