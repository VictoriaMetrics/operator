package finalize

import (
	"context"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAuthIngressDelete handles case, when user wants to remove spec.Ingress from vmauth config.
func VMAuthIngressDelete(ctx context.Context, rclient client.Client, crd *victoriametricsv1beta1.VMAuth) error {
	vmauthIngress := &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crd.PrefixedName(),
			Namespace: crd.Namespace,
		},
	}
	if err := removeFinalizeObjByName(ctx, rclient, vmauthIngress, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if err := SafeDelete(ctx, rclient, vmauthIngress); err != nil {
		return err
	}
	return nil
}

func OnVMAuthDelete(ctx context.Context, rclient client.Client, crd *victoriametricsv1beta1.VMAuth) error {
	// check deployment
	if err := removeFinalizeObjByName(ctx, rclient, &appsv1.Deployment{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}

	// check service
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	if crd.Spec.ServiceSpec != nil {
		if err := removeFinalizeObjByName(ctx, rclient, &v1.Service{}, crd.Spec.ServiceSpec.NameOrDefault(crd.PrefixedName()), crd.Namespace); err != nil {
			return err
		}
	}

	// check secret
	if err := removeFinalizeObjByName(ctx, rclient, &v1.Secret{}, crd.ConfigSecretName(), crd.Namespace); err != nil {
		return err
	}

	// check PDB
	if err := removeFinalizeObjByName(ctx, rclient, &policyv1beta1.PodDisruptionBudget{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}
	// check ingress
	if err := removeFinalizeObjByName(ctx, rclient, &v1beta1.Ingress{}, crd.PrefixedName(), crd.Namespace); err != nil {
		return err
	}

	if err := finalizePsp(ctx, rclient, crd); err != nil {
		return err
	}
	// remove from self.
	if err := removeFinalizeObjByName(ctx, rclient, crd, crd.Name, crd.Namespace); err != nil {
		return err
	}
	if err := deleteSA(ctx, rclient, crd); err != nil {
		return err
	}
	return nil
}
