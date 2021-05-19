package factory

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/psp"
	"github.com/VictoriaMetrics/operator/internal/config"
	version "github.com/hashicorp/go-version"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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

	if cr.Spec.PodDisruptionBudget != nil {
		err := CreateOrUpdatePodDisruptionBudgetForAlertManager(ctx, cr, rclient)
		if err != nil {
			return nil, fmt.Errorf("cannot update pod disruption budget for vmagent: %w", err)
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
			if err = rclient.Create(ctx, newSts); err != nil {
				return nil, fmt.Errorf("cannot create new alertmanager sts: %w", err)
			}
			l.Info("new sts was created for alertmanager")
			return newSts, nil
		}
		return nil, fmt.Errorf("cannot get alertmanager sts: %w", err)
	}
	if err := performRollingUpdateOnSts(ctx, rclient, newSts.Name, newSts.Namespace, cr.SelectorLabels(), c); err != nil {
		return nil, fmt.Errorf("cannot update statefulset for vmalertmanager: %w", err)
	}
	recreatedSts, err := wasCreatedSTS(ctx, rclient, volumeName(cr.Name), newSts, currentSts)
	if err != nil {
		return nil, err
	}
	if recreatedSts != nil {
		return recreatedSts, nil
	}
	if err := growSTSPVC(ctx, rclient, newSts, volumeName(cr.Name)); err != nil {
		return nil, err
	}

	return newSts, updateStsForAlertManager(ctx, rclient, currentSts, newSts)
}

func updateStsForAlertManager(ctx context.Context, rclient client.Client, oldSts, newSts *appsv1.StatefulSet) error {
	newSts.Annotations = labels.Merge(newSts.Annotations, oldSts.Annotations)
	newSts.Spec.Template.Annotations = labels.Merge(newSts.Spec.Template.Annotations, oldSts.Spec.Template.Annotations)
	// hack for break reconcile loop at kubernetes 1.18
	newSts.Status.Replicas = oldSts.Status.Replicas
	newSts.Finalizers = victoriametricsv1beta1.MergeFinalizers(oldSts, victoriametricsv1beta1.FinalizerName)

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
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: *spec,
	}

	if cr.Spec.ImagePullSecrets != nil && len(cr.Spec.ImagePullSecrets) > 0 {
		statefulset.Spec.Template.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets
	}

	storageSpec := cr.Spec.Storage
	switch {
	case storageSpec == nil:
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, v1.Volume{
			Name: volumeName(cr.Name),
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		})
	case storageSpec.EmptyDir != nil:
		emptyDir := storageSpec.EmptyDir
		statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, v1.Volume{
			Name: volumeName(cr.Name),
			VolumeSource: v1.VolumeSource{
				EmptyDir: emptyDir,
			},
		})
	default:
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
	cr = cr.DeepCopy()
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
	}

	additionalService := buildDefaultService(cr, cr.Spec.PortName, func(svc *v1.Service) {
		svc.Spec.Ports[0].Port = 9093
	})
	mergeServiceSpec(additionalService, cr.Spec.ServiceSpec)

	newService := buildDefaultService(cr, cr.Spec.PortName, func(svc *v1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.Ports[0].Port = 9093
		svc.Spec.Ports = append(svc.Spec.Ports,
			v1.ServicePort{
				Name:       "tcp-mesh",
				Port:       9094,
				TargetPort: intstr.FromInt(9094),
				Protocol:   v1.ProtocolTCP,
			},
			v1.ServicePort{
				Name:       "udp-mesh",
				Port:       9094,
				TargetPort: intstr.FromInt(9094),
				Protocol:   v1.ProtocolUDP,
			},
		)
	})

	if cr.Spec.ServiceSpec != nil {
		if additionalService.Name == newService.Name {
			log.Error(fmt.Errorf("vmalertmanager additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
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

func makeStatefulSetSpec(cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf) (*appsv1.StatefulSetSpec, error) {

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
		Host:   c.VMAlertManager.LocalHost + ":9093",
		Path:   path.Clean(webRoutePrefix + "/-/reload"),
	}

	var clusterPeerDomain string
	if c.ClusterDomainName != "" {
		clusterPeerDomain = fmt.Sprintf("%s.%s.svc.%s.", cr.PrefixedName(), cr.Namespace, c.ClusterDomainName)
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

	ver, err := version.NewVersion(cr.Spec.Image.Tag)
	if err != nil {
		log.Error(err, "cannot parse alert manager version")
	} else {
		// Adjust VMAlertmanager command line args to specified AM version
		if ver.LessThan(version.Must(version.NewVersion("v0.15.0"))) {
			for i := range amArgs {
				// below VMAlertmanager v0.15.0 peer address port specification is not necessary
				if strings.Contains(amArgs[i], "--cluster.peer") {
					amArgs[i] = strings.TrimSuffix(amArgs[i], ":9094")
				}

				// below VMAlertmanager v0.15.0 high availability flags are prefixed with 'mesh' instead of 'cluster'
				amArgs[i] = strings.Replace(amArgs[i], "--cluster.", "--mesh.", 1)
			}
		}
		if ver.LessThan(version.Must(version.NewVersion("v0.13.0"))) {
			for i := range amArgs {
				// below VMAlertmanager v0.13.0 all flags are with single dash.
				amArgs[i] = strings.Replace(amArgs[i], "--", "-", 1)
			}
		}
		if ver.LessThan(version.Must(version.NewVersion("v0.7.0"))) {
			// below VMAlertmanager v0.7.0 the flag 'web.route-prefix' does not exist
			amArgs = filter(amArgs, func(s string) bool {
				return !strings.Contains(s, "web.route-prefix")
			})
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
	if c.VMAlertManager.ConfigReloaderCPU != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[v1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
	}
	if c.VMAlertManager.ConfigReloaderMemory != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[v1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
	}

	terminationGracePeriod := int64(120)

	healthPath := func() string {
		return path.Clean(webRoutePrefix + "/-/healthy")
	}

	vmaContainer := v1.Container{
		Args:            amArgs,
		Name:            "alertmanager",
		Image:           image,
		ImagePullPolicy: cr.Spec.Image.PullPolicy,
		Ports:           ports,
		VolumeMounts:    amVolumeMounts,
		Resources:       buildResources(cr.Spec.Resources, config.Resource(c.VMAlertDefault.Resource), c.VMAlertManager.UseDefaultResources),
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
	}
	vmaContainer = buildProbe(vmaContainer, cr.Spec.EmbeddedProbes, healthPath, cr.Spec.PortName, true)
	defaultContainers := []v1.Container{
		vmaContainer,
		{
			Name:  "config-reloader",
			Image: c.VMAlertManager.ConfigReloaderImage,
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

	for i := range cr.Spec.TopologySpreadConstraints {
		if cr.Spec.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			}
		}
	}

	return &appsv1.StatefulSetSpec{
		ServiceName:         cr.PrefixedName(),
		Replicas:            cr.Spec.ReplicaCount,
		PodManagementPolicy: appsv1.ParallelPodManagement,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			Type: appsv1.OnDeleteStatefulSetStrategyType,
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
	// expect configuration secret to be pre created by user.
	if cr.Spec.ConfigRawYaml == "" && cr.Spec.ConfigSecret != "" {
		// its users responsibility to create secret.
		return nil
	}
	// case for raw config is defined by user.
	if cr.Spec.ConfigSecret == "" {
		cr.Spec.ConfigSecret = cr.PrefixedName()
	}
	if cr.Spec.ConfigRawYaml == "" {
		cr.Spec.ConfigRawYaml = defaultAMConfig
	}
	defaultAMSecretConfig := &v1.Secret{
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
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("creating default alertmanager config with secret", "secret_name", defaultAMSecretConfig.Name)
			return rclient.Create(ctx, defaultAMSecretConfig)
		}
		return err
	}
	defaultAMSecretConfig.Annotations = labels.Merge(defaultAMSecretConfig.Annotations, existAMSecretConfig.Annotations)
	return rclient.Update(ctx, defaultAMSecretConfig)
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

func CreateOrUpdatePodDisruptionBudgetForAlertManager(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client) error {
	pdb := buildDefaultPDB(cr, cr.Spec.PodDisruptionBudget)
	return reconcilePDB(ctx, rclient, cr.Kind, pdb)
}
