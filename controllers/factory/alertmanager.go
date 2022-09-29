package factory

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/alertmanager"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/psp"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
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
	defaultRetention            = "120h"
	alertmanagerSecretConfigKey = "alertmanager.yaml"
	alertmanagerConfDir         = "/etc/alertmanager/config"
	alertmanagerConfFile        = alertmanagerConfDir + "/alertmanager.yaml"
	alertmanagerStorageDir      = "/alertmanager"
	defaultPortName             = "web"
	defaultAMConfig             = `
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

func CreateOrUpdateAlertManager(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client, c *config.BaseOperatorConf) error {
	l := log.WithValues("reconcile.VMAlertManager.sts", cr.Name, "ns", cr.Namespace)

	if err := psp.CreateServiceAccountForCRD(ctx, cr, rclient); err != nil {
		return fmt.Errorf("failed create service account: %w", err)
	}
	if c.PSPAutoCreateEnabled {
		if err := psp.CreateOrUpdateServiceAccountWithPSP(ctx, cr, rclient); err != nil {
			l.Error(err, "cannot create podsecuritypolicy")
			return fmt.Errorf("cannot create podsecurity policy for alertmanager, err=%w", err)
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		if err := CreateOrUpdatePodDisruptionBudget(ctx, rclient, cr, cr.Kind, cr.Spec.PodDisruptionBudget); err != nil {
			return fmt.Errorf("cannot update pod disruption budget for alertmanager: %w", err)
		}
	}
	// special hack, we need version for alertmanager a bit earlier.
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMAlertManager.AlertManagerVersion
	}
	amVersion, err := version.NewVersion(cr.Spec.Image.Tag)
	if err != nil {
		l.Error(err, "cannot parse alertmanager version", "tag", cr.Spec.Image.Tag)
	}
	newSts, err := newStsForAlertManager(cr, c, amVersion)
	if err != nil {
		return fmt.Errorf("cannot generate alertmanager sts, name: %s,err: %w", cr.Name, err)
	}
	// check secret with config
	if err := createDefaultAMConfig(ctx, cr, rclient, amVersion); err != nil {
		return fmt.Errorf("failed to check default Alertmanager config: %w", err)
	}

	stsOpts := k8stools.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		VolumeName:     cr.GetVolumeName,
		SelectorLabels: cr.SelectorLabels,
		UpdateStrategy: cr.UpdateStrategy,
	}
	return k8stools.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, c)
}

func newStsForAlertManager(cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf, amVersion *version.Version) (*appsv1.StatefulSet, error) {

	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMAlertManager.AlertmanagerDefaultBaseImage
	}
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
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

	spec, err := makeStatefulSetSpec(cr, c, amVersion)
	if err != nil {
		return nil, err
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Labels:          c.Labels.Merge(cr.AllLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: *spec,
	}

	if cr.Spec.ImagePullSecrets != nil && len(cr.Spec.ImagePullSecrets) > 0 {
		statefulset.Spec.Template.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets
	}

	cr.Spec.Storage.IntoSTSVolume(cr.GetVolumeName(), &statefulset.Spec)
	statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, cr.Spec.Volumes...)

	return statefulset, nil
}

func CreateOrUpdateAlertManagerService(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client) (*v1.Service, error) {
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

func makeStatefulSetSpec(cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf, amVersion *version.Version) (*appsv1.StatefulSetSpec, error) {

	cr = cr.DeepCopy()

	image := fmt.Sprintf("%s:%s", formatContainerImage(c.ContainerRegistry, cr.Spec.Image.Repository), cr.Spec.Image.Tag)

	amArgs := []string{
		fmt.Sprintf("--config.file=%s", alertmanagerConfFile),
		fmt.Sprintf("--storage.path=%s", alertmanagerStorageDir),
		fmt.Sprintf("--data.retention=%s", cr.Spec.Retention),
	}

	if *cr.Spec.ReplicaCount == 1 {
		amArgs = append(amArgs, "--cluster.listen-address=")
	} else {
		amArgs = append(amArgs, "--cluster.listen-address=[$(POD_IP)]:9094")
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
		amArgs = append(amArgs, fmt.Sprintf("--cluster.peer=%s-%d.%s:9094", cr.PrefixedName(), i, clusterPeerDomain))
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

	if amVersion != nil {
		// Adjust VMAlertmanager command line args to specified AM version
		if amVersion.LessThan(version.Must(version.NewVersion("v0.15.0"))) {
			for i := range amArgs {
				// below VMAlertmanager v0.15.0 peer address port specification is not necessary
				if strings.Contains(amArgs[i], "--cluster.peer") {
					amArgs[i] = strings.TrimSuffix(amArgs[i], ":9094")
				}

				// below VMAlertmanager v0.15.0 high availability flags are prefixed with 'mesh' instead of 'cluster'
				amArgs[i] = strings.Replace(amArgs[i], "--cluster.", "--mesh.", 1)
			}
		}
		if amVersion.LessThan(version.Must(version.NewVersion("v0.13.0"))) {
			for i := range amArgs {
				// below VMAlertmanager v0.13.0 all flags are with single dash.
				amArgs[i] = strings.Replace(amArgs[i], "--", "-", 1)
			}
		}
		if amVersion.LessThan(version.Must(version.NewVersion("v0.7.0"))) {
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
					SecretName: cr.ConfigSecretName(),
				},
			},
		},
	}

	amVolumeMounts := []v1.VolumeMount{
		{
			Name:      "config-volume",
			MountPath: alertmanagerConfDir,
		},
		{
			Name:      cr.GetVolumeName(),
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
	if cr.Spec.TerminationGracePeriodSeconds != nil {
		terminationGracePeriod = *cr.Spec.TerminationGracePeriodSeconds
	}

	amArgs = addExtraArgsOverrideDefaults(amArgs, cr.Spec.ExtraArgs, "--")
	sort.Strings(amArgs)

	envs := []v1.EnvVar{
		{
			// Necessary for '--cluster.listen-address' flag
			Name: "POD_IP",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
	}
	envs = append(envs, cr.Spec.ExtraEnvs...)

	vmaContainer := v1.Container{
		Args:                     amArgs,
		Name:                     "alertmanager",
		Image:                    image,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             amVolumeMounts,
		Resources:                buildResources(cr.Spec.Resources, config.Resource(c.VMAlertDefault.Resource), c.VMAlertManager.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
	}
	vmaContainer = buildProbe(vmaContainer, cr) // cr.Spec.EmbeddedProbes, healthPath, cr.Spec.PortName, true)
	defaultContainers := []v1.Container{
		vmaContainer,
		{
			Name:  "config-reloader",
			Image: fmt.Sprintf("%s", formatContainerImage(c.ContainerRegistry, c.VMAlertManager.ConfigReloaderImage)),
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
			Type: cr.UpdateStrategy(),
		},
		Selector: &metav1.LabelSelector{
			MatchLabels: cr.SelectorLabels(),
		},
		VolumeClaimTemplates: cr.Spec.ClaimTemplates,
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
				DNSConfig:                     cr.Spec.DNSConfig,
				TopologySpreadConstraints:     cr.Spec.TopologySpreadConstraints,
				ReadinessGates:                cr.Spec.ReadinessGates,
			},
		},
	}, nil
}

var alertmanagerConfigMinimumVersion = version.Must(version.NewVersion("v0.22.0"))

// createDefaultAMConfig - check if secret with config exist,
// if not create with predefined or user value.
func createDefaultAMConfig(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client, amVersion *version.Version) error {
	cr = cr.DeepCopy()
	l := log.WithValues("alertmanager", cr.Name)

	// name of tls object and it's value
	// e.g. namespace_secret_name_secret_key
	tlsAssets := make(map[string]string)

	var alertmananagerConfig []byte
	switch {
	// fetch content from user defined secret
	case cr.Spec.ConfigSecret != "":
		// retrieve content
		secretContent, err := getSecretContentForAlertmanager(ctx, rclient, cr.Spec.ConfigSecret, cr.Namespace)
		if err != nil {
			return fmt.Errorf("cannot fetch secret content for alertmanager config secret, err: %w", err)
		}
		alertmananagerConfig = secretContent
	// use in-line config
	case cr.Spec.ConfigRawYaml != "":
		alertmananagerConfig = []byte(cr.Spec.ConfigRawYaml)
	}
	if cr.Spec.ConfigSelector != nil || cr.Spec.SelectAllByDefault {
		// it makes sense only for alertmanager version above v22.0
		if amVersion != nil && !amVersion.LessThan(alertmanagerConfigMinimumVersion) {
			mergedCfg, err := buildAlertmanagerConfigWithCRDs(ctx, rclient, cr, alertmananagerConfig, l, tlsAssets)
			if err != nil {
				return fmt.Errorf("cannot build alertmanager config with configSelector, err: %w", err)
			}
			alertmananagerConfig = mergedCfg
		} else {
			l.Info("alertmanager version doesnt supports VMAlertmanagerConfig CRD", "version", amVersion)
		}
	}

	// apply default config to be able just start alertmanager
	if len(alertmananagerConfig) == 0 {
		alertmananagerConfig = []byte(defaultAMConfig)
	}
	newAMSecretConfig := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.ConfigSecretName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Data: map[string][]byte{alertmanagerSecretConfigKey: alertmananagerConfig},
	}

	for assetKey, assetValue := range tlsAssets {
		newAMSecretConfig.Data[assetKey] = []byte(assetValue)
	}
	var existAMSecretConfig v1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.ConfigSecretName()}, &existAMSecretConfig); err != nil {
		if errors.IsNotFound(err) {
			log.Info("creating default alertmanager config with secret", "secret_name", newAMSecretConfig.Name)
			return rclient.Create(ctx, newAMSecretConfig)
		}
		return err
	}

	newAMSecretConfig.Annotations = labels.Merge(existAMSecretConfig.Annotations, newAMSecretConfig.Annotations)
	newAMSecretConfig.Finalizers = victoriametricsv1beta1.MergeFinalizers(&existAMSecretConfig, victoriametricsv1beta1.FinalizerName)
	return rclient.Update(ctx, newAMSecretConfig)
}

func getSecretContentForAlertmanager(ctx context.Context, rclient client.Client, secretName, ns string) ([]byte, error) {
	var s v1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, &s); err != nil {
		// return nil for backward compatability
		if errors.IsNotFound(err) {
			log.Error(err, "alertmanager config secret doens't exist, default config is used", "secret", secretName, "ns", ns)
			return nil, nil
		}
		return nil, fmt.Errorf("cannot get secret: %s at ns: %s, err: %w", secretName, ns, err)
	}
	if d, ok := s.Data[alertmanagerSecretConfigKey]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("cannot find alertmanager config key: %q at secret: %q", alertmanagerSecretConfigKey, secretName)
}

func buildAlertmanagerConfigWithCRDs(ctx context.Context, rclient client.Client, cr *victoriametricsv1beta1.VMAlertmanager, originConfig []byte, l logr.Logger, tlsAssets map[string]string) ([]byte, error) {
	amConfigs := make(map[string]*victoriametricsv1beta1.VMAlertmanagerConfig)
	var badCfgCount int
	// handle case for config selector.
	namespaces, objSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.ConfigNamespaceSelector, cr.Spec.ConfigSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}
	if err := visitObjectsWithSelector(ctx, rclient, namespaces, &victoriametricsv1beta1.VMAlertmanagerConfigList{}, objSelector, cr.Spec.SelectAllByDefault, func(list client.ObjectList) {
		ams := list.(*victoriametricsv1beta1.VMAlertmanagerConfigList)
		for i := range ams.Items {
			item := ams.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			if err := item.Validate(); err != nil {
				l.Error(err, "validation failed for alertmanager config", "objectName", item.Name)
				badCfgCount++
				continue
			}
			amConfigs[item.AsKey()] = &item
		}
	}); err != nil {
		return nil, fmt.Errorf("cannot select alertmanager configs: %w", err)
	}

	parsedCfg, err := alertmanager.BuildConfig(ctx, rclient, !cr.Spec.DisableNamespaceMatcher, originConfig, amConfigs, tlsAssets)
	if err != nil {
		return nil, err
	}
	l.Info("selected alertmanager configs", "len", len(amConfigs), "invalid configs", badCfgCount+parsedCfg.BadObjectsCount)
	if len(parsedCfg.ParseErrors) > 0 {
		l.Error(fmt.Errorf("errors: %s", strings.Join(parsedCfg.ParseErrors, ";")), "bad configs found during alertmanager config building")
	}
	badConfigsTotal.WithLabelValues("vmalertmanager_config").Add(float64(badCfgCount))
	return parsedCfg.Data, nil
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
