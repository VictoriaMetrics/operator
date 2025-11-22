package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Service - reconcile needed and actual state of service for given crd,
// it will recreate service if needed.
// NOTE it doesn't perform validation:
// in case of spec.type= LoadBalancer or NodePort, clusterIP: None is not allowed,
// its users responsibility to define it correctly.
func Service(ctx context.Context, rclient client.Client, newService, prevService *corev1.Service) error {
	svcForReconcile := newService.DeepCopy()
	return retryOnConflict(func() error {
		return reconcileService(ctx, rclient, svcForReconcile, prevService)
	})
}

func reconcileService(ctx context.Context, rclient client.Client, newService, prevService *corev1.Service) error {
	// helper for proper service deletion.
	recreateService := func(svc *corev1.Service) error {
		if err := finalize.RemoveFinalizer(ctx, rclient, svc); err != nil {
			return err
		}
		if err := finalize.SafeDelete(ctx, rclient, svc); err != nil {
			return fmt.Errorf("cannot delete service at recreate: %w", err)
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("recreating new Service %s", newService.Name))
		if err := rclient.Create(ctx, newService); err != nil {
			return fmt.Errorf("cannot create service at recreate: %w", err)
		}
		return nil
	}
	currentService := &corev1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, currentService)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new Service %s", newService.Name))
			err := rclient.Create(ctx, newService)
			if err != nil {
				return fmt.Errorf("cannot create new service: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cannot get service for existing service: %w", err)
	}
	if !currentService.DeletionTimestamp.IsZero() {
		return &errRecreate{
			origin: fmt.Errorf("waiting for service %q to be removed", newService.Name),
		}
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, currentService); err != nil {
		return err
	}
	var isPrevServiceEqual bool
	var prevSpecDiff string
	if prevService != nil {
		isPrevServiceEqual = equality.Semantic.DeepDerivative(prevService, newService)
		if !isPrevServiceEqual {
			prevSpecDiff = diffDeepDerivative(prevService, newService)
		}
		// keep LoadBalancerClass assigned by cloud-controller
		// See https://github.com/VictoriaMetrics/operator/issues/1550
		if prevService.Spec.LoadBalancerClass == nil && newService.Spec.LoadBalancerClass == nil {
			newService.Spec.LoadBalancerClass = currentService.Spec.LoadBalancerClass
		}
	}
	// invariants
	switch {
	case newService.Spec.Type != currentService.Spec.Type:
		// type mismatch.
		// need to remove it and recreate.
		return recreateService(currentService)
	case newService.Spec.ClusterIP != "" &&
		newService.Spec.ClusterIP != "None" &&
		newService.Spec.ClusterIP != currentService.Spec.ClusterIP:
		// ip was changed by user, remove old service and create new one.
		return recreateService(currentService)
	case newService.Spec.ClusterIP == "None" && currentService.Spec.ClusterIP != "None":
		// serviceType changed from clusterIP to headless
		return recreateService(currentService)
	case newService.Spec.ClusterIP == "" && currentService.Spec.ClusterIP == "None":
		// serviceType changes from headless to clusterIP
		return recreateService(currentService)
	case ptr.Deref(newService.Spec.LoadBalancerClass, "") != ptr.Deref(currentService.Spec.LoadBalancerClass, ""):
		return recreateService(currentService)
	}

	// keep given clusterIP for service.
	if newService.Spec.ClusterIP != "None" {
		newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	}
	// keep allocated node ports.
	if newService.Spec.Type == currentService.Spec.Type {
		for i := range currentService.Spec.Ports {
			existPort := currentService.Spec.Ports[i]
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

	rclient.Scheme().Default(newService)
	isEqual := equality.Semantic.DeepDerivative(newService.Spec, currentService.Spec)
	if isEqual &&
		isPrevServiceEqual &&
		equality.Semantic.DeepEqual(newService.Labels, currentService.Labels) &&
		isObjectMetaEqual(currentService, newService, prevService) {
		return nil
	}

	vmv1beta1.AddFinalizer(newService, currentService)
	mergeObjectMetadataIntoNew(currentService, newService, prevService)

	logMsg := fmt.Sprintf("updating service %s configuration, is_current_equal=%v, is_prev_equal=%v, is_prev_nil=%v",
		newService.Name, isEqual, isPrevServiceEqual, prevService == nil)

	if len(prevSpecDiff) > 0 {
		logMsg += fmt.Sprintf(", prev_spec_diff=%s", prevSpecDiff)
	}
	if !isEqual {
		logMsg += fmt.Sprintf(", curr_spec_diff=%s", diffDeepDerivative(newService.Spec, currentService.Spec))
	}
	logger.WithContext(ctx).Info(logMsg)

	err = rclient.Update(ctx, newService)
	if err != nil {
		return err
	}

	return nil
}
