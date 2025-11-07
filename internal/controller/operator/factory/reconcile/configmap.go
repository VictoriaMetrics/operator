package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// ConfigMap reconciles configmap object
func ConfigMap(ctx context.Context, rclient client.Client, newCM *corev1.ConfigMap, prevCMMEta *metav1.ObjectMeta) error {
	var currentCM corev1.ConfigMap
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: newCM.Namespace, Name: newCM.Name}, &currentCM); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new ConfigMap %s", newCM.Name))
			return rclient.Create(ctx, newCM)
		}
	}
	if !currentCM.DeletionTimestamp.IsZero() {
		return &errRecreate{
			origin: fmt.Errorf("waiting for configmap %q to be removed", newCM.Name),
		}
	}
	var prevCM *corev1.ConfigMap
	if prevCMMEta != nil {
		prevCM = &corev1.ConfigMap{
			ObjectMeta: *prevCMMEta,
		}
	}
	if equality.Semantic.DeepEqual(newCM.Data, currentCM.Data) &&
		isObjectMetaEqual(&currentCM, newCM, prevCM) {
		return nil
	}

	vmv1beta1.AddFinalizer(newCM, &currentCM)
	mergeObjectMetadataIntoNew(newCM, &currentCM, prevCM)

	logger.WithContext(ctx).Info(fmt.Sprintf("updating ConfigMap %s configuration", newCM.Name))

	return rclient.Update(ctx, newCM)
}
