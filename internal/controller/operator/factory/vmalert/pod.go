package vmalert

import (
	"fmt"
	"path"
	"sort"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	configDir               = "/etc/vmalert/config"
	datasourceKey           = "datasource"
	remoteReadKey           = "remoteRead"
	remoteWriteKey          = "remoteWrite"
	notifierConfigMountPath = `/etc/vm/notifier_config`
	configSecretsDir        = "/etc/vmalert/remote_secrets"
	bearerTokenKey          = "bearerToken"
	basicAuthPasswordKey    = "basicAuthPassword"
	oauth2SecretKey         = "oauth2SecretKey"
	tlsAssetsDir            = "/etc/vmalert-tls/certs"
)

func buildNotifierKey(idx int) string {
	return fmt.Sprintf("notifier-%d", idx)
}

func buildRemoteSecretKey(source, suffix string) string {
	return fmt.Sprintf("%s_%s", strings.ToUpper(source), strings.ToUpper(suffix))
}

func newPodSpec(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, remoteSecrets map[string]*authSecret) (*corev1.PodSpec, error) {

	args := buildArgs(cr, ruleConfigMapNames, remoteSecrets)

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
	},
		corev1.Volume{
			Name: "remote-secrets",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.PrefixedName(),
				},
			},
		},
	)

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
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "tls-assets",
		ReadOnly:  true,
		MountPath: tlsAssetsDir,
	},
		corev1.VolumeMount{
			Name:      "remote-secrets",
			ReadOnly:  true,
			MountPath: configSecretsDir,
		},
	)

	volumes, volumeMounts = cr.Spec.License.MaybeAddToVolumes(volumes, volumeMounts, vmv1beta1.SecretsDir)

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
		crVolumeMount := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		}
		volumeMounts = append(volumeMounts, crVolumeMount)
		configReloaderWatchMounts = append(configReloaderWatchMounts, crVolumeMount)
	}

	for _, name := range ruleConfigMapNames {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      name,
			MountPath: path.Join(configDir, name),
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

	var containers []corev1.Container

	container := corev1.Container{
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
	container = build.Probe(container, cr)
	build.AddConfigReloadAuthKeyToApp(&container, cr.Spec.ExtraArgs, &cr.Spec.CommonConfigReloaderParams)
	containers = append(containers, container)

	if !cr.IsUnmanaged() {
		crContainer := buildConfigReloaderContainer(cr, ruleConfigMapNames, configReloaderWatchMounts)
		containers = append(containers, crContainer)
	}

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, containers, useStrictSecurity)
	containers, err := k8stools.MergePatchContainers(containers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.TopologySpreadConstraints {
		if cr.Spec.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			}
		}
	}
	volumes = build.AddConfigReloadAuthKeyVolume(volumes, &cr.Spec.CommonConfigReloaderParams)

	return &corev1.PodSpec{
		ServiceAccountName: cr.GetServiceAccountName(),
		InitContainers:     cr.Spec.InitContainers,
		Containers:         containers,
		Volumes:            volumes,
	}, nil
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

func buildAuthArgs(args []string, flagPrefix string, ha vmv1beta1.HTTPAuth, remoteSecrets map[string]*authSecret) []string {
	if s, ok := remoteSecrets[flagPrefix]; ok {
		// safety checks must be performed by previous code
		if ha.BasicAuth != nil {
			args = append(args, fmt.Sprintf("-%s.basicAuth.username=%s", flagPrefix, s.Username))
			if len(s.Password) > 0 {
				args = append(args, fmt.Sprintf("-%s.basicAuth.passwordFile=%s", flagPrefix, path.Join(configSecretsDir, buildRemoteSecretKey(flagPrefix, basicAuthPasswordKey))))
			}
			if len(ha.BasicAuth.PasswordFile) > 0 {
				args = append(args, fmt.Sprintf("-%s.basicAuth.passwordFile=%s", flagPrefix, ha.BasicAuth.PasswordFile))
			}
		}
		if ha.BearerAuth != nil {
			if len(s.bearerValue) > 0 {
				args = append(args, fmt.Sprintf("-%s.bearerTokenFile=%s", flagPrefix, path.Join(configSecretsDir, buildRemoteSecretKey(flagPrefix, bearerTokenKey))))
			} else if len(ha.TokenFilePath) > 0 {
				args = append(args, fmt.Sprintf("-%s.bearerTokenFile=%s", flagPrefix, ha.TokenFilePath))
			}
		}
		if ha.OAuth2 != nil {
			if len(ha.OAuth2.ClientSecretFile) > 0 {
				args = append(args, fmt.Sprintf("-%s.oauth2.clientSecretFile=%s", flagPrefix, ha.OAuth2.ClientSecretFile))
			} else {
				args = append(args, fmt.Sprintf("-%s.oauth2.clientSecretFile=%s", flagPrefix, path.Join(configSecretsDir, buildRemoteSecretKey(flagPrefix, oauth2SecretKey))))
			}
			args = append(args, fmt.Sprintf("-%s.oauth2.clientID=%s", flagPrefix, s.ClientID))
			args = append(args, fmt.Sprintf("-%s.oauth2.tokenUrl=%s", flagPrefix, ha.OAuth2.TokenURL))
			args = append(args, fmt.Sprintf("-%s.oauth2.scopes=%s", flagPrefix, strings.Join(ha.OAuth2.Scopes, ",")))
		}
	}

	return args
}

func buildArgs(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, remoteSecrets map[string]*authSecret) []string {
	pathPrefix := path.Join(tlsAssetsDir, cr.Namespace)
	args := []string{
		fmt.Sprintf("-datasource.url=%s", cr.Spec.Datasource.URL),
	}

	args = buildHeadersArg("datasource.headers", args, cr.Spec.Datasource.Headers)
	args = append(args, buildNotifiersArgs(cr, remoteSecrets)...)
	args = buildAuthArgs(args, datasourceKey, cr.Spec.Datasource.HTTPAuth, remoteSecrets)

	if cr.Spec.Datasource.TLSConfig != nil {
		tlsConf := cr.Spec.Datasource.TLSConfig
		args = tlsConf.AsArgs(args, datasourceKey, pathPrefix)
	}

	if cr.Spec.RemoteWrite != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.url=%s", cr.Spec.RemoteWrite.URL))
		args = buildAuthArgs(args, remoteWriteKey, cr.Spec.RemoteWrite.HTTPAuth, remoteSecrets)
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
		if cr.Spec.RemoteWrite.TLSConfig != nil {
			tlsConf := cr.Spec.RemoteWrite.TLSConfig
			args = tlsConf.AsArgs(args, remoteWriteKey, pathPrefix)
		}
	}
	for k, v := range cr.Spec.ExternalLabels {
		args = append(args, fmt.Sprintf("-external.label=%s=%s", k, v))
	}

	if cr.Spec.RemoteRead != nil {
		args = append(args, fmt.Sprintf("-remoteRead.url=%s", cr.Spec.RemoteRead.URL))
		args = buildAuthArgs(args, remoteReadKey, cr.Spec.RemoteRead.HTTPAuth, remoteSecrets)
		args = buildHeadersArg("remoteRead.headers", args, cr.Spec.RemoteRead.Headers)
		if cr.Spec.RemoteRead.Lookback != nil {
			args = append(args, fmt.Sprintf("-remoteRead.lookback=%s", *cr.Spec.RemoteRead.Lookback))
		}
		if cr.Spec.RemoteRead.TLSConfig != nil {
			tlsConf := cr.Spec.RemoteRead.TLSConfig
			args = tlsConf.AsArgs(args, remoteReadKey, pathPrefix)
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
		args = append(args, fmt.Sprintf("-rule=%q", path.Join(configDir, cm, "*.yaml")))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	for _, rulePath := range cr.Spec.RulePath {
		args = append(args, fmt.Sprintf("-rule=%q", rulePath))
	}
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	return args
}

type remoteFlag struct {
	isNotNull   bool
	flagSetting string
}

func buildNotifiersArgs(cr *vmv1beta1.VMAlert, ntBasicAuth map[string]*authSecret) []string {
	var finalArgs []string
	var notifierArgs []remoteFlag
	notifierTargets := cr.Spec.Notifiers

	if _, ok := cr.Spec.ExtraArgs["notifier.blackhole"]; ok {
		// notifier.blackhole disables sending notifications completely, so we don't need to add any notifier args
		// also no need to add notifier.blackhole to args as it will be added with ExtraArgs
		return finalArgs
	}

	if len(notifierTargets) == 0 && cr.Spec.NotifierConfigRef != nil {
		return append(finalArgs, fmt.Sprintf("-notifier.config=%s/%s", notifierConfigMountPath, cr.Spec.NotifierConfigRef.Key))
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

	for i, nt := range notifierTargets {
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
		s := ntBasicAuth[buildNotifierKey(i)]
		if nt.BasicAuth != nil {
			if s == nil {
				panic("secret for basic notifier cannot be nil")
			}
			authUser.isNotNull = true
			user = s.Username
			if len(s.Password) > 0 {
				passFile = path.Join(configSecretsDir, buildRemoteSecretKey(buildNotifierKey(i), basicAuthPasswordKey))
				authPasswordFile.isNotNull = true
			}
			if len(nt.BasicAuth.PasswordFile) > 0 {
				passFile = nt.BasicAuth.PasswordFile
				authPasswordFile.isNotNull = true
			}
		}
		authUser.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(user, `"`, `\"`))
		authPasswordFile.flagSetting += fmt.Sprintf("%s,", passFile)

		var tokenPath string
		if nt.BearerAuth != nil {
			if len(nt.TokenFilePath) > 0 {
				bearerTokenPath.isNotNull = true
				tokenPath = nt.TokenFilePath
			} else if len(s.bearerValue) > 0 {
				bearerTokenPath.isNotNull = true
				tokenPath = path.Join(configSecretsDir, buildRemoteSecretKey(buildNotifierKey(i), bearerTokenKey))
			}
		}
		bearerTokenPath.flagSetting += fmt.Sprintf("%s,", tokenPath)
		var scopes, tokenURL, secretFile, clientID string
		if nt.OAuth2 != nil {
			if s == nil {
				panic("secret for oauth2 notifier cannot be nil")
			}
			if len(nt.OAuth2.Scopes) > 0 {
				oauth2Scopes.isNotNull = true
				scopes = strings.Join(nt.OAuth2.Scopes, ",")
			}
			if len(nt.OAuth2.TokenURL) > 0 {
				oauth2TokenURL.isNotNull = true
				tokenURL = nt.OAuth2.TokenURL
			}
			clientID = s.ClientID
			oauth2ClientID.isNotNull = true
			if len(s.ClientSecret) > 0 {
				oauth2SecretFile.isNotNull = true
				secretFile = path.Join(configSecretsDir, buildRemoteSecretKey(buildNotifierKey(i), oauth2SecretKey))
			} else {
				oauth2SecretFile.isNotNull = true
				secretFile = nt.OAuth2.ClientSecretFile
			}
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

	return finalArgs
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
		confReloadArgs = append(confReloadArgs, fmt.Sprintf("%s=%s", volumeWatchArg, path.Join(configDir, cm)))
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
			MountPath: path.Join(configDir, name),
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
