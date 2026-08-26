package vtagent

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"path"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	defaultTmpDataPath = "/vtagent-data"

	tmpDataVolumeName           = "tmp-data"
	remoteWriteAssetsMounthPath = "/etc/vt/remote-write-assets"
	tlsServerConfigMountPath    = "/etc/vt/tls-server-secrets"
)

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTAgent) error {
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			build.AddOTLPGRPCPortToService(svc, prevCR.Spec.GRPCSpec)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.ServiceSpec)
	}
	newService := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		build.AddOTLPGRPCPortToService(svc, cr.Spec.GRPCSpec)
	})

	owner := cr.AsOwner()
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, cr.Spec.ServiceSpec)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vtagent additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService, &owner); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vtagent: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService, &owner); err != nil {
		return fmt.Errorf("cannot reconcile service for vtagent: %w", err)
	}
	return nil
}

func buildScrape(cr *vmv1.VTAgent) *vmv1beta1.VMPodScrape {
	if cr == nil || ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMPodScrape(cr, "http")
}

// CreateOrUpdate creates statefulset for vtagent and configures it
// waits for healthy state
func CreateOrUpdate(ctx context.Context, cr *vmv1.VTAgent, rclient client.Client) error {
	if cr.Paused() {
		return nil
	}
	var prevCR *vmv1.VTAgent
	if cr.Status.LastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.Status.LastAppliedSpec
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete objects from prev state: %w", err)
		}
	}
	owner := cr.AsOwner()
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA, &owner); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	if err := createOrUpdateService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		svs := buildScrape(cr)
		prevSvs := buildScrape(prevCR)
		if err := reconcile.VMPodScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create or update scrape object: %w", err)
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB, &owner); err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vtagent: %w", err)
		}
	}
	cfg := config.MustGetBaseConfig()
	if cr.Spec.VPA != nil && !cfg.VPAAPIEnabled {
		return fmt.Errorf("spec.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
	}
	if err := createOrUpdateVPA(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot create or update vpa for vtagent: %w", err)
	}
	if cr.Spec.NetworkPolicy != nil {
		var prevNP *networkingv1.NetworkPolicy
		if prevCR != nil && prevCR.Spec.NetworkPolicy != nil {
			prevNP = build.NetworkPolicy(prevCR, prevCR.Spec.NetworkPolicy)
		}
		if err := reconcile.NetworkPolicy(ctx, rclient, build.NetworkPolicy(cr, cr.Spec.NetworkPolicy), prevNP, &owner); err != nil {
			return fmt.Errorf("cannot update network policy for vtagent: %w", err)
		}
	}
	return createOrUpdateSts(ctx, rclient, cr, prevCR)
}

func createOrUpdateSts(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTAgent) error {
	var prevSts *appsv1.StatefulSet
	if prevCR != nil {
		var err error
		prevSts, err = newStatefulSet(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev statefulset for vtagent: %w", err)
		}
	}
	newSts, err := newStatefulSet(cr)
	if err != nil {
		return fmt.Errorf("cannot build new statefulset for vtagent: %w", err)
	}
	owner := cr.AsOwner()
	o := reconcile.StatefulSetOpts{
		SelectorLabels: cr.SelectorLabels(),
	}
	if err := reconcile.StatefulSet(ctx, rclient, newSts, prevSts, &owner, &o); err != nil {
		return fmt.Errorf("cannot reconcile statefulset for vtagent: %w", err)
	}
	return nil
}

func newStatefulSet(cr *vmv1.VTAgent) (*appsv1.StatefulSet, error) {
	podSpec, err := newPodSpec(cr)
	if err != nil {
		return nil, err
	}
	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.RollingUpdateStrategy,
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			ServiceName:         cr.PrefixedName(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.PodLabels(),
					Annotations: cr.PodAnnotations(),
				},
				Spec: *podSpec,
			},
		},
	}

	if cr.Spec.PersistentVolumeClaimRetentionPolicy != nil {
		stsSpec.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.PersistentVolumeClaimRetentionPolicy
	}
	build.StatefulSetAddCommonParams(stsSpec, &cr.Spec.CommonAppsParams)

	if cr.Spec.TmpDataPath == nil {
		if err := cr.Spec.Storage.IntoSTSVolume(tmpDataVolumeName, &stsSpec.Spec); err != nil {
			return nil, err
		}
	}
	stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.ClaimTemplates...)
	return stsSpec, nil
}

func newPodSpec(cr *vmv1.VTAgent) (*corev1.PodSpec, error) {
	var args []string

	if rwArgs, err := buildRemoteWriteArgs(cr); err != nil {
		return nil, fmt.Errorf("failed to build remote write args: %w", err)
	} else {
		args = append(args, rwArgs...)
	}

	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	args = build.AddOTLPGRPCArgsTo(args, cr.Spec.GRPCSpec, tlsServerConfigMountPath)

	var vtMounts []corev1.VolumeMount
	var volumes []corev1.Volume
	tmpDataPath := defaultTmpDataPath

	if cr.Spec.TmpDataPath == nil {
		vtMounts = append(vtMounts,
			corev1.VolumeMount{
				Name:      tmpDataVolumeName,
				MountPath: tmpDataPath,
			},
		)
	} else {
		tmpDataPath = *cr.Spec.TmpDataPath
	}
	args = append(args, fmt.Sprintf("-tmpDataPath=%s", tmpDataPath))

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)
	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AddOTLPGRPCPortTo(ports, cr.Spec.GRPCSpec)

	vtMounts = append(vtMounts, cr.Spec.VolumeMounts...)
	volumes = append(volumes, cr.Spec.Volumes...)
	volumes, vtMounts = build.AddOTLPGRPCTLSConfigToVolumes(volumes, vtMounts, cr.Spec.GRPCSpec, tlsServerConfigMountPath)

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vtMounts = append(vtMounts, corev1.VolumeMount{
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
		cvm := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		}
		vtMounts = append(vtMounts, cvm)
	}
	volumes, vtMounts = addRemoteWriteAssetsToVolumes(volumes, vtMounts, cr)
	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)

	vtagentContainer := corev1.Container{
		Name:                     "vtagent",
		Image:                    cr.Spec.Image.Reference(),
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		VolumeMounts:             vtMounts,
		Resources:                cr.Spec.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	build.Probe(&vtagentContainer, cr, &cr.Spec.CommonAppsParams)
	build.Lifecycle(&vtagentContainer, &cr.Spec.CommonAppsParams)
	var operatorContainers []corev1.Container
	var ic []corev1.Container
	var err error
	ic, err = k8stools.MergePatchContainers(ic, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
	}

	operatorContainers = append(operatorContainers, vtagentContainer)
	build.AddStrictSecuritySettingsToContainers(operatorContainers, &cr.Spec.CommonAppsParams)

	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
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

	return &corev1.PodSpec{
		Volumes:            volumes,
		InitContainers:     ic,
		Containers:         containers,
		ServiceAccountName: cr.GetServiceAccountName(),
	}, nil
}

func remoteWriteAssetVolumeName(secretName string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(secretName))
	suffix := fmt.Sprintf("-%08x", h.Sum32())
	base := k8stools.SanitizeVolumeName("rw-secret-" + secretName)
	if maxBase := validation.DNS1123LabelMaxLength - len(suffix); len(base) > maxBase {
		base = base[:maxBase]
	}
	return strings.TrimRight(base, "-") + suffix
}

func addRemoteWriteAssetsToVolumes(dstVolumes []corev1.Volume, dstMounts []corev1.VolumeMount, cr *vmv1.VTAgent) ([]corev1.Volume, []corev1.VolumeMount) {
	addSecretVolume := func(sr *corev1.SecretKeySelector) {
		name := remoteWriteAssetVolumeName(sr.Name)
		for _, dst := range dstVolumes {
			if dst.Name == name {
				return
			}
		}
		dstVolumes = append(dstVolumes, corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: sr.Name,
				},
			},
		})
	}
	addSecretMount := func(sr *corev1.SecretKeySelector) {
		name := remoteWriteAssetVolumeName(sr.Name)
		for _, dst := range dstMounts {
			if dst.Name == name {
				return
			}
		}
		dstMounts = append(dstMounts, corev1.VolumeMount{
			Name:      name,
			MountPath: fmt.Sprintf("%s/%s", remoteWriteAssetsMounthPath, sr.Name),
		})
	}
	addSecretVolumeMount := func(sr *corev1.SecretKeySelector) {
		if sr == nil {
			return
		}
		addSecretMount(sr)
		addSecretVolume(sr)
	}
	for _, rw := range cr.Spec.RemoteWrite {
		if rw.TLSConfig != nil {
			addSecretVolumeMount(rw.TLSConfig.CASecret)
			addSecretVolumeMount(rw.TLSConfig.CertSecret)
			addSecretVolumeMount(rw.TLSConfig.KeySecret)
		}
		addSecretVolumeMount(rw.BearerTokenSecret)
		if rw.BasicAuth != nil {
			if len(rw.BasicAuth.Username.Name) > 0 {
				addSecretVolumeMount(&rw.BasicAuth.Username)
			}
			if len(rw.BasicAuth.Password.Name) > 0 {
				addSecretVolumeMount(&rw.BasicAuth.Password)
			}
		}
		if rw.OAuth2 != nil {
			addSecretVolumeMount(rw.OAuth2.ClientIDSecret)
			addSecretVolumeMount(rw.OAuth2.ClientSecret)
		}
	}
	return dstVolumes, dstMounts
}

func buildRemoteWriteArgs(cr *vmv1.VTAgent) ([]string, error) {
	// do not limit maxDiskUsage by default
	// it's better to align behavior with vtagent defaults
	var maxDiskUsage string
	if cr.Spec.RemoteWriteSettings != nil && cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL != nil {
		maxDiskUsage = cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL.String()
	}

	var args []string
	var hasAnyDiskUsagesSet bool
	var storageLimit int64

	if cr.Spec.TmpDataPath == nil && cr.Spec.Storage != nil {
		storage := cr.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests.Storage()
		if !storage.IsZero() {
			storageInt, ok := storage.AsInt64()
			if ok {
				storageLimit = storageInt
			}
		}
	}

	if len(cr.Spec.RemoteWrite) > 0 {
		remoteTargets := cr.Spec.RemoteWrite
		url := build.NewEmptyFlag("-remoteWrite.url")
		format := build.NewEmptyFlag("-remoteWrite.format")
		authUserFile := build.NewEmptyFlag("-remoteWrite.basicAuth.usernameFile")
		authPasswordFile := build.NewEmptyFlag("-remoteWrite.basicAuth.passwordFile")
		bearerTokenFile := build.NewEmptyFlag("-remoteWrite.bearerTokenFile")
		sendTimeout := build.NewEmptyFlag("-remoteWrite.sendTimeout")
		tlsCAs := build.NewEmptyFlag("-remoteWrite.tlsCAFile")
		tlsCerts := build.NewEmptyFlag("-remoteWrite.tlsCertFile")
		tlsKeys := build.NewEmptyFlag("-remoteWrite.tlsKeyFile")
		tlsInsecure := build.NewFlag("-remoteWrite.tlsInsecureSkipVerify", "false")
		tlsServerName := build.NewEmptyFlag("-remoteWrite.tlsServerName")
		oauth2ClientID := build.NewEmptyFlag("-remoteWrite.oauth2.clientID")
		oauth2ClientSecretFile := build.NewEmptyFlag("-remoteWrite.oauth2.clientSecretFile")
		oauth2Scopes := build.NewEmptyFlag("-remoteWrite.oauth2.scopes")
		oauth2EndpointParams := build.NewEmptyFlag("-remoteWrite.oauth2.endpointParams")

		oauth2TokenURL := build.NewEmptyFlag("-remoteWrite.oauth2.tokenUrl")
		headers := build.NewFlag("-remoteWrite.headers", "''")
		proxyURL := build.NewEmptyFlag("-remoteWrite.proxyURL")

		var maxDiskUsagesPerRW []string

		userSetGlobalMaxDiskUsage := cr.Spec.RemoteWriteSettings != nil && cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL != nil
		if storageLimit > 0 && !userSetGlobalMaxDiskUsage {
			maxDiskUsage = strconv.FormatInt((storageLimit)/int64(len(remoteTargets)), 10)
		}
		for i, rw := range remoteTargets {
			url.Add(rw.URL, i)
			if rw.Format != nil {
				format.Add(*rw.Format, i)
			}
			if rw.TLSConfig != nil {
				if len(rw.TLSConfig.CAFile) > 0 {
					tlsCAs.Add(rw.TLSConfig.CAFile, i)
				} else {
					tlsCAs.Add(formatSecretSelectorKeyPath(rw.TLSConfig.CASecret), i)
				}

				if len(rw.TLSConfig.CertFile) > 0 {
					tlsCerts.Add(rw.TLSConfig.CertFile, i)
				} else {
					tlsCerts.Add(formatSecretSelectorKeyPath(rw.TLSConfig.CertSecret), i)
				}
				if len(rw.TLSConfig.KeyFile) > 0 {
					tlsKeys.Add(rw.TLSConfig.KeyFile, i)
				} else {
					tlsKeys.Add(formatSecretSelectorKeyPath(rw.TLSConfig.KeySecret), i)
				}
				if rw.TLSConfig.InsecureSkipVerify {
					tlsInsecure.Add("true", i)
				}
				tlsServerName.Add(rw.TLSConfig.ServerName, i)
			}
			if len(rw.BearerTokenPath) > 0 {
				bearerTokenFile.Add(rw.BearerTokenPath, i)
			} else {
				bearerTokenFile.Add(formatSecretSelectorKeyPath(rw.BearerTokenSecret), i)
			}
			if rw.BasicAuth != nil {
				if len(rw.BasicAuth.Username.Name) > 0 {
					authUserFile.Add(formatSecretSelectorKeyPath(&rw.BasicAuth.Username), i)
				}
				if len(rw.BasicAuth.PasswordFile) > 0 {
					authPasswordFile.Add(rw.BasicAuth.PasswordFile, i)
				} else if len(rw.BasicAuth.Password.Name) > 0 {
					authPasswordFile.Add(formatSecretSelectorKeyPath(&rw.BasicAuth.Password), i)
				}
			}
			if rw.SendTimeout != nil {
				sendTimeout.Add(*rw.SendTimeout, i)
			}
			if len(rw.Headers) > 0 {
				value := ""
				for _, headerValue := range rw.Headers {
					value += headerValue + "^^"
				}
				value = strings.TrimSuffix(value, "^^")
				headers.Add(fmt.Sprintf("'%s'", value), i)
			}
			if rw.OAuth2 != nil {
				if len(rw.OAuth2.TokenURL) > 0 {
					oauth2TokenURL.Add(rw.OAuth2.TokenURL, i)
				}
				if len(rw.OAuth2.Scopes) > 0 {
					oauth2Scopes.Add(strings.Join(rw.OAuth2.Scopes, ";"), i)
				}
				if len(rw.OAuth2.ClientSecretFile) > 0 {
					oauth2ClientSecretFile.Add(rw.OAuth2.ClientSecretFile, i)
				} else {
					oauth2ClientSecretFile.Add(formatSecretSelectorKeyPath(rw.OAuth2.ClientSecret), i)
				}
				if len(rw.OAuth2.ClientIDFile) > 0 {
					oauth2ClientID.Add(rw.OAuth2.ClientIDFile, i)
				} else {
					oauth2ClientID.Add(formatSecretSelectorKeyPath(rw.OAuth2.ClientIDSecret), i)
				}
				if len(rw.OAuth2.EndpointParams) > 0 {
					jsonData, err := json.Marshal(rw.OAuth2.EndpointParams)
					if err != nil {
						return nil, fmt.Errorf("cannot marshal oauth2.EndpointParams as a json: %w", err)
					}
					escapedJSON := strings.ReplaceAll(string(jsonData), `\`, `\\`)
					escapedJSON = strings.ReplaceAll(escapedJSON, "'", "\\'")
					oauth2EndpointParams.Add(fmt.Sprintf("'%s'", escapedJSON), i)
				}
			}
			if rw.MaxDiskUsage != nil {
				maxDiskUsagesPerRW = append(maxDiskUsagesPerRW, rw.MaxDiskUsage.String())
				hasAnyDiskUsagesSet = true
			} else {
				maxDiskUsagesPerRW = append(maxDiskUsagesPerRW, maxDiskUsage)
			}
			if rw.ProxyURL != nil {
				proxyURL.Add(*rw.ProxyURL, i)
			}
		}
		maxDiskUsagePerURL := build.NewFlag("-remoteWrite.maxDiskUsagePerURL", maxDiskUsage)
		if hasAnyDiskUsagesSet {
			for i, usage := range maxDiskUsagesPerRW {
				maxDiskUsagePerURL.Add(usage, i)
			}
		}

		totalCount := len(remoteTargets)
		args = build.AppendFlagsToArgs(args, totalCount, url, format, authUserFile, bearerTokenFile, tlsInsecure, sendTimeout, proxyURL)
		args = build.AppendFlagsToArgs(args, totalCount, tlsServerName, tlsKeys, tlsCerts, tlsCAs)
		args = build.AppendFlagsToArgs(args, totalCount, oauth2ClientID, oauth2ClientSecretFile, oauth2Scopes, oauth2TokenURL, oauth2EndpointParams)
		args = build.AppendFlagsToArgs(args, totalCount, headers, authPasswordFile, maxDiskUsagePerURL)
	}

	if cr.Spec.RemoteWriteSettings != nil {
		rws := cr.Spec.RemoteWriteSettings
		if rws.FlushInterval != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.flushInterval=%s", *rws.FlushInterval))
		}
		if rws.Queues != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.queues=%d", *rws.Queues))
		}
		if rws.ShowURL != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.showURL=%t", *rws.ShowURL))
		}
		if rws.TmpDataPath != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.tmpDataPath=%s", *rws.TmpDataPath))
		}
		if rws.MaxBlockSize != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.maxBlockSize=%s", *rws.MaxBlockSize))
		}
	}

	if !hasAnyDiskUsagesSet && len(maxDiskUsage) > 0 {
		args = append(args, fmt.Sprintf("-remoteWrite.maxDiskUsagePerURL=%s", maxDiskUsage))
	}
	return args, nil
}

func createOrUpdateVPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTAgent) error {
	if cr.Spec.VPA == nil {
		return nil
	}
	targetRef := autoscalingv1.CrossVersionObjectReference{
		Name:       cr.PrefixedName(),
		Kind:       string(vmv1beta1.WorkloadKindStatefulSet),
		APIVersion: "apps/v1",
	}
	newVPA := build.VPA(cr, targetRef, cr.Spec.VPA)
	var prevVPA *vpav1.VerticalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VPA != nil {
		prevTargetRef := autoscalingv1.CrossVersionObjectReference{
			Name:       prevCR.PrefixedName(),
			Kind:       string(vmv1beta1.WorkloadKindStatefulSet),
			APIVersion: "apps/v1",
		}
		prevVPA = build.VPA(prevCR, prevTargetRef, prevCR.Spec.VPA)
	}
	owner := cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1.VTAgent) error {
	svcName := cr.PrefixedName()
	keepServices := sets.New(svcName)
	keepPodScrapes := sets.New[string]()
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		keepPodScrapes.Insert(svcName)
	}
	if cr.Spec.ServiceSpec != nil && !cr.Spec.ServiceSpec.UseAsDefault {
		extraSvcName := cr.Spec.ServiceSpec.NameOrDefault(svcName)
		keepServices.Insert(extraSvcName)
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, cr, keepServices, true); err != nil {
		return fmt.Errorf("cannot remove services: %w", err)
	}
	if err := finalize.RemoveOrphanedVMPodScrapes(ctx, rclient, cr, keepPodScrapes, true); err != nil {
		return fmt.Errorf("cannot remove podScrapes: %w", err)
	}
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	var objsToRemove []client.Object
	if cr.Spec.PodDisruptionBudget == nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if config.MustGetBaseConfig().VPAAPIEnabled && cr.Spec.VPA == nil {
		objsToRemove = append(objsToRemove, &vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta})
	}
	if cr.Spec.NetworkPolicy == nil {
		objsToRemove = append(objsToRemove, &networkingv1.NetworkPolicy{ObjectMeta: objMeta})
	}
	if !cr.IsOwnsServiceAccount() {
		objsToRemove = append(objsToRemove, &corev1.ServiceAccount{ObjectMeta: objMeta})
	}
	return finalize.SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, cr)
}

func formatSecretSelectorKeyPath(secretKey *corev1.SecretKeySelector) string {
	if secretKey == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s", remoteWriteAssetsMounthPath, secretKey.Name, secretKey.Key)
}
