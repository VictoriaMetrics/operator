package vlagent

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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	vlAgentConfDir                  = "/etc/vlagent/config"
	vlAgentConfOutDir               = "/etc/vlagent/config_out"
	vlAgentPersistentQueueDir       = "/tmp/vlagent-remotewrite-data"
	vlAgentPersistentQueueSTSDir    = "/vlagent_pq/vlagent-remotewrite-data"
	vlAgentPersistentQueueMountName = "persistent-queue-data"

	shardNumPlaceholder      = "%SHARD_NUM%"
	tlsAssetsDir             = "/etc/vlagent-tls/certs"
	tlsServerConfigMountPath = "/etc/vm/tls-server-secrets"
	defaultMaxDiskUsage      = "1073741824"

	kubeNodeEnvName     = "KUBE_NODE_NAME"
	kubeNodeEnvTemplate = "%{" + kubeNodeEnvName + "}"
)

// To save compatibility in the single-shard version still need to fill in %SHARD_NUM% placeholder
var defaultPlaceholders = map[string]string{shardNumPlaceholder: "0"}

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLAgent) (*corev1.Service, error) {

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			if prevCR.Spec.Mode == vmv1.StatefulSetMode {
				svc.Spec.ClusterIP = "None"
			}
			syslogSpec := prevCR.Spec.SyslogSpec
			build.AddSyslogPortsToService(svc, syslogSpec)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, cr.Spec.ServiceSpec)
	}
	newService := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		if cr.Spec.Mode == vmv1.StatefulSetMode {
			svc.Spec.ClusterIP = "None"
		}
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
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vlagent: %w", err)
	}
	return newService, nil
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

	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		if cr.Spec.Mode == vmv1.DaemonSetMode {
			ps := build.VMPodScrapeForObjectWithSpec(cr, cr.Spec.ServiceScrapeSpec, cr.Spec.ExtraArgs)
			err = reconcile.VMPodScrapeForCRD(ctx, rclient, ps)
		} else {
			err = reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		}
		if err != nil {
			return fmt.Errorf("cannot create or update scrape object: %w", err)
		}
	}

	ac := getAssetsCache(ctx, rclient, cr)
	if err = createOrUpdateConfigurationSecret(ctx, rclient, cr, prevCR, ac); err != nil {
		return err
	}
	if cr.Spec.PodDisruptionBudget != nil && cr.Spec.Mode != vmv1.DaemonSetMode {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		err = reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB)
		if err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vlagent: %w", err)
		}
	}

	var prevDeploy runtime.Object

	if prevCR != nil {
		prevDeploy, err = newDeploy(prevCR, ac)
		if err != nil {
			return fmt.Errorf("cannot build new deploy for vlagent: %w", err)
		}
	}
	newDeploy, err := newDeploy(cr, ac)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vlagent: %w", err)
	}
	return createOrUpdateDeploy(ctx, rclient, cr, prevCR, newDeploy, prevDeploy)
}

func createOrUpdateDeploy(ctx context.Context, rclient client.Client, cr, _ *vmv1.VLAgent, newDeploy, prevObjectSpec runtime.Object) error {
	deploymentNames := make(map[string]struct{})
	stsNames := make(map[string]struct{})

	var err error
	switch newDeploy := newDeploy.(type) {
	case *appsv1.Deployment:
		var prevDeploy *appsv1.Deployment
		if prevObjectSpec != nil {
			prevAppObject, ok := prevObjectSpec.(*appsv1.Deployment)
			if ok {
				prevDeploy = prevAppObject
				prevDeploy, err = k8stools.RenderPlaceholders(prevDeploy, defaultPlaceholders)
				if err != nil {
					return fmt.Errorf("cannot fill placeholders for prev deployment in vlagent: %w", err)
				}
			}
		}

		newDeploy, err = k8stools.RenderPlaceholders(newDeploy, defaultPlaceholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for deployment in vlagent: %w", err)
		}
		if err := reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false); err != nil {
			return err
		}
		deploymentNames[newDeploy.Name] = struct{}{}
	case *appsv1.StatefulSet:
		var prevSTS *appsv1.StatefulSet
		if prevObjectSpec != nil {
			prevAppObject, ok := prevObjectSpec.(*appsv1.StatefulSet)
			if ok {
				prevSTS = prevAppObject
				prevSTS, err = k8stools.RenderPlaceholders(prevSTS, defaultPlaceholders)
				if err != nil {
					return fmt.Errorf("cannot fill placeholders for prev sts in vlagent: %w", err)
				}
			}
		}
		newDeploy, err = k8stools.RenderPlaceholders(newDeploy, defaultPlaceholders)
		if err != nil {
			return fmt.Errorf("cannot fill placeholders for sts in vlagent: %w", err)
		}
		stsOpts := reconcile.STSOptions{
			HasClaim:       len(newDeploy.Spec.VolumeClaimTemplates) > 0,
			SelectorLabels: cr.SelectorLabels,
		}
		if err := reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newDeploy, prevSTS); err != nil {
			return err
		}
		stsNames[newDeploy.Name] = struct{}{}
	case *appsv1.DaemonSet:
		var prevDeploy *appsv1.DaemonSet
		if prevObjectSpec != nil {
			prevAppObject, ok := prevObjectSpec.(*appsv1.DaemonSet)
			if ok {
				prevDeploy = prevAppObject
			}
		}
		if err := reconcile.DaemonSet(ctx, rclient, newDeploy, prevDeploy); err != nil {
			return err
		}
	default:
		panic(fmt.Sprintf("BUG: unexpected deploy object type: %T", newDeploy))
	}
	if err := finalize.RemoveOrphanedDeployments(ctx, rclient, cr, deploymentNames); err != nil {
		return err
	}
	if err := finalize.RemoveOrphanedSTSs(ctx, rclient, cr, stsNames); err != nil {
		return err
	}
	if err := removeStaleDaemonSet(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot remove vlagent daemonSet: %w", err)
	}
	return nil
}

// newDeploy builds vlagent deployment spec.
func newDeploy(cr *vmv1.VLAgent, ac *build.AssetsCache) (runtime.Object, error) {

	podSpec, err := makeSpec(cr, ac)
	if err != nil {
		return nil, err
	}

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	switch cr.Spec.Mode {
	case vmv1.DaemonSetMode:
		dsSpec := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cr.PrefixedName(),
				Namespace:       cr.Namespace,
				Labels:          cr.AllLabels(),
				Annotations:     cr.AnnotationsFiltered(),
				OwnerReferences: cr.AsOwner(),
				Finalizers:      []string{vmv1beta1.FinalizerName},
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: cr.SelectorLabels(),
				},
				MinReadySeconds: cr.Spec.MinReadySeconds,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      cr.PodLabels(),
						Annotations: cr.PodAnnotations(),
					},
					Spec: *podSpec,
				},
			},
		}
		build.DaemonSetAddCommonParams(dsSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
		dsSpec.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(dsSpec.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)

		return dsSpec, nil
	// fast path, use sts
	case vmv1.StatefulSetMode:
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
					Type: cr.Spec.StatefulRollingUpdateStrategy,
				},
				PodManagementPolicy: appsv1.ParallelPodManagement,
				ServiceName:         buildSTSServiceName(cr),
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
		stsSpec.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(stsSpec.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)
		cr.Spec.StatefulStorage.IntoSTSVolume(vlAgentPersistentQueueMountName, &stsSpec.Spec)
		stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.ClaimTemplates...)
		return stsSpec, nil
	default:
		strategyType := appsv1.RollingUpdateDeploymentStrategyType
		if cr.Spec.UpdateStrategy != nil {
			strategyType = *cr.Spec.UpdateStrategy
		}
		depSpec := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cr.PrefixedName(),
				Namespace:       cr.Namespace,
				Labels:          cr.AllLabels(),
				Annotations:     cr.AnnotationsFiltered(),
				OwnerReferences: cr.AsOwner(),
				Finalizers:      []string{vmv1beta1.FinalizerName},
			},
			Spec: appsv1.DeploymentSpec{
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
					Spec: *podSpec,
				},
			},
		}
		build.DeploymentAddCommonParams(depSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
		depSpec.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(depSpec.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)
		return depSpec, nil
	}
}

func buildSTSServiceName(cr *vmv1.VLAgent) string {
	// set service name for sts if additional service is headless
	if cr.Spec.ServiceSpec != nil &&
		!cr.Spec.ServiceSpec.UseAsDefault &&
		cr.Spec.ServiceSpec.Spec.ClusterIP == corev1.ClusterIPNone {
		return cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName())
	}
	return cr.PrefixedName()
}

func makeSpec(cr *vmv1.VLAgent, ac *build.AssetsCache) (*corev1.PodSpec, error) {
	var args []string

	if rwArgs, err := buildRemoteWriteArgs(cr, ac); err != nil {
		return nil, fmt.Errorf("failed to build remote write args: %w", err)
	} else {
		args = append(args, rwArgs...)
	}

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

	if cr.Spec.Mode == vmv1.DaemonSetMode {
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

	var agentVolumeMounts []corev1.VolumeMount
	var volumes []corev1.Volume

	if cr.Spec.SyslogSpec != nil {
		args = build.AddSyslogArgsTo(args, cr.Spec.SyslogSpec, tlsServerConfigMountPath)
		volumes, agentVolumeMounts = build.AddSyslogTLSConfigToVolumes(volumes, agentVolumeMounts, cr.Spec.SyslogSpec, tlsServerConfigMountPath)
		ports = build.AddSyslogPortsTo(ports, cr.Spec.SyslogSpec)
	}

	volumes, agentVolumeMounts = ac.VolumeTo(volumes, agentVolumeMounts)
	// mount data path any way, even if user changes its value
	// we cannot rely on value of remoteWriteSettings.
	pqMountPath := vlAgentPersistentQueueDir
	if cr.Spec.Mode == vmv1.StatefulSetMode {
		pqMountPath = vlAgentPersistentQueueSTSDir
	}
	agentVolumeMounts = append(agentVolumeMounts,
		corev1.VolumeMount{
			Name:      vlAgentPersistentQueueMountName,
			MountPath: pqMountPath,
		},
	)
	agentVolumeMounts = append(agentVolumeMounts, cr.Spec.VolumeMounts...)
	// in case for sts, we have to use persistentVolumeClaimTemplate instead
	if cr.Spec.Mode != vmv1.StatefulSetMode {
		volumes = append(volumes, corev1.Volume{
			Name: vlAgentPersistentQueueMountName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

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

	volumes, agentVolumeMounts = build.LicenseVolumeTo(volumes, agentVolumeMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

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

	build.AddServiceAccountTokenVolumeMount(&vlagentContainer, &cr.Spec.CommonApplicationDeploymentParams)
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	vlagentContainer = build.Probe(vlagentContainer, cr)

	var operatorContainers []corev1.Container
	var ic []corev1.Container
	// conditional add config reloader container
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

func buildRemoteWriteArgs(cr *vmv1.VLAgent, ac *build.AssetsCache) ([]string, error) {
	maxDiskUsage := defaultMaxDiskUsage
	if cr.Spec.RemoteWriteSettings != nil && cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL != nil {
		maxDiskUsage = cr.Spec.RemoteWriteSettings.MaxDiskUsagePerURL.String()
	}

	var args []string
	var hasAnyDiskUsagesSet bool
	var storageLimit int64

	pqMountPath := vlAgentPersistentQueueDir
	if cr.Spec.Mode == vmv1.StatefulSetMode {
		pqMountPath = vlAgentPersistentQueueSTSDir
		if cr.Spec.StatefulStorage != nil {
			if storage, ok := cr.Spec.StatefulStorage.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
				storageInt, ok := storage.AsInt64()
				if ok {
					storageLimit = storageInt
				}
			}
		}
	}

	if len(cr.Spec.RemoteWrite) > 0 {
		remoteTargets := cr.Spec.RemoteWrite
		url := build.NewFlag("-remoteWrite.url", "")
		authUser := build.NewFlag("-remoteWrite.basicAuth.username", `""`)
		authPasswordFile := build.NewFlag("-remoteWrite.basicAuth.passwordFile", "")
		bearerTokenFile := build.NewFlag("-remoteWrite.bearerTokenFile", `""`)
		sendTimeout := build.NewFlag("-remoteWrite.sendTimeout", "")
		tlsCAs := build.NewFlag("-remoteWrite.tlsCAFile", "")
		tlsCerts := build.NewFlag("-remoteWrite.tlsCertFile", "")
		tlsKeys := build.NewFlag("-remoteWrite.tlsKeyFile", "")
		tlsInsecure := build.NewFlag("-remoteWrite.tlsInsecureSkipVerify", "false")
		tlsServerName := build.NewFlag("-remoteWrite.tlsServerName", "")
		oauth2ClientID := build.NewFlag("-remoteWrite.oauth2.clientID", "")
		oauth2ClientSecretFile := build.NewFlag("-remoteWrite.oauth2.clientSecretFile", "")
		oauth2Scopes := build.NewFlag("-remoteWrite.oauth2.scopes", "")
		oauth2TokenURL := build.NewFlag("-remoteWrite.oauth2.tokenUrl", "")
		headers := build.NewFlag("-remoteWrite.headers", "")
		proxyURL := build.NewFlag("-remoteWrite.proxyURL", "")

		var maxDiskUsagesPerRW []string

		if storageLimit > 0 && maxDiskUsage == defaultMaxDiskUsage {
			// conditionally change default value of maxDiskUsage
			// user defined value must have prioirty over automatically calculated.
			//
			// it's fine to have over-provisioing of total disk usage
			// however, we should return warning during validation.
			maxDiskUsage = strconv.FormatInt((storageLimit)/int64(len(remoteTargets)), 10)
		}
		for i, rw := range remoteTargets {
			url.Add(rw.URL, i)
			if rw.TLSConfig != nil {
				creds, err := ac.BuildTLSCreds(cr.Namespace, rw.TLSConfig)
				if err != nil {
					return nil, err
				}
				tlsCAs.Add(creds.CAFile, i)
				tlsCerts.Add(creds.CertFile, i)
				tlsKeys.Add(creds.KeyFile, i)
				if rw.TLSConfig.InsecureSkipVerify {
					tlsInsecure.Add("true", i)
				}
				tlsServerName.Add(rw.TLSConfig.ServerName, i)
			}
			if rw.BasicAuth != nil {
				user, err := ac.LoadKeyFromSecret(cr.Namespace, &rw.BasicAuth.Username)
				if err != nil {
					return nil, fmt.Errorf("cannot load BasicAuth username: %w", err)
				}
				authUser.Add(strconv.Quote(user), i)
				if len(rw.BasicAuth.Password.Name) > 0 {
					passFile, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, &rw.BasicAuth.Password)
					if err != nil {
						return nil, fmt.Errorf("cannot load BasicAuth password: %w", err)
					}
					authPasswordFile.Add(passFile, i)
				}
				if len(rw.BasicAuth.PasswordFile) > 0 {
					authPasswordFile.Add(rw.BasicAuth.PasswordFile, i)
				}
			}
			if rw.BearerTokenSecret != nil && rw.BearerTokenSecret.Name != "" {
				value, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, rw.BearerTokenSecret)
				if err != nil {
					return nil, fmt.Errorf("cannot load BearerTokenSecret: %w", err)
				}
				bearerTokenFile.Add(strconv.Quote(value), i)
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
				headers.Add(value, i)
			}
			if rw.OAuth2 != nil {
				if len(rw.OAuth2.TokenURL) > 0 {
					oauth2TokenURL.Add(rw.OAuth2.TokenURL, i)
				}
				if len(rw.OAuth2.Scopes) > 0 {
					oauth2Scopes.Add(strings.Join(rw.OAuth2.Scopes, ","), i)
				}
				if len(rw.OAuth2.ClientSecretFile) > 0 {
					oauth2ClientSecretFile.Add(rw.OAuth2.ClientSecretFile, i)
				}
				if rw.OAuth2.ClientSecret != nil {
					oaSecretKeyFile, err := ac.LoadPathFromSecret(build.SecretConfigResourceKind, cr.Namespace, rw.OAuth2.ClientSecret)
					if err != nil {
						return nil, fmt.Errorf("cannot load OAuth2 ClientSecret: %w", err)
					}
					oauth2ClientSecretFile.Add(oaSecretKeyFile, i)
				}
				secret, err := ac.LoadKeyFromSecretOrConfigMap(cr.Namespace, &rw.OAuth2.ClientID)
				if err != nil {
					return nil, err
				}
				oauth2ClientID.Add(secret, i)
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
		args = build.AppendFlagsToArgs(args, totalCount, oauth2ClientID, oauth2ClientSecretFile, oauth2Scopes, oauth2TokenURL)
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
			args = append(args, fmt.Sprintf("-remoteWrite.maxBlockSize=%d", *rws.MaxBlockSize))
		}
	}

	args = append(args, fmt.Sprintf("-remoteWrite.tmpDataPath=%s", pqMountPath))
	if !hasAnyDiskUsagesSet {
		args = append(args, fmt.Sprintf("-remoteWrite.maxDiskUsagePerURL=%s", maxDiskUsage))
	}
	return args, nil
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLAgent) error {
	if prevCR == nil {
		return nil
	}
	// TODO check for stream aggr removed

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

	if prevCR.Spec.Mode != vmv1.DaemonSetMode && cr.Spec.Mode == vmv1.DaemonSetMode {
		// transit into DaemonSetMode
		if cr.Spec.PodDisruptionBudget != nil {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
				return fmt.Errorf("cannot delete PDB from prev state: %w", err)
			}
		}
		if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
				return fmt.Errorf("cannot delete VMServiceScrape during daemonset transition: %w", err)
			}
		}
	}

	if prevCR.Spec.Mode == vmv1.DaemonSetMode && cr.Spec.Mode != vmv1.DaemonSetMode {
		// transit into non DaemonSetMode
		if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMPodScrape{ObjectMeta: objMeta}); err != nil {
				return fmt.Errorf("cannot delete VMPodScrape during transition for non-daemonsetMode: %w", err)
			}
		}
	}

	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}

func removeStaleDaemonSet(ctx context.Context, rclient client.Client, cr *vmv1.VLAgent) error {
	if cr.Spec.Mode == vmv1.DaemonSetMode {
		return nil
	}
	ds := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.PrefixedName(),
			Namespace: cr.Namespace,
		},
	}
	return finalize.SafeDeleteWithFinalizer(ctx, rclient, &ds)
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1.VLAgent) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.SecretConfigResourceKind: {
			MountDir:   vlAgentConfDir,
			SecretName: build.ResourceName(build.SecretConfigResourceKind, cr),
		},
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}

func createOrUpdateConfigurationSecret(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLAgent, ac *build.AssetsCache) error {
	// HACK: makeSpec could load content into ac and it must be called
	// before secret config reconcile
	if _, err := makeSpec(cr, ac); err != nil {
		return err
	}

	for kind, secret := range ac.GetOutput() {
		var prevSecretMeta *metav1.ObjectMeta
		if prevCR != nil {
			prevSecretMeta = ptr.To(build.ResourceMeta(kind, prevCR))
		}
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		secret.Annotations = map[string]string{
			"generated": "true",
		}
		if err := reconcile.Secret(ctx, rclient, &secret, prevSecretMeta); err != nil {
			return err
		}
	}

	return nil
}
