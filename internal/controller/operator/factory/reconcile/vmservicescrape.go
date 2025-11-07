package reconcile

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// VMServiceScrapeForCRD creates or updates given object
func VMServiceScrapeForCRD(ctx context.Context, rclient client.Client, vss *vmv1beta1.VMServiceScrape) error {
	return retryOnConflict(func() error {
		var existVSS vmv1beta1.VMServiceScrape
		err := rclient.Get(ctx, types.NamespacedName{Namespace: vss.Namespace, Name: vss.Name}, &existVSS)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating VMServiceScrape %s", vss.Name))
				return rclient.Create(ctx, vss)
			}
			return err
		}
		if !existVSS.DeletionTimestamp.IsZero() {
			return &errRecreate{
				origin: fmt.Errorf("waiting for servicescrape %q to be removed", vss.Name),
			}
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &existVSS); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(vss.Spec, existVSS.Spec) &&
			equality.Semantic.DeepEqual(vss.Labels, existVSS.Labels) &&
			equality.Semantic.DeepEqual(vss.Annotations, existVSS.Annotations) {
			return nil
		}
		// TODO: @f41gh7 allow 3rd party applications to add annotations for generated VMServiceScrape
		existVSS.Annotations = vss.Annotations
		existVSS.Spec = vss.Spec
		existVSS.Labels = vss.Labels
		logMsg := fmt.Sprintf("updating VMServiceScrape %s for CRD object spec_diff: %s", vss.Name, diffDeep(vss.Spec, existVSS.Spec))
		logger.WithContext(ctx).Info(logMsg)

		return rclient.Update(ctx, &existVSS)
	})
}

// VMPodScrapeForCRD creates or updates given object
func VMPodScrapeForCRD(ctx context.Context, rclient client.Client, vps *vmv1beta1.VMPodScrape) error {
	return retryOnConflict(func() error {
		var existVPS vmv1beta1.VMPodScrape
		err := rclient.Get(ctx, types.NamespacedName{Namespace: vps.Namespace, Name: vps.Name}, &existVPS)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating VMPodScrape %s", vps.Name))
				return rclient.Create(ctx, vps)
			}
			return err
		}
		if !existVPS.DeletionTimestamp.IsZero() {
			return &errRecreate{
				origin: fmt.Errorf("waiting for podscrape %q to be removed", vps.Name),
			}
		}
		if err := finalize.FreeIfNeeded(ctx, rclient, &existVPS); err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(vps.Spec, existVPS.Spec) &&
			equality.Semantic.DeepEqual(vps.Labels, existVPS.Labels) &&
			equality.Semantic.DeepEqual(vps.Annotations, existVPS.Annotations) {
			return nil
		}
		// TODO: @f41gh7 allow 3rd party applications to add annotations for generated VMPodScrape
		existVPS.Annotations = vps.Annotations
		existVPS.Spec = vps.Spec
		existVPS.Labels = vps.Labels
		logMsg := fmt.Sprintf("updating VMPodScrape %s for CRD object spec_diff: %s", vps.Name, diffDeep(vps.Spec, existVPS.Spec))
		logger.WithContext(ctx).Info(logMsg)

		return rclient.Update(ctx, &existVPS)
	})
}
