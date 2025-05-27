package vmagent

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

func newPodSpec(cr *vmv1beta1.VMAgent, ssCache *scrapesSecretsCache) (*corev1.PodSpec, error) {
	var args []string

	if len(cr.Spec.RemoteWrite) > 0 {
		args = append(args, buildRemoteWrites(cr, ssCache)...)
	}
	args = append(args, buildRemoteWriteSettings(cr)...)

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	if cr.Spec.DaemonSetMode {
		envs = append(envs, corev1.EnvVar{
			Name: kubeNodeEnvName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		})
	}

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AppendInsertPorts(ports, cr.Spec.InsertPorts)

	var volumeMounts []corev1.VolumeMount
	// mount data path any way, even if user changes its value
	// we cannot rely on value of remoteWriteSettings.
	pqMountPath := persistentQueueDir
	if cr.Spec.StatefulMode {
		pqMountPath = persistentQueueStatefulSetDir
	}
	volumeMounts = append(volumeMounts,
		corev1.VolumeMount{
			Name:      cr.GetVolumeName(),
			MountPath: pqMountPath,
		},
	)
	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)

	var volumes []corev1.Volume
	// in case for sts, we have to use persistentVolumeClaimTemplate instead
	if !cr.Spec.StatefulMode {
		volumes = append(volumes, corev1.Volume{
			Name: cr.GetVolumeName(),
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	volumes = append(volumes, cr.Spec.Volumes...)

	if !cr.Spec.IngestOnlyMode {
		args = append(args,
			fmt.Sprintf("-promscrape.config=%s", path.Join(confOutDir, configEnvsubstFilename)))

		volumes = append(volumes,
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

		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "config-out",
				ReadOnly:  true,
				MountPath: confOutDir,
			},
			corev1.VolumeMount{
				Name:      "tls-assets",
				ReadOnly:  true,
				MountPath: tlsAssetsDir,
			},
		)
		volumes = append(volumes,
			corev1.Volume{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cr.PrefixedName(),
					},
				},
			})
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "config",
				ReadOnly:  true,
				MountPath: confDir,
			})
	}
	if cr.HasAnyStreamAggrRule() {
		volumes = append(volumes, corev1.Volume{
			Name: "stream-aggr-conf",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cr.StreamAggrConfigName(),
					},
				},
			},
		},
		)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "stream-aggr-conf",
			ReadOnly:  true,
			MountPath: vmv1beta1.StreamAggrConfigDir,
		},
		)
	}

	if cr.HasAnyRelabellingConfigs() {
		volumes = append(volumes,
			corev1.Volume{
				Name: "relabeling-assets",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: cr.RelabelingAssetName(),
						},
					},
				},
			},
		)
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "relabeling-assets",
				ReadOnly:  true,
				MountPath: vmv1beta1.RelabelingConfigDir,
			},
		)
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
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	volumes, volumeMounts = cr.Spec.License.MaybeAddToVolumes(volumes, volumeMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	if cr.Spec.RelabelConfig != nil || len(cr.Spec.InlineRelabelConfig) > 0 {
		args = append(args, "-remoteWrite.relabelConfig="+path.Join(vmv1beta1.RelabelingConfigDir, globalRelabelingName))
	}
	if cr.Spec.StreamAggrConfig != nil {
		if cr.Spec.StreamAggrConfig.HasAnyRule() {
			args = append(args, "-streamAggr.config="+path.Join(vmv1beta1.StreamAggrConfigDir, globalAggregationConfigName))
		}
		if cr.Spec.StreamAggrConfig.KeepInput {
			args = append(args, "-streamAggr.keepInput=true")
		}
		if cr.Spec.StreamAggrConfig.DropInput {
			args = append(args, "-streamAggr.dropInput=true")
		}
		if cr.Spec.StreamAggrConfig.DedupInterval != "" {
			args = append(args, fmt.Sprintf("-streamAggr.dedupInterval=%s", cr.Spec.StreamAggrConfig.DedupInterval))
		}
		if len(cr.Spec.StreamAggrConfig.DropInputLabels) > 0 {
			args = append(args, fmt.Sprintf("-streamAggr.dropInputLabels=%s", strings.Join(cr.Spec.StreamAggrConfig.DropInputLabels, ",")))
		}
		if cr.Spec.StreamAggrConfig.IgnoreOldSamples {
			args = append(args, "-streamAggr.ignoreOldSamples=true")
		}
		if cr.Spec.StreamAggrConfig.EnableWindows {
			args = append(args, "-streamAggr.enableWindows=true")
		}
	}

	args = build.AppendArgsForInsertPorts(args, cr.Spec.InsertPorts)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	container := corev1.Container{
		Name:                     "vmagent",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		VolumeMounts:             volumeMounts,
		Resources:                cr.Spec.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	build.AddServiceAccountTokenVolumeMount(&container, &cr.Spec.CommonApplicationDeploymentParams)
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	container = build.Probe(container, cr)

	build.AddConfigReloadAuthKeyToApp(&container, cr.Spec.ExtraArgs, &cr.Spec.CommonConfigReloaderParams)

	var containers []corev1.Container
	var ic []corev1.Container
	// conditional add config reloader container
	if !cr.Spec.IngestOnlyMode || cr.HasAnyRelabellingConfigs() || cr.HasAnyStreamAggrRule() {
		configReloader := buildConfigReloaderContainer(cr)
		containers = append(containers, configReloader)
		if !cr.Spec.IngestOnlyMode {
			ic = append(ic,
				buildInitConfigContainer(ptr.Deref(cr.Spec.UseVMConfigReloader, false), cr, configReloader.Args)...)
			build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, ic, useStrictSecurity)
		}
	}
	var err error
	ic, err = k8stools.MergePatchContainers(ic, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
	}

	containers = append(containers, container)

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, containers, useStrictSecurity)

	containers, err = k8stools.MergePatchContainers(containers, cr.Spec.Containers)
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
		Volumes:            volumes,
		InitContainers:     ic,
		Containers:         containers,
		ServiceAccountName: cr.GetServiceAccountName(),
	}, nil
}

type remoteFlag struct {
	isNotNull   bool
	flagSetting string
}

func buildRemoteWriteSettings(cr *vmv1beta1.VMAgent) []string {
	// limit to 1GB
	// most people do not care about this setting,
	// but it may harmfully affect kubernetes cluster health
	maxDiskUsage := defaultMaxDiskUsage
	var containsMaxDiskUsage bool
	for i := range cr.Spec.RemoteWrite {
		rws := cr.Spec.RemoteWrite[i]
		if rws.MaxDiskUsage != nil {
			containsMaxDiskUsage = true
			break
		}
	}
	var args []string
	if cr.Spec.RemoteWriteSettings == nil {
		// fast path
		pqMountPath := persistentQueueDir
		if cr.Spec.StatefulMode {
			pqMountPath = persistentQueueStatefulSetDir
		}
		args = append(args,
			fmt.Sprintf("-remoteWrite.tmpDataPath=%s", pqMountPath))

		if !containsMaxDiskUsage {
			args = append(args, fmt.Sprintf("-remoteWrite.maxDiskUsagePerURL=%s", maxDiskUsage))
		}
		return args
	}

	rws := *cr.Spec.RemoteWriteSettings
	if rws.FlushInterval != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.flushInterval=%s", *rws.FlushInterval))
	}
	if rws.MaxBlockSize != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.maxBlockSize=%d", *rws.MaxBlockSize))
	}

	if rws.Queues != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.queues=%d", *rws.Queues))
	}
	if rws.ShowURL != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.showURL=%t", *rws.ShowURL))
	}
	pqMountPath := persistentQueueDir
	if cr.Spec.StatefulMode {
		pqMountPath = persistentQueueStatefulSetDir
	}
	if rws.TmpDataPath != nil {
		pqMountPath = *rws.TmpDataPath
	}
	args = append(args, fmt.Sprintf("-remoteWrite.tmpDataPath=%s", pqMountPath))

	if rws.MaxDiskUsagePerURL != nil {
		maxDiskUsage = rws.MaxDiskUsagePerURL.String()
	}
	if !containsMaxDiskUsage {
		args = append(args, "-remoteWrite.maxDiskUsagePerURL="+maxDiskUsage)
	}
	if rws.Labels != nil {
		lbls := sortMap(rws.Labels)
		flagValue := "-remoteWrite.label="
		if len(lbls) > 0 {
			flagValue += fmt.Sprintf("%s=%s", lbls[0].key, lbls[0].value)
			for _, lv := range lbls[1:] {
				flagValue += fmt.Sprintf(",%s=%s", lv.key, lv.value)
			}
			args = append(args, flagValue)
		}

	}
	if rws.UseMultiTenantMode {
		args = append(args, "-enableMultitenantHandlers=true")
	}
	return args
}

type item struct {
	key, value string
}

func sortMap(m map[string]string) []item {
	var kv []item
	for k, v := range m {
		kv = append(kv, item{key: k, value: v})
	}
	sort.Slice(kv, func(i, j int) bool {
		return kv[i].key < kv[j].key
	})
	return kv
}

func buildRemoteWrites(cr *vmv1beta1.VMAgent, ssCache *scrapesSecretsCache) []string {
	var finalArgs []string
	var remoteArgs []remoteFlag
	remoteTargets := cr.Spec.RemoteWrite

	url := remoteFlag{flagSetting: "-remoteWrite.url=", isNotNull: true}

	authUser := remoteFlag{flagSetting: "-remoteWrite.basicAuth.username="}
	authPasswordFile := remoteFlag{flagSetting: "-remoteWrite.basicAuth.passwordFile="}
	bearerTokenFile := remoteFlag{flagSetting: "-remoteWrite.bearerTokenFile="}
	urlRelabelConfig := remoteFlag{flagSetting: "-remoteWrite.urlRelabelConfig="}
	sendTimeout := remoteFlag{flagSetting: "-remoteWrite.sendTimeout="}
	tlsCAs := remoteFlag{flagSetting: "-remoteWrite.tlsCAFile="}
	tlsCerts := remoteFlag{flagSetting: "-remoteWrite.tlsCertFile="}
	tlsKeys := remoteFlag{flagSetting: "-remoteWrite.tlsKeyFile="}
	tlsInsecure := remoteFlag{flagSetting: "-remoteWrite.tlsInsecureSkipVerify="}
	tlsServerName := remoteFlag{flagSetting: "-remoteWrite.tlsServerName="}
	oauth2ClientID := remoteFlag{flagSetting: "-remoteWrite.oauth2.clientID="}
	oauth2ClientSecretFile := remoteFlag{flagSetting: "-remoteWrite.oauth2.clientSecretFile="}
	oauth2Scopes := remoteFlag{flagSetting: "-remoteWrite.oauth2.scopes="}
	oauth2TokenURL := remoteFlag{flagSetting: "-remoteWrite.oauth2.tokenUrl="}
	headers := remoteFlag{flagSetting: "-remoteWrite.headers="}
	streamAggrConfig := remoteFlag{flagSetting: "-remoteWrite.streamAggr.config="}
	streamAggrKeepInput := remoteFlag{flagSetting: "-remoteWrite.streamAggr.keepInput="}
	streamAggrDropInput := remoteFlag{flagSetting: "-remoteWrite.streamAggr.dropInput="}
	streamAggrDedupInterval := remoteFlag{flagSetting: "-remoteWrite.streamAggr.dedupInterval="}
	streamAggrDropInputLabels := remoteFlag{flagSetting: "-remoteWrite.streamAggr.dropInputLabels="}
	streamAggrIgnoreFirstIntervals := remoteFlag{flagSetting: "-remoteWrite.streamAggr.ignoreFirstIntervals="}
	streamAggrIgnoreOldSamples := remoteFlag{flagSetting: "-remoteWrite.streamAggr.ignoreOldSamples="}
	streamAggrEnableWindows := remoteFlag{flagSetting: "-remoteWrite.streamAggr.enableWindows="}
	maxDiskUsagePerURL := remoteFlag{flagSetting: "-remoteWrite.maxDiskUsagePerURL="}
	forceVMProto := remoteFlag{flagSetting: "-remoteWrite.forceVMProto="}

	pathPrefix := path.Join(tlsAssetsDir, cr.Namespace)

	maxDiskUsage := defaultMaxDiskUsage
	if cr.Spec.RemoteWriteSettings != nil && cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL != nil {
		maxDiskUsage = cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL.String()
	}

	for i := range remoteTargets {
		rws := remoteTargets[i]
		url.flagSetting += fmt.Sprintf("%s,", rws.URL)

		var caPath, certPath, keyPath, ServerName string
		var insecure bool
		if rws.TLSConfig != nil {
			if rws.TLSConfig.CAFile != "" {
				caPath = rws.TLSConfig.CAFile
			} else if rws.TLSConfig.CA.PrefixedName() != "" {
				caPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.CA.PrefixedName(), rws.TLSConfig.CA.Key())
			}
			if caPath != "" {
				tlsCAs.isNotNull = true
			}
			if rws.TLSConfig.CertFile != "" {
				certPath = rws.TLSConfig.CertFile
			} else if rws.TLSConfig.Cert.PrefixedName() != "" {
				certPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.Cert.PrefixedName(), rws.TLSConfig.Cert.Key())
			}
			if certPath != "" {
				tlsCerts.isNotNull = true
			}
			switch {
			case rws.TLSConfig.KeyFile != "":
				keyPath = rws.TLSConfig.KeyFile
			case rws.TLSConfig.KeySecret != nil:
				keyPath = rws.TLSConfig.BuildAssetPath(pathPrefix, rws.TLSConfig.KeySecret.Name, rws.TLSConfig.KeySecret.Key)
			}
			if keyPath != "" {
				tlsKeys.isNotNull = true
			}
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
		var passFile string
		if rws.BasicAuth != nil {
			if s, ok := ssCache.baSecrets[rws.AsMapKey()]; ok {
				authUser.isNotNull = true

				user = s.Username
				if len(s.Password) > 0 {
					authPasswordFile.isNotNull = true
					passFile = path.Join(confDir, rws.AsSecretKey(i, "basicAuthPassword"))
				}
				if len(rws.BasicAuth.PasswordFile) > 0 {
					passFile = rws.BasicAuth.PasswordFile
					authPasswordFile.isNotNull = true
				}
			}
		}
		authUser.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(user, `"`, `\"`))
		authPasswordFile.flagSetting += fmt.Sprintf("%s,", passFile)

		var value string
		if rws.BearerTokenSecret != nil && rws.BearerTokenSecret.Name != "" {
			bearerTokenFile.isNotNull = true
			value = path.Join(confDir, rws.AsSecretKey(i, "bearerToken"))
		}
		bearerTokenFile.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(value, `"`, `\"`))

		value = ""

		if rws.UrlRelabelConfig != nil || len(rws.InlineUrlRelabelConfig) > 0 {
			urlRelabelConfig.isNotNull = true
			value = path.Join(vmv1beta1.RelabelingConfigDir, fmt.Sprintf(urlRelabelingName, i))
		}

		urlRelabelConfig.flagSetting += fmt.Sprintf("%s,", value)

		value = ""

		if rws.SendTimeout != nil {
			if !sendTimeout.isNotNull {
				sendTimeout.isNotNull = true
			}
			value = *rws.SendTimeout
		}
		sendTimeout.flagSetting += fmt.Sprintf("%s,", value)

		value = ""
		if len(rws.Headers) > 0 {
			headers.isNotNull = true
			for _, headerValue := range rws.Headers {
				value += headerValue + "^^"
			}
			value = strings.TrimSuffix(value, "^^")
		}
		headers.flagSetting += fmt.Sprintf("%s,", value)
		var oaturl, oascopes, oaclientID, oaSecretKeyFile string
		if rws.OAuth2 != nil {
			if len(rws.OAuth2.TokenURL) > 0 {
				oauth2TokenURL.isNotNull = true
				oaturl = rws.OAuth2.TokenURL
			}

			if len(rws.OAuth2.Scopes) > 0 {
				oauth2Scopes.isNotNull = true
				oascopes = strings.Join(rws.OAuth2.Scopes, ",")
			}

			if len(rws.OAuth2.ClientSecretFile) > 0 {
				oauth2ClientSecretFile.isNotNull = true
				oaSecretKeyFile = rws.OAuth2.ClientSecretFile
			}

			sv := ssCache.oauth2Secrets[rws.AsMapKey()]
			if rws.OAuth2.ClientSecret != nil && sv != nil {
				oauth2ClientSecretFile.isNotNull = true
				oaSecretKeyFile = path.Join(confDir, rws.AsSecretKey(i, "oauth2Secret"))
			}

			if len(rws.OAuth2.ClientID.PrefixedName()) > 0 && sv != nil {
				oaclientID = sv.ClientID
				oauth2ClientID.isNotNull = true
			}

		}

		oauth2TokenURL.flagSetting += fmt.Sprintf("%s,", oaturl)
		oauth2ClientSecretFile.flagSetting += fmt.Sprintf("%s,", oaSecretKeyFile)
		oauth2ClientID.flagSetting += fmt.Sprintf("%s,", oaclientID)
		oauth2Scopes.flagSetting += fmt.Sprintf("%s,", oascopes)

		var dedupIntVal, streamConfVal string
		var keepInputVal, dropInputVal, ignoreOldSamples, enableWindows bool
		var ignoreFirstIntervalsVal int
		if rws.StreamAggrConfig != nil {
			if rws.StreamAggrConfig.HasAnyRule() {
				streamAggrConfig.isNotNull = true
				streamConfVal = path.Join(vmv1beta1.StreamAggrConfigDir, rws.AsConfigMapKey(i, "stream-aggr-conf"))
			}

			dedupIntVal = rws.StreamAggrConfig.DedupInterval
			if dedupIntVal != "" {
				streamAggrDedupInterval.isNotNull = true
			}

			keepInputVal = rws.StreamAggrConfig.KeepInput
			if keepInputVal {
				streamAggrKeepInput.isNotNull = true
			}
			dropInputVal = rws.StreamAggrConfig.DropInput
			if dropInputVal {
				streamAggrDropInput.isNotNull = true
			}
			if len(rws.StreamAggrConfig.DropInputLabels) > 0 {
				streamAggrDropInputLabels.isNotNull = true
				streamAggrDropInputLabels.flagSetting += fmt.Sprintf("%s,", strings.Join(rws.StreamAggrConfig.DropInputLabels, ","))
			}
			ignoreFirstIntervalsVal = rws.StreamAggrConfig.IgnoreFirstIntervals
			if ignoreFirstIntervalsVal > 0 {
				streamAggrIgnoreFirstIntervals.isNotNull = true
			}
			ignoreOldSamples = rws.StreamAggrConfig.IgnoreOldSamples
			if ignoreOldSamples {
				streamAggrIgnoreOldSamples.isNotNull = true
			}
			enableWindows = rws.StreamAggrConfig.EnableWindows
			if enableWindows {
				streamAggrEnableWindows.isNotNull = true
			}
		}
		streamAggrConfig.flagSetting += fmt.Sprintf("%s,", streamConfVal)
		streamAggrKeepInput.flagSetting += fmt.Sprintf("%v,", keepInputVal)
		streamAggrDropInput.flagSetting += fmt.Sprintf("%v,", dropInputVal)
		streamAggrDedupInterval.flagSetting += fmt.Sprintf("%s,", dedupIntVal)
		streamAggrIgnoreFirstIntervals.flagSetting += fmt.Sprintf("%d,", ignoreFirstIntervalsVal)
		streamAggrIgnoreOldSamples.flagSetting += fmt.Sprintf("%v,", ignoreOldSamples)
		streamAggrEnableWindows.flagSetting += fmt.Sprintf("%v", enableWindows)

		value = maxDiskUsage
		if rws.MaxDiskUsage != nil {
			value = rws.MaxDiskUsage.String()
			maxDiskUsagePerURL.isNotNull = true
		}
		maxDiskUsagePerURL.flagSetting += fmt.Sprintf("%s,", value)
		value = ""

		if rws.ForceVMProto {
			forceVMProto.isNotNull = true
		}
		forceVMProto.flagSetting += fmt.Sprintf("%t,", rws.ForceVMProto)
	}

	remoteArgs = append(remoteArgs, url, authUser, bearerTokenFile, urlRelabelConfig, tlsInsecure, sendTimeout)
	remoteArgs = append(remoteArgs, tlsServerName, tlsKeys, tlsCerts, tlsCAs)
	remoteArgs = append(remoteArgs, oauth2ClientID, oauth2ClientSecretFile, oauth2Scopes, oauth2TokenURL)
	remoteArgs = append(remoteArgs, headers, authPasswordFile)
	remoteArgs = append(remoteArgs, streamAggrConfig, streamAggrKeepInput, streamAggrDedupInterval, streamAggrDropInput, streamAggrDropInputLabels, streamAggrIgnoreFirstIntervals, streamAggrIgnoreOldSamples, streamAggrEnableWindows)
	remoteArgs = append(remoteArgs, maxDiskUsagePerURL, forceVMProto)

	for _, remoteArgType := range remoteArgs {
		if remoteArgType.isNotNull {
			finalArgs = append(finalArgs, strings.TrimSuffix(remoteArgType.flagSetting, ","))
		}
	}
	return finalArgs
}

func buildConfigReloaderContainer(cr *vmv1beta1.VMAgent) corev1.Container {
	var configReloadVolumeMounts []corev1.VolumeMount
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	if !cr.Spec.IngestOnlyMode {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "config-out",
				MountPath: confOutDir,
			},
		)
		if !useVMConfigReloader {
			configReloadVolumeMounts = append(configReloadVolumeMounts,
				corev1.VolumeMount{
					Name:      "config",
					MountPath: confDir,
				})
		}
	}
	if cr.HasAnyRelabellingConfigs() {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "relabeling-assets",
				ReadOnly:  true,
				MountPath: vmv1beta1.RelabelingConfigDir,
			})
	}
	if cr.HasAnyStreamAggrRule() {
		configReloadVolumeMounts = append(configReloadVolumeMounts,
			corev1.VolumeMount{
				Name:      "stream-aggr-conf",
				ReadOnly:  true,
				MountPath: vmv1beta1.StreamAggrConfigDir,
			})
	}

	configReloadArgs := buildConfigReloaderArgs(cr)
	cntr := corev1.Container{
		Name:                     "config-reloader",
		Image:                    cr.Spec.ConfigReloaderImageTag,
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
		Resources:    cr.Spec.ConfigReloaderResources,
	}
	if useVMConfigReloader {
		cntr.Command = nil
		build.AddServiceAccountTokenVolumeMount(&cntr, &cr.Spec.CommonApplicationDeploymentParams)
	}
	build.AddsPortProbesToConfigReloaderContainer(useVMConfigReloader, &cntr)
	build.AddConfigReloadAuthKeyToReloader(&cntr, &cr.Spec.CommonConfigReloaderParams)
	return cntr
}

func buildConfigReloaderArgs(cr *vmv1beta1.VMAgent) []string {
	// by default use watched-dir
	// it should simplify parsing for latest and empty version tags.
	dirsArg := "watched-dir"

	args := []string{
		fmt.Sprintf("--reload-url=%s", vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
	}
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

	if !cr.Spec.IngestOnlyMode {
		args = append(args, fmt.Sprintf("--config-envsubst-file=%s", path.Join(confOutDir, configEnvsubstFilename)))
		if useVMConfigReloader {
			args = append(args, fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.PrefixedName()))
			args = append(args, "--config-secret-key=vmagent.yaml.gz")
		} else {
			args = append(args, fmt.Sprintf("--config-file=%s", path.Join(confDir, gzippedFilename)))
		}
	}
	if cr.HasAnyStreamAggrRule() {
		args = append(args, fmt.Sprintf("--%s=%s", dirsArg, vmv1beta1.StreamAggrConfigDir))
	}
	if cr.HasAnyRelabellingConfigs() {
		args = append(args, fmt.Sprintf("--%s=%s", dirsArg, vmv1beta1.RelabelingConfigDir))
	}
	if len(cr.Spec.ConfigReloaderExtraArgs) > 0 {
		for idx, arg := range args {
			cleanArg := strings.Split(strings.TrimLeft(arg, "-"), "=")[0]
			if replacement, ok := cr.Spec.ConfigReloaderExtraArgs[cleanArg]; ok {
				delete(cr.Spec.ConfigReloaderExtraArgs, cleanArg)
				args[idx] = fmt.Sprintf(`--%s=%s`, cleanArg, replacement)
			}
		}
		for k, v := range cr.Spec.ConfigReloaderExtraArgs {
			args = append(args, fmt.Sprintf(`--%s=%s`, k, v))
		}
		sort.Strings(args)
	}

	return args
}

func buildInitConfigContainer(useVMConfigReloader bool, cr *vmv1beta1.VMAgent, configReloaderArgs []string) []corev1.Container {
	var initReloader corev1.Container
	baseImage := cr.Spec.ConfigReloaderImageTag
	resources := cr.Spec.ConfigReloaderResources
	if useVMConfigReloader {
		initReloader = corev1.Container{
			Image: baseImage,
			Name:  "config-init",
			Args:  append(configReloaderArgs, "--only-init-config"),
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "config-out",
					MountPath: confOutDir,
				},
			},
			Resources: resources,
		}
		build.AddServiceAccountTokenVolumeMount(&initReloader, &cr.Spec.CommonApplicationDeploymentParams)
		return []corev1.Container{initReloader}
	}
	initReloader = corev1.Container{
		Image: baseImage,
		Name:  "config-init",
		Command: []string{
			"/bin/sh",
		},
		Args: []string{
			"-c",
			fmt.Sprintf("gunzip -c %s > %s", path.Join(confDir, gzippedFilename), path.Join(confOutDir, configEnvsubstFilename)),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: confDir,
			},
			{
				Name:      "config-out",
				MountPath: confOutDir,
			},
		},
		Resources: resources,
	}

	return []corev1.Container{initReloader}
}
