package reconcile

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// VMServiceScrapeForCRD creates or updates given object
func VMServiceScrapeForCRD(ctx context.Context, rclient client.Client, newObj, prevObj *vmv1beta1.VMServiceScrape, owner *metav1.OwnerReference) error {
	if build.IsControllerDisabled("VMServiceScrape") {
		return nil
	}
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	return retryOnConflict(func() error {
		var existingObj vmv1beta1.VMServiceScrape
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating VMServiceScrape=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return err
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		if !metaChanged && equality.Semantic.DeepEqual(newObj.Spec, existingObj.Spec) {
			return nil
		}
		specDiff := diffDeep(newObj.Spec, existingObj.Spec)
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating VMServiceScrape=%s, spec_diff=%s", nsn, specDiff))
		return rclient.Update(ctx, &existingObj)
	})
}

// VMPodScrapeForCRD creates or updates given object
func VMPodScrapeForCRD(ctx context.Context, rclient client.Client, newObj, prevObj *vmv1beta1.VMPodScrape, owner *metav1.OwnerReference) error {
	if build.IsControllerDisabled("VMPodScrape") {
		return nil
	}
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}
	return retryOnConflict(func() error {
		var existingObj vmv1beta1.VMPodScrape
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating VMPodScrape=%s", nsn))
				return rclient.Create(ctx, newObj)
			}
			return err
		}
		if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner)
		if err != nil {
			return err
		}
		if !metaChanged && equality.Semantic.DeepEqual(newObj.Spec, existingObj.Spec) {
			return nil
		}
		specDiff := diffDeep(newObj.Spec, existingObj.Spec)
		existingObj.Spec = newObj.Spec
		logger.WithContext(ctx).Info(fmt.Sprintf("updating VMPodScrape=%s, spec_diff=%s", nsn, specDiff))
		return rclient.Update(ctx, &existingObj)
	})
}
