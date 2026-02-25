package vmagent

import (
	"context"
	"fmt"
	"maps"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/vmscrapes"
)

const (
	confDir                     = "/etc/vmagent/config"
	confOutDir                  = "/etc/vmagent/config_out"
	persistentQueueDir          = "/tmp/vmagent-remotewrite-data"
	persistentQueueSTSDir       = "/vmagent_pq/vmagent-remotewrite-data"
	persistentQueueMountName    = "persistent-queue-data"
	globalRelabelingName        = "global_relabeling.yaml"
	urlRelabelingName           = "url_relabeling-%d.yaml"
	globalAggregationConfigName = "global_aggregation.yaml"

	tlsAssetsDir          = "/etc/vmagent-tls/certs"
	scrapeGzippedFilename = "vmagent.yaml.gz"
	configFilename        = "vmagent.yaml"
	defaultMaxDiskUsage   = "1073741824"
)

func buildVMAgentServiceScrape(cr *vmv1beta1.VMAgent, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr)
}

func buildVMAgentPodScrape(cr *vmv1beta1.VMAgent) *vmv1beta1.VMPodScrape {
	if cr == nil || ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMPodScrape(cr, "http")
}

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil {
		prevSvc = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			if prevCR.Spec.StatefulMode {
				svc.Spec.ClusterIP = "None"
			}
			build.AppendInsertPortsToService(prevCR.Spec.InsertPorts, svc)
		})
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, cr.Spec.ServiceSpec)
	}
	svc := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		if cr.Spec.StatefulMode {
			svc.Spec.ClusterIP = "None"
		}
		build.AppendInsertPortsToService(cr.Spec.InsertPorts, svc)
	})
	owner := cr.AsOwner()
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, cr.Spec.ServiceSpec)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("vmagent additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmagent: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile service for vmagent: %w", err)
	}
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		var err error
		if cr.Spec.DaemonSetMode {
			svs := buildVMAgentPodScrape(cr)
			prevSvs := buildVMAgentPodScrape(prevCR)
			err = reconcile.VMPodScrape(ctx, rclient, svs, prevSvs, &owner)
		} else {
			svs := buildVMAgentServiceScrape(cr, svc)
			prevSvs := buildVMAgentServiceScrape(prevCR, prevSvc)
			err = reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner)
		}
		if err != nil {
			return fmt.Errorf("cannot create or update scrape object: %w", err)
		}
	}
	return nil
}

// CreateOrUpdate creates deployment for vmagent and configures it
// waits for healthy state
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	var prevCR *vmv1beta1.VMAgent
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete objects from prev state: %w", err)
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
		if !ptr.Deref(cr.Spec.IngestOnlyMode, false) || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
			if err := createK8sAPIAccess(ctx, rclient, cr, prevCR, config.IsClusterWideAccessAllowed()); err != nil {
				return fmt.Errorf("cannot create vmagent role and binding for it, err: %w", err)
			}
		}
	}

	if err := createOrUpdateService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}

	ac := getAssetsCache(ctx, rclient, cr)
	if err := createOrUpdateScrapeConfig(ctx, rclient, cr, prevCR, nil, ac); err != nil {
		return err
	}

	if err := createOrUpdateRelabelConfigsAssets(ctx, rclient, cr, prevCR, ac); err != nil {
		return fmt.Errorf("cannot update relabeling asset for vmagent: %w", err)
	}

	if err := createOrUpdateStreamAggrConfig(ctx, rclient, cr, prevCR, ac); err != nil {
		return fmt.Errorf("cannot update stream aggregation config for vmagent: %w", err)
	}

	newAppTpl, err := newK8sApp(cr, ac)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
	}

	var prevAppTpl client.Object

	if prevCR != nil {
		var err error
		prevAppTpl, err = newK8sApp(prevCR, ac)
		if err != nil {
			return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
		}
	}

	return createOrUpdateApp(ctx, rclient, cr, prevCR, newAppTpl, prevAppTpl)
}

func createOrUpdateApp(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, newAppTpl, prevAppTpl client.Object) error {
	deploymentToKeep := sets.New[string]()
	stsToKeep := sets.New[string]()
	pdbToKeep := sets.New[string]()
	shardCount := cr.GetShardCount()
	prevShardCount := prevCR.GetShardCount()

	isUpscaling := false
	if prevCR.IsSharded() {
		if prevShardCount < shardCount {
			logger.WithContext(ctx).Info(fmt.Sprintf("VMAgent shard upscaling from=%d to=%d", prevShardCount, shardCount))
			isUpscaling = true
		} else {
			logger.WithContext(ctx).Info(fmt.Sprintf("VMAgent shard downscaling from=%d to=%d", prevShardCount, shardCount))
		}
	}

	var wg sync.WaitGroup

	type returnValue struct {
		deploymentName string
		stsName        string
		err            error
	}
	rtCh := make(chan *returnValue)
	owner := cr.AsOwner()
	updateShard := func(shardNum int) {
		var rv returnValue
		defer func() {
			rtCh <- &rv
			wg.Done()
		}()

		if cr.Spec.PodDisruptionBudget != nil && !cr.Spec.DaemonSetMode {
			pdb := build.ShardPodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget, shardNum)
			var prevPDB *policyv1.PodDisruptionBudget
			if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
				prevPDB = build.ShardPodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget, shardNum)
			}
			if err := reconcile.PDB(ctx, rclient, pdb, prevPDB, &owner); err != nil {
				rv.err = err
				return
			}
		}

		switch newAppTpl := newAppTpl.(type) {
		case *appsv1.Deployment:
			newApp := newAppTpl
			var prevApp *appsv1.Deployment
			var err error
			if cr.IsSharded() {
				newApp, err = build.RenderShard(newAppTpl, shardNum)
				if err != nil {
					rv.err = fmt.Errorf("cannot fill placeholders for %T sharded vmagent(%d): %w", newAppTpl, shardNum, err)
					return
				}
				if !ptr.Deref(cr.Spec.IngestOnlyMode, false) {
					patchShardContainers(newApp.Spec.Template.Spec.Containers, shardNum, shardCount)
				}
			}
			if prevAppTpl != nil {
				// prev object could be deployment due to switching from statefulmode
				if prevObjApp, ok := prevAppTpl.(*appsv1.Deployment); ok {
					if prevCR.IsSharded() {
						var err error
						prevApp, err = build.RenderShard(prevObjApp, shardNum)
						if err != nil {
							rv.err = fmt.Errorf("cannot fill placeholders for prev %T sharded vmagent(%d): %w", newAppTpl, shardNum, err)
							return
						}
						if !ptr.Deref(prevCR.Spec.IngestOnlyMode, false) {
							patchShardContainers(prevApp.Spec.Template.Spec.Containers, shardNum, shardCount)
						}
					} else {
						prevApp = prevObjApp
					}
				}
			}

			if err := reconcile.Deployment(ctx, rclient, newApp, prevApp, false, &owner); err != nil {
				rv.err = fmt.Errorf("cannot reconcile deployment for vmagent(%d): %w", shardNum, err)
				return
			}
			rv.deploymentName = newApp.Name
		case *appsv1.StatefulSet:
			newApp := newAppTpl
			var prevApp *appsv1.StatefulSet
			var err error
			if cr.IsSharded() {
				newApp, err = build.RenderShard(newAppTpl, shardNum)
				if err != nil {
					rv.err = fmt.Errorf("cannot fill placeholders for %T in sharded vmagent(%d): %w", newAppTpl, shardNum, err)
					return
				}
				if !ptr.Deref(cr.Spec.IngestOnlyMode, false) {
					patchShardContainers(newApp.Spec.Template.Spec.Containers, shardNum, shardCount)
				}
			}
			if prevAppTpl != nil {
				// prev object could be deployment due to switching to statefulmode
				if prevObjApp, ok := prevAppTpl.(*appsv1.StatefulSet); ok {
					if prevCR.IsSharded() {
						var err error
						prevApp, err = build.RenderShard(prevObjApp, shardNum)
						if err != nil {
							rv.err = fmt.Errorf("cannot fill placeholders for prev %T in sharded vmagent(%d): %w", newAppTpl, shardNum, err)
							return
						}
						if !ptr.Deref(prevCR.Spec.IngestOnlyMode, false) {
							patchShardContainers(prevApp.Spec.Template.Spec.Containers, shardNum, shardCount)
						}
					} else {
						prevApp = prevObjApp
					}
				}
			}
			selectorLabels := maps.Clone(newApp.Spec.Selector.MatchLabels)
			opts := reconcile.STSOptions{
				HasClaim: len(newApp.Spec.VolumeClaimTemplates) > 0,
				SelectorLabels: func() map[string]string {
					return selectorLabels
				},
			}
			if err := reconcile.StatefulSet(ctx, rclient, opts, newApp, prevApp, &owner); err != nil {
				rv.err = fmt.Errorf("cannot reconcile %T for vmagent(%d): %w", newApp, shardNum, err)
				return
			}
			rv.stsName = newApp.Name
		case *appsv1.DaemonSet:
			newApp := newAppTpl
			var prevApp *appsv1.DaemonSet
			if prevAppTpl != nil {
				prevApp, _ = prevAppTpl.(*appsv1.DaemonSet)
			}
			if err := reconcile.DaemonSet(ctx, rclient, newApp, prevApp, &owner); err != nil {
				rv.err = fmt.Errorf("cannot reconcile daemonset for vmagent: %w", err)
				return
			}
		default:
			panic(fmt.Sprintf("BUG: unexpected deploy object type: %T", newAppTpl))
		}
	}

	for shardNum := range build.ShardNumIter(isUpscaling, shardCount) {
		wg.Add(1)
		go updateShard(shardNum)
	}

	go func() {
		wg.Wait()
		close(rtCh)
	}()

	for rt := range rtCh {
		if rt.err != nil {
			return rt.err
		}
		if rt.deploymentName != "" {
			deploymentToKeep.Insert(rt.deploymentName)
			if cr.Spec.PodDisruptionBudget != nil {
				pdbToKeep.Insert(rt.deploymentName)
			}
		}
		if rt.stsName != "" {
			stsToKeep.Insert(rt.stsName)
			if cr.Spec.PodDisruptionBudget != nil {
				pdbToKeep.Insert(rt.stsName)
			}
		}
	}

	if err := finalize.RemoveOrphanedPDBs(ctx, rclient, cr, pdbToKeep, true); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedDeployments(ctx, rclient, cr, deploymentToKeep, true); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, stsToKeep, true); err != nil {
		return err
	}
	return nil
}

// newK8sApp builds vmagent deployment spec.
func newK8sApp(cr *vmv1beta1.VMAgent, ac *build.AssetsCache) (client.Object, error) {

	podSpec, err := newPodSpec(cr, ac)
	if err != nil {
		return nil, err
	}

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	if cr.Spec.DaemonSetMode {
		dsSpec := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cr.PrefixedName(),
				Namespace:       cr.Namespace,
				Labels:          cr.FinalLabels(),
				Annotations:     cr.FinalAnnotations(),
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: cr.SelectorLabels(),
				},
				MinReadySeconds: cr.Spec.MinReadySeconds,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      cr.PodLabels(),
						Annotations: cr.PodAnnotations(),
					},
					Spec: *podSpec,
				},
			},
		}
		build.DaemonSetAddCommonParams(dsSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
		dsSpec.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(dsSpec.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)

		return dsSpec, nil
	}
	// fast path, use sts
	if cr.Spec.StatefulMode {
		stsSpec := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            build.ShardName(cr),
				Namespace:       cr.Namespace,
				Labels:          cr.FinalLabels(),
				Annotations:     cr.FinalAnnotations(),
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			},
			Spec: appsv1.StatefulSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: build.ShardSelectorLabels(cr),
				},
				UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
					Type: cr.Spec.StatefulRollingUpdateStrategy,
				},
				PodManagementPolicy: appsv1.ParallelPodManagement,
				ServiceName:         buildSTSServiceName(cr),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      build.ShardPodLabels(cr),
						Annotations: cr.PodAnnotations(),
					},
					Spec: *podSpec,
				},
			},
		}
		if cr.Spec.PersistentVolumeClaimRetentionPolicy != nil {
			stsSpec.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.PersistentVolumeClaimRetentionPolicy
		}
		build.StatefulSetAddCommonParams(stsSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
		stsSpec.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(stsSpec.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)
		cr.Spec.StatefulStorage.IntoSTSVolume(persistentQueueMountName, &stsSpec.Spec)
		stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.ClaimTemplates...)
		return stsSpec, nil
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.UpdateStrategy
	}
	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            build.ShardName(cr),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: build.ShardSelectorLabels(cr),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.RollingUpdate,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      build.ShardPodLabels(cr),
					Annotations: cr.PodAnnotations(),
				},
				Spec: *podSpec,
			},
		},
	}
	build.DeploymentAddCommonParams(depSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
	depSpec.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(depSpec.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)

	return depSpec, nil
}

func buildSTSServiceName(cr *vmv1beta1.VMAgent) string {
	// set service name for sts if additional service is headless
	if cr.Spec.ServiceSpec != nil &&
		!cr.Spec.ServiceSpec.UseAsDefault &&
		cr.Spec.ServiceSpec.Spec.ClusterIP == corev1.ClusterIPNone {
		return cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName())
	}
	// special case for sharded mode
	if cr.IsSharded() {
		return cr.PrefixedName()
	}
	return ""
}

func newPodSpec(cr *vmv1beta1.VMAgent, ac *build.AssetsCache) (*corev1.PodSpec, error) {
	var args []string

	if rwArgs, err := buildRemoteWriteArgs(cr, ac); err != nil {
		return nil, fmt.Errorf("failed to build remote write args: %w", err)
	} else {
		args = append(args, rwArgs...)
	}

	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}

	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	if cr.Spec.DaemonSetMode {
		envs = append(envs, corev1.EnvVar{
			Name: vmv1beta1.KubeNodeEnvName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		})
	}

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AppendInsertPorts(ports, cr.Spec.InsertPorts)

	var agentVolumeMounts []corev1.VolumeMount
	var crMounts []corev1.VolumeMount
	// mount data path any way, even if user changes its value
	// we cannot rely on value of remoteWriteSettings.
	pqMountPath := persistentQueueDir
	if cr.Spec.StatefulMode {
		pqMountPath = persistentQueueSTSDir
	}
	agentVolumeMounts = append(agentVolumeMounts,
		corev1.VolumeMount{
			Name:      persistentQueueMountName,
			MountPath: pqMountPath,
		},
	)
	agentVolumeMounts = append(agentVolumeMounts, cr.Spec.VolumeMounts...)

	var volumes []corev1.Volume
	// in case for sts, we have to use persistentVolumeClaimTemplate instead
	if !cr.Spec.StatefulMode {
		volumes = append(volumes, corev1.Volume{
			Name: persistentQueueMountName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	volumes = append(volumes, cr.Spec.Volumes...)

	if !ptr.Deref(cr.Spec.IngestOnlyMode, false) {
		args = append(args,
			fmt.Sprintf("-promscrape.config=%s", path.Join(confOutDir, configFilename)))

		// preserve order of volumes and volumeMounts
		// it must prevent vmagent restarts during operator version change
		volumes = append(volumes, corev1.Volume{
			Name: string(build.TLSAssetsResourceKind),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
				},
			},
		})

		volumes = append(volumes, corev1.Volume{
			Name: "config-out",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		volumes = append(volumes, corev1.Volume{
			Name: string(build.SecretConfigResourceKind),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: build.ResourceName(build.SecretConfigResourceKind, cr),
				},
			},
		})

		m := corev1.VolumeMount{
			Name:      "config-out",
			MountPath: confOutDir,
		}
		crMounts = append(crMounts, m)
		m.ReadOnly = true
		agentVolumeMounts = append(agentVolumeMounts, m)
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      string(build.TLSAssetsResourceKind),
			MountPath: tlsAssetsDir,
			ReadOnly:  true,
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      string(build.SecretConfigResourceKind),
			MountPath: confDir,
			ReadOnly:  true,
		})

	}
	mountsLen := len(agentVolumeMounts)
	volumes, agentVolumeMounts = build.StreamAggrVolumeTo(volumes, agentVolumeMounts, cr)
	volumes, agentVolumeMounts = build.RelabelVolumeTo(volumes, agentVolumeMounts, cr)
	crMounts = append(crMounts, agentVolumeMounts[mountsLen:]...)
	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		cvm := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		}
		agentVolumeMounts = append(agentVolumeMounts, cvm)
		crMounts = append(crMounts, cvm)
	}

	volumes, agentVolumeMounts = build.LicenseVolumeTo(volumes, agentVolumeMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	relabelKeys := []string{globalRelabelingName}
	relabelConfigs := []*vmv1beta1.CommonRelabelParams{&cr.Spec.CommonRelabelParams}
	args = build.RelabelArgsTo(args, "remoteWrite.relabelConfig", relabelKeys, relabelConfigs...)

	streamAggrKeys := []string{globalAggregationConfigName}
	streamAggrConfigs := []*vmv1beta1.StreamAggrConfig{cr.Spec.StreamAggrConfig}
	args = build.StreamAggrArgsTo(args, "streamAggr", streamAggrKeys, streamAggrConfigs...)

	args = build.AppendArgsForInsertPorts(args, cr.Spec.InsertPorts)
	args = build.AddHTTPShutdownDelayArg(args, cr.Spec.ExtraArgs, cr.Spec.TerminationGracePeriodSeconds, cr.ParsedLastAppliedSpec == nil)
	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)

	vmagentContainer := corev1.Container{
		Name:                     "vmagent",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		VolumeMounts:             agentVolumeMounts,
		Resources:                cr.Spec.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	build.AddServiceAccountTokenVolumeMount(&vmagentContainer, cr.AutomountServiceAccountToken())
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	vmagentContainer = build.Probe(vmagentContainer, cr)

	build.AddConfigReloadAuthKeyToApp(&vmagentContainer, cr.Spec.ExtraArgs, &cr.Spec.CommonConfigReloaderParams)

	var operatorContainers []corev1.Container
	var ic []corev1.Container
	// conditional add config reloader container
	if !ptr.Deref(cr.Spec.IngestOnlyMode, false) || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
		ss := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: cr.PrefixedName(),
			},
			Key: configFilename,
		}
		configReloader := build.ConfigReloaderContainer(false, cr, crMounts, ss)
		operatorContainers = append(operatorContainers, configReloader)
		if !ptr.Deref(cr.Spec.IngestOnlyMode, false) {
			ic = append(ic, build.ConfigReloaderContainer(true, cr, crMounts, ss))
			build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, ic, useStrictSecurity)
		}
	}
	var err error
	ic, err = k8stools.MergePatchContainers(ic, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
	}

	operatorContainers = append([]corev1.Container{vmagentContainer}, operatorContainers...)

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, operatorContainers, useStrictSecurity)

	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.TopologySpreadConstraints {
		if cr.Spec.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			}
		}
	}
	volumes = build.AddConfigReloadAuthKeyVolume(volumes, &cr.Spec.CommonConfigReloaderParams)

	return &corev1.PodSpec{
		Volumes:            volumes,
		InitContainers:     ic,
		Containers:         containers,
		ServiceAccountName: cr.GetServiceAccountName(),
	}, nil
}

func patchShardContainers(containers []corev1.Container, shardNum, shardCount int) {
	for i := range containers {
		container := &containers[i]
		if container.Name == "vmagent" {
			args := container.Args
			// filter extraArgs defined by user
			cnt := 0
			for i := range args {
				arg := args[i]
				if !strings.Contains(arg, "promscrape.cluster.membersCount") && !strings.Contains(arg, "promscrape.cluster.memberNum") {
					args[cnt] = arg
					cnt++
				}
			}
			args = args[:cnt]
			args = append(args, fmt.Sprintf("-promscrape.cluster.membersCount=%d", shardCount))
			args = append(args, fmt.Sprintf("-promscrape.cluster.memberNum=%d", shardNum))
			container.Args = args
		}
	}
}

// buildRelabelingsAssets combines all possible relabeling config configuration and adding it to the configmap.
func buildRelabelingsAssets(cr *vmv1beta1.VMAgent, ac *build.AssetsCache) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: build.ResourceMeta(build.RelabelConfigResourceKind, cr),
		Data:       make(map[string]string),
	}
	// global section
	if len(cr.Spec.InlineRelabelConfig) > 0 {
		rcs := vmscrapes.AddRelabelConfigs(nil, cr.Spec.InlineRelabelConfig)
		data, err := yaml.Marshal(rcs)
		if err != nil {
			return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
		}
		if len(data) > 0 {
			cfgCM.Data[globalRelabelingName] = string(data)
		}
	}
	if cr.Spec.RelabelConfig != nil {
		// need to fetch content from
		data, err := ac.LoadKeyFromConfigMap(cr.Namespace, cr.Spec.RelabelConfig)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", cr.Spec.RelabelConfig.Name, err)
		}
		if len(data) > 0 {
			cfgCM.Data[globalRelabelingName] += data
		}
	}
	// per remoteWrite section.
	for i := range cr.Spec.RemoteWrite {
		rw := cr.Spec.RemoteWrite[i]
		if len(rw.InlineUrlRelabelConfig) > 0 {
			rcs := vmscrapes.AddRelabelConfigs(nil, rw.InlineUrlRelabelConfig)
			data, err := yaml.Marshal(rcs)
			if err != nil {
				return nil, fmt.Errorf("cannot serialize urlRelabelConfig as yaml: %w", err)
			}
			if len(data) > 0 {
				cfgCM.Data[fmt.Sprintf(urlRelabelingName, i)] = string(data)
			}
		}
		if rw.UrlRelabelConfig != nil {
			data, err := ac.LoadKeyFromConfigMap(cr.Namespace, rw.UrlRelabelConfig)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", rw.UrlRelabelConfig.Name, err)
			}
			if len(data) > 0 {
				cfgCM.Data[fmt.Sprintf(urlRelabelingName, i)] += data
			}
		}
	}
	return cfgCM, nil
}

// createOrUpdateRelabelConfigsAssets builds relabeling configs for vmagent at separate configmap, serialized as yaml
func createOrUpdateRelabelConfigsAssets(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, ac *build.AssetsCache) error {
	if !cr.HasAnyRelabellingConfigs() {
		return nil
	}
	assetsCM, err := buildRelabelingsAssets(cr, ac)
	if err != nil {
		return err
	}
	var prevConfigMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevConfigMeta = ptr.To(build.ResourceMeta(build.RelabelConfigResourceKind, prevCR))
	}
	owner := cr.AsOwner()
	_, err = reconcile.ConfigMap(ctx, rclient, assetsCM, prevConfigMeta, &owner)
	return err
}

// buildStreamAggrConfig combines all possible stream aggregation configs and adding it to the configmap.
func buildStreamAggrConfig(cr *vmv1beta1.VMAgent, ac *build.AssetsCache) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: build.ResourceMeta(build.StreamAggrConfigResourceKind, cr),
		Data:       make(map[string]string),
	}
	// global section
	if cr.Spec.StreamAggrConfig != nil {
		if len(cr.Spec.StreamAggrConfig.Rules) > 0 {
			data, err := yaml.Marshal(cr.Spec.StreamAggrConfig.Rules)
			if err != nil {
				return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
			}
			if len(data) > 0 {
				cfgCM.Data[globalAggregationConfigName] = string(data)
			}
		}
		if cr.Spec.StreamAggrConfig.RuleConfigMap != nil {
			data, err := ac.LoadKeyFromConfigMap(cr.Namespace, cr.Spec.StreamAggrConfig.RuleConfigMap)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", cr.Spec.StreamAggrConfig.RuleConfigMap.Name, err)
			}
			if len(data) > 0 {
				cfgCM.Data[globalAggregationConfigName] += data
			}
		}
	}

	for i := range cr.Spec.RemoteWrite {
		rw := cr.Spec.RemoteWrite[i]
		if rw.StreamAggrConfig != nil {
			c := rw.StreamAggrConfig
			if len(c.Rules) > 0 {
				data, err := yaml.Marshal(c.Rules)
				if err != nil {
					return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
				}
				if len(data) > 0 {
					cfgCM.Data[rw.AsConfigMapKey(i, "stream-aggr-conf")] = string(data)
				}
			}
			if c.RuleConfigMap != nil {
				data, err := ac.LoadKeyFromConfigMap(cr.Namespace, c.RuleConfigMap)
				if err != nil {
					return nil, err
				}
				if len(data) > 0 {
					cfgCM.Data[rw.AsConfigMapKey(i, "stream-aggr-conf")] += data
				}
			}

		}

	}
	return cfgCM, nil
}

// createOrUpdateStreamAggrConfig builds stream aggregation configs for vmagent at separate configmap, serialized as yaml
func createOrUpdateStreamAggrConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, ac *build.AssetsCache) error {
	// fast path
	if !cr.HasAnyStreamAggrRule() {
		return nil
	}
	streamAggrCM, err := buildStreamAggrConfig(cr, ac)
	if err != nil {
		return err
	}
	var prevConfigMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevConfigMeta = ptr.To(build.ResourceMeta(build.StreamAggrConfigResourceKind, cr))
	}
	owner := cr.AsOwner()
	_, err = reconcile.ConfigMap(ctx, rclient, streamAggrCM, prevConfigMeta, &owner)
	return err
}

type item struct {
	key, value string
}

func sortMap(m map[string]string) []item {
	var kv []item
	for k, v := range m {
		kv = append(kv, item{key: k, value: v})
	}
	sort.Slice(kv, func(i, j int) bool {
		return kv[i].key < kv[j].key
	})
	return kv
}

func buildRemoteWriteArgs(cr *vmv1beta1.VMAgent, ac *build.AssetsCache) ([]string, error) {
	maxDiskUsage := defaultMaxDiskUsage
	if cr.Spec.RemoteWriteSettings != nil && cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL != nil {
		maxDiskUsage = cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL.String()
	}

	var args []string
	var hasAnyDiskUsagesSet bool
	var storageLimit int64

	pqMountPath := persistentQueueDir
	if cr.Spec.StatefulMode {
		pqMountPath = persistentQueueSTSDir
		if cr.Spec.StatefulStorage != nil {
			if storage, ok := cr.Spec.StatefulStorage.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				storageInt, ok := storage.AsInt64()
				if ok {
					storageLimit = storageInt
				}
			}
		}
	}

	if len(cr.Spec.RemoteWrite) > 0 {
		remoteTargets := cr.Spec.RemoteWrite
		url := build.NewFlag("-remoteWrite.url", "")
		authUser := build.NewFlag("-remoteWrite.basicAuth.username", `""`)
		authPasswordFile := build.NewFlag("-remoteWrite.basicAuth.passwordFile", "")
		bearerTokenFile := build.NewFlag("-remoteWrite.bearerTokenFile", `""`)
		sendTimeout := build.NewFlag("-remoteWrite.sendTimeout", "")
		tlsCAs := build.NewFlag("-remoteWrite.tlsCAFile", "")
		tlsCerts := build.NewFlag("-remoteWrite.tlsCertFile", "")
		tlsKeys := build.NewFlag("-remoteWrite.tlsKeyFile", "")
		tlsInsecure := build.NewFlag("-remoteWrite.tlsInsecureSkipVerify", "false")
		tlsServerName := build.NewFlag("-remoteWrite.tlsServerName", "")
		oauth2ClientID := build.NewFlag("-remoteWrite.oauth2.clientID", "")
		oauth2ClientSecretFile := build.NewFlag("-remoteWrite.oauth2.clientSecretFile", "")
		oauth2Scopes := build.NewFlag("-remoteWrite.oauth2.scopes", "")
		oauth2TokenURL := build.NewFlag("-remoteWrite.oauth2.tokenUrl", "")
		headers := build.NewFlag("-remoteWrite.headers", "")
		forceVMProto := build.NewFlag("-remoteWrite.forceVMProto", "false")
		proxyURL := build.NewFlag("-remoteWrite.proxyURL", "")
		awsEC2Endpoint := build.NewFlag("-remoteWrite.aws.ec2Endpoint", "")
		awsRegion := build.NewFlag("-remoteWrite.aws.region", "")
		awsRoleARN := build.NewFlag("-remoteWrite.aws.roleARN", "")
		awsService := build.NewFlag("-remoteWrite.aws.service", "")
		awsSTSEndpoint := build.NewFlag("-remoteWrite.aws.stsEndpoint", "")
		awsUseSigv4 := build.NewFlag("-remoteWrite.aws.useSigv4", "false")

		var streamAggrConfigs []*vmv1beta1.StreamAggrConfig
		var streamAggrKeys []string

		var relabelConfigs []*vmv1beta1.CommonRelabelParams
		var relabelKeys []string

		var maxDiskUsagesPerRW []string

		if storageLimit > 0 && maxDiskUsage == defaultMaxDiskUsage {
			// conditionally change default value of maxDiskUsage
			// user defined value must have priority over automatically calculated.
			//
			// it's fine to have over-provisioing of total disk usage
			// however, we should return warning during validation.
			maxDiskUsage = strconv.FormatInt((storageLimit)/int64(len(remoteTargets)), 10)
		}
		for i, rw := range remoteTargets {
			url.Add(rw.URL, i)
			if rw.TLSConfig != nil {
				creds, err := ac.BuildTLSCreds(cr.Namespace, rw.TLSConfig)
				if err != nil {
					return nil, err
				}
				tlsCAs.Add(creds.CAFile, i)
				tlsCerts.Add(creds.CertFile, i)
				tlsKeys.Add(creds.KeyFile, i)
				if rw.TLSConfig.InsecureSkipVerify {
					tlsInsecure.Add("true", i)
				}
				tlsServerName.Add(rw.TLSConfig.ServerName, i)
			}
			if rw.BasicAuth != nil {
				user, err := ac.LoadKeyFromSecret(cr.Namespace, &rw.BasicAuth.Username)
				if err != nil {
					return nil, fmt.Errorf("cannot load BasicAuth username: %w", err)
				}
				authUser.Add(strconv.Quote(user), i)
				if len(rw.BasicAuth.Password.Name) > 0 {
					passFile, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, &rw.BasicAuth.Password)
					if err != nil {
						return nil, fmt.Errorf("cannot load BasicAuth password: %w", err)
					}
					authPasswordFile.Add(passFile, i)
				}
				if len(rw.BasicAuth.PasswordFile) > 0 {
					authPasswordFile.Add(rw.BasicAuth.PasswordFile, i)
				}
			}
			if rw.BearerTokenSecret != nil && rw.BearerTokenSecret.Name != "" {
				value, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, rw.BearerTokenSecret)
				if err != nil {
					return nil, fmt.Errorf("cannot load BearerTokenSecret: %w", err)
				}
				bearerTokenFile.Add(strconv.Quote(value), i)
			}
			var relabelConfig *vmv1beta1.CommonRelabelParams
			if rw.UrlRelabelConfig != nil || len(rw.InlineUrlRelabelConfig) > 0 {
				relabelConfig = &vmv1beta1.CommonRelabelParams{
					RelabelConfig:       rw.UrlRelabelConfig,
					InlineRelabelConfig: rw.InlineUrlRelabelConfig,
				}
			}
			relabelConfigs = append(relabelConfigs, relabelConfig)
			relabelKeys = append(relabelKeys, fmt.Sprintf(urlRelabelingName, i))
			if rw.SendTimeout != nil {
				sendTimeout.Add(*rw.SendTimeout, i)
			}
			if len(rw.Headers) > 0 {
				value := ""
				for _, headerValue := range rw.Headers {
					value += headerValue + "^^"
				}
				value = strings.TrimSuffix(value, "^^")
				headers.Add(value, i)
			}
			if rw.OAuth2 != nil {
				if len(rw.OAuth2.TokenURL) > 0 {
					oauth2TokenURL.Add(rw.OAuth2.TokenURL, i)
				}
				if len(rw.OAuth2.Scopes) > 0 {
					oauth2Scopes.Add(strings.Join(rw.OAuth2.Scopes, ","), i)
				}
				if len(rw.OAuth2.ClientSecretFile) > 0 {
					oauth2ClientSecretFile.Add(rw.OAuth2.ClientSecretFile, i)
				}
				if rw.OAuth2.ClientSecret != nil {
					oaSecretKeyFile, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, rw.OAuth2.ClientSecret)
					if err != nil {
						return nil, fmt.Errorf("cannot load OAuth2 ClientSecret: %w", err)
					}
					oauth2ClientSecretFile.Add(oaSecretKeyFile, i)
				}
				if len(rw.OAuth2.ClientID.PrefixedName()) > 0 {
					secret, err := ac.LoadKeyFromSecretOrConfigMap(cr.Namespace, &rw.OAuth2.ClientID)
					if err != nil {
						return nil, err
					}
					oauth2ClientID.Add(secret, i)
				}
			}
			if rw.AWS != nil {
				awsEC2Endpoint.Add(rw.AWS.EC2Endpoint, i)
				awsRegion.Add(rw.AWS.Region, i)
				awsRoleARN.Add(rw.AWS.RoleARN, i)
				awsService.Add(rw.AWS.Service, i)
				awsSTSEndpoint.Add(rw.AWS.STSEndpoint, i)
				awsUseSigv4.Add(strconv.FormatBool(rw.AWS.UseSigv4), i)
			}
			streamAggrConfigs = append(streamAggrConfigs, rw.StreamAggrConfig)
			streamAggrKeys = append(streamAggrKeys, rw.AsConfigMapKey(i, "stream-aggr-conf"))
			if rw.MaxDiskUsage != nil {
				maxDiskUsagesPerRW = append(maxDiskUsagesPerRW, rw.MaxDiskUsage.String())
				hasAnyDiskUsagesSet = true
			} else {
				maxDiskUsagesPerRW = append(maxDiskUsagesPerRW, maxDiskUsage)
			}
			forceVMProto.Add(strconv.FormatBool(rw.ForceVMProto), i)
			if rw.ProxyURL != nil {
				proxyURL.Add(*rw.ProxyURL, i)
			}
		}
		maxDiskUsagePerURL := build.NewFlag("-remoteWrite.maxDiskUsagePerURL", maxDiskUsage)
		if hasAnyDiskUsagesSet {
			for i, usage := range maxDiskUsagesPerRW {
				maxDiskUsagePerURL.Add(usage, i)
			}
		}

		totalCount := len(remoteTargets)
		args = build.AppendFlagsToArgs(args, totalCount, url, authUser, bearerTokenFile)
		args = build.RelabelArgsTo(args, "remoteWrite.urlRelabelConfig", relabelKeys, relabelConfigs...)
		args = build.AppendFlagsToArgs(args, totalCount, tlsInsecure, sendTimeout, proxyURL)
		args = build.AppendFlagsToArgs(args, totalCount, tlsServerName, tlsKeys, tlsCerts, tlsCAs)
		args = build.AppendFlagsToArgs(args, totalCount, oauth2ClientID, oauth2ClientSecretFile, oauth2Scopes, oauth2TokenURL)
		args = build.AppendFlagsToArgs(args, totalCount, headers, authPasswordFile, maxDiskUsagePerURL, forceVMProto)
		args = build.StreamAggrArgsTo(args, "remoteWrite.streamAggr", streamAggrKeys, streamAggrConfigs...)
		args = build.AppendFlagsToArgs(args, totalCount, awsEC2Endpoint, awsRegion, awsRoleARN, awsService, awsSTSEndpoint, awsUseSigv4)
	}

	if cr.Spec.RemoteWriteSettings != nil {
		rws := cr.Spec.RemoteWriteSettings
		if rws.MaxDiskUsagePerURL != nil {
			maxDiskUsage = rws.MaxDiskUsagePerURL.String()
		}
		if rws.FlushInterval != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.flushInterval=%s", *rws.FlushInterval))
		}
		if rws.Queues != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.queues=%d", *rws.Queues))
		}
		if rws.ShowURL != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.showURL=%t", *rws.ShowURL))
		}
		if rws.TmpDataPath != nil {
			pqMountPath = *rws.TmpDataPath
		}
		if rws.MaxBlockSize != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.maxBlockSize=%d", *rws.MaxBlockSize))
		}
		if rws.Labels != nil {
			lbls := sortMap(rws.Labels)
			flagValue := "-remoteWrite.label="
			if len(lbls) > 0 {
				flagValue += fmt.Sprintf("%s=%s", lbls[0].key, lbls[0].value)
				for _, lv := range lbls[1:] {
					flagValue += fmt.Sprintf(",%s=%s", lv.key, lv.value)
				}
				args = append(args, flagValue)
			}
		}
		if rws.UseMultiTenantMode {
			args = append(args, "-enableMultitenantHandlers=true")
		}
	}

	args = append(args, fmt.Sprintf("-remoteWrite.tmpDataPath=%s", pqMountPath))
	if !hasAnyDiskUsagesSet {
		args = append(args, fmt.Sprintf("-remoteWrite.maxDiskUsagePerURL=%s", maxDiskUsage))
	}
	return args, nil
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) error {
	svcName := cr.PrefixedName()
	keepServices := sets.New(svcName)
	keepServiceScrapes := sets.New[string]()
	keepPodScrapes := sets.New[string]()
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		if cr.Spec.DaemonSetMode {
			keepPodScrapes.Insert(svcName)
		} else {
			keepServiceScrapes.Insert(svcName)
		}
	}
	if cr.Spec.ServiceSpec != nil && !cr.Spec.ServiceSpec.UseAsDefault {
		extraSvcName := cr.Spec.ServiceSpec.NameOrDefault(svcName)
		keepServices.Insert(extraSvcName)
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, cr, keepServices, true); err != nil {
		return fmt.Errorf("cannot remove services: %w", err)
	}
	if err := finalize.RemoveOrphanedVMServiceScrapes(ctx, rclient, cr, keepServiceScrapes, true); err != nil {
		return fmt.Errorf("cannot remove serviceScrapes: %w", err)
	}
	if err := finalize.RemoveOrphanedVMPodScrapes(ctx, rclient, cr, keepPodScrapes, true); err != nil {
		return fmt.Errorf("cannot remove podScrapes: %w", err)
	}

	var objsToRemove []client.Object
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if !cr.Spec.DaemonSetMode {
		objsToRemove = append(objsToRemove, &appsv1.DaemonSet{ObjectMeta: objMeta})
	}
	if !cr.IsOwnsServiceAccount() {
		objsToRemove = append(objsToRemove, &corev1.ServiceAccount{ObjectMeta: objMeta})
		rbacMeta := metav1.ObjectMeta{Name: cr.GetRBACName()}
		if config.IsClusterWideAccessAllowed() {
			objsToRemove = append(objsToRemove,
				&rbacv1.ClusterRoleBinding{ObjectMeta: rbacMeta},
				&rbacv1.ClusterRole{ObjectMeta: rbacMeta},
			)
		} else {
			rbacMeta.Namespace = cr.Namespace
			objsToRemove = append(objsToRemove,
				&rbacv1.RoleBinding{ObjectMeta: rbacMeta},
				&rbacv1.Role{ObjectMeta: rbacMeta},
			)
		}
	}
	return finalize.SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, cr)
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.SecretConfigResourceKind: {
			MountDir:   confDir,
			SecretName: build.ResourceName(build.SecretConfigResourceKind, cr),
		},
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}

// CreateOrUpdateScrapeConfig builds scrape configuration for VMAgent
func CreateOrUpdateScrapeConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent, childObject client.Object) error {
	var prevCR *vmv1beta1.VMAgent
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
	}
	ac := getAssetsCache(ctx, rclient, cr)
	if err := createOrUpdateScrapeConfig(ctx, rclient, cr, prevCR, childObject, ac); err != nil {
		return err
	}
	return nil
}

func createOrUpdateScrapeConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, childObject client.Object, ac *build.AssetsCache) error {
	if ptr.Deref(cr.Spec.IngestOnlyMode, false) {
		return nil
	}
	// HACK: newPodSpec could load content into ac and it must be called
	// before secret config reconcile
	//
	// TODO: @f41gh7 rewrite this section with VLAgent secret assets injection pattern
	if _, err := newPodSpec(cr, ac); err != nil {
		return err
	}

	pos := &vmscrapes.ParsedObjects{
		Namespace:            cr.Namespace,
		APIServerConfig:      cr.Spec.APIServerConfig,
		MustUseNodeSelector:  cr.Spec.DaemonSetMode,
		HasClusterWideAccess: config.IsClusterWideAccessAllowed() || !cr.IsOwnsServiceAccount(),
		ExternalLabels:       cr.ExternalLabels(),
	}
	if !pos.HasClusterWideAccess {
		logger.WithContext(ctx).Info("Setting discovery for the single namespace only." +
			"Since operator launched with set WATCH_NAMESPACE param. " +
			"Set custom ServiceAccountName property for VMAgent if needed.")
		pos.IgnoreNamespaceSelectors = true
	}
	sp := &cr.Spec.CommonScrapeParams
	if err := pos.Init(ctx, rclient, sp); err != nil {
		return err
	}
	pos.ValidateObjects(sp)

	// Update secret based on the most recent configuration.
	generatedConfig, err := pos.GenerateConfig(
		ctx,
		sp,
		ac,
	)
	if err != nil {
		return fmt.Errorf("generating config for vmagent failed: %w", err)
	}

	owner := cr.AsOwner()
	secrets := ac.GetOutput()
	keys := make([]build.ResourceKind, 0, len(secrets))
	for kind := range secrets {
		keys = append(keys, kind)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	for _, kind := range keys {
		secret := secrets[kind]
		var prevSecretMeta *metav1.ObjectMeta
		if prevCR != nil {
			prevSecretMeta = ptr.To(build.ResourceMeta(kind, prevCR))
		}
		if kind == build.SecretConfigResourceKind {
			// Compress config to avoid 1mb secret limit for a while
			d, err := build.GzipConfig(generatedConfig)
			if err != nil {
				return fmt.Errorf("cannot gzip config for vmagent: %w", err)
			}
			secret.Data[scrapeGzippedFilename] = d
		}
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		secret.Annotations = map[string]string{
			"generated": "true",
		}
		if err := reconcile.Secret(ctx, rclient, &secret, prevSecretMeta, &owner); err != nil {
			return err
		}
	}

	parentName := fmt.Sprintf("%s.%s.vmagent", cr.Name, cr.Namespace)
	if err := pos.UpdateStatusesForScrapeObjects(ctx, rclient, parentName, childObject); err != nil {
		return err
	}

	return nil
}
