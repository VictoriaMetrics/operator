package factory

import (
	"context"
	"fmt"
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/coreos/prometheus-operator/pkg/k8sutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"path"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	vmAlertConfigDir = "/etc/vmalert/config"
)

func CreateOrUpdateVmAlertService(ctx context.Context, cr *victoriametricsv1beta1.VmAlert, rclient client.Client, c *conf.BaseOperatorConf) (*corev1.Service, error) {
	l := log.WithValues("controller", "vmalert.service.crud", "vmalert", cr.Name())
	newService := newServiceVmAlert(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: newService.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vm vmalert")
			err := rclient.Create(ctx, newService)
			if err != nil {
				return nil, fmt.Errorf("cannot create new service for vmalert: %w", err)
			}
		} else {
			return nil, fmt.Errorf("cannot get vmalert service: %w", err)
		}
	}
	for annotation, value := range currentService.Annotations {
		newService.Annotations[annotation] = value
	}
	if currentService.Spec.ClusterIP != "" {
		newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	}
	if currentService.ResourceVersion != "" {
		newService.ResourceVersion = currentService.ResourceVersion
	}
	err = rclient.Update(ctx, newService)
	if err != nil {
		return nil, fmt.Errorf("cannot update vmalert service: %w", err)
	}
	l.Info("vmalert svc reconciled")
	return newService, nil
}

func newServiceVmAlert(cr *victoriametricsv1beta1.VmAlert, c *conf.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmAlertDefault.Port
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.FinalLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: cr.CommonLabels(),
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
}

func CreateOrUpdateVmAlert(ctx context.Context, cr *victoriametricsv1beta1.VmAlert, rclient client.Client, c *conf.BaseOperatorConf, cmNames []string) (reconcile.Result, error) {
	l := log.WithValues("controller", "vmalert.crud", "vmalert", cr.Name())
	//recon deploy
	l.Info("generating new deployment")
	newDeploy, err := newDeployForVmAlert(cr, c, cmNames)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot generate new deploy for vmalert: %w", err)
	}

	currDeploy := &appsv1.Deployment{}
	err = rclient.Get(ctx, types.NamespacedName{Namespace: newDeploy.Namespace, Name: newDeploy.Name}, currDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			//deploy not exists create it
			err := rclient.Create(ctx, newDeploy)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("cannot create vmalert deploy: %w", err)
			}
		} else {
			return reconcile.Result{}, fmt.Errorf("cannot get deploy for vmalert: %w", err)
		}
	}
	for annotation, value := range currDeploy.Annotations {
		newDeploy.Annotations[annotation] = value
	}
	for annotation, value := range currDeploy.Spec.Template.Annotations {
		newDeploy.Spec.Template.Annotations[annotation] = value
	}

	err = rclient.Update(ctx, newDeploy)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot update vmalert deploy: %w", err)
	}
	l.Info("reconciled vmalert deploy")

	return reconcile.Result{}, nil
}

// newDeployForCR returns a busybox pod with the same name/namespace as the cr
func newDeployForVmAlert(cr *victoriametricsv1beta1.VmAlert, c *conf.BaseOperatorConf, ruleConfigMapNames []string) (*appsv1.Deployment, error) {

	cr = cr.DeepCopy()
	if cr.Spec.Image == nil {
		cr.Spec.Image = &c.VmAlertDefault.Image
	}
	if cr.Spec.Version == "" {
		cr.Spec.Version = c.VmAgentDefault.Version
	}
	if cr.Spec.Resources.Requests == nil {
		cr.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.Resources.Limits == nil {
		cr.Spec.Resources.Limits = corev1.ResourceList{}

	}
	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceMemory]; !ok {
		cr.Spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VmAlertDefault.Resource.Limit.Mem)
	}
	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceCPU]; !ok {
		cr.Spec.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VmAlertDefault.Resource.Limit.Cpu)
	}

	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceMemory]; !ok {
		cr.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VmAlertDefault.Resource.Request.Mem)
	}
	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceCPU]; !ok {
		cr.Spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VmAlertDefault.Resource.Request.Cpu)
	}

	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmAlertDefault.Port
	}

	generatedSpec, err := vmAlertSpecGen(cr, c, ruleConfigMapNames)
	if err != nil {
		return nil, fmt.Errorf("cannot generate new spec for vmalert: %w", err)
	}

	if cr.Spec.ImagePullSecrets != nil && len(cr.Spec.ImagePullSecrets) > 0 {
		generatedSpec.Template.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.FinalLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: *generatedSpec,
	}
	return deploy, nil
}

func vmAlertSpecGen(cr *victoriametricsv1beta1.VmAlert, c *conf.BaseOperatorConf, ruleConfigMapNames []string) (*appsv1.DeploymentSpec, error) {
	cr = cr.DeepCopy()

	confReloadArgs := []string{
		fmt.Sprintf("-webhook-url=http://localhost:%s/-/reload", cr.Spec.Port),
	}

	args := []string{
		fmt.Sprintf("-notifier.url=%s", cr.Spec.NotifierURL),
		fmt.Sprintf("-datasource.url=%s", cr.Spec.Datasource.URL),
	}
	if cr.Spec.RemoteWrite.URL != "" {
		//this param cannot be used until v1.35.5 vm release with flag breaking changes
		args = append(args, fmt.Sprintf("-remoteWrite.url=%s", cr.Spec.RemoteWrite.URL))

	}
	if cr.Spec.RemoteRead.URL != "" {
		//this param cannot be used until v1.35.5 vm release with flag breaking changes
		args = append(args, fmt.Sprintf("-remoteRead.url=%s", cr.Spec.RemoteRead.URL))

	}
	if cr.Spec.EvaluationInterval != "" {
		args = append(args, fmt.Sprintf("-evaluationInterval=%s", cr.Spec.EvaluationInterval))
	}
	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}

	for _, cm := range ruleConfigMapNames {
		args = append(args, fmt.Sprintf("-rule=%s", path.Join(vmAlertConfigDir, cm, "*.yaml")))
	}

	for _, cm := range ruleConfigMapNames {
		confReloadArgs = append(confReloadArgs, fmt.Sprintf("-volume-dir=%s", path.Join(vmAlertConfigDir, cm)))
	}
	for arg, value := range cr.Spec.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s=%s", arg, value))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	for _, rulePath := range cr.Spec.RulePath {
		args = append(args, "-rule="+rulePath)
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.ExtraEnvs...)

	volumes := []corev1.Volume{}

	for _, name := range ruleConfigMapNames {
		volumes = append(volumes, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: name,
					},
				},
			},
		})
	}

	volumeMounts := []corev1.VolumeMount{}
	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8sutil.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(secretsDir, s),
		})
	}

	for _, c := range cr.Spec.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: k8sutil.SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(configmapsDir, c),
		})
	}

	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)

	for _, name := range ruleConfigMapNames {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      name,
			MountPath: path.Join(vmAlertConfigDir, name),
		})
	}
	reloaderVolumes := []corev1.VolumeMount{}
	for _, name := range ruleConfigMapNames {
		reloaderVolumes = append(reloaderVolumes, corev1.VolumeMount{
			Name:      name,
			MountPath: path.Join(vmAlertConfigDir, name),
		})
	}

	resources := corev1.ResourceRequirements{Limits: corev1.ResourceList{}}
	if c.VmAlertDefault.ConfigReloaderCPU != "0" {
		resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VmAlertDefault.ConfigReloaderCPU)
	}
	if c.VmAlertDefault.ConfigReloaderMemory != "0" {
		resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VmAlertDefault.ConfigReloaderMemory)
	}

	livenessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.Port),
			Scheme: "HTTP",
			Path:   "/health",
		},
	}
	readinessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.Port),
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

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})

	defaultContainers := []corev1.Container{
		{
			Args:                     args,
			Name:                     "vmalert",
			Image:                    *cr.Spec.Image + ":" + cr.Spec.Version,
			Ports:                    ports,
			VolumeMounts:             volumeMounts,
			LivenessProbe:            livenessProbe,
			ReadinessProbe:           readinessProbe,
			Resources:                cr.Spec.Resources,
			Env:                      envs,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		}, {
			Name:                     "config-reloader",
			Image:                    c.VmAlertDefault.ConfigReloadImage,
			Args:                     confReloadArgs,
			Resources:                resources,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			VolumeMounts:             reloaderVolumes,
		},
	}

	containers, err := MergePatchContainers(defaultContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}
	spec := &appsv1.DeploymentSpec{
		Replicas: cr.Spec.ReplicaCount,

		Selector: &metav1.LabelSelector{
			MatchLabels: cr.CommonLabels(),
		},

		Strategy: appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      cr.PodLabels(),
				Annotations: cr.PodAnnotations(),
			},
			Spec: corev1.PodSpec{
				Containers:  containers,
				Volumes:     volumes,
				Affinity:    cr.Spec.Affinity,
				Tolerations: cr.Spec.Tolerations,
			},
		},
	}
	return spec, nil
}
