package reconcile

import (
	"context"
	"fmt"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// VMAuth performs an update or create of VMAuth and till it's ready
func VMAuth(ctx context.Context, rclient client.Client, newObj, prevObj *vmv1beta1.VMAuth, owner *metav1.OwnerReference) error {
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	rclient.Scheme().Default(newObj)
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	removeFinalizer := false
	err := retryOnConflict(func() error {
		var existingObj vmv1beta1.VMAuth
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new VMAuth=%s", nsn.String()))
				if err := rclient.Create(ctx, newObj); err != nil {
					return fmt.Errorf("cannot create new VMAuth=%s: %w", nsn.String(), err)
				}
				return nil
			}
			return fmt.Errorf("cannot get VMAuth=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, removeFinalizer, false)
		if err != nil {
			return err
		}
		logMessageMetadata := []string{fmt.Sprintf("name=%s, is_prev_nil=%t", nsn.String(), prevObj == nil)}
		specDiff := diffDeepDerivative(newObj.Spec, existingObj.Spec)
		needsUpdate := metaChanged || len(specDiff) > 0
		logMessageMetadata = append(logMessageMetadata, fmt.Sprintf("spec_diff=%s", specDiff))
		if !needsUpdate {
			return nil
		}
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating VMAuth %s", strings.Join(logMessageMetadata, ", ")))
		if err := rclient.Update(ctx, &existingObj); err != nil {
			return fmt.Errorf("cannot update VMAuth=%s: %w", nsn.String(), err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := waitForStatus(ctx, rclient, newObj, vmStatusInterval, vmv1beta1.UpdateStatusOperational); err != nil {
		return fmt.Errorf("failed to wait for VMAuth=%s to be ready: %w", nsn.String(), err)
	}
	return nil
}
