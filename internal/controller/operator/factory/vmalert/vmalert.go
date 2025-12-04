package vmalert

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	vmAlertConfigDir        = "/etc/vmalert/config"
	datasourceKey           = "datasource"
	remoteReadKey           = "remoteRead"
	remoteWriteKey          = "remoteWrite"
	notifierConfigMountPath = `/etc/vm/notifier_config`
	vmalertConfigSecretsDir = "/etc/vmalert/remote_secrets"
	tlsAssetsDir            = "/etc/vmalert-tls/certs"
)

// createOrUpdateService creates service for vmalert
func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlert) (*corev1.Service, error) {

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, nil)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.ServiceSpec)
	}

	newService := build.Service(cr, cr.Spec.Port, nil)

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(newService, s)
		if additionalSvc.Name == newService.Name {
			return fmt.Errorf("vmalert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, cr.PrefixedName())
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmalert: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmalert: %w", err)
	}
	return newService, nil
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.SecretConfigResourceKind: {
			MountDir:   vmalertConfigSecretsDir,
			SecretName: build.ResourceName(build.SecretConfigResourceKind, cr),
		},
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}

// CreateOrUpdate creates vmalert deployment for given CRD
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMAlert, rclient client.Client, cmNames []string) error {
	cfg := config.MustGetBaseConfig()
	var prevCR *vmv1beta1.VMAlert
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
		if err := discoverNotifiersIfNeeded(ctx, rclient, prevCR); err != nil {
			return fmt.Errorf("cannot discover notifiers for prev spec: %w", err)
		}
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete objects from previous state: %w", err)
		}
	}
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}
	if err := discoverNotifiersIfNeeded(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot discover notifiers for new spec: %w", err)
	}

	ac := getAssetsCache(ctx, rclient, cr)

	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, disableSelfScrape) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		if err != nil {
			return fmt.Errorf("cannot create vmservicescrape: %w", err)
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB); err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vmalert: %w", err)
		}
	}

	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		prevDeploy, err = newDeploy(prevCR, cmNames, ac)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}

	newDeploy, err := newDeploy(cr, cmNames, ac)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vmalert: %w", err)
	}

	err = createOrUpdateAssets(ctx, rclient, cr, prevCR, ac)
	if err != nil {
		return err
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false)
}

// newDeploy returns a busybox pod with the same name/namespace as the cr
func newDeploy(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, ac *build.AssetsCache) (*appsv1.Deployment, error) {

	generatedSpec, err := newPodSpec(cr, ruleConfigMapNames, ac)
	if err != nil {
		return nil, fmt.Errorf("cannot generate new spec for vmalert: %w", err)
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: *generatedSpec,
	}
	cfg := config.MustGetBaseConfig()
	build.DeploymentAddCommonParams(deploy, ptr.Deref(cr.Spec.UseStrictSecurity, cfg.EnableStrictSecurity), &cr.Spec.CommonApplicationDeploymentParams)
	return deploy, nil
}

func newPodSpec(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, ac *build.AssetsCache) (*appsv1.DeploymentSpec, error) {
	args, err := buildArgs(cr, ruleConfigMapNames, ac)
	if err != nil {
		return nil, err
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.ExtraEnvs...)

	var volumes []corev1.Volume
	volumes = append(volumes, cr.Spec.Volumes...)

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
	var configReloaderWatchMounts []corev1.VolumeMount

	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)

	volumes, volumeMounts = build.LicenseVolumeTo(volumes, volumeMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	volumes, volumeMounts = ac.VolumeTo(volumes, volumeMounts)

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
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
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
		vm := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		}
		volumeMounts = append(volumeMounts, vm)
		configReloaderWatchMounts = append(configReloaderWatchMounts, vm)
	}

	for _, name := range ruleConfigMapNames {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      name,
			MountPath: path.Join(vmAlertConfigDir, name),
		})
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

	var vmalertContainers []corev1.Container

	vmalertContainer := corev1.Container{
		Args:                     args,
		Name:                     "vmalert",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             volumeMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	vmalertContainer = build.Probe(vmalertContainer, cr)
	build.AddConfigReloadAuthKeyToApp(&vmalertContainer, cr.Spec.ExtraArgs, &cr.Spec.CommonConfigReloaderParams)
	vmalertContainers = append(vmalertContainers, vmalertContainer)

	if !cr.IsUnmanaged() {
		crc := buildConfigReloaderContainer(cr, ruleConfigMapNames, configReloaderWatchMounts)
		vmalertContainers = append(vmalertContainers, crc)
	}

	cfg := config.MustGetBaseConfig()
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, cfg.EnableStrictSecurity)

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, vmalertContainers, useStrictSecurity)
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
	volumes = build.AddConfigReloadAuthKeyVolume(volumes, &cr.Spec.CommonConfigReloaderParams)

	spec := &appsv1.DeploymentSpec{

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
				ServiceAccountName: cr.GetServiceAccountName(),
				InitContainers:     cr.Spec.InitContainers,
				Containers:         containers,
				Volumes:            volumes,
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

func buildAuthArgs(args []string, namespace, flagPrefix string, cfg vmv1beta1.HTTPAuth, ac *build.AssetsCache) ([]string, error) {
	// safety checks must be performed by previous code
	if cfg.BasicAuth != nil {
		if len(cfg.BasicAuth.PasswordFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.basicAuth.passwordFile=%s", flagPrefix, cfg.BasicAuth.PasswordFile))
		} else {
			file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, namespace, &cfg.BasicAuth.Password)
			if err != nil {
				return nil, err
			}
			args = append(args, fmt.Sprintf("-%s.basicAuth.passwordFile=%s", flagPrefix, file))
		}
		secret, err := ac.LoadKeyFromSecret(namespace, &cfg.BasicAuth.Username)
		if err != nil {
			return nil, err
		}
		args = append(args, fmt.Sprintf("-%s.basicAuth.username=%s", flagPrefix, secret))
	}
	if cfg.TLSConfig != nil {
		creds, err := ac.BuildTLSCreds(namespace, cfg.TLSConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load notifier TLS credentials: %w", err)
		}
		if len(creds.CAFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.tlsCAFile=%s", flagPrefix, creds.CAFile))
		}
		if len(creds.CertFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.tlsCertFile=%s", flagPrefix, creds.CertFile))
		}
		if len(creds.KeyFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.tlsKeyFile=%s", flagPrefix, creds.KeyFile))
		}
		if cfg.TLSConfig.ServerName != "" {
			args = append(args, fmt.Sprintf("-%s.tlsServerName=%s", flagPrefix, cfg.TLSConfig.ServerName))
		}
		if cfg.TLSConfig.InsecureSkipVerify {
			args = append(args, fmt.Sprintf("-%s.tlsInsecureSkipVerify=%v", flagPrefix, cfg.TLSConfig.InsecureSkipVerify))
		}
	}
	if cfg.BearerAuth != nil {
		if cfg.TokenSecret != nil {
			file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, namespace, cfg.TokenSecret)
			if err != nil {
				return nil, err
			}
			args = append(args, fmt.Sprintf("-%s.bearerTokenFile=%s", flagPrefix, file))
		} else if len(cfg.TokenFilePath) > 0 {
			args = append(args, fmt.Sprintf("-%s.bearerTokenFile=%s", flagPrefix, cfg.TokenFilePath))
		}
	}
	if cfg.OAuth2 != nil {
		if cfg.OAuth2.ClientSecret != nil {
			file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, namespace, cfg.OAuth2.ClientSecret)
			if err != nil {
				return nil, err
			}
			args = append(args, fmt.Sprintf("-%s.oauth2.clientSecretFile=%s", flagPrefix, file))
		}
		if len(cfg.OAuth2.ClientSecretFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.oauth2.clientSecretFile=%s", flagPrefix, cfg.OAuth2.ClientSecretFile))
		}
		if len(cfg.OAuth2.ClientID.PrefixedName()) > 0 {
			secret, err := ac.LoadKeyFromSecretOrConfigMap(namespace, &cfg.OAuth2.ClientID)
			if err != nil {
				return nil, err
			}
			args = append(args, fmt.Sprintf("-%s.oauth2.clientID=%s", flagPrefix, secret))
		}
		args = append(args, fmt.Sprintf("-%s.oauth2.tokenUrl=%s", flagPrefix, cfg.OAuth2.TokenURL))
		args = append(args, fmt.Sprintf("-%s.oauth2.scopes=%s", flagPrefix, strings.Join(cfg.OAuth2.Scopes, ",")))
	}
	return args, nil
}

func buildArgs(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, ac *build.AssetsCache) ([]string, error) {
	args := []string{
		fmt.Sprintf("-datasource.url=%s", cr.Spec.Datasource.URL),
	}

	args = buildHeadersArg("datasource.headers", args, cr.Spec.Datasource.Headers)
	notifierArgs, err := buildNotifiersArgs(cr, ac)
	if err != nil {
		return nil, err
	}
	args = append(args, notifierArgs...)
	args, err = buildAuthArgs(args, cr.Namespace, datasourceKey, cr.Spec.Datasource.HTTPAuth, ac)
	if err != nil {
		return nil, err
	}

	if cr.Spec.RemoteWrite != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.url=%s", cr.Spec.RemoteWrite.URL))
		args, err = buildAuthArgs(args, cr.Namespace, remoteWriteKey, cr.Spec.RemoteWrite.HTTPAuth, ac)
		if err != nil {
			return nil, err
		}
		args = buildHeadersArg("remoteWrite.headers", args, cr.Spec.RemoteWrite.Headers)
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
	}
	for k, v := range cr.Spec.ExternalLabels {
		args = append(args, fmt.Sprintf("-external.label=%s=%s", k, v))
	}

	if cr.Spec.RemoteRead != nil {
		args = append(args, fmt.Sprintf("-remoteRead.url=%s", cr.Spec.RemoteRead.URL))
		args, err = buildAuthArgs(args, cr.Namespace, remoteReadKey, cr.Spec.RemoteRead.HTTPAuth, ac)
		if err != nil {
			return nil, err
		}
		args = buildHeadersArg("remoteRead.headers", args, cr.Spec.RemoteRead.Headers)
		if cr.Spec.RemoteRead.Lookback != nil {
			args = append(args, fmt.Sprintf("-remoteRead.lookback=%s", *cr.Spec.RemoteRead.Lookback))
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

	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}

	for _, rulePath := range cr.Spec.RulePath {
		args = append(args, fmt.Sprintf("-rule=%q", rulePath))
	}
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	return args, nil
}

func createOrUpdateAssets(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlert, ac *build.AssetsCache) error {
	for kind, secret := range ac.GetOutput() {
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		var prevSecretMeta *metav1.ObjectMeta
		if prevCR != nil {
			prevSecretMeta = ptr.To(build.ResourceMeta(kind, prevCR))
		}
		err := reconcile.Secret(ctx, rclient, &secret, prevSecretMeta)
		if err != nil {
			return err
		}
	}
	return nil
}

func buildNotifiersArgs(cr *vmv1beta1.VMAlert, ac *build.AssetsCache) ([]string, error) {
	var args []string
	notifierTargets := cr.Spec.Notifiers

	if _, ok := cr.Spec.ExtraArgs["notifier.blackhole"]; ok {
		// notifier.blackhole disables sending notifications completely, so we don't need to add any notifier args
		// also no need to add notifier.blackhole to args as it will be added with ExtraArgs
		return args, nil
	}

	if len(notifierTargets) == 0 && cr.Spec.NotifierConfigRef != nil {
		return append(args, fmt.Sprintf("-notifier.config=%s/%s", notifierConfigMountPath, cr.Spec.NotifierConfigRef.Key)), nil
	}

	url := build.NewFlag("-notifier.url", "")
	authUser := build.NewFlag("-notifier.basicAuth.username", `""`)
	authPasswordFile := build.NewFlag("-notifier.basicAuth.passwordFile", "")
	tlsCAs := build.NewFlag("-notifier.tlsCAFile", "")
	tlsCerts := build.NewFlag("-notifier.tlsCertFile", "")
	tlsKeys := build.NewFlag("-notifier.tlsKeyFile", "")
	tlsServerName := build.NewFlag("-notifier.tlsServerName", "")
	tlsInsecure := build.NewFlag("-notifier.tlsInsecureSkipVerify", "false")
	headers := build.NewFlag("-notifier.headers", "")
	bearerTokenPath := build.NewFlag("-notifier.bearerTokenFile", "")
	oauth2SecretFile := build.NewFlag("-notifier.oauth2.clientSecretFile", "")
	oauth2ClientID := build.NewFlag("-notifier.oauth2.clientID", "")
	oauth2Scopes := build.NewFlag("-notifier.oauth2.scopes", "")
	oauth2TokenURL := build.NewFlag("-notifier.oauth2.tokenUrl", "")

	for i, nt := range notifierTargets {
		url.Add(nt.URL, i)
		ntTLS := nt.TLSConfig
		if ntTLS != nil {
			creds, err := ac.BuildTLSCreds(cr.Namespace, ntTLS)
			if err != nil {
				return nil, fmt.Errorf("failed to load notifier TLS credentials: %w", err)
			}
			if creds.CAFile != "" {
				tlsCAs.Add(creds.CAFile, i)
			}
			if creds.CertFile != "" {
				tlsCerts.Add(creds.CertFile, i)
			}
			if creds.KeyFile != "" {
				tlsKeys.Add(creds.KeyFile, i)
			}
			tlsInsecure.Add(strconv.FormatBool(ntTLS.InsecureSkipVerify), i)
			if ntTLS.ServerName != "" {
				tlsServerName.Add(ntTLS.ServerName, i)
			}
		}
		if len(nt.Headers) > 0 {
			var value string
			for _, headerKV := range nt.Headers {
				value += headerKV + "^^"
			}
			headers.Add(strings.TrimSuffix(value, "^^"), i)
		}
		if nt.BasicAuth != nil {
			if len(nt.BasicAuth.PasswordFile) > 0 {
				authPasswordFile.Add(nt.BasicAuth.PasswordFile, i)
			} else {
				file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, &nt.BasicAuth.Password)
				if err != nil {
					return nil, err
				}
				authPasswordFile.Add(file, i)
			}
			secret, err := ac.LoadKeyFromSecret(cr.Namespace, &nt.BasicAuth.Username)
			if err != nil {
				return nil, err
			}
			authUser.Add(strconv.Quote(secret), i)
		}
		if nt.BearerAuth != nil {
			if nt.TokenSecret != nil {
				file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, nt.TokenSecret)
				if err != nil {
					return nil, err
				}
				bearerTokenPath.Add(file, i)
			}
			if len(nt.TokenFilePath) > 0 {
				bearerTokenPath.Add(nt.TokenFilePath, i)
			}
		}
		if nt.OAuth2 != nil {
			if nt.OAuth2.ClientSecret != nil {
				file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, nt.OAuth2.ClientSecret)
				if err != nil {
					return nil, err
				}
				oauth2SecretFile.Add(file, i)
			} else {
				oauth2SecretFile.Add(nt.OAuth2.ClientSecretFile, i)
			}
			if len(nt.OAuth2.Scopes) > 0 {
				oauth2Scopes.Add(strings.Join(nt.OAuth2.Scopes, ","), i)
			}
			if len(nt.OAuth2.TokenURL) > 0 {
				oauth2TokenURL.Add(nt.OAuth2.TokenURL, i)
			}
			if len(nt.OAuth2.ClientID.PrefixedName()) > 0 {
				secret, err := ac.LoadKeyFromSecretOrConfigMap(cr.Namespace, &nt.OAuth2.ClientID)
				if err != nil {
					return nil, err
				}
				oauth2ClientID.Add(secret, i)
			}
		}
	}
	if !url.IsSet() {
		args = append(args, "-notifier.url=")
	}
	totalCount := len(notifierTargets)
	args = build.AppendFlagsToArgs(args, totalCount, url, authUser, authPasswordFile)
	args = build.AppendFlagsToArgs(args, totalCount, tlsServerName, tlsKeys, tlsCerts, tlsCAs, tlsInsecure, headers, bearerTokenPath)
	args = build.AppendFlagsToArgs(args, totalCount, oauth2SecretFile, oauth2ClientID, oauth2Scopes, oauth2TokenURL)
	return args, nil
}

func buildConfigReloaderContainer(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, extraWatchVolumeMounts []corev1.VolumeMount) corev1.Container {

	var args []string
	volumeWatchArg := "-volume-dir"
	reloadURLArg := "-webhook-url"

	cfg := config.MustGetBaseConfig()
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, cfg.UseVMConfigReloader)
	if useVMConfigReloader {
		volumeWatchArg = "--watched-dir"
		reloadURLArg = "--reload-url"
		if cfg.EnableTCP6 {
			args = append(args, "--enableTCP6")
		}
	}
	args = append(args, fmt.Sprintf("%s=%s", reloadURLArg, vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)))
	for _, cm := range ruleConfigMapNames {
		args = append(args, fmt.Sprintf("%s=%s", volumeWatchArg, path.Join(vmAlertConfigDir, cm)))
	}
	for _, wm := range extraWatchVolumeMounts {
		args = append(args, fmt.Sprintf("%s=%s", volumeWatchArg, wm.MountPath))
	}
	if len(cr.Spec.ConfigReloaderExtraArgs) > 0 {
		newArgs := args[:0]
		for _, arg := range args {
			argName := strings.Split(strings.TrimLeft(arg, "-"), "=")[0]
			if _, ok := cr.Spec.ConfigReloaderExtraArgs[argName]; !ok {
				newArgs = append(newArgs, arg)
			}
		}
		for k, v := range cr.Spec.ConfigReloaderExtraArgs {
			newArgs = append(newArgs, fmt.Sprintf(`--%s=%s`, k, v))
		}
		sort.Strings(newArgs)
		args = newArgs
	}
	var reloaderVolumes []corev1.VolumeMount
	for _, name := range ruleConfigMapNames {
		reloaderVolumes = append(reloaderVolumes, corev1.VolumeMount{
			Name:      name,
			MountPath: path.Join(vmAlertConfigDir, name),
		})
	}
	reloaderVolumes = append(reloaderVolumes, extraWatchVolumeMounts...)
	sort.Slice(reloaderVolumes, func(i, j int) bool {
		return reloaderVolumes[i].Name < reloaderVolumes[j].Name
	})
	configReloaderContainer := corev1.Container{
		Name:                     "config-reloader",
		Image:                    cr.Spec.ConfigReloaderImageTag,
		Args:                     args,
		Resources:                cr.Spec.ConfigReloaderResources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts:             reloaderVolumes,
	}
	if useVMConfigReloader {
		build.AddsPortProbesToConfigReloaderContainer(useVMConfigReloader, &configReloaderContainer)
	}
	build.AddConfigReloadAuthKeyToReloader(&configReloaderContainer, &cr.Spec.CommonConfigReloaderParams)
	return configReloaderContainer
}

func discoverNotifiersIfNeeded(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) error {
	var additionalNotifiers []vmv1beta1.VMAlertNotifierSpec

	if cr.Spec.Notifier != nil {
		cr.Spec.Notifiers = append(cr.Spec.Notifiers, *cr.Spec.Notifier)
	}
	cfg := config.MustGetBaseConfig()
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
		o, err := n.Selector.AsListOptions()
		if err != nil {
			return fmt.Errorf("cannot convert notifier selector as ListOptions: %w", err)
		}
		if err := k8stools.ListObjectsByNamespace(ctx, rclient, cfg.WatchNamespaces, func(l *vmv1beta1.VMAlertmanagerList) {
			for _, item := range l.Items {
				if !item.DeletionTimestamp.IsZero() || (n.Selector.Namespace != nil && !n.Selector.Namespace.IsMatch(&item)) {
					continue
				}
				additionalNotifiers = append(additionalNotifiers, item.AsNotifiers()...)
			}
		}, o); err != nil {
			return fmt.Errorf("cannot list alertmanager with discovery selector: %w", err)
		}
	}
	cr.Spec.Notifiers = cr.Spec.Notifiers[:cnt]

	if len(additionalNotifiers) > 0 {
		sort.Slice(additionalNotifiers, func(i, j int) bool {
			return additionalNotifiers[i].URL > additionalNotifiers[j].URL
		})
	}
	cr.Spec.Notifiers = append(cr.Spec.Notifiers, additionalNotifiers...)
	return nil
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) error {
	cfg := config.MustGetBaseConfig()
	owner := cr.AsOwner()

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}

	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, disableSelfScrape) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	svcName := cr.PrefixedName()
	keepServices := map[string]struct{}{
		svcName: {},
	}
	if cr.Spec.ServiceSpec != nil && !cr.Spec.ServiceSpec.UseAsDefault {
		extraSvcName := cr.Spec.ServiceSpec.NameOrDefault(svcName)
		keepServices[extraSvcName] = struct{}{}
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, cr, keepServices); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}
	if !cr.IsOwnsServiceAccount() {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.ServiceAccount{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceaccount: %w", err)
		}
	}
	return nil
}
