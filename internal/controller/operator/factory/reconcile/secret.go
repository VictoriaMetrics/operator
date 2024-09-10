package reconcile

import (
	"context"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Secret reconciles secret object
func Secret(ctx context.Context, rclient client.Client, s *corev1.Secret) error {
	var curSecret corev1.Secret

	if err := rclient.Get(ctx, types.NamespacedName{Namespace: s.Namespace, Name: s.Name}, &curSecret); err != nil {
		if errors.IsNotFound(err) {
			logger.WithContext(ctx).Info("creating new configuration secret")
			return rclient.Create(ctx, s)
		}
		return err
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &curSecret); err != nil {
		return err
	}
	s.Annotations = labels.Merge(curSecret.Annotations, s.Annotations)
	s.ResourceVersion = curSecret.ResourceVersion
	// fast path
	if equality.Semantic.DeepEqual(s.Data, curSecret.Data) &&
		equality.Semantic.DeepEqual(s.Labels, curSecret.Labels) &&
		equality.Semantic.DeepEqual(s.Annotations, curSecret.Annotations) {
		return nil
	}
	logger.WithContext(ctx).Info("updating configuration secret")
	return rclient.Update(ctx, s)
}
