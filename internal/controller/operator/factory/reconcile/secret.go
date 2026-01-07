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

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Secret reconciles secret object
func Secret(ctx context.Context, rclient client.Client, newS *corev1.Secret, prevMeta *metav1.ObjectMeta) error {
	var currentS corev1.Secret

	if err := rclient.Get(ctx, types.NamespacedName{Namespace: newS.Namespace, Name: newS.Name}, &currentS); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new Secret %s", newS.Name))
			return rclient.Create(ctx, newS)
		}
		return err
	}
	if !currentS.DeletionTimestamp.IsZero() {
		return newErrRecreate(ctx, &currentS)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &currentS); err != nil {
		return err
	}

	var prevS *corev1.Secret
	if prevMeta != nil {
		prevS = &corev1.Secret{
			ObjectMeta: *prevMeta,
		}
	}

	if equality.Semantic.DeepEqual(newS.Data, currentS.Data) &&
		equality.Semantic.DeepEqual(newS.Labels, currentS.Labels) &&
		isObjectMetaEqual(&currentS, newS, prevS) {
		return nil
	}

	mergeObjectMetadataIntoNew(&currentS, newS, prevS)

	logger.WithContext(ctx).Info(fmt.Sprintf("updating configuration Secret %s", newS.Name))

	return rclient.Update(ctx, newS)
}
