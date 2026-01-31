package reconcile

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// VMServiceScrapeForCRD creates or updates given object
func VMServiceScrapeForCRD(ctx context.Context, rclient client.Client, newObj *vmv1beta1.VMServiceScrape) error {
	if build.IsControllerDisabled("VMServiceScrape") {
		return nil
	}
	nsn := types.NamespacedName{Namespace: newObj.Namespace, Name: newObj.Name}
	return retryOnConflict(func() error {
		var existingObj vmv1beta1.VMServiceScrape
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating VMServiceScrape=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return err
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(newObj.Spec, existingObj.Spec) &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			equality.Semantic.DeepEqual(newObj.Annotations, existingObj.Annotations) {
			return nil
		}
		// TODO: @f41gh7 allow 3rd party applications to add annotations for generated VMServiceScrape
		specDiff := diffDeep(newObj.Spec, existingObj.Spec)
		existingObj.Annotations = newObj.Annotations
		existingObj.Spec = newObj.Spec
		existingObj.Labels = newObj.Labels
		logMsg := fmt.Sprintf("updating VMServiceScrape=%s for CRD object spec_diff: %s", nsn, specDiff)
		logger.WithContext(ctx).Info(logMsg)

		return rclient.Update(ctx, &existingObj)
	})
}

// VMPodScrapeForCRD creates or updates given object
func VMPodScrapeForCRD(ctx context.Context, rclient client.Client, newObj *vmv1beta1.VMPodScrape) error {
	if build.IsControllerDisabled("VMPodScrape") {
		return nil
	}
	nsn := types.NamespacedName{Namespace: newObj.Namespace, Name: newObj.Name}
	return retryOnConflict(func() error {
		var existingObj vmv1beta1.VMPodScrape
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating VMPodScrape=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return err
		}
		if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(newObj.Spec, existingObj.Spec) &&
			equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
			equality.Semantic.DeepEqual(newObj.Annotations, existingObj.Annotations) {
			return nil
		}
		// TODO: @f41gh7 allow 3rd party applications to add annotations for generated VMPodScrape
		specDiff := diffDeep(newObj.Spec, existingObj.Spec)
		existingObj.Annotations = newObj.Annotations
		existingObj.Spec = newObj.Spec
		existingObj.Labels = newObj.Labels
		logMsg := fmt.Sprintf("updating VMPodScrape=%s for CRD object spec_diff: %s", nsn, specDiff)
		logger.WithContext(ctx).Info(logMsg)

		return rclient.Update(ctx, &existingObj)
	})
}
