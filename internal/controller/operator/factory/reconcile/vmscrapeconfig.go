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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// VMScrapeConfig creates or updates given object
func VMScrapeConfig(ctx context.Context, rclient client.Client, newObj, prevObj *vmv1beta1.VMScrapeConfig, owner *metav1.OwnerReference, isConversion bool) error {
	if build.IsControllerDisabled("VMScrapeConfig") {
		return nil
	}
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	removeFinalizer := true
	return retryOnConflict(func() error {
		var existingObj vmv1beta1.VMScrapeConfig
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating VMScrapeConfig=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return err
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		if isConversion && existingObj.Annotations[vmv1beta1.IgnoreConversionLabel] == vmv1beta1.IgnoreConversion {
			logger.WithContext(ctx).Info(fmt.Sprintf("syncing for VMScrapeConfig=%s was disabled by annotation", nsn.String()))
			return nil
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, removeFinalizer, isConversion)
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
		logger.WithContext(ctx).Info(fmt.Sprintf("updating VMScrapeConfig %s", strings.Join(logMessageMetadata, ", ")))
		return rclient.Update(ctx, &existingObj)
	})
}
