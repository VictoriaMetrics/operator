package reconcile

import (
	"context"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigMap reconciles configmap object
func ConfigMap(ctx context.Context, rclient client.Client, cm *corev1.ConfigMap) error {
	var existCM corev1.ConfigMap
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cm.Namespace, Name: cm.Name}, &existCM); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, cm)
		}
	}
	cm.Annotations = labels.Merge(existCM.Annotations, cm.Annotations)
	vmv1beta1.AddFinalizer(cm, &existCM)

	if equality.Semantic.DeepEqual(cm.Data, existCM.Data) &&
		equality.Semantic.DeepEqual(cm.Labels, existCM.Labels) &&
		equality.Semantic.DeepEqual(cm.Annotations, existCM.Annotations) {
		return nil
	}
	logger.WithContext(ctx).Info("updating configmap configuration", "cm_name", cm.Name)

	return rclient.Update(ctx, cm)
}
