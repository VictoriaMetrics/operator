package factory

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/psp"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	SecretsDir          = "/etc/vm/secrets"
	ConfigMapsDir       = "/etc/vm/configs"
	TemplatesDir        = "/etc/vm/templates"
	RelabelingConfigDir = "/etc/vm/relabeling"
	vmSingleDataDir     = "/victoria-metrics-data"
	vmBackuperCreds     = "/etc/vm/creds"
	vmDataVolumeName    = "data"
)

func CreateVMSingleStorage(ctx context.Context, cr *victoriametricsv1beta1.VMSingle, rclient client.Client) (*corev1.PersistentVolumeClaim, error) {

	l := log.WithValues("vm.single.pvc.create", cr.Name)
	l.Info("reconciling pvc")
	newPvc := makeVMSinglePvc(cr)
	existPvc := &corev1.PersistentVolumeClaim{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, existPvc)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new pvc for vmsingle")
			if err := rclient.Create(ctx, newPvc); err != nil {
				return nil, fmt.Errorf("cannot create new pvc for vmsingle: %w", err)
			}

			return newPvc, nil
		}
		return nil, fmt.Errorf("cannot get existing pvc for vmsingle: %w", err)
	}
	if existPvc.Spec.Resources.String() != newPvc.Spec.Resources.String() {
		l.Info("volume requests isn't same, update required")
	}
	newResources := newPvc.Spec.Resources.DeepCopy()
	newPvc.Spec = existPvc.Spec
	newPvc.Spec.Resources = *newResources
	newPvc.Annotations = labels.Merge(existPvc.Annotations, newPvc.Annotations)
	newPvc.Finalizers = victoriametricsv1beta1.MergeFinalizers(existPvc, victoriametricsv1beta1.FinalizerName)

	if err := rclient.Update(ctx, newPvc); err != nil {
		return nil, err
	}

	return newPvc, nil
}

func makeVMSinglePvc(cr *victoriametricsv1beta1.VMSingle) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.PrefixedName(),
			Namespace:   cr.Namespace,
			Labels:      labels.Merge(cr.Spec.StorageMetadata.Labels, cr.SelectorLabels()),
			Annotations: cr.Spec.StorageMetadata.Annotations,
			Finalizers:  []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: *cr.Spec.Storage,
	}
	if cr.Spec.RemovePvcAfterDelete {
		pvcObject.OwnerReferences = cr.AsOwner()
	}
	return pvcObject
}

func CreateOrUpdateVMSingle(ctx context.Context, cr *victoriametricsv1beta1.VMSingle, rclient client.Client, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {

	if err := psp.CreateServiceAccountForCRD(ctx, cr, rclient); err != nil {
		return nil, fmt.Errorf("failed create service account: %w", err)
	}
	if c.PSPAutoCreateEnabled {
		if err := psp.CreateOrUpdateServiceAccountWithPSP(ctx, cr, rclient); err != nil {
			return nil, fmt.Errorf("cannot create podsecurity policy for vmsingle, err=%w", err)
		}
	}
	newDeploy, err := newDeployForVMSingle(cr, c)
	if err != nil {
		return nil, fmt.Errorf("cannot generate new deploy for vmsingle: %w", err)
	}

	if err := k8stools.HandleDeployUpdate(ctx, rclient, newDeploy); err != nil {
		return nil, err
	}

	return newDeploy, nil
}

func newDeployForVMSingle(cr *victoriametricsv1beta1.VMSingle, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {
	cr = cr.DeepCopy()

	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMSingleDefault.Image
	}
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMSingleDefault.Version
	}
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMSingleDefault.Port
	}
	if cr.Spec.Image.PullPolicy == "" {
		cr.Spec.Image.PullPolicy = corev1.PullIfNotPresent
	}
	podSpec, err := makeSpecForVMSingle(cr, c)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.AllLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.ReplicaCount,
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
	return depSpec, nil
}

func makeSpecForVMSingle(cr *victoriametricsv1beta1.VMSingle, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod),
	}

	// if customStorageDataPath is not empty, do not add pvc.
	shouldAddPVC := cr.Spec.StorageDataPath == ""

	storagePath := vmSingleDataDir
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
	if len(cr.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	args = buildArgsForAdditionalPorts(args, cr.Spec.InsertPorts)

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = buildAdditionalContainerPorts(ports, cr.Spec.InsertPorts)
	volumes := []corev1.Volume{}

	storageSpec := cr.Spec.Storage

	if storageSpec == nil {
		volumes = append(volumes, corev1.Volume{
			Name: vmDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if shouldAddPVC {
		volumes = append(volumes, corev1.Volume{
			Name: vmDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cr.PrefixedName(),
				},
			},
		})
	}
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
	vmMounts := []corev1.VolumeMount{
		{
			Name:      vmDataVolumeName,
			MountPath: storagePath,
		},
	}

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
			MountPath: path.Join(SecretsDir, s),
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
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

	args = addExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	vmsingleContainer := corev1.Container{
		Name:                     "vmsingle",
		Image:                    fmt.Sprintf("%s:%s", formatContainerImage(c.ContainerRegistry, cr.Spec.Image.Repository), cr.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                buildResources(cr.Spec.Resources, config.Resource(c.VMSingleDefault.Resource), c.VMSingleDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}

	vmsingleContainer = buildProbe(vmsingleContainer, cr)

	operatorContainers := []corev1.Container{vmsingleContainer}
	initContainers := cr.Spec.InitContainers

	if cr.Spec.VMBackup != nil {
		vmBackupManagerContainer, err := makeSpecForVMBackuper(cr.Spec.VMBackup, c, cr.Spec.Port, storagePath, vmDataVolumeName, cr.Spec.ExtraArgs, false)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			operatorContainers = append(operatorContainers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMBackup.Restore != nil &&
			cr.Spec.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := makeSpecForVMRestore(cr.Spec.VMBackup, c, storagePath, vmDataVolumeName)
			if err != nil {
				return nil, err
			}
			if vmRestore != nil {
				initContainers = append(initContainers, *vmRestore)
			}
		}
	}

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
			NodeSelector:                  cr.Spec.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                initContainers,
			Containers:                    containers,
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               cr.Spec.SecurityContext,
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.Affinity,
			RuntimeClassName:              cr.Spec.RuntimeClassName,
			SchedulerName:                 cr.Spec.SchedulerName,
			Tolerations:                   cr.Spec.Tolerations,
			PriorityClassName:             cr.Spec.PriorityClassName,
			HostNetwork:                   cr.Spec.HostNetwork,
			DNSPolicy:                     cr.Spec.DNSPolicy,
			DNSConfig:                     cr.Spec.DNSConfig,
			TopologySpreadConstraints:     cr.Spec.TopologySpreadConstraints,
			HostAliases:                   cr.Spec.HostAliases,
			TerminationGracePeriodSeconds: cr.Spec.TerminationGracePeriodSeconds,
			ReadinessGates:                cr.Spec.ReadinessGates,
		},
	}

	return vmSingleSpec, nil

}

func CreateOrUpdateVMSingleService(ctx context.Context, cr *victoriametricsv1beta1.VMSingle, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {

	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMSingleDefault.Port
	}
	addBackupPort := func(svc *corev1.Service) {
		if cr.Spec.VMBackup != nil {
			if cr.Spec.VMBackup.Port == "" {
				cr.Spec.VMBackup.Port = c.VMBackup.Port
			}
			parsedPort := intstr.Parse(cr.Spec.VMBackup.Port)
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "vmbackupmanager",
				Protocol:   corev1.ProtocolTCP,
				Port:       parsedPort.IntVal,
				TargetPort: parsedPort,
			})
		}
	}
	newService := buildDefaultService(cr, cr.Spec.Port, addBackupPort)
	buildAdditionalServicePorts(cr.Spec.InsertPorts, newService)

	if cr.Spec.ServiceSpec != nil {
		additionalService := buildDefaultService(cr, cr.Spec.Port, nil)
		mergeServiceSpec(additionalService, cr.Spec.ServiceSpec)
		buildAdditionalServicePorts(cr.Spec.InsertPorts, additionalService)
		if additionalService.Name == newService.Name {
			log.Error(fmt.Errorf("vmsingle additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if _, err := reconcileServiceForCRD(ctx, rclient, additionalService); err != nil {
			return nil, err
		}
	}

	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.SelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: cr.PrefixedName}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.ServiceSpec); err != nil {
		return nil, err
	}

	return reconcileServiceForCRD(ctx, rclient, newService)
}

func makeSpecForVMBackuper(
	cr *victoriametricsv1beta1.VMBackup,
	c *config.BaseOperatorConf,
	port string,
	storagePath, dataVolumeName string,
	extraArgs map[string]string,
	isCluster bool,
) (*corev1.Container, error) {
	if !cr.AcceptEULA {
		log.Info("EULA wasn't accepted, update your backup setting. You must switch to victoriametrics/vmbackupmanager:v1.56.0-enterprise  image or higher.")
		return nil, nil
	}
	if cr.Image.Repository == "" {
		cr.Image.Repository = c.VMBackup.Image
	}
	if cr.Image.Tag == "" {
		cr.Image.Tag = c.VMBackup.Version
	}
	if cr.Image.PullPolicy == "" {
		cr.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if cr.Port == "" {
		cr.Port = c.VMBackup.Port
	}

	snapshotCreateURL := cr.SnapshotCreateURL
	snapshotDeleteURL := cr.SnapShotDeleteURL
	if snapshotCreateURL == "" {
		//http://localhost:port/snaphsot/create
		snapshotCreateURL = cr.SnapshotCreatePathWithFlags(port, extraArgs)
	}
	if snapshotDeleteURL == "" {
		//http://localhost:port/snaphsot/delete
		snapshotDeleteURL = cr.SnapshotDeletePathWithFlags(port, extraArgs)
	}
	backupDst := cr.Destination
	// add suffix with pod name for cluster backupmanager
	// it's needed to create consistent backup across cluster nodes
	if isCluster && !cr.DestinationDisableSuffixAdd {
		backupDst = strings.TrimSuffix(backupDst, "/") + "/$(POD_NAME)/"
	}
	args := []string{
		fmt.Sprintf("-storageDataPath=%s", storagePath),
		fmt.Sprintf("-dst=%s", backupDst),
		fmt.Sprintf("-snapshot.createURL=%s", snapshotCreateURL),
		fmt.Sprintf("-snapshot.deleteURL=%s", snapshotDeleteURL),
		"-eula",
	}
	if cr.LogLevel != nil {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", *cr.LogLevel))
	}
	if cr.LogFormat != nil {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", *cr.LogFormat))
	}
	for arg, value := range cr.ExtraArgs {
		args = append(args, fmt.Sprintf("-%s=%s", arg, value))
	}
	if cr.Concurrency != nil {
		args = append(args, fmt.Sprintf("-concurrency=%d", *cr.Concurrency))
	}
	if cr.CustomS3Endpoint != nil {
		args = append(args, fmt.Sprintf("-customS3Endpoint=%s", *cr.CustomS3Endpoint))
	}
	if cr.DisableHourly != nil && *cr.DisableHourly {
		args = append(args, "-disableHourly")
	}
	if cr.DisableDaily != nil && *cr.DisableDaily {
		args = append(args, "-disableDaily")
	}
	if cr.DisableMonthly != nil && *cr.DisableMonthly {
		args = append(args, "-disableMonthly")
	}
	if cr.DisableWeekly != nil && *cr.DisableWeekly {
		args = append(args, "-disableWeekly")
	}

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Port).IntVal})

	mounts := []corev1.VolumeMount{
		{
			Name:      dataVolumeName,
			MountPath: storagePath,
			ReadOnly:  false,
		},
	}
	mounts = append(mounts, cr.VolumeMounts...)

	if cr.CredentialsSecret != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + cr.CredentialsSecret.Name),
			MountPath: vmBackuperCreds,
			ReadOnly:  true,
		})
		args = append(args, fmt.Sprintf("-credsFilePath=%s/%s", vmBackuperCreds, cr.CredentialsSecret.Key))
	}
	extraEnvs := cr.ExtraEnvs
	if len(cr.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	// expose POD_NAME information by default
	// its needed to create uniq path for backup
	extraEnvs = append(extraEnvs, corev1.EnvVar{
		Name: "POD_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: "metadata.name",
			},
		},
	})

	livenessProbeHandler := corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	readinessProbeHandler := corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	livenessFailureThreshold := int32(3)
	livenessProbe := &corev1.Probe{
		ProbeHandler:     livenessProbeHandler,
		PeriodSeconds:    5,
		TimeoutSeconds:   probeTimeoutSeconds,
		FailureThreshold: livenessFailureThreshold,
	}
	readinessProbe := &corev1.Probe{
		ProbeHandler:     readinessProbeHandler,
		TimeoutSeconds:   probeTimeoutSeconds,
		PeriodSeconds:    5,
		FailureThreshold: 10,
	}

	sort.Strings(args)
	vmBackuper := &corev1.Container{
		Name:                     "vmbackuper",
		Image:                    fmt.Sprintf("%s:%s", formatContainerImage(c.ContainerRegistry, cr.Image.Repository), cr.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		Env:                      extraEnvs,
		VolumeMounts:             mounts,
		LivenessProbe:            livenessProbe,
		ReadinessProbe:           readinessProbe,
		Resources:                buildResources(cr.Resources, config.Resource(c.VMBackup.Resource), c.VMBackup.UseDefaultResources),
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return vmBackuper, nil
}

func makeSpecForVMRestore(
	cr *victoriametricsv1beta1.VMBackup,
	c *config.BaseOperatorConf,
	storagePath, dataVolumeName string,
) (*corev1.Container, error) {
	if cr.Image.Repository == "" {
		cr.Image.Repository = c.VMBackup.Image
	}
	if cr.Image.Tag == "" {
		cr.Image.Tag = c.VMBackup.Version
	}
	if cr.Image.PullPolicy == "" {
		cr.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if cr.Port == "" {
		cr.Port = c.VMBackup.Port
	}

	args := []string{
		fmt.Sprintf("-storageDataPath=%s", storagePath),
		"-eula",
	}

	if cr.LogLevel != nil {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", *cr.LogLevel))
	}
	if cr.LogFormat != nil {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", *cr.LogFormat))
	}
	for arg, value := range cr.ExtraArgs {
		args = append(args, fmt.Sprintf("-%s=%s", arg, value))
	}
	if cr.Concurrency != nil {
		args = append(args, fmt.Sprintf("-concurrency=%d", *cr.Concurrency))
	}
	if cr.CustomS3Endpoint != nil {
		args = append(args, fmt.Sprintf("-customS3Endpoint=%s", *cr.CustomS3Endpoint))
	}

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Port).IntVal})

	mounts := []corev1.VolumeMount{
		{
			Name:      dataVolumeName,
			MountPath: storagePath,
			ReadOnly:  false,
		},
	}
	mounts = append(mounts, cr.VolumeMounts...)

	if cr.CredentialsSecret != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + cr.CredentialsSecret.Name),
			MountPath: vmBackuperCreds,
			ReadOnly:  true,
		})
		args = append(args, fmt.Sprintf("-credsFilePath=%s/%s", vmBackuperCreds, cr.CredentialsSecret.Key))
	}
	extraEnvs := cr.ExtraEnvs
	if len(cr.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	sort.Strings(args)

	args = append([]string{"restore"}, args...)

	vmRestore := &corev1.Container{
		Name:                     "vmbackuper-restore",
		Image:                    fmt.Sprintf("%s:%s", formatContainerImage(c.ContainerRegistry, cr.Image.Repository), cr.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		Env:                      extraEnvs,
		VolumeMounts:             mounts,
		Resources:                buildResources(cr.Resources, config.Resource(c.VMBackup.Resource), c.VMBackup.UseDefaultResources),
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return vmRestore, nil
}
