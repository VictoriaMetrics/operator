package vlcluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// CreateOrUpdate syncs VLCluster object to the desired state
func CreateOrUpdate(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) error {
	if !build.MustSkipRuntimeValidation() {
		if err := cr.Validate(); err != nil {
			return err
		}
	}
	var prevCR *vmv1.VLCluster
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
	}
	cfg := config.MustGetBaseConfig()
	if !cfg.VPAAPIEnabled {
		if cr.Spec.VLStorage != nil && cr.Spec.VLStorage.VPA != nil {
			return fmt.Errorf("spec.vlstorage.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
		}
		if cr.Spec.VLSelect != nil && cr.Spec.VLSelect.VPA != nil {
			return fmt.Errorf("spec.vlselect.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
		}
		if cr.Spec.VLInsert != nil && cr.Spec.VLInsert.VPA != nil {
			return fmt.Errorf("spec.vlinsert.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
		}
	}
	if cr.IsOwnsServiceAccount() {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
		sa := build.ServiceAccount(b)
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentRoot)
			prevSA = build.ServiceAccount(b)
		}
		owner := cr.AsOwner()
		if err := reconcile.ServiceAccount(ctx, rclient, sa, prevSA, &owner); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	// handle case for loadbalancing
	if cr.Spec.RequestsLoadBalancer.Enabled && cr.Spec.VLStorage != nil {
		if err := createOrUpdateVMAuthLB(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot reconcile vmauthlb: %w", err)
		}
	}

	if err := createOrUpdateVLStorage(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot reconcile storage: %w", err)
	}
	if err := createOrUpdateVLSelect(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot reconcile select: %w", err)
	}

	if err := createOrUpdateVLInsert(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot reconcile insert: %w", err)
	}

	if prevCR != nil {
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("failed to remove objects from previous cluster state: %w", err)
		}
	}
	return nil
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) error {
	newStorage := cr.Spec.VLStorage
	newSelect := cr.Spec.VLSelect
	newInsert := cr.Spec.VLInsert
	newLB := cr.Spec.RequestsLoadBalancer

	cc := finalize.NewChildCleaner()

	if newStorage == nil {
		if err := finalize.OnStorageDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot remove orphaned storage resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
		if newStorage.PodDisruptionBudget != nil {
			cc.KeepPDB(commonName)
		}
		if newStorage.HPA != nil {
			cc.KeepHPA(commonName)
		}
		if newStorage.VPA != nil {
			cc.KeepVPA(commonName)
		}
		if !ptr.Deref(newStorage.DisableSelfServiceScrape, false) {
			cc.KeepScrape(commonName)
		}
		cc.KeepService(commonName)
		if newStorage.ServiceSpec != nil && !newStorage.ServiceSpec.UseAsDefault {
			cc.KeepService(newStorage.ServiceSpec.NameOrDefault(commonName))
		}
	}

	if newSelect == nil {
		if err := finalize.OnSelectDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot remove orphaned select resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
		if newSelect.PodDisruptionBudget != nil {
			cc.KeepPDB(commonName)
		}
		if newSelect.HPA != nil {
			cc.KeepHPA(commonName)
		}
		if newSelect.VPA != nil {
			cc.KeepVPA(commonName)
		}
		cc.KeepService(commonName)
		if newSelect.ServiceSpec != nil && !newSelect.ServiceSpec.UseAsDefault {
			cc.KeepService(newSelect.ServiceSpec.NameOrDefault(commonName))
		}
		scrapeName := commonName
		if newLB.Enabled && !newLB.DisableSelectBalancing {
			scrapeName = cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect)
			cc.KeepService(scrapeName)
		}
		if !ptr.Deref(newSelect.DisableSelfServiceScrape, false) {
			cc.KeepScrape(scrapeName)
		}
	}

	if newInsert == nil {
		if err := finalize.OnInsertDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot remove orphaned insert resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
		if newInsert.PodDisruptionBudget != nil {
			cc.KeepPDB(commonName)
		}
		if newInsert.HPA != nil {
			cc.KeepHPA(commonName)
		}
		if newInsert.VPA != nil {
			cc.KeepVPA(commonName)
		}
		cc.KeepService(commonName)
		if newInsert.ServiceSpec != nil && !newInsert.ServiceSpec.UseAsDefault {
			cc.KeepService(newInsert.ServiceSpec.NameOrDefault(commonName))
		}
		scrapeName := commonName
		if newLB.Enabled && !newLB.DisableInsertBalancing {
			scrapeName = cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
			cc.KeepService(scrapeName)
		}
		if !ptr.Deref(newInsert.DisableSelfServiceScrape, false) {
			cc.KeepScrape(scrapeName)
		}
	}
	if newLB.Enabled {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentBalancer)
		if newLB.Spec.PodDisruptionBudget != nil {
			cc.KeepPDB(commonName)
		}
		if !ptr.Deref(newLB.Spec.DisableSelfServiceScrape, false) {
			cc.KeepScrape(commonName)
		}
		cc.KeepService(commonName)
		if newLB.Spec.AdditionalServiceSpec != nil && !newLB.Spec.AdditionalServiceSpec.UseAsDefault {
			cc.KeepService(newLB.Spec.AdditionalServiceSpec.NameOrDefault(commonName))
		}
	} else {
		if err := finalize.OnClusterLoadBalancerDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot remove orphaned loadbalancer components: %w", err)
		}
	}
	if !cr.IsOwnsServiceAccount() {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
		owner := cr.AsOwner()
		objMeta := metav1.ObjectMeta{Name: b.PrefixedName(), Namespace: b.GetNamespace()}
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.ServiceAccount{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceaccount: %w", err)
		}
	}
	return cc.RemoveOrphaned(ctx, rclient, cr)
}
