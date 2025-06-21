package vmsingle

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	confDir                = "/etc/vm/config"
	confOutDir             = "/etc/vm/config_out"
	tlsAssetsDir           = "/etc/vm-tls/certs"
	dataDir                = "/victoria-metrics-data"
	dataVolumeName         = "data"
	streamAggrSecretKey    = "config.yaml"
	relabelingName         = "relabeling.yaml"
	scrapeGzippedFilename  = "scrape.yaml.gz"
	scrapeEnvsubstFilename = "scrape.env.yaml"
)

func createStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle) error {
	newPvc := makePvc(cr)
	var prevPVC *corev1.PersistentVolumeClaim
	if prevCR != nil && prevCR.Spec.Storage != nil {
		prevPVC = makePvc(prevCR)
	}

	return reconcile.PersistentVolumeClaim(ctx, rclient, newPvc, prevPVC)
}

func makePvc(cr *vmv1beta1.VMSingle) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          labels.Merge(cr.Spec.StorageMetadata.Labels, cr.SelectorLabels()),
			Annotations:     cr.Spec.StorageMetadata.Annotations,
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
		},
		Spec: *cr.Spec.Storage,
	}
	if len(pvcObject.Spec.AccessModes) == 0 {
		pvcObject.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}
	return pvcObject
}

// CreateOrUpdate performs an update for single node resource
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) error {

	var prevCR *vmv1beta1.VMSingle
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
		if !cr.Spec.EnableScraping {
			if err := createK8sAPIAccess(ctx, rclient, cr, prevCR, config.IsClusterWideAccessAllowed()); err != nil {
				return fmt.Errorf("cannot create vmsingle role and binding for it, err: %w", err)
			}
		}
	}

	if cr.Spec.Storage != nil && cr.Spec.StorageDataPath == "" {
		if err := createStorage(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot create storage: %w", err)
		}
	}
	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr, "vmbackupmanager"))
		if err != nil {
			return fmt.Errorf("cannot create serviceScrape for vmsingle: %w", err)
		}
	}

	ac := getAssetsCache(ctx, rclient, cr)
	if err := createOrUpdateScrapeConfig(ctx, rclient, cr, prevCR, nil, ac); err != nil {
		return err
	}
	if err := createOrUpdateRelabelConfigsAssets(ctx, rclient, cr, prevCR, ac); err != nil {
		return fmt.Errorf("cannot update relabeling asset for vmsingle: %w", err)
	}
	if err := createOrUpdateStreamAggrConfig(ctx, rclient, cr, prevCR, ac); err != nil {
		return fmt.Errorf("cannot update stream aggregation config for vmsingle: %w", err)
	}

	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		prevDeploy, err = newDeploy(ctx, prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeploy, err := newDeploy(ctx, cr)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vmsingle: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false)
}

func newDeploy(ctx context.Context, cr *vmv1beta1.VMSingle) (*appsv1.Deployment, error) {

	podSpec, err := makeSpec(ctx, cr)
	if err != nil {
		return nil, err
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
				// we use recreate, coz of volume claim
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(cr.Spec.UseStrictSecurity, false), &cr.Spec.CommonApplicationDeploymentParams)
	return depSpec, nil
}

func makeSpec(ctx context.Context, cr *vmv1beta1.VMSingle) (*corev1.PodTemplateSpec, error) {
	var args []string

	if cr.Spec.RetentionPeriod != "" {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod))
	}

	// if customStorageDataPath is not empty, do not add volumes
	// and volumeMounts
	// it's user responsibility to provide correct values
	mustAddVolumeMounts := cr.Spec.StorageDataPath == ""

	storagePath := dataDir
	if cr.Spec.StorageDataPath != "" {
		storagePath = cr.Spec.StorageDataPath
	}
	args = append(args, fmt.Sprintf("-storageDataPath=%s", storagePath))
	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	args = build.AppendArgsForInsertPorts(args, cr.Spec.InsertPorts)

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AppendInsertPorts(ports, cr.Spec.InsertPorts)

	var volumes []corev1.Volume
	var vmMounts []corev1.VolumeMount
	var configReloaderWatchMounts []corev1.VolumeMount

	if cr.Spec.EnableScraping {
		args = append(args, fmt.Sprintf("-promscrape.config=%s", path.Join(confOutDir, scrapeEnvsubstFilename)))

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
		vmMounts = append(vmMounts,
			corev1.VolumeMount{
				Name:      "config-out",
				ReadOnly:  true,
				MountPath: confOutDir,
			},
		)
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      string(build.TLSAssetsResourceKind),
			MountPath: tlsAssetsDir,
			ReadOnly:  true,
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      string(build.SecretConfigResourceKind),
			MountPath: confDir,
			ReadOnly:  true,
		})
	}

	volumes, vmMounts = addVolumeMountsTo(volumes, vmMounts, cr, mustAddVolumeMounts, storagePath)

	if cr.Spec.VMBackup != nil && cr.Spec.VMBackup.CredentialsSecret != nil {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + cr.Spec.VMBackup.CredentialsSecret.Name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.Spec.VMBackup.CredentialsSecret.Name,
				},
			},
		})
	}

	volumes = append(volumes, cr.Spec.Volumes...)
	vmMounts = append(vmMounts, cr.Spec.VolumeMounts...)

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
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
		vmMounts = append(vmMounts, cvm)
		configReloaderWatchMounts = append(configReloaderWatchMounts, cvm)
	}

	volumes, vmMounts = build.RelabelVolumeTo(volumes, vmMounts, build.ResourceName(build.RelabelConfigResourceKind, cr), &cr.Spec.CommonRelabelParams)
	relabelKeys := []string{"relabel.yaml"}
	relabelConfigs := []*vmv1beta1.CommonRelabelParams{&cr.Spec.CommonRelabelParams}
	args = build.RelabelArgsTo(args, "relabelConfig", relabelKeys, relabelConfigs...)

	volumes, vmMounts = build.StreamAggrVolumeTo(volumes, vmMounts, build.ResourceName(build.StreamAggrConfigResourceKind, cr), cr.Spec.StreamAggrConfig)
	streamAggrKeys := []string{streamAggrSecretKey}
	streamAggrConfigs := []*vmv1beta1.StreamAggrConfig{cr.Spec.StreamAggrConfig}
	args = build.StreamAggrArgsTo(args, "streamAggr", streamAggrKeys, streamAggrConfigs...)

	// deduplication can work without stream aggregation rules
	if cr.Spec.StreamAggrConfig != nil && cr.Spec.StreamAggrConfig.DedupInterval != "" {
		args = append(args, fmt.Sprintf("--streamAggr.dedupInterval=%s", cr.Spec.StreamAggrConfig.DedupInterval))
	}

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	vmsingleContainer := corev1.Container{
		Name:                     "vmsingle",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}

	vmsingleContainer = build.Probe(vmsingleContainer, cr)
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	var operatorContainers []corev1.Container
	var initContainers []corev1.Container

	if cr.Spec.EnableScraping || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
		configReloader := buildConfigReloaderContainer(cr, configReloaderWatchMounts)
		operatorContainers = append(operatorContainers, configReloader)
		if cr.Spec.EnableScraping {
			initContainers = append(initContainers,
				buildInitConfigContainer(ptr.Deref(cr.Spec.UseVMConfigReloader, false), cr, configReloader.Args)...)
			build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, initContainers, useStrictSecurity)
		}
	}

	if cr.Spec.VMBackup != nil {
		vmBackupManagerContainer, err := build.VMBackupManager(ctx, cr.Spec.VMBackup, cr.Spec.Port, storagePath, dataVolumeName, cr.Spec.ExtraArgs, false, cr.Spec.License)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			operatorContainers = append(operatorContainers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMBackup.Restore != nil &&
			cr.Spec.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := build.VMRestore(cr.Spec.VMBackup, storagePath, dataVolumeName)
			if err != nil {
				return nil, err
			}
			if vmRestore != nil {
				initContainers = append(initContainers, *vmRestore)
			}
		}
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, initContainers, ptr.Deref(cr.Spec.UseStrictSecurity, false))
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply initContainer patch: %w", err)
	}

	operatorContainers = append(operatorContainers, vmsingleContainer)

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	vmSingleSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(),
			Annotations: cr.PodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     ic,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}

	return vmSingleSpec, nil
}

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle) (*corev1.Service, error) {

	addExtraPorts := func(svc *corev1.Service, vmb *vmv1beta1.VMBackup) {
		if cr.Spec.Port != "8428" {
			// conditionally add 8428 port to be compatible with binary port
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "http-alias",
				Protocol:   "TCP",
				Port:       8428,
				TargetPort: intstr.Parse(cr.Spec.Port),
			})
		}

		if vmb != nil {
			parsedPort := intstr.Parse(vmb.Port)
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "vmbackupmanager",
				Protocol:   corev1.ProtocolTCP,
				Port:       parsedPort.IntVal,
				TargetPort: parsedPort,
			})
		}
	}
	newService := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		addExtraPorts(svc, cr.Spec.VMBackup)
		build.AppendInsertPortsToService(cr.Spec.InsertPorts, svc)
	})

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			addExtraPorts(svc, prevCR.Spec.VMBackup)
			build.AppendInsertPortsToService(prevCR.Spec.InsertPorts, svc)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.ServiceSpec)
	}
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vmsingle additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmsingle: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmsingle: %w", err)
	}
	return newService, nil
}

// buildRelabelingsAssets combines all possible relabeling config configuration and adding it to the configmap.
func buildRelabelingsAssets(cr *vmv1beta1.VMSingle, ac *build.AssetsCache) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: build.ResourceMeta(build.RelabelConfigResourceKind, cr),
		Data:       make(map[string]string),
	}
	if len(cr.Spec.InlineRelabelConfig) > 0 {
		rcs := addRelabelConfigs(nil, cr.Spec.InlineRelabelConfig)
		data, err := yaml.Marshal(rcs)
		if err != nil {
			return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
		}
		if len(data) > 0 {
			cm.Data[relabelingName] = string(data)
		}
	}
	if cr.Spec.RelabelConfig != nil {
		// need to fetch content from
		data, err := ac.LoadKeyFromConfigMap(cr.Namespace, cr.Spec.RelabelConfig)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", cr.Spec.RelabelConfig.Name, err)
		}
		if len(data) > 0 {
			cm.Data[relabelingName] += data
		}
	}
	return cm, nil
}

// createOrUpdateRelabelConfigsAssets builds relabeling configs for vmsingle at separate configmap, serialized as yaml
func createOrUpdateRelabelConfigsAssets(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle, ac *build.AssetsCache) error {
	if !cr.HasAnyRelabellingConfigs() {
		return nil
	}
	assestsCM, err := buildRelabelingsAssets(cr, ac)
	if err != nil {
		return err
	}
	var prevConfigMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevConfigMeta = ptr.To(build.ResourceMeta(build.RelabelConfigResourceKind, prevCR))
	}
	return reconcile.ConfigMap(ctx, rclient, assestsCM, prevConfigMeta)
}

// buildStreamAggrConfig build configmap with stream aggregation config for vmsingle.
func buildStreamAggrConfig(cr *vmv1beta1.VMSingle, ac *build.AssetsCache) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: build.ResourceMeta(build.StreamAggrConfigResourceKind, cr),
		Data:       make(map[string]string),
	}
	if len(cr.Spec.StreamAggrConfig.Rules) > 0 {
		data, err := yaml.Marshal(cr.Spec.StreamAggrConfig.Rules)
		if err != nil {
			return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
		}
		if len(data) > 0 {
			cfgCM.Data[streamAggrSecretKey] = string(data)
		}
	}
	if cr.Spec.StreamAggrConfig.RuleConfigMap != nil {
		data, err := ac.LoadKeyFromConfigMap(cr.Namespace, cr.Spec.StreamAggrConfig.RuleConfigMap)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", cr.Spec.StreamAggrConfig.RuleConfigMap.Name, err)
		}
		if len(data) > 0 {
			cfgCM.Data[streamAggrSecretKey] += data
		}
	}
	return cfgCM, nil
}

// createOrUpdateStreamAggrConfig builds stream aggregation configs for vmsingle at separate configmap, serialized as yaml
func createOrUpdateStreamAggrConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle, ac *build.AssetsCache) error {
	if !cr.HasAnyStreamAggrRule() {
		return nil
	}
	streamAggrCM, err := buildStreamAggrConfig(cr, ac)
	if err != nil {
		return err
	}
	var prevCMMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevCMMeta = ptr.To(build.ResourceMeta(build.StreamAggrConfigResourceKind, prevCR))
	}
	return reconcile.ConfigMap(ctx, rclient, streamAggrCM, prevCMMeta)
}

func buildConfigReloaderContainer(cr *vmv1beta1.VMSingle, extraWatchsMounts []corev1.VolumeMount) corev1.Container {
	var configReloadVolumeMounts []corev1.VolumeMount
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	if cr.Spec.EnableScraping {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "config-out",
				MountPath: confOutDir,
			},
		)
		if !useVMConfigReloader {
			configReloadVolumeMounts = append(configReloadVolumeMounts,
				corev1.VolumeMount{
					Name:      "config",
					MountPath: confDir,
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

func buildConfigReloaderArgs(cr *vmv1beta1.VMSingle, extraWatchVolumes []corev1.VolumeMount) []string {
	// by default use watched-dir
	// it should simplify parsing for latest and empty version tags.
	dirsArg := "watched-dir"

	args := []string{
		fmt.Sprintf("--reload-url=%s", vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
	}
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

	if cr.Spec.EnableScraping {
		args = append(args, fmt.Sprintf("--config-envsubst-file=%s", path.Join(confOutDir, scrapeEnvsubstFilename)))
		if useVMConfigReloader {
			args = append(args, fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.PrefixedName()))
			args = append(args, "--config-secret-key=vmagent.yaml.gz")
		} else {
			args = append(args, fmt.Sprintf("--config-file=%s", path.Join(confDir, scrapeGzippedFilename)))
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

func buildInitConfigContainer(useVMConfigReloader bool, cr *vmv1beta1.VMSingle, configReloaderArgs []string) []corev1.Container {
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
					MountPath: confOutDir,
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
			fmt.Sprintf("gunzip -c %s > %s", path.Join(confDir, scrapeGzippedFilename), path.Join(confOutDir, scrapeEnvsubstFilename)),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: confDir,
			},
			{
				Name:      "config-out",
				MountPath: confOutDir,
			},
		},
		Resources: resources,
	}

	return []corev1.Container{initReloader}
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle) error {
	if prevCR == nil {
		return nil
	}
	// TODO check storage for nil
	// TODO check for stream aggr removed

	prevSvc, currSvc := cr.ParsedLastAppliedSpec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}

func addVolumeMountsTo(volumes []corev1.Volume, vmMounts []corev1.VolumeMount, cr *vmv1beta1.VMSingle, mustAddVolumeMounts bool, storagePath string) ([]corev1.Volume, []corev1.VolumeMount) {

	switch {
	case mustAddVolumeMounts:
		// add volume and mount point by operator directly
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      dataVolumeName,
			MountPath: storagePath},
		)

		vlSource := corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
		if cr.Spec.Storage != nil {
			vlSource = corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cr.PrefixedName(),
				},
			}
		}
		volumes = append(volumes, corev1.Volume{
			Name:         dataVolumeName,
			VolumeSource: vlSource})

	case len(cr.Spec.Volumes) > 0:
		// add missing volumeMount point for backward compatibility
		// it simplifies management of external PVCs
		var volumeNamePresent bool
		for _, volume := range cr.Spec.Volumes {
			if volume.Name == dataVolumeName {
				volumeNamePresent = true
				break
			}
		}
		if volumeNamePresent {
			var mustSkipVolumeAdd bool
			for _, volumeMount := range cr.Spec.VolumeMounts {
				if volumeMount.Name == dataVolumeName {
					mustSkipVolumeAdd = true
					break
				}
			}
			if !mustSkipVolumeAdd {
				vmMounts = append(vmMounts, corev1.VolumeMount{
					Name:      dataVolumeName,
					MountPath: storagePath,
				})
			}
		}

	}

	return volumes, vmMounts
}
