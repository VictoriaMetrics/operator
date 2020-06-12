package factory

import (
	"context"
	"fmt"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
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
)

const (
	vmAgentConfigsDir  = "/etc/vmagent/configs"
	vmAgentSecretDir   = "/etc/vmagent/secrets"
	vmAgentConfDir     = "/etc/vmagent/config"
	vmAgentConOfOutDir = "/etc/vmagent/config_out"
)

func CreateOrUpdateVmAgentService(ctx context.Context, cr *victoriametricsv1beta1.VmAgent, rclient client.Client, c *conf.BaseOperatorConf) (*corev1.Service, error) {
	l := log.WithValues("recon.vm.service.name", cr.Name())
	NewService := newServiceVmAgent(cr, c)

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

func newServiceVmAgent(cr *victoriametricsv1beta1.VmAgent, c *conf.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VmAgentDefault.Port
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
func CreateOrUpdateVmAgent(ctx context.Context, cr *victoriametricsv1beta1.VmAgent, rclient client.Client, c *conf.BaseOperatorConf) (reconcile.Result, error) {
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
	l.Info("create or update vm agent deploy")
	newDeploy, err := newDeployForVmAgent(cr, c)
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

	podSpec, err := makeSpecForVmAgent(cr, c)
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

func makeSpecForVmAgent(cr *victoriametricsv1beta1.VmAgent, c *conf.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-promscrape.config=%s", path.Join(vmAgentConOfOutDir, configEnvsubstFilename)),
	}
	for _, remote := range cr.Spec.RemoteWrite {
		args = append(args, "-remoteWrite.url="+remote.URL)
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

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: configSecretName(cr.Name()),
				},
			},
		},
		{
			Name: "tls-assets",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.TLSAssetName(),
				},
			},
		},
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
		{
			Name:      "tls-assets",
			ReadOnly:  true,
			MountPath: tlsAssetsDir,
		},
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
		},
	}

	return vmAgentSpec, nil
}

//add ownership - it needs for object changing tracking
func addAddtionalScrapeConfigOwnership(cr *victoriametricsv1beta1.VmAgent, rclient client.Client, l logr.Logger) error {
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
		if owner.Name == cr.Name() {
			return nil
		}
	}
	secret.OwnerReferences = append(secret.OwnerReferences, metav1.OwnerReference{
		APIVersion:         cr.APIVersion,
		Kind:               cr.Kind,
		Name:               cr.Name(),
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

func CreateOrUpdateTlsAssets(ctx context.Context, cr *victoriametricsv1beta1.VmAgent, rclient client.Client) error {
	monitors, err := SelectServiceMonitors(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("cannot select service monitors for tls Assets: %w", err)
	}
	assets, err := loadTLSAssets(ctx, rclient, monitors)
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
			return fmt.Errorf("cannot get existing tls secret: %s, for vmagent: %s, err: %w", tlsAssetsSecret.Name, cr.Name(), err)
		}
		err := rclient.Create(ctx, tlsAssetsSecret)
		if err != nil {
			return fmt.Errorf("cannot create tls asset secret: %s for vmagent: %s, err :%w", tlsAssetsSecret.Name, cr.Name(), err)
		}
		log.Info("create new tls asset secret: %s, for vmagent: %s", tlsAssetsSecret.Name, cr.Name())
		return nil
	}
	for annotation, value := range currentAssetSecret.Annotations {
		tlsAssetsSecret.Annotations[annotation] = value
	}
	return rclient.Update(ctx, tlsAssetsSecret)
}

func loadTLSAssets(ctx context.Context, rclient client.Client, monitors map[string]*monitoringv1.ServiceMonitor) (map[string]string, error) {
	assets := map[string]string{}
	nsSecretCache := make(map[string]*corev1.Secret)
	nsConfigMapCache := make(map[string]*corev1.ConfigMap)

	for _, mon := range monitors {
		for _, ep := range mon.Spec.Endpoints {
			if ep.TLSConfig == nil {
				continue
			}

			prefix := mon.Namespace + "/"
			secretSelectors := map[string]*corev1.SecretKeySelector{}
			configMapSelectors := map[string]*corev1.ConfigMapKeySelector{}
			if ep.TLSConfig.CA != (monitoringv1.SecretOrConfigMap{}) {
				selectorKey := ep.TLSConfig.CA.BuildSelectorWithPrefix(prefix)
				switch {
				case ep.TLSConfig.CA.Secret != nil:
					secretSelectors[selectorKey] = ep.TLSConfig.CA.Secret
				case ep.TLSConfig.CA.ConfigMap != nil:
					configMapSelectors[selectorKey] = ep.TLSConfig.CA.ConfigMap
				}
			}
			if ep.TLSConfig.Cert != (monitoringv1.SecretOrConfigMap{}) {
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
						"failed to extract endpoint tls asset for servicemonitor %s from secret %s and key %s in namespace %s",
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
						"failed to extract endpoint tls asset for servicemonitor %v from configmap %v and key %v in namespace %v",
						mon.Name, selector.Name, selector.Key, mon.Namespace,
					)
				}

				assets[ep.TLSConfig.BuildAssetPath(mon.Namespace, selector.Name, selector.Key)] = asset
			}

		}
	}

	return assets, nil
}
