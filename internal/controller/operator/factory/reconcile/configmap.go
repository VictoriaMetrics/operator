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
func ConfigMap(ctx context.Context, rclient client.Client, newObj *corev1.ConfigMap, prevMeta *metav1.ObjectMeta) (bool, error) {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	updated := true
	var prevAnnotations, prevLabels map[string]string
	if prevMeta != nil {
		prevAnnotations = prevMeta.Annotations
		prevLabels = prevMeta.Labels
	}
	err := retryOnConflict(func() error {
		var existingObj corev1.ConfigMap
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ConfigMap=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing ConfigMap=%s: %w", nsn, err)
		}
		if err := needsGarbageCollection(ctx, rclient, &existingObj); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(newObj.Data, existingObj.Data) &&
			equality.Semantic.DeepEqual(newObj.BinaryData, existingObj.BinaryData) &&
			areMapsEqual(existingObj.Labels, newObj.Labels, prevLabels) &&
			areMapsEqual(existingObj.Annotations, newObj.Annotations, prevAnnotations) {
			updated = false
			return nil
		}
		existingObj.Labels = mergeMaps(existingObj.Labels, newObj.Labels, prevLabels)
		existingObj.Annotations = mergeMaps(existingObj.Annotations, newObj.Annotations, prevMeta.Annotations)
		existingObj.Data = newObj.Data
		existingObj.BinaryData = newObj.BinaryData
		addFinalizerIfAbsent(&existingObj)
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ConfigMap=%s configuration", nsn))
		return rclient.Update(ctx, &existingObj)
	})
	return updated, err
}
