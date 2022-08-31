package factory

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/psp"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	vmAlertConfigDir = "/etc/vmalert/config"
)

func CreateOrUpdateVMAlertService(ctx context.Context, cr *victoriametricsv1beta1.VMAlert, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMAlertDefault.Port
	}
	additionalSvc := buildDefaultService(cr, cr.Spec.Port, nil)
	mergeServiceSpec(additionalSvc, cr.Spec.ServiceSpec)
	newService := buildDefaultService(cr, cr.Spec.Port, nil)

	// user may want to abuse it, if serviceSpec.name == crd.prefixedName,
	// log error?
	if cr.Spec.ServiceSpec != nil {
		if additionalSvc.Name == newService.Name {
			log.Error(fmt.Errorf("vmalert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, cr.PrefixedName()), "cannot create additional service")
		} else if _, err := reconcileServiceForCRD(ctx, rclient, additionalSvc); err != nil {
			return nil, err
		}
	}
	rca := finalize.RemoveSvcArgs{
		PrefixedName:   cr.PrefixedName,
		GetNameSpace:   cr.GetNamespace,
		SelectorLabels: cr.SelectorLabels,
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.ServiceSpec); err != nil {
		return nil, err
	}

	return reconcileServiceForCRD(ctx, rclient, newService)
}

func CreateOrUpdateVMAlert(ctx context.Context, cr *victoriametricsv1beta1.VMAlert, rclient client.Client, c *config.BaseOperatorConf, cmNames []string) (reconcile.Result, error) {
	l := log.WithValues("controller", "vmalert.crud", "vmalert", cr.Name)
	// copy to avoid side effects.
	cr = cr.DeepCopy()
	var additionalNotifiers []victoriametricsv1beta1.VMAlertNotifierSpec

	if cr.Spec.Notifier != nil {
		cr.Spec.Notifiers = append(cr.Spec.Notifiers, *cr.Spec.Notifier)
	}
	// trim notifiers with non-empty notifier Selector
	var cnt int
	for i := range cr.Spec.Notifiers {
		n := cr.Spec.Notifiers[i]
		// fast path
		if n.Selector == nil {
			cr.Spec.Notifiers[cnt] = n
			cnt++
			continue
		}
		// discover alertmanagers
		var ams victoriametricsv1beta1.VMAlertmanagerList
		amListOpts, err := n.Selector.AsListOptions()
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("cannot convert notifier selector as ListOptions: %w", err)
		}
		if err := rclient.List(ctx, &ams, amListOpts, config.MustGetNamespaceListOptions()); err != nil {
			return reconcile.Result{}, fmt.Errorf("cannot list alertmanagers for vmalert notifier sd: %w", err)
		}
		for _, item := range ams.Items {
			if !item.DeletionTimestamp.IsZero() && n.Selector.Namespace != nil && !n.Selector.Namespace.IsMatch(&item) {
				continue
			}
			dsc := item.AsNotifiers()
			additionalNotifiers = append(additionalNotifiers, dsc...)
		}
	}
	cr.Spec.Notifiers = cr.Spec.Notifiers[:cnt]

	if len(additionalNotifiers) > 0 {
		sort.Slice(additionalNotifiers, func(i, j int) bool {
			return additionalNotifiers[i].URL > additionalNotifiers[j].URL
		})
		l.Info("additional notifiers with sd selector", "len", len(additionalNotifiers))
	}
	cr.Spec.Notifiers = append(cr.Spec.Notifiers, additionalNotifiers...)
	if len(cr.Spec.Notifiers) == 0 && cr.Spec.NotifierConfigRef == nil {
		return reconcile.Result{}, fmt.Errorf("cannot create or update vmalert: %s, cannot find any notifiers. At cr.spec.Notifiers, cr.spec.Notifier, cr.spec.notifierConfigRef and discovered alertmanager are empty", cr.Name)
	}
	if err := psp.CreateServiceAccountForCRD(ctx, cr, rclient); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed create service account: %w", err)
	}
	if c.PSPAutoCreateEnabled {
		if err := psp.CreateOrUpdateServiceAccountWithPSP(ctx, cr, rclient); err != nil {
			return reconcile.Result{}, fmt.Errorf("cannot create podsecurity policy for vmalert, err=%w", err)
		}
	}
	remoteSecrets, err := loadVMAlertRemoteSecrets(ctx, rclient, cr)
	if err != nil {
		l.Error(err, "cannot get basic auth secretsInNs for vmalert")
		return reconcile.Result{}, err
	}

	if cr.Spec.PodDisruptionBudget != nil {
		if err := CreateOrUpdatePodDisruptionBudget(ctx, rclient, cr, cr.Kind, cr.Spec.PodDisruptionBudget); err != nil {
			return reconcile.Result{}, fmt.Errorf("cannot update pod disruption budget for vmalert: %w", err)
		}
	}

	err = CreateOrUpdateTlsAssetsForVMAlert(ctx, cr, rclient)
	if err != nil {
		return reconcile.Result{}, err
	}
	newDeploy, err := newDeployForVMAlert(cr, c, cmNames, remoteSecrets)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot generate new deploy for vmalert: %w", err)
	}

	currDeploy := &appsv1.Deployment{}
	err = rclient.Get(ctx, types.NamespacedName{Namespace: newDeploy.Namespace, Name: newDeploy.Name}, currDeploy)
	if err != nil {
		if errors.IsNotFound(err) {
			err := rclient.Create(ctx, newDeploy)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("cannot create vmalert deploy: %w", err)
			}
		} else {
			return reconcile.Result{}, fmt.Errorf("cannot get deploy for vmalert: %w", err)
		}
	}
	newDeploy.Annotations = labels.Merge(currDeploy.Annotations, newDeploy.Annotations)
	newDeploy.Finalizers = victoriametricsv1beta1.MergeFinalizers(currDeploy, victoriametricsv1beta1.FinalizerName)
	if err := rclient.Update(ctx, newDeploy); err != nil {
		return reconcile.Result{}, fmt.Errorf("cannot update vmalert deploy: %w", err)
	}
	return reconcile.Result{}, nil
}

// newDeployForCR returns a busybox pod with the same name/namespace as the cr
func newDeployForVMAlert(cr *victoriametricsv1beta1.VMAlert, c *config.BaseOperatorConf, ruleConfigMapNames []string, remoteSecrets map[string]BasicAuthCredentials) (*appsv1.Deployment, error) {

	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMAlertDefault.Image
	}
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMAlertDefault.Version
	}
	if cr.Spec.Image.PullPolicy == "" {
		cr.Spec.Image.PullPolicy = corev1.PullIfNotPresent
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
			Labels:          c.Labels.Merge(cr.AllLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: *generatedSpec,
	}
	return deploy, nil
}

func vmAlertSpecGen(cr *victoriametricsv1beta1.VMAlert, c *config.BaseOperatorConf, ruleConfigMapNames []string, remoteSecrets map[string]BasicAuthCredentials) (*appsv1.DeploymentSpec, error) {

	confReloadArgs := []string{
		fmt.Sprintf("-webhook-url=%s", cr.ReloadPathWithPort(cr.Spec.Port)),
	}
	for _, cm := range ruleConfigMapNames {
		confReloadArgs = append(confReloadArgs, fmt.Sprintf("-volume-dir=%s", path.Join(vmAlertConfigDir, cm)))
	}

	args := buildVMAlertArgs(cr, ruleConfigMapNames, remoteSecrets)

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
	if cr.Spec.NotifierConfigRef != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "vmalert-notifier-config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.Spec.NotifierConfigRef.Name,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "vmalert-notifier-config",
			MountPath: notifierConfigMountPath,
		})
	}
	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
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
	var reloaderVolumes []corev1.VolumeMount
	for _, name := range ruleConfigMapNames {
		reloaderVolumes = append(reloaderVolumes, corev1.VolumeMount{
			Name:      name,
			MountPath: path.Join(vmAlertConfigDir, name),
		})
	}

	resources := corev1.ResourceRequirements{Limits: corev1.ResourceList{}}
	if c.VMAlertDefault.ConfigReloaderCPU != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAlertDefault.ConfigReloaderCPU)
	}
	if c.VMAlertDefault.ConfigReloaderMemory != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAlertDefault.ConfigReloaderMemory)
	}

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})

	// sort for consistency
	sort.Strings(args)
	sort.Slice(volumes, func(i, j int) bool {
		return volumes[i].Name < volumes[j].Name
	})
	sort.Slice(volumeMounts, func(i, j int) bool {
		return volumeMounts[i].Name < volumeMounts[j].Name
	})
	sort.Slice(reloaderVolumes, func(i, j int) bool {
		return reloaderVolumes[i].Name < reloaderVolumes[j].Name
	})
	sort.Strings(confReloadArgs)

	vmalertContainer := corev1.Container{
		Args:                     args,
		Name:                     "vmalert",
		Image:                    fmt.Sprintf("%s:%s", formatContainerImage(c.ContainerRegistry, cr.Spec.Image.Repository), cr.Spec.Image.Tag),
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             volumeMounts,
		Resources:                buildResources(cr.Spec.Resources, config.Resource(c.VMAlertDefault.Resource), c.VMAlertDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	vmalertContainer = buildProbe(vmalertContainer, cr)

	vmalertContainers := []corev1.Container{vmalertContainer, {
		Name:                     "config-reloader",
		Image:                    fmt.Sprintf("%s", formatContainerImage(c.ContainerRegistry, c.VMAlertDefault.ConfigReloadImage)),
		Args:                     confReloadArgs,
		Resources:                resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts:             reloaderVolumes,
	},
	}

	containers, err := k8stools.MergePatchContainers(vmalertContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.UpdateStrategy
	}

	for i := range cr.Spec.TopologySpreadConstraints {
		if cr.Spec.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			}
		}
	}

	spec := &appsv1.DeploymentSpec{
		Replicas: cr.Spec.ReplicaCount,

		Selector: &metav1.LabelSelector{
			MatchLabels: cr.SelectorLabels(),
		},

		Strategy: appsv1.DeploymentStrategy{
			Type:          strategyType,
			RollingUpdate: cr.Spec.RollingUpdate,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      cr.PodLabels(),
				Annotations: cr.PodAnnotations(),
			},
			Spec: corev1.PodSpec{
				NodeSelector:                  cr.Spec.NodeSelector,
				SchedulerName:                 cr.Spec.SchedulerName,
				RuntimeClassName:              cr.Spec.RuntimeClassName,
				ServiceAccountName:            cr.GetServiceAccountName(),
				Containers:                    containers,
				Volumes:                       volumes,
				PriorityClassName:             cr.Spec.PriorityClassName,
				SecurityContext:               cr.Spec.SecurityContext,
				Affinity:                      cr.Spec.Affinity,
				Tolerations:                   cr.Spec.Tolerations,
				HostNetwork:                   cr.Spec.HostNetwork,
				DNSPolicy:                     cr.Spec.DNSPolicy,
				DNSConfig:                     cr.Spec.DNSConfig,
				TopologySpreadConstraints:     cr.Spec.TopologySpreadConstraints,
				TerminationGracePeriodSeconds: cr.Spec.TerminationGracePeriodSeconds,
				ReadinessGates:                cr.Spec.ReadinessGates,
			},
		},
	}
	return spec, nil
}

func buildHeadersArg(flagName string, src []string, headers []string) []string {
	if len(headers) == 0 {
		return src
	}
	var headerFlagValue string
	for _, headerKV := range headers {
		headerFlagValue += headerKV + "^^"
	}
	headerFlagValue = strings.TrimSuffix(headerFlagValue, "^^")
	src = append(src, fmt.Sprintf("--%s=%s", flagName, headerFlagValue))
	return src
}

func buildBearerArgs(args []string, flagPrefix string, tokenSpec *victoriametricsv1beta1.BearerAuth) []string {
	if tokenSpec == nil {
		return args
	}
	if len(tokenSpec.TokenFilePath) > 0 {
		args = append(args, fmt.Sprintf("-%s.bearerTokenFile=%s", flagPrefix, tokenSpec.TokenFilePath))
		return args
	}
	// todo implement it
	if tokenSpec.TokenSecret != nil {
		log.Info("tokenSecret is not supported atm, ignoring it for prefix: ", "prefix", flagPrefix)
	}
	return args
}

func buildVMAlertBasicAuthArgs(args []string, flagPrefix string, baSpec *victoriametricsv1beta1.BasicAuth, remoteSecrets map[string]BasicAuthCredentials) []string {
	if baSpec == nil {
		return args
	}
	if s, ok := remoteSecrets[flagPrefix]; ok {
		args = append(args, fmt.Sprintf("-%s.basicAuth.username=%s", flagPrefix, s.username))
		if len(s.password) > 0 {
			args = append(args, fmt.Sprintf("-%s.basicAuth.password=%s", flagPrefix, s.password))
		}
		if len(baSpec.PasswordFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.basicAuth.passwordFile=%s", flagPrefix, baSpec.PasswordFile))
		}
	}

	return args
}

// todo add oauth2
func buildVMAlertArgs(cr *victoriametricsv1beta1.VMAlert, ruleConfigMapNames []string, remoteSecrets map[string]BasicAuthCredentials) []string {
	args := []string{
		fmt.Sprintf("-datasource.url=%s", cr.Spec.Datasource.URL),
	}

	args = buildHeadersArg("datasource.headers", args, cr.Spec.Datasource.HTTPAuth.Headers)
	args = buildBearerArgs(args, "datasource", cr.Spec.Datasource.HTTPAuth.BearerAuth)
	args = append(args, BuildNotifiersArgs(cr, remoteSecrets)...)

	args = buildVMAlertBasicAuthArgs(args, "datasource", cr.Spec.Datasource.HTTPAuth.BasicAuth, remoteSecrets)

	if cr.Spec.Datasource.HTTPAuth.TLSConfig != nil {
		tlsConf := cr.Spec.Datasource.HTTPAuth.TLSConfig
		args = tlsConf.AsArgs(args, "datasource", cr.Namespace)
	}

	if cr.Spec.RemoteWrite != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.url=%s", cr.Spec.RemoteWrite.URL))
		args = buildVMAlertBasicAuthArgs(args, "remoteWrite", cr.Spec.RemoteWrite.HTTPAuth.BasicAuth, remoteSecrets)
		args = buildHeadersArg("remoteWrite.headers", args, cr.Spec.RemoteWrite.HTTPAuth.Headers)
		args = buildBearerArgs(args, "remoteWrite", cr.Spec.RemoteWrite.HTTPAuth.BearerAuth)
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
		if cr.Spec.RemoteWrite.HTTPAuth.TLSConfig != nil {
			tlsConf := cr.Spec.RemoteWrite.HTTPAuth.TLSConfig
			args = tlsConf.AsArgs(args, "remoteWrite", cr.Namespace)
		}
	}
	for k, v := range cr.Spec.ExternalLabels {
		args = append(args, fmt.Sprintf("-external.label=%s=%s", k, v))
	}

	if cr.Spec.RemoteRead != nil {
		args = append(args, fmt.Sprintf("-remoteRead.url=%s", cr.Spec.RemoteRead.URL))
		args = buildVMAlertBasicAuthArgs(args, "remoteRead", cr.Spec.RemoteRead.HTTPAuth.BasicAuth, remoteSecrets)
		args = buildBearerArgs(args, "remoteRead", cr.Spec.RemoteRead.HTTPAuth.BearerAuth)
		args = buildHeadersArg("remoteRead.headers", args, cr.Spec.RemoteRead.HTTPAuth.Headers)
		if cr.Spec.RemoteRead.Lookback != nil {
			args = append(args, fmt.Sprintf("-remoteRead.lookback=%s", *cr.Spec.RemoteRead.Lookback))
		}
		if cr.Spec.RemoteRead.HTTPAuth.TLSConfig != nil {
			tlsConf := cr.Spec.RemoteRead.HTTPAuth.TLSConfig
			args = tlsConf.AsArgs(args, "remoteRead", cr.Namespace)
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
		args = append(args, fmt.Sprintf("-rule=%q", path.Join(vmAlertConfigDir, cm, "*.yaml")))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	for _, rulePath := range cr.Spec.RulePath {
		args = append(args, fmt.Sprintf("-rule=%q", rulePath))
	}
	if len(cr.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	args = addExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	return args
}

func loadVMAlertRemoteSecrets(
	ctx context.Context,
	rclient client.Client,
	cr *victoriametricsv1beta1.VMAlert,
) (map[string]BasicAuthCredentials, error) {
	datasource := cr.Spec.Datasource
	remoteWrite := cr.Spec.RemoteWrite
	remoteRead := cr.Spec.RemoteRead
	secrets := map[string]BasicAuthCredentials{}
	for i, notifier := range cr.Spec.Notifiers {
		if notifier.HTTPAuth.BasicAuth != nil {
			credentials, err := loadBasicAuthSecret(ctx, rclient, cr.Namespace, notifier.HTTPAuth.BasicAuth)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for notifier config. %w", err)
			}
			secrets[cr.NotifierAsMapKey(i)] = credentials
		}
	}
	// load basic auth for datasource configuration
	if datasource.HTTPAuth.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(ctx, rclient, cr.Namespace, datasource.HTTPAuth.BasicAuth)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for datasource config. %w", err)
		}
		secrets["datasource"] = credentials
	}
	// load basic auth for remote write configuration
	if remoteWrite != nil && remoteWrite.HTTPAuth.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(ctx, rclient, cr.Namespace, remoteWrite.HTTPAuth.BasicAuth)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for VMAlert remote write config. %w", err)
		}
		secrets["remoteWrite"] = credentials
	}
	// load basic auth for remote write configuration
	if remoteRead != nil && remoteRead.HTTPAuth.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(ctx, rclient, cr.Namespace, remoteRead.HTTPAuth.BasicAuth)
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
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Data: map[string][]byte{},
	}

	for key, asset := range assets {
		tlsAssetsSecret.Data[key] = []byte(asset)
	}
	currentAssetSecret := &corev1.Secret{}
	err = rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: tlsAssetsSecret.Name}, currentAssetSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, tlsAssetsSecret)
		}
		return fmt.Errorf("cannot get existing tls secret: %s, for vmalert: %s, err: %w", tlsAssetsSecret.Name, cr.Name, err)
	}
	for annotation, value := range currentAssetSecret.Annotations {
		tlsAssetsSecret.Annotations[annotation] = value
	}
	tlsAssetsSecret.Annotations = labels.Merge(currentAssetSecret.Annotations, tlsAssetsSecret.Annotations)
	tlsAssetsSecret.Finalizers = victoriametricsv1beta1.MergeFinalizers(currentAssetSecret, victoriametricsv1beta1.FinalizerName)
	return rclient.Update(ctx, tlsAssetsSecret)
}

func loadTLSAssetsForVMAlert(ctx context.Context, rclient client.Client, cr *victoriametricsv1beta1.VMAlert) (map[string]string, error) {
	assets := map[string]string{}
	nsSecretCache := make(map[string]*corev1.Secret)
	nsConfigMapCache := make(map[string]*corev1.ConfigMap)
	tlsConfigs := []*victoriametricsv1beta1.TLSConfig{}

	for _, notifier := range cr.Spec.Notifiers {
		if notifier.HTTPAuth.TLSConfig != nil {
			tlsConfigs = append(tlsConfigs, notifier.HTTPAuth.TLSConfig)
		}
	}
	if cr.Spec.RemoteRead != nil && cr.Spec.RemoteRead.HTTPAuth.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.RemoteRead.HTTPAuth.TLSConfig)
	}
	if cr.Spec.RemoteWrite != nil && cr.Spec.RemoteWrite.HTTPAuth.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.RemoteWrite.HTTPAuth.TLSConfig)
	}
	if cr.Spec.Datasource.HTTPAuth.TLSConfig != nil {
		tlsConfigs = append(tlsConfigs, cr.Spec.Datasource.HTTPAuth.TLSConfig)
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
				selector,
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

const notifierConfigMountPath = `/etc/vm/notifier_config`

func BuildNotifiersArgs(cr *victoriametricsv1beta1.VMAlert, ntBasicAuth map[string]BasicAuthCredentials) []string {
	var finalArgs []string
	var notifierArgs []remoteFlag
	notifierTargets := cr.Spec.Notifiers

	if len(notifierTargets) == 0 && cr.Spec.NotifierConfigRef != nil {
		return append(finalArgs, fmt.Sprintf("-notifier.config=%s/%s", notifierConfigMountPath, cr.Spec.NotifierConfigRef.Key))
	}

	url := remoteFlag{flagSetting: "-notifier.url=", isNotNull: true}
	authUser := remoteFlag{flagSetting: "-notifier.basicAuth.username="}
	authPassword := remoteFlag{flagSetting: "-notifier.basicAuth.password="}
	tlsCAs := remoteFlag{flagSetting: "-notifier.tlsCAFile="}
	tlsCerts := remoteFlag{flagSetting: "-notifier.tlsCertFile="}
	tlsKeys := remoteFlag{flagSetting: "-notifier.tlsKeyFile="}
	tlsServerName := remoteFlag{flagSetting: "-notifier.tlsServerName="}
	tlsInSecure := remoteFlag{flagSetting: "-notifier.tlsInsecureSkipVerify="}
	headers := remoteFlag{flagSetting: "-notifier.headers="}
	bearerTokenPath := remoteFlag{flagSetting: "-notifier.bearerTokenFile="}

	pathPrefix := path.Join(tlsAssetsDir, cr.Namespace)

	for i, nt := range notifierTargets {

		url.flagSetting += fmt.Sprintf("%s,", nt.URL)

		var caPath, certPath, keyPath, ServerName string
		var inSecure bool
		ntTls := nt.HTTPAuth.TLSConfig
		if ntTls != nil {
			if ntTls.CAFile != "" {
				caPath = ntTls.CAFile
			} else if ntTls.CA.Name() != "" {
				caPath = ntTls.BuildAssetPath(pathPrefix, ntTls.CA.Name(), ntTls.CA.Key())
			}
			if caPath != "" {
				tlsCAs.isNotNull = true
			}
			if ntTls.CertFile != "" {
				certPath = ntTls.CertFile
			} else if ntTls.Cert.Name() != "" {
				certPath = ntTls.BuildAssetPath(pathPrefix, ntTls.Cert.Name(), ntTls.Cert.Key())
			}
			if certPath != "" {
				tlsCerts.isNotNull = true
			}
			if ntTls.KeyFile != "" {
				keyPath = ntTls.KeyFile
			} else if ntTls.KeySecret != nil {
				keyPath = ntTls.BuildAssetPath(pathPrefix, ntTls.KeySecret.Name, ntTls.KeySecret.Key)
			}
			if keyPath != "" {
				tlsKeys.isNotNull = true
			}
			if ntTls.InsecureSkipVerify {
				tlsInSecure.isNotNull = true
				inSecure = true
			}
			if ntTls.ServerName != "" {
				ServerName = ntTls.ServerName
				tlsServerName.isNotNull = true
			}
		}
		tlsCAs.flagSetting += fmt.Sprintf("%s,", caPath)
		tlsCerts.flagSetting += fmt.Sprintf("%s,", certPath)
		tlsKeys.flagSetting += fmt.Sprintf("%s,", keyPath)
		tlsServerName.flagSetting += fmt.Sprintf("%s,", ServerName)
		tlsInSecure.flagSetting += fmt.Sprintf("%v,", inSecure)
		var headerFlagValue string
		if len(nt.HTTPAuth.Headers) > 0 {
			for _, headerKV := range nt.HTTPAuth.Headers {
				headerFlagValue += headerKV + "^^"
			}
			headers.isNotNull = true
		}
		headerFlagValue = strings.TrimSuffix(headerFlagValue, "^^")
		headers.flagSetting += fmt.Sprintf("%s,", headerFlagValue)
		var user string
		var pass string
		if nt.HTTPAuth.BasicAuth != nil {
			if s, ok := ntBasicAuth[cr.NotifierAsMapKey(i)]; ok {
				authUser.isNotNull = true
				authPassword.isNotNull = true
				user = s.username
				pass = s.password
			}
		}
		var tokenPath string
		if nt.HTTPAuth.BearerAuth != nil {
			// todo implement token secret
			if len(nt.HTTPAuth.BearerAuth.TokenFilePath) > 0 {
				bearerTokenPath.isNotNull = true
				tokenPath = nt.HTTPAuth.BearerAuth.TokenFilePath
			}
		}
		bearerTokenPath.flagSetting += fmt.Sprintf("%s,", tokenPath)
		// todo implement oauth2

		authUser.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(user, `"`, `\"`))
		authPassword.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(pass, `"`, `\"`))

	}
	notifierArgs = append(notifierArgs, url, authUser, authPassword)
	notifierArgs = append(notifierArgs, tlsServerName, tlsKeys, tlsCerts, tlsCAs, tlsInSecure, headers, bearerTokenPath)

	for _, remoteArgType := range notifierArgs {
		if remoteArgType.isNotNull {
			finalArgs = append(finalArgs, strings.TrimSuffix(remoteArgType.flagSetting, ","))
		}
	}

	return finalArgs
}

func CreateOrUpdatePodDisruptionBudgetForVMAlert(ctx context.Context, cr *victoriametricsv1beta1.VMAlert, rclient client.Client) error {
	pdb := buildDefaultPDB(cr, cr.Spec.PodDisruptionBudget)
	return reconcilePDB(ctx, rclient, cr.Kind, pdb)
}
