package build

import (
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type objectForServiceAccountBuilder interface {
	AllLabels() map[string]string
	AnnotationsFiltered() map[string]string
	AsOwner() []metav1.OwnerReference
	GetNSName() string
	GetServiceAccount() *vmv1beta1.ServiceAccount
	IsOwnsServiceAccount() bool
	PrefixedName() string
}

// ServiceAccount builds service account for CRD
func ServiceAccount(cr objectForServiceAccountBuilder) *v1.ServiceAccount {
	sa := cr.GetServiceAccount()
	return &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            sa.Name,
			Namespace:       cr.GetNSName(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		AutomountServiceAccountToken: &sa.AutomountToken,
	}
}
