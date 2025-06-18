package vmalert

import (
	"context"
	"fmt"
	"path"
	"sort"
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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	vmAlertConfigDir        = "/etc/vmalert/config"
	datasourceKey           = "datasource"
	remoteReadKey           = "remoteRead"
	remoteWriteKey          = "remoteWrite"
	notifierConfigMountPath = `/etc/vm/notifier_config`
	vmalertConfigSecretsDir = "/etc/vmalert/remote_secrets"
	bearerTokenKey          = "bearerToken"
	basicAuthPasswordKey    = "basicAuthPassword"
	oauth2SecretKey         = "oauth2SecretKey"
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

// CreateOrUpdate creates vmalert deployment for given CRD
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMAlert, rclient client.Client, cmNames []string) error {
	var prevCR *vmv1beta1.VMAlert
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if err := deletePrevStateResources(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot delete objects from previous state: %w", err)
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
	if err := discoverNotifierIfNeeded(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot discover additional notifiers: %w", err)
	}

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
	ac := build.NewAssetsCache(ctx, rclient, cfg)

	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
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
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: *generatedSpec,
	}
	build.DeploymentAddCommonParams(deploy, ptr.Deref(cr.Spec.UseStrictSecurity, false), &cr.Spec.CommonApplicationDeploymentParams)
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

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

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
		if len(cfg.TLSConfig.CAFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.tlsCAFile=%s", flagPrefix, cfg.TLSConfig.CAFile))
		} else if len(cfg.TLSConfig.CA.PrefixedName()) > 0 {
			file, err := ac.LoadPathFromSecretOrConfigMap(build.TLSAssetsResourceKind, namespace, &cfg.TLSConfig.CA)
			if err != nil {
				return nil, err
			}
			args = append(args, fmt.Sprintf("-%s.tlsCAFile=%s", flagPrefix, file))
		}
		if len(cfg.TLSConfig.CertFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.tlsCertFile=%s", flagPrefix, cfg.TLSConfig.CertFile))
		} else if len(cfg.TLSConfig.Cert.PrefixedName()) > 0 {
			file, err := ac.LoadPathFromSecretOrConfigMap(build.TLSAssetsResourceKind, namespace, &cfg.TLSConfig.Cert)
			if err != nil {
				return nil, err
			}
			args = append(args, fmt.Sprintf("-%s.tlsCertFile=%s", flagPrefix, file))
		}
		if len(cfg.TLSConfig.KeyFile) > 0 {
			args = append(args, fmt.Sprintf("-%s.tlsKeyFile=%s", flagPrefix, cfg.TLSConfig.KeyFile))
		} else if cfg.TLSConfig.KeySecret != nil {
			file, err := ac.LoadPathFromSecret(build.TLSAssetsResourceKind, namespace, cfg.TLSConfig.KeySecret)
			if err != nil {
				return nil, err
			}
			args = append(args, fmt.Sprintf("-%s.tlsKeyFile=%s", flagPrefix, file))
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
		secret, err := ac.LoadKeyFromSecretOrConfigMap(namespace, &cfg.OAuth2.ClientID)
		if err != nil {
			return nil, err
		}
		args = append(args, fmt.Sprintf("-%s.oauth2.clientID=%s", flagPrefix, secret))
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

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

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

type remoteFlag struct {
	isNotNull   bool
	flagSetting string
}

func buildNotifiersArgs(cr *vmv1beta1.VMAlert, ac *build.AssetsCache) ([]string, error) {
	var finalArgs []string
	var notifierArgs []remoteFlag
	notifierTargets := cr.Spec.Notifiers

	if _, ok := cr.Spec.ExtraArgs["notifier.blackhole"]; ok {
		// notifier.blackhole disables sending notifications completely, so we don't need to add any notifier args
		// also no need to add notifier.blackhole to args as it will be added with ExtraArgs
		return finalArgs, nil
	}

	if len(notifierTargets) == 0 && cr.Spec.NotifierConfigRef != nil {
		return append(finalArgs, fmt.Sprintf("-notifier.config=%s/%s", notifierConfigMountPath, cr.Spec.NotifierConfigRef.Key)), nil
	}

	url := remoteFlag{flagSetting: "-notifier.url=", isNotNull: true}
	authUser := remoteFlag{flagSetting: "-notifier.basicAuth.username="}
	authPasswordFile := remoteFlag{flagSetting: "-notifier.basicAuth.passwordFile="}
	tlsCAs := remoteFlag{flagSetting: "-notifier.tlsCAFile="}
	tlsCerts := remoteFlag{flagSetting: "-notifier.tlsCertFile="}
	tlsKeys := remoteFlag{flagSetting: "-notifier.tlsKeyFile="}
	tlsServerName := remoteFlag{flagSetting: "-notifier.tlsServerName="}
	tlsInSecure := remoteFlag{flagSetting: "-notifier.tlsInsecureSkipVerify="}
	headers := remoteFlag{flagSetting: "-notifier.headers="}
	bearerTokenPath := remoteFlag{flagSetting: "-notifier.bearerTokenFile="}
	oauth2SecretFile := remoteFlag{flagSetting: "-notifier.oauth2.clientSecretFile="}
	oauth2ClientID := remoteFlag{flagSetting: "-notifier.oauth2.clientID="}
	oauth2Scopes := remoteFlag{flagSetting: "-notifier.oauth2.scopes="}
	oauth2TokenURL := remoteFlag{flagSetting: "-notifier.oauth2.tokenUrl="}

	pathPrefix := path.Join(tlsAssetsDir, cr.Namespace)

	for _, nt := range notifierTargets {
		url.flagSetting += fmt.Sprintf("%s,", nt.URL)

		var caPath, certPath, keyPath, ServerName string
		var inSecure bool
		ntTLS := nt.TLSConfig
		if ntTLS != nil {
			if ntTLS.CAFile != "" {
				caPath = ntTLS.CAFile
			} else if ntTLS.CA.PrefixedName() != "" {
				caPath = ntTLS.BuildAssetPath(pathPrefix, ntTLS.CA.PrefixedName(), ntTLS.CA.Key())
			}
			if caPath != "" {
				tlsCAs.isNotNull = true
			}
			if ntTLS.CertFile != "" {
				certPath = ntTLS.CertFile
			} else if ntTLS.Cert.PrefixedName() != "" {
				certPath = ntTLS.BuildAssetPath(pathPrefix, ntTLS.Cert.PrefixedName(), ntTLS.Cert.Key())
			}
			if certPath != "" {
				tlsCerts.isNotNull = true
			}
			if ntTLS.KeyFile != "" {
				keyPath = ntTLS.KeyFile
			} else if ntTLS.KeySecret != nil {
				keyPath = ntTLS.BuildAssetPath(pathPrefix, ntTLS.KeySecret.Name, ntTLS.KeySecret.Key)
			}
			if keyPath != "" {
				tlsKeys.isNotNull = true
			}
			if ntTLS.InsecureSkipVerify {
				tlsInSecure.isNotNull = true
				inSecure = true
			}
			if ntTLS.ServerName != "" {
				ServerName = ntTLS.ServerName
				tlsServerName.isNotNull = true
			}
		}
		tlsCAs.flagSetting += fmt.Sprintf("%s,", caPath)
		tlsCerts.flagSetting += fmt.Sprintf("%s,", certPath)
		tlsKeys.flagSetting += fmt.Sprintf("%s,", keyPath)
		tlsServerName.flagSetting += fmt.Sprintf("%s,", ServerName)
		tlsInSecure.flagSetting += fmt.Sprintf("%v,", inSecure)
		var headerFlagValue string
		if len(nt.Headers) > 0 {
			for _, headerKV := range nt.Headers {
				headerFlagValue += headerKV + "^^"
			}
			headers.isNotNull = true
		}
		headerFlagValue = strings.TrimSuffix(headerFlagValue, "^^")
		headers.flagSetting += fmt.Sprintf("%s,", headerFlagValue)
		var user, passFile string
		if nt.BasicAuth != nil {
			if len(nt.BasicAuth.PasswordFile) > 0 {
				passFile = nt.BasicAuth.PasswordFile
				authPasswordFile.isNotNull = true
			} else {
				file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, &nt.BasicAuth.Password)
				if err != nil {
					return nil, err
				}
				passFile = file
				authPasswordFile.isNotNull = true
			}
			authUser.isNotNull = true
			secret, err := ac.LoadKeyFromSecret(cr.Namespace, &nt.BasicAuth.Username)
			if err != nil {
				return nil, err
			}
			user = secret
		}
		authUser.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(user, `"`, `\"`))
		authPasswordFile.flagSetting += fmt.Sprintf("%s,", passFile)

		var tokenPath string
		if nt.BearerAuth != nil {
			if nt.TokenSecret != nil {
				file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, nt.TokenSecret)
				if err != nil {
					return nil, err
				}
				bearerTokenPath.isNotNull = true
				tokenPath = file
			}
			if len(nt.TokenFilePath) > 0 {
				bearerTokenPath.isNotNull = true
				tokenPath = nt.TokenFilePath
			}
		}
		bearerTokenPath.flagSetting += fmt.Sprintf("%s,", tokenPath)
		var scopes, tokenURL, secretFile, clientID string
		if nt.OAuth2 != nil {
			if nt.OAuth2.ClientSecret != nil {
				file, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, nt.OAuth2.ClientSecret)
				if err != nil {
					return nil, err
				}
				oauth2SecretFile.isNotNull = true
				secretFile = file
			} else {
				oauth2SecretFile.isNotNull = true
				secretFile = nt.OAuth2.ClientSecretFile
			}
			if len(nt.OAuth2.Scopes) > 0 {
				oauth2Scopes.isNotNull = true
				scopes = strings.Join(nt.OAuth2.Scopes, ",")
			}
			if len(nt.OAuth2.TokenURL) > 0 {
				oauth2TokenURL.isNotNull = true
				tokenURL = nt.OAuth2.TokenURL
			}
			secret, err := ac.LoadKeyFromSecretOrConfigMap(cr.Namespace, &nt.OAuth2.ClientID)
			if err != nil {
				return nil, err
			}
			clientID = secret
			oauth2ClientID.isNotNull = true
		}
		oauth2Scopes.flagSetting += fmt.Sprintf("%s,", scopes)
		oauth2TokenURL.flagSetting += fmt.Sprintf("%s,", tokenURL)
		oauth2ClientID.flagSetting += fmt.Sprintf("%s,", clientID)
		oauth2SecretFile.flagSetting += fmt.Sprintf("%s,", secretFile)
	}
	notifierArgs = append(notifierArgs, url, authUser, authPasswordFile)
	notifierArgs = append(notifierArgs, tlsServerName, tlsKeys, tlsCerts, tlsCAs, tlsInSecure, headers, bearerTokenPath)
	notifierArgs = append(notifierArgs, oauth2SecretFile, oauth2ClientID, oauth2Scopes, oauth2TokenURL)

	for _, remoteArgType := range notifierArgs {
		if remoteArgType.isNotNull {
			finalArgs = append(finalArgs, strings.TrimSuffix(remoteArgType.flagSetting, ","))
		}
	}

	return finalArgs, nil
}

func buildConfigReloaderContainer(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, extraWatchVolumeMounts []corev1.VolumeMount) corev1.Container {
	volumeWatchArg := "-volume-dir"
	reloadURLArg := "-webhook-url"
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	if useVMConfigReloader {
		volumeWatchArg = "--watched-dir"
		reloadURLArg = "--reload-url"
	}
	confReloadArgs := []string{
		fmt.Sprintf("%s=%s", reloadURLArg, vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
	}
	for _, cm := range ruleConfigMapNames {
		confReloadArgs = append(confReloadArgs, fmt.Sprintf("%s=%s", volumeWatchArg, path.Join(vmAlertConfigDir, cm)))
	}
	for _, wm := range extraWatchVolumeMounts {
		confReloadArgs = append(confReloadArgs, fmt.Sprintf("%s=%s", volumeWatchArg, wm.MountPath))
	}
	if len(cr.Spec.ConfigReloaderExtraArgs) > 0 {
		for idx, arg := range confReloadArgs {
			cleanArg := strings.Split(strings.TrimLeft(arg, "-"), "=")[0]
			if replacement, ok := cr.Spec.ConfigReloaderExtraArgs[cleanArg]; ok {
				delete(cr.Spec.ConfigReloaderExtraArgs, cleanArg)
				confReloadArgs[idx] = fmt.Sprintf(`--%s=%s`, cleanArg, replacement)
			}
		}
		for k, v := range cr.Spec.ConfigReloaderExtraArgs {
			confReloadArgs = append(confReloadArgs, fmt.Sprintf(`--%s=%s`, k, v))
		}
		sort.Strings(confReloadArgs)
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
		Args:                     confReloadArgs,
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

func discoverNotifierIfNeeded(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) error {
	var additionalNotifiers []vmv1beta1.VMAlertNotifierSpec

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
		var ams vmv1beta1.VMAlertmanagerList
		amListOpts, err := n.Selector.AsListOptions()
		if err != nil {
			return fmt.Errorf("cannot convert notifier selector as ListOptions: %w", err)
		}
		if err := k8stools.ListObjectsByNamespace(ctx, rclient, config.MustGetWatchNamespaces(), func(objects *vmv1beta1.VMAlertmanagerList) {
			ams.Items = append(ams.Items, objects.Items...)
		}, amListOpts); err != nil {
			return fmt.Errorf("cannot list alertmanager with discovery selector: %w", err)
		}

		for _, item := range ams.Items {
			if !item.DeletionTimestamp.IsZero() || (n.Selector.Namespace != nil && !n.Selector.Namespace.IsMatch(&item)) {
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
		logger.WithContext(ctx).Info(fmt.Sprintf("additional notifiers count=%d discovered with sd selectors", len(additionalNotifiers)))
	}
	cr.Spec.Notifiers = append(cr.Spec.Notifiers, additionalNotifiers...)
	return nil
}

func deletePrevStateResources(ctx context.Context, cr *vmv1beta1.VMAlert, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}
	prevSvc, currSvc := cr.ParsedLastAppliedSpec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && cr.ParsedLastAppliedSpec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}

	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}
