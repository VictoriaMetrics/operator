package migrate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// DiscoverChartVersion returns the Helm chart version actually installed for releaseName, read
// from the "helm.sh/chart" label ("<chart>-<version>") Helm renders onto every resource it
// creates.
func DiscoverChartVersion(ctx context.Context, c client.Client, namespace, releaseName, chart string) (string, error) {
	d, err := Discover(ctx, c, namespace, releaseName)
	if err != nil {
		return "", fmt.Errorf("cannot discover release resources to determine its chart version: %w", err)
	}
	var chartLabels map[string]string
	switch {
	case d.Deployment != nil:
		chartLabels = d.Deployment.Labels
	case len(d.StatefulSets) > 0:
		chartLabels = d.StatefulSets[0].Labels
	case len(d.Services) > 0:
		chartLabels = d.Services[0].Labels
	default:
		return "", fmt.Errorf("no resources found for release %q in namespace %q to determine its chart version", releaseName, namespace)
	}
	chartLabel, ok := chartLabels["helm.sh/chart"]
	if !ok {
		return "", fmt.Errorf("release %q has no helm.sh/chart label — cannot determine its installed chart version", releaseName)
	}
	version := strings.TrimPrefix(chartLabel, chart+"-")
	if version == chartLabel {
		return "", fmt.Errorf("helm.sh/chart label %q on release %q doesn't match expected chart %q", chartLabel, releaseName, chart)
	}
	return version, nil
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
	pvcByName := make(map[string]corev1.PersistentVolumeClaim, len(pvcs.Items))
	for _, pvc := range pvcs.Items {
		pvcByName[pvc.Name] = pvc
	}
	for i := range statefulSets.Items {
		sts := &statefulSets.Items[i]
		for _, name := range statefulSetPVCNames(sts) {
			if _, ok := pvcByName[name]; ok {
				continue
			}
			var pvc corev1.PersistentVolumeClaim
			if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pvc); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return nil, fmt.Errorf("cannot get persistentvolumeclaim %q owned by statefulset %q: %w", name, sts.Name, err)
			}
			pvcByName[pvc.Name] = pvc
		}
	}
	allPVCs := make([]corev1.PersistentVolumeClaim, 0, len(pvcByName))
	for _, pvc := range pvcByName {
		allPVCs = append(allPVCs, pvc)
	}
	sort.Slice(allPVCs, func(i, j int) bool { return allPVCs[i].Name < allPVCs[j].Name })

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
		PVCs:         allPVCs,
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

// statefulSetPVCNames returns the names Kubernetes assigns to every PVC a StatefulSet's
// volumeClaimTemplates produce: one per (claim template, ordinal) pair, named
// "<claimTemplateName>-<statefulSetName>-<ordinal>". Replicas defaults to 1, matching the
// StatefulSet API's own defaulting, since the fake client used in tests doesn't apply it.
func statefulSetPVCNames(sts *appsv1.StatefulSet) []string {
	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}
	names := make([]string, 0, len(sts.Spec.VolumeClaimTemplates)*int(replicas))
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		for ordinal := int32(0); ordinal < replicas; ordinal++ {
			names = append(names, fmt.Sprintf("%s-%s-%d", vct.Name, sts.Name, ordinal))
		}
	}
	return names
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
