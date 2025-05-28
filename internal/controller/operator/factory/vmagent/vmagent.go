package vmagent

import (
	"context"
	"fmt"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	confDir                       = "/etc/vmagent/config"
	confOutDir                    = "/etc/vmagent/config_out"
	persistentQueueDir            = "/tmp/vmagent-remotewrite-data"
	persistentQueueStatefulSetDir = "/vmagent_pq/vmagent-remotewrite-data"
	globalRelabelingName          = "global_relabeling.yaml"
	urlRelabelingName             = "url_relabeling-%d.yaml"
	globalAggregationConfigName   = "global_aggregation.yaml"

	tlsAssetsDir           = "/etc/vmagent-tls/certs"
	gzippedFilename        = "vmagent.yaml.gz"
	configEnvsubstFilename = "vmagent.env.yaml"
	defaultMaxDiskUsage    = "1073741824"

	kubeNodeEnvName     = "KUBE_NODE_NAME"
	kubeNodeEnvTemplate = "%{" + kubeNodeEnvName + "}"
)

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) (*corev1.Service, error) {
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			if prevCR.Spec.StatefulMode {
				svc.Spec.ClusterIP = "None"
			}
			build.AppendInsertPortsToService(prevCR.Spec.InsertPorts, svc)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, cr.Spec.ServiceSpec)
	}
	newService := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		if cr.Spec.StatefulMode {
			svc.Spec.ClusterIP = "None"
		}
		build.AppendInsertPortsToService(cr.Spec.InsertPorts, svc)
	})

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, cr.Spec.ServiceSpec)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vmagent additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmagent: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmagent: %w", err)
	}
	return newService, nil
}

// CreateOrUpdateDeploy creates deployment for vmagent and configures it
// waits for healthy state
func CreateOrUpdateDeploy(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) error {
	var prevCR *vmv1beta1.VMAgent
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
		if !cr.Spec.IngestOnlyMode {
			if err := createK8sAPIAccess(ctx, rclient, cr, prevCR, config.IsClusterWideAccessAllowed()); err != nil {
				return fmt.Errorf("cannot create vmagent role and binding for it, err: %w", err)
			}
		}
	}

	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		if cr.Spec.DaemonSetMode {
			ps := build.VMPodScrapeForObjectWithSpec(cr, cr.Spec.ServiceScrapeSpec, cr.Spec.ExtraArgs)
			err = reconcile.VMPodScrapeForCRD(ctx, rclient, ps)
		} else {
			err = reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		}
		if err != nil {
			return fmt.Errorf("cannot create or update scrape object: %w", err)
		}
	}

	ssCache, err := createOrUpdateConfigurationSecret(ctx, rclient, cr, prevCR, nil)
	if err != nil {
		return err
	}

	if err := createOrUpdateRelabelConfigsAssets(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot update relabeling asset for vmagent: %w", err)
	}

	if err := createOrUpdateStreamAggrConfig(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot update stream aggregation config for vmagent: %w", err)
	}

	if cr.Spec.PodDisruptionBudget != nil && !cr.Spec.DaemonSetMode {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		err = reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB)
		if err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vmagent: %w", err)
		}
	}

	var prevDeploy runtime.Object

	if prevCR != nil {
		prevDeploy, err = newDeploy(prevCR, ssCache)
		if err != nil {
			return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
		}
	}
	newDeploy, err := newDeploy(cr, ssCache)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
	}

	if !cr.Spec.DaemonSetMode && cr.Spec.ShardCount != nil && *cr.Spec.ShardCount > 1 {
		err = build.CreateOrUpdateShardedDeploy(ctx, rclient, cr, prevCR, newDeploy, prevDeploy)
	} else {
		err = build.CreateOrUpdateDeploy(ctx, rclient, cr, prevCR, newDeploy, prevDeploy)
	}
	if err != nil {
		return err
	}
	if !cr.Spec.DaemonSetMode {
		return nil
	}
	return finalize.RemoveOrphanedDaemonSet(ctx, rclient, cr.GetName(), cr.GetNamespace())
}

// newDeploy builds vmagent deployment spec.
func newDeploy(cr *vmv1beta1.VMAgent, ssCache *scrapesSecretsCache) (runtime.Object, error) {

	podSpec, err := newPodSpec(cr, ssCache)
	if err != nil {
		return nil, err
	}
	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	if cr.Spec.DaemonSetMode {
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
	}
	// fast path, use sts
	if cr.Spec.StatefulMode {
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
				ServiceName:         buildStatefulSetServiceName(cr),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      cr.PodLabels(),
						Annotations: cr.PodAnnotations(),
					},
					Spec: *podSpec,
				},
			},
		}
		build.StatefulSetAddCommonParams(stsSpec, useStrictSecurity, &cr.Spec.CommonApplicationDeploymentParams)
		stsSpec.Spec.Template.Spec.Volumes = build.AddServiceAccountTokenVolume(stsSpec.Spec.Template.Spec.Volumes, &cr.Spec.CommonApplicationDeploymentParams)
		cr.Spec.StatefulStorage.IntoStatefulSetVolume(cr.GetVolumeName(), &stsSpec.Spec)
		stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.ClaimTemplates...)
		return stsSpec, nil
	}

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

func buildStatefulSetServiceName(cr *vmv1beta1.VMAgent) string {
	// set service name for sts if additional service is headless
	if cr.Spec.ServiceSpec != nil &&
		!cr.Spec.ServiceSpec.UseAsDefault &&
		cr.Spec.ServiceSpec.Spec.ClusterIP == corev1.ClusterIPNone {
		return cr.Spec.ServiceSpec.NameOrDefault(cr.PrefixedName())
	}
	// special case for sharded mode
	if cr.Spec.ShardCount != nil {
		return cr.PrefixedName()
	}
	return ""
}

func buildRelabelingsAssetsMeta(cr *vmv1beta1.VMAgent) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.RelabelingAssetName(),
		Labels:          cr.AllLabels(),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: cr.AsOwner(),
	}
}

// buildRelabelingsAssets combines all possible relabeling config configuration and adding it to the configmap.
func buildRelabelingsAssets(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: buildRelabelingsAssetsMeta(cr),
		Data:       make(map[string]string),
	}
	// global section
	if len(cr.Spec.InlineRelabelConfig) > 0 {
		rcs := addRelabelConfigs(nil, cr.Spec.InlineRelabelConfig)
		data, err := yaml.Marshal(rcs)
		if err != nil {
			return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
		}
		if len(data) > 0 {
			cfgCM.Data[globalRelabelingName] = string(data)
		}
	}
	if cr.Spec.RelabelConfig != nil {
		// need to fetch content from
		data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cr.Spec.RelabelConfig.Name, Namespace: cr.Namespace}},
			cr.Spec.RelabelConfig.Key)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", cr.Spec.RelabelConfig.Name, err)
		}
		if len(data) > 0 {
			cfgCM.Data[globalRelabelingName] += data
		}
	}
	// per remoteWrite section.
	for i := range cr.Spec.RemoteWrite {
		rw := cr.Spec.RemoteWrite[i]
		if len(rw.InlineUrlRelabelConfig) > 0 {
			rcs := addRelabelConfigs(nil, rw.InlineUrlRelabelConfig)
			data, err := yaml.Marshal(rcs)
			if err != nil {
				return nil, fmt.Errorf("cannot serialize urlRelabelConfig as yaml: %w", err)
			}
			if len(data) > 0 {
				cfgCM.Data[fmt.Sprintf(urlRelabelingName, i)] = string(data)
			}
		}
		if rw.UrlRelabelConfig != nil {
			data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: rw.UrlRelabelConfig.Name, Namespace: cr.Namespace}},
				rw.UrlRelabelConfig.Key)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", rw.UrlRelabelConfig.Name, err)
			}
			if len(data) > 0 {
				cfgCM.Data[fmt.Sprintf(urlRelabelingName, i)] += data
			}
		}
	}
	return cfgCM, nil
}

// createOrUpdateRelabelConfigsAssets builds relabeling configs for vmagent at separate configmap, serialized as yaml
func createOrUpdateRelabelConfigsAssets(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	if !cr.HasAnyRelabellingConfigs() {
		return nil
	}
	assestsCM, err := buildRelabelingsAssets(ctx, rclient, cr)
	if err != nil {
		return err
	}
	var prevConfigMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevConfigMeta = ptr.To(buildRelabelingsAssetsMeta(prevCR))
	}
	return reconcile.ConfigMap(ctx, rclient, assestsCM, prevConfigMeta)
}

func buildStreamAggrConfigMeta(cr *vmv1beta1.VMAgent) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.StreamAggrConfigName(),
		Labels:          cr.AllLabels(),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: cr.AsOwner(),
	}
}

// buildStreamAggrConfig combines all possible stream aggregation configs and adding it to the configmap.
func buildStreamAggrConfig(ctx context.Context, cr *vmv1beta1.VMAgent, rclient client.Client) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: buildStreamAggrConfigMeta(cr),
		Data:       make(map[string]string),
	}
	// global section
	if cr.Spec.StreamAggrConfig != nil {
		if len(cr.Spec.StreamAggrConfig.Rules) > 0 {
			data, err := yaml.Marshal(cr.Spec.StreamAggrConfig.Rules)
			if err != nil {
				return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
			}
			if len(data) > 0 {
				cfgCM.Data[globalAggregationConfigName] = string(data)
			}
		}
		if cr.Spec.StreamAggrConfig.RuleConfigMap != nil {
			data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cr.Spec.StreamAggrConfig.RuleConfigMap.Name, Namespace: cr.Namespace}},
				cr.Spec.StreamAggrConfig.RuleConfigMap.Key)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", cr.Spec.StreamAggrConfig.RuleConfigMap.Name, err)
			}
			if len(data) > 0 {
				cfgCM.Data[globalAggregationConfigName] += data
			}
		}
	}

	for i := range cr.Spec.RemoteWrite {
		rw := cr.Spec.RemoteWrite[i]
		if rw.StreamAggrConfig != nil {
			if len(rw.StreamAggrConfig.Rules) > 0 {
				data, err := yaml.Marshal(rw.StreamAggrConfig.Rules)
				if err != nil {
					return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
				}
				if len(data) > 0 {
					cfgCM.Data[rw.AsConfigMapKey(i, "stream-aggr-conf")] = string(data)
				}
			}
			if rw.StreamAggrConfig.RuleConfigMap != nil {
				data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
					&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: rw.StreamAggrConfig.RuleConfigMap.Name, Namespace: cr.Namespace}},
					rw.StreamAggrConfig.RuleConfigMap.Key)
				if err != nil {
					return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", rw.StreamAggrConfig.RuleConfigMap.Name, err)
				}
				if len(data) > 0 {
					cfgCM.Data[rw.AsConfigMapKey(i, "stream-aggr-conf")] += data
				}
			}

		}

	}
	return cfgCM, nil
}

// createOrUpdateStreamAggrConfig builds stream aggregation configs for vmagent at separate configmap, serialized as yaml
func createOrUpdateStreamAggrConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
	// fast path
	if !cr.HasAnyStreamAggrRule() {
		return nil
	}
	streamAggrCM, err := buildStreamAggrConfig(ctx, cr, rclient)
	if err != nil {
		return err
	}
	var prevConfigMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevConfigMeta = ptr.To(buildStreamAggrConfigMeta(prevCR))
	}
	return reconcile.ConfigMap(ctx, rclient, streamAggrCM, prevConfigMeta)
}

func createOrUpdateTLSAssets(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, assets map[string]string) error {
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

func addAssetsToCache(
	ctx context.Context,
	rclient client.Client,
	objectNS string,
	tlsConfig *vmv1beta1.TLSConfig,
	ssCache *scrapesSecretsCache,
) error {
	if tlsConfig == nil {
		return nil
	}
	assets, nsSecretCache, nsConfigMapCache := ssCache.tlsAssets, ssCache.nsSecretCache, ssCache.nsCMCache

	fetchAssetFor := func(assetPath string, src vmv1beta1.SecretOrConfigMap) error {
		var asset string
		var err error
		cacheKey := objectNS + "/" + src.PrefixedName()
		switch {
		case src.Secret != nil:
			asset, err = k8stools.GetCredFromSecret(
				ctx,
				rclient,
				objectNS,
				src.Secret,
				cacheKey,
				nsSecretCache,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to extract endpoint tls asset from secret %s and key %s in namespace %s: %w",
					src.PrefixedName(), src.Key(), objectNS, err,
				)
			}

		case src.ConfigMap != nil:
			asset, err = k8stools.GetCredFromConfigMap(
				ctx,
				rclient,
				objectNS,
				*src.ConfigMap,
				cacheKey,
				nsConfigMapCache,
			)
			if err != nil {
				return fmt.Errorf(
					"failed to extract endpoint tls asset for  configmap %v and key %v in namespace %v",
					src.PrefixedName(), src.Key(), objectNS,
				)
			}
		}
		if len(asset) > 0 {
			assets[assetPath] = asset
		}
		return nil
	}

	if err := fetchAssetFor(tlsConfig.BuildAssetPath(objectNS, tlsConfig.CA.PrefixedName(), tlsConfig.CA.Key()), tlsConfig.CA); err != nil {
		return fmt.Errorf("cannot fetch CA tls asset: %w", err)
	}

	if err := fetchAssetFor(tlsConfig.BuildAssetPath(objectNS, tlsConfig.Cert.PrefixedName(), tlsConfig.Cert.Key()), tlsConfig.Cert); err != nil {
		return fmt.Errorf("cannot fetch Cert tls asset: %w", err)
	}

	if tlsConfig.KeySecret != nil {
		asset, err := k8stools.GetCredFromSecret(
			ctx,
			rclient,
			objectNS,
			tlsConfig.KeySecret,
			objectNS+"/"+tlsConfig.KeySecret.Name,
			nsSecretCache,
		)
		if err != nil {
			return fmt.Errorf(
				"failed to extract endpoint tls asset from secret %s and key %s in namespace %s",
				tlsConfig.KeySecret.Name, tlsConfig.KeySecret.Key, objectNS,
			)
		}
		assets[tlsConfig.BuildAssetPath(objectNS, tlsConfig.KeySecret.Name, tlsConfig.KeySecret.Key)] = asset
	}

	return nil
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent) error {
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

	if !prevCR.Spec.DaemonSetMode && cr.Spec.DaemonSetMode {
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

	if prevCR.Spec.DaemonSetMode && !cr.Spec.DaemonSetMode {
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
