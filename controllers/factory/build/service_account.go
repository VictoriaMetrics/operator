package build

import (
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type objectForServiceBuilder interface {
	AnnotationsFiltered() map[string]string
	AllLabels() map[string]string
	PrefixedName() string
	GetServiceAccountName() string
	IsOwnsServiceAccount() bool
	GetPSPName() string
	GetNSName() string
	AsOwner() []metav1.OwnerReference
	AsCRDOwner() []metav1.OwnerReference
}

// ServiceAccount builds service account for CRD
func ServiceAccount(cr objectForServiceBuilder) *v1.ServiceAccount {
	return &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetServiceAccountName(),
			Namespace:       cr.GetNSName(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
	}
}
