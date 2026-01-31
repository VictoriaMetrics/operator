package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func Service(ctx context.Context, rclient client.Client, newObj, prevObj *corev1.Service) error {
	svcForReconcile := newObj.DeepCopy()
	return retryOnConflict(func() error {
		return reconcileService(ctx, rclient, svcForReconcile, prevObj)
	})
}

func reconcileService(ctx context.Context, rclient client.Client, newObj, prevObj *corev1.Service) error {
	// helper for proper service deletion.
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	recreateService := func(svc *corev1.Service) error {
		if err := finalize.RemoveFinalizer(ctx, rclient, svc); err != nil {
			return err
		}
		if err := finalize.SafeDelete(ctx, rclient, svc); err != nil {
			return fmt.Errorf("cannot delete Service=%s at recreate: %w", nsn, err)
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("recreating new Service=%s", nsn))
		if err := rclient.Create(ctx, newObj); err != nil {
			return fmt.Errorf("cannot create Service=%s at recreate: %w", nsn, err)
		}
		return nil
	}
	var existingObj corev1.Service
	if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new Service=%s", nsn))
			err := rclient.Create(ctx, newObj)
			if err != nil {
				return fmt.Errorf("cannot create new Service=%s: %w", nsn, err)
			}
			return nil
		}
		return fmt.Errorf("cannot get service for existing Service=%s: %w", nsn, err)
	}
	if err := freeIfNeeded(ctx, rclient, &existingObj); err != nil {
		return err
	}
	var isPrevServiceEqual bool
	var prevSpecDiff string
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
		isPrevServiceEqual = equality.Semantic.DeepDerivative(prevObj, newObj)
		if !isPrevServiceEqual {
			prevSpecDiff = diffDeepDerivative(prevObj, newObj)
		}
		// keep LoadBalancerClass assigned by cloud-controller
		// See https://github.com/VictoriaMetrics/operator/issues/1550
		if prevObj.Spec.LoadBalancerClass == nil && newObj.Spec.LoadBalancerClass == nil {
			newObj.Spec.LoadBalancerClass = existingObj.Spec.LoadBalancerClass
		}
	}
	// invariants
	switch {
	case newObj.Spec.Type != existingObj.Spec.Type:
		// type mismatch.
		// need to remove it and recreate.
		return recreateService(&existingObj)
	case newObj.Spec.ClusterIP != "" &&
		newObj.Spec.ClusterIP != "None" &&
		newObj.Spec.ClusterIP != existingObj.Spec.ClusterIP:
		// ip was changed by user, remove old service and create new one.
		return recreateService(&existingObj)
	case newObj.Spec.ClusterIP == "None" && existingObj.Spec.ClusterIP != "None":
		// serviceType changed from clusterIP to headless
		return recreateService(&existingObj)
	case newObj.Spec.ClusterIP == "" && existingObj.Spec.ClusterIP == "None":
		// serviceType changes from headless to clusterIP
		return recreateService(&existingObj)
	case ptr.Deref(newObj.Spec.LoadBalancerClass, "") != ptr.Deref(existingObj.Spec.LoadBalancerClass, ""):
		return recreateService(&existingObj)
	}

	// keep given clusterIP for service.
	if newObj.Spec.ClusterIP != "None" {
		newObj.Spec.ClusterIP = existingObj.Spec.ClusterIP
	}
	// keep allocated node ports.
	if newObj.Spec.Type == existingObj.Spec.Type {
		for i := range existingObj.Spec.Ports {
			existPort := existingObj.Spec.Ports[i]
			for j := range newObj.Spec.Ports {
				newPort := &newObj.Spec.Ports[j]
				// add missing port, only if its not defined by user.
				if existPort.Name == newPort.Name && newPort.NodePort == 0 {
					newPort.NodePort = existPort.NodePort
					break
				}
			}
		}
	}

	rclient.Scheme().Default(newObj)
	isEqual := equality.Semantic.DeepDerivative(newObj.Spec, existingObj.Spec)
	if isEqual &&
		isPrevServiceEqual &&
		equality.Semantic.DeepEqual(newObj.Labels, existingObj.Labels) &&
		isObjectMetaEqual(&existingObj, newObj, prevMeta) {
		return nil
	}

	vmv1beta1.AddFinalizer(newObj, &existingObj)
	mergeObjectMetadataIntoNew(&existingObj, newObj, prevMeta)

	logMsg := fmt.Sprintf("updating service=%s configuration, is_current_equal=%v, is_prev_equal=%v, is_prev_nil=%v",
		nsn, isEqual, isPrevServiceEqual, prevObj == nil)

	if len(prevSpecDiff) > 0 {
		logMsg += fmt.Sprintf(", prev_spec_diff=%s", prevSpecDiff)
	}
	if !isEqual {
		logMsg += fmt.Sprintf(", curr_spec_diff=%s", diffDeepDerivative(newObj.Spec, existingObj.Spec))
	}
	logger.WithContext(ctx).Info(logMsg)

	return rclient.Update(ctx, newObj)
}
