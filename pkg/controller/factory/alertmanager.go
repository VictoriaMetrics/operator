package factory

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	monitoringv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1beta1"
	"github.com/blang/semver"
	"github.com/coreos/prometheus-operator/pkg/k8sutil"
	"github.com/go-logr/logr"
	gerr "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/utils/pointer"
	"net/url"
	"path"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"strings"
)

const (
	governingServiceName   = "alertmanager-operated"
	defaultRetention       = "120h"
	secretsDir             = "/etc/alertmanager/secrets/"
	configmapsDir          = "/etc/alertmanager/configmaps/"
	alertmanagerConfDir    = "/etc/alertmanager/config"
	alertmanagerConfFile   = alertmanagerConfDir + "/alertmanager.yaml"
	alertmanagerStorageDir = "/alertmanager"
	defaultPortName        = "web"
)

var (
	minReplicas         int32 = 1
	probeTimeoutSeconds int32 = 5
)

func CreateOrUpdateAlertManager(cr *monitoringv1beta1.Alertmanager, rclient client.Client, c *conf.BaseOperatorConf, l logr.Logger) (*appsv1.StatefulSet, error) {
	l = l.WithValues("reconcile.AlertManager.sts", cr.Name)
	newSts, err := newStsForAlertManager(cr, c)
	if err != nil {
		l.Error(err, "cannot generate sts")
		return nil, err
	}
	// Set Alertmanager instance as the owner and controller
	// Check if this sts already exists
	oldSts := &appsv1.StatefulSet{}
	err = rclient.Get(context.TODO(), types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, oldSts)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("Creating a new sts", "sts.Namespace", newSts.Namespace, "sts.Name", newSts.Name)
			err = rclient.Create(context.TODO(), newSts)
			if err != nil {
				l.Error(err, "cannot create new sts")
				return nil, err
			}
			l.Info("new sts was created for alertmanager")
		} else {
			l.Error(err, "cannot get sts")
			return nil, err
		}
	}
	if oldSts.Annotations != nil {
		newSts.Annotations = oldSts.Annotations
	}
	l.Info("updating sts with new version")
	err = rclient.Update(context.TODO(), newSts)
	if err != nil {
		l.Error(err, "cannot update alertmanager sts")
		return nil, err
	}
	return newSts, nil
}

func newStsForAlertManager(cr *monitoringv1beta1.Alertmanager, c *conf.BaseOperatorConf) (*appsv1.StatefulSet, error) {

	if cr.Spec.BaseImage == "" {
		cr.Spec.BaseImage = c.AlertManager.AlertmanagerDefaultBaseImage
	}
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
	}
	if cr.Spec.Version == "" {
		cr.Spec.Version = c.AlertManager.AlertManagerVersion
	}
	if cr.Spec.Replicas == nil {
		cr.Spec.Replicas = &minReplicas
	}
	intZero := int32(0)
	if cr.Spec.Replicas != nil && *cr.Spec.Replicas < 0 {
		cr.Spec.Replicas = &intZero
	}
	if cr.Spec.Retention == "" {
		cr.Spec.Retention = defaultRetention
	}
	if cr.Spec.Resources.Requests == nil {
		cr.Spec.Resources.Requests = v1.ResourceList{}
	}
	if _, ok := cr.Spec.Resources.Requests[v1.ResourceMemory]; !ok {
		cr.Spec.Resources.Requests[v1.ResourceMemory] = resource.MustParse("200Mi")
	}
	if cr.Spec.ConfigSecret == "" {
		cr.Spec.ConfigSecret = configSecretName(cr.Name)
	}

	spec, err := makeStatefulSetSpec(cr, c)
	if err != nil {
		return nil, err
	}

	// do not transfer kubectl annotations to the statefulset so it is not
	// pruned by kubectl
	annotations := make(map[string]string)
	for key, value := range cr.ObjectMeta.Annotations {
		if !strings.HasPrefix(key, "kubectl.kubernetes.io/") {
			annotations[key] = value
		}
	}
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        prefixedName(cr.Name),
			Labels:      c.Labels.Merge(cr.ObjectMeta.Labels),
			Annotations: annotations,
			Namespace:   cr.Namespace,
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
		Spec: *spec,
	}

	if cr.Spec.ImagePullSecrets != nil && len(cr.Spec.ImagePullSecrets) > 0 {
		statefulset.Spec.Template.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets
	}

	storageSpec := cr.Spec.Storage
	if storageSpec == nil {
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, v1.Volume{
			Name: volumeName(cr.Name),
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		})
	} else if storageSpec.EmptyDir != nil {
		emptyDir := storageSpec.EmptyDir
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, v1.Volume{
			Name: volumeName(cr.Name),
			VolumeSource: v1.VolumeSource{
				EmptyDir: emptyDir,
			},
		})
	} else {
		pvcTemplate := MakeVolumeClaimTemplate(storageSpec.VolumeClaimTemplate)
		if pvcTemplate.Name == "" {
			pvcTemplate.Name = volumeName(cr.Name)
		}
		if storageSpec.VolumeClaimTemplate.Spec.AccessModes == nil {
			pvcTemplate.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}
		} else {
			pvcTemplate.Spec.AccessModes = storageSpec.VolumeClaimTemplate.Spec.AccessModes
		}
		pvcTemplate.Spec.Resources = storageSpec.VolumeClaimTemplate.Spec.Resources
		pvcTemplate.Spec.Selector = storageSpec.VolumeClaimTemplate.Spec.Selector
		statefulset.Spec.VolumeClaimTemplates = append(statefulset.Spec.VolumeClaimTemplates, *pvcTemplate)
	}

	statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, cr.Spec.Volumes...)

	return statefulset, nil
}

func CreateOrUpdateAlertManagerService(cr *monitoringv1beta1.Alertmanager, rclient client.Client, c *conf.BaseOperatorConf, l logr.Logger) (*v1.Service, error) {

	l = l.WithValues("recon.alertmanager.service", cr.Name)

	newSvc := newAlertManagerService(cr, c)
	oldSvc := &v1.Service{}
	err := rclient.Get(context.TODO(), types.NamespacedName{Name: newSvc.Name, Namespace: newSvc.Namespace}, oldSvc)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for sts")
			err := rclient.Create(context.TODO(), newSvc)
			if err != nil {
				l.Error(err, "cannot create service for alertmanager")
			}
		} else {
			l.Error(err, "cannot get service for sts")
			return nil, err
		}
	}
	if oldSvc.Annotations != nil {
		newSvc.Annotations = oldSvc.Annotations
	}
	if oldSvc.Spec.ClusterIP != "" {
		newSvc.Spec.ClusterIP = oldSvc.Spec.ClusterIP
	}
	if oldSvc.ResourceVersion != "" {
		newSvc.ResourceVersion = oldSvc.ResourceVersion
	}
	err = rclient.Update(context.TODO(), newSvc)
	if err != nil {
		l.Error(err, "cannot update service")
		return nil, err
	}

	return newSvc, nil

}

func newAlertManagerService(cr *monitoringv1beta1.Alertmanager, c *conf.BaseOperatorConf) *v1.Service {

	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      governingServiceName,
			Namespace: cr.Namespace,
			Labels: c.Labels.Merge(map[string]string{
				"operated-alertmanager": "true",
			}),
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
		Spec: v1.ServiceSpec{
			ClusterIP: "None",
			Ports: []v1.ServicePort{
				{
					Name:       cr.Spec.PortName,
					Port:       9093,
					TargetPort: intstr.FromString(cr.Spec.PortName),
					Protocol:   v1.ProtocolTCP,
				},
				{
					Name:       "tcp-mesh",
					Port:       9094,
					TargetPort: intstr.FromInt(9094),
					Protocol:   v1.ProtocolTCP,
				},
				{
					Name:       "udp-mesh",
					Port:       9094,
					TargetPort: intstr.FromInt(9094),
					Protocol:   v1.ProtocolUDP,
				},
			},
			Selector: map[string]string{
				"app": "alertmanager",
			},
		},
	}
	return svc
}

func makeStatefulSetSpec(a *monitoringv1beta1.Alertmanager, config *conf.BaseOperatorConf) (*appsv1.StatefulSetSpec, error) {
	// Before editing 'a' create deep copy, to prevent side effects. For more
	// details see https://github.com/coreos/prometheus-operator/issues/1659
	a = a.DeepCopy()

	// Version is used by default.
	// If the tag is specified, we use the tag to identify the container image.
	// If the sha is specified, we use the sha to identify the container image,
	// as it has even stronger immutable guarantees to identify the image.
	image := fmt.Sprintf("%s:%s", a.Spec.BaseImage, a.Spec.Version)
	if a.Spec.Tag != "" {
		image = fmt.Sprintf("%s:%s", a.Spec.BaseImage, a.Spec.Tag)
	}
	if a.Spec.SHA != "" {
		image = fmt.Sprintf("%s@sha256:%s", a.Spec.BaseImage, a.Spec.SHA)
	}
	if a.Spec.Image != nil && *a.Spec.Image != "" {
		image = *a.Spec.Image
	}

	version, err := semver.ParseTolerant(a.Spec.Version)
	if err != nil {
		return nil, err
	}

	amArgs := []string{
		fmt.Sprintf("--config.file=%s", alertmanagerConfFile),
		fmt.Sprintf("--cluster.listen-address=[$(POD_IP)]:%d", 9094),
		fmt.Sprintf("--storage.path=%s", alertmanagerStorageDir),
		fmt.Sprintf("--data.retention=%s", a.Spec.Retention),
	}

	if a.Spec.ListenLocal {
		amArgs = append(amArgs, "--web.listen-address=127.0.0.1:9093")
	} else {
		amArgs = append(amArgs, "--web.listen-address=:9093")
	}

	if a.Spec.ExternalURL != "" {
		amArgs = append(amArgs, "--web.external-url="+a.Spec.ExternalURL)
	}

	webRoutePrefix := "/"
	if a.Spec.RoutePrefix != "" {
		webRoutePrefix = a.Spec.RoutePrefix
	}
	amArgs = append(amArgs, fmt.Sprintf("--web.route-prefix=%v", webRoutePrefix))

	if a.Spec.LogLevel != "" && a.Spec.LogLevel != "info" {
		amArgs = append(amArgs, fmt.Sprintf("--log.level=%s", a.Spec.LogLevel))
	}

	if version.GTE(semver.MustParse("0.16.0")) {
		if a.Spec.LogFormat != "" && a.Spec.LogFormat != "logfmt" {
			amArgs = append(amArgs, fmt.Sprintf("--log.format=%s", a.Spec.LogFormat))
		}
	}

	if a.Spec.ClusterAdvertiseAddress != "" {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.advertise-address=%s", a.Spec.ClusterAdvertiseAddress))
	}

	localReloadURL := &url.URL{
		Scheme: "http",
		Host:   config.AlertManager.LocalHost + ":9093",
		Path:   path.Clean(webRoutePrefix + "/-/reload"),
	}

	livenessProbeHandler := v1.Handler{
		HTTPGet: &v1.HTTPGetAction{
			Path: path.Clean(webRoutePrefix + "/-/healthy"),
			Port: intstr.FromString(a.Spec.PortName),
		},
	}

	readinessProbeHandler := v1.Handler{
		HTTPGet: &v1.HTTPGetAction{
			Path: path.Clean(webRoutePrefix + "/-/ready"),
			Port: intstr.FromString(a.Spec.PortName),
		},
	}

	var livenessProbe *v1.Probe
	var readinessProbe *v1.Probe
	if !a.Spec.ListenLocal {
		livenessProbe = &v1.Probe{
			Handler:          livenessProbeHandler,
			TimeoutSeconds:   probeTimeoutSeconds,
			FailureThreshold: 10,
		}

		readinessProbe = &v1.Probe{
			Handler:             readinessProbeHandler,
			InitialDelaySeconds: 3,
			TimeoutSeconds:      3,
			PeriodSeconds:       5,
			FailureThreshold:    10,
		}
	}

	podAnnotations := map[string]string{}
	podLabels := map[string]string{}
	if a.Spec.PodMetadata != nil {
		if a.Spec.PodMetadata.Labels != nil {
			for k, v := range a.Spec.PodMetadata.Labels {
				podLabels[k] = v
			}
		}
		if a.Spec.PodMetadata.Annotations != nil {
			for k, v := range a.Spec.PodMetadata.Annotations {
				podAnnotations[k] = v
			}
		}
	}
	podLabels["app"] = "alertmanager"
	podLabels["alertmanager"] = a.Name

	var clusterPeerDomain string
	if config.AlertManager.ClusterDomain != "" {
		clusterPeerDomain = fmt.Sprintf("%s.%s.svc.%s.", governingServiceName, a.Namespace, config.AlertManager.ClusterDomain)
	} else {
		// The default DNS search path is .svc.<cluster domain>
		clusterPeerDomain = governingServiceName
	}
	for i := int32(0); i < *a.Spec.Replicas; i++ {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.peer=%s-%d.%s:9094", prefixedName(a.Name), i, clusterPeerDomain))
	}

	for _, peer := range a.Spec.AdditionalPeers {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.peer=%s", peer))
	}

	ports := []v1.ContainerPort{
		{
			Name:          "mesh-tcp",
			ContainerPort: 9094,
			Protocol:      v1.ProtocolTCP,
		},
		{
			Name:          "mesh-udp",
			ContainerPort: 9094,
			Protocol:      v1.ProtocolUDP,
		},
	}
	if !a.Spec.ListenLocal {
		ports = append([]v1.ContainerPort{
			{
				Name:          a.Spec.PortName,
				ContainerPort: 9093,
				Protocol:      v1.ProtocolTCP,
			},
		}, ports...)
	}

	// Adjust Alertmanager command line args to specified AM version
	//
	// Alertmanager versions < v0.15.0 are only supported on a best effort basis
	// starting with Prometheus Operator v0.30.0.
	switch version.Major {
	case 0:
		if version.Minor < 15 {
			for i := range amArgs {
				// below Alertmanager v0.15.0 peer address port specification is not necessary
				if strings.Contains(amArgs[i], "--cluster.peer") {
					amArgs[i] = strings.TrimSuffix(amArgs[i], ":9094")
				}

				// below Alertmanager v0.15.0 high availability flags are prefixed with 'mesh' instead of 'cluster'
				amArgs[i] = strings.Replace(amArgs[i], "--cluster.", "--mesh.", 1)
			}
		}
		if version.Minor < 13 {
			for i := range amArgs {
				// below Alertmanager v0.13.0 all flags are with single dash.
				amArgs[i] = strings.Replace(amArgs[i], "--", "-", 1)
			}
		}
		if version.Minor < 7 {
			// below Alertmanager v0.7.0 the flag 'web.route-prefix' does not exist
			amArgs = filter(amArgs, func(s string) bool {
				return !strings.Contains(s, "web.route-prefix")
			})
		}
	default:
		return nil, gerr.Errorf("unsupported Alertmanager major version %s", version)
	}

	volumes := []v1.Volume{
		{
			Name: "config-volume",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: a.Spec.ConfigSecret,
				},
			},
		},
	}

	volName := volumeName(a.Name)
	if a.Spec.Storage != nil {
		if a.Spec.Storage.VolumeClaimTemplate.Name != "" {
			volName = a.Spec.Storage.VolumeClaimTemplate.Name
		}
	}

	amVolumeMounts := []v1.VolumeMount{
		{
			Name:      "config-volume",
			MountPath: alertmanagerConfDir,
		},
		{
			Name:      volName,
			MountPath: alertmanagerStorageDir,
			SubPath:   subPathForStorage(a.Spec.Storage),
		},
	}

	for _, s := range a.Spec.Secrets {
		volumes = append(volumes, v1.Volume{
			Name: k8sutil.SanitizeVolumeName("secret-" + s),
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		amVolumeMounts = append(amVolumeMounts, v1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: secretsDir + s,
		})
	}

	for _, c := range a.Spec.ConfigMaps {
		volumes = append(volumes, v1.Volume{
			Name: k8sutil.SanitizeVolumeName("configmap-" + c),
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		amVolumeMounts = append(amVolumeMounts, v1.VolumeMount{
			Name:      k8sutil.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: configmapsDir + c,
		})
	}

	amVolumeMounts = append(amVolumeMounts, a.Spec.VolumeMounts...)

	resources := v1.ResourceRequirements{Limits: v1.ResourceList{}}
	if config.AlertManager.ConfigReloaderCPU != "0" {
		resources.Limits[v1.ResourceCPU] = resource.MustParse(config.AlertManager.ConfigReloaderCPU)
	}
	if config.AlertManager.ConfigReloaderMemory != "0" {
		resources.Limits[v1.ResourceMemory] = resource.MustParse(config.AlertManager.ConfigReloaderMemory)
	}

	terminationGracePeriod := int64(120)
	finalLabels := config.Labels.Merge(podLabels)

	defaultContainers := []v1.Container{
		{
			Args:           amArgs,
			Name:           "alertmanager",
			Image:          image,
			Ports:          ports,
			VolumeMounts:   amVolumeMounts,
			LivenessProbe:  livenessProbe,
			ReadinessProbe: readinessProbe,
			Resources:      a.Spec.Resources,
			Env: []v1.EnvVar{
				{
					// Necessary for '--cluster.listen-address' flag
					Name: "POD_IP",
					ValueFrom: &v1.EnvVarSource{
						FieldRef: &v1.ObjectFieldSelector{
							FieldPath: "status.podIP",
						},
					},
				},
			},
			TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		}, {
			Name:  "config-reloader",
			Image: config.AlertManager.ConfigReloaderImage,
			Args: []string{
				fmt.Sprintf("-webhook-url=%s", localReloadURL),
				fmt.Sprintf("-volume-dir=%s", alertmanagerConfDir),
			},
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "config-volume",
					ReadOnly:  true,
					MountPath: alertmanagerConfDir,
				},
			},
			Resources:                resources,
			TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
		},
	}

	containers, err := MergePatchContainers(defaultContainers, a.Spec.Containers)
	if err != nil {
		return nil, gerr.Wrap(err, "failed to merge containers spec")
	}

	// PodManagementPolicy is set to Parallel to mitigate issues in kubernetes: https://github.com/kubernetes/kubernetes/issues/60164
	// This is also mentioned as one of limitations of StatefulSets: https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#limitations
	return &appsv1.StatefulSetSpec{
		ServiceName:         governingServiceName,
		Replicas:            a.Spec.Replicas,
		PodManagementPolicy: appsv1.ParallelPodManagement,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.RollingUpdateStatefulSetStrategyType,
		},
		Selector: &metav1.LabelSelector{
			MatchLabels: podLabels,
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      finalLabels,
				Annotations: podAnnotations,
			},
			Spec: v1.PodSpec{
				NodeSelector:                  a.Spec.NodeSelector,
				PriorityClassName:             a.Spec.PriorityClassName,
				TerminationGracePeriodSeconds: &terminationGracePeriod,
				InitContainers:                a.Spec.InitContainers,
				Containers:                    containers,
				Volumes:                       volumes,
				ServiceAccountName:            a.Spec.ServiceAccountName,
				SecurityContext:               a.Spec.SecurityContext,
				Tolerations:                   a.Spec.Tolerations,
				Affinity:                      a.Spec.Affinity,
			},
		},
	}, nil
}

func MakeVolumeClaimTemplate(e monitoringv1.EmbeddedPersistentVolumeClaim) *v1.PersistentVolumeClaim {
	pvc := v1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: e.APIVersion,
			Kind:       e.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              e.Name,
			Labels:            e.Labels,
			Annotations:       e.Annotations,
			CreationTimestamp: metav1.Time{},
		},
		Spec:   e.Spec,
		Status: e.Status,
	}
	return &pvc
}

func subPathForStorage(s *monitoringv1.StorageSpec) string {
	if s == nil {
		return ""
	}

	return "alertmanager-db"
}

func filter(strings []string, f func(string) bool) []string {
	filteredStrings := make([]string, 0)
	for _, s := range strings {
		if f(s) {
			filteredStrings = append(filteredStrings, s)
		}
	}
	return filteredStrings
}

// MergePatchContainers adds patches to base using a strategic merge patch and iterating by container name, failing on the first error
func MergePatchContainers(base, patches []v1.Container) ([]v1.Container, error) {
	var out []v1.Container

	// map of containers that still need to be patched by name
	containersToPatch := make(map[string]v1.Container)
	for _, c := range patches {
		containersToPatch[c.Name] = c
	}

	for _, container := range base {
		// If we have a patch result, iterate over each container and try and calculate the patch
		if patchContainer, ok := containersToPatch[container.Name]; ok {
			// Get the json for the container and the patch
			containerBytes, err := json.Marshal(container)
			if err != nil {
				return nil, gerr.Wrap(err, fmt.Sprintf("failed to marshal json for container %s", container.Name))
			}
			patchBytes, err := json.Marshal(patchContainer)
			if err != nil {
				return nil, gerr.Wrap(err, fmt.Sprintf("failed to marshal json for patch container %s", container.Name))
			}

			// Calculate the patch result
			jsonResult, err := strategicpatch.StrategicMergePatch(containerBytes, patchBytes, v1.Container{})
			if err != nil {
				return nil, gerr.Wrap(err, fmt.Sprintf("failed to generate merge patch for %s", container.Name))
			}
			var patchResult v1.Container
			if err := json.Unmarshal(jsonResult, &patchResult); err != nil {
				return nil, gerr.Wrap(err, fmt.Sprintf("failed to unmarshal merged container %s", container.Name))
			}

			// Add the patch result and remove the corresponding key from the to do list
			out = append(out, patchResult)
			delete(containersToPatch, container.Name)
		} else {
			// This container didn't need to be patched
			out = append(out, container)
		}
	}

	// Iterate over the patches and add all the containers that were not previously part of a patch result
	for _, container := range patches {
		if _, ok := containersToPatch[container.Name]; ok {
			out = append(out, container)
		}
	}

	return out, nil
}
