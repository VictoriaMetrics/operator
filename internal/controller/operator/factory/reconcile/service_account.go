package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// ServiceAccount creates service account or updates exist one
func ServiceAccount(ctx context.Context, rclient client.Client, newObj, prevObj *corev1.ServiceAccount, owner *metav1.OwnerReference) error {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	removeFinalizer := true
	return retryOnConflict(func() error {
		var existingObj corev1.ServiceAccount
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ServiceAccount=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get ServiceAccount=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, removeFinalizer, false)
		if err != nil {
			return err
		}
		if !metaChanged {
			return nil
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ServiceAccount=%s", nsn.String()))
		return rclient.Update(ctx, &existingObj)
	})
}
