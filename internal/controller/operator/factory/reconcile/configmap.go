package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
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
		if errors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new ConfigMap=%s", newCM.Name))
			return rclient.Create(ctx, newCM)
		}
	}
	var prevAnnotations map[string]string
	if prevCMMEta != nil {
		prevAnnotations = prevCMMEta.Annotations
	}
	if equality.Semantic.DeepEqual(newCM.Data, currentCM.Data) &&
		equality.Semantic.DeepEqual(newCM.Labels, currentCM.Labels) &&
		isAnnotationsEqual(currentCM.Annotations, newCM.Annotations, prevAnnotations) {
		return nil
	}

	newCM.Annotations = mergeAnnotations(currentCM.Annotations, newCM.Annotations, prevAnnotations)

	vmv1beta1.AddFinalizer(newCM, &currentCM)
	logger.WithContext(ctx).Info(fmt.Sprintf("updating configmap=%s configuration", newCM.Name))

	return rclient.Update(ctx, newCM)
}
