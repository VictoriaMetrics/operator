package factory

import (
	"context"
	"fmt"
	"net/url"
	"path"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/psp"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/blang/semver"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"strings"
)

const (
	defaultRetention       = "120h"
	alertmanagerConfDir    = "/etc/alertmanager/config"
	alertmanagerConfFile   = alertmanagerConfDir + "/alertmanager.yaml"
	alertmanagerStorageDir = "/alertmanager"
	defaultPortName        = "web"
	defaultAMConfig        = `
global:
  resolve_timeout: 5m
route:
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  receiver: 'webhook'
receivers:
- name: 'webhook'
  webhook_configs:
  - url: 'http://localhost:30500/'
`
)

var (
	minReplicas         int32 = 1
	probeTimeoutSeconds int32 = 5
	log                       = logf.Log.WithName("factory")
)

func CreateOrUpdateAlertManager(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
	l := log.WithValues("reconcile.VMAlertManager.sts", cr.Name, "ns", cr.Namespace)

	if err := psp.CreateServiceAccountForCRD(ctx, cr, rclient); err != nil {
		return nil, fmt.Errorf("failed create service account: %w", err)
	}
	if c.PSPAutoCreateEnabled {
		if err := psp.CreateOrUpdateServiceAccountWithPSP(ctx, cr, rclient); err != nil {
			l.Error(err, "cannot create podsecuritypolicy")
			return nil, fmt.Errorf("cannot create podsecurity policy for alertmanager, err=%w", err)
		}
	}
	newSts, err := newStsForAlertManager(cr, c)
	if err != nil {
		return nil, fmt.Errorf("cannot generate alertmanager sts, name: %s,err: %w", cr.Name, err)
	}
	// check secret with config
	if err := createDefaultAMConfig(ctx, cr, rclient); err != nil {
		return nil, fmt.Errorf("failed to check default Alertmanager config: %w", err)
	}
	currentSts := &appsv1.StatefulSet{}
	err = rclient.Get(ctx, types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, currentSts)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("Creating a new sts", "sts.Namespace", newSts.Namespace, "sts.Name", newSts.Name)
			err = rclient.Create(ctx, newSts)
			if err != nil {
				return nil, fmt.Errorf("cannot create new alertmanager sts: %w", err)
			}
			l.Info("new sts was created for alertmanager")
		} else {
			return nil, fmt.Errorf("cannot get alertmanager sts: %w", err)
		}
	}
	return newSts, updateStsForAlertManager(ctx, rclient, currentSts, newSts)
}

func updateStsForAlertManager(ctx context.Context, rclient client.Client, oldSts, newSts *appsv1.StatefulSet) error {
	for k, v := range oldSts.Annotations {
		newSts.Annotations[k] = v
	}
	for k, v := range oldSts.Spec.Template.Annotations {
		newSts.Spec.Template.Annotations[k] = v
	}
	log.Info("updating vmalertmanager sts")
	return rclient.Update(ctx, newSts)

}

func newStsForAlertManager(cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {

	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMAlertManager.AlertmanagerDefaultBaseImage
	}
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
	}
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMAlertManager.AlertManagerVersion
	}
	if cr.Spec.ReplicaCount == nil {
		cr.Spec.ReplicaCount = &minReplicas
	}
	intZero := int32(0)
	if cr.Spec.ReplicaCount != nil && *cr.Spec.ReplicaCount < 0 {
		cr.Spec.ReplicaCount = &intZero
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
		cr.Spec.ConfigSecret = cr.PrefixedName()
	}

	spec, err := makeStatefulSetSpec(cr, c)
	if err != nil {
		return nil, err
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Labels:          c.Labels.Merge(cr.Labels()),
			Annotations:     cr.Annotations(),
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
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

func CreateOrUpdateAlertManagerService(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client, c *config.BaseOperatorConf) (*v1.Service, error) {

	l := log.WithValues("recon.alertmanager.service", cr.Name)

	newService := newAlertManagerService(cr, c)
	oldService := &v1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Name: newService.Name, Namespace: newService.Namespace}, oldService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for sts")
			err := rclient.Create(ctx, newService)
			if err != nil {
				return nil, fmt.Errorf("cannot create service for vmalertmanager sts: %w", err)
			}
		} else {
			return nil, fmt.Errorf("cannot get service for vmalertmanager sts: %w", err)
		}
	}
	for annotation, value := range oldService.Annotations {
		newService.Annotations[annotation] = value
	}
	if oldService.Spec.ClusterIP != "" {
		newService.Spec.ClusterIP = oldService.Spec.ClusterIP
	}
	if oldService.ResourceVersion != "" {
		newService.ResourceVersion = oldService.ResourceVersion
	}
	err = rclient.Update(ctx, newService)
	if err != nil {
		return nil, fmt.Errorf("cannot update vmalert server: %w", err)
	}

	return newService, nil
}

func newAlertManagerService(cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf) *v1.Service {

	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.Labels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
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
			Selector: cr.SelectorLabels(),
		},
	}
	return svc
}

func makeStatefulSetSpec(cr *victoriametricsv1beta1.VMAlertmanager, config *config.BaseOperatorConf) (*appsv1.StatefulSetSpec, error) {

	cr = cr.DeepCopy()

	image := fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag)

	amArgs := []string{
		fmt.Sprintf("--config.file=%s", alertmanagerConfFile),
		fmt.Sprintf("--cluster.listen-address=[$(POD_IP)]:%d", 9094),
		fmt.Sprintf("--storage.path=%s", alertmanagerStorageDir),
		fmt.Sprintf("--data.retention=%s", cr.Spec.Retention),
	}

	if cr.Spec.ListenLocal {
		amArgs = append(amArgs, "--web.listen-address=127.0.0.1:9093")
	} else {
		amArgs = append(amArgs, "--web.listen-address=:9093")
	}

	if cr.Spec.ExternalURL != "" {
		amArgs = append(amArgs, "--web.external-url="+cr.Spec.ExternalURL)
	}

	webRoutePrefix := "/"
	if cr.Spec.RoutePrefix != "" {
		webRoutePrefix = cr.Spec.RoutePrefix
	}
	amArgs = append(amArgs, fmt.Sprintf("--web.route-prefix=%s", webRoutePrefix))

	if cr.Spec.LogLevel != "" && cr.Spec.LogLevel != "info" {
		amArgs = append(amArgs, fmt.Sprintf("--log.level=%s", cr.Spec.LogLevel))
	}

	if cr.Spec.LogFormat != "" {
		amArgs = append(amArgs, fmt.Sprintf("--log.format=%s", cr.Spec.LogFormat))
	}

	if cr.Spec.ClusterAdvertiseAddress != "" {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.advertise-address=%s", cr.Spec.ClusterAdvertiseAddress))
	}

	localReloadURL := &url.URL{
		Scheme: "http",
		Host:   config.VMAlertManager.LocalHost + ":9093",
		Path:   path.Clean(webRoutePrefix + "/-/reload"),
	}

	livenessProbeHandler := v1.Handler{
		HTTPGet: &v1.HTTPGetAction{
			Path: path.Clean(webRoutePrefix + "/-/healthy"),
			Port: intstr.FromString(cr.Spec.PortName),
		},
	}

	readinessProbeHandler := v1.Handler{
		HTTPGet: &v1.HTTPGetAction{
			Path: path.Clean(webRoutePrefix + "/-/ready"),
			Port: intstr.FromString(cr.Spec.PortName),
		},
	}

	var livenessProbe *v1.Probe
	var readinessProbe *v1.Probe
	if !cr.Spec.ListenLocal {
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

	var clusterPeerDomain string
	if config.ClusterDomainName != "" {
		clusterPeerDomain = fmt.Sprintf("%s.%s.svc.%s.", cr.PrefixedName(), cr.Namespace, config.ClusterDomainName)
	} else {
		// The default DNS search path is .svc.<cluster domain>
		clusterPeerDomain = cr.PrefixedName()
	}
	for i := int32(0); i < *cr.Spec.ReplicaCount; i++ {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.peer=%s-%d.%s:9094", prefixedName(cr.Name), i, clusterPeerDomain))
	}

	for _, peer := range cr.Spec.AdditionalPeers {
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
	if !cr.Spec.ListenLocal {
		ports = append([]v1.ContainerPort{
			{
				Name:          cr.Spec.PortName,
				ContainerPort: 9093,
				Protocol:      v1.ProtocolTCP,
			},
		}, ports...)
	}

	version, err := semver.ParseTolerant(cr.Spec.Image.Tag)
	if err != nil {
		log.Error(err, "cannot parse alert manager version")
	} else {
		// Adjust VMAlertmanager command line args to specified AM version
		switch version.Major {
		case 0:
			if version.Minor < 15 {
				for i := range amArgs {
					// below VMAlertmanager v0.15.0 peer address port specification is not necessary
					if strings.Contains(amArgs[i], "--cluster.peer") {
						amArgs[i] = strings.TrimSuffix(amArgs[i], ":9094")
					}

					// below VMAlertmanager v0.15.0 high availability flags are prefixed with 'mesh' instead of 'cluster'
					amArgs[i] = strings.Replace(amArgs[i], "--cluster.", "--mesh.", 1)
				}
			}
			if version.Minor < 13 {
				for i := range amArgs {
					// below VMAlertmanager v0.13.0 all flags are with single dash.
					amArgs[i] = strings.Replace(amArgs[i], "--", "-", 1)
				}
			}
			if version.Minor < 7 {
				// below VMAlertmanager v0.7.0 the flag 'web.route-prefix' does not exist
				amArgs = filter(amArgs, func(s string) bool {
					return !strings.Contains(s, "web.route-prefix")
				})
			}
		}
	}

	volumes := []v1.Volume{
		{
			Name: "config-volume",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: cr.Spec.ConfigSecret,
				},
			},
		},
	}

	volName := volumeName(cr.Name)
	if cr.Spec.Storage != nil {
		if cr.Spec.Storage.VolumeClaimTemplate.Name != "" {
			volName = cr.Spec.Storage.VolumeClaimTemplate.Name
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
			SubPath:   subPathForStorage(cr.Spec.Storage),
		},
	}

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, v1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		amVolumeMounts = append(amVolumeMounts, v1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.ConfigMaps {
		volumes = append(volumes, v1.Volume{
			Name: k8stools.SanitizeVolumeName("configmap-" + c),
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		amVolumeMounts = append(amVolumeMounts, v1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

	amVolumeMounts = append(amVolumeMounts, cr.Spec.VolumeMounts...)

	resources := v1.ResourceRequirements{Limits: v1.ResourceList{}}
	if config.VMAlertManager.ConfigReloaderCPU != "0" {
		resources.Limits[v1.ResourceCPU] = resource.MustParse(config.VMAlertManager.ConfigReloaderCPU)
	}
	if config.VMAlertManager.ConfigReloaderMemory != "0" {
		resources.Limits[v1.ResourceMemory] = resource.MustParse(config.VMAlertManager.ConfigReloaderMemory)
	}

	terminationGracePeriod := int64(120)

	defaultContainers := []v1.Container{
		{
			Args:            amArgs,
			Name:            "alertmanager",
			Image:           image,
			ImagePullPolicy: cr.Spec.Image.PullPolicy,
			Ports:           ports,
			VolumeMounts:    amVolumeMounts,
			LivenessProbe:   livenessProbe,
			ReadinessProbe:  readinessProbe,
			Resources:       cr.Spec.Resources,
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
			Image: config.VMAlertManager.ConfigReloaderImage,
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

	containers, err := k8stools.MergePatchContainers(defaultContainers, cr.Spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("failed to merge containers spec: %w", err)
	}

	return &appsv1.StatefulSetSpec{
		ServiceName:         cr.PrefixedName(),
		Replicas:            cr.Spec.ReplicaCount,
		PodManagementPolicy: appsv1.ParallelPodManagement,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.RollingUpdateStatefulSetStrategyType,
		},
		Selector: &metav1.LabelSelector{
			MatchLabels: cr.SelectorLabels(),
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      cr.PodLabels(),
				Annotations: cr.PodAnnotations(),
			},
			Spec: v1.PodSpec{
				NodeSelector:                  cr.Spec.NodeSelector,
				PriorityClassName:             cr.Spec.PriorityClassName,
				TerminationGracePeriodSeconds: &terminationGracePeriod,
				InitContainers:                cr.Spec.InitContainers,
				Containers:                    containers,
				Volumes:                       volumes,
				RuntimeClassName:              cr.Spec.RuntimeClassName,
				SchedulerName:                 cr.Spec.SchedulerName,
				ServiceAccountName:            cr.GetServiceAccountName(),
				SecurityContext:               cr.Spec.SecurityContext,
				Tolerations:                   cr.Spec.Tolerations,
				Affinity:                      cr.Spec.Affinity,
				HostNetwork:                   cr.Spec.HostNetwork,
				DNSPolicy:                     cr.Spec.DNSPolicy,
				TopologySpreadConstraints:     cr.Spec.TopologySpreadConstraints,
			},
		},
	}, nil
}

func MakeVolumeClaimTemplate(e victoriametricsv1beta1.EmbeddedPersistentVolumeClaim) *v1.PersistentVolumeClaim {
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

// createDefaultAMConfig - check if secret with config exist,
// if not create with predefined or user value.
func createDefaultAMConfig(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client) error {
	cr = cr.DeepCopy()
	if cr.Spec.ConfigSecret == "" {
		cr.Spec.ConfigSecret = cr.PrefixedName()
	}
	var mustUpdateConfig bool
	if cr.Spec.ConfigRawYaml == "" {
		cr.Spec.ConfigRawYaml = defaultAMConfig
	} else {
		mustUpdateConfig = true
	}
	amSecretConfig := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.ConfigSecret,
			Namespace:       cr.Namespace,
			Labels:          cr.Labels(),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
		},
		StringData: map[string]string{"alertmanager.yaml": cr.Spec.ConfigRawYaml},
	}
	var existAMSecretConfig v1.Secret

	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Spec.ConfigSecret}, &existAMSecretConfig)
	// fast path
	if err == nil {
		if mustUpdateConfig {
			return rclient.Update(ctx, amSecretConfig)
		}
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}
	log.Info("creating default alertmanager config with secret: %s", "secret_name", amSecretConfig.ObjectMeta.Name)
	return rclient.Create(ctx, amSecretConfig)
}

func subPathForStorage(s *victoriametricsv1beta1.StorageSpec) string {
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
