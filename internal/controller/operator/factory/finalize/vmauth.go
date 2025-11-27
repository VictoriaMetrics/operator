package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMAuthDelete deletes all vmauth related resources
func OnVMAuthDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}

	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}
	if cr.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &corev1.Service{}, cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()), cr.Namespace); err != nil {
			return err
		}
	}

	// check secret
	if err := removeFinalizeObjByName(ctx, rclient, &corev1.Secret{}, cr.ConfigSecretName(), cr.Namespace); err != nil {
		return err
	}

	// check PDB
	if cr.Spec.PodDisruptionBudget != nil {
		if err := finalizePDB(ctx, rclient, cr); err != nil {
			return err
		}
	}
	if cr.Spec.Ingress != nil {
		vmauthIngress := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.PrefixedName(),
				Namespace: cr.Namespace,
			},
		}
		if err := removeFinalizeObjByName(ctx, rclient, vmauthIngress, cr.PrefixedName(), cr.Namespace); err != nil {
			return err
		}
		if err := SafeDelete(ctx, rclient, vmauthIngress); err != nil {
			return err
		}
	}

	// check ingress
	if err := removeFinalizeObjByName(ctx, rclient, &networkingv1.Ingress{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}

	if cr.Spec.HTTPRoute != nil {
		httpRoute := &gwapiv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.PrefixedName(),
				Namespace: cr.Namespace,
			},
		}
		if err := removeFinalizeObjByName(ctx, rclient, httpRoute, cr.PrefixedName(), cr.Namespace); err != nil {
			return err
		}
		if err := SafeDelete(ctx, rclient, httpRoute); err != nil {
			return err
		}
	}

	// check ingress
	if err := removeFinalizeObjByName(ctx, rclient, &gwapiv1.HTTPRoute{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}

	// check HPA
	if err := removeFinalizeObjByName(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{}, cr.PrefixedName(), cr.Namespace); err != nil {
		return err
	}

	if err := deleteSA(ctx, rclient, cr); err != nil {
		return err
	}
	if err := removeConfigReloaderRole(ctx, rclient, cr); err != nil {
		return err
	}
	// remove from self.
	if err := removeFinalizeObjByName(ctx, rclient, cr, cr.Name, cr.Namespace); err != nil {
		return err
	}

	return nil
}
