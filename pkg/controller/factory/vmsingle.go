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
	"strings"
)

const (
	vmSingleSecretDir    = "/etc/vm/secrets"
	vmSingleConfigMapDir = "/etc/vm/configs"
	vmSingleDataDir      = "/victoria-metrics-data"
)

func CreateVmStorage(cr *victoriametricsv1beta1.VmSingle, rclient client.Client, c *conf.BaseOperatorConf, l logr.Logger) (*corev1.PersistentVolumeClaim, error) {

	l = l.WithValues("vm.single.pvc.create", prefixedVmSingleName(cr.Name))
	l.Info("reconciling pvc")
	newPvc := makeVmPvc(cr, c)
	existPvc := &corev1.PersistentVolumeClaim{}
	err := rclient.Get(context.TODO(), types.NamespacedName{Namespace: cr.Namespace, Name: prefixedVmSingleName(cr.Name)}, existPvc)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new pvc")
			if err := rclient.Create(context.TODO(), newPvc); err != nil {
				l.Error(err, "cannot create new pvc")
				return nil, err
			}

			return newPvc, nil
		} else {
			l.Error(err, "cannot get existing pvc")
			return nil, err
		}
	}

	if existPvc.Spec.Resources.String() != newPvc.Spec.Resources.String() {
		l.Info("volume requests isnt same, update required")
		existPvc.Spec.Resources = newPvc.Spec.Resources
		err := rclient.Update(context.TODO(), existPvc)
		if err != nil {
			l.Error(err, "cannot update pvc size, we can suppress it")
		}
	}
	newPvc = existPvc

	return newPvc, nil
}

func makeVmPvc(cr *victoriametricsv1beta1.VmSingle, c *conf.BaseOperatorConf) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        prefixedVmSingleName(cr.Name),
			Namespace:   cr.Namespace,
			Labels:      c.Labels.Merge(cr.ObjectMeta.Labels),
			Annotations: cr.Annotations,
		},
		Spec: *cr.Spec.Storage,
	}
	if cr.Spec.RemovePvcAfterDelete {
		pvcObject.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         cr.APIVersion,
				Kind:               cr.Kind,
				Name:               cr.Name,
				UID:                cr.UID,
				Controller:         pointer.BoolPtr(true),
				BlockOwnerDeletion: pointer.BoolPtr(true),
			},
		}
	}
	return pvcObject
}

func CreateOrUpdateVmSingle(cr *victoriametricsv1beta1.VmSingle, rclient client.Client, c *conf.BaseOperatorConf, l logr.Logger) (*appsv1.Deployment, error) {

	l.Info("create or update vm single deploy")

	newDeploy, err := newDeployForVmSingle(cr, c)
	if err != nil {
		l.Error(err, "cannot build new deploy for single")
		return nil, err
	}

	l = l.WithValues("single.deploy.name", newDeploy.Name, "single.deploy.namespace", newDeploy.Namespace)

	currentDeploy := &appsv1.Deployment{}
	err = rclient.Get(context.TODO(), types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, currentDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			//create new
			l.Info("vmsingle deploy not found, creating new one")
			err := rclient.Create(context.TODO(), newDeploy)
			if err != nil {
				l.Error(err, "cannot create new vmsingle deploy")
				return nil, err
			}
			l.Info("new vmsingle deploy was created")
		} else {
			l.Error(err, "cannot get vmsingle deploy")
			return nil, err
		}
	}
	l.Info("vm vmsingle was found, updating it")
	if currentDeploy.Annotations != nil {
		newDeploy.Annotations = currentDeploy.Annotations
	}
	if currentDeploy.Spec.Template.Annotations != nil {
		newDeploy.Spec.Template.Annotations = currentDeploy.Spec.Template.Annotations
	}

	err = rclient.Update(context.TODO(), newDeploy)
	if err != nil {
		l.Error(err, "cannot update single deploy")
	}
	l.Info("single deploy reconciled")

	return nil, nil
}

func newDeployForVmSingle(cr *victoriametricsv1beta1.VmSingle, c *conf.BaseOperatorConf) (*appsv1.Deployment, error) {
	cr = cr.DeepCopy()

	if cr.Spec.Image == nil {
		cr.Spec.Image = &c.VmSingleDefault.Image
	}
	if cr.Spec.Version == "" {
		cr.Spec.Version = c.VmSingleDefault.Version
	}
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmSingleDefault.Port
	}

	if cr.Spec.Resources.Requests == nil {
		cr.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.Resources.Limits == nil {
		cr.Spec.Resources.Limits = corev1.ResourceList{}
	}
	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceMemory]; !ok {
		cr.Spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VmSingleDefault.Resource.Limit.Mem)
	}
	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceCPU]; !ok {
		cr.Spec.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VmSingleDefault.Resource.Limit.Cpu)
	}

	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceMemory]; !ok {
		cr.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VmSingleDefault.Resource.Request.Mem)
	}
	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceCPU]; !ok {
		cr.Spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VmSingleDefault.Resource.Request.Cpu)
	}

	annotations := make(map[string]string)
	for key, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(key, "kubectl.kubernetes.io/") {
			annotations[key] = value
		}
	}

	podSpec, err := makeSpecForVmSingle(cr, c)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        prefixedVmSingleName(cr.Name),
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
			//its hardcoded by design
			//but we can add valition
			Replicas: pointer.Int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabelsVmSingle(cr)},
			Strategy: appsv1.DeploymentStrategy{
				//we use recreate, coz of volume claim
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: *podSpec,
		},
	}

	return depSpec, nil
}

func makeSpecForVmSingle(cr *victoriametricsv1beta1.VmSingle, c *conf.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
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

	args = append(args, cr.Spec.ExtraArgs...)

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	volumes := []corev1.Volume{}

	storageSpec := cr.Spec.Storage
	if storageSpec == nil {
		volumes = append(volumes, corev1.Volume{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else {

		volumes = append(volumes, corev1.Volume{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: prefixedVmSingleName(cr.Name),
				},
			},
		})
	}
	volumes = append(volumes, cr.Spec.Volumes...)
	vmMounts := []corev1.VolumeMount{
		{
			Name:      "data",
			MountPath: vmSingleDataDir,
		},
	}

	vmMounts = append(vmMounts, cr.Spec.VolumeMounts...)

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8sutil.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(vmSingleSecretDir, s),
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
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmSingleConfigMapDir, c),
		})
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

	operatorContainers := append([]corev1.Container{
		{
			Name:                     "vmsignle",
			Image:                    fmt.Sprintf("%s:%s", *cr.Spec.Image, cr.Spec.Version),
			Ports:                    ports,
			Args:                     args,
			VolumeMounts:             vmMounts,
			LivenessProbe:            livenessProbe,
			ReadinessProbe:           readinessProbe,
			Resources:                cr.Spec.Resources,
			Env:                      envs,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		},
	}, additionalContainers...)

	containers, err := k8sutil.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	vmSignleSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      getVmSingleLabels(cr),
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

	return vmSignleSpec, nil

}

func CreateOrUpdateVmSingleService(cr *victoriametricsv1beta1.VmSingle, rclient client.Client, c *conf.BaseOperatorConf, l logr.Logger) (*corev1.Service, error) {
	newSvc := newServiceVmSingle(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(context.TODO(), types.NamespacedName{Namespace: cr.Namespace, Name: newSvc.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vm vmsingle")
			err := rclient.Create(context.TODO(), newSvc)
			if err != nil {
				l.Error(err, "cannot create new service for vmsingle")
				return nil, err
			}
		} else {
			l.Error(err, "cannot get vmsingle service for recon")
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
		l.Error(err, "cannot update vmsingle service")
		return nil, err
	}
	l.Info("vmsingle svc reconciled")
	return newSvc, nil
}

func newServiceVmSingle(cr *victoriametricsv1beta1.VmSingle, c *conf.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmSingleDefault.Port
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
			Selector: selectorLabelsVmSingle(cr),
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

func prefixedVmSingleName(name string) string {
	return fmt.Sprintf("vmsingle-%s", name)
}

func getVmSingleLabels(cr *victoriametricsv1beta1.VmSingle) map[string]string {
	labels := selectorLabelsVmSingle(cr)
	if cr.Spec.PodMetadata != nil {
		if cr.Spec.PodMetadata.Labels != nil {
			for k, v := range cr.Spec.PodMetadata.Labels {
				labels[k] = v
			}
		}
	}

	return labels

}
func selectorLabelsVmSingle(cr *victoriametricsv1beta1.VmSingle) map[string]string {
	labels := map[string]string{}
	labels["app.kubernetes.io/name"] = "vmsingle"
	labels["app.kubernetes.io/instance"] = cr.Name
	labels["app.kubernetes.io/component"] = "monitoring"
	return labels
}
