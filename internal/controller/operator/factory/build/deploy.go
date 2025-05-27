package build

import (
	"context"
	"fmt"
	"iter"
	"strconv"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	shardNumPlaceholder = "%SHARD_NUM%"
)

// To save compatibility in the single-shard version still need to fill in %SHARD_NUM% placeholder
var defaultPlaceholders = map[string]string{shardNumPlaceholder: "0"}

func CreateOrUpdateShardedDeploy(ctx context.Context, rclient client.Client, cr, prevCR shardOpts, newDeploy, prevDeploy runtime.Object) error {
	var err error
	statefulSetNames := make(map[string]struct{})
	deploymentNames := make(map[string]struct{})
	shardCount := cr.GetShardCount()
	prevShardCount := prevCR.GetShardCount()
	logger.WithContext(ctx).Info(fmt.Sprintf("using cluster version of %T with shards count=%d", cr, shardCount))

	isUpscaling := false
	if prevShardCount > 0 {
		if prevShardCount < shardCount {
			logger.WithContext(ctx).Info(fmt.Sprintf("%T shard upscaling from=%d to=%d", cr, prevShardCount, shardCount))
			isUpscaling = true
		}
	}
	for shardNum := range shardNumIter(isUpscaling, shardCount) {
		shardedDeploy := newDeploy.DeepCopyObject()
		var prevShardedObject runtime.Object
		cr.AddShardSettings(shardedDeploy, shardNum)
		if prevDeploy != nil {
			prevShardedObject = prevDeploy.DeepCopyObject()
			cr.AddShardSettings(prevShardedObject, shardNum)
		}
		placeholders := map[string]string{shardNumPlaceholder: strconv.Itoa(shardNum)}
		switch shardedDeploy := shardedDeploy.(type) {
		case *appsv1.Deployment:
			var prevDeploy *appsv1.Deployment
			shardedDeploy, err = k8stools.RenderPlaceholders(shardedDeploy, placeholders)
			if err != nil {
				return fmt.Errorf("cannot fill placeholders for Deployment sharded %T: %w", cr, err)
			}
			if prevShardedObject != nil {
				// prev object could be deployment due to switching from statefulmode
				prevObjApp, ok := prevShardedObject.(*appsv1.Deployment)
				if ok {
					prevDeploy = prevObjApp
					prevDeploy, err = k8stools.RenderPlaceholders(prevDeploy, placeholders)
					if err != nil {
						return fmt.Errorf("cannot fill placeholders for prev Deployment sharded %T: %w", cr, err)
					}
				}
			}
			if err := reconcile.Deployment(ctx, rclient, shardedDeploy, prevDeploy, false); err != nil {
				return err
			}
			deploymentNames[shardedDeploy.Name] = struct{}{}
		case *appsv1.StatefulSet:
			var prevSts *appsv1.StatefulSet
			shardedDeploy, err = k8stools.RenderPlaceholders(shardedDeploy, placeholders)
			if err != nil {
				return fmt.Errorf("cannot fill placeholders for StatefulSet in sharded %T: %w", cr, err)
			}
			if prevShardedObject != nil {
				// prev object could be deployment due to switching to statefulmode
				prevObjApp, ok := prevShardedObject.(*appsv1.StatefulSet)
				if ok {
					prevSts = prevObjApp
					prevSts, err = k8stools.RenderPlaceholders(prevSts, placeholders)
					if err != nil {
						return fmt.Errorf("cannot fill placeholders for prev StatefulSet in sharded %T: %w", cr, err)
					}
				}
			}
			stsOpts := reconcile.StatefulSetOptions{
				HasClaim: len(shardedDeploy.Spec.VolumeClaimTemplates) > 0,
				SelectorLabels: func() map[string]string {
					selectorLabels := cr.SelectorLabels()
					selectorLabels["shard-num"] = strconv.Itoa(shardNum)
					return selectorLabels
				},
			}
			if err := reconcile.HandleStatefulSetUpdate(ctx, rclient, stsOpts, shardedDeploy, prevSts); err != nil {
				return err
			}
			statefulSetNames[shardedDeploy.Name] = struct{}{}
		}
	}
	if err := finalize.RemoveOrphanedDeployments(ctx, rclient, cr, deploymentNames); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedStatefulSets(ctx, rclient, cr, statefulSetNames); err != nil {
		return err
	}
	return nil
}

func shardNumIter(backward bool, shardCount int) iter.Seq[int] {
	if backward {
		return func(yield func(int) bool) {
			for shardCount > 0 {
				shardCount--
				if !yield(shardCount) {
					return
				}
			}
		}
	}
	return func(yield func(int) bool) {
		for i := 0; i < shardCount; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func CreateOrUpdateDeploy(ctx context.Context, rclient client.Client, cr, _ deployOpts, newDeploy, prevObjectSpec runtime.Object) error {
	deploymentNames := make(map[string]struct{})
	statefulSetNames := make(map[string]struct{})
	var err error
	switch newDeploy := newDeploy.(type) {
	case *appsv1.Deployment:
		var prevDeploy *appsv1.Deployment
		if prevObjectSpec != nil {
			prevAppObject, ok := prevObjectSpec.(*appsv1.Deployment)
			if ok {
				prevDeploy = prevAppObject
				prevDeploy, err = k8stools.RenderPlaceholders(prevDeploy, defaultPlaceholders)
				if err != nil {
					return fmt.Errorf("cannot fill placeholders for prev Deployment in %T: %w", cr, err)
				}
			}
		}

		newDeploy, err = k8stools.RenderPlaceholders(newDeploy, defaultPlaceholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for Deployment in %T: %w", cr, err)
		}
		if err := reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false); err != nil {
			return err
		}
		deploymentNames[newDeploy.Name] = struct{}{}
	case *appsv1.StatefulSet:
		var prevStatefulSet *appsv1.StatefulSet
		if prevObjectSpec != nil {
			prevAppObject, ok := prevObjectSpec.(*appsv1.StatefulSet)
			if ok {
				prevStatefulSet = prevAppObject
				prevStatefulSet, err = k8stools.RenderPlaceholders(prevStatefulSet, defaultPlaceholders)
				if err != nil {
					return fmt.Errorf("cannot fill placeholders for prev StatefulSet in %T: %w", cr, err)
				}
			}
		}
		newDeploy, err = k8stools.RenderPlaceholders(newDeploy, defaultPlaceholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for StatefulSet in %T: %w", cr, err)
		}
		stsOpts := reconcile.StatefulSetOptions{
			HasClaim:       len(newDeploy.Spec.VolumeClaimTemplates) > 0,
			SelectorLabels: cr.SelectorLabels,
		}
		if err := reconcile.HandleStatefulSetUpdate(ctx, rclient, stsOpts, newDeploy, prevStatefulSet); err != nil {
			return err
		}
		statefulSetNames[newDeploy.Name] = struct{}{}
	case *appsv1.DaemonSet:
		var prevDeploy *appsv1.DaemonSet
		if prevObjectSpec != nil {
			prevAppObject, ok := prevObjectSpec.(*appsv1.DaemonSet)
			if ok {
				prevDeploy = prevAppObject
			}
		}
		if err := reconcile.DaemonSet(ctx, rclient, newDeploy, prevDeploy); err != nil {
			return err
		}
	default:
		panic(fmt.Sprintf("BUG: unexpected deploy object type: %T", newDeploy))
	}
	if err := finalize.RemoveOrphanedDeployments(ctx, rclient, cr, deploymentNames); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedStatefulSets(ctx, rclient, cr, statefulSetNames); err != nil {
		return err
	}
	return nil
}
