package reconcile

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// Service - reconcile needed and actual state of service for given crd,
// it will recreate service if needed.
// NOTE it doesn't perform validation:
// in case of spec.type= LoadBalancer or NodePort, clusterIP: None is not allowed,
// its users responsibility to define it correctly.
func Service(ctx context.Context, rclient client.Client, newObj, prevObj *corev1.Service, owner *metav1.OwnerReference) error {
	svcForReconcile := newObj.DeepCopy()
	return retryOnConflict(func() error {
		return reconcileService(ctx, rclient, svcForReconcile, prevObj, owner)
	})
}

func reconcileService(ctx context.Context, rclient client.Client, newObj, prevObj *corev1.Service, owner *metav1.OwnerReference) error {
	// helper for proper service deletion.
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	var existingObj corev1.Service
	if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Info(fmt.Sprintf("creating new Service=%s", nsn.String()))
			err := rclient.Create(ctx, newObj)
			if err != nil {
				return fmt.Errorf("cannot create new Service=%s: %w", nsn.String(), err)
			}
			return nil
		}
		return fmt.Errorf("cannot get service for existing Service=%s: %w", nsn.String(), err)
	}
	if err := collectGarbage(ctx, rclient, &existingObj); err != nil {
		return err
	}
	var prevMeta *metav1.ObjectMeta
	if prevObj != nil {
		prevMeta = &prevObj.ObjectMeta
	}

	spec := &newObj.Spec
	// keep LoadBalancerClass assigned by cloud-controller
	// See https://github.com/VictoriaMetrics/operator/issues/1550
	if spec.LoadBalancerClass == nil && (prevObj == nil || prevObj.Spec.LoadBalancerClass == nil) {
		spec.LoadBalancerClass = existingObj.Spec.LoadBalancerClass
	}

	recreateService := func() error {
		if err := finalize.RemoveFinalizer(ctx, rclient, &existingObj); err != nil {
			return err
		}
		if err := finalize.SafeDelete(ctx, rclient, &existingObj); err != nil {
			return fmt.Errorf("cannot delete Service=%s at recreate: %w", nsn.String(), err)
		}
		logger.WithContext(ctx).Info(fmt.Sprintf("recreating new Service=%s", nsn.String()))
		if err := rclient.Create(ctx, newObj); err != nil {
			return fmt.Errorf("cannot create Service=%s at recreate: %w", nsn.String(), err)
		}
		return nil
	}

	// invariants
	switch {
	case newObj.Spec.Type != existingObj.Spec.Type:
		// type mismatch.
		// need to remove it and recreate.
		return recreateService()
	case newObj.Spec.ClusterIP != "" &&
		newObj.Spec.ClusterIP != "None" &&
		newObj.Spec.ClusterIP != existingObj.Spec.ClusterIP:
		// ip was changed by user, remove old service and create new one.
		return recreateService()
	case newObj.Spec.ClusterIP == "None" && existingObj.Spec.ClusterIP != "None":
		// serviceType changed from clusterIP to headless
		return recreateService()
	case newObj.Spec.ClusterIP == "" && existingObj.Spec.ClusterIP == "None":
		// serviceType changes from headless to clusterIP
		return recreateService()
	case ptr.Deref(newObj.Spec.LoadBalancerClass, "") != ptr.Deref(existingObj.Spec.LoadBalancerClass, ""):
		return recreateService()
	}

	// keep given clusterIP for service.
	if spec.ClusterIP != "None" {
		spec.ClusterIP = existingObj.Spec.ClusterIP
	}

	// keep allocated node ports.
	if newObj.Spec.Type == existingObj.Spec.Type {
		for i := range existingObj.Spec.Ports {
			existPort := existingObj.Spec.Ports[i]
			for j := range spec.Ports {
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
	metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, true, false)
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
	logger.WithContext(ctx).Info(fmt.Sprintf("updating Service %s", strings.Join(logMessageMetadata, ", ")))
	return rclient.Update(ctx, &existingObj)
}
