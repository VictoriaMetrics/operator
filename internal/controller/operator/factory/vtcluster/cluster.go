package vtcluster

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
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
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			b := newOptsBuilder(prevCR, prevCR.PrefixedName(), prevCR.SelectorLabels())
			prevSA = build.ServiceAccount(b)
		}
		b := newOptsBuilder(cr, cr.PrefixedName(), cr.SelectorLabels())
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(b), prevSA); err != nil {
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

	if err := deletePrevStateResources(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("failed to remove objects from previous cluster state: %w", err)
	}
	return nil
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if prevCR == nil {
		// fast path
		return nil
	}
	prevSpec := prevCR.Spec

	newStorage := cr.Spec.Storage
	newSelect := cr.Spec.Select
	newInsert := cr.Spec.Insert
	prevStorage := prevSpec.Storage
	prevSelect := prevSpec.Select
	prevInsert := prevSpec.Insert

	newLB := cr.Spec.RequestsLoadBalancer
	prevLB := prevCR.Spec.RequestsLoadBalancer

	if prevStorage != nil {
		if newStorage == nil {
			if err := finalize.OnVTStorageDelete(ctx, rclient, cr, prevStorage); err != nil {
				return fmt.Errorf("cannot remove storage from prev state: %w", err)
			}
		} else {
			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: cr.GetVTStorageName()}
			if newStorage.PodDisruptionBudget == nil && prevStorage.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev storage: %w", err)
				}
			}
			if ptr.Deref(newStorage.DisableSelfServiceScrape, false) && !ptr.Deref(prevStorage.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev storage: %w", err)
				}
			}
			prevSvc, currSvc := prevStorage.ServiceSpec, newStorage.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, cr.GetVTStorageName(), cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove storage additional service: %w", err)
			}
		}
	}

	if prevSelect != nil {
		if newSelect == nil {
			if err := finalize.OnVTSelectDelete(ctx, rclient, cr, prevSelect); err != nil {
				return fmt.Errorf("cannot remove select from prev state: %w", err)
			}
		} else {
			commonObjMeta := metav1.ObjectMeta{
				Namespace: cr.Namespace, Name: cr.GetVTSelectName()}
			if newSelect.PodDisruptionBudget == nil && prevSelect.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev select: %w", err)
				}
			}
			if newSelect.HPA == nil && prevSelect.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove HPA from prev select: %w", err)
				}
			}
			if ptr.Deref(newSelect.DisableSelfServiceScrape, false) && !ptr.Deref(prevSelect.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev select: %w", err)
				}
			}
			prevSvc, currSvc := prevSelect.ServiceSpec, newSelect.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, cr.GetVTSelectName(), cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove select additional service: %w", err)
			}
		}
		// transition to load-balancer state
		// have to remove prev service scrape
		newDisableBalancing := newLB.DisableSelectBalancing || newSelect == nil
		prevDisableBalancing := prevLB.DisableSelectBalancing
		if newLB.Enabled && !newDisableBalancing && (!prevLB.Enabled || prevDisableBalancing) {
			// remove service scrape because service was renamed
			if !ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetVTSelectName(), Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for non-lb select svc: %w", err)
				}
			}
		}
		// disabled loadbalancer only for component
		// transit to the k8s service balancing mode
		if prevLB.Enabled && !prevDisableBalancing && (!newLB.Enabled || newDisableBalancing) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      cr.GetVTSelectLBName(),
				Namespace: cr.Namespace,
			}}); err != nil {
				return fmt.Errorf("cannot remove select lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetVTSelectLBName(), Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for lb select svc: %w", err)
				}
			}
		}
	}

	if prevInsert != nil {
		if newInsert == nil {
			if err := finalize.OnVTInsertDelete(ctx, rclient, cr, prevInsert); err != nil {
				return fmt.Errorf("cannot remove insert from prev state: %w", err)
			}
		} else {

			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: cr.GetVTInsertName()}
			if newInsert.PodDisruptionBudget == nil && prevInsert.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev insert: %w", err)
				}
			}
			if newInsert.HPA == nil && prevInsert.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove HPA from prev insert: %w", err)
				}
			}
			if ptr.Deref(newInsert.DisableSelfServiceScrape, false) && !ptr.Deref(prevInsert.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev insert: %w", err)
				}
			}
			prevSvc, currSvc := prevInsert.ServiceSpec, newInsert.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, cr.GetVTInsertName(), cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove insert additional service: %w", err)
			}
		}
		// transition to load-balancer state
		// have to remove prev service scrape
		newDisableBalancing := newLB.DisableInsertBalancing || newInsert == nil
		prevDisableBalancing := prevLB.DisableInsertBalancing
		if newLB.Enabled && !newDisableBalancing && (!prevLB.Enabled || prevDisableBalancing) {
			// remove service scrape because service was renamed
			if !ptr.Deref(cr.Spec.Insert.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetVTInsertName(), Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for non-lb insert svc: %w", err)
				}
			}
		}
		// disabled loadbalancer only for component
		// transit to the k8s service balancing mode
		if prevLB.Enabled && !prevDisableBalancing && (!newLB.Enabled || newDisableBalancing) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      cr.GetVTInsertLBName(),
				Namespace: cr.Namespace,
			}}); err != nil {
				return fmt.Errorf("cannot remove insert lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.Insert.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetVTInsertLBName(), Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for lb insert svc: %w", err)
				}
			}
		}
	}

	if prevLB.Enabled && !newLB.Enabled {
		if err := finalize.OnVTClusterLoadBalancerDelete(ctx, rclient, prevCR); err != nil {
			return fmt.Errorf("failed to remove loadbalancer components enabled at prev state: %w", err)
		}
	}
	if newLB.Enabled {
		// case for child objects
		if prevLB.Spec.PodDisruptionBudget != nil && newLB.Spec.PodDisruptionBudget == nil {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.GetVMAuthLBName(),
					Namespace: cr.Namespace,
				},
			}); err != nil {
				return fmt.Errorf("cannot delete PodDisruptionBudget for cluster lb: %w", err)
			}
		}
	}

	return nil
}
