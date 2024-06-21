package reconcile

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/finalize"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceForCRD - reconcile needed and actual state of service for given crd,
// it will recreate service if needed.
// NOTE it doesn't perform validation:
// in case of spec.type= LoadBalancer or NodePort, clusterIP: None is not allowed,
// its users responsibility to define it correctly.
func ServiceForCRD(ctx context.Context, rclient client.Client, newService *v1.Service) error {
	// use a copy of service to avoid any side effects
	svcForReconcile := newService.DeepCopy()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return reconcileService(ctx, rclient, svcForReconcile)
	})
}

func reconcileService(ctx context.Context, rclient client.Client, newService *v1.Service) error {
	// helper for proper service deletion.
	handleDelete := func(svc *v1.Service) error {
		if err := finalize.RemoveFinalizer(ctx, rclient, svc); err != nil {
			return err
		}
		return finalize.SafeDelete(ctx, rclient, svc)
	}
	existingService := &v1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, existingService)
	if err != nil {
		if errors.IsNotFound(err) {
			// service not exists, creating it.
			err := rclient.Create(ctx, newService)
			if err != nil {
				return fmt.Errorf("cannot create new service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cannot get service for existing service: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, existingService); err != nil {
		return err
	}
	// lets save annotations and labels even after recreation.
	if newService.Spec.Type != existingService.Spec.Type {
		// type mismatch.
		// need to remove it and recreate.
		if err := handleDelete(existingService); err != nil {
			return err
		}
		// recursive call. operator reconciler must throttle it.
		return ServiceForCRD(ctx, rclient, newService)
	}
	// invariants.
	if newService.Spec.ClusterIP != "" && newService.Spec.ClusterIP != "None" && newService.Spec.ClusterIP != existingService.Spec.ClusterIP {
		// ip was changed by user, remove old service and create new one.
		if err := handleDelete(existingService); err != nil {
			return err
		}
		return ServiceForCRD(ctx, rclient, newService)
	}
	// existing service isn't None
	if newService.Spec.ClusterIP == "None" && existingService.Spec.ClusterIP != "None" {
		if err := handleDelete(existingService); err != nil {
			return err
		}
		return ServiceForCRD(ctx, rclient, newService)
	}
	// make service non-headless.
	if newService.Spec.ClusterIP == "" && existingService.Spec.ClusterIP == "None" {
		if err := handleDelete(existingService); err != nil {
			return err
		}
		return ServiceForCRD(ctx, rclient, newService)
	}
	// keep given clusterIP for service.
	if newService.Spec.ClusterIP != "None" {
		newService.Spec.ClusterIP = existingService.Spec.ClusterIP
	}

	// need to keep allocated node ports.
	if newService.Spec.Type == existingService.Spec.Type {
		// there is no need in optimization, it should be fast enough.
		for i := range existingService.Spec.Ports {
			existPort := existingService.Spec.Ports[i]
			for j := range newService.Spec.Ports {
				newPort := &newService.Spec.Ports[j]
				// add missing port, only if its not defined by user.
				if existPort.Name == newPort.Name && newPort.NodePort == 0 {
					newPort.NodePort = existPort.NodePort
					break
				}
			}
		}
	}
	if existingService.ResourceVersion != "" {
		newService.ResourceVersion = existingService.ResourceVersion
	}
	newService.Annotations = labels.Merge(existingService.Annotations, newService.Annotations)
	newService.Finalizers = vmv1beta1.MergeFinalizers(existingService, vmv1beta1.FinalizerName)

	err = rclient.Update(ctx, newService)
	if err != nil {
		return fmt.Errorf("cannot update vmalert server: %w", err)
	}

	return nil
}
