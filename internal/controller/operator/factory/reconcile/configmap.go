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

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// ConfigMap reconciles configmap object
func ConfigMap(ctx context.Context, rclient client.Client, newObj *corev1.ConfigMap, prevMeta *metav1.ObjectMeta, owner *metav1.OwnerReference) (bool, error) {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	updated := true
	err := retryOnConflict(func() error {
		var existingObj corev1.ConfigMap
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ConfigMap=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing ConfigMap=%s: %w", nsn, err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		if !metaChanged &&
			equality.Semantic.DeepEqual(newObj.Data, existingObj.Data) &&
			equality.Semantic.DeepEqual(newObj.BinaryData, existingObj.BinaryData) {
			updated = false
			return nil
		}
		existingObj.Data = newObj.Data
		existingObj.BinaryData = newObj.BinaryData
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ConfigMap=%s configuration", nsn))
		return rclient.Update(ctx, &existingObj)
	})
	return updated, err
}
