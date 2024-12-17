package vmalert

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func buildNotifierKey(idx int) string {
	return fmt.Sprintf("notifier-%d", idx)
}

func buildRemoteSecretKey(source, suffix string) string {
	return fmt.Sprintf("%s_%s", strings.ToUpper(source), strings.ToUpper(suffix))
}

// createOrUpdateVMAlertService creates service for vmalert
func createOrUpdateVMAlertService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlert) (*corev1.Service, error) {

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

func createOrUpdateVMAlertSecret(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlert, ssCache map[string]*authSecret) error {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Annotations:     cr.AnnotationsFiltered(),
			Labels:          cr.AllLabels(),
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
		},
		Data: map[string][]byte{},
	}
	addSecretKeys := func(sourcePrefix string, ha vmv1beta1.HTTPAuth) {
		ba := ssCache[sourcePrefix]
		if ba == nil {
			return
		}
		if ha.BasicAuth != nil && ba.BasicAuthCredentials != nil {
			if len(ba.Password) > 0 {
				s.Data[buildRemoteSecretKey(sourcePrefix, basicAuthPasswordKey)] = []byte(ba.Password)
			}
		}
		if ha.BearerAuth != nil && len(ba.bearerValue) > 0 {
			s.Data[buildRemoteSecretKey(sourcePrefix, bearerTokenKey)] = []byte(ba.bearerValue)
		}
		if ha.OAuth2 != nil && ba.OAuthCreds != nil {
			if len(ba.ClientSecret) > 0 {
				s.Data[buildRemoteSecretKey(sourcePrefix, oauth2SecretKey)] = []byte(ba.ClientSecret)
			}
		}
	}
	if cr.Spec.RemoteRead != nil {
		addSecretKeys(remoteReadKey, cr.Spec.RemoteRead.HTTPAuth)
	}
	if cr.Spec.RemoteWrite != nil {
		addSecretKeys(remoteWriteKey, cr.Spec.RemoteWrite.HTTPAuth)
	}
	addSecretKeys(datasourceKey, cr.Spec.Datasource.HTTPAuth)
	for idx, nf := range cr.Spec.Notifiers {
		addSecretKeys(buildNotifierKey(idx), nf.HTTPAuth)
	}
	var prevSecretMeta *metav1.ObjectMeta
	if cr.ParsedLastAppliedSpec != nil {
		prevSecretMeta = &metav1.ObjectMeta{
			Name:        prevCR.PrefixedName(),
			Annotations: prevCR.AnnotationsFiltered(),
			Labels:      prevCR.AllLabels(),
			Namespace:   prevCR.Namespace,
		}
	}

	return reconcile.Secret(ctx, rclient, s, prevSecretMeta)
}

// CreateOrUpdateVMAlert creates vmalert deployment for given CRD
func CreateOrUpdateVMAlert(ctx context.Context, cr *vmv1beta1.VMAlert, rclient client.Client, cmNames []string) error {
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

	remoteSecrets, err := loadVMAlertRemoteSecrets(ctx, rclient, cr)
	if err != nil {
		return err
	}
	// create secret for remoteSecrets
	if err := createOrUpdateVMAlertSecret(ctx, rclient, cr, prevCR, remoteSecrets); err != nil {
		return err
	}

	svc, err := createOrUpdateVMAlertService(ctx, rclient, cr, prevCR)
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

	err = createOrUpdateTLSAssetsForVMAlert(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		prevDeploy, err = newDeployForVMAlert(prevCR, cmNames, remoteSecrets)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}

	newDeploy, err := newDeployForVMAlert(cr, cmNames, remoteSecrets)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vmalert: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false)
}

// newDeployForCR returns a busybox pod with the same name/namespace as the cr
func newDeployForVMAlert(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, remoteSecrets map[string]*authSecret) (*appsv1.Deployment, error) {

	generatedSpec, err := vmAlertSpecGen(cr, ruleConfigMapNames, remoteSecrets)
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

func vmAlertSpecGen(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, remoteSecrets map[string]*authSecret) (*appsv1.DeploymentSpec, error) {

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
	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "tls-assets",
		ReadOnly:  true,
		MountPath: tlsAssetsDir,
	},
		corev1.VolumeMount{
			Name:      "remote-secrets",
			ReadOnly:  true,
			MountPath: vmalertConfigSecretsDir,
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
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
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
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	vmalertContainer = build.Probe(vmalertContainer, cr)
	vmalertContainers = append(vmalertContainers, vmalertContainer)

	vmalertContainers = buildConfigReloaderContainer(vmalertContainers, cr, ruleConfigMapNames)

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

func buildVMAlertAuthArgs(args []string, flagPrefix string, ha vmv1beta1.HTTPAuth, remoteSecrets map[string]*authSecret) []string {
	if s, ok := remoteSecrets[flagPrefix]; ok {
		// safety checks must be performed by previous code
		if ha.BasicAuth != nil {
			args = append(args, fmt.Sprintf("-%s.basicAuth.username=%s", flagPrefix, s.Username))
			if len(s.Password) > 0 {
				args = append(args, fmt.Sprintf("-%s.basicAuth.passwordFile=%s", flagPrefix, path.Join(vmalertConfigSecretsDir, buildRemoteSecretKey(flagPrefix, basicAuthPasswordKey))))
			}
			if len(ha.BasicAuth.PasswordFile) > 0 {
				args = append(args, fmt.Sprintf("-%s.basicAuth.passwordFile=%s", flagPrefix, ha.BasicAuth.PasswordFile))
			}
		}
		if ha.BearerAuth != nil {
			if len(s.bearerValue) > 0 {
				args = append(args, fmt.Sprintf("-%s.bearerTokenFile=%s", flagPrefix, path.Join(vmalertConfigSecretsDir, buildRemoteSecretKey(flagPrefix, bearerTokenKey))))
			} else if len(ha.BearerAuth.TokenFilePath) > 0 {
				args = append(args, fmt.Sprintf("-%s.bearerTokenFile=%s", flagPrefix, ha.BearerAuth.TokenFilePath))
			}
		}
		if ha.OAuth2 != nil {
			if len(ha.OAuth2.ClientSecretFile) > 0 {
				args = append(args, fmt.Sprintf("-%s.oauth2.clientSecretFile=%s", flagPrefix, ha.OAuth2.ClientSecretFile))
			} else {
				args = append(args, fmt.Sprintf("-%s.oauth2.clientSecretFile=%s", flagPrefix, path.Join(vmalertConfigSecretsDir, buildRemoteSecretKey(flagPrefix, oauth2SecretKey))))
			}
			args = append(args, fmt.Sprintf("-%s.oauth2.clientID=%s", flagPrefix, s.OAuthCreds.ClientID))
			args = append(args, fmt.Sprintf("-%s.oauth2.tokenUrl=%s", flagPrefix, ha.OAuth2.TokenURL))
			args = append(args, fmt.Sprintf("-%s.oauth2.scopes=%s", flagPrefix, strings.Join(ha.OAuth2.Scopes, ",")))
		}
	}

	return args
}

func buildVMAlertArgs(cr *vmv1beta1.VMAlert, ruleConfigMapNames []string, remoteSecrets map[string]*authSecret) []string {
	pathPrefix := path.Join(tlsAssetsDir, cr.Namespace)
	args := []string{
		fmt.Sprintf("-datasource.url=%s", cr.Spec.Datasource.URL),
	}

	args = buildHeadersArg("datasource.headers", args, cr.Spec.Datasource.HTTPAuth.Headers)
	args = append(args, buildNotifiersArgs(cr, remoteSecrets)...)
	args = buildVMAlertAuthArgs(args, datasourceKey, cr.Spec.Datasource.HTTPAuth, remoteSecrets)

	if cr.Spec.Datasource.HTTPAuth.TLSConfig != nil {
		tlsConf := cr.Spec.Datasource.HTTPAuth.TLSConfig
		args = tlsConf.AsArgs(args, datasourceKey, pathPrefix)
	}

	if cr.Spec.RemoteWrite != nil {
		args = append(args, fmt.Sprintf("-remoteWrite.url=%s", cr.Spec.RemoteWrite.URL))
		args = buildVMAlertAuthArgs(args, remoteWriteKey, cr.Spec.RemoteWrite.HTTPAuth, remoteSecrets)
		args = buildHeadersArg("remoteWrite.headers", args, cr.Spec.RemoteWrite.HTTPAuth.Headers)
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
			args = tlsConf.AsArgs(args, remoteWriteKey, pathPrefix)
		}
	}
	for k, v := range cr.Spec.ExternalLabels {
		args = append(args, fmt.Sprintf("-external.label=%s=%s", k, v))
	}

	if cr.Spec.RemoteRead != nil {
		args = append(args, fmt.Sprintf("-remoteRead.url=%s", cr.Spec.RemoteRead.URL))
		args = buildVMAlertAuthArgs(args, remoteReadKey, cr.Spec.RemoteRead.HTTPAuth, remoteSecrets)
		args = buildHeadersArg("remoteRead.headers", args, cr.Spec.RemoteRead.HTTPAuth.Headers)
		if cr.Spec.RemoteRead.Lookback != nil {
			args = append(args, fmt.Sprintf("-remoteRead.lookback=%s", *cr.Spec.RemoteRead.Lookback))
		}
		if cr.Spec.RemoteRead.HTTPAuth.TLSConfig != nil {
			tlsConf := cr.Spec.RemoteRead.HTTPAuth.TLSConfig
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
		args = append(args, fmt.Sprintf("-rule=%q", path.Join(vmAlertConfigDir, cm, "*.yaml")))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))

	for _, rulePath := range cr.Spec.RulePath {
		args = append(args, fmt.Sprintf("-rule=%q", rulePath))
	}
	if len(cr.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	return args
}

type authSecret struct {
	bearerValue string
	*k8stools.BasicAuthCredentials
	*k8stools.OAuthCreds
}

func loadVMAlertRemoteSecrets(
	ctx context.Context,
	rclient client.Client,
	cr *vmv1beta1.VMAlert,
) (map[string]*authSecret, error) {
	datasource := cr.Spec.Datasource
	remoteWrite := cr.Spec.RemoteWrite
	remoteRead := cr.Spec.RemoteRead
	ns := cr.Namespace

	nsSecretCache := make(map[string]*corev1.Secret)
	nsCMCache := make(map[string]*corev1.ConfigMap)
	authSecretsBySource := make(map[string]*authSecret)
	loadHTTPAuthSecrets := func(ctx context.Context, rclient client.Client, ns string, httpAuth vmv1beta1.HTTPAuth) (*authSecret, error) {
		var as authSecret
		if httpAuth.BasicAuth != nil {
			credentials, err := k8stools.LoadBasicAuthSecret(ctx, rclient, cr.Namespace, httpAuth.BasicAuth, nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("could not load basicAuth config. %w", err)
			}
			as.BasicAuthCredentials = &credentials
		}
		if httpAuth.BearerAuth != nil && httpAuth.TokenSecret != nil {
			token, err := k8stools.GetCredFromSecret(ctx, rclient, cr.Namespace, httpAuth.BearerAuth.TokenSecret, buildCacheKey(ns, httpAuth.TokenSecret.Name), nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("cannot load bearer auth token: %w", err)
			}
			as.bearerValue = token
		}
		if httpAuth.OAuth2 != nil {
			oauth2, err := k8stools.LoadOAuthSecrets(ctx, rclient, httpAuth.OAuth2, cr.Namespace, nsSecretCache, nsCMCache)
			if err != nil {
				return nil, fmt.Errorf("cannot load oauth2 creds err: %w", err)
			}
			as.OAuthCreds = oauth2
		}
		return &as, nil
	}
	for i, notifier := range cr.Spec.Notifiers {
		as, err := loadHTTPAuthSecrets(ctx, rclient, ns, notifier.HTTPAuth)
		if err != nil {
			return nil, err
		}
		authSecretsBySource[buildNotifierKey(i)] = as
	}
	// load basic auth for datasource configuration
	as, err := loadHTTPAuthSecrets(ctx, rclient, ns, datasource.HTTPAuth)
	if err != nil {
		return nil, fmt.Errorf("could not generate basicAuth for datasource config. %w", err)
	}
	authSecretsBySource[datasourceKey] = as

	// load basic auth for remote write configuration
	if remoteWrite != nil {
		as, err := loadHTTPAuthSecrets(ctx, rclient, ns, remoteWrite.HTTPAuth)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for datasource config. %w", err)
		}
		authSecretsBySource[remoteWriteKey] = as
	}
	// load basic auth for remote write configuration
	if remoteRead != nil {
		as, err := loadHTTPAuthSecrets(ctx, rclient, ns, remoteRead.HTTPAuth)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for datasource config. %w", err)
		}
		authSecretsBySource[remoteReadKey] = as
	}
	return authSecretsBySource, nil
}

func createOrUpdateTLSAssetsForVMAlert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlert) error {
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
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Data: map[string][]byte{},
	}

	for key, asset := range assets {
		tlsAssetsSecret.Data[key] = []byte(asset)
	}
	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = &metav1.ObjectMeta{
			Name:        prevCR.TLSAssetName(),
			Labels:      prevCR.AllLabels(),
			Annotations: prevCR.AnnotationsFiltered(),
			Namespace:   prevCR.Namespace,
		}
	}
	return reconcile.Secret(ctx, rclient, tlsAssetsSecret, prevSecretMeta)
}

func FetchTLSAssets(ctx context.Context, rclient client.Client, namespace string, tc *vmv1beta1.TLSConfig, assetPathDst map[string]string) error {
	return nil
}

func loadTLSAssetsForVMAlert(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) (map[string]string, error) {
	assets := map[string]string{}
	nsSecretCache := make(map[string]*corev1.Secret)
	nsConfigMapCache := make(map[string]*corev1.ConfigMap)
	tlsConfigs := []*vmv1beta1.TLSConfig{}

	for _, notifier := range cr.Spec.Notifiers {
		if notifier.TLSConfig != nil {
			tlsConfigs = append(tlsConfigs, notifier.TLSConfig)
		}
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

	fetchAssetFor := func(assetPath string, src vmv1beta1.SecretOrConfigMap) error {
		var asset string
		var err error
		cacheKey := cr.Namespace + "/" + src.PrefixedName()
		switch {
		case src.Secret != nil:
			asset, err = k8stools.GetCredFromSecret(
				ctx,
				rclient,
				cr.Namespace,
				src.Secret,
				cacheKey,
				nsSecretCache,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to extract endpoint tls asset from secret %s and key %s in namespace %s",
					src.PrefixedName(), src.Key(), cr.Namespace,
				)
			}

		case src.ConfigMap != nil:
			asset, err = k8stools.GetCredFromConfigMap(
				ctx,
				rclient,
				cr.Namespace,
				*src.ConfigMap,
				cacheKey,
				nsConfigMapCache,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to extract endpoint tls asset for  configmap %v and key %v in namespace %v: %w",
					src.PrefixedName(), src.Key(), cr.Namespace, err,
				)
			}
		}
		if len(asset) > 0 {
			assets[assetPath] = asset
		}
		return nil
	}

	for _, rw := range tlsConfigs {
		if err := fetchAssetFor(rw.BuildAssetPath(cr.Namespace, rw.CA.PrefixedName(), rw.CA.Key()), rw.CA); err != nil {
			return nil, fmt.Errorf("cannot fetch tls asset for CA: %w", err)
		}
		if err := fetchAssetFor(rw.BuildAssetPath(cr.Namespace, rw.Cert.PrefixedName(), rw.Cert.Key()), rw.Cert); err != nil {
			return nil, fmt.Errorf("cannot fetch tls asset for Cert: %w", err)
		}

		if rw.KeySecret != nil {
			asset, err := k8stools.GetCredFromSecret(
				ctx,
				rclient,
				cr.Namespace,
				rw.KeySecret,
				cr.Namespace+"/"+rw.KeySecret.Name,
				nsSecretCache,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract endpoint tls asset for vmservicescrape %s from secret %s and key %s in namespace %s",
					cr.Name, rw.CA.PrefixedName(), rw.CA.Key(), cr.Namespace,
				)
			}
			assets[rw.BuildAssetPath(cr.Namespace, rw.KeySecret.Name, rw.KeySecret.Key)] = asset
		}
	}

	return assets, nil
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
		ntTLS := nt.HTTPAuth.TLSConfig
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
		if len(nt.HTTPAuth.Headers) > 0 {
			for _, headerKV := range nt.HTTPAuth.Headers {
				headerFlagValue += headerKV + "^^"
			}
			headers.isNotNull = true
		}
		headerFlagValue = strings.TrimSuffix(headerFlagValue, "^^")
		headers.flagSetting += fmt.Sprintf("%s,", headerFlagValue)
		var user, passFile string
		s := ntBasicAuth[buildNotifierKey(i)]
		if nt.HTTPAuth.BasicAuth != nil {
			if s == nil {
				panic("secret for basic notifier cannot be nil")
			}
			authUser.isNotNull = true
			user = s.Username
			if len(s.Password) > 0 {
				passFile = path.Join(vmalertConfigSecretsDir, buildRemoteSecretKey(buildNotifierKey(i), basicAuthPasswordKey))
				authPasswordFile.isNotNull = true
			}
			if len(nt.HTTPAuth.BasicAuth.PasswordFile) > 0 {
				passFile = nt.HTTPAuth.BasicAuth.PasswordFile
				authPasswordFile.isNotNull = true
			}
		}
		authUser.flagSetting += fmt.Sprintf("\"%s\",", strings.ReplaceAll(user, `"`, `\"`))
		authPasswordFile.flagSetting += fmt.Sprintf("%s,", passFile)

		var tokenPath string
		if nt.HTTPAuth.BearerAuth != nil {
			if len(nt.HTTPAuth.BearerAuth.TokenFilePath) > 0 {
				bearerTokenPath.isNotNull = true
				tokenPath = nt.HTTPAuth.BearerAuth.TokenFilePath
			} else if len(s.bearerValue) > 0 {
				bearerTokenPath.isNotNull = true
				tokenPath = path.Join(vmalertConfigSecretsDir, buildRemoteSecretKey(buildNotifierKey(i), bearerTokenKey))
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
				secretFile = path.Join(vmalertConfigSecretsDir, buildRemoteSecretKey(buildNotifierKey(i), oauth2SecretKey))
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

func buildCacheKey(ns, keyName string) string {
	return fmt.Sprintf("%s/%s", ns, keyName)
}

func buildConfigReloaderContainer(dst []corev1.Container, cr *vmv1beta1.VMAlert, ruleConfigMapNames []string) []corev1.Container {
	if cr.IsUnmanaged() {
		return dst
	}
	volumeWatchArg := "-volume-dir"
	reloadURLArg := "-webhook-url"
	useCustomConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	if useCustomConfigReloader {
		volumeWatchArg = "--watched-dir"
		reloadURLArg = "--reload-url"
	}
	confReloadArgs := []string{
		fmt.Sprintf("%s=%s", reloadURLArg, vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
	}
	for _, cm := range ruleConfigMapNames {
		confReloadArgs = append(confReloadArgs, fmt.Sprintf("%s=%s", volumeWatchArg, path.Join(vmAlertConfigDir, cm)))
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
	if useCustomConfigReloader {
		build.AddsPortProbesToConfigReloaderContainer(useCustomConfigReloader, &configReloaderContainer)

	}

	dst = append(dst, configReloaderContainer)
	return dst
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
		logger.WithContext(ctx).Info(fmt.Sprintf("additional notifiers=%d with sd selectors", len(additionalNotifiers)))
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
