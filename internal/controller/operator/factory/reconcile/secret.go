package reconcile

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Secret reconciles secret object
func Secret(ctx context.Context, rclient client.Client, newObj *corev1.Secret, prevMeta *metav1.ObjectMeta, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	removeFinalizer := true
	return retryOnConflict(func() error {
		var existingObj corev1.Secret
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new Secret=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing Secret=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, removeFinalizer)
		if err != nil {
			return err
		}
		logMessageMetadata := []string{fmt.Sprintf("name=%s", nsn.String())}
		isDataEqual := equality.Semantic.DeepDerivative(newObj.Data, existingObj.Data)
		needsUpdate := metaChanged || !isDataEqual
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("data_changed=%t", !isDataEqual))

		if !needsUpdate {
			return nil
		}
		existingObj.Data = newObj.Data
		logger.WithContext(ctx).Info(fmt.Sprintf("updating Secret %s", strings.Join(logMessageMetadata, ", ")))
		return rclient.Update(ctx, &existingObj)
	})
}
