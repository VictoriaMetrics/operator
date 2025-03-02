package vmagent

import (
	"context"
	"fmt"
	"iter"
	"path"
	"sort"
	"strconv"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmAgentConfDir                  = "/etc/vmagent/config"
	vmAgentConOfOutDir              = "/etc/vmagent/config_out"
	vmAgentPersistentQueueDir       = "/tmp/vmagent-remotewrite-data"
	vmAgentPersistentQueueSTSDir    = "/vmagent_pq/vmagent-remotewrite-data"
	vmAgentPersistentQueueMountName = "persistent-queue-data"
	globalRelabelingName            = "global_relabeling.yaml"
	urlRelabelingName               = "url_relabeling-%d.yaml"
	globalAggregationConfigName     = "global_aggregation.yaml"

	shardNumPlaceholder    = "%SHARD_NUM%"
	tlsAssetsDir           = "/etc/vmagent-tls/certs"
	vmagentGzippedFilename = "vmagent.yaml.gz"
	configEnvsubstFilename = "vmagent.env.yaml"
	defaultMaxDiskUsage    = "1073741824"
)

// To save compatibility in the single-shard version still need to fill in %SHARD_NUM% placeholder
var defaultPlaceholders = map[string]string{shardNumPlaceholder: "0"}

func createOrUpdateVMAgentService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) (*corev1.Service, error) {

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			if prevCR.Spec.StatefulMode {
				svc.Spec.ClusterIP = "None"
			}
			build.AppendInsertPortsToService(prevCR.Spec.InsertPorts, svc)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, cr.Spec.ServiceSpec)
	}
	newService := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		if cr.Spec.StatefulMode {
			svc.Spec.ClusterIP = "None"
		}
		build.AppendInsertPortsToService(cr.Spec.InsertPorts, svc)
	})

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, cr.Spec.ServiceSpec)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vmagent additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmagent: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmagent: %w", err)
	}
	return newService, nil
}

// CreateOrUpdateVMAgent creates deployment for vmagent and configures it
// waits for healthy state
func CreateOrUpdateVMAgent(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	var prevCR *vmv1beta1.VMAgent
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
		if !cr.Spec.IngestOnlyMode {
			if err := createVMAgentK8sAPIAccess(ctx, rclient, cr, prevCR, config.IsClusterWideAccessAllowed()); err != nil {
				return fmt.Errorf("cannot create vmagent role and binding for it, err: %w", err)
			}
		}

	}

	svc, err := createOrUpdateVMAgentService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err = reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		if err != nil {
			return fmt.Errorf("cannot create serviceScrape: %w", err)
		}
	}

	ssCache, err := createOrUpdateConfigurationSecret(ctx, rclient, cr, prevCR, nil)
	if err != nil {
		return err
	}

	if err := createOrUpdateRelabelConfigsAssets(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot update relabeling asset for vmagent: %w", err)
	}

	if err := createOrUpdateStreamAggrConfig(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot update stream aggregation config for vmagent: %w", err)
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		err = reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB)
		if err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vmagent: %w", err)
		}
	}

	var prevDeploy runtime.Object

	if prevCR != nil {
		prevDeploy, err = newDeployForVMAgent(prevCR, ssCache)
		if err != nil {
			return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
		}
	}
	newDeploy, err := newDeployForVMAgent(cr, ssCache)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
	}

	if cr.Spec.ShardCount != nil && *cr.Spec.ShardCount > 1 {
		return createOrUpdateShardedDeploy(ctx, rclient, cr, prevCR, newDeploy, prevDeploy)
	}
	return createOrUpdateDeploy(ctx, rclient, cr, prevCR, newDeploy, prevDeploy)
}

func createOrUpdateDeploy(ctx context.Context, rclient client.Client, cr, _ *vmv1beta1.VMAgent, newDeploy, prevObjectSpec runtime.Object) error {
	deploymentNames := make(map[string]struct{})
	stsNames := make(map[string]struct{})

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
					return fmt.Errorf("cannot fill placeholders for prev deployment in vmagent: %w", err)
				}
			}
		}

		newDeploy, err = k8stools.RenderPlaceholders(newDeploy, defaultPlaceholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for deployment in vmagent: %w", err)
		}
		if err := reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false); err != nil {
			return err
		}
		deploymentNames[newDeploy.Name] = struct{}{}
	case *appsv1.StatefulSet:
		var prevSTS *appsv1.StatefulSet
		if prevObjectSpec != nil {
			prevAppObject, ok := prevObjectSpec.(*appsv1.StatefulSet)
			if ok {
				prevSTS = prevAppObject
				prevSTS, err = k8stools.RenderPlaceholders(prevSTS, defaultPlaceholders)
				if err != nil {
					return fmt.Errorf("cannot fill placeholders for prev sts in vmagent: %w", err)
				}
			}
		}
		newDeploy, err = k8stools.RenderPlaceholders(newDeploy, defaultPlaceholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for sts in vmagent: %w", err)
		}
		stsOpts := reconcile.STSOptions{
			HasClaim:       len(newDeploy.Spec.VolumeClaimTemplates) > 0,
			SelectorLabels: cr.SelectorLabels,
		}
		if err := reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newDeploy, prevSTS); err != nil {
			return err
		}
		stsNames[newDeploy.Name] = struct{}{}
	}
	if err := finalize.RemoveOrphanedDeployments(ctx, rclient, cr, deploymentNames); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, stsNames); err != nil {
		return err
	}
	return nil
}

func createOrUpdateShardedDeploy(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, newDeploy, prevDeploy runtime.Object) error {
	deploymentNames := make(map[string]struct{})
	var err error
	stsNames := make(map[string]struct{})
	shardsCount := *cr.Spec.ShardCount
	logger.WithContext(ctx).Info(fmt.Sprintf("using cluster version of VMAgent with shards count=%d", shardsCount))

	isUpscaling := false
	if prevCR != nil && prevCR.Spec.ShardCount != nil {
		if *prevCR.Spec.ShardCount < shardsCount {
			logger.WithContext(ctx).Info(fmt.Sprintf("VMAgent shard upscaling from=%d to=%d", *prevCR.Spec.ShardCount, shardsCount))
			isUpscaling = true
		}
	}
	for shardNum := range shardNumIter(isUpscaling, shardsCount) {
		shardedDeploy := newDeploy.DeepCopyObject()
		var prevShardedObject runtime.Object
		addShardSettingsToVMAgent(shardNum, shardsCount, shardedDeploy)
		if prevDeploy != nil {
			prevShardedObject = prevDeploy.DeepCopyObject()
			addShardSettingsToVMAgent(shardNum, shardsCount, prevShardedObject)
		}
		placeholders := map[string]string{shardNumPlaceholder: strconv.Itoa(shardNum)}
		switch shardedDeploy := shardedDeploy.(type) {
		case *appsv1.Deployment:
			var prevDeploy *appsv1.Deployment
			shardedDeploy, err = k8stools.RenderPlaceholders(shardedDeploy, placeholders)
			if err != nil {
				return fmt.Errorf("cannot fill placeholders for deployment sharded vmagent: %w", err)
			}
			if prevShardedObject != nil {
				// prev object could be deployment due to switching from statefulmode
				prevObjApp, ok := prevShardedObject.(*appsv1.Deployment)
				if ok {
					prevDeploy = prevObjApp
					prevDeploy, err = k8stools.RenderPlaceholders(prevDeploy, placeholders)
					if err != nil {
						return fmt.Errorf("cannot fill placeholders for prev deployment sharded vmagent: %w", err)
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
				return fmt.Errorf("cannot fill placeholders for sts in sharded vmagent: %w", err)
			}
			if prevShardedObject != nil {
				// prev object could be deployment due to switching to statefulmode
				prevObjApp, ok := prevShardedObject.(*appsv1.StatefulSet)
				if ok {
					prevSts = prevObjApp
					prevSts, err = k8stools.RenderPlaceholders(prevSts, placeholders)
					if err != nil {
						return fmt.Errorf("cannot fill placeholders for prev sts in sharded vmagent: %w", err)
					}
				}
			}
			stsOpts := reconcile.STSOptions{
				HasClaim: len(shardedDeploy.Spec.VolumeClaimTemplates) > 0,
				SelectorLabels: func() map[string]string {
					selectorLabels := cr.SelectorLabels()
					selectorLabels["shard-num"] = strconv.Itoa(shardNum)
					return selectorLabels
				},
			}
			if err := reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, shardedDeploy, prevSts); err != nil {
				return err
			}
			stsNames[shardedDeploy.Name] = struct{}{}
		}
	}
	if err := finalize.RemoveOrphanedDeployments(ctx, rclient, cr, deploymentNames); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, stsNames); err != nil {
		return err
	}
	return nil
}

func shardNumIter(backward bool, shardCount int) iter.Seq[int] {
	if backward {
		return func(yield func(int) bool) {
			for shardCount >= 0 {
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

// newDeployForVMAgent builds vmagent deployment spec.
func newDeployForVMAgent(cr *vmv1beta1.VMAgent, ssCache *scrapesSecretsCache) (runtime.Object, error) {

	podSpec, err := makeSpecForVMAgent(cr, ssCache)
	if err != nil {
		return nil, err
	}
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	// fast path, use sts
	if cr.Spec.StatefulMode {
		stsSpec := &appsv1.StatefulSet{
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
				ServiceName:         buildSTSServiceName(cr),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      cr.PodLabels(),
						Annotations: cr.PodAnnotations(),
					},
					Spec: *podSpec,
				},
			},
		}
		build.StatefulSetAddCommonParams(stsSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
		cr.Spec.StatefulStorage.IntoSTSVolume(vmAgentPersistentQueueMountName, &stsSpec.Spec)
		stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.ClaimTemplates...)
		return stsSpec, nil
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.UpdateStrategy
	}
	depSpec := &appsv1.Deployment{
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
					Annotations: cr.PodAnnotations(),
				},
				Spec: *podSpec,
			},
		},
	}
	build.DeploymentAddCommonParams(depSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
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
	if cr.Spec.ShardCount != nil {
		return cr.PrefixedName()
	}
	return ""
}

func makeSpecForVMAgent(cr *vmv1beta1.VMAgent, ssCache *scrapesSecretsCache) (*corev1.PodSpec, error) {
	var args []string

	if len(cr.Spec.RemoteWrite) > 0 {
		args = append(args, buildRemoteWrites(cr, ssCache)...)
	}
	args = append(args, buildRemoteWriteSettings(cr)...)

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}
	if len(cr.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AppendInsertPorts(ports, cr.Spec.InsertPorts)

	var agentVolumeMounts []corev1.VolumeMount
	// mount data path any way, even if user changes its value
	// we cannot rely on value of remoteWriteSettings.
	pqMountPath := vmAgentPersistentQueueDir
	if cr.Spec.StatefulMode {
		pqMountPath = vmAgentPersistentQueueSTSDir
	}
	agentVolumeMounts = append(agentVolumeMounts,
		corev1.VolumeMount{
			Name:      vmAgentPersistentQueueMountName,
			MountPath: pqMountPath,
		},
	)
	agentVolumeMounts = append(agentVolumeMounts, cr.Spec.VolumeMounts...)

	var volumes []corev1.Volume
	// in case for sts, we have to use persistentVolumeClaimTemplate instead
	if !cr.Spec.StatefulMode {
		volumes = append(volumes, corev1.Volume{
			Name: vmAgentPersistentQueueMountName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	volumes = append(volumes, cr.Spec.Volumes...)

	if !cr.Spec.IngestOnlyMode {
		args = append(args,
			fmt.Sprintf("-promscrape.config=%s", path.Join(vmAgentConOfOutDir, configEnvsubstFilename)))

		volumes = append(volumes,
			corev1.Volume{
				Name: "tls-assets",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cr.TLSAssetName(),
					},
				},
			},
			corev1.Volume{
				Name: "config-out",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		)

		agentVolumeMounts = append(agentVolumeMounts,
			corev1.VolumeMount{
				Name:      "config-out",
				ReadOnly:  true,
				MountPath: vmAgentConOfOutDir,
			},
			corev1.VolumeMount{
				Name:      "tls-assets",
				ReadOnly:  true,
				MountPath: tlsAssetsDir,
			},
		)
		volumes = append(volumes,
			corev1.Volume{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cr.PrefixedName(),
					},
				},
			})
		agentVolumeMounts = append(agentVolumeMounts,
			corev1.VolumeMount{
				Name:      "config",
				ReadOnly:  true,
				MountPath: vmAgentConfDir,
			})
	}
	if cr.HasAnyStreamAggrRule() {
		volumes = append(volumes, corev1.Volume{
			Name: "stream-aggr-conf",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cr.StreamAggrConfigName(),
					},
				},
			},
		},
		)
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      "stream-aggr-conf",
			ReadOnly:  true,
			MountPath: vmv1beta1.StreamAggrConfigDir,
		},
		)
	}

	if cr.HasAnyRelabellingConfigs() {
		volumes = append(volumes,
			corev1.Volume{
				Name: "relabeling-assets",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cr.RelabelingAssetName(),
						},
					},
				},
			},
		)

		agentVolumeMounts = append(agentVolumeMounts,
			corev1.VolumeMount{
				Name:      "relabeling-assets",
				ReadOnly:  true,
				MountPath: vmv1beta1.RelabelingConfigDir,
			},
		)
	}

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
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	volumes, agentVolumeMounts = cr.Spec.License.MaybeAddToVolumes(volumes, agentVolumeMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	if cr.Spec.RelabelConfig != nil || len(cr.Spec.InlineRelabelConfig) > 0 {
		args = append(args, "-remoteWrite.relabelConfig="+path.Join(vmv1beta1.RelabelingConfigDir, globalRelabelingName))
	}

	if cr.Spec.StreamAggrConfig != nil {
		if cr.Spec.StreamAggrConfig.HasAnyRule() {
			args = append(args, "-streamAggr.config="+path.Join(vmv1beta1.StreamAggrConfigDir, globalAggregationConfigName))
		}
		if cr.Spec.StreamAggrConfig.KeepInput {
			args = append(args, "-streamAggr.keepInput=true")
		}
		if cr.Spec.StreamAggrConfig.DropInput {
			args = append(args, "-streamAggr.dropInput=true")
		}
		if cr.Spec.StreamAggrConfig.DedupInterval != "" {
			args = append(args, fmt.Sprintf("-streamAggr.dedupInterval=%s", cr.Spec.StreamAggrConfig.DedupInterval))
		}
		if len(cr.Spec.StreamAggrConfig.DropInputLabels) > 0 {
			args = append(args, fmt.Sprintf("-streamAggr.dropInputLabels=%s", strings.Join(cr.Spec.StreamAggrConfig.DropInputLabels, ",")))
		}
		if cr.Spec.StreamAggrConfig.IgnoreOldSamples {
			args = append(args, "-streamAggr.ignoreOldSamples=true")
		}
	}

	args = build.AppendArgsForInsertPorts(args, cr.Spec.InsertPorts)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)

	vmagentContainer := corev1.Container{
		Name:                     "vmagent",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		Env:                      envs,
		VolumeMounts:             agentVolumeMounts,
		Resources:                cr.Spec.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	vmagentContainer = build.Probe(vmagentContainer, cr)

	var operatorContainers []corev1.Container
	var ic []corev1.Container
	// conditional add config reloader container
	if !cr.Spec.IngestOnlyMode || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
		configReloader := buildConfigReloaderContainer(cr)
		operatorContainers = append(operatorContainers, configReloader)
		if !cr.Spec.IngestOnlyMode {
			ic = append(ic,
				buildInitConfigContainer(ptr.Deref(cr.Spec.UseVMConfigReloader, false), cr.Spec.ConfigReloaderImageTag, cr.Spec.ConfigReloaderResources, configReloader.Args)...)
			build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, ic, useStrictSecurity)
		}
	}
	var err error
	ic, err = k8stools.MergePatchContainers(ic, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
	}

	operatorContainers = append(operatorContainers, vmagentContainer)

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

	return &corev1.PodSpec{
		Volumes:            volumes,
		InitContainers:     ic,
		Containers:         containers,
		ServiceAccountName: cr.GetServiceAccountName(),
	}, nil
}

func addShardSettingsToVMAgent(shardNum, shardsCount int, dep runtime.Object) {
	var containers []corev1.Container
	switch dep := dep.(type) {
	case *appsv1.StatefulSet:
		containers = dep.Spec.Template.Spec.Containers
		dep.Name = fmt.Sprintf("%s-%d", dep.Name, shardNum)
		// need to mutate selectors ?
		dep.Spec.Selector.MatchLabels["shard-num"] = strconv.Itoa(shardNum)
		dep.Spec.Template.Labels["shard-num"] = strconv.Itoa(shardNum)
	case *appsv1.Deployment:
		containers = dep.Spec.Template.Spec.Containers
		dep.Name = fmt.Sprintf("%s-%d", dep.Name, shardNum)
		// need to mutate selectors ?
		dep.Spec.Selector.MatchLabels["shard-num"] = strconv.Itoa(shardNum)
		dep.Spec.Template.Labels["shard-num"] = strconv.Itoa(shardNum)
	}
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
			args = append(args, fmt.Sprintf("-promscrape.cluster.membersCount=%d", shardsCount))
			args = append(args, fmt.Sprintf("-promscrape.cluster.memberNum=%d", shardNum))
			container.Args = args
		}
	}
}

func buildRelabelingsAssetsMeta(cr *vmv1beta1.VMAgent) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.RelabelingAssetName(),
		Labels:          cr.AllLabels(),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: cr.AsOwner(),
	}
}

// buildVMAgentRelabelingsAssets combines all possible relabeling config configuration and adding it to the configmap.
func buildVMAgentRelabelingsAssets(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: buildRelabelingsAssetsMeta(cr),
		Data:       make(map[string]string),
	}
	// global section
	if len(cr.Spec.InlineRelabelConfig) > 0 {
		rcs := addRelabelConfigs(nil, cr.Spec.InlineRelabelConfig)
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
		data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cr.Spec.RelabelConfig.Name, Namespace: cr.Namespace}},
			cr.Spec.RelabelConfig.Key)
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
			rcs := addRelabelConfigs(nil, rw.InlineUrlRelabelConfig)
			data, err := yaml.Marshal(rcs)
			if err != nil {
				return nil, fmt.Errorf("cannot serialize urlRelabelConfig as yaml: %w", err)
			}
			if len(data) > 0 {
				cfgCM.Data[fmt.Sprintf(urlRelabelingName, i)] = string(data)
			}
		}
		if rw.UrlRelabelConfig != nil {
			data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: rw.UrlRelabelConfig.Name, Namespace: cr.Namespace}},
				rw.UrlRelabelConfig.Key)
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
func createOrUpdateRelabelConfigsAssets(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	if !cr.HasAnyRelabellingConfigs() {
		return nil
	}
	assestsCM, err := buildVMAgentRelabelingsAssets(ctx, rclient, cr)
	if err != nil {
		return err
	}
	var prevConfigMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevConfigMeta = ptr.To(buildRelabelingsAssetsMeta(prevCR))
	}
	return reconcile.ConfigMap(ctx, rclient, assestsCM, prevConfigMeta)
}

func buildStreamAggrConfigMeta(cr *vmv1beta1.VMAgent) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.StreamAggrConfigName(),
		Labels:          cr.AllLabels(),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: cr.AsOwner(),
	}
}

// buildStreamAggrConfig combines all possible stream aggregation configs and adding it to the configmap.
func buildStreamAggrConfig(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: buildStreamAggrConfigMeta(cr),
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
			data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cr.Spec.StreamAggrConfig.RuleConfigMap.Name, Namespace: cr.Namespace}},
				cr.Spec.StreamAggrConfig.RuleConfigMap.Key)
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
			if len(rw.StreamAggrConfig.Rules) > 0 {
				data, err := yaml.Marshal(rw.StreamAggrConfig.Rules)
				if err != nil {
					return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
				}
				if len(data) > 0 {
					cfgCM.Data[rw.AsConfigMapKey(i, "stream-aggr-conf")] = string(data)
				}
			}
			if rw.StreamAggrConfig.RuleConfigMap != nil {
				data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
					&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: rw.StreamAggrConfig.RuleConfigMap.Name, Namespace: cr.Namespace}},
					rw.StreamAggrConfig.RuleConfigMap.Key)
				if err != nil {
					return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", rw.StreamAggrConfig.RuleConfigMap.Name, err)
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
func createOrUpdateStreamAggrConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	// fast path
	if !cr.HasAnyStreamAggrRule() {
		return nil
	}
	streamAggrCM, err := buildStreamAggrConfig(ctx, cr, rclient)
	if err != nil {
		return err
	}
	var prevConfigMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevConfigMeta = ptr.To(buildStreamAggrConfigMeta(prevCR))
	}
	return reconcile.ConfigMap(ctx, rclient, streamAggrCM, prevConfigMeta)
}

func createOrUpdateTLSAssets(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, assets map[string]string) error {
	tlsAssetsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.TLSAssetName(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Data: map[string][]byte{},
	}

	for key, asset := range assets {
		tlsAssetsSecret.Data[key] = []byte(asset)
	}
	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = &metav1.ObjectMeta{
			Name:        prevCR.TLSAssetName(),
			Labels:      prevCR.AllLabels(),
			Annotations: prevCR.AnnotationsFiltered(),
			Namespace:   prevCR.Namespace,
		}
	}
	return reconcile.Secret(ctx, rclient, tlsAssetsSecret, prevSecretMeta)
}

func addAssetsToCache(
	ctx context.Context,
	rclient client.Client,
	objectNS string,
	tlsConfig *vmv1beta1.TLSConfig,
	ssCache *scrapesSecretsCache,
) error {
	if tlsConfig == nil {
		return nil
	}
	assets, nsSecretCache, nsConfigMapCache := ssCache.tlsAssets, ssCache.nsSecretCache, ssCache.nsCMCache

	fetchAssetFor := func(assetPath string, src vmv1beta1.SecretOrConfigMap) error {
		var asset string
		var err error
		cacheKey := objectNS + "/" + src.PrefixedName()
		switch {
		case src.Secret != nil:
			asset, err = k8stools.GetCredFromSecret(
				ctx,
				rclient,
				objectNS,
				src.Secret,
				cacheKey,
				nsSecretCache,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to extract endpoint tls asset from secret %s and key %s in namespace %s: %w",
					src.PrefixedName(), src.Key(), objectNS, err,
				)
			}

		case src.ConfigMap != nil:
			asset, err = k8stools.GetCredFromConfigMap(
				ctx,
				rclient,
				objectNS,
				*src.ConfigMap,
				cacheKey,
				nsConfigMapCache,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to extract endpoint tls asset for  configmap %v and key %v in namespace %v",
					src.PrefixedName(), src.Key(), objectNS,
				)
			}
		}
		if len(asset) > 0 {
			assets[assetPath] = asset
		}
		return nil
	}

	if err := fetchAssetFor(tlsConfig.BuildAssetPath(objectNS, tlsConfig.CA.PrefixedName(), tlsConfig.CA.Key()), tlsConfig.CA); err != nil {
		return fmt.Errorf("cannot fetch CA tls asset: %w", err)
	}

	if err := fetchAssetFor(tlsConfig.BuildAssetPath(objectNS, tlsConfig.Cert.PrefixedName(), tlsConfig.Cert.Key()), tlsConfig.Cert); err != nil {
		return fmt.Errorf("cannot fetch Cert tls asset: %w", err)
	}

	if tlsConfig.KeySecret != nil {
		asset, err := k8stools.GetCredFromSecret(
			ctx,
			rclient,
			objectNS,
			tlsConfig.KeySecret,
			objectNS+"/"+tlsConfig.KeySecret.Name,
			nsSecretCache,
		)
		if err != nil {
			return fmt.Errorf(
				"failed to extract endpoint tls asset from secret %s and key %s in namespace %s",
				tlsConfig.KeySecret.Name, tlsConfig.KeySecret.Key, objectNS,
			)
		}
		assets[tlsConfig.BuildAssetPath(objectNS, tlsConfig.KeySecret.Name, tlsConfig.KeySecret.Key)] = asset
	}

	return nil
}

type remoteFlag struct {
	isNotNull   bool
	flagSetting string
}

func buildRemoteWriteSettings(cr *vmv1beta1.VMAgent) []string {
	var args []string
	if cr.Spec.RemoteWriteSettings == nil {
		// fast path
		pqMountPath := vmAgentPersistentQueueDir
		if cr.Spec.StatefulMode {
			pqMountPath = vmAgentPersistentQueueSTSDir
		}
		args = append(args,
			"-remoteWrite.maxDiskUsagePerURL=1073741824",
			fmt.Sprintf("-remoteWrite.tmpDataPath=%s", pqMountPath))
		return args
	}

	rws := *cr.Spec.RemoteWriteSettings
	if rws.FlushInterval != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.flushInterval=%s", *rws.FlushInterval))
	}
	if rws.MaxBlockSize != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.maxBlockSize=%d", *rws.MaxBlockSize))
	}
	// limit to 1GB
	// most people do not care about this setting,
	// but it may harmfully affect kubernetes cluster health
	maxDiskUsage := defaultMaxDiskUsage
	if rws.MaxDiskUsagePerURL != nil {
		maxDiskUsage = fmt.Sprintf("%d", *rws.MaxDiskUsagePerURL)
	}
	if rws.Queues != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.queues=%d", *rws.Queues))
	}
	if rws.ShowURL != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.showURL=%t", *rws.ShowURL))
	}
	pqMountPath := vmAgentPersistentQueueDir
	if cr.Spec.StatefulMode {
		pqMountPath = vmAgentPersistentQueueSTSDir
	}
	if rws.TmpDataPath != nil {
		pqMountPath = *rws.TmpDataPath
	}
	args = append(args, fmt.Sprintf("-remoteWrite.tmpDataPath=%s", pqMountPath))
	var containsMaxDiskUsage bool
	for arg := range cr.Spec.ExtraArgs {
		if arg == "remoteWrite.maxDiskUsagePerURL" {
			containsMaxDiskUsage = true
		}
		break
	}
	if !containsMaxDiskUsage {
		for i := range cr.Spec.RemoteWrite {
			rws := cr.Spec.RemoteWrite[i]
			if rws.MaxDiskUsage != nil {
				containsMaxDiskUsage = true
				break
			}
		}
	}
	if !containsMaxDiskUsage {
		// limit to 1GB
		args = append(args, "-remoteWrite.maxDiskUsagePerURL="+maxDiskUsage)
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
	return args
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

func buildRemoteWrites(cr *vmv1beta1.VMAgent, ssCache *scrapesSecretsCache) []string {
	var finalArgs []string
	var remoteArgs []remoteFlag
	remoteTargets := cr.Spec.RemoteWrite

	url := remoteFlag{flagSetting: "-remoteWrite.url=", isNotNull: true}

	authUser := remoteFlag{flagSetting: "-remoteWrite.basicAuth.username="}
	authPasswordFile := remoteFlag{flagSetting: "-remoteWrite.basicAuth.passwordFile="}
	bearerTokenFile := remoteFlag{flagSetting: "-remoteWrite.bearerTokenFile="}
	urlRelabelConfig := remoteFlag{flagSetting: "-remoteWrite.urlRelabelConfig="}
	sendTimeout := remoteFlag{flagSetting: "-remoteWrite.sendTimeout="}
	tlsCAs := remoteFlag{flagSetting: "-remoteWrite.tlsCAFile="}
	tlsCerts := remoteFlag{flagSetting: "-remoteWrite.tlsCertFile="}
	tlsKeys := remoteFlag{flagSetting: "-remoteWrite.tlsKeyFile="}
	tlsInsecure := remoteFlag{flagSetting: "-remoteWrite.tlsInsecureSkipVerify="}
	tlsServerName := remoteFlag{flagSetting: "-remoteWrite.tlsServerName="}
	oauth2ClientID := remoteFlag{flagSetting: "-remoteWrite.oauth2.clientID="}
	oauth2ClientSecretFile := remoteFlag{flagSetting: "-remoteWrite.oauth2.clientSecretFile="}
	oauth2Scopes := remoteFlag{flagSetting: "-remoteWrite.oauth2.scopes="}
	oauth2TokenURL := remoteFlag{flagSetting: "-remoteWrite.oauth2.tokenUrl="}
	headers := remoteFlag{flagSetting: "-remoteWrite.headers="}
	streamAggrConfig := remoteFlag{flagSetting: "-remoteWrite.streamAggr.config="}
	streamAggrKeepInput := remoteFlag{flagSetting: "-remoteWrite.streamAggr.keepInput="}
	streamAggrDropInput := remoteFlag{flagSetting: "-remoteWrite.streamAggr.dropInput="}
	streamAggrDedupInterval := remoteFlag{flagSetting: "-remoteWrite.streamAggr.dedupInterval="}
	streamAggrDropInputLabels := remoteFlag{flagSetting: "-remoteWrite.streamAggr.dropInputLabels="}
	streamAggrIgnoreFirstIntervals := remoteFlag{flagSetting: "-remoteWrite.streamAggr.ignoreFirstIntervals="}
	streamAggrIgnoreOldSamples := remoteFlag{flagSetting: "-remoteWrite.streamAggr.ignoreOldSamples="}
	maxDiskUsagePerURL := remoteFlag{flagSetting: "-remoteWrite.maxDiskUsagePerURL="}
	forceVMProto := remoteFlag{flagSetting: "-remoteWrite.forceVMProto="}

	pathPrefix := path.Join(tlsAssetsDir, cr.Namespace)

	var maxDiskUsageInExtraArgs bool
	var forceVMProtoInExtraArgs bool
	for arg := range cr.Spec.ExtraArgs {
		if arg == "remoteWrite.maxDiskUsagePerURL" {
			maxDiskUsageInExtraArgs = true
		}
		if arg == "remoteWrite.forceVMProto" {
			forceVMProtoInExtraArgs = true
		}
		if maxDiskUsageInExtraArgs && forceVMProtoInExtraArgs {
			break
		}
	}

	for i := range remoteTargets {
		rws := remoteTargets[i]
		if !maxDiskUsageInExtraArgs && rws.MaxDiskUsage != nil {
			maxDiskUsagePerURL.isNotNull = true
		}
		if !forceVMProtoInExtraArgs && rws.ForceVMProto {
			forceVMProto.isNotNull = true
		}
		if maxDiskUsagePerURL.isNotNull && forceVMProto.isNotNull {
			break
		}
	}

	for i := range remoteTargets {
		rws := remoteTargets[i]
		url.flagSetting += fmt.Sprintf("%s,", rws.URL)

		var caPath, certPath, keyPath, ServerName string
		var insecure bool
		if rws.TLSConfig != nil {
			if rws.TLSConfig.CAFile != "" {
				caPath = rws.TLSConfig.CAFile
			} else if rws.TLSConfig.CA.PrefixedName() != "" {
				caPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.CA.PrefixedName(), rws.TLSConfig.CA.Key())
			}
			if caPath != "" {
				tlsCAs.isNotNull = true
			}
			if rws.TLSConfig.CertFile != "" {
				certPath = rws.TLSConfig.CertFile
			} else if rws.TLSConfig.Cert.PrefixedName() != "" {
				certPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.Cert.PrefixedName(), rws.TLSConfig.Cert.Key())
			}
			if certPath != "" {
				tlsCerts.isNotNull = true
			}
			switch {
			case rws.TLSConfig.KeyFile != "":
				keyPath = rws.TLSConfig.KeyFile
			case rws.TLSConfig.KeySecret != nil:
				keyPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.KeySecret.Name, rws.TLSConfig.KeySecret.Key)
			}
			if keyPath != "" {
				tlsKeys.isNotNull = true
			}
			if rws.TLSConfig.InsecureSkipVerify {
				tlsInsecure.isNotNull = true
			}
			if rws.TLSConfig.ServerName != "" {
				ServerName = rws.TLSConfig.ServerName
				tlsServerName.isNotNull = true
			}
			insecure = rws.TLSConfig.InsecureSkipVerify
		}
		tlsCAs.flagSetting += fmt.Sprintf("%s,", caPath)
		tlsCerts.flagSetting += fmt.Sprintf("%s,", certPath)
		tlsKeys.flagSetting += fmt.Sprintf("%s,", keyPath)
		tlsServerName.flagSetting += fmt.Sprintf("%s,", ServerName)
		tlsInsecure.flagSetting += fmt.Sprintf("%v,", insecure)

		var user string
		var passFile string
		if rws.BasicAuth != nil {
			if s, ok := ssCache.baSecrets[rws.AsMapKey()]; ok {
				authUser.isNotNull = true

				user = s.Username
				if len(s.Password) > 0 {
					authPasswordFile.isNotNull = true
					passFile = path.Join(vmAgentConfDir, rws.AsSecretKey(i, "basicAuthPassword"))
				}
				if len(rws.BasicAuth.PasswordFile) > 0 {
					passFile = rws.BasicAuth.PasswordFile
					authPasswordFile.isNotNull = true
				}
			}
		}
		authUser.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(user, `"`, `\"`))
		authPasswordFile.flagSetting += fmt.Sprintf("%s,", passFile)

		var value string
		if rws.BearerTokenSecret != nil && rws.BearerTokenSecret.Name != "" {
			bearerTokenFile.isNotNull = true
			value = path.Join(vmAgentConfDir, rws.AsSecretKey(i, "bearerToken"))
		}
		bearerTokenFile.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(value, `"`, `\"`))

		value = ""

		if rws.UrlRelabelConfig != nil || len(rws.InlineUrlRelabelConfig) > 0 {
			urlRelabelConfig.isNotNull = true
			value = path.Join(vmv1beta1.RelabelingConfigDir, fmt.Sprintf(urlRelabelingName, i))
		}

		urlRelabelConfig.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.SendTimeout != nil {
			if !sendTimeout.isNotNull {
				sendTimeout.isNotNull = true
			}
			value = *rws.SendTimeout
		}
		sendTimeout.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if len(rws.Headers) > 0 {
			headers.isNotNull = true
			for _, headerValue := range rws.Headers {
				value += headerValue + "^^"
			}
			value = strings.TrimSuffix(value, "^^")
		}
		headers.flagSetting += fmt.Sprintf("%s,", value)
		value = ""
		var oaturl, oascopes, oaclientID, oaSecretKeyFile string
		if rws.OAuth2 != nil {
			if len(rws.OAuth2.TokenURL) > 0 {
				oauth2TokenURL.isNotNull = true
				oaturl = rws.OAuth2.TokenURL
			}

			if len(rws.OAuth2.Scopes) > 0 {
				oauth2Scopes.isNotNull = true
				oascopes = strings.Join(rws.OAuth2.Scopes, ",")
			}

			if len(rws.OAuth2.ClientSecretFile) > 0 {
				oauth2ClientSecretFile.isNotNull = true
				oaSecretKeyFile = rws.OAuth2.ClientSecretFile
			}

			sv := ssCache.oauth2Secrets[fmt.Sprintf("remoteWriteSpec/%s", rws.URL)]
			if rws.OAuth2.ClientSecret != nil && sv != nil {
				oauth2ClientSecretFile.isNotNull = true
				oaSecretKeyFile = path.Join(vmAgentConfDir, rws.AsSecretKey(i, "oauth2Secret"))
			}

			if len(rws.OAuth2.ClientID.PrefixedName()) > 0 && sv != nil {
				oaclientID = sv.ClientID
				oauth2ClientID.isNotNull = true
			}

		}
		oauth2TokenURL.flagSetting += fmt.Sprintf("%s,", oaturl)
		oauth2ClientSecretFile.flagSetting += fmt.Sprintf("%s,", oaSecretKeyFile)
		oauth2ClientID.flagSetting += fmt.Sprintf("%s,", oaclientID)
		oauth2Scopes.flagSetting += fmt.Sprintf("%s,", oascopes)

		var dedupIntVal, streamConfVal string
		var keepInputVal, dropInputVal, ignoreOldSamples bool
		var ignoreFirstIntervalsVal int
		if rws.StreamAggrConfig != nil {
			if rws.StreamAggrConfig.HasAnyRule() {
				streamAggrConfig.isNotNull = true
				streamConfVal = path.Join(vmv1beta1.StreamAggrConfigDir, rws.AsConfigMapKey(i, "stream-aggr-conf"))
			}

			dedupIntVal = rws.StreamAggrConfig.DedupInterval
			if dedupIntVal != "" {
				streamAggrDedupInterval.isNotNull = true
			}

			keepInputVal = rws.StreamAggrConfig.KeepInput
			if keepInputVal {
				streamAggrKeepInput.isNotNull = true
			}
			dropInputVal = rws.StreamAggrConfig.DropInput
			if dropInputVal {
				streamAggrDropInput.isNotNull = true
			}
			if len(rws.StreamAggrConfig.DropInputLabels) > 0 {
				streamAggrDropInputLabels.isNotNull = true
				streamAggrDropInputLabels.flagSetting += fmt.Sprintf("%s,", strings.Join(rws.StreamAggrConfig.DropInputLabels, ","))
			}
			ignoreFirstIntervalsVal = rws.StreamAggrConfig.IgnoreFirstIntervals
			if ignoreFirstIntervalsVal > 0 {
				streamAggrIgnoreFirstIntervals.isNotNull = true
			}
			ignoreOldSamples = rws.StreamAggrConfig.IgnoreOldSamples
			if ignoreOldSamples {
				streamAggrIgnoreOldSamples.isNotNull = true
			}
		}
		streamAggrConfig.flagSetting += fmt.Sprintf("%s,", streamConfVal)
		streamAggrKeepInput.flagSetting += fmt.Sprintf("%v,", keepInputVal)
		streamAggrDropInput.flagSetting += fmt.Sprintf("%v,", dropInputVal)
		streamAggrDedupInterval.flagSetting += fmt.Sprintf("%s,", dedupIntVal)
		streamAggrIgnoreFirstIntervals.flagSetting += fmt.Sprintf("%d,", ignoreFirstIntervalsVal)
		streamAggrIgnoreOldSamples.flagSetting += fmt.Sprintf("%v,", ignoreOldSamples)

		if maxDiskUsagePerURL.isNotNull {
			if rws.MaxDiskUsage != nil {
				maxDiskUsagePerURL.flagSetting += fmt.Sprintf("%s,", *rws.MaxDiskUsage)
			} else {
				maxDiskUsagePerURL.flagSetting += fmt.Sprintf("%s,", defaultMaxDiskUsage)
			}
		}

		if forceVMProto.isNotNull {
			forceVMProto.flagSetting += fmt.Sprintf("%t,", rws.ForceVMProto)
		}
	}

	remoteArgs = append(remoteArgs, url, authUser, bearerTokenFile, urlRelabelConfig, tlsInsecure, sendTimeout)
	remoteArgs = append(remoteArgs, tlsServerName, tlsKeys, tlsCerts, tlsCAs)
	remoteArgs = append(remoteArgs, oauth2ClientID, oauth2ClientSecretFile, oauth2Scopes, oauth2TokenURL)
	remoteArgs = append(remoteArgs, headers, authPasswordFile)
	remoteArgs = append(remoteArgs, streamAggrConfig, streamAggrKeepInput, streamAggrDedupInterval, streamAggrDropInput, streamAggrDropInputLabels, streamAggrIgnoreFirstIntervals, streamAggrIgnoreOldSamples)
	remoteArgs = append(remoteArgs, maxDiskUsagePerURL, forceVMProto)

	for _, remoteArgType := range remoteArgs {
		if remoteArgType.isNotNull {
			finalArgs = append(finalArgs, strings.TrimSuffix(remoteArgType.flagSetting, ","))
		}
	}
	return finalArgs
}

func buildConfigReloaderContainer(cr *vmv1beta1.VMAgent) corev1.Container {
	var configReloadVolumeMounts []corev1.VolumeMount
	useCustomConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	if !cr.Spec.IngestOnlyMode {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "config-out",
				MountPath: vmAgentConOfOutDir,
			},
		)
		if !useCustomConfigReloader {
			configReloadVolumeMounts = append(configReloadVolumeMounts,
				corev1.VolumeMount{
					Name:      "config",
					MountPath: vmAgentConfDir,
				})
		}
	}
	if cr.HasAnyRelabellingConfigs() {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "relabeling-assets",
				ReadOnly:  true,
				MountPath: vmv1beta1.RelabelingConfigDir,
			})
	}
	if cr.HasAnyStreamAggrRule() {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "stream-aggr-conf",
				ReadOnly:  true,
				MountPath: vmv1beta1.StreamAggrConfigDir,
			})
	}

	configReloadArgs := buildConfigReloaderArgs(cr)
	cntr := corev1.Container{
		Name:                     "config-reloader",
		Image:                    cr.Spec.ConfigReloaderImageTag,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Env: []corev1.EnvVar{
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
		},
		Command:      []string{"/bin/prometheus-config-reloader"},
		Args:         configReloadArgs,
		VolumeMounts: configReloadVolumeMounts,
		Resources:    cr.Spec.ConfigReloaderResources,
	}
	if useCustomConfigReloader {
		cntr.Command = nil
	}
	build.AddsPortProbesToConfigReloaderContainer(useCustomConfigReloader, &cntr)

	return cntr
}

func buildConfigReloaderArgs(cr *vmv1beta1.VMAgent) []string {
	// by default use watched-dir
	// it should simplify parsing for latest and empty version tags.
	dirsArg := "watched-dir"

	args := []string{
		fmt.Sprintf("--reload-url=%s", vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
	}
	useCustomConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

	if !cr.Spec.IngestOnlyMode {
		args = append(args, fmt.Sprintf("--config-envsubst-file=%s", path.Join(vmAgentConOfOutDir, configEnvsubstFilename)))
		if useCustomConfigReloader {
			args = append(args, fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.PrefixedName()))
			args = append(args, "--config-secret-key=vmagent.yaml.gz")
		} else {
			args = append(args, fmt.Sprintf("--config-file=%s", path.Join(vmAgentConfDir, vmagentGzippedFilename)))
		}
	}
	if cr.HasAnyStreamAggrRule() {
		args = append(args, fmt.Sprintf("--%s=%s", dirsArg, vmv1beta1.StreamAggrConfigDir))
	}
	if cr.HasAnyRelabellingConfigs() {
		args = append(args, fmt.Sprintf("--%s=%s", dirsArg, vmv1beta1.RelabelingConfigDir))
	}
	if useCustomConfigReloader {
		args = vmv1beta1.MaybeEnableProxyProtocol(args, cr.Spec.ExtraArgs)
	}
	if len(cr.Spec.ConfigReloaderExtraArgs) > 0 {
		for idx, arg := range args {
			cleanArg := strings.Split(strings.TrimLeft(arg, "-"), "=")[0]
			if replacement, ok := cr.Spec.ConfigReloaderExtraArgs[cleanArg]; ok {
				delete(cr.Spec.ConfigReloaderExtraArgs, cleanArg)
				args[idx] = fmt.Sprintf(`--%s=%s`, cleanArg, replacement)
			}
		}
		for k, v := range cr.Spec.ConfigReloaderExtraArgs {
			args = append(args, fmt.Sprintf(`--%s=%s`, k, v))
		}
		sort.Strings(args)
	}

	return args
}

func buildInitConfigContainer(useCustomConfigReloader bool, baseImage string, resources corev1.ResourceRequirements, configReloaderArgs []string) []corev1.Container {
	var initReloader corev1.Container
	if useCustomConfigReloader {
		initReloader = corev1.Container{
			Image: baseImage,
			Name:  "config-init",
			Args:  append(configReloaderArgs, "--only-init-config"),
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "config-out",
					MountPath: vmAgentConOfOutDir,
				},
			},
			Resources: resources,
		}
		return []corev1.Container{initReloader}
	}
	initReloader = corev1.Container{
		Image: baseImage,
		Name:  "config-init",
		Command: []string{
			"/bin/sh",
		},
		Args: []string{
			"-c",
			fmt.Sprintf("gunzip -c %s > %s", path.Join(vmAgentConfDir, vmagentGzippedFilename), path.Join(vmAgentConOfOutDir, configEnvsubstFilename)),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: vmAgentConfDir,
			},
			{
				Name:      "config-out",
				MountPath: vmAgentConOfOutDir,
			},
		},
		Resources: resources,
	}
	return []corev1.Container{initReloader}
}

func deletePrevStateResources(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}
	// TODO check for stream aggr removed

	prevSvc, currSvc := cr.ParsedLastAppliedSpec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
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
