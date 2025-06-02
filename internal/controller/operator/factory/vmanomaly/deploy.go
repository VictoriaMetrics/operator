package vmanomaly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// CreateOrUpdate creates vmanomalyand and builds config for it
func CreateOrUpdate(ctx context.Context, cr *vmv1.VMAnomaly, rclient client.Client) error {
	var prevCR *vmv1.VMAnomaly
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if err := deletePrevStateResources(ctx, cr, rclient); err != nil {
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
		if ptr.Deref(cr.Spec.UseVMConfigReloader, false) {
			if err := createConfigSecretAccess(ctx, rclient, cr, prevCR); err != nil {
				return err
			}
		}
	}

	err := createOrUpdateConfig(ctx, rclient, cr, prevCR, nil)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMPodScrapeForCRD(ctx, rclient, build.VMPodScrapeForObjectWithSpec(cr, cr.Spec.ServiceScrapeSpec, cr.Spec.ExtraArgs))
		if err != nil {
			return err
		}
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

	var prevDeploy runtime.Object
	hash := sha256.New()
	hash.Write([]byte("bla"))
	hashBytes := hash.Sum(nil)
	configHash := hex.EncodeToString(hashBytes)

	if prevCR != nil {
		var err error
		prevDeploy, err = newDeploy(prevCR, configHash)
		if err != nil {
			return fmt.Errorf("cannot build new deploy for vmanomaly: %w", err)
		}
	}
	newDeploy, err := newDeploy(cr, configHash)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmanomaly: %w", err)
	}

	if cr.Spec.ShardCount != nil && *cr.Spec.ShardCount > 1 {
		return build.CreateOrUpdateShardedDeploy(ctx, rclient, cr, prevCR, newDeploy, prevDeploy)
	}
	return build.CreateOrUpdateDeploy(ctx, rclient, cr, prevCR, newDeploy, prevDeploy)
}

// newDeploy builds vmanomaly deployment spec.
func newDeploy(cr *vmv1.VMAnomaly, configHash string) (runtime.Object, error) {
	podSpec, err := newPodSpec(cr)
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

	if cr.Spec.StatefulMode {
		app := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cr.PrefixedName(),
				Namespace:       cr.Namespace,
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

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.UpdateStrategy
	}
	app := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.RollingUpdate,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.PodLabels(),
					Annotations: podAnnotations,
				},
				Spec: *podSpec,
			},
		},
	}
	build.DeploymentAddCommonParams(app, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
	app.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(app.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)

	return app, nil
}

func deletePrevStateResources(ctx context.Context, cr *vmv1.VMAnomaly, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && cr.ParsedLastAppliedSpec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}
