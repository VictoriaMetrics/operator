package migrate

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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
	sel := labels.SelectorFromSet(helmLabels(releaseName))
	opts := &client.ListOptions{Namespace: namespace, LabelSelector: sel}

	var allLabels []map[string]string
	for _, l := range []struct {
		list client.ObjectList
		kind string
	}{
		{&appsv1.DeploymentList{}, "deployments"},
		{&appsv1.StatefulSetList{}, "statefulsets"},
		{&corev1.ServiceList{}, "services"},
		{&corev1.ConfigMapList{}, "configmaps"},
		{&corev1.SecretList{}, "secrets"},
		{&corev1.PersistentVolumeClaimList{}, "persistentvolumeclaims"},
	} {
		if err := c.List(ctx, l.list, opts); err != nil {
			return "", fmt.Errorf("cannot list %s for release %q: %w", l.kind, releaseName, err)
		}
		items, err := apimeta.ExtractList(l.list)
		if err != nil {
			return "", fmt.Errorf("cannot extract %s items: %w", l.kind, err)
		}
		for _, it := range items {
			allLabels = append(allLabels, it.(client.Object).GetLabels())
		}
	}
	if len(allLabels) == 0 {
		return "", fmt.Errorf("no resources found for release %q in namespace %q to determine its chart version", releaseName, namespace)
	}
	var chartLabel string
	for _, labels := range allLabels {
		v, ok := labels["helm.sh/chart"]
		if !ok {
			continue
		}
		if chartLabel == "" {
			chartLabel = v
		} else if v != chartLabel {
			return "", fmt.Errorf("release %q's resources disagree on their helm.sh/chart label (%q vs %q) — likely a partially-applied Helm upgrade; resolve it or pass -chart-version explicitly", releaseName, chartLabel, v)
		}
	}
	if chartLabel == "" {
		return "", fmt.Errorf("release %q has no helm.sh/chart label on any discovered resource — cannot determine its installed chart version", releaseName)
	}
	version := strings.TrimPrefix(chartLabel, chart+"-")
	if version == chartLabel {
		return "", fmt.Errorf("helm.sh/chart label %q on release %q doesn't match expected chart %q", chartLabel, releaseName, chart)
	}
	return strings.ReplaceAll(version, "_", "+"), nil
}

func discoverComponent(ctx context.Context, c client.Client, namespace, releaseName, component string) (*Discovered, error) {
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
	if len(statefulSets.Items) > 0 {
		var allPVCs corev1.PersistentVolumeClaimList
		if err := c.List(ctx, &allPVCs, client.InNamespace(namespace)); err != nil {
			return nil, fmt.Errorf("cannot list all persistentvolumeclaims in namespace %q: %w", namespace, err)
		}
		for i := range statefulSets.Items {
			sts := &statefulSets.Items[i]
			prefixes := statefulSetPVCPrefixes(sts)
			for _, pvc := range allPVCs.Items {
				if _, ok := pvcByName[pvc.Name]; ok {
					continue
				}
				if matchesStatefulSetPVC(&pvc, sts, prefixes) {
					pvcByName[pvc.Name] = pvc
				}
			}
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

func statefulSetPVCPrefixes(sts *appsv1.StatefulSet) []string {
	prefixes := make([]string, 0, len(sts.Spec.VolumeClaimTemplates))
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		prefixes = append(prefixes, vct.Name+"-"+sts.Name+"-")
	}
	return prefixes
}

func matchesStatefulSetPVC(pvc *corev1.PersistentVolumeClaim, sts *appsv1.StatefulSet, prefixes []string) bool {
	for _, ref := range pvc.OwnerReferences {
		if ref.UID == sts.UID {
			return true
		}
	}
	if len(pvc.OwnerReferences) > 0 {
		return false
	}
	for _, prefix := range prefixes {
		if suffix, ok := strings.CutPrefix(pvc.Name, prefix); ok {
			if _, err := strconv.Atoi(suffix); err == nil {
				return true
			}
		}
	}
	return false
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
// discoverComponent), erroring if none or more than one is found.
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
