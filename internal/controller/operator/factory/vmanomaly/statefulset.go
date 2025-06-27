package vmanomaly

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strconv"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// CreateOrUpdate creates vmanomalyand and builds config for it
func CreateOrUpdate(ctx context.Context, cr *vmv1.VMAnomaly, rclient client.Client) error {
	var prevCR *vmv1.VMAnomaly
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if err := deletePrevStateResources(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot delete objects from prev state: %w", err)
	}
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMPodScrapeForCRD(ctx, rclient, build.VMPodScrapeForObjectWithSpec(cr, cr.Spec.ServiceScrapeSpec, cr.Spec.ExtraArgs))
		if err != nil {
			return err
		}
	}

	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	ac := build.NewAssetsCache(ctx, rclient, cfg)

	configHash, err := createOrUpdateConfig(ctx, rclient, cr, prevCR, ac)
	if err != nil {
		return err
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB); err != nil {
			return err
		}
	}

	var prevStatefulSet *appsv1.StatefulSet

	if prevCR != nil {
		var err error
		prevStatefulSet, err = newStatefulSet(prevCR, configHash, ac)
		if err != nil {
			return fmt.Errorf("cannot build prev statefulSet for vmanomaly: %w", err)
		}
	}
	newStatefulSet, err := newStatefulSet(cr, configHash, ac)
	if err != nil {
		return fmt.Errorf("cannot build new statefulSet for vmanomaly: %w", err)
	}

	if cr.GetShardCount() > 1 {
		return createOrUpdateShardedStatefulSet(ctx, rclient, cr, prevCR, newStatefulSet, prevStatefulSet)
	}
	return createOrUpdateStatefulSet(ctx, rclient, cr, newStatefulSet, prevStatefulSet)
}

func shardMutator(cr *vmv1.VMAnomaly, app *appsv1.StatefulSet, shardNum int) {
	if cr == nil || cr.Spec.ShardCount == nil {
		return
	}
	shardCount := *cr.Spec.ShardCount
	var containers = app.Spec.Template.Spec.Containers
	app.Name = fmt.Sprintf("%s-%d", app.Name, shardNum)
	app.Spec.Selector.MatchLabels["shard-num"] = strconv.Itoa(shardNum)
	app.Spec.Template.Labels["shard-num"] = strconv.Itoa(shardNum)
	for i := range containers {
		container := &containers[i]
		if container.Name != "vmanomaly" {
			continue
		}
		// filter any env with the shard configuration name
		envs := container.Env[:0]
		for _, env := range container.Env {
			if env.Name != "VMANOMALY_MEMBERS_COUNT" && env.Name != "VMANOMALY_MEMBER_NUM" {
				envs = append(envs, env)
			}
		}
		envs = append(envs, []corev1.EnvVar{
			{
				Name:  "VMANOMALY_MEMBERS_COUNT",
				Value: strconv.Itoa(shardCount),
			},
			{
				Name:  "VMANOMALY_MEMBER_NUM",
				Value: strconv.Itoa(shardNum),
			},
		}...)
		container.Env = envs
	}
}

// newStatefulSet builds vmanomaly statefulSet
func newStatefulSet(cr *vmv1.VMAnomaly, configHash string, ac *build.AssetsCache) (*appsv1.StatefulSet, error) {
	podSpec, err := newPodSpec(cr, ac)
	if err != nil {
		return nil, err
	}
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)
	podAnnotations := cr.PodAnnotations()
	if len(configHash) > 0 {
		podAnnotations = labels.Merge(podAnnotations, map[string]string{
			"checksum/config": configHash,
		})
	}

	app := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.RollingUpdateStrategy,
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.PodLabels(),
					Annotations: podAnnotations,
				},
				Spec: *podSpec,
			},
		},
	}

	build.StatefulSetAddCommonParams(app, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
	app.Spec.Template.Spec.Volumes = append(app.Spec.Template.Spec.Volumes, cr.Spec.Volumes...)
	cr.Spec.Storage.IntoSTSVolume(cr.GetVolumeName(), &app.Spec)
	app.Spec.VolumeClaimTemplates = append(app.Spec.VolumeClaimTemplates, cr.Spec.ClaimTemplates...)
	return app, nil
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly) error {
	if prevCR == nil {
		return nil
	}
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && prevCR.Spec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMPodScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove podScrape: %w", err)
		}
	}

	return nil
}

const (
	shardNumPlaceholder = "%SHARD_NUM%"
)

// To save compatibility in the single-shard version still need to fill in %SHARD_NUM% placeholder
var defaultPlaceholders = map[string]string{shardNumPlaceholder: "0"}

func createOrUpdateShardedStatefulSet(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly, newStatefulSet, prevStatefulSet *appsv1.StatefulSet) error {
	statefulSetNames := make(map[string]struct{})
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
	var wg sync.WaitGroup
	type shardResult struct {
		name string
		err  error
	}
	resultChan := make(chan *shardResult)
	shardCtx, cancel := context.WithCancel(ctx)
	updateShard := func(num int) {
		defer wg.Done()
		newShardedApp, err := getShard(cr, newStatefulSet, num)
		if err != nil {
			resultChan <- &shardResult{
				err: fmt.Errorf("failed to get new StatefulSet: %w", err),
			}
			return
		}
		prevShardedApp, err := getShard(prevCR, prevStatefulSet, num)
		if err != nil {
			resultChan <- &shardResult{
				err: fmt.Errorf("failed to get prev StatefulSet: %w", err),
			}
			return
		}
		statefulSetOpts := reconcile.STSOptions{
			HasClaim: len(newShardedApp.Spec.VolumeClaimTemplates) > 0,
			SelectorLabels: func() map[string]string {
				selectorLabels := cr.SelectorLabels()
				selectorLabels["shard-num"] = strconv.Itoa(num)
				return selectorLabels
			},
		}
		if err := reconcile.HandleSTSUpdate(shardCtx, rclient, statefulSetOpts, newShardedApp, prevShardedApp); err != nil {
			resultChan <- &shardResult{
				err: err,
			}
			return
		}
		resultChan <- &shardResult{
			name: newShardedApp.Name,
		}
	}
	for shardNum := range shardNumIter(isUpscaling, shardCount) {
		wg.Add(1)
		go updateShard(shardNum)
	}
	go func() {
		wg.Wait()
		close(resultChan)
		cancel()
	}()
	var errs []error
	for r := range resultChan {
		if r.err != nil {
			cancel()
			errs = append(errs, r.err)
		}
		if r.name != "" {
			statefulSetNames[r.name] = struct{}{}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, statefulSetNames); err != nil {
		return err
	}
	return nil
}

func getShard(cr *vmv1.VMAnomaly, app *appsv1.StatefulSet, num int) (*appsv1.StatefulSet, error) {
	if app == nil {
		return nil, nil
	}
	shardedApp := app.DeepCopyObject().(*appsv1.StatefulSet)
	shardMutator(cr, shardedApp, num)
	placeholders := map[string]string{shardNumPlaceholder: strconv.Itoa(num)}
	shardedApp, err := k8stools.RenderPlaceholders(shardedApp, placeholders)
	if err != nil {
		return nil, fmt.Errorf("cannot fill placeholders for StatefulSet in sharded %T: %w", cr, err)
	}
	return shardedApp, nil
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

func createOrUpdateStatefulSet(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly, newStatefulSet, prevObjectSpec *appsv1.StatefulSet) error {
	var err error
	statefulSetNames := make(map[string]struct{})
	if prevObjectSpec != nil {
		prevObjectSpec, err = k8stools.RenderPlaceholders(prevObjectSpec, defaultPlaceholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for prev StatefulSet: %w", err)
		}
	}
	newStatefulSet, err = k8stools.RenderPlaceholders(newStatefulSet, defaultPlaceholders)
	if err != nil {
		return fmt.Errorf("cannot fill placeholders for StatefulSet: %w", err)
	}
	opts := reconcile.STSOptions{
		HasClaim:       len(newStatefulSet.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.SelectorLabels,
	}
	if err := reconcile.HandleSTSUpdate(ctx, rclient, opts, newStatefulSet, prevObjectSpec); err != nil {
		return err
	}
	statefulSetNames[newStatefulSet.Name] = struct{}{}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, statefulSetNames); err != nil {
		return err
	}
	return nil
}
