package reconcile

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceAccount creates service account or updates exist one
func ServiceAccount(ctx context.Context, rclient client.Client, sa *corev1.ServiceAccount) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var existSA corev1.ServiceAccount
		if err := rclient.Get(ctx, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, &existSA); err != nil {
			if errors.IsNotFound(err) {
				return rclient.Create(ctx, sa)
			}
			return fmt.Errorf("cannot get ServiceAccount for given CRD Object=%q, err=%w", sa.Name, err)
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &existSA); err != nil {
			return err
		}

		existSA.OwnerReferences = sa.OwnerReferences
		existSA.Finalizers = vmv1beta1.MergeFinalizers(&existSA, vmv1beta1.FinalizerName)
		existSA.Annotations = labels.Merge(existSA.Annotations, sa.Annotations)
		existSA.Labels = sa.Labels
		return rclient.Update(ctx, &existSA)
	})
}
