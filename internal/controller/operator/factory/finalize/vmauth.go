package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

// OnVMAuthDelete deletes all vmauth related resources
func OnVMAuthDelete(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
	ns := cr.GetNamespace()
	objMeta := metav1.ObjectMeta{
		Namespace: ns,
		Name:      cr.PrefixedName(),
	}
	objsToRemove := []client.Object{
		&appsv1.Deployment{ObjectMeta: objMeta},
		&corev1.Service{ObjectMeta: objMeta},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.GetServiceAccountName(),
			Namespace: ns,
		}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.ConfigSecretName(),
			Namespace: ns,
		}},
		&networkingv1.Ingress{ObjectMeta: objMeta},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta},
		&rbacv1.RoleBinding{ObjectMeta: objMeta},
		&rbacv1.Role{ObjectMeta: objMeta},
		&policyv1.PodDisruptionBudget{ObjectMeta: objMeta},
	}
	if cr.Spec.ServiceSpec != nil {
		objsToRemove = append(objsToRemove, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName()),
			Namespace: ns,
		}})
	}
	cfg := config.MustGetBaseConfig()
	if cfg.VPAAPIEnabled {
		objsToRemove = append(objsToRemove, &vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta})
	}
	if cfg.GatewayAPIEnabled {
		objsToRemove = append(objsToRemove, &gwapiv1.HTTPRoute{ObjectMeta: objMeta})
	}
	objsToRemove = append(objsToRemove, cr)
	deleteOwnerReferences := make([]bool, len(objsToRemove))
	return removeFinalizers(ctx, rclient, objsToRemove, deleteOwnerReferences, cr)
}
