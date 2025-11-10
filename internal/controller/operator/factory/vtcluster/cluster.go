package vtcluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// CreateOrUpdate syncs VTCluster object to the desired state
func CreateOrUpdate(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster) error {
	var prevCR *vmv1.VTCluster
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if cr.IsOwnsServiceAccount() {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
		sa := build.ServiceAccount(b)
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentRoot)
			prevSA = build.ServiceAccount(b)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, sa, prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	// handle case for loadbalancing
	if cr.Spec.RequestsLoadBalancer.Enabled && cr.Spec.Storage != nil {
		if err := createOrUpdateVMAuthLB(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot reconcile vmauthlb: %w", err)
		}
	}

	if err := createOrUpdateVTStorage(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot reconcile storage: %w", err)
	}
	if err := createOrUpdateVTSelect(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot reconcile select: %w", err)
	}

	if err := createOrUpdateVTInsert(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot reconcile insert: %w", err)
	}

	if prevCR != nil {
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("failed to remove objects from previous cluster state: %w", err)
		}
	}
	return nil
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1.VTCluster) error {
	newStorage := cr.Spec.Storage
	newSelect := cr.Spec.Select
	newInsert := cr.Spec.Insert
	newLB := cr.Spec.RequestsLoadBalancer

	keepServices := make(map[string]struct{})
	keepScrapes := make(map[string]struct{})
	keepPDBs := make(map[string]struct{})
	keepHPAs := make(map[string]struct{})

	cfg := config.MustGetBaseConfig()
	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation

	if newStorage == nil {
		if err := finalize.OnStorageDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot remove orphaned storage resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
		if newStorage.PodDisruptionBudget != nil {
			keepPDBs[commonName] = struct{}{}
		}
		if !ptr.Deref(newStorage.DisableSelfServiceScrape, disableSelfScrape) {
			keepScrapes[commonName] = struct{}{}
		}
		keepServices[commonName] = struct{}{}
		if newStorage.ServiceSpec != nil && !newStorage.ServiceSpec.UseAsDefault {
			extraSvcName := newStorage.ServiceSpec.NameOrDefault(commonName)
			keepServices[extraSvcName] = struct{}{}
		}
	}

	if newSelect == nil {
		if err := finalize.OnSelectDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot remove orphaned select resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
		if newSelect.PodDisruptionBudget != nil {
			keepPDBs[commonName] = struct{}{}
		}
		if newSelect.HPA == nil {
			keepHPAs[commonName] = struct{}{}
		}
		keepServices[commonName] = struct{}{}
		if newSelect.ServiceSpec != nil && !newSelect.ServiceSpec.UseAsDefault {
			extraSvcName := newSelect.ServiceSpec.NameOrDefault(commonName)
			keepServices[extraSvcName] = struct{}{}
		}
		scrapeName := commonName
		if newLB.Enabled && !newLB.DisableSelectBalancing {
			scrapeName = cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect)
			keepServices[scrapeName] = struct{}{}
		}
		if !ptr.Deref(newSelect.DisableSelfServiceScrape, disableSelfScrape) {
			keepScrapes[scrapeName] = struct{}{}
		}
	}

	if newInsert == nil {
		if err := finalize.OnInsertDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot remove orphaned insert resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
		if newInsert.PodDisruptionBudget != nil {
			keepPDBs[commonName] = struct{}{}
		}
		if newInsert.HPA == nil {
			keepHPAs[commonName] = struct{}{}
		}
		keepServices[commonName] = struct{}{}
		if newInsert.ServiceSpec != nil && !newInsert.ServiceSpec.UseAsDefault {
			extraSvcName := newInsert.ServiceSpec.NameOrDefault(commonName)
			keepServices[extraSvcName] = struct{}{}
		}
		scrapeName := commonName
		if newLB.Enabled && !newLB.DisableInsertBalancing {
			scrapeName = cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
			keepServices[scrapeName] = struct{}{}
		}
		if !ptr.Deref(newInsert.DisableSelfServiceScrape, disableSelfScrape) {
			keepScrapes[scrapeName] = struct{}{}
		}
	}
	if newLB.Enabled {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentBalancer)
		if newLB.Spec.PodDisruptionBudget != nil {
			keepPDBs[commonName] = struct{}{}
		}
		if !ptr.Deref(newLB.Spec.DisableSelfServiceScrape, disableSelfScrape) {
			keepScrapes[commonName] = struct{}{}
		}
		keepServices[commonName] = struct{}{}
		if newLB.Spec.AdditionalServiceSpec != nil && !newLB.Spec.AdditionalServiceSpec.UseAsDefault {
			extraSvcName := newLB.Spec.AdditionalServiceSpec.NameOrDefault(commonName)
			keepServices[extraSvcName] = struct{}{}
		}
	} else {
		if err := finalize.OnClusterLoadBalancerDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot remove orphaned loadbalancer components: %w", err)
		}
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
	ls := b.SelectorLabels()
	delete(ls, "app.kubernetes.io/name")
	b.SetSelectorLabels(ls)
	if err := finalize.RemoveOrphanedPDBs(ctx, rclient, b, keepPDBs); err != nil {
		return fmt.Errorf("cannot remove orphaned PDBs: %w", err)
	}
	if err := finalize.RemoveOrphanedHPAs(ctx, rclient, b, keepHPAs); err != nil {
		return fmt.Errorf("cannot remove orphaned HPAs: %w", err)
	}
	if err := finalize.RemoveOrphanedVMServiceScrapes(ctx, rclient, b, keepScrapes); err != nil {
		return fmt.Errorf("cannot remove orphaned vmservicescrapes: %w", err)
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, b, keepServices); err != nil {
		return fmt.Errorf("cannot remove orphaned services: %w", err)
	}
	return nil
}
