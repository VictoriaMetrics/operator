package migrate

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Discovered captures the old Helm release's resources, found by label rather than assumed
// naming, so callers can rebind/repoint them by identity without ever guessing a name.
type Discovered struct {
	// Deployment is set for single-node charts, which use a Deployment + standalone PVC.
	Deployment *appsv1.Deployment
	// StatefulSets is set for cluster charts, one entry per component.
	StatefulSets []appsv1.StatefulSet
	Services     []corev1.Service
	PVCs         []corev1.PersistentVolumeClaim
	ConfigMaps   []corev1.ConfigMap
	Secrets      []corev1.Secret
}

// Discover finds all resources belonging to the given Helm release in namespace.
func Discover(ctx context.Context, c client.Client, namespace, releaseName string) (*Discovered, error) {
	return discover(ctx, c, namespace, releaseName, helmLabels(releaseName))
}

// DiscoverComponent finds one component's resources (e.g. "vmstorage") of a multi-component
// release, via the app.kubernetes.io/component label the VictoriaMetrics Helm charts set.
func DiscoverComponent(ctx context.Context, c client.Client, namespace, releaseName, component string) (*Discovered, error) {
	l := helmLabels(releaseName)
	l["app.kubernetes.io/component"] = component
	return discover(ctx, c, namespace, releaseName, l)
}

func discover(ctx context.Context, c client.Client, namespace, releaseName string, matchLabels map[string]string) (*Discovered, error) {
	sel := labels.SelectorFromSet(matchLabels)
	opts := &client.ListOptions{Namespace: namespace, LabelSelector: sel}

	var deployments appsv1.DeploymentList
	if err := c.List(ctx, &deployments, opts); err != nil {
		return nil, fmt.Errorf("cannot list deployments for release %q: %w", releaseName, err)
	}
	var statefulSets appsv1.StatefulSetList
	if err := c.List(ctx, &statefulSets, opts); err != nil {
		return nil, fmt.Errorf("cannot list statefulsets for release %q: %w", releaseName, err)
	}
	var services corev1.ServiceList
	if err := c.List(ctx, &services, opts); err != nil {
		return nil, fmt.Errorf("cannot list services for release %q: %w", releaseName, err)
	}
	var pvcs corev1.PersistentVolumeClaimList
	if err := c.List(ctx, &pvcs, opts); err != nil {
		return nil, fmt.Errorf("cannot list persistentvolumeclaims for release %q: %w", releaseName, err)
	}
	var configMaps corev1.ConfigMapList
	if err := c.List(ctx, &configMaps, opts); err != nil {
		return nil, fmt.Errorf("cannot list configmaps for release %q: %w", releaseName, err)
	}
	var secrets corev1.SecretList
	if err := c.List(ctx, &secrets, opts); err != nil {
		return nil, fmt.Errorf("cannot list secrets for release %q: %w", releaseName, err)
	}

	d := &Discovered{
		StatefulSets: statefulSets.Items,
		Services:     services.Items,
		PVCs:         pvcs.Items,
		ConfigMaps:   configMaps.Items,
		Secrets:      secrets.Items,
	}
	switch len(deployments.Items) {
	case 0:
	case 1:
		d.Deployment = &deployments.Items[0]
	default:
		return nil, fmt.Errorf("found %d deployments for release %q, expected at most 1 — this chart shape is not supported", len(deployments.Items), releaseName)
	}
	return d, nil
}

// SingleNodePVC returns the standalone PVC used by a single-node chart's Deployment, erroring
// if none or more than one is found (a single-node release should have exactly one data PVC).
func (d *Discovered) SingleNodePVC() (*corev1.PersistentVolumeClaim, error) {
	switch len(d.PVCs) {
	case 0:
		return nil, fmt.Errorf("no PersistentVolumeClaim found for this release")
	case 1:
		return &d.PVCs[0], nil
	default:
		return nil, fmt.Errorf("found %d PersistentVolumeClaims for this release, expected exactly 1 for a single-node chart", len(d.PVCs))
	}
}

// SingleStatefulSet returns the one StatefulSet backing a cluster component (discovered via
// DiscoverComponent), erroring if none or more than one is found.
func (d *Discovered) SingleStatefulSet() (*appsv1.StatefulSet, error) {
	switch len(d.StatefulSets) {
	case 0:
		return nil, fmt.Errorf("no StatefulSet found for this component")
	case 1:
		return &d.StatefulSets[0], nil
	default:
		return nil, fmt.Errorf("found %d StatefulSets for this component, expected exactly 1", len(d.StatefulSets))
	}
}
