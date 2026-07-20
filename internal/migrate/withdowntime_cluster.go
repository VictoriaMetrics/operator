package migrate

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterComponentSpec describes one component of a multi-component cluster chart (e.g.
// VMCluster's vmstorage/vmselect/vminsert) for WithDowntimeCluster's per-component handling.
// The vm/vl packages precompute every target-CR-specific detail here.
type ClusterComponentSpec struct {
	// HelmComponentLabel is the old chart's app.kubernetes.io/component label value.
	HelmComponentLabel string
	// TargetWorkloadName is the target CR's StatefulSet/Deployment name for this component.
	TargetWorkloadName string
	// TargetPVCTemplateName is the operator's volumeClaimTemplate name for this component's
	// PVCs, or "" if it has no persistent storage configured on the target CR.
	TargetPVCTemplateName string
	// TargetSelectorLabels are the target CR's pod selector labels, used to cut over this
	// component's Service(s) once ready.
	TargetSelectorLabels map[string]string
}

// WithDowntimeCluster runs the WithDowntime strategy for a multi-component cluster chart: for
// each component, delete the old workload and rebind its per-ordinal PVCs under the target
// CR's naming, then create the target CR and repoint each component's Service(s).
func WithDowntimeCluster(ctx context.Context, c client.Client, opts Options, target operationalCR, components []ClusterComponentSpec) error {
	type discoveredComponent struct {
		spec ClusterComponentSpec
		d    *Discovered
	}
	discovered := make([]discoveredComponent, 0, len(components))
	for _, comp := range components {
		d, err := DiscoverComponent(ctx, c, opts.Namespace, opts.ReleaseName, comp.HelmComponentLabel)
		if err != nil {
			return fmt.Errorf("discovery failed for component %q: %w", comp.HelmComponentLabel, err)
		}
		if d.Deployment == nil && len(d.StatefulSets) == 0 {
			return fmt.Errorf("no Deployment or StatefulSet found for component %q in namespace %q", comp.HelmComponentLabel, opts.Namespace)
		}
		discovered = append(discovered, discoveredComponent{comp, d})
	}

	fmt.Printf("plan: for each of %d cluster components, delete the old workload and rebind any PVCs under the operator's naming, "+
		"then create %s %s/%s and repoint each component's Service(s) at the new pods\n",
		len(discovered), target.GetObjectKind().GroupVersionKind().Kind, target.GetNamespace(), target.GetName())

	if opts.DryRun {
		fmt.Println("dry-run: stopping before any mutation")
		return nil
	}
	if !opts.Yes {
		if !Confirm("proceed with the above plan?") {
			return fmt.Errorf("aborted by user")
		}
	}

	for _, dc := range discovered {
		comp, d := dc.spec, dc.d
		switch {
		case d.Deployment != nil:
			dependentConfigMaps, dependentSecrets := dependentConfigsOf(&d.Deployment.Spec.Template.Spec, d.ConfigMaps, d.Secrets)
			if err := c.Delete(ctx, d.Deployment); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("cannot delete old Deployment for component %q: %w", comp.HelmComponentLabel, err)
			}
			if err := deleteDependentConfigs(ctx, c, dependentConfigMaps, dependentSecrets); err != nil {
				return err
			}
		default:
			sts, err := d.SingleStatefulSet()
			if err != nil {
				return fmt.Errorf("component %q: %w", comp.HelmComponentLabel, err)
			}
			dependentConfigMaps, dependentSecrets := dependentConfigsOf(&sts.Spec.Template.Spec, d.ConfigMaps, d.Secrets)
			if err := c.Delete(ctx, sts); err != nil && !k8serrors.IsNotFound(err) {
				return fmt.Errorf("cannot delete old StatefulSet for component %q: %w", comp.HelmComponentLabel, err)
			}
			if err := deleteDependentConfigs(ctx, c, dependentConfigMaps, dependentSecrets); err != nil {
				return err
			}
			if len(d.PVCs) == 0 {
				continue
			}
			if comp.TargetPVCTemplateName == "" {
				return fmt.Errorf("component %q has %d existing PVC(s) but the target CR has no storage configured for it — refusing to proceed and orphan data", comp.HelmComponentLabel, len(d.PVCs))
			}
			if err := rebindClusterPVCs(ctx, c, d.PVCs, comp.TargetPVCTemplateName, comp.TargetWorkloadName, target.GetNamespace()); err != nil {
				return fmt.Errorf("component %q: %w", comp.HelmComponentLabel, err)
			}
		}
	}

	if err := c.Create(ctx, target); err != nil {
		return fmt.Errorf("cannot create target CR %s/%s: %w", target.GetNamespace(), target.GetName(), err)
	}
	if err := WaitForOperational(ctx, c, target); err != nil {
		return fmt.Errorf("target CR did not become ready: %w", err)
	}

	for _, dc := range discovered {
		svcPtrs := make([]*corev1.Service, len(dc.d.Services))
		for i := range dc.d.Services {
			svcPtrs[i] = &dc.d.Services[i]
		}
		if err := CutoverServices(ctx, c, svcPtrs, dc.spec.TargetSelectorLabels); err != nil {
			return fmt.Errorf("traffic cutover failed for component %q: %w", dc.spec.HelmComponentLabel, err)
		}
	}

	fmt.Println("migration complete")
	return nil
}

// rebindClusterPVCs rebinds each of a StatefulSet component's per-ordinal PVCs to
// <targetPVCTemplateName>-<targetWorkloadName>-<ordinal>. Requires contiguous ordinals from
// 0, since a gap would mean the discovered set isn't one coherent StatefulSet.
func rebindClusterPVCs(ctx context.Context, c client.Client, pvcs []corev1.PersistentVolumeClaim, targetPVCTemplateName, targetWorkloadName, targetNamespace string) error {
	byOrdinal, err := pvcsByOrdinal(pvcs)
	if err != nil {
		return err
	}
	for i := 0; i < len(byOrdinal); i++ {
		pvc, ok := byOrdinal[i]
		if !ok {
			return fmt.Errorf("PVC ordinals are not contiguous from 0 (missing ordinal %d among %d discovered PVCs) — refusing to guess a partial rebind", i, len(pvcs))
		}
		targetName := fmt.Sprintf("%s-%s-%d", targetPVCTemplateName, targetWorkloadName, i)
		if _, err := RebindPVC(ctx, c, pvc, targetName, targetNamespace); err != nil {
			return fmt.Errorf("cannot rebind PVC %q (ordinal %d): %w", pvc.Name, i, err)
		}
	}
	return nil
}

// pvcsByOrdinal parses each PVC's trailing "-<ordinal>" suffix into a map keyed by ordinal.
func pvcsByOrdinal(pvcs []corev1.PersistentVolumeClaim) (map[int]*corev1.PersistentVolumeClaim, error) {
	byOrdinal := make(map[int]*corev1.PersistentVolumeClaim, len(pvcs))
	for i := range pvcs {
		pvc := &pvcs[i]
		idx := strings.LastIndex(pvc.Name, "-")
		if idx < 0 {
			return nil, fmt.Errorf("PVC %q doesn't look like a StatefulSet-managed PVC (no ordinal suffix)", pvc.Name)
		}
		ordinal, err := strconv.Atoi(pvc.Name[idx+1:])
		if err != nil {
			return nil, fmt.Errorf("cannot parse ordinal from PVC %q: %w", pvc.Name, err)
		}
		byOrdinal[ordinal] = pvc
	}
	return byOrdinal, nil
}
