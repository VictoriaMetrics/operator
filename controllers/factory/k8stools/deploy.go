package k8stools

import (
	"context"
	"fmt"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func HandleDeployUpdate(ctx context.Context, rclient client.Client, newDeploy *appsv1.Deployment) error {

	var currentDeploy appsv1.Deployment
	err := rclient.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, &currentDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := rclient.Create(ctx, newDeploy); err != nil {
				return fmt.Errorf("cannot create new deployment for app: %s, err: %w", newDeploy.Name, err)
			}
			return nil
		}
		return fmt.Errorf("cannot get deployment for app: %s err: %w", newDeploy.Name, err)
	}
	newDeploy.Spec.Template.Annotations = MergeAnnotations(currentDeploy.Spec.Template.Annotations, newDeploy.Spec.Template.Annotations)
	newDeploy.Finalizers = victoriametricsv1beta1.MergeFinalizers(&currentDeploy, victoriametricsv1beta1.FinalizerName)
	newDeploy.Status = currentDeploy.Status
	newDeploy.Annotations = MergeAnnotations(currentDeploy.Annotations, newDeploy.Annotations)

	if err := rclient.Update(ctx, newDeploy); err != nil {
		return fmt.Errorf("cannot update deployment for app: %s, err: %w", newDeploy.Name, err)
	}

	return nil
}
