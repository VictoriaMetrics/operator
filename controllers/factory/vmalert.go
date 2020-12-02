package factory

import (
	"context"
	"fmt"
	"path"
	"sort"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	vmAlertConfigDir = "/etc/vmalert/config"
)

func CreateOrUpdateVMAlertService(ctx context.Context, cr *victoriametricsv1beta1.VMAlert, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	l := log.WithValues("controller", "vmalert.service.crud", "vmalert", cr.Name)
	newService := newServiceVMAlert(cr, c)

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

func newServiceVMAlert(cr *victoriametricsv1beta1.VMAlert, c *config.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMAlertDefault.Port
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

func CreateOrUpdateVMAlert(ctx context.Context, cr *victoriametricsv1beta1.VMAlert, rclient client.Client, c *config.BaseOperatorConf, cmNames []string) (reconcile.Result, error) {
	l := log.WithValues("controller", "vmalert.crud", "vmalert", cr.Name)
	//recon deploy
	secretsInNs := &corev1.SecretList{}
	err := rclient.List(ctx, secretsInNs, &client.ListOptions{Namespace: cr.Namespace})
	if err != nil {
		l.Error(err, "cannot list secretsInNs at vmalert namespace")
		return reconcile.Result{}, err
	}
	remoteSecrets, err := loadVMAlertRemoteSecrets(cr, secretsInNs)
	if err != nil {
		l.Error(err, "cannot get basic auth secretsInNs for vmalert")
		return reconcile.Result{}, err
	}

	err = CreateOrUpdateTlsAssetsForVMAlert(ctx, cr, rclient)
	if err != nil {
		return reconcile.Result{}, err
	}
	l.Info("generating new deployment")
	newDeploy, err := newDeployForVMAlert(cr, c, cmNames, remoteSecrets)
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
func newDeployForVMAlert(cr *victoriametricsv1beta1.VMAlert, c *config.BaseOperatorConf, ruleConfigMapNames []string, remoteSecrets map[string]BasicAuthCredentials) (*appsv1.Deployment, error) {

	cr = cr.DeepCopy()
	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMAlertDefault.Image
	}
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMAgentDefault.Version
	}
	if cr.Spec.Image.PullPolicy == "" {
		cr.Spec.Image.PullPolicy = corev1.PullIfNotPresent
	}

	if cr.Spec.Resources.Requests == nil {
		cr.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.Resources.Limits == nil {
		cr.Spec.Resources.Limits = corev1.ResourceList{}

	}
	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.Resources.Limits[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.Resources.Requests[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if !cpuResourceIsSet {
		cr.Spec.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMAlertDefault.Resource.Request.Cpu)
		cr.Spec.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAlertDefault.Resource.Limit.Cpu)

	}
	if !memResourceIsSet {
		cr.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMAlertDefault.Resource.Request.Mem)
		cr.Spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAlertDefault.Resource.Limit.Mem)
	}

	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMAlertDefault.Port
	}

	generatedSpec, err := vmAlertSpecGen(cr, c, ruleConfigMapNames, remoteSecrets)
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

func vmAlertSpecGen(cr *victoriametricsv1beta1.VMAlert, c *config.BaseOperatorConf, ruleConfigMapNames []string, remoteSecrets map[string]BasicAuthCredentials) (*appsv1.DeploymentSpec, error) {
	cr = cr.DeepCopy()

	confReloadArgs := []string{
		fmt.Sprintf("-webhook-url=%s", cr.ReloadPathWithPort(cr.Spec.Port)),
	}
	args := []string{
		fmt.Sprintf("-notifier.url=%s", cr.Spec.Notifier.URL),
		fmt.Sprintf("-datasource.url=%s", cr.Spec.Datasource.URL),
	}
	if cr.Spec.Datasource.BasicAuth != nil {
		if s, ok := remoteSecrets["datasource"]; ok {
			args = append(args, fmt.Sprintf("-datasource.basicAuth.username=%s", s.username))
			args = append(args, fmt.Sprintf("-datasource.basicAuth.password=%s", s.password))
		}
		if cr.Spec.Datasource.TLSConfig != nil {
			tlsConf := cr.Spec.Datasource.TLSConfig
			if tlsConf.CAFile != "" {
				args = append(args, fmt.Sprintf("-datasource.tlsCAFile=%s", tlsConf.CAFile))
			} else {
				args = append(args, fmt.Sprintf("-datasource.tlsCAFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.CA.Name(), tlsConf.CA.Key())))
			}
			if tlsConf.CertFile != "" {
				args = append(args, fmt.Sprintf("-datasource.tlsCertFile=%s", tlsConf.CertFile))
			} else {
				args = append(args, fmt.Sprintf("-datasource.tlsCertFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.Cert.Name(), tlsConf.Cert.Key())))
			}
			if tlsConf.KeyFile != "" {
				args = append(args, fmt.Sprintf("-datasource.tlsKeyFile=%s", tlsConf.KeyFile))
			} else {
				args = append(args, fmt.Sprintf("-datasource.tlsKeyFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.KeySecret.Name, tlsConf.KeySecret.Key)))
			}
			args = append(args, fmt.Sprintf("-datasource.tlsServerName=%s", tlsConf.ServerName))
			args = append(args, fmt.Sprintf("-datasource.tlsInsecureSkipVerify=%v", tlsConf.InsecureSkipVerify))

		}
	}
	if cr.Spec.Notifier.BasicAuth != nil {
		if s, ok := remoteSecrets["notifier"]; ok {
			args = append(args, fmt.Sprintf("-notifier.basicAuth.username%s", s.username))
			args = append(args, fmt.Sprintf("-notifier.basicAuth.password=%s", s.password))
		}
	}
	if cr.Spec.Notifier.TLSConfig != nil {
		tlsConf := cr.Spec.Notifier.TLSConfig
		if tlsConf.CAFile != "" {
			args = append(args, fmt.Sprintf("-notifier.tlsCAFile=%s", tlsConf.CAFile))
		} else {
			args = append(args, fmt.Sprintf("-notifier.tlsCAFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.CA.Name(), tlsConf.CA.Key())))
		}
		if tlsConf.CertFile != "" {
			args = append(args, fmt.Sprintf("-notifier.tlsCertFile=%s", tlsConf.CertFile))
		} else {
			args = append(args, fmt.Sprintf("-notifier.tlsCertFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.Cert.Name(), tlsConf.Cert.Key())))
		}
		if tlsConf.KeyFile != "" {
			args = append(args, fmt.Sprintf("-notifier.tlsKeyFile=%s", tlsConf.KeyFile))
		} else {
			args = append(args, fmt.Sprintf("-notifier.tlsKeyFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.KeySecret.Name, tlsConf.KeySecret.Key)))
		}
		args = append(args, fmt.Sprintf("-notifier.tlsServerName=%s", tlsConf.ServerName))
		args = append(args, fmt.Sprintf("-notifier.tlsInsecureSkipVerify=%v", tlsConf.InsecureSkipVerify))

	}

	if cr.Spec.RemoteWrite != nil {
		//this param cannot be used until v1.35.5 vm release with flag breaking changes
		args = append(args, fmt.Sprintf("-remoteWrite.url=%s", cr.Spec.RemoteWrite.URL))
		if cr.Spec.RemoteWrite.BasicAuth != nil {
			if s, ok := remoteSecrets["remoteWrite"]; ok {
				args = append(args, fmt.Sprintf("-remoteWrite.basicAuth.username=%s", s.username))
				args = append(args, fmt.Sprintf("-remoteWrite.basicAuth.password=%s", s.password))
			}
		}
		if cr.Spec.RemoteWrite.Concurrency != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.concurrency=%d", *cr.Spec.RemoteWrite.Concurrency))
		}
		if cr.Spec.RemoteWrite.FlushInterval != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.flushInterval=%s", *cr.Spec.RemoteWrite.FlushInterval))
		}
		if cr.Spec.RemoteWrite.MaxBatchSize != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.maxBatchSize=%d", *cr.Spec.RemoteWrite.MaxBatchSize))
		}
		if cr.Spec.RemoteWrite.MaxQueueSize != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.maxQueueSize=%d", *cr.Spec.RemoteWrite.MaxQueueSize))
		}
		if cr.Spec.RemoteWrite.TLSConfig != nil {
			tlsConf := cr.Spec.RemoteWrite.TLSConfig
			if tlsConf.CAFile != "" {
				args = append(args, fmt.Sprintf("-remoteWrite.tlsCAFile=%s", tlsConf.CAFile))
			} else {
				args = append(args, fmt.Sprintf("-remoteWrite.tlsCAFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.CA.Name(), tlsConf.CA.Key())))
			}
			if tlsConf.CertFile != "" {
				args = append(args, fmt.Sprintf("-remoteWrite.tlsCertFile=%s", tlsConf.CertFile))
			} else {
				args = append(args, fmt.Sprintf("-remoteWrite.tlsCertFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.Cert.Name(), tlsConf.Cert.Key())))
			}
			if tlsConf.KeyFile != "" {
				args = append(args, fmt.Sprintf("-remoteWrite.tlsKeyFile=%s", tlsConf.KeyFile))
			} else {
				args = append(args, fmt.Sprintf("-remoteWrite.tlsKeyFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.KeySecret.Name, tlsConf.KeySecret.Key)))
			}
			args = append(args, fmt.Sprintf("-remoteWrite.tlsServerName=%s", tlsConf.ServerName))
			args = append(args, fmt.Sprintf("-remoteWrite.tlsInsecureSkipVerify=%v", tlsConf.InsecureSkipVerify))

		}

	}
	if cr.Spec.RemoteRead != nil {
		args = append(args, fmt.Sprintf("-remoteRead.url=%s", cr.Spec.RemoteRead.URL))
		if cr.Spec.RemoteRead.BasicAuth != nil {
			if s, ok := remoteSecrets["remoteRead"]; ok {
				args = append(args, fmt.Sprintf("-remoteRead.basicAuth.username=%s", s.username))
				args = append(args, fmt.Sprintf("-remoteRead.basicAuth.password=%s", s.password))
			}
		}
		if cr.Spec.RemoteRead.Lookback != nil {
			args = append(args, fmt.Sprintf("-remoteRead.lookback=%s", *cr.Spec.RemoteRead.Lookback))
		}
		if cr.Spec.RemoteRead.TLSConfig != nil {
			tlsConf := cr.Spec.RemoteRead.TLSConfig
			if tlsConf.CAFile != "" {
				args = append(args, fmt.Sprintf("-remoteRead.tlsCAFile=%s", tlsConf.CAFile))
			} else {
				args = append(args, fmt.Sprintf("-remoteRead.tlsCAFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.CA.Name(), tlsConf.CA.Key())))
			}
			if tlsConf.CertFile != "" {
				args = append(args, fmt.Sprintf("-remoteRead.tlsCertFile=%s", tlsConf.CertFile))
			} else {
				args = append(args, fmt.Sprintf("-remoteRead.tlsCertFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.Cert.Name(), tlsConf.Cert.Key())))
			}
			if tlsConf.KeyFile != "" {
				args = append(args, fmt.Sprintf("-remoteRead.tlsKeyFile=%s", tlsConf.KeyFile))
			} else {
				args = append(args, fmt.Sprintf("-remoteRead.tlsKeyFile=%s", cr.Spec.Datasource.TLSConfig.BuildAssetPath(cr.Namespace, tlsConf.KeySecret.Name, tlsConf.KeySecret.Key)))
			}
			args = append(args, fmt.Sprintf("-remoteRead.tlsServerName=%s", tlsConf.ServerName))
			args = append(args, fmt.Sprintf("-remoteRead.tlsInsecureSkipVerify=%v", tlsConf.InsecureSkipVerify))

		}

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
	if len(cr.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.ExtraEnvs...)

	var volumes []corev1.Volume
	volumes = append(volumes, cr.Spec.Volumes...)

	volumes = append(volumes, corev1.Volume{
		Name: "tls-assets",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: cr.TLSAssetName(),
			},
		},
	})

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

	var volumeMounts []corev1.VolumeMount
	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
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
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
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
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

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
	if c.VMAlertDefault.ConfigReloaderCPU != "0" {
		resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAlertDefault.ConfigReloaderCPU)
	}
	if c.VMAlertDefault.ConfigReloaderMemory != "0" {
		resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAlertDefault.ConfigReloaderMemory)
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

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})

	sort.Strings(args)
	defaultContainers := []corev1.Container{
		{
			Args:                     args,
			Name:                     "vmalert",
			Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
			ImagePullPolicy:          cr.Spec.Image.PullPolicy,
			Ports:                    ports,
			VolumeMounts:             volumeMounts,
			LivenessProbe:            livenessProbe,
			ReadinessProbe:           readinessProbe,
			Resources:                cr.Spec.Resources,
			Env:                      envs,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		}, {
			Name:                     "config-reloader",
			Image:                    c.VMAlertDefault.ConfigReloadImage,
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
			MatchLabels: cr.SelectorLabels(),
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
				Containers:                containers,
				Volumes:                   volumes,
				SecurityContext:           cr.Spec.SecurityContext,
				Affinity:                  cr.Spec.Affinity,
				Tolerations:               cr.Spec.Tolerations,
				HostNetwork:               cr.Spec.HostNetwork,
				DNSPolicy:                 cr.Spec.DNSPolicy,
				TopologySpreadConstraints: cr.Spec.TopologySpreadConstraints,
			},
		},
	}
	return spec, nil
}

func loadVMAlertRemoteSecrets(
	cr *victoriametricsv1beta1.VMAlert,
	SecretsInNS *corev1.SecretList,
) (map[string]BasicAuthCredentials, error) {
	datasource := cr.Spec.Datasource
	remoteWrite := cr.Spec.RemoteWrite
	remoteRead := cr.Spec.RemoteRead
	notifier := cr.Spec.Notifier
	secrets := map[string]BasicAuthCredentials{}
	if notifier.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(notifier.BasicAuth, SecretsInNS)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for notifier config. %w", err)
		}
		secrets["notifier"] = credentials
	}
	// load basic auth for datasource configuration
	if datasource.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(datasource.BasicAuth, SecretsInNS)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for datasource config. %w", err)
		}
		secrets["datasource"] = credentials
	}
	// load basic auth for remote write configuration
	if remoteWrite != nil && remoteWrite.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(remoteWrite.BasicAuth, SecretsInNS)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for VMAlert remote write config. %w", err)
		}
		secrets["remoteWrite"] = credentials
	}
	// load basic auth for remote write configuration
	if remoteRead != nil && remoteRead.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(remoteRead.BasicAuth, SecretsInNS)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for VMAlert remote read config. %w", err)
		}
		secrets["remoteRead"] = credentials
	}
	return secrets, nil
}

func CreateOrUpdateTlsAssetsForVMAlert(ctx context.Context, cr *victoriametricsv1beta1.VMAlert, rclient client.Client) error {
	assets, err := loadTLSAssetsForVMAlert(ctx, rclient, cr)
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

func loadTLSAssetsForVMAlert(ctx context.Context, rclient client.Client, cr *victoriametricsv1beta1.VMAlert) (map[string]string, error) {
	assets := map[string]string{}
	nsSecretCache := make(map[string]*corev1.Secret)
	nsConfigMapCache := make(map[string]*corev1.ConfigMap)
	tlsConfigs := []*victoriametricsv1beta1.TLSConfig{}
	if cr.Spec.Notifier.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.Notifier.TLSConfig)
	}
	if cr.Spec.RemoteRead != nil && cr.Spec.RemoteRead.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.RemoteRead.TLSConfig)
	}
	if cr.Spec.RemoteWrite != nil && cr.Spec.RemoteWrite.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.RemoteWrite.TLSConfig)
	}
	if cr.Spec.Datasource.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.Datasource.TLSConfig)
	}

	for _, rw := range tlsConfigs {
		prefix := cr.Namespace + "/"
		secretSelectors := map[string]*corev1.SecretKeySelector{}
		configMapSelectors := map[string]*corev1.ConfigMapKeySelector{}
		selectorKey := rw.CA.BuildSelectorWithPrefix(prefix)
		switch {
		case rw.CA.Secret != nil:
			secretSelectors[selectorKey] = rw.CA.Secret
		case rw.CA.ConfigMap != nil:
			configMapSelectors[selectorKey] = rw.CA.ConfigMap
		}
		selectorKey = rw.Cert.BuildSelectorWithPrefix(prefix)

		switch {
		case rw.Cert.Secret != nil:
			secretSelectors[selectorKey] = rw.Cert.Secret

		case rw.Cert.ConfigMap != nil:
			configMapSelectors[selectorKey] = rw.Cert.ConfigMap
		}
		if rw.KeySecret != nil {
			secretSelectors[prefix+rw.KeySecret.Name+"/"+rw.KeySecret.Key] = rw.KeySecret
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
					"failed to extract endpoint tls asset for vmservicescrape %s from secret %s and key %s in namespace %s",
					cr.Name, selector.Name, selector.Key, cr.Namespace,
				)
			}

			assets[rw.BuildAssetPath(cr.Namespace, selector.Name, selector.Key)] = asset
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
			assets[rw.BuildAssetPath(cr.Namespace, selector.Name, selector.Key)] = asset
		}
	}

	return assets, nil
}
