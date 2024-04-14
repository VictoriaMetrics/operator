package reconcile

import (
	"context"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMServiceScrapeForCRD creates or updates given object
func VMServiceScrapeForCRD(ctx context.Context, rclient client.Client, vss *victoriametricsv1beta1.VMServiceScrape) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var existVSS victoriametricsv1beta1.VMServiceScrape
		err := rclient.Get(ctx, types.NamespacedName{Namespace: vss.Namespace, Name: vss.Name}, &existVSS)
		if err != nil {
			if errors.IsNotFound(err) {
				return rclient.Create(ctx, vss)
			}
			return err
		}
		updateIsNeeded := !equality.Semantic.DeepEqual(vss.Spec, existVSS.Spec) || !equality.Semantic.DeepEqual(vss.Labels, existVSS.Labels) || !equality.Semantic.DeepEqual(vss.Annotations, existVSS.Annotations)
		existVSS.Spec = vss.Spec
		existVSS.Labels = vss.Labels
		existVSS.Annotations = vss.Annotations
		if updateIsNeeded {
			return rclient.Update(ctx, &existVSS)
		}
		return nil
	})
}
