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
func ConfigMap(ctx context.Context, rclient client.Client, newObj *corev1.ConfigMap, prevMeta *metav1.ObjectMeta) (bool, error) {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	updated := true
	err := retryOnConflict(func() error {
		var existingObj corev1.ConfigMap
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ConfigMap=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return err
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}
		if equality.Semantic.DeepEqual(newObj.Data, existingObj.Data) &&
			isObjectMetaEqual(&existingObj, newObj, prevMeta) {
			updated = false
			return nil
		}

		vmv1beta1.AddFinalizer(newObj, &existingObj)
		mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ConfigMap=%s configuration", nsn))
		return rclient.Update(ctx, newObj)
	})
	return updated, err
}
