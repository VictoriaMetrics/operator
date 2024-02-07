package psp

import (
	"context"
	"fmt"

	v1beta12 "github.com/VictoriaMetrics/operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDObject interface {
	AnnotationsFiltered() map[string]string
	AllLabels() map[string]string
	PrefixedName() string
	GetServiceAccountName() string
	IsOwnsServiceAccount() bool
	GetNSName() string
	AsOwner() []metav1.OwnerReference
	AsCRDOwner() []metav1.OwnerReference
}

// CreateServiceAccountForCRD creates service account for CRD if needed
func CreateServiceAccountForCRD(ctx context.Context, cr CRDObject, rclient client.Client) error {
	if !cr.IsOwnsServiceAccount() {
		return nil
	}
	newSA := buildSA(cr)
	var existSA v1.ServiceAccount
	if err := rclient.Get(ctx, types.NamespacedName{Name: cr.GetServiceAccountName(), Namespace: cr.GetNSName()}, &existSA); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newSA)
		}
		return fmt.Errorf("cannot get ServiceAccount for given CRD Object=%q, err=%w", cr.PrefixedName(), err)
	}

	existSA.OwnerReferences = newSA.OwnerReferences
	existSA.Finalizers = v1beta12.MergeFinalizers(&existSA, v1beta12.FinalizerName)
	existSA.Annotations = labels.Merge(existSA.Annotations, newSA.Annotations)
	existSA.Labels = newSA.Labels
	return rclient.Update(ctx, &existSA)
}

func buildSA(cr CRDObject) *v1.ServiceAccount {
	return &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetServiceAccountName(),
			Namespace:       cr.GetNSName(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta12.FinalizerName},
		},
	}
}
