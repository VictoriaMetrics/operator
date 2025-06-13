package vmanomaly

import (
	"context"
	"fmt"
	"iter"
	"strconv"

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
		build.TLSResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSResourceKind, cr),
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

	var prevDeploy *appsv1.StatefulSet

	if prevCR != nil {
		var err error
		prevDeploy, err = newDeploy(prevCR, configHash, ac)
		if err != nil {
			return fmt.Errorf("cannot build new deploy for vmanomaly: %w", err)
		}
	}
	newDeploy, err := newDeploy(cr, configHash, ac)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmanomaly: %w", err)
	}

	if cr.Spec.ShardCount != nil && *cr.Spec.ShardCount > 1 {
		return createOrUpdateShardedDeploy(ctx, rclient, cr, prevCR, newDeploy, prevDeploy)
	}
	return createOrUpdateDeploy(ctx, rclient, cr, newDeploy, prevDeploy)
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
		envs := container.Env[:0]
		for _, env := range container.Env {
			if env.Name != "VMANOMALY_MEMBERS_COUNT" && env.Name != "VMANOMALY_MEMBER_NUM" {
				envs = append(envs, env)
			}
		}
		container.Env = append(container.Env, []corev1.EnvVar{
			{
				Name:  "VMANOMALY_MEMBERS_COUNT",
				Value: strconv.Itoa(shardCount),
			},
			{
				Name:  "VMANOMALY_MEMBER_NUM",
				Value: strconv.Itoa(shardNum),
			},
		}...)
	}
}

// newDeploy builds vmanomaly deployment spec.
func newDeploy(cr *vmv1.VMAnomaly, configHash string, ac *build.AssetsCache) (*appsv1.StatefulSet, error) {
	podSpec, err := newPodSpec(cr, ac)
	if err != nil {
		return nil, err
	}
	ac.InjectAssetsIntoPodSpec(podSpec)
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)
	podAnnotations := cr.PodAnnotations()
	if len(configHash) > 0 {
		podAnnotations = labels.Merge(podAnnotations, map[string]string{
			"checksum/config": configHash,
		})
	}

	app := &appsv1.StatefulSet{
		ObjectMeta: build.ResourceMeta(build.DefaultResourceKind, cr),
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.StatefulRollingUpdateStrategy,
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
	app.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(app.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)
	cr.Spec.StatefulStorage.IntoSTSVolume(cr.GetVolumeName(), &app.Spec)
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

func createOrUpdateShardedDeploy(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly, newDeploy, prevDeploy *appsv1.StatefulSet) error {
	var err error
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
	for shardNum := range shardNumIter(isUpscaling, shardCount) {
		shardedDeploy := newDeploy.DeepCopyObject().(*appsv1.StatefulSet)
		var prevShardedObject *appsv1.StatefulSet
		shardMutator(cr, shardedDeploy, shardNum)
		if prevDeploy != nil {
			prevShardedObject = prevDeploy.DeepCopyObject().(*appsv1.StatefulSet)
			shardMutator(cr, prevShardedObject, shardNum)
		}
		placeholders := map[string]string{shardNumPlaceholder: strconv.Itoa(shardNum)}
		var prevStatefulSet *appsv1.StatefulSet
		shardedDeploy, err = k8stools.RenderPlaceholders(shardedDeploy, placeholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for StatefulSet in sharded %T: %w", cr, err)
		}
		if prevShardedObject != nil {
			prevStatefulSet, err = k8stools.RenderPlaceholders(prevStatefulSet, placeholders)
			if err != nil {
				return fmt.Errorf("cannot fill placeholders for prev StatefulSet in sharded %T: %w", cr, err)
			}
		}
		statefulSetOpts := reconcile.STSOptions{
			HasClaim: len(shardedDeploy.Spec.VolumeClaimTemplates) > 0,
			SelectorLabels: func() map[string]string {
				selectorLabels := cr.SelectorLabels()
				selectorLabels["shard-num"] = strconv.Itoa(shardNum)
				return selectorLabels
			},
		}
		if err := reconcile.HandleSTSUpdate(ctx, rclient, statefulSetOpts, shardedDeploy, prevStatefulSet); err != nil {
			return err
		}
		statefulSetNames[shardedDeploy.Name] = struct{}{}
	}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, statefulSetNames); err != nil {
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

func createOrUpdateDeploy(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly, newDeploy, prevObjectSpec *appsv1.StatefulSet) error {
	var err error
	statefulSetNames := make(map[string]struct{})
	if prevObjectSpec != nil {
		prevObjectSpec, err = k8stools.RenderPlaceholders(prevObjectSpec, defaultPlaceholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for prev StatefulSet in %T: %w", cr, err)
		}
	}
	newDeploy, err = k8stools.RenderPlaceholders(newDeploy, defaultPlaceholders)
	if err != nil {
		return fmt.Errorf("cannot fill placeholders for StatefulSet in %T: %w", cr, err)
	}
	opts := reconcile.STSOptions{
		HasClaim:       len(newDeploy.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.SelectorLabels,
	}
	if err := reconcile.HandleSTSUpdate(ctx, rclient, opts, newDeploy, prevObjectSpec); err != nil {
		return err
	}
	statefulSetNames[newDeploy.Name] = struct{}{}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, statefulSetNames); err != nil {
		return err
	}
	return nil
}
