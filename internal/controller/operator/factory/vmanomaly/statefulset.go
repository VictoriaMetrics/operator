package vmanomaly

import (
	"context"
	"errors"
	"fmt"
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

	ac := getAssetsCache(ctx, rclient, cr)
	return createOrUpdateApp(ctx, rclient, cr, prevCR, ac)
}

func patchShardContainers(containers []corev1.Container, num *int, shardCount int) {
	if num == nil {
		return
	}
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
				Value: strconv.Itoa(*num),
			},
		}...)
		container.Env = envs
	}
}

// newK8sApp builds vmanomaly k8s app
func newK8sApp(cr *vmv1.VMAnomaly, configHash string, ac *build.AssetsCache) (*appsv1.StatefulSet, error) {
	podSpec, err := newPodSpec(cr, ac)
	if err != nil {
		return nil, err
	}
	shardCount := cr.GetShardCount()
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)
	podAnnotations := cr.PodAnnotations()
	if len(configHash) > 0 && !reloadSupported(cr) {
		podAnnotations = labels.Merge(podAnnotations, map[string]string{
			"checksum/config": configHash,
		})
	}

	app := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            build.ShardName(cr.PrefixedName(), shardCount),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: build.ShardLabels(cr.SelectorLabels(), shardCount),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.RollingUpdateStrategy,
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      build.ShardLabels(cr.PodLabels(), shardCount),
					Annotations: podAnnotations,
				},
				Spec: *podSpec,
			},
		},
	}
	if cr.Spec.PersistentVolumeClaimRetentionPolicy != nil {
		app.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.PersistentVolumeClaimRetentionPolicy
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
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMPodScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove podScrape: %w", err)
		}
	}
	return nil
}

func createOrUpdateApp(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly, ac *build.AssetsCache) error {
	configHash, err := createOrUpdateConfig(ctx, rclient, cr, prevCR, nil, ac)
	if err != nil {
		return err
	}

	var prevAppTpl *appsv1.StatefulSet

	if prevCR != nil {
		var err error
		prevAppTpl, err = newK8sApp(prevCR, configHash, ac)
		if err != nil {
			return fmt.Errorf("cannot build prev statefulSet for vmanomaly: %w", err)
		}
	}
	newAppTpl, err := newK8sApp(cr, configHash, ac)
	if err != nil {
		return fmt.Errorf("cannot build new statefulSet for vmanomaly: %w", err)
	}

	stsToKeep := make(map[string]struct{})
	pdbToKeep := make(map[string]struct{})
	shardCount := cr.GetShardCount()
	prevShardCount := prevCR.GetShardCount()
	logger.WithContext(ctx).Info(fmt.Sprintf("using cluster version of %T with shards count=%d", cr, shardCount))

	isUpscaling := false
	if prevShardCount > 1 && prevShardCount < shardCount {
		logger.WithContext(ctx).Info(fmt.Sprintf("%T shard upscaling from=%d to=%d", cr, prevShardCount, shardCount))
		isUpscaling = true
	}
	var wg sync.WaitGroup
	type returnValue struct {
		name string
		err  error
	}
	rtCh := make(chan *returnValue)
	shardCtx, cancel := context.WithCancel(ctx)
	updateShard := func(num *int) {
		var rv returnValue
		defer func() {
			rtCh <- &rv
			wg.Done()
		}()
		if cr.Spec.PodDisruptionBudget != nil {
			pdb := build.PodDisruptionBudgetSharded(cr, cr.Spec.PodDisruptionBudget, num)
			var prevPDB *policyv1.PodDisruptionBudget
			if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
				prevPDB = build.PodDisruptionBudgetSharded(prevCR, prevCR.Spec.PodDisruptionBudget, num)
			}
			if err := reconcile.PDB(ctx, rclient, pdb, prevPDB); err != nil {
				rv.err = err
				return
			}
		}

		newApp, err := build.RenderShard(newAppTpl, nil, num)
		if err != nil {
			rv.err = fmt.Errorf("failed to render new StatefulSet: %w", err)
			return
		}
		patchShardContainers(newApp.Spec.Template.Spec.Containers, num, shardCount)
		var prevApp *appsv1.StatefulSet
		if prevAppTpl != nil {
			prevApp, err = build.RenderShard(prevAppTpl, nil, num)
			if err != nil {
				rv.err = fmt.Errorf("failed to get prev StatefulSet: %w", err)
				return
			}
			patchShardContainers(prevApp.Spec.Template.Spec.Containers, num, shardCount)
		}
		statefulSetOpts := reconcile.STSOptions{
			HasClaim: len(newApp.Spec.VolumeClaimTemplates) > 0,
			SelectorLabels: func() map[string]string {
				return newApp.Spec.Selector.MatchLabels
			},
		}
		if err := reconcile.HandleSTSUpdate(shardCtx, rclient, statefulSetOpts, newApp, prevApp); err != nil {
			rv.err = err
			return
		}
		rv.name = newApp.Name
	}
	for shardNum := range build.ShardNumIter(isUpscaling, shardCount) {
		wg.Add(1)
		go updateShard(shardNum)
	}
	go func() {
		wg.Wait()
		close(rtCh)
		cancel()
	}()
	var errs []error
	for r := range rtCh {
		if r.err != nil {
			cancel()
			errs = append(errs, r.err)
		}
		if r.name != "" {
			stsToKeep[r.name] = struct{}{}
			if cr.Spec.PodDisruptionBudget != nil {
				pdbToKeep[r.name] = struct{}{}
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	if err := finalize.RemoveOrphanedPDBs(ctx, rclient, cr, pdbToKeep); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, stsToKeep); err != nil {
		return err
	}
	return nil
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}
