package reconcile

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Service - reconcile needed and actual state of service for given crd,
// it will recreate service if needed.
// NOTE it doesn't perform validation:
// in case of spec.type= LoadBalancer or NodePort, clusterIP: None is not allowed,
// its users responsibility to define it correctly.
func Service(ctx context.Context, rclient client.Client, newService, prevService *corev1.Service) error {
	svcForReconcile := newService.DeepCopy()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return reconcileService(ctx, rclient, svcForReconcile, prevService)
	})
}

func reconcileService(ctx context.Context, rclient client.Client, newService, prevService *corev1.Service) error {
	var isPrevServiceEqual bool
	if prevService != nil {
		isPrevServiceEqual = equality.Semantic.DeepDerivative(prevService, newService)
	}
	// helper for proper service deletion.
	recreateService := func(svc *corev1.Service) error {
		if err := finalize.RemoveFinalizer(ctx, rclient, svc); err != nil {
			return err
		}
		if err := finalize.SafeDelete(ctx, rclient, svc); err != nil {
			return fmt.Errorf("cannot delete service at recreate: %w", err)
		}
		if err := rclient.Create(ctx, newService); err != nil {
			return fmt.Errorf("cannot create service at recreate: %w", err)
		}
		return nil
	}
	existingService := &corev1.Service{}
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
	// invariants
	switch {
	case newService.Spec.Type != existingService.Spec.Type:
		// type mismatch.
		// need to remove it and recreate.
		return recreateService(existingService)
	case newService.Spec.ClusterIP != "" &&
		newService.Spec.ClusterIP != "None" &&
		newService.Spec.ClusterIP != existingService.Spec.ClusterIP:
		// ip was changed by user, remove old service and create new one.
		return recreateService(existingService)
	case newService.Spec.ClusterIP == "None" && existingService.Spec.ClusterIP != "None":
		// serviceType changed from clusterIP to headless
		return recreateService(existingService)
	case newService.Spec.ClusterIP == "" && existingService.Spec.ClusterIP == "None":
		// serviceType changes from headless to clusterIP
		return recreateService(existingService)
	}

	// keep given clusterIP for service.
	if newService.Spec.ClusterIP != "None" {
		newService.Spec.ClusterIP = existingService.Spec.ClusterIP
	}
	// keep allocated node ports.
	if newService.Spec.Type == existingService.Spec.Type {
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
	vmv1beta1.AddFinalizer(newService, existingService)

	rclient.Scheme().Default(newService)
	isEqual := equality.Semantic.DeepDerivative(newService.Spec, existingService.Spec)
	if isEqual &&
		isPrevServiceEqual &&
		equality.Semantic.DeepEqual(newService.Labels, existingService.Labels) &&
		equality.Semantic.DeepEqual(newService.Labels, existingService.Labels) {
		return nil
	}
	if prevService != nil {
		logger.WithContext(ctx).Info("updating service configuration",
			"is_current_equal", isEqual, "is_prev_equal", isPrevServiceEqual,
		)

	}

	err = rclient.Update(ctx, newService)
	if err != nil {
		return err
	}

	return nil
}

// AdditionalServices reconcile AdditionalServices
// by conditionally removing service from previous state
func AdditionalServices(ctx context.Context, rclient client.Client,
	defaultName, namespace string,
	prevSvc, currSvc *vmv1beta1.AdditionalServiceSpec) error {

	if currSvc == nil &&
		prevSvc != nil &&
		!prevSvc.UseAsDefault {
		// services was removed from previous state
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient,
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      prevSvc.NameOrDefault(defaultName)}}); err != nil {
			return fmt.Errorf("cannot remove additional service: %w", err)
		}
	}

	if currSvc != nil &&
		prevSvc != nil {
		// service name was changed
		// or service was marked as default
		if prevSvc.NameOrDefault(defaultName) != currSvc.NameOrDefault(defaultName) ||
			(!prevSvc.UseAsDefault && currSvc.UseAsDefault) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient,
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      prevSvc.NameOrDefault(defaultName)}}); err != nil {
				return fmt.Errorf("cannot remove additional service: %w", err)
			}
		}
	}
	return nil
}
