package factory

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	vmAgentConfDir     = "/etc/vmagent/config"
	vmAgentConOfOutDir = "/etc/vmagent/config_out"
)

func CreateOrUpdateVMAgentService(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	l := log.WithValues("recon.vm.service.name", cr.Name)
	NewService := newServiceVMAgent(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: NewService.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vm agent")
			err := rclient.Create(ctx, NewService)
			if err != nil {
				return nil, fmt.Errorf("cannot create new service for vmagent: %w", err)
			}
		} else {
			return nil, fmt.Errorf("cannot get vmagent service for reconcile: %w", err)
		}
	}
	for annotation, value := range currentService.Annotations {
		NewService.Annotations[annotation] = value
	}
	if currentService.Spec.ClusterIP != "" {
		NewService.Spec.ClusterIP = currentService.Spec.ClusterIP
	}
	if currentService.ResourceVersion != "" {
		NewService.ResourceVersion = currentService.ResourceVersion
	}
	err = rclient.Update(ctx, NewService)
	if err != nil {
		l.Error(err, "cannot update vmagent service")
		return nil, err
	}

	l.Info("vmagent service reconciled")
	return NewService, nil
}

func newServiceVMAgent(cr *victoriametricsv1beta1.VMAgent, c *config.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMAgentDefault.Port
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
}

//we assume, that configmaps were created before this function was called
func CreateOrUpdateVMAgent(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client, c *config.BaseOperatorConf) (reconcile.Result, error) {
	l := log.WithValues("controller", "vmagent.crud")

	//we have to create empty or full cm first
	err := CreateOrUpdateConfigurationSecret(ctx, cr, rclient, c)
	if err != nil {
		l.Error(err, "cannot create configmap")
		return reconcile.Result{}, err
	}

	err = CreateOrUpdateTlsAssets(ctx, cr, rclient)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot update tls asset for vmagent: %w", err)
	}

	// getting secrets for remotewrite spec
	rwsBasicAuthSecrets, rwsTokens, err := LoadRemoteWriteSecrets(ctx, cr, rclient, l)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot get remote write secrets for vmagent: %w", err)
	}
	l.Info("create or update vm agent deploy")

	newDeploy, err := newDeployForVMAgent(cr, c, rwsBasicAuthSecrets, rwsTokens)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot build new deploy for vmagent: %w", err)
	}

	l = l.WithValues("vmagent.deploy.name", newDeploy.Name, "vmagent.deploy.namespace", newDeploy.Namespace)

	currentDeploy := &appsv1.Deployment{}
	err = rclient.Get(ctx, types.NamespacedName{Name: newDeploy.Name, Namespace: newDeploy.Namespace}, currentDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			//create new
			l.Info("vmagent deploy not found, creating new one")
			err := rclient.Create(ctx, newDeploy)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("cannot create new vmagent deploy: %w", err)
			}
			l.Info("new vmagent deploy was created")
		} else {
			return reconcile.Result{}, fmt.Errorf("cannot get vmagent deploy: %s,err: %w", newDeploy.Name, err)
		}
	}
	l.Info("updating  vmagent")
	for annotation, value := range currentDeploy.Annotations {
		newDeploy.Annotations[annotation] = value
	}
	for annotation, value := range currentDeploy.Spec.Template.Annotations {
		newDeploy.Spec.Template.Annotations[annotation] = value
	}

	err = rclient.Update(ctx, newDeploy)
	if err != nil {
		l.Error(err, "cannot update vmagent deploy")
	}

	//its safe to ignore
	_ = addAddtionalScrapeConfigOwnership(cr, rclient, l)
	l.Info("vmagent deploy reconciled")

	return reconcile.Result{}, nil
}

// newDeployForCR returns a busybox pod with the same name/namespace as the cr
func newDeployForVMAgent(cr *victoriametricsv1beta1.VMAgent, c *config.BaseOperatorConf, rwsBasicAuth map[string]BasicAuthCredentials, rwsTokens map[string]BearerToken) (*appsv1.Deployment, error) {
	cr = cr.DeepCopy()

	//inject default
	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMAgentDefault.Image
	}
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMAgentDefault.Version
	}
	if cr.Spec.Image.PullPolicy == "" {
		cr.Spec.Image.PullPolicy = corev1.PullIfNotPresent
	}

	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMAgentDefault.Port
	}
	if cr.Spec.Resources.Requests == nil {
		cr.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.Resources.Limits == nil {
		cr.Spec.Resources.Limits = corev1.ResourceList{}
	}

	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceMemory]; !ok {
		cr.Spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAgentDefault.Resource.Limit.Mem)
	}
	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceCPU]; !ok {
		cr.Spec.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAgentDefault.Resource.Limit.Cpu)
	}

	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceMemory]; !ok {
		cr.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMAgentDefault.Resource.Request.Mem)
	}
	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceCPU]; !ok {
		cr.Spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMAgentDefault.Resource.Request.Cpu)
	}

	podSpec, err := makeSpecForVMAgent(cr, c, rwsBasicAuth, rwsTokens)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.FinalLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{},
			},
			Template: *podSpec,
		},
	}
	return depSpec, nil
}

func makeSpecForVMAgent(cr *victoriametricsv1beta1.VMAgent, c *config.BaseOperatorConf, rwsBasicAuth map[string]BasicAuthCredentials, rwsTokens map[string]BearerToken) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-promscrape.config=%s", path.Join(vmAgentConOfOutDir, configEnvsubstFilename)),
	}

	if len(cr.Spec.RemoteWrite) > 0 {
		args = append(args, BuildRemoteWrites(cr, rwsBasicAuth, rwsTokens)...)
	}

	for arg, value := range cr.Spec.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s=%s", arg, value))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}
	if len(cr.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	var volumes []corev1.Volume
	volumes = append(volumes, cr.Spec.Volumes...)
	volumes = append(volumes, corev1.Volume{
		Name: "config",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: cr.PrefixedName(),
			},
		},
	},
		corev1.Volume{
			Name: "tls-assets",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.TLSAssetName(),
				},
			},
		},
		corev1.Volume{
			Name: "config-out",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	)

	var agentVolumeMounts []corev1.VolumeMount

	agentVolumeMounts = append(agentVolumeMounts, cr.Spec.VolumeMounts...)
	agentVolumeMounts = append(agentVolumeMounts,
		corev1.VolumeMount{
			Name:      "config-out",
			ReadOnly:  true,
			MountPath: vmAgentConOfOutDir,
		},
		corev1.VolumeMount{
			Name:      "tls-assets",
			ReadOnly:  true,
			MountPath: tlsAssetsDir,
		},
	)

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, c),
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
		fmt.Sprintf("--reload-url=%s", cr.ReloadPathWithPort(cr.Spec.Port)),
		fmt.Sprintf("--config-file=%s", path.Join(vmAgentConfDir, configFilename)),
		fmt.Sprintf("--config-envsubst-file=%s", path.Join(vmAgentConOfOutDir, configEnvsubstFilename)),
	}

	if cr.Spec.RelabelConfig != nil {
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("configmap-" + cr.Spec.RelabelConfig.Name),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cr.Spec.RelabelConfig.Name,
					},
				},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("configmap-" + cr.Spec.RelabelConfig.Name),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, cr.Spec.RelabelConfig.Name),
		})

		args = append(args, "-remoteWrite.relabelConfig="+path.Join(ConfigMapsDir, cr.Spec.RelabelConfig.Name, cr.Spec.RelabelConfig.Key))
	}

	for _, rw := range cr.Spec.RemoteWrite {
		if rw.UrlRelabelConfig == nil {
			continue
		}
		if rw.UrlRelabelConfig.Name == cr.Spec.RelabelConfig.Name {
			continue
		}
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("configmap-" + rw.UrlRelabelConfig.Name),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: rw.UrlRelabelConfig.Name,
					},
				},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("configmap-" + rw.UrlRelabelConfig.Name),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, rw.UrlRelabelConfig.Name),
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

	prometheusConfigReloaderResources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{}, Requests: corev1.ResourceList{}}
	if c.VMAgentDefault.ConfigReloaderCPU != "0" {
		prometheusConfigReloaderResources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAgentDefault.ConfigReloaderCPU)
		prometheusConfigReloaderResources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMAgentDefault.ConfigReloaderCPU)
	}
	if c.VMAgentDefault.ConfigReloaderMemory != "0" {
		prometheusConfigReloaderResources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAgentDefault.ConfigReloaderMemory)
		prometheusConfigReloaderResources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMAgentDefault.ConfigReloaderMemory)
	}

	operatorContainers := append([]corev1.Container{
		{
			Name:                     "vmagent",
			Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
			ImagePullPolicy:          cr.Spec.Image.PullPolicy,
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
			Image:                    c.VMAgentDefault.ConfigReloadImage,
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

	containers, err := MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	vmAgentSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(),
			Annotations: cr.PodAnnotations(),
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
			HostNetwork:        cr.Spec.HostNetwork,
			DNSPolicy:          cr.Spec.DNSPolicy,
		},
	}

	return vmAgentSpec, nil
}

//add ownership - it needs for object changing tracking
func addAddtionalScrapeConfigOwnership(cr *victoriametricsv1beta1.VMAgent, rclient client.Client, l logr.Logger) error {
	if cr.Spec.AdditionalScrapeConfigs == nil {
		return nil
	}
	secret := &corev1.Secret{}
	err := rclient.Get(context.Background(), types.NamespacedName{Namespace: cr.Namespace, Name: cr.Spec.AdditionalScrapeConfigs.Name}, secret)
	if err != nil {
		return fmt.Errorf("cannot get secret with additional scrape config: %s, err: %w ", cr.Spec.AdditionalScrapeConfigs.Name, err)
	}
	for _, owner := range secret.OwnerReferences {
		//owner exists
		if owner.Name == cr.Name {
			return nil
		}
	}
	secret.OwnerReferences = append(secret.OwnerReferences, metav1.OwnerReference{
		APIVersion:         cr.APIVersion,
		Kind:               cr.Kind,
		Name:               cr.Name,
		Controller:         pointer.BoolPtr(false),
		BlockOwnerDeletion: pointer.BoolPtr(false),
		UID:                cr.UID,
	})

	l.Info("updating additional scrape secret ownership", "secret", secret.Name)
	err = rclient.Update(context.Background(), secret)
	if err != nil {
		return fmt.Errorf("cannot update secret ownership for vmagent Additional scrape: %s, err: %w", secret.Name, err)
	}
	l.Info("scrape secret was updated with new owner", "secret", secret.Name)
	return nil
}

func CreateOrUpdateTlsAssets(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client) error {
	scrapes, err := SelectServiceScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("cannot select service scrapes for tls Assets: %w", err)
	}
	assets, err := loadTLSAssets(ctx, rclient, cr, scrapes)
	if err != nil {
		return fmt.Errorf("cannot load tls assets: %w", err)
	}

	tlsAssetsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.TLSAssetName(),
			Labels:          cr.FinalLabels(),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
		},
		Data: map[string][]byte{},
	}

	for key, asset := range assets {
		tlsAssetsSecret.Data[key] = []byte(asset)
	}
	currentAssetSecret := &corev1.Secret{}
	err = rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: tlsAssetsSecret.Name}, currentAssetSecret)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("cannot get existing tls secret: %s, for vmagent: %s, err: %w", tlsAssetsSecret.Name, cr.Name, err)
		}
		err := rclient.Create(ctx, tlsAssetsSecret)
		if err != nil {
			return fmt.Errorf("cannot create tls asset secret: %s for vmagent: %s, err :%w", tlsAssetsSecret.Name, cr.Name, err)
		}
		log.Info("create new tls asset secret: %s, for vmagent: %s", tlsAssetsSecret.Name, cr.Name)
		return nil
	}
	for annotation, value := range currentAssetSecret.Annotations {
		tlsAssetsSecret.Annotations[annotation] = value
	}
	return rclient.Update(ctx, tlsAssetsSecret)
}

func loadTLSAssets(ctx context.Context, rclient client.Client, cr *victoriametricsv1beta1.VMAgent, scrapes map[string]*victoriametricsv1beta1.VMServiceScrape) (map[string]string, error) {
	assets := map[string]string{}
	nsSecretCache := make(map[string]*corev1.Secret)
	nsConfigMapCache := make(map[string]*corev1.ConfigMap)

	for _, rw := range cr.Spec.RemoteWrite {
		if rw.TLSConfig == nil {
			continue
		}
		prefix := cr.Namespace + "/"
		secretSelectors := map[string]*corev1.SecretKeySelector{}
		configMapSelectors := map[string]*corev1.ConfigMapKeySelector{}
		selectorKey := rw.TLSConfig.CA.BuildSelectorWithPrefix(prefix)
		switch {
		case rw.TLSConfig.CA.Secret != nil:
			secretSelectors[selectorKey] = rw.TLSConfig.CA.Secret
		case rw.TLSConfig.CA.ConfigMap != nil:
			configMapSelectors[selectorKey] = rw.TLSConfig.CA.ConfigMap
		}
		selectorKey = rw.TLSConfig.Cert.BuildSelectorWithPrefix(prefix)

		switch {
		case rw.TLSConfig.Cert.Secret != nil:
			secretSelectors[selectorKey] = rw.TLSConfig.Cert.Secret

		case rw.TLSConfig.Cert.ConfigMap != nil:
			configMapSelectors[selectorKey] = rw.TLSConfig.Cert.ConfigMap
		}
		if rw.TLSConfig.KeySecret != nil {
			secretSelectors[prefix+rw.TLSConfig.KeySecret.Name+"/"+rw.TLSConfig.KeySecret.Key] = rw.TLSConfig.KeySecret
		}
		for key, selector := range secretSelectors {
			asset, err := getCredFromSecret(
				ctx,
				rclient,
				cr.Namespace,
				*selector,
				key,
				nsSecretCache,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract endpoint tls asset for vmagentremote write target %s from secret %s and key %s in namespace %s",
					cr.Name, selector.Name, selector.Key, cr.Namespace,
				)
			}

			assets[rw.TLSConfig.BuildAssetPath(cr.Namespace, selector.Name, selector.Key)] = asset
		}

		for key, selector := range configMapSelectors {
			asset, err := getCredFromConfigMap(
				ctx,
				rclient,
				cr.Namespace,
				*selector,
				key,
				nsConfigMapCache,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract endpoint tls asset for vmservicescrape %v from configmap %v and key %v in namespace %v",
					cr.Name, selector.Name, selector.Key, cr.Namespace,
				)
			}
			assets[rw.TLSConfig.BuildAssetPath(cr.Namespace, selector.Name, selector.Key)] = asset
		}
	}
	for _, mon := range scrapes {
		for _, ep := range mon.Spec.Endpoints {
			if ep.TLSConfig == nil {
				continue
			}
			prefix := mon.Namespace + "/"
			secretSelectors := map[string]*corev1.SecretKeySelector{}
			configMapSelectors := map[string]*corev1.ConfigMapKeySelector{}
			if ep.TLSConfig.CA != (victoriametricsv1beta1.SecretOrConfigMap{}) {
				selectorKey := ep.TLSConfig.CA.BuildSelectorWithPrefix(prefix)
				switch {
				case ep.TLSConfig.CA.Secret != nil:
					secretSelectors[selectorKey] = ep.TLSConfig.CA.Secret
				case ep.TLSConfig.CA.ConfigMap != nil:
					configMapSelectors[selectorKey] = ep.TLSConfig.CA.ConfigMap
				}
			}
			if ep.TLSConfig.Cert != (victoriametricsv1beta1.SecretOrConfigMap{}) {
				selectorKey := ep.TLSConfig.Cert.BuildSelectorWithPrefix(prefix)
				switch {
				case ep.TLSConfig.Cert.Secret != nil:
					secretSelectors[selectorKey] = ep.TLSConfig.Cert.Secret
				case ep.TLSConfig.Cert.ConfigMap != nil:
					configMapSelectors[selectorKey] = ep.TLSConfig.Cert.ConfigMap
				}
			}
			if ep.TLSConfig.KeySecret != nil {
				secretSelectors[prefix+ep.TLSConfig.KeySecret.Name+"/"+ep.TLSConfig.KeySecret.Key] = ep.TLSConfig.KeySecret
			}

			for key, selector := range secretSelectors {
				asset, err := getCredFromSecret(
					ctx,
					rclient,
					mon.Namespace,
					*selector,
					key,
					nsSecretCache,
				)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to extract endpoint tls asset for vmservicescrape %s from secret %s and key %s in namespace %s",
						mon.Name, selector.Name, selector.Key, mon.Namespace,
					)
				}

				assets[ep.TLSConfig.BuildAssetPath(mon.Namespace, selector.Name, selector.Key)] = asset
			}

			for key, selector := range configMapSelectors {
				asset, err := getCredFromConfigMap(
					ctx,
					rclient,
					mon.Namespace,
					*selector,
					key,
					nsConfigMapCache,
				)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to extract endpoint tls asset for vmservicescrape %v from configmap %v and key %v in namespace %v",
						mon.Name, selector.Name, selector.Key, mon.Namespace,
					)
				}

				assets[ep.TLSConfig.BuildAssetPath(mon.Namespace, selector.Name, selector.Key)] = asset
			}

		}
	}

	return assets, nil
}

func LoadRemoteWriteSecrets(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client, l logr.Logger) (map[string]BasicAuthCredentials, map[string]BearerToken, error) {
	SecretsInNS := &corev1.SecretList{}
	err := rclient.List(ctx, SecretsInNS)
	if err != nil {
		l.Error(err, "cannot list secrets at vmagent namespace")
		return nil, nil, err
	}
	rwsBasicAuthSecrets, err := loadBasicAuthSecrets(ctx, rclient, nil, nil, cr.Spec.RemoteWrite, SecretsInNS)
	if err != nil {
		l.Error(err, "cannot load basic auth secrets for remote write specs")
		return nil, nil, err
	}

	rwsBearerTokens, err := loadBearerTokensFromSecrets(ctx, rclient, nil, cr.Spec.RemoteWrite, SecretsInNS)
	if err != nil {
		l.Error(err, "cannot get bearer tokens for remote write specs")
		return nil, nil, err
	}
	return rwsBasicAuthSecrets, rwsBearerTokens, nil
}

type remoteFlag struct {
	isNotNull   bool
	flagSetting string
}

func BuildRemoteWrites(cr *victoriametricsv1beta1.VMAgent, rwsBasicAuth map[string]BasicAuthCredentials, rwsTokens map[string]BearerToken) []string {
	var finalArgs []string
	var remoteArgs []remoteFlag
	remoteTargets := cr.Spec.RemoteWrite

	url := remoteFlag{flagSetting: "-remoteWrite.url=", isNotNull: true}
	authUser := remoteFlag{flagSetting: "-remoteWrite.basicAuth.username="}
	authPassword := remoteFlag{flagSetting: "-remoteWrite.basicAuth.password="}
	bearerToken := remoteFlag{flagSetting: "-remoteWrite.bearerToken="}
	flushInterval := remoteFlag{flagSetting: "-remoteWrite.flushInterval="}
	labels := remoteFlag{flagSetting: "-remoteWrite.label="}
	maxBlockSize := remoteFlag{flagSetting: "-remoteWrite.maxBlockSize="}
	maxDiskUsage := remoteFlag{flagSetting: "-remoteWrite.maxDiskUsagePerURL="}
	queues := remoteFlag{flagSetting: "-remoteWrite.queues="}
	urlRelabelConfig := remoteFlag{flagSetting: "-remoteWrite.urlRelabelConfig="}
	sendTimeout := remoteFlag{flagSetting: "-remoteWrite.sendTimeout="}
	showURL := remoteFlag{flagSetting: "-remoteWrite.showURL="}
	tmpDataPath := remoteFlag{flagSetting: "-remoteWrite.tmpDataPath="}
	tlsCAs := remoteFlag{flagSetting: "-remoteWrite.tlsCAFile="}
	tlsCerts := remoteFlag{flagSetting: "-remoteWrite.tlsCertFile="}
	tlsKeys := remoteFlag{flagSetting: "-remoteWrite.tlsKeyFile="}
	tlsInsecure := remoteFlag{flagSetting: "-remoteWrite.tlsInsecureSkipVerify="}
	tlsServerName := remoteFlag{flagSetting: "-remoteWrite.tlsServerName="}

	pathPrefix := path.Join(tlsAssetsDir, cr.Namespace)

	for _, rws := range remoteTargets {

		url.flagSetting += fmt.Sprintf("%s,", rws.URL)

		var caPath, certPath, keyPath, ServerName string
		var insecure bool
		if rws.TLSConfig != nil {
			if rws.TLSConfig.CAFile != "" {
				caPath = rws.TLSConfig.CAFile
			} else {
				caPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.CA.Name(), rws.TLSConfig.CA.Key())
			}
			tlsCAs.isNotNull = true
			if rws.TLSConfig.CertFile != "" {
				certPath = rws.TLSConfig.CertFile
			} else {
				certPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.Cert.Name(), rws.TLSConfig.Cert.Key())

			}
			tlsCerts.isNotNull = true
			if rws.TLSConfig.KeyFile != "" {
				keyPath = rws.TLSConfig.KeyFile
			} else {
				keyPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.KeySecret.Name, rws.TLSConfig.KeySecret.Key)
			}
			tlsKeys.isNotNull = true
			if rws.TLSConfig.InsecureSkipVerify {
				tlsInsecure.isNotNull = true
			}
			if rws.TLSConfig.ServerName != "" {
				ServerName = rws.TLSConfig.ServerName
				tlsServerName.isNotNull = true
			}
			insecure = rws.TLSConfig.InsecureSkipVerify
		}
		tlsCAs.flagSetting += fmt.Sprintf("%s,", caPath)
		tlsCerts.flagSetting += fmt.Sprintf("%s,", certPath)
		tlsKeys.flagSetting += fmt.Sprintf("%s,", keyPath)
		tlsServerName.flagSetting += fmt.Sprintf("%s,", ServerName)
		tlsInsecure.flagSetting += fmt.Sprintf("%v,", insecure)

		var user string
		var pass string
		if rws.BasicAuth != nil {
			if s, ok := rwsBasicAuth[fmt.Sprintf("remoteWriteSpec/%s", rws.URL)]; ok {
				authUser.isNotNull = true
				authPassword.isNotNull = true
				user = s.username
				pass = s.password
			}
		}
		authUser.flagSetting += fmt.Sprintf("\"%s\",", strings.Replace(user, `"`, `\"`, -1))
		authPassword.flagSetting += fmt.Sprintf("\"%s\",", strings.Replace(pass, `"`, `\"`, -1))

		var value string
		if rws.BearerTokenSecret != nil {
			if s, ok := rwsTokens[fmt.Sprintf("remoteWriteSpec/%s", rws.URL)]; ok {
				bearerToken.isNotNull = true
				value = string(s)
			}
		}
		bearerToken.flagSetting += fmt.Sprintf("\"%s\",", strings.Replace(value, `"`, `\"`, -1))

		value = ""
		if rws.FlushInterval != nil {
			flushInterval.isNotNull = true
			value = *rws.FlushInterval
		}
		flushInterval.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.Labels != nil {
			labels.isNotNull = true
			for n, v := range rws.Labels {
				value += fmt.Sprintf("%v=%v,", n, v)
			}
		}
		labels.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.MaxBlockSize != nil {
			maxBlockSize.isNotNull = true
			value = strconv.Itoa(int(*rws.MaxBlockSize))
		}
		maxBlockSize.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.MaxDiskUsagePerURL != nil {
			maxDiskUsage.isNotNull = true
			value = strconv.Itoa(int(*rws.MaxDiskUsagePerURL))
		}
		maxDiskUsage.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.Queues != nil {
			queues.isNotNull = true
			value = strconv.Itoa(int(*rws.Queues))
		}
		queues.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.UrlRelabelConfig != nil {
			urlRelabelConfig.isNotNull = true
			value = path.Join(ConfigMapsDir, rws.UrlRelabelConfig.Name, rws.UrlRelabelConfig.Key)
		}
		urlRelabelConfig.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.SendTimeout != nil {
			sendTimeout.isNotNull = true
			value = *rws.SendTimeout
		}
		sendTimeout.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.ShowURL != nil {
			showURL.isNotNull = true
			value = strconv.FormatBool(*rws.ShowURL)
		}
		showURL.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if rws.TmpDataPath != nil {
			tmpDataPath.isNotNull = true
			value = *rws.TmpDataPath
		}
		tmpDataPath.flagSetting += fmt.Sprintf("%s,", value)
	}
	remoteArgs = append(remoteArgs, url, authUser, authPassword, bearerToken, flushInterval, labels, maxBlockSize, maxDiskUsage, queues, urlRelabelConfig, sendTimeout, showURL, tmpDataPath)
	remoteArgs = append(remoteArgs, tlsServerName, tlsInsecure, tlsKeys, tlsCerts, tlsCAs)
	for _, remoteArgType := range remoteArgs {
		if remoteArgType.isNotNull {
			finalArgs = append(finalArgs, strings.TrimSuffix(remoteArgType.flagSetting, ","))
		}
	}
	return finalArgs
}
