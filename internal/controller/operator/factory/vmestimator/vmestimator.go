package vmestimator

import (
	"context"
	"fmt"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// CreateOrUpdate syncs VMEstimator object to the desired state
func CreateOrUpdate(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) error {
	if cr.Paused() {
		return nil
	}
	if !build.MustSkipRuntimeValidation() {
		if err := cr.Validate(); err != nil {
			return err
		}
	}
	var prevCR *vmv1.VMEstimator
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
	}
	if !config.MustGetBaseConfig().VPAAPIEnabled {
		if cr.Spec.Single != nil && cr.Spec.Single.VPA != nil {
			return fmt.Errorf("spec.single.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
		}
		if cr.Spec.Storage != nil && cr.Spec.Storage.VPA != nil {
			return fmt.Errorf("spec.storage.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
		}
		if cr.Spec.Select != nil && cr.Spec.Select.VPA != nil {
			return fmt.Errorf("spec.select.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
		}
	}
	owner := cr.AsOwner()
	if cr.IsOwnsServiceAccount() {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
		sa := build.ServiceAccount(b)
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentRoot)
			prevSA = build.ServiceAccount(b)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, sa, prevSA, &owner); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	var configHash string
	if cr.Spec.Single != nil || cr.Spec.Storage != nil {
		var err error
		configHash, err = createOrUpdateConfig(ctx, rclient, cr, prevCR)
		if err != nil {
			return fmt.Errorf("cannot reconcile config: %w", err)
		}
	}
	if err := createOrUpdateSingle(ctx, rclient, cr, prevCR, configHash); err != nil {
		return fmt.Errorf("cannot reconcile single: %w", err)
	}
	if err := createOrUpdateStorage(ctx, rclient, cr, prevCR, configHash); err != nil {
		return fmt.Errorf("cannot reconcile storage: %w", err)
	}
	if err := createOrUpdateSelect(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot reconcile select: %w", err)
	}
	if prevCR != nil {
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("failed to remove objects from previous state: %w", err)
		}
	}
	return nil
}

// componentObjects defines optional objects, which could be created for vmestimator component
type componentObjects struct {
	pdb *vmv1beta1.EmbeddedPodDisruptionBudgetSpec
	np  *vmv1beta1.EmbeddedNetworkPolicy
	vpa *vmv1beta1.EmbeddedVPA
}

// createOrUpdateComponentObjects reconciles optional objects of the component, which runs as the given workload kind
func createOrUpdateComponentObjects(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator, kind vmv1beta1.ClusterComponent, workloadKind string, cur, prev componentObjects) error {
	owner := cr.AsOwner()
	b := build.NewChildBuilder(cr, kind)
	var prevB *build.ChildBuilder
	if prevCR != nil {
		prevB = build.NewChildBuilder(prevCR, kind)
	}
	if cur.pdb != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prev.pdb != nil {
			prevPDB = build.PodDisruptionBudget(prevB, prev.pdb)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(b, cur.pdb), prevPDB, &owner); err != nil {
			return fmt.Errorf("cannot reconcile PDB: %w", err)
		}
	}
	if cur.np != nil {
		var prevNP *networkingv1.NetworkPolicy
		if prev.np != nil {
			prevNP = build.NetworkPolicy(prevB, prev.np)
		}
		if err := reconcile.NetworkPolicy(ctx, rclient, build.NetworkPolicy(b, cur.np), prevNP, &owner); err != nil {
			return fmt.Errorf("cannot reconcile NetworkPolicy: %w", err)
		}
	}
	if cur.vpa != nil {
		targetRef := autoscalingv1.CrossVersionObjectReference{
			Name:       b.PrefixedName(),
			Kind:       workloadKind,
			APIVersion: "apps/v1",
		}
		var prevVPA *vpav1.VerticalPodAutoscaler
		if prev.vpa != nil {
			prevVPA = build.VPA(prevB, targetRef, prev.vpa)
		}
		if err := reconcile.VPA(ctx, rclient, build.VPA(b, targetRef, cur.vpa), prevVPA, &owner); err != nil {
			return fmt.Errorf("cannot reconcile VPA: %w", err)
		}
	}
	return nil
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1.VMEstimator) error {
	cc := finalize.NewChildCleaner()
	if single := cr.Spec.Single; single == nil {
		if err := finalize.OnVMEstimatorSingleDelete(ctx, rclient, cr, true); err != nil {
			return fmt.Errorf("cannot remove orphaned single resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(singleKind)
		if single.PodDisruptionBudget != nil {
			cc.KeepPDB(commonName)
		}
		if single.NetworkPolicy != nil {
			cc.KeepNetworkPolicy(commonName)
		}
		if single.VPA != nil {
			cc.KeepVPA(commonName)
		}
		if !ptr.Deref(single.DisableSelfServiceScrape, false) {
			cc.KeepScrape(commonName)
		}
		cc.KeepService(commonName)
		if single.ServiceSpec != nil && !single.ServiceSpec.UseAsDefault {
			cc.KeepService(single.ServiceSpec.NameOrDefault(commonName))
		}
	}

	if storage := cr.Spec.Storage; storage == nil {
		if err := finalize.OnStorageDelete(ctx, rclient, cr, true); err != nil {
			return fmt.Errorf("cannot remove orphaned storage resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(storageKind)
		if storage.PodDisruptionBudget != nil {
			cc.KeepPDB(commonName)
		}
		if storage.NetworkPolicy != nil {
			cc.KeepNetworkPolicy(commonName)
		}
		if storage.VPA != nil {
			cc.KeepVPA(commonName)
		}
		if !ptr.Deref(storage.DisableSelfServiceScrape, false) {
			cc.KeepScrape(commonName)
		}
		cc.KeepService(commonName)
		cc.KeepService(cr.PrefixedInsertName())
		if storage.ServiceSpec != nil && !storage.ServiceSpec.UseAsDefault {
			cc.KeepService(storage.ServiceSpec.NameOrDefault(commonName))
		}
	}

	if sel := cr.Spec.Select; sel == nil {
		if err := finalize.OnSelectDelete(ctx, rclient, cr, true); err != nil {
			return fmt.Errorf("cannot remove orphaned select resources: %w", err)
		}
	} else {
		commonName := cr.PrefixedName(selectKind)
		if sel.PodDisruptionBudget != nil {
			cc.KeepPDB(commonName)
		}
		if sel.NetworkPolicy != nil {
			cc.KeepNetworkPolicy(commonName)
		}
		if sel.HPA != nil {
			cc.KeepHPA(commonName)
		}
		if sel.VPA != nil {
			cc.KeepVPA(commonName)
		}
		if !ptr.Deref(sel.DisableSelfServiceScrape, false) {
			cc.KeepScrape(commonName)
		}
		cc.KeepService(commonName)
		if sel.ServiceSpec != nil && !sel.ServiceSpec.UseAsDefault {
			cc.KeepService(sel.ServiceSpec.NameOrDefault(commonName))
		}
	}

	if !cr.IsOwnsServiceAccount() {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
		objMeta := metav1.ObjectMeta{Name: b.PrefixedName(), Namespace: b.GetNamespace()}
		objsToRemove := []client.Object{&corev1.ServiceAccount{ObjectMeta: objMeta}}
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, b); err != nil {
			return fmt.Errorf("cannot remove serviceaccount: %w", err)
		}
	}
	return cc.RemoveOrphaned(ctx, rclient, cr)
}
