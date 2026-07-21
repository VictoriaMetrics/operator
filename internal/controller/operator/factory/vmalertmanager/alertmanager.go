package vmalertmanager

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const templatesDir = "/etc/vm/templates"

// CreateOrUpdateAlertManager creates alertmanager and builds config for it
func CreateOrUpdateAlertManager(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	if cr.Paused() {
		return nil
	}
	var prevCR *vmv1beta1.VMAlertmanager
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete orphaned resources: %w", err)
		}
	}
	owner := cr.AsOwner()
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA, &owner); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
		if err := createConfigSecretAccess(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
	}

	if err := createOrUpdateAlertManagerService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB, &owner); err != nil {
			return err
		}
	}
	cfg := config.MustGetBaseConfig()
	if cr.Spec.VPA != nil && !cfg.VPAAPIEnabled {
		return fmt.Errorf("spec.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
	}
	if err := createOrUpdateVPA(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot create or update vpa for vmalertmanager: %w", err)
	}
	if cr.Spec.NetworkPolicy != nil {
		var prevNP *networkingv1.NetworkPolicy
		if prevCR != nil && prevCR.Spec.NetworkPolicy != nil {
			prevNP = build.NetworkPolicy(prevCR, prevCR.Spec.NetworkPolicy)
		}
		if err := reconcile.NetworkPolicy(ctx, rclient, build.NetworkPolicy(cr, cr.Spec.NetworkPolicy), prevNP, &owner); err != nil {
			return fmt.Errorf("cannot update network policy for vmalertmanager: %w", err)
		}
	}
	var prevSts *appsv1.StatefulSet
	if prevCR != nil {
		var err error
		prevSts, err = newStsForAlertManager(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev alertmanager sts, name: %s,err: %w", cr.Name, err)
		}
	}
	newSts, err := newStsForAlertManager(cr)
	if err != nil {
		return fmt.Errorf("cannot generate alertmanager sts, name: %s,err: %w", cr.Name, err)
	}

	o := reconcile.StatefulSetOpts{
		SelectorLabels: cr.SelectorLabels(),
	}
	return reconcile.StatefulSet(ctx, rclient, newSts, prevSts, &owner, &o)
}

func createOrUpdateVPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlertmanager) error {
	if cr.Spec.VPA == nil {
		return nil
	}
	targetRef := autoscalingv1.CrossVersionObjectReference{
		Name:       cr.PrefixedName(),
		Kind:       string(vmv1beta1.WorkloadKindStatefulSet),
		APIVersion: "apps/v1",
	}
	newVPA := build.VPA(cr, targetRef, cr.Spec.VPA)
	var prevVPA *vpav1.VerticalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VPA != nil {
		prevVPA = build.VPA(prevCR, targetRef, prevCR.Spec.VPA)
	}
	owner := cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager) error {
	svcName := cr.PrefixedName()
	keepServices := sets.New(svcName)
	keepServicesScrapes := sets.New[string]()
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		keepServicesScrapes.Insert(svcName)
	}
	if cr.Spec.ServiceSpec != nil && !cr.Spec.ServiceSpec.UseAsDefault {
		extraSvcName := cr.Spec.ServiceSpec.NameOrDefault(svcName)
		keepServices.Insert(extraSvcName)
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, cr, keepServices, true); err != nil {
		return fmt.Errorf("cannot remove services: %w", err)
	}
	if err := finalize.RemoveOrphanedVMServiceScrapes(ctx, rclient, cr, keepServicesScrapes, true); err != nil {
		return fmt.Errorf("cannot remove serviceScrapes: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	var objsToRemove []client.Object
	if cr.Spec.PodDisruptionBudget == nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if config.MustGetBaseConfig().VPAAPIEnabled && cr.Spec.VPA == nil {
		objsToRemove = append(objsToRemove, &vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta})
	}
	if cr.Spec.NetworkPolicy == nil {
		objsToRemove = append(objsToRemove, &networkingv1.NetworkPolicy{ObjectMeta: objMeta})
	}
	if !cr.IsOwnsServiceAccount() {
		objsToRemove = append(objsToRemove, &corev1.ServiceAccount{ObjectMeta: objMeta})
	}
	return finalize.SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, cr)
}
