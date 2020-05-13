package factory

import (
	"context"
	"fmt"
	"github.com/VictoriaMetrics/operator/conf"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/coreos/prometheus-operator/pkg/k8sutil"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"path"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
)

const (
	vmAlertConfigDir = "/etc/vmalert/config"
)

func CreateOrUpdateVmAlertService(cr *victoriametricsv1beta1.VmAlert, rclient client.Client, c *conf.BaseOperatorConf, l logr.Logger) (*corev1.Service, error) {
	l = l.WithValues("recon.vmalert.service.name", cr.Name)
	newSvc := newServiceVmAlert(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(context.TODO(), types.NamespacedName{Namespace: cr.Namespace, Name: newSvc.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vm vmalert")
			err := rclient.Create(context.TODO(), newSvc)
			if err != nil {
				l.Error(err, "cannot create new service for vmalert")
				return nil, err
			}
		} else {
			l.Error(err, "cannot get vmalert service for recon")
			return nil, err
		}
	}
	if currentService.Annotations != nil {
		newSvc.Annotations = currentService.Annotations
	}
	if currentService.Spec.ClusterIP != "" {
		newSvc.Spec.ClusterIP = currentService.Spec.ClusterIP
	}
	if currentService.ResourceVersion != "" {
		newSvc.ResourceVersion = currentService.ResourceVersion
	}
	err = rclient.Update(context.TODO(), newSvc)
	if err != nil {
		l.Error(err, "cannot update vmalert service")
		return nil, err
	}
	l.Info("vmalert svc reconciled")
	return newSvc, nil
}

func newServiceVmAlert(cr *victoriametricsv1beta1.VmAlert, c *conf.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmAlertDefault.Port
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        prefixedAlertName(cr.Name),
			Namespace:   cr.Namespace,
			Labels:      c.Labels.Merge(cr.ObjectMeta.Labels),
			Annotations: cr.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         cr.APIVersion,
					Kind:               cr.Kind,
					Name:               cr.Name,
					UID:                cr.UID,
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLabelsVmAlert(cr),
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

func CreateOrUpdateVmAlert(cr *victoriametricsv1beta1.VmAlert, rclient client.Client, c *conf.BaseOperatorConf, cmNames []string, l logr.Logger) (reconcile.Result, error) {
	l = l.WithValues("create.or.update.vmalert.name", cr.Name)
	//recon deploy
	l.Info("generating new deployment")
	newDeploy, err := newDeployForVmAlert(cr, c, cmNames)
	if err != nil {
		return reconcile.Result{}, err
	}

	currDeploy := &appsv1.Deployment{}
	err = rclient.Get(context.TODO(), types.NamespacedName{Namespace: newDeploy.Namespace, Name: newDeploy.Name}, currDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			//deploy not exists create it
			err := rclient.Create(context.TODO(), newDeploy)
			if err != nil {
				l.Error(err, "cannot create vmalert deploy")
				return reconcile.Result{}, err
			}
		} else {
			l.Error(err, "cannot get deploy")
			return reconcile.Result{}, err
		}
	}
	if currDeploy.Annotations != nil {
		newDeploy.Annotations = currDeploy.Annotations
	}
	if currDeploy.Spec.Template.Annotations != nil {
		newDeploy.Spec.Template.Annotations = currDeploy.Spec.Template.Annotations
	}

	err = rclient.Update(context.TODO(), newDeploy)
	if err != nil {
		l.Error(err, "cannot update deploy")
		return reconcile.Result{}, err
	}
	l.Info("reconciled vmalert deploy")

	return reconcile.Result{}, nil
}

// newDeployForCR returns a busybox pod with the same name/namespace as the cr
func newDeployForVmAlert(cr *victoriametricsv1beta1.VmAlert, c *conf.BaseOperatorConf, ruleConfigMapNames []string) (*appsv1.Deployment, error) {

	cr = cr.DeepCopy()
	//todo move inject default into separate func
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

	if cr.Spec.ConfigSecret == "" {
		cr.Spec.ConfigSecret = cr.Name
	}
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmAlertDefault.Port
	}
	annotations := make(map[string]string)
	for key, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(key, "kubectl.kubernetes.io/") {
			annotations[key] = value
		}
	}
	labels := getVmAlertLabels(cr)
	for key, value := range cr.ObjectMeta.Labels {
		labels[key] = value
	}

	generatedSpec, err := vmAlertSpecGen(cr, c, ruleConfigMapNames)
	if err != nil {
		return nil, err
	}

	if cr.Spec.ImagePullSecrets != nil && len(cr.Spec.ImagePullSecrets) > 0 {
		generatedSpec.Template.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        prefixedAlertName(cr.Name),
			Namespace:   cr.Namespace,
			Labels:      c.Labels.Merge(labels),
			Annotations: annotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         cr.APIVersion,
					Kind:               cr.Kind,
					Name:               cr.Name,
					UID:                cr.UID,
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
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
		fmt.Sprintf("-datasource.url=%s", cr.Spec.DataSource.URL),
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
	args = append(args, cr.Spec.ExtraArgs...)

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

	amVolumeMounts := []corev1.VolumeMount{}
	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8sutil.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		amVolumeMounts = append(amVolumeMounts, corev1.VolumeMount{
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
		amVolumeMounts = append(amVolumeMounts, corev1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(configmapsDir, c),
		})
	}

	amVolumeMounts = append(amVolumeMounts, cr.Spec.VolumeMounts...)

	for _, name := range ruleConfigMapNames {
		amVolumeMounts = append(amVolumeMounts, corev1.VolumeMount{
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
			VolumeMounts:             amVolumeMounts,
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
	podAnnotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		if cr.Spec.PodMetadata.Annotations != nil {
			for k, v := range cr.Spec.PodMetadata.Annotations {
				podAnnotations[k] = v
			}
		}
	}

	containers, err := MergePatchContainers(defaultContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}
	spec := &appsv1.DeploymentSpec{
		Replicas: cr.Spec.Replicas,

		Selector: &metav1.LabelSelector{
			MatchLabels: selectorLabelsVmAlert(cr),
		},

		Strategy: appsv1.DeploymentStrategy{
			Type: appsv1.RollingUpdateDeploymentStrategyType,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      getVmAlertLabels(cr),
				Annotations: podAnnotations,
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

func prefixedAlertName(name string) string {
	return fmt.Sprintf("vmalert-%s", name)
}

func getVmAlertLabels(cr *victoriametricsv1beta1.VmAlert) map[string]string {
	labels := selectorLabelsVmAlert(cr)
	for key, value := range cr.ObjectMeta.Labels {
		labels[key] = value
	}
	if cr.Spec.PodMetadata != nil {
		for key, value := range cr.Spec.PodMetadata.Labels {
			labels[key] = value
		}
	}
	return labels
}
func selectorLabelsVmAlert(cr *victoriametricsv1beta1.VmAlert) map[string]string {
	labels := map[string]string{}
	labels["app.kubernetes.io/name"] = "vmalert"
	labels["app.kubernetes.io/instance"] = cr.Name
	labels["app.kubernetes.io/component"] = "monitoring"

	return labels
}
