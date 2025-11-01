package vlagent

import (
	"context"
	"encoding/json"
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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	vlAgentPersistentQueueSTSDir    = "/vlagent_pq/vlagent-remotewrite-data"
	vlAgentPersistentQueueMountName = "persistent-queue-data"

	remoteWriteAssetsMounthPath = "/etc/vl/remote-write-assets"
	tlsServerConfigMountPath    = "/etc/vl/tls-server-secrets"
)

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLAgent) error {

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			syslogSpec := prevCR.Spec.SyslogSpec
			build.AddSyslogPortsToService(svc, syslogSpec)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, cr.Spec.ServiceSpec)
	}
	newService := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		syslogSpec := cr.Spec.SyslogSpec
		build.AddSyslogPortsToService(svc, syslogSpec)
	})

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, cr.Spec.ServiceSpec)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vlagent additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vlagent: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return fmt.Errorf("cannot reconcile service for vlagent: %w", err)
	}
	return nil
}

// CreateOrUpdate creates deployment for vlagent and configures it
// waits for healthy state
func CreateOrUpdate(ctx context.Context, cr *vmv1.VLAgent, rclient client.Client) error {
	var prevCR *vmv1.VLAgent
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if err := deletePrevStateResources(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot delete objects from prev state: %w", err)
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

	if err := createOrUpdateService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}

	cfg := config.MustGetBaseConfig()
	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, disableSelfScrape) {
		ps := build.VMPodScrapeForObjectWithSpec(cr, cr.Spec.ServiceScrapeSpec, cr.Spec.ExtraArgs)
		if err := reconcile.VMPodScrapeForCRD(ctx, rclient, ps); err != nil {
			return fmt.Errorf("cannot create or update scrape object: %w", err)
		}
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB); err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vlagent: %w", err)
		}
	}

	var prevDeploy *appsv1.StatefulSet

	if prevCR != nil {
		var err error
		prevDeploy, err = newDeploy(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build new deploy for vlagent: %w", err)
		}
	}
	newDeploy, err := newDeploy(cr)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vlagent: %w", err)
	}
	return createOrUpdateDeploy(ctx, rclient, cr, newDeploy, prevDeploy)
}

func createOrUpdateDeploy(ctx context.Context, rclient client.Client, cr *vmv1.VLAgent, newSTS, prevSTS *appsv1.StatefulSet) error {
	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSTS.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.SelectorLabels,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSTS, prevSTS)
}

func newDeploy(cr *vmv1.VLAgent) (*appsv1.StatefulSet, error) {
	podSpec, err := makeSpec(cr)
	if err != nil {
		return nil, err
	}

	cfg := config.MustGetBaseConfig()
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, cfg.EnableStrictSecurity)
	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
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
	build.StatefulSetAddCommonParams(stsSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)

	if cr.Spec.RemoteWriteSettings == nil || cr.Spec.RemoteWriteSettings.TmpDataPath == nil {
		cr.Spec.Storage.IntoSTSVolume(vlAgentPersistentQueueMountName, &stsSpec.Spec)
	}
	stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.ClaimTemplates...)
	return stsSpec, nil
}

func makeSpec(cr *vmv1.VLAgent) (*corev1.PodSpec, error) {
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

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)
	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})

	var agentVolumeMounts []corev1.VolumeMount
	var volumes []corev1.Volume

	if cr.Spec.SyslogSpec != nil {
		args = build.AddSyslogArgsTo(args, cr.Spec.SyslogSpec, tlsServerConfigMountPath)
		volumes, agentVolumeMounts = build.AddSyslogTLSConfigToVolumes(volumes, agentVolumeMounts, cr.Spec.SyslogSpec, tlsServerConfigMountPath)
		ports = build.AddSyslogPortsTo(ports, cr.Spec.SyslogSpec)
	}

	if cr.Spec.RemoteWriteSettings == nil || cr.Spec.RemoteWriteSettings.TmpDataPath == nil {
		// do not mount pq if user provided own path
		agentVolumeMounts = append(agentVolumeMounts,
			corev1.VolumeMount{
				Name:      vlAgentPersistentQueueMountName,
				MountPath: vlAgentPersistentQueueSTSDir,
			},
		)
	}

	agentVolumeMounts = append(agentVolumeMounts, cr.Spec.VolumeMounts...)
	volumes = append(volumes, cr.Spec.Volumes...)

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		agentVolumeMounts = append(agentVolumeMounts, corev1.VolumeMount{
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
		agentVolumeMounts = append(agentVolumeMounts, cvm)
	}
	volumes, agentVolumeMounts = addRemoteWriteAssetsToVolumes(volumes, agentVolumeMounts, cr)
	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)

	vlagentContainer := corev1.Container{
		Name:                     "vlagent",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		VolumeMounts:             agentVolumeMounts,
		Resources:                cr.Spec.Resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, cfg.EnableStrictSecurity)

	vlagentContainer = build.Probe(vlagentContainer, cr)
	var operatorContainers []corev1.Container
	var ic []corev1.Container
	var err error
	ic, err = k8stools.MergePatchContainers(ic, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
	}

	operatorContainers = append(operatorContainers, vlagentContainer)
	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, operatorContainers, useStrictSecurity)

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

func addRemoteWriteAssetsToVolumes(dstVolumes []corev1.Volume, dstMounts []corev1.VolumeMount, cr *vmv1.VLAgent) ([]corev1.Volume, []corev1.VolumeMount) {
	addSecretVolume := func(sr *corev1.SecretKeySelector) {
		name := sr.Name
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
		name := sr.Name
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
		if rw.OAuth2 != nil {
			addSecretVolumeMount(rw.OAuth2.ClientIDSecret)
			addSecretVolumeMount(rw.OAuth2.ClientSecret)
		}
	}
	return dstVolumes, dstMounts
}

func buildRemoteWriteArgs(cr *vmv1.VLAgent) ([]string, error) {
	// do not limit maxDiskUsage by default
	// it's better to align behavior with vlagent defaults
	var maxDiskUsage string
	if cr.Spec.RemoteWriteSettings != nil && cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL != nil {
		maxDiskUsage = cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL.String()
	}

	var args []string
	var hasAnyDiskUsagesSet bool
	var storageLimit int64

	pqMountPath := vlAgentPersistentQueueSTSDir
	if cr.Spec.Storage != nil {
		if storage, ok := cr.Spec.Storage.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			storageInt, ok := storage.AsInt64()
			if ok {
				storageLimit = storageInt
			}
		}
	}

	if len(cr.Spec.RemoteWrite) > 0 {
		remoteTargets := cr.Spec.RemoteWrite
		url := build.NewEmptyFlag("-remoteWrite.url")
		authUser := build.NewEmptyFlag("-remoteWrite.basicAuth.username")
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

		if storageLimit > 0 {
			// conditionally change default value of maxDiskUsage
			// user defined value must have priority over automatically calculated.
			//
			// it's fine to have over-provisioning of total disk usage
			// however, we should return warning during validation.
			maxDiskUsage = strconv.FormatInt((storageLimit)/int64(len(remoteTargets)), 10)
		}
		for i, rw := range remoteTargets {
			url.Add(rw.URL, i)
			if rw.TLSConfig != nil {
				if len(rw.TLSConfig.CAFile) > 0 {
					tlsCAs.Add(rw.TLSConfig.CAFile, i)
				} else {
					tlsCAs.Add(formatSecretSelectorKeyPath(rw.TLSConfig.CASecret), i)
				}

				tlsCerts.Add(formatSecretSelectorKeyPath(rw.TLSConfig.CertSecret), i)
				tlsKeys.Add(formatSecretSelectorKeyPath(rw.TLSConfig.KeySecret), i)
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
					oauth2EndpointParams.Add(fmt.Sprintf("'%s'", jsonData), i)
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
		args = build.AppendFlagsToArgs(args, totalCount, url, authUser, bearerTokenFile, tlsInsecure, sendTimeout, proxyURL)
		args = build.AppendFlagsToArgs(args, totalCount, tlsServerName, tlsKeys, tlsCerts, tlsCAs)
		args = build.AppendFlagsToArgs(args, totalCount, oauth2ClientID, oauth2ClientSecretFile, oauth2Scopes, oauth2TokenURL, oauth2EndpointParams)
		args = build.AppendFlagsToArgs(args, totalCount, headers, authPasswordFile, maxDiskUsagePerURL)
	}

	if cr.Spec.RemoteWriteSettings != nil {
		rws := cr.Spec.RemoteWriteSettings
		if rws.MaxDiskUsagePerURL != nil {
			maxDiskUsage = rws.MaxDiskUsagePerURL.String()
		}
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
			pqMountPath = *rws.TmpDataPath
		}
		if rws.MaxBlockSize != nil {
			args = append(args, fmt.Sprintf("-remoteWrite.maxBlockSize=%s", *rws.MaxBlockSize))
		}
	}

	args = append(args, fmt.Sprintf("-remoteWrite.tmpDataPath=%s", pqMountPath))
	if !hasAnyDiskUsagesSet && len(maxDiskUsage) > 0 {
		args = append(args, fmt.Sprintf("-remoteWrite.maxDiskUsagePerURL=%s", maxDiskUsage))
	}
	return args, nil
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLAgent) error {
	if prevCR == nil {
		return nil
	}
	prevSvc, currSvc := prevCR.Spec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && prevCR.Spec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}

	cfg := config.MustGetBaseConfig()
	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, disableSelfScrape) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, disableSelfScrape) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMPodScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}

func formatSecretSelectorKeyPath(secretKey *corev1.SecretKeySelector) string {
	if secretKey == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s", remoteWriteAssetsMounthPath, secretKey.Name, secretKey.Key)
}
