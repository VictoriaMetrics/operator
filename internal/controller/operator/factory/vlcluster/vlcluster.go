package vlcluster

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
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// CreateOrUpdate syncs VLCluster object to the desired state
func CreateOrUpdate(ctx context.Context, rclient client.Client, cr *vmv1.VLCluster) error {
	var prevCR *vmv1.VLCluster
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

	if err := deletePrevStateResources(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("failed to remove objects from previous cluster state: %w", err)
	}
	return nil
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if prevCR == nil {
		// fast path
		return nil
	}
	vmst := cr.Spec.VLStorage
	vmse := cr.Spec.VLSelect
	vmis := cr.Spec.VLInsert
	prevSpec := prevCR.Spec
	prevSt := prevSpec.VLStorage
	prevSe := prevSpec.VLSelect
	prevIs := prevSpec.VLInsert

	prevLB := prevSpec.RequestsLoadBalancer
	newLB := cr.Spec.RequestsLoadBalancer

	cfg := config.MustGetBaseConfig()
	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation

	if prevSt != nil {
		if vmst == nil {
			if err := finalize.OnVLStorageDelete(ctx, rclient, cr, prevSt); err != nil {
				return fmt.Errorf("cannot remove storage from prev state: %w", err)
			}
		} else {
			commonName := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: commonName}
			if vmst.PodDisruptionBudget == nil && prevSt.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev storage: %w", err)
				}
			}
			if ptr.Deref(vmst.DisableSelfServiceScrape, disableSelfScrape) && !ptr.Deref(prevSt.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev storage: %w", err)
				}
			}
			prevSvc, currSvc := prevSt.ServiceSpec, vmst.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, commonName, cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove storage additional service: %w", err)
			}
		}
	}

	if prevSe != nil {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
		commonInternalName := cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect)
		if vmse == nil {
			if err := finalize.OnVLSelectDelete(ctx, rclient, cr, prevSe); err != nil {
				return fmt.Errorf("cannot remove select from prev state: %w", err)
			}
		} else {
			commonObjMeta := metav1.ObjectMeta{
				Namespace: cr.Namespace, Name: commonName}
			if vmse.PodDisruptionBudget == nil && prevSe.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev select: %w", err)
				}
			}
			if vmse.HPA == nil && prevSe.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove HPA from prev select: %w", err)
				}
			}
			if ptr.Deref(vmse.DisableSelfServiceScrape, disableSelfScrape) && !ptr.Deref(prevSe.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev select: %w", err)
				}
			}
			prevSvc, currSvc := prevSe.ServiceSpec, vmse.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, commonName, cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove select additional service: %w", err)
			}
		}
		// transition to load-balancer state
		// have to remove prev service scrape
		if newLB.Enabled && !newLB.DisableSelectBalancing && (!prevLB.Enabled || prevLB.DisableSelectBalancing) {
			// remove service scrape because service was renamed
			if !ptr.Deref(cr.Spec.VLSelect.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: commonName, Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for non-lb select svc: %w", err)
				}
			}
		}
		// disabled loadbalancer only for component
		// transit to the k8s service balancing mode
		if prevLB.Enabled && !prevLB.DisableSelectBalancing && (!newLB.Enabled || newLB.DisableSelectBalancing) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      commonInternalName,
				Namespace: cr.Namespace,
			}}); err != nil {
				return fmt.Errorf("cannot remove select lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.VLSelect.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: commonInternalName, Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for lb select svc: %w", err)
				}
			}
		}
	}

	if prevIs != nil {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
		commonInternalName := cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
		if vmis == nil {
			if err := finalize.OnVLInsertDelete(ctx, rclient, cr, prevIs); err != nil {
				return fmt.Errorf("cannot remove insert from prev state: %w", err)
			}
		} else {

			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: commonName}
			if vmis.PodDisruptionBudget == nil && prevIs.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev insert: %w", err)
				}
			}
			if vmis.HPA == nil && prevIs.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove HPA from prev insert: %w", err)
				}
			}
			if ptr.Deref(vmis.DisableSelfServiceScrape, disableSelfScrape) && !ptr.Deref(prevIs.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev insert: %w", err)
				}
			}
			prevSvc, currSvc := prevIs.ServiceSpec, vmis.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, commonName, cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove insert additional service: %w", err)
			}
		}
		// transition to load-balancer state
		// have to remove prev service scrape
		if newLB.Enabled && !newLB.DisableInsertBalancing && (!prevLB.Enabled || prevLB.DisableInsertBalancing) {
			// remove service scrape because service was renamed
			if !ptr.Deref(cr.Spec.VLInsert.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: commonName, Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for non-lb insert svc: %w", err)
				}
			}
		}
		// disabled loadbalancer only for component
		// transit to the k8s service balancing mode
		if prevLB.Enabled && !prevLB.DisableInsertBalancing && (!newLB.Enabled || newLB.DisableInsertBalancing) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      commonInternalName,
				Namespace: cr.Namespace,
			}}); err != nil {
				return fmt.Errorf("cannot remove insert lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.VLInsert.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: commonInternalName, Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for lb insert svc: %w", err)
				}
			}
		}
	}

	if prevLB.Enabled && !newLB.Enabled {
		if err := finalize.OnVLClusterLoadBalancerDelete(ctx, rclient, prevCR); err != nil {
			return fmt.Errorf("failed to remove loadbalancer components enabled at prev state: %w", err)
		}
	}
	if newLB.Enabled {
		// case for child objects
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentBalancer)
		if prevLB.Spec.PodDisruptionBudget != nil && newLB.Spec.PodDisruptionBudget == nil {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      commonName,
					Namespace: cr.Namespace,
				},
			}); err != nil {
				return fmt.Errorf("cannot delete PodDisruptionBudget for cluster lb: %w", err)
			}
		}
	}

	return nil
}
