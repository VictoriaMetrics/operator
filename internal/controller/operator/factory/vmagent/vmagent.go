package vmagent

import (
	"context"
	"fmt"
	"iter"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	vmAgentConfDir                  = "/etc/vmagent/config"
	vmAgentConfOutDir               = "/etc/vmagent/config_out"
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

	kubeNodeEnvName     = "KUBE_NODE_NAME"
	kubeNodeEnvTemplate = "%{" + kubeNodeEnvName + "}"
)

// To save compatibility in the single-shard version still need to fill in %SHARD_NUM% placeholder
var defaultPlaceholders = map[string]string{shardNumPlaceholder: "0"}

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) (*corev1.Service, error) {

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

// CreateOrUpdate creates deployment for vmagent and configures it
// waits for healthy state
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	var prevCR *vmv1beta1.VMAgent
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
		if !cr.Spec.IngestOnlyMode {
			if err := createVMAgentK8sAPIAccess(ctx, rclient, cr, prevCR, config.IsClusterWideAccessAllowed()); err != nil {
				return fmt.Errorf("cannot create vmagent role and binding for it, err: %w", err)
			}
		}
	}

	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		if cr.Spec.DaemonSetMode {
			ps := build.VMPodScrapeForObjectWithSpec(cr, cr.Spec.ServiceScrapeSpec, cr.Spec.ExtraArgs)
			err = reconcile.VMPodScrapeForCRD(ctx, rclient, ps)
		} else {
			err = reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		}
		if err != nil {
			return fmt.Errorf("cannot create or update scrape object: %w", err)
		}
	}

	ac := getAssetsCache(ctx, rclient, cr)
	if err = createOrUpdateConfigurationSecret(ctx, rclient, cr, prevCR, nil, ac); err != nil {
		return err
	}

	if err := createOrUpdateRelabelConfigsAssets(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot update relabeling asset for vmagent: %w", err)
	}

	if err := createOrUpdateStreamAggrConfig(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot update stream aggregation config for vmagent: %w", err)
	}

	if cr.Spec.PodDisruptionBudget != nil && !cr.Spec.DaemonSetMode {
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
		prevDeploy, err = newDeploy(prevCR, ac)
		if err != nil {
			return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
		}
	}
	newDeploy, err := newDeploy(cr, ac)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
	}

	if !cr.Spec.DaemonSetMode && cr.Spec.ShardCount != nil && *cr.Spec.ShardCount > 1 {
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
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, stsNames); err != nil {
		return err
	}
	if err := removeStaleDaemonSet(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot remove vmagent daemonSet: %w", err)
	}
	return nil
}

func createOrUpdateShardedDeploy(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, newDeploy, prevDeploy runtime.Object) error {
	deploymentNames := make(map[string]struct{})
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
	var wg sync.WaitGroup

	type returnValue struct {
		deploymentName string
		stsName        string
		err            error
	}
	rtCh := make(chan *returnValue)

	for shardNum := range shardNumIter(isUpscaling, shardsCount) {
		wg.Add(1)
		go func(shardNum int) {
			var rv returnValue
			defer func() {
				rtCh <- &rv
				wg.Done()
			}()
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
				var err error
				shardedDeploy, err = k8stools.RenderPlaceholders(shardedDeploy, placeholders)
				if err != nil {
					rv.err = fmt.Errorf("cannot fill placeholders for deployment sharded vmagent(%d): %w", shardNum, err)
					return
				}
				if prevShardedObject != nil {
					// prev object could be deployment due to switching from statefulmode
					prevObjApp, ok := prevShardedObject.(*appsv1.Deployment)
					if ok {
						prevDeploy = prevObjApp
						prevDeploy, err = k8stools.RenderPlaceholders(prevDeploy, placeholders)
						if err != nil {
							rv.err = fmt.Errorf("cannot fill placeholders for prev deployment sharded vmagent(%d): %w", shardNum, err)
							return
						}
					}
				}

				if err := reconcile.Deployment(ctx, rclient, shardedDeploy, prevDeploy, false); err != nil {
					rv.err = fmt.Errorf("cannot reconcile deployment for sharded vmagent(%d): %w", shardNum, err)
					return
				}
				rv.deploymentName = shardedDeploy.Name
			case *appsv1.StatefulSet:
				var prevSts *appsv1.StatefulSet
				var err error
				shardedDeploy, err = k8stools.RenderPlaceholders(shardedDeploy, placeholders)
				if err != nil {
					rv.err = fmt.Errorf("cannot fill placeholders for sts in sharded vmagent(%d): %w", shardNum, err)
					return
				}
				if prevShardedObject != nil {
					// prev object could be deployment due to switching to statefulmode
					prevObjApp, ok := prevShardedObject.(*appsv1.StatefulSet)
					if ok {
						prevSts = prevObjApp
						prevSts, err = k8stools.RenderPlaceholders(prevSts, placeholders)
						if err != nil {
							rv.err = fmt.Errorf("cannot fill placeholders for prev sts in sharded vmagent(%d): %w", shardNum, err)
							return
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
					rv.err = fmt.Errorf("cannot reconcile sts for sharded vmagent(%d): %w", shardNum, err)
					return
				}
				rv.stsName = shardedDeploy.Name

			default:
				panic(fmt.Sprintf("BUG: unexpected deploy object type: %T", shardedDeploy))
			}
		}(shardNum)
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
			deploymentNames[rt.deploymentName] = struct{}{}
		}
		if rt.stsName != "" {
			stsNames[rt.stsName] = struct{}{}
		}
	}

	if err := finalize.RemoveOrphanedDeployments(ctx, rclient, cr, deploymentNames); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, stsNames); err != nil {
		return err
	}
	if err := removeStaleDaemonSet(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot remove vmagent daemonSet: %w", err)
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

// newDeploy builds vmagent deployment spec.
func newDeploy(cr *vmv1beta1.VMAgent, ac *build.AssetsCache) (runtime.Object, error) {

	podSpec, err := makeSpec(cr, ac)
	if err != nil {
		return nil, err
	}

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	if cr.Spec.DaemonSetMode {
		dsSpec := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cr.PrefixedName(),
				Namespace:       cr.Namespace,
				Labels:          cr.AllLabels(),
				Annotations:     cr.AnnotationsFiltered(),
				OwnerReferences: cr.AsOwner(),
				Finalizers:      []string{vmv1beta1.FinalizerName},
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
		if cr.Spec.PersistentVolumeClaimRetentionPolicy != nil {
			stsSpec.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.PersistentVolumeClaimRetentionPolicy
		}
		build.StatefulSetAddCommonParams(stsSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
		stsSpec.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(stsSpec.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)
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
	if cr.Spec.ShardCount != nil {
		return cr.PrefixedName()
	}
	return ""
}

func makeSpec(cr *vmv1beta1.VMAgent, ac *build.AssetsCache) (*corev1.PodSpec, error) {
	var args []string

	if rwArgs, err := buildRemoteWriteArgs(cr, ac); err != nil {
		return nil, fmt.Errorf("failed to build remote write args: %w", err)
	} else {
		args = append(args, rwArgs...)
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

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
			Name: kubeNodeEnvName,
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
	var configReloaderWatchMounts []corev1.VolumeMount
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
			fmt.Sprintf("-promscrape.config=%s", path.Join(vmAgentConfOutDir, configEnvsubstFilename)))

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

		volumes = append(volumes,
			corev1.Volume{
				Name: "config-out",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		)
		volumes = append(volumes, corev1.Volume{
			Name: string(build.SecretConfigResourceKind),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: build.ResourceName(build.SecretConfigResourceKind, cr),
				},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts,
			corev1.VolumeMount{
				Name:      "config-out",
				ReadOnly:  true,
				MountPath: vmAgentConfOutDir,
			},
		)
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      string(build.TLSAssetsResourceKind),
			MountPath: tlsAssetsDir,
			ReadOnly:  true,
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      string(build.SecretConfigResourceKind),
			MountPath: vmAgentConfDir,
			ReadOnly:  true,
		})

	}
	volumes, agentVolumeMounts = build.StreamAggrVolumeTo(volumes, agentVolumeMounts, cr)
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
		cvm := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		}
		agentVolumeMounts = append(agentVolumeMounts, cvm)
		configReloaderWatchMounts = append(configReloaderWatchMounts, cvm)
	}

	volumes, agentVolumeMounts = build.LicenseVolumeTo(volumes, agentVolumeMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	if cr.Spec.RelabelConfig != nil || len(cr.Spec.InlineRelabelConfig) > 0 {
		args = append(args, "-remoteWrite.relabelConfig="+path.Join(vmv1beta1.RelabelingConfigDir, globalRelabelingName))
	}

	streamAggrKeys := []string{globalAggregationConfigName}
	streamAggrConfigs := []*vmv1beta1.StreamAggrConfig{cr.Spec.StreamAggrConfig}
	args = build.StreamAggrArgsTo(args, "streamAggr", streamAggrKeys, streamAggrConfigs...)
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
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		VolumeMounts:             agentVolumeMounts,
		Resources:                cr.Spec.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	build.AddServiceAccountTokenVolumeMount(&vmagentContainer, &cr.Spec.CommonApplicationDeploymentParams)
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	vmagentContainer = build.Probe(vmagentContainer, cr)

	build.AddConfigReloadAuthKeyToApp(&vmagentContainer, cr.Spec.ExtraArgs, &cr.Spec.CommonConfigReloaderParams)

	var operatorContainers []corev1.Container
	var ic []corev1.Container
	// conditional add config reloader container
	if !cr.Spec.IngestOnlyMode || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
		configReloader := buildConfigReloaderContainer(cr, configReloaderWatchMounts)
		operatorContainers = append(operatorContainers, configReloader)
		if !cr.Spec.IngestOnlyMode {
			ic = append(ic,
				buildInitConfigContainer(ptr.Deref(cr.Spec.UseVMConfigReloader, false), cr, configReloader.Args)...)
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
	volumes = build.AddConfigReloadAuthKeyVolume(volumes, &cr.Spec.CommonConfigReloaderParams)

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

// buildStreamAggrConfig combines all possible stream aggregation configs and adding it to the configmap.
func buildStreamAggrConfig(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (*corev1.ConfigMap, error) {
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
				data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
					&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: c.RuleConfigMap.Name, Namespace: cr.Namespace}},
					c.RuleConfigMap.Key)
				if err != nil {
					return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", c.RuleConfigMap.Name, err)
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
		prevConfigMeta = ptr.To(build.ResourceMeta(build.StreamAggrConfigResourceKind, cr))
	}
	return reconcile.ConfigMap(ctx, rclient, streamAggrCM, prevConfigMeta)
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

	pqMountPath := vmAgentPersistentQueueDir
	if cr.Spec.StatefulMode {
		pqMountPath = vmAgentPersistentQueueSTSDir
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
		urlRelabelConfig := build.NewFlag("-remoteWrite.urlRelabelConfig", "")
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
			if rw.UrlRelabelConfig != nil || len(rw.InlineUrlRelabelConfig) > 0 {
				urlRelabelConfig.Add(path.Join(vmv1beta1.RelabelingConfigDir, fmt.Sprintf(urlRelabelingName, i)), i)
			}
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
				secret, err := ac.LoadKeyFromSecretOrConfigMap(cr.Namespace, &rw.OAuth2.ClientID)
				if err != nil {
					return nil, err
				}
				oauth2ClientID.Add(secret, i)
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
		args = build.AppendFlagsToArgs(args, totalCount, url, authUser, bearerTokenFile, urlRelabelConfig, tlsInsecure, sendTimeout, proxyURL)
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

func buildConfigReloaderContainer(cr *vmv1beta1.VMAgent, extraWatchsMounts []corev1.VolumeMount) corev1.Container {
	var configReloadVolumeMounts []corev1.VolumeMount
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	if !cr.Spec.IngestOnlyMode {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "config-out",
				MountPath: vmAgentConfOutDir,
			},
		)
		if !useVMConfigReloader {
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

	configReloadArgs := buildConfigReloaderArgs(cr, extraWatchsMounts)

	configReloadVolumeMounts = append(configReloadVolumeMounts, extraWatchsMounts...)
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
	if useVMConfigReloader {
		cntr.Command = nil
		build.AddServiceAccountTokenVolumeMount(&cntr, &cr.Spec.CommonApplicationDeploymentParams)
	}
	build.AddsPortProbesToConfigReloaderContainer(useVMConfigReloader, &cntr)
	build.AddConfigReloadAuthKeyToReloader(&cntr, &cr.Spec.CommonConfigReloaderParams)
	return cntr
}

func buildConfigReloaderArgs(cr *vmv1beta1.VMAgent, extraWatchVolumes []corev1.VolumeMount) []string {
	// by default use watched-dir
	// it should simplify parsing for latest and empty version tags.
	dirsArg := "watched-dir"

	args := []string{
		fmt.Sprintf("--reload-url=%s", vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
	}
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

	if !cr.Spec.IngestOnlyMode {
		args = append(args, fmt.Sprintf("--config-envsubst-file=%s", path.Join(vmAgentConfOutDir, configEnvsubstFilename)))
		if useVMConfigReloader {
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
	for _, vl := range extraWatchVolumes {
		args = append(args, fmt.Sprintf("--%s=%s", dirsArg, vl.MountPath))
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

func buildInitConfigContainer(useVMConfigReloader bool, cr *vmv1beta1.VMAgent, configReloaderArgs []string) []corev1.Container {
	var initReloader corev1.Container
	baseImage := cr.Spec.ConfigReloaderImageTag
	resources := cr.Spec.ConfigReloaderResources
	if useVMConfigReloader {
		initReloader = corev1.Container{
			Image: baseImage,
			Name:  "config-init",
			Args:  append(configReloaderArgs, "--only-init-config"),
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "config-out",
					MountPath: vmAgentConfOutDir,
				},
			},
			Resources: resources,
		}
		build.AddServiceAccountTokenVolumeMount(&initReloader, &cr.Spec.CommonApplicationDeploymentParams)
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
			fmt.Sprintf("gunzip -c %s > %s", path.Join(vmAgentConfDir, vmagentGzippedFilename), path.Join(vmAgentConfOutDir, configEnvsubstFilename)),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: vmAgentConfDir,
			},
			{
				Name:      "config-out",
				MountPath: vmAgentConfOutDir,
			},
		},
		Resources: resources,
	}

	return []corev1.Container{initReloader}
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	if prevCR == nil {
		return nil
	}
	// TODO check for stream aggr removed

	prevSvc, currSvc := prevCR.Spec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && prevCR.Spec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}

	if !prevCR.Spec.DaemonSetMode && cr.Spec.DaemonSetMode {
		// transit into DaemonSetMode
		if cr.Spec.PodDisruptionBudget != nil {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
				return fmt.Errorf("cannot delete PDB from prev state: %w", err)
			}
		}
		if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
				return fmt.Errorf("cannot delete VMServiceScrape during daemonset transition: %w", err)
			}
		}
	}

	if prevCR.Spec.DaemonSetMode && !cr.Spec.DaemonSetMode {
		// transit into non DaemonSetMode
		if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMPodScrape{ObjectMeta: objMeta}); err != nil {
				return fmt.Errorf("cannot delete VMPodScrape during transition for non-daemonsetMode: %w", err)
			}
		}
	}

	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}

func removeStaleDaemonSet(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) error {
	if cr.Spec.DaemonSetMode {
		return nil
	}
	ds := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.PrefixedName(),
			Namespace: cr.Namespace,
		},
	}
	return finalize.SafeDeleteWithFinalizer(ctx, rclient, &ds)
}
