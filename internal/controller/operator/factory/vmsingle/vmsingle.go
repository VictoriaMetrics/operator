package vmsingle

import (
	"context"
	"fmt"
	"path"
	"sort"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
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
	confDir               = "/etc/vm/config"
	confOutDir            = "/etc/vm/config_out"
	tlsAssetsDir          = "/etc/vm-tls/certs"
	dataDir               = "/victoria-metrics-data"
	dataVolumeName        = "data"
	streamAggrSecretKey   = "config.yaml"
	relabelingName        = "relabeling.yaml"
	scrapeGzippedFilename = "scrape.yaml.gz"
	configFilename        = "scrape.yaml"
)

func isStorageEmpty(pvc *corev1.PersistentVolumeClaimSpec) bool {
	return pvc == nil || pvc.Resources.Requests.Storage().IsZero()
}

func createStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle) error {
	newPvc := makePvc(cr)
	var prevPVC *corev1.PersistentVolumeClaim
	if prevCR != nil && !isStorageEmpty(prevCR.Spec.Storage) {
		prevPVC = makePvc(prevCR)
	}
	owner := cr.AsOwner()
	return reconcile.PersistentVolumeClaim(ctx, rclient, newPvc, prevPVC, &owner)
}

func makePvc(cr *vmv1beta1.VMSingle) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.ResourceLabels(cr.Spec.StorageMetadata.Labels),
			Annotations:     labels.Merge(cr.FinalAnnotations(), cr.Spec.StorageMetadata.Annotations),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
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
	if cr.Paused() {
		return nil
	}

	var prevCR *vmv1beta1.VMSingle
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete objects from prev state: %w", err)
		}
	}
	owner := cr.AsOwner()
	cfg := config.MustGetBaseConfig()
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA, &owner); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
		if !ptr.Deref(cr.Spec.IngestOnlyMode, true) {
			if err := createK8sAPIAccess(ctx, rclient, cr, prevCR, cfg.WatchNamespaces); err != nil {
				return fmt.Errorf("cannot create vmsingle role and binding for it, err: %w", err)
			}
		}
	}

	if !isStorageEmpty(cr.Spec.Storage) {
		if err := createStorage(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot create storage: %w", err)
		}
	}
	if err := createOrUpdateService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if cr.Spec.VPA != nil && !cfg.VPAAPIEnabled {
		return fmt.Errorf("spec.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
	}
	if err := createOrUpdateVPA(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot create or update vpa for vmsingle: %w", err)
	}

	ac := getAssetsCache(ctx, rclient, cr)
	extraCount, err := createOrUpdateScrapeConfig(ctx, rclient, cr, prevCR, nil, ac)
	if err != nil {
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
		var err error
		prevDeploy, err = newDeploy(ctx, prevCR, 0)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeploy, err := newDeploy(ctx, cr, extraCount)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vmsingle: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, &owner, nil)
}

func newDeploy(ctx context.Context, cr *vmv1beta1.VMSingle, extraConfigSecretCount int) (*appsv1.Deployment, error) {

	podSpec, err := newPodSpec(ctx, cr, extraConfigSecretCount)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
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
	build.DeploymentAddCommonParams(depSpec, &cr.Spec.CommonAppsParams)
	return depSpec, nil
}

func newPodSpec(ctx context.Context, cr *vmv1beta1.VMSingle, extraConfigSecretCount int) (*corev1.PodTemplateSpec, error) {
	var args []string

	if cr.Spec.RetentionPeriod != "" {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod))
	}

	if cr.Spec.Downsampling.HasAnyRule() {
		for _, rule := range cr.Spec.Downsampling.Rules {
			for _, p := range rule.Periods {
				period := fmt.Sprintf("%s:%s", p.Offset, p.Interval)
				if rule.Filter != "" {
					period = rule.Filter + ":" + period
				}
				args = append(args, fmt.Sprintf("-downsampling.period=%s", period))
			}
		}
	}
	if cr.Spec.Downsampling != nil && cr.Spec.Downsampling.DedupInterval != "" {
		args = append(args, fmt.Sprintf("-dedup.minScrapeInterval=%s", cr.Spec.Downsampling.DedupInterval))
	}

	if cr.Spec.RetentionFilters != nil {
		for _, rf := range *cr.Spec.RetentionFilters {
			args = append(args, fmt.Sprintf("-retentionFilter=%s:%s", rf.Filter, rf.Retention))
		}
	}

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

	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	args = build.AppendArgsForInsertPorts(args, cr.Spec.InsertPorts)

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AppendInsertPorts(ports, cr.Spec.InsertPorts)

	var crMounts []corev1.VolumeMount

	var pvcSrc *corev1.PersistentVolumeClaimVolumeSource
	if !isStorageEmpty(cr.Spec.Storage) {
		pvcSrc = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: cr.PrefixedName(),
		}
	}

	volumes, vmMounts, err := build.StorageVolumeMountsTo(cr.Spec.Volumes, cr.Spec.VolumeMounts, pvcSrc, storagePath, build.DataVolumeName, false)
	if err != nil {
		return nil, err
	}

	if !ptr.Deref(cr.Spec.IngestOnlyMode, true) {
		args = append(args, fmt.Sprintf("-promscrape.config=%s", path.Join(confOutDir, configFilename)))

		// preserve order of volumes and volumeMounts
		// it must prevent vmsingle restarts during operator version change
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
		m := corev1.VolumeMount{
			Name:      "config-out",
			MountPath: confOutDir,
		}
		crMounts = append(crMounts, m)
		m.ReadOnly = true
		vmMounts = append(vmMounts, m)
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

		// Shared EmptyDir for overflow: config-reloader writes decompressed job files here,
		// vmsingle reads them via scrape_config_files. Only present when overflow is active.
		if extraConfigSecretCount > 0 {
			volumes = append(volumes, corev1.Volume{
				Name: "sc-files-out",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
			vmMounts = append(vmMounts, corev1.VolumeMount{
				Name:      "sc-files-out",
				MountPath: vmscrapes.ExtraConfigOutDir,
				ReadOnly:  true,
			})
		}
	}

	commonMounts := vmMounts

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
		crMounts = append(crMounts, cvm)
	}

	mountsLen := len(vmMounts)
	volumes, vmMounts = build.StreamAggrVolumeTo(volumes, vmMounts, cr)
	volumes, vmMounts = build.RelabelVolumeTo(volumes, vmMounts, cr)
	crMounts = append(crMounts, vmMounts[mountsLen:]...)

	relabelKeys := []string{"relabel.yaml"}
	relabelConfigs := []*vmv1beta1.CommonRelabelParams{&cr.Spec.CommonRelabelParams}
	args = build.RelabelArgsTo(args, "relabelConfig", relabelKeys, relabelConfigs...)

	streamAggrKeys := []string{streamAggrSecretKey}
	streamAggrConfigs := []*vmv1beta1.StreamAggrConfig{cr.Spec.StreamAggrConfig}
	args = build.StreamAggrArgsTo(args, "streamAggr", streamAggrKeys, streamAggrConfigs...)

	// deduplication can work without stream aggregation rules
	if cr.Spec.StreamAggrConfig != nil && cr.Spec.StreamAggrConfig.DedupInterval != "" {
		args = append(args, fmt.Sprintf("--streamAggr.dedupInterval=%s", cr.Spec.StreamAggrConfig.DedupInterval))
	}

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)
	volumes, vmMounts = build.OpenShiftServiceCAVolumeTo(volumes, vmMounts)
	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	vmsingleContainer := corev1.Container{
		Name:                     "vmsingle",
		Image:                    cr.Spec.Image.Reference(),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}

	build.Probe(&vmsingleContainer, cr, &cr.Spec.CommonAppsParams)
	build.Lifecycle(&vmsingleContainer, &cr.Spec.CommonAppsParams)

	containers := []corev1.Container{vmsingleContainer}
	var ic []corev1.Container

	if !ptr.Deref(cr.Spec.IngestOnlyMode, true) || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
		var ss *corev1.SecretKeySelector
		if !ptr.Deref(cr.Spec.IngestOnlyMode, true) {
			ss = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cr.PrefixedName(),
				},
				Key: configFilename,
			}
			ic = append(ic, build.ConfigReloaderContainer(true, cr, crMounts, ss))
			build.AddStrictSecuritySettingsToContainers(ic, &cr.Spec.CommonAppsParams)
		}
		configReloader := build.ConfigReloaderContainer(false, cr, crMounts, ss)
		if extraConfigSecretCount > 0 {
			// sc-files-out is write-side for the reloader; not in crMounts to avoid --watched-dir.
			configReloader.VolumeMounts = append(configReloader.VolumeMounts, corev1.VolumeMount{
				Name:      "sc-files-out",
				MountPath: vmscrapes.ExtraConfigOutDir,
			})
			for i := 1; i <= extraConfigSecretCount; i++ {
				rawDir := fmt.Sprintf(vmscrapes.ExtraConfigRawDirFmt, i)
				rawBasename := fmt.Sprintf("sc-raw-%d", i)
				volumes = append(volumes, corev1.Volume{
					Name: rawBasename,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: vmscrapes.ExtraConfigSecretName(cr, i),
						},
					},
				})
				configReloader.VolumeMounts = append(configReloader.VolumeMounts, corev1.VolumeMount{
					Name:      rawBasename,
					MountPath: rawDir,
					ReadOnly:  true,
				})
				configReloader.Args = append(configReloader.Args,
					fmt.Sprintf("--watched-dir=%s", rawDir),
					fmt.Sprintf("--target-dir=%s", path.Join(vmscrapes.ExtraConfigOutDir, rawBasename)),
				)
			}
		}
		containers = append(containers, configReloader)
	}

	if cr.Spec.VMBackup != nil {
		vmBackupManagerContainer, err := build.VMBackupManager(ctx, cr.Spec.VMBackup, cr.Spec.Port, storagePath, commonMounts, cr.Spec.ExtraArgs, false, cr.Spec.License)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			containers = append(containers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMBackup.Restore != nil &&
			cr.Spec.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := build.VMRestore(cr.Spec.VMBackup, storagePath, commonMounts)
			if err != nil {
				return nil, err
			}
			if vmRestore != nil {
				ic = append(ic, *vmRestore)
			}
		}
	}

	build.AddStrictSecuritySettingsToContainers(ic, &cr.Spec.CommonAppsParams)
	ic, err = k8stools.MergePatchContainers(ic, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply initContainer patch: %w", err)
	}

	build.AddStrictSecuritySettingsToContainers(containers, &cr.Spec.CommonAppsParams)
	containers, err = k8stools.MergePatchContainers(containers, cr.Spec.Containers)
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

func buildScrape(cr *vmv1beta1.VMSingle, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr, "vmbackupmanager")
}

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle) error {
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
	svc := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		addExtraPorts(svc, cr.Spec.VMBackup)
		build.AppendInsertPortsToService(cr.Spec.InsertPorts, svc)
	})

	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil {
		prevSvc = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			addExtraPorts(svc, prevCR.Spec.VMBackup)
			build.AppendInsertPortsToService(prevCR.Spec.InsertPorts, svc)
		})
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.ServiceSpec)
	}
	owner := cr.AsOwner()
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("vmsingle additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmsingle: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile service for vmsingle: %w", err)
	}
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		svs := buildScrape(cr, svc)
		prevSvs := buildScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create serviceScrape for vmsingle: %w", err)
		}
	}
	return nil
}

// buildRelabelingsAssets combines all possible relabeling config configuration and adding it to the configmap.
func buildRelabelingsAssets(cr *vmv1beta1.VMSingle, ac *build.AssetsCache) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: build.ResourceMeta(build.RelabelConfigResourceKind, cr),
		Data:       make(map[string]string),
	}
	if len(cr.Spec.InlineRelabelConfig) > 0 {
		rcs := vmscrapes.AddRelabelConfigs(nil, cr.Spec.InlineRelabelConfig)
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
	owner := cr.AsOwner()
	_, err = reconcile.ConfigMap(ctx, rclient, assestsCM, prevConfigMeta, &owner)
	return err
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
	owner := cr.AsOwner()
	_, err = reconcile.ConfigMap(ctx, rclient, streamAggrCM, prevCMMeta, &owner)
	return err
}

func createOrUpdateVPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle) error {
	if cr.Spec.VPA == nil {
		return nil
	}
	targetRef := autoscalingv1.CrossVersionObjectReference{
		Name:       cr.PrefixedName(),
		Kind:       string(vmv1beta1.WorkloadKindDeployment),
		APIVersion: "apps/v1",
	}
	newVPA := build.VPA(cr, targetRef, cr.Spec.VPA)
	var prevVPA *vpav1.VerticalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VPA != nil {
		prevVPA = build.VPA(prevCR, targetRef, prevCR.Spec.VPA)
	}
	owner := cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) error {
	// TODO check storage for nil

	svcName := cr.PrefixedName()
	keepServices := sets.New[string](svcName)
	keepServiceScrapes := sets.New[string]()
	keepConfigMaps := sets.New[string]()
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		keepServiceScrapes.Insert(svcName)
	}
	if cr.HasAnyRelabellingConfigs() {
		keepConfigMaps.Insert(build.ResourceName(build.RelabelConfigResourceKind, cr))
	}
	if cr.HasAnyStreamAggrRule() {
		keepConfigMaps.Insert(build.ResourceName(build.StreamAggrConfigResourceKind, cr))
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
	if err := finalize.RemoveOrphanedConfigMaps(ctx, rclient, cr, keepConfigMaps, true); err != nil {
		return fmt.Errorf("cannot remove configmaps: %w", err)
	}

	cfg := config.MustGetBaseConfig()
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	var objsToRemove []client.Object
	if cfg.VPAAPIEnabled && cr.Spec.VPA == nil {
		objsToRemove = append(objsToRemove, &vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta})
	}
	if !cr.IsOwnsServiceAccount() {
		objsToRemove = append(objsToRemove, &corev1.ServiceAccount{ObjectMeta: objMeta})
		rbacName := cr.GetRBACName()
		if len(cfg.WatchNamespaces) == 0 {
			objsToRemove = append(objsToRemove,
				&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: rbacName}},
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: rbacName}},
			)
		} else {
			for _, ns := range cfg.WatchNamespaces {
				objsToRemove = append(objsToRemove,
					&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: ns}},
					&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: ns}},
				)
			}
		}
	}
	return finalize.SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, cr)
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) *build.AssetsCache {
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

// CreateOrUpdateScrapeConfig builds scrape configuration for VMSingle
func CreateOrUpdateScrapeConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle, childObject client.Object) error {
	var prevCR *vmv1beta1.VMSingle
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
	}
	ac := getAssetsCache(ctx, rclient, cr)
	if _, err := createOrUpdateScrapeConfig(ctx, rclient, cr, prevCR, childObject, ac); err != nil {
		return err
	}
	return nil
}

func createOrUpdateScrapeConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle, childObject client.Object, ac *build.AssetsCache) (int, error) {
	if ptr.Deref(cr.Spec.IngestOnlyMode, true) {
		return 0, nil
	}

	pos := &vmscrapes.ParsedObjects{
		Namespace:            cr.Namespace,
		APIServerConfig:      cr.Spec.APIServerConfig,
		HasClusterWideAccess: config.IsClusterWideAccessAllowed() || !cr.IsOwnsServiceAccount(),
		ExternalLabels:       cr.ExternalLabels(),
	}
	if !pos.HasClusterWideAccess {
		logger.WithContext(ctx).Info("Setting discovery for the single namespace only." +
			"Since operator launched with set WATCH_NAMESPACE param. " +
			"Set custom ServiceAccountName property for VMSingle if needed.")
		pos.IgnoreNamespaceSelectors = true
	}
	sp := &cr.Spec.CommonScrapeParams
	if err := pos.Init(ctx, rclient, sp); err != nil {
		return 0, err
	}
	pos.ValidateObjects(sp)

	cfgBase, jobs, err := pos.BuildScrapeJobsConfig(ctx, sp, ac)
	if err != nil {
		return 0, fmt.Errorf("generating config for vmsingle failed: %w", err)
	}

	buckets, err := build.PackItems(jobs, config.MustGetBaseConfig().ConfigDataBudgetBytes, 150)
	if err != nil {
		return 0, fmt.Errorf("splitting scrape config into buckets for vmsingle: %w", err)
	}
	extraCount := len(buckets) - 1

	mainCfg := cfgBase
	if extraCount > 0 {
		mainCfg = append(mainCfg, yaml.MapItem{
			Key:   "scrape_config_files",
			Value: []string{vmscrapes.ExtraConfigFilesGlob},
		})
	}
	mainCfg = append(mainCfg, yaml.MapItem{Key: "scrape_configs", Value: buckets[0]})
	generatedConfig, err := yaml.Marshal(mainCfg)
	if err != nil {
		return 0, fmt.Errorf("marshalling config for vmsingle: %w", err)
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
			d, err := build.GzipConfig(generatedConfig)
			if err != nil {
				return 0, fmt.Errorf("cannot gzip config for vmsingle: %w", err)
			}
			secret.Data[scrapeGzippedFilename] = d
		}
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		secret.Annotations = map[string]string{
			"generated": "true",
		}
		if err := reconcile.Secret(ctx, rclient, &secret, prevSecretMeta, &owner); err != nil {
			return 0, err
		}
	}

	for i, bucket := range buckets[1:] {
		idx := i + 1
		extraData, err := yaml.Marshal(bucket)
		if err != nil {
			return 0, fmt.Errorf("marshalling extra scrape config bucket %d for vmsingle: %w", idx, err)
		}
		compressed, err := build.GzipConfig(extraData)
		if err != nil {
			return 0, fmt.Errorf("gzipping extra scrape config bucket %d for vmsingle: %w", idx, err)
		}
		s := vmscrapes.BuildExtraConfigSecret(cr, idx, compressed)
		if err := reconcile.Secret(ctx, rclient, s, nil, &owner); err != nil {
			return 0, err
		}
	}

	if err := vmscrapes.RemoveStaleExtraConfigSecrets(ctx, rclient, cr, extraCount); err != nil {
		return 0, err
	}

	parentName := fmt.Sprintf("%s.%s.vmsingle", cr.Name, cr.Namespace)
	if err := pos.UpdateStatusesForScrapeObjects(ctx, rclient, parentName, childObject); err != nil {
		return 0, err
	}

	return extraCount, nil
}
