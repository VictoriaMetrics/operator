package factory

import (
	"context"
	"fmt"
	"path"
	"sort"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/psp"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	SecretsDir       = "/etc/vm/secrets"
	ConfigMapsDir    = "/etc/vm/configs"
	vmSingleDataDir  = "/victoria-metrics-data"
	vmBackuperCreds  = "/etc/vm/creds"
	vmDataVolumeName = "data"
)

func CreateVMStorage(ctx context.Context, cr *victoriametricsv1beta1.VMSingle, rclient client.Client, c *config.BaseOperatorConf) (*corev1.PersistentVolumeClaim, error) {

	l := log.WithValues("vm.single.pvc.create", cr.Name)
	l.Info("reconciling pvc")
	newPvc := makeVMSinglePvc(cr, c)
	existPvc := &corev1.PersistentVolumeClaim{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, existPvc)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new pvc for vmsingle")
			if err := rclient.Create(ctx, newPvc); err != nil {
				return nil, fmt.Errorf("cannot create new pvc for vmsingle: %w", err)
			}

			return newPvc, nil
		} else {
			return nil, fmt.Errorf("cannot get existing pvc for vmsingle: %w", err)
		}
	}

	if existPvc.Spec.Resources.String() != newPvc.Spec.Resources.String() {
		l.Info("volume requests isn't same, update required")
		existPvc.Spec.Resources = newPvc.Spec.Resources
		err := rclient.Update(ctx, existPvc)
		if err != nil {
			l.Error(err, "cannot update pvc size, we can suppress it")
		}
	}
	newPvc = existPvc

	return newPvc, nil
}

func makeVMSinglePvc(cr *victoriametricsv1beta1.VMSingle, c *config.BaseOperatorConf) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.PrefixedName(),
			Namespace:   cr.Namespace,
			Labels:      c.Labels.Merge(cr.Labels()),
			Annotations: cr.Annotations(),
		},
		Spec: *cr.Spec.Storage,
	}
	if cr.Spec.RemovePvcAfterDelete {
		pvcObject.OwnerReferences = cr.AsOwner()
	}
	return pvcObject
}

func CreateOrUpdateVMSingle(ctx context.Context, cr *victoriametricsv1beta1.VMSingle, rclient client.Client, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {

	l := log.WithValues("controller", "vmsingle.crud", "vmsingle", cr.Name)
	l.Info("create or update vm single deploy")

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

	l = l.WithValues("single.deploy.name", newDeploy.Name, "single.deploy.namespace", newDeploy.Namespace)

	currentDeploy := &appsv1.Deployment{}
	err = rclient.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, currentDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			//create new
			l.Info("vmsingle deploy not found, creating new one")
			err := rclient.Create(ctx, newDeploy)
			if err != nil {
				return nil, fmt.Errorf("cannot create new vmsingle deploy: %w", err)
			}
			l.Info("new vmsingle deploy was created")
		} else {
			return nil, fmt.Errorf("cannot get vmsingle deploy: %w", err)
		}
	}
	l.Info("vm vmsingle was found, updating it")

	newDeploy.Annotations = labels.Merge(newDeploy.Annotations, currentDeploy.Annotations)
	newDeploy.Spec.Template.Annotations = labels.Merge(newDeploy.Spec.Template.Annotations, currentDeploy.Spec.Template.Annotations)

	err = rclient.Update(ctx, newDeploy)
	if err != nil {
		return nil, fmt.Errorf("cannot upddate vmsingle deploy: %w", err)
	}
	l.Info("single deploy reconciled")

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

	if cr.Spec.Resources.Requests == nil {
		cr.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.Resources.Limits == nil {
		cr.Spec.Resources.Limits = corev1.ResourceList{}
	}

	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if !cpuResourceIsSet && c.VMSingleDefault.UseDefaultResources {
		cr.Spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMSingleDefault.Resource.Request.Cpu)
		cr.Spec.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMSingleDefault.Resource.Limit.Cpu)

	}
	if !memResourceIsSet && c.VMSingleDefault.UseDefaultResources {
		cr.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMSingleDefault.Resource.Request.Mem)
		cr.Spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMSingleDefault.Resource.Limit.Mem)
	}
	podSpec, err := makeSpecForVMSingle(cr, c)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.Labels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				//we use recreate, coz of volume claim
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: *podSpec,
		},
	}

	return depSpec, nil
}

func makeSpecForVMSingle(cr *victoriametricsv1beta1.VMSingle, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-storageDataPath=%s", vmSingleDataDir),
		fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod),
	}
	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}

	for arg, value := range cr.Spec.ExtraArgs {
		args = append(args, fmt.Sprintf("-%s=%s", arg, value))
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
	} else {
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
			MountPath: vmSingleDataDir,
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

	livenessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.Port),
			Scheme: "HTTP",
			Path:   cr.HealthPath(),
		},
	}
	readinessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.Port),
			Scheme: "HTTP",
			Path:   cr.HealthPath(),
		},
	}
	livenessFailureThreshold := int32(3)
	livenessProbe := &corev1.Probe{
		Handler:          livenessProbeHandler,
		PeriodSeconds:    5,
		TimeoutSeconds:   probeTimeoutSeconds,
		FailureThreshold: livenessFailureThreshold,
	}
	readinessProbe := &corev1.Probe{
		Handler:          readinessProbeHandler,
		TimeoutSeconds:   probeTimeoutSeconds,
		PeriodSeconds:    5,
		FailureThreshold: 10,
	}

	var additionalContainers []corev1.Container

	sort.Strings(args)
	operatorContainers := append([]corev1.Container{
		{
			Name:                     "vmsingle",
			Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
			Ports:                    ports,
			Args:                     args,
			VolumeMounts:             vmMounts,
			LivenessProbe:            livenessProbe,
			ReadinessProbe:           readinessProbe,
			Resources:                cr.Spec.Resources,
			Env:                      envs,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		},
	}, additionalContainers...)

	if cr.Spec.VMBackup != nil {
		vmBackuper, err := makeSpecForVMBackuper(cr.Spec.VMBackup, c, cr.Spec.Port, vmDataVolumeName, cr.Spec.ExtraArgs)
		if err != nil {
			return nil, err
		}
		operatorContainers = append(operatorContainers, *vmBackuper)
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
			Volumes:                   volumes,
			InitContainers:            cr.Spec.InitContainers,
			Containers:                containers,
			ServiceAccountName:        cr.GetServiceAccountName(),
			SecurityContext:           cr.Spec.SecurityContext,
			ImagePullSecrets:          cr.Spec.ImagePullSecrets,
			Affinity:                  cr.Spec.Affinity,
			RuntimeClassName:          cr.Spec.RuntimeClassName,
			SchedulerName:             cr.Spec.SchedulerName,
			Tolerations:               cr.Spec.Tolerations,
			PriorityClassName:         cr.Spec.PriorityClassName,
			HostNetwork:               cr.Spec.HostNetwork,
			DNSPolicy:                 cr.Spec.DNSPolicy,
			TopologySpreadConstraints: cr.Spec.TopologySpreadConstraints,
			HostAliases:               cr.Spec.HostAliases,
		},
	}

	return vmSingleSpec, nil

}

func CreateOrUpdateVMSingleService(ctx context.Context, cr *victoriametricsv1beta1.VMSingle, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	l := log.WithValues("controller", "vmalert.service.crud")
	newService := newServiceVMSingle(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: newService.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vm vmsingle")
			err := rclient.Create(ctx, newService)
			if err != nil {
				return nil, fmt.Errorf("cannot create new service for vmsingle")
			}
		} else {
			return nil, fmt.Errorf("cannot get vmsingle service: %w", err)
		}
	}
	newService.Annotations = labels.Merge(newService.Annotations, currentService.Annotations)
	if currentService.Spec.ClusterIP != "" {
		newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	}
	if currentService.ResourceVersion != "" {
		newService.ResourceVersion = currentService.ResourceVersion
	}
	err = rclient.Update(ctx, newService)
	if err != nil {
		return nil, fmt.Errorf("cannot update vmsingle service: %w", err)
	}
	l.Info("vmsingle svc reconciled")
	return newService, nil
}

func newServiceVMSingle(cr *victoriametricsv1beta1.VMSingle, c *config.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMSingleDefault.Port
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.Labels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: cr.SelectorLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       intstr.Parse(cr.Spec.Port).IntVal,
					TargetPort: intstr.Parse(cr.Spec.Port),
				},
			},
		},
	}
	buildAdditionalServicePorts(cr.Spec.InsertPorts, svc)
	return svc
}

func makeSpecForVMBackuper(
	cr *victoriametricsv1beta1.VMBackup,
	c *config.BaseOperatorConf,
	port string,
	dataVolumeName string,
	extraArgs map[string]string,
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
	if cr.Resources.Requests == nil {
		cr.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Resources.Limits == nil {
		cr.Resources.Limits = corev1.ResourceList{}
	}
	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := cr.Resources.Limits[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Resources.Limits[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := cr.Resources.Requests[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Resources.Requests[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if !cpuResourceIsSet && c.VMBackup.UseDefaultResources {
		cr.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMBackup.Resource.Request.Cpu)
		cr.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMBackup.Resource.Limit.Cpu)
	}
	if !memResourceIsSet && c.VMBackup.UseDefaultResources {
		cr.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMBackup.Resource.Request.Mem)
		cr.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMBackup.Resource.Limit.Mem)
	}

	args := []string{
		fmt.Sprintf("-storageDataPath=%s", vmSingleDataDir),
		fmt.Sprintf("-dst=%s", cr.Destination),
		//http://localhost:port/snaphsot/create
		fmt.Sprintf("-snapshot.createURL=%s", cr.SnapshotCreatePathWithFlags(port, extraArgs)),
		//http://localhost:port/snaphsot/delete
		fmt.Sprintf("-snapshot.deleteURL=%s", cr.SnapshotDeletePathWithFlags(port, extraArgs)),
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

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Port).IntVal})

	mounts := []corev1.VolumeMount{
		{
			Name:      dataVolumeName,
			MountPath: vmSingleDataDir,
			ReadOnly:  true,
		},
	}
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

	livenessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	readinessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	livenessFailureThreshold := int32(3)
	livenessProbe := &corev1.Probe{
		Handler:          livenessProbeHandler,
		PeriodSeconds:    5,
		TimeoutSeconds:   probeTimeoutSeconds,
		FailureThreshold: livenessFailureThreshold,
	}
	readinessProbe := &corev1.Probe{
		Handler:          readinessProbeHandler,
		TimeoutSeconds:   probeTimeoutSeconds,
		PeriodSeconds:    5,
		FailureThreshold: 10,
	}

	sort.Strings(args)
	vmBackuper := &corev1.Container{
		Name:                     "vmbackuper",
		Image:                    fmt.Sprintf("%s:%s", cr.Image.Repository, cr.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		Env:                      extraEnvs,
		VolumeMounts:             mounts,
		LivenessProbe:            livenessProbe,
		ReadinessProbe:           readinessProbe,
		Resources:                cr.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return vmBackuper, nil
}
