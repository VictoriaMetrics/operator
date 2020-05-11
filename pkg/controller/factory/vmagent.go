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
	vmAgentConfigsDir  = "/etc/vmagent/configs"
	vmAgentSecretDir   = "/etc/vmagent/secret"
	vmAgentConfDir     = "/etc/vmagent/config"
	vmAgentConOfOutDir = "/etc/vmagent/config_out"
)

func CreateOrUpdateVmAgentService(cr *victoriametricsv1beta1.VmAgent, rclient client.Client, c *conf.BaseOperatorConf, l logr.Logger) (*corev1.Service, error) {
	l = l.WithValues("recon.vm.service.name", cr.Name)
	newSvc := newServiceVmAgent(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(context.TODO(), types.NamespacedName{Namespace: cr.Namespace, Name: newSvc.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vm agent")
			err := rclient.Create(context.TODO(), newSvc)
			if err != nil {
				l.Error(err, "cannot create new service for vmagent")
				return nil, err
			}
		} else {
			l.Error(err, "cannot get vmagent service for recon")
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
		l.Error(err, "cannot update vmagent service")
		return nil, err
	}
	l.Info("vmagent svc reconciled")
	return newSvc, nil
}

func newServiceVmAgent(cr *victoriametricsv1beta1.VmAgent, c *conf.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmAgentDefault.Port
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        prefixedAgentName(cr.Name),
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
			Selector: selectorLabelsVmAgent(cr),
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

//we assume, that configmaps were created before this function was called
func CreateOrUpdateVmAgent(cr *victoriametricsv1beta1.VmAgent, rclient client.Client, c *conf.BaseOperatorConf, l logr.Logger) (reconcile.Result, error) {

	l.Info("create or update vm agent deploy")
	//recon deploy
	//well, there can be collisions for deployment name
	//if we have vmagent: ex1
	//vmalert: ex1
	//so prefix must be set
	newDeploy, err := newDeployForVmAgent(cr, c)
	if err != nil {
		l.Error(err, "cannot build new deploy for vmagent")
		return reconcile.Result{}, err
	}

	l = l.WithValues("vmagent.deploy.name", newDeploy.Name, "vmagent.deploy.namespace", newDeploy.Namespace)

	currentDeploy := &appsv1.Deployment{}
	err = rclient.Get(context.TODO(), types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, currentDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			//create new
			l.Info("vmagent deploy not found, creating new one")
			err := rclient.Create(context.TODO(), newDeploy)
			if err != nil {
				l.Error(err, "cannot create new vmagent deploy")
				return reconcile.Result{}, err
			}
			l.Info("new vmagent deploy was created")
		} else {
			l.Error(err, "cannot get vmagent deploy")
			return reconcile.Result{}, err
		}
	}
	l.Info("vm agent was found, updating it")
	if currentDeploy.Annotations != nil {
		newDeploy.Annotations = currentDeploy.Annotations
	}
	if currentDeploy.Spec.Template.Annotations != nil {
		newDeploy.Spec.Template.Annotations = currentDeploy.Spec.Template.Annotations
	}

	err = rclient.Update(context.TODO(), newDeploy)
	if err != nil {
		l.Error(err, "cannot try to update vmagent deploy")
	}
	l.Info("vmagent deploy reconciled")

	return reconcile.Result{}, nil
}

// newDeployForCR returns a busybox pod with the same name/namespace as the cr
func newDeployForVmAgent(cr *victoriametricsv1beta1.VmAgent, c *conf.BaseOperatorConf) (*appsv1.Deployment, error) {
	cr = cr.DeepCopy()

	//inject default
	if cr.Spec.Image == nil {
		cr.Spec.Image = &c.VmAgentDefault.Image
	}
	if cr.Spec.Version == "" {
		cr.Spec.Version = c.VmAgentDefault.Version
	}
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmAgentDefault.Port
	}
	//todo default values
	if cr.Spec.Resources.Requests == nil {
		cr.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.Resources.Limits == nil {
		cr.Spec.Resources.Limits = corev1.ResourceList{}
	}

	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceMemory]; !ok {
		cr.Spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VmAgentDefault.Resource.Limit.Mem)
	}
	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceCPU]; !ok {
		cr.Spec.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VmAgentDefault.Resource.Limit.Cpu)
	}

	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceMemory]; !ok {
		cr.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VmAgentDefault.Resource.Request.Mem)
	}
	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceCPU]; !ok {
		cr.Spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VmAgentDefault.Resource.Request.Cpu)
	}

	annotations := make(map[string]string)
	for key, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(key, "kubectl.kubernetes.io/") {
			annotations[key] = value
		}
	}

	podSpec, err := makeSpecForVmAgent(cr, c)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        prefixedAgentName(cr.Name),
			Namespace:   cr.Namespace,
			Labels:      c.Labels.Merge(cr.ObjectMeta.Labels),
			Annotations: cr.ObjectMeta.Annotations,
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
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.Replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabelsVmAgent(cr)},
			//TODO change it
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				//TODO add defaults
				RollingUpdate: &appsv1.RollingUpdateDeployment{},
			},
			Template: *podSpec,
		},
	}
	return depSpec, nil
}

func makeSpecForVmAgent(cr *victoriametricsv1beta1.VmAgent, c *conf.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-promscrape.config=%s", path.Join(vmAgentConOfOutDir, configEnvsubstFilename)),
	}
	for _, remote := range cr.Spec.RemoteWrite {
		args = append(args, "-remoteWrite.url="+remote.URL)
	}

	args = append(args, cr.Spec.ExtraArgs...)

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: configSecretName(cr.Name),
				},
			},
		},
		//TODO do we need it ?
		//{
		//	Name: "tls-assets",
		//	VolumeSource: corev1.VolumeSource{
		//		Secret: &corev1.SecretVolumeSource{
		//			SecretName: tlsAssetsSecretName(cr.Name),
		//		},
		//	},
		//},
		{
			Name: "config-out",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	agentVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "config-out",
			ReadOnly:  true,
			MountPath: vmAgentConOfOutDir,
		},
		//TODO need it ?
		//{
		//	Name:      "tls-assets",
		//	ReadOnly:  true,
		//	MountPath: tlsAssetsDir,
		//},
	}

	agentVolumeMounts = append(agentVolumeMounts, cr.Spec.VolumeMounts...)

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8sutil.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(vmAgentSecretDir, s),
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
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmAgentConfigsDir, c),
		})
	}

	configReloadVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: vmAgentConfDir,
		},
		{
			Name:      "config-out",
			MountPath: vmAgentConOfOutDir,
		},
	}

	configReloadArgs := []string{
		fmt.Sprintf("--log-format=%s", c.LogFormat),
		fmt.Sprintf("--reload-url=http://localhost:%s/-/reload", cr.Spec.Port),
		fmt.Sprintf("--config-file=%s", path.Join(vmAgentConfDir, configFilename)),
		fmt.Sprintf("--config-envsubst-file=%s", path.Join(vmAgentConOfOutDir, configEnvsubstFilename)),
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

	podAnnotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		if cr.Spec.PodMetadata.Annotations != nil {
			for k, v := range cr.Spec.PodMetadata.Annotations {
				podAnnotations[k] = v
			}
		}
	}

	var additionalContainers []corev1.Container

	prometheusConfigReloaderResources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{}, Requests: corev1.ResourceList{}}
	if c.VmAgentDefault.ConfigReloaderCPU != "0" {
		prometheusConfigReloaderResources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VmAgentDefault.ConfigReloaderCPU)
		prometheusConfigReloaderResources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VmAgentDefault.ConfigReloaderCPU)
	}
	if c.VmAgentDefault.ConfigReloaderMemory != "0" {
		prometheusConfigReloaderResources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VmAgentDefault.ConfigReloaderMemory)
		prometheusConfigReloaderResources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VmAgentDefault.ConfigReloaderMemory)
	}

	operatorContainers := append([]corev1.Container{
		{
			Name:                     "vmagent",
			Image:                    fmt.Sprintf("%s:%s", *cr.Spec.Image, cr.Spec.Version),
			Ports:                    ports,
			Args:                     args,
			Env:                      envs,
			VolumeMounts:             agentVolumeMounts,
			LivenessProbe:            livenessProbe,
			ReadinessProbe:           readinessProbe,
			Resources:                cr.Spec.Resources,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		}, {
			Name:                     "config-reloader",
			Image:                    c.VmAgentDefault.ConfigReloadImage,
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
			Resources:    prometheusConfigReloaderResources,
		},
	}, additionalContainers...)

	containers, err := k8sutil.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	vmAgentSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      getVmAgentLabels(cr),
			Annotations: podAnnotations,
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.Spec.ServiceAccountName,
			SecurityContext:    cr.Spec.SecurityContext,
			ImagePullSecrets:   cr.Spec.ImagePullSecrets,
			Affinity:           cr.Spec.Affinity,
			SchedulerName:      "",
			Tolerations:        cr.Spec.Tolerations,
			PriorityClassName:  cr.Spec.PriorityClassName,
		},
	}

	return vmAgentSpec, nil
}

func prefixedAgentName(name string) string {
	return fmt.Sprintf("vmagent-%s", name)
}
func getVmAgentLabels(cr *victoriametricsv1beta1.VmAgent) map[string]string {
	labels := selectorLabelsVmAgent(cr)
	if cr.Spec.PodMetadata != nil {
		if cr.Spec.PodMetadata.Labels != nil {
			for k, v := range cr.Spec.PodMetadata.Labels {
				labels[k] = v
			}
		}
	}

	return labels

}

func selectorLabelsVmAgent(cr *victoriametricsv1beta1.VmAgent) map[string]string {
	labels := map[string]string{}
	labels["app.kubernetes.io/name"] = "vmagent"
	labels["app.kubernetes.io/instance"] = cr.Name
	labels["app.kubernetes.io/component"] = "monitoring"

	return labels

}
