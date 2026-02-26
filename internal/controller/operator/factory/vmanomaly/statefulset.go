package vmanomaly

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func buildScrape(cr *vmv1.VMAnomaly) *vmv1beta1.VMPodScrape {
	if cr == nil || ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMPodScrape(cr, "monitoring-http")
}

// CreateOrUpdate creates vmanomalyand and builds config for it
func CreateOrUpdate(ctx context.Context, cr *vmv1.VMAnomaly, rclient client.Client) error {
	var prevCR *vmv1.VMAnomaly
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete orphaned resources: %w", err)
		}
	}
	owner := cr.AsOwner()
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA, &owner); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		svs := buildScrape(cr)
		prevSvs := buildScrape(prevCR)
		if err := reconcile.VMPodScrape(ctx, rclient, svs, prevSvs, &owner); err != nil {
			return err
		}
	}

	rcfg := map[build.ResourceKind]*build.ResourceCfg{
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	ac := build.NewAssetsCache(ctx, rclient, rcfg)
	configHash, err := createOrUpdateConfig(ctx, rclient, cr, prevCR, ac)
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
	return createOrUpdateApp(ctx, rclient, cr, prevCR, newAppTpl, prevAppTpl)
}

func patchShardContainers(containers []corev1.Container, shardNum, shardCount int) {
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

// newK8sApp builds vmanomaly StatefulSet
func newK8sApp(cr *vmv1.VMAnomaly, configHash string, ac *build.AssetsCache) (*appsv1.StatefulSet, error) {
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
			Name:            build.ShardName(cr),
			Namespace:       cr.GetNamespace(),
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: build.ShardSelectorLabels(cr),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.RollingUpdateStrategy,
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      build.ShardPodLabels(cr),
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

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) error {
	owner := cr.AsOwner()
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	keepPodScrapes := sets.New[string]()
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		keepPodScrapes.Insert(cr.PrefixedName())
	}
	if err := finalize.RemoveOrphanedVMPodScrapes(ctx, rclient, cr, keepPodScrapes); err != nil {
		return fmt.Errorf("cannot remove podScrapes: %w", err)
	}
	if !cr.IsOwnsServiceAccount() {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.ServiceAccount{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceaccount: %w", err)
		}
	}
	return nil
}

func createOrUpdateApp(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly, newAppTpl, prevAppTpl *appsv1.StatefulSet) error {
	stsToKeep := sets.New[string]()
	pdbToKeep := sets.New[string]()
	shardCount := cr.GetShardCount()
	prevShardCount := prevCR.GetShardCount()

	isUpscaling := false
	if prevCR.IsSharded() {
		if prevShardCount < shardCount {
			logger.WithContext(ctx).Info(fmt.Sprintf("%T shard upscaling from=%d to=%d", cr, prevShardCount, shardCount))
			isUpscaling = true
		} else {
			logger.WithContext(ctx).Info(fmt.Sprintf("%T shard downscaling from=%d to=%d", cr, prevShardCount, shardCount))
		}
	}

	var wg sync.WaitGroup
	type returnValue struct {
		name string
		err  error
	}
	rtCh := make(chan *returnValue)
	shardCtx, cancel := context.WithCancel(ctx)
	owner := cr.AsOwner()
	updateShard := func(num int) {
		var rv returnValue
		defer func() {
			rtCh <- &rv
			wg.Done()
		}()
		if cr.Spec.PodDisruptionBudget != nil {
			pdb := build.ShardPodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget, num)
			var prevPDB *policyv1.PodDisruptionBudget
			if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
				prevPDB = build.ShardPodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget, num)
			}
			if err := reconcile.PDB(ctx, rclient, pdb, prevPDB, &owner); err != nil {
				rv.err = err
				return
			}
		}
		newApp, err := getShard(cr, newAppTpl, num)
		if err != nil {
			rv.err = fmt.Errorf("failed to get new StatefulSet: %w", err)
			return
		}
		prevApp, err := getShard(prevCR, prevAppTpl, num)
		if err != nil {
			rv.err = fmt.Errorf("failed to get prev StatefulSet: %w", err)
			return
		}
		selectorLabels := maps.Clone(newApp.Spec.Selector.MatchLabels)
		opts := reconcile.STSOptions{
			HasClaim: len(newApp.Spec.VolumeClaimTemplates) > 0,
			SelectorLabels: func() map[string]string {
				return selectorLabels
			},
		}
		if err := reconcile.StatefulSet(shardCtx, rclient, opts, newApp, prevApp, &owner); err != nil {
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
			stsToKeep.Insert(r.name)
			if cr.Spec.PodDisruptionBudget != nil {
				pdbToKeep.Insert(r.name)
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

func getShard(cr *vmv1.VMAnomaly, appTpl *appsv1.StatefulSet, num int) (*appsv1.StatefulSet, error) {
	if appTpl == nil || !cr.IsSharded() {
		return appTpl, nil
	}
	app, err := build.RenderShard(appTpl, num)
	if err != nil {
		return nil, fmt.Errorf("cannot fill placeholders for StatefulSet in sharded %T: %w", cr, err)
	}
	patchShardContainers(app.Spec.Template.Spec.Containers, num, cr.GetShardCount())
	return app, nil
}
