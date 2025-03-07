package vmagent

import (
	"bytes"
	"compress/gzip"
	"context"
	stderrors "errors"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/VictoriaMetrics/metricsql"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

var vmagentSecretFetchErrsTotal prometheus.Counter

func init() {
	vmagentSecretFetchErrsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "operator_vmagent_config_fetch_secret_errors_total",
		Help: "Indicates if user defined objects contain missing link for secret",
	})
	metrics.Registry.MustRegister(vmagentSecretFetchErrsTotal)
}

type scrapesSecretsCache struct {
	bearerTokens         map[string]string
	baSecrets            map[string]*k8stools.BasicAuthCredentials
	oauth2Secrets        map[string]*k8stools.OAuthCreds
	authorizationSecrets map[string]string
	nsSecretCache        map[string]*corev1.Secret
	nsCMCache            map[string]*corev1.ConfigMap
	tlsAssets            map[string]string
}

type scrapeObjects struct {
	sss              []*vmv1beta1.VMServiceScrape
	pss              []*vmv1beta1.VMPodScrape
	stss             []*vmv1beta1.VMStaticScrape
	nss              []*vmv1beta1.VMNodeScrape
	prss             []*vmv1beta1.VMProbe
	scss             []*vmv1beta1.VMScrapeConfig
	sssBroken        []*vmv1beta1.VMServiceScrape
	pssBroken        []*vmv1beta1.VMPodScrape
	stssBroken       []*vmv1beta1.VMStaticScrape
	nssBroken        []*vmv1beta1.VMNodeScrape
	prssBroken       []*vmv1beta1.VMProbe
	scssBroken       []*vmv1beta1.VMScrapeConfig
	totalBrokenCount int
}

// CreateOrUpdateConfigurationSecret builds scrape configuration for VMAgent
func CreateOrUpdateConfigurationSecret(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent, childObject client.Object) error {
	var prevCR *vmv1beta1.VMAgent
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if _, err := createOrUpdateConfigurationSecret(ctx, rclient, cr, prevCR, childObject); err != nil {
		return err
	}
	return nil
}

func createOrUpdateConfigurationSecret(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, childObject client.Object) (*scrapesSecretsCache, error) {
	if cr.Spec.IngestOnlyMode {
		return nil, nil
	}
	sss, err := selectServiceScrapes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting ServiceScrapes failed: %w", err)
	}

	pScrapes, err := selectPodScrapes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting PodScrapes failed: %w", err)
	}

	probes, err := selectVMProbes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting VMProbes failed: %w", err)
	}

	nodes, err := selectVMNodeScrapes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting VMNodeScrapes failed: %w", err)
	}

	statics, err := selectStaticScrapes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting PodScrapes failed: %w", err)
	}

	scrapeConfigs, err := selectScrapeConfig(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting ScrapeConfigs failed: %w", err)
	}
	sos := &scrapeObjects{
		sss:  sss,
		pss:  pScrapes,
		prss: probes,
		nss:  nodes,
		stss: statics,
		scss: scrapeConfigs,
	}
	// filter out all service scrapes that access
	// the file system.
	// TODO: @f41gh7 properly check file system for other components
	// with additional function
	// keep it for backward-compatibility
	var brokenServiceScrapes []*vmv1beta1.VMServiceScrape
	if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
		var cnt int
	OUTER:
		for _, sm := range sos.sss {
			for _, endpoint := range sm.Spec.Endpoints {
				if err := testForArbitraryFSAccess(endpoint.EndpointAuth); err != nil {
					sm.Status.CurrentSyncError = err.Error()
					brokenServiceScrapes = append(brokenServiceScrapes, sm)
					continue OUTER
				}
			}
			sos.sss[cnt] = sm
			cnt++
		}
		sos.sss = sos.sss[:cnt]
	}

	ssCache, err := loadScrapeSecrets(ctx, rclient, sos, cr.Namespace, cr.Spec.APIServerConfig, cr.Spec.RemoteWrite)
	if err != nil {
		return nil, fmt.Errorf("cannot load scrape target secrets: %w", err)
	}

	if err := createOrUpdateTLSAssets(ctx, rclient, cr, prevCR, ssCache.tlsAssets); err != nil {
		return nil, fmt.Errorf("cannot create tls assets secret for vmagent: %w", err)
	}

	additionalScrapeConfigs, err := loadAdditionalScrapeConfigsSecret(ctx, rclient, cr.Spec.AdditionalScrapeConfigs, cr.Namespace)
	if err != nil {
		return nil, fmt.Errorf("loading additional scrape configs from Secret failed: %w", err)
	}
	// TODO: @f41gh7  move it to the separate function
	sos.sssBroken = append(sos.sssBroken, brokenServiceScrapes...)

	// Update secret based on the most recent configuration.
	generatedConfig, err := generateConfig(
		ctx,
		cr,
		sos,
		ssCache,
		additionalScrapeConfigs,
	)
	if err != nil {
		return nil, fmt.Errorf("generating config for vmagent failed: %w", err)
	}

	s := makeConfigSecret(cr, ssCache)
	s.Annotations = map[string]string{
		"generated": "true",
	}

	// Compress config to avoid 1mb secret limit for a while
	var buf bytes.Buffer
	if err = gzipConfig(&buf, generatedConfig); err != nil {
		return nil, fmt.Errorf("cannot gzip config for vmagent: %w", err)
	}
	s.Data[vmagentGzippedFilename] = buf.Bytes()

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = ptr.To(buildConfigMeta(prevCR))
	}
	if err := reconcile.Secret(ctx, rclient, s, prevSecretMeta); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmagent config secret: %w", err)
	}
	if err := updateStatusesForScrapeObjects(ctx, rclient, cr, sos, childObject); err != nil {
		return nil, err
	}

	return ssCache, nil
}

func updateStatusesForScrapeObjects(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent, sos *scrapeObjects, childObject client.Object) error {

	vmagentSecretFetchErrsTotal.Add(float64(sos.totalBrokenCount))

	parentObject := fmt.Sprintf("%s.%s.vmagent", cr.Name, cr.Namespace)
	if childObject != nil && !reflect.ValueOf(childObject).IsNil() {
		// fast path
		switch t := childObject.(type) {
		case *vmv1beta1.VMStaticScrape:
			for _, o := range sos.stss {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMStaticScrape{o})
				}
			}
			for _, o := range sos.stssBroken {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMStaticScrape{o})
				}
			}

		case *vmv1beta1.VMProbe:
			for _, o := range sos.prss {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMProbe{o})
				}
			}
			for _, o := range sos.prssBroken {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMProbe{o})
				}
			}
		case *vmv1beta1.VMScrapeConfig:
			for _, o := range sos.scss {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMScrapeConfig{o})
				}
			}
			for _, o := range sos.scssBroken {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMScrapeConfig{o})
				}
			}

		case *vmv1beta1.VMNodeScrape:
			for _, o := range sos.nss {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMNodeScrape{o})
				}
			}
			for _, o := range sos.nssBroken {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMNodeScrape{o})
				}
			}
		case *vmv1beta1.VMPodScrape:
			for _, o := range sos.pss {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMPodScrape{o})
				}
			}
			for _, o := range sos.pssBroken {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMPodScrape{o})
				}
			}
		case *vmv1beta1.VMServiceScrape:
			for _, o := range sos.sss {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMServiceScrape{o})
				}
			}
			for _, o := range sos.sssBroken {
				if o.Name == t.Name && o.Namespace == t.Namespace {
					return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMServiceScrape{o})
				}
			}
		}
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.sss); err != nil {
		return fmt.Errorf("cannot update statuses for service scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.sssBroken); err != nil {
		return fmt.Errorf("cannot update statuses for broken scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.pss); err != nil {
		return fmt.Errorf("cannot update statuses for pod scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.pssBroken); err != nil {
		return fmt.Errorf("cannot update statuses for broken pod scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.nss); err != nil {
		return fmt.Errorf("cannot update statuses for node scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.nssBroken); err != nil {
		return fmt.Errorf("cannot update statuses for broken node scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.prss); err != nil {
		return fmt.Errorf("cannot update statuses for probe scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.prssBroken); err != nil {
		return fmt.Errorf("cannot update statuses for broken probe scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.stss); err != nil {
		return fmt.Errorf("cannot update statuses for static scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.stssBroken); err != nil {
		return fmt.Errorf("cannot update statuses for broken static scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.scss); err != nil {
		return fmt.Errorf("cannot update statuses for scrapeconfig scrape objects: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sos.scssBroken); err != nil {
		return fmt.Errorf("cannot update statuses for broken scrapeconfig scrape objects: %w", err)
	}
	return nil
}

type scrapeObjectWithStatus interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}

// returned objects with not found links have erased type
func forEachCollectSkipNotFound[T scrapeObjectWithStatus](src []T, apply func(s T) error) ([]T, []T, error) {
	var cnt int
	var notNotFoundLinks []T
	for _, o := range src {
		if err := apply(o); err != nil {
			var ne *k8stools.KeyNotFoundError
			switch {
			case stderrors.As(err, &ne):
				notNotFoundLinks = append(notNotFoundLinks, o)
			case errors.IsNotFound(err):
				notNotFoundLinks = append(notNotFoundLinks, o)
			default:
				return nil, nil, err
			}
			st := o.GetStatusMetadata()
			st.CurrentSyncError = fmt.Sprintf("cannot find refrenced object: %s", err)
			continue
		}
		src[cnt] = o
		cnt++
	}
	src = src[:cnt]
	return src, notNotFoundLinks, nil
}
func loadSecretsToCacheFrom(ctx context.Context, rclient client.Client, ep *vmv1beta1.EndpointAuth, cacheKey, namespace string, ss *scrapesSecretsCache) error {
	if ep.BasicAuth != nil {
		credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, ep.BasicAuth, namespace, ss.nsSecretCache)
		if err != nil {
			return fmt.Errorf("cannot load basicAuth secret for=%s: %w", cacheKey, err)
		}
		ss.baSecrets[cacheKey] = credentials
	}

	if ep.OAuth2 != nil {
		oauth2, err := k8stools.LoadOAuthSecrets(ctx, rclient, ep.OAuth2, namespace, ss.nsSecretCache, ss.nsCMCache)
		if err != nil {
			return fmt.Errorf("cannot load oauth2 secret for=%s: %w", cacheKey, err)
		}
		ss.oauth2Secrets[cacheKey] = oauth2
	}
	if ep.BearerTokenSecret != nil && ep.BearerTokenSecret.Name != "" {
		token, err := k8stools.GetCredFromSecret(ctx, rclient, namespace, ep.BearerTokenSecret, buildCacheKey(namespace, ep.BearerTokenSecret.Name), ss.nsSecretCache)
		if err != nil {
			return fmt.Errorf("cannot load bearer secret for=%s: %w", cacheKey, err)
		}
		ss.bearerTokens[cacheKey] = token
	}
	if ep.Authorization != nil && ep.Authorization.Credentials != nil {
		secretValue, err := k8stools.GetCredFromSecret(ctx, rclient, namespace, ep.Authorization.Credentials, buildCacheKey(namespace, ep.Authorization.Credentials.Name), ss.nsSecretCache)
		if err != nil {
			return fmt.Errorf("cannot load authorization secret for=%s: %w", cacheKey, err)
		}
		ss.authorizationSecrets[cacheKey] = secretValue
	}

	if err := addAssetsToCache(ctx, rclient, namespace, ep.TLSConfig, ss); err != nil {
		return fmt.Errorf("cannot add tlsAsset for=%s %w", cacheKey, err)
	}

	return nil
}

func loadScrapeSecrets(
	ctx context.Context,
	rclient client.Client,
	sos *scrapeObjects,
	vmagentCRNamespace string,
	apiserverConfig *vmv1beta1.APIServerConfig,
	remoteWriteSpecs []vmv1beta1.VMAgentRemoteWriteSpec,
) (*scrapesSecretsCache, error) {
	ssCache := &scrapesSecretsCache{
		baSecrets:            map[string]*k8stools.BasicAuthCredentials{},
		oauth2Secrets:        map[string]*k8stools.OAuthCreds{},
		bearerTokens:         map[string]string{},
		authorizationSecrets: map[string]string{},
		nsSecretCache:        map[string]*corev1.Secret{},
		nsCMCache:            map[string]*corev1.ConfigMap{},
		tlsAssets:            map[string]string{},
	}
	var err error
	sos.sss, sos.sssBroken, err = forEachCollectSkipNotFound(sos.sss, func(mon *vmv1beta1.VMServiceScrape) error {
		for i, ep := range mon.Spec.Endpoints {
			if err := loadSecretsToCacheFrom(ctx, rclient, &ep.EndpointAuth, mon.AsMapKey(i), mon.Namespace, ssCache); err != nil {
				return err
			}
			if ep.VMScrapeParams != nil && ep.VMScrapeParams.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, ep.VMScrapeParams.ProxyClientConfig, mon.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("cannot load secret for VMServiceScrape: %w", err)
				}
				if ba != nil {
					ssCache.baSecrets[mon.AsProxyKey(i)] = ba
				}
				ssCache.bearerTokens[mon.AsProxyKey(i)] = token
				if err := addAssetsToCache(ctx, rclient, mon.Namespace, ep.VMScrapeParams.ProxyClientConfig.TLSConfig, ssCache); err != nil {
					return fmt.Errorf("cannot add proxy tlsAsset: %w", err)
				}

			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sos.nss, sos.nssBroken, err = forEachCollectSkipNotFound(sos.nss, func(node *vmv1beta1.VMNodeScrape) error {
		if err := loadSecretsToCacheFrom(ctx, rclient, &node.Spec.EndpointAuth, node.AsMapKey(), node.Namespace, ssCache); err != nil {
			return err
		}
		if node.Spec.VMScrapeParams != nil && node.Spec.VMScrapeParams.ProxyClientConfig != nil {
			ba, token, err := loadProxySecrets(ctx, rclient, node.Spec.VMScrapeParams.ProxyClientConfig, node.Namespace, ssCache.nsSecretCache)
			if err != nil {
				return err
			}
			if ba != nil {
				ssCache.baSecrets[node.AsProxyKey()] = ba
			}
			ssCache.bearerTokens[node.AsProxyKey()] = token
			if err := addAssetsToCache(ctx, rclient, node.Namespace, node.Spec.VMScrapeParams.ProxyClientConfig.TLSConfig, ssCache); err != nil {
				return fmt.Errorf("cannot add proxy tlsAsset: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	sos.pss, sos.pssBroken, err = forEachCollectSkipNotFound(sos.pss, func(pod *vmv1beta1.VMPodScrape) error {
		for i, ep := range pod.Spec.PodMetricsEndpoints {
			if err := loadSecretsToCacheFrom(ctx, rclient, &ep.EndpointAuth, pod.AsMapKey(i), pod.Namespace, ssCache); err != nil {
				return err
			}
			if ep.VMScrapeParams != nil && ep.VMScrapeParams.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, ep.VMScrapeParams.ProxyClientConfig, pod.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("cannot load secret for VMPodScrape: %w", err)
				}
				if ba != nil {
					ssCache.baSecrets[pod.AsProxyKey(i)] = ba
				}
				ssCache.bearerTokens[pod.AsProxyKey(i)] = token
				if err := addAssetsToCache(ctx, rclient, pod.Namespace, ep.VMScrapeParams.ProxyClientConfig.TLSConfig, ssCache); err != nil {
					return fmt.Errorf("cannot add proxy tlsAsset: %w", err)
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	sos.prss, sos.prssBroken, err = forEachCollectSkipNotFound(sos.prss, func(probe *vmv1beta1.VMProbe) error {
		if err := loadSecretsToCacheFrom(ctx, rclient, &probe.Spec.EndpointAuth, probe.AsMapKey(), probe.Namespace, ssCache); err != nil {
			return err
		}
		if probe.Spec.VMScrapeParams != nil && probe.Spec.VMScrapeParams.ProxyClientConfig != nil {
			ba, token, err := loadProxySecrets(ctx, rclient, probe.Spec.VMScrapeParams.ProxyClientConfig, probe.Namespace, ssCache.nsSecretCache)
			if err != nil {
				return err
			}
			if ba != nil {
				ssCache.baSecrets[probe.AsProxyKey()] = ba
			}
			ssCache.bearerTokens[probe.AsProxyKey()] = token
			if err := addAssetsToCache(ctx, rclient, probe.Namespace, probe.Spec.VMScrapeParams.ProxyClientConfig.TLSConfig, ssCache); err != nil {
				return fmt.Errorf("cannot add proxy tlsAsset: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sos.stss, sos.stssBroken, err = forEachCollectSkipNotFound(sos.stss, func(staticCfg *vmv1beta1.VMStaticScrape) error {
		for i, ep := range staticCfg.Spec.TargetEndpoints {
			if err := loadSecretsToCacheFrom(ctx, rclient, &ep.EndpointAuth, staticCfg.AsMapKey(i), staticCfg.Namespace, ssCache); err != nil {
				return err
			}

			if ep.VMScrapeParams != nil && ep.VMScrapeParams.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, ep.VMScrapeParams.ProxyClientConfig, staticCfg.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not load secret for vmstaticScrape:  %w", err)
				}
				if ba != nil {
					ssCache.baSecrets[staticCfg.AsProxyKey(i)] = ba
				}
				ssCache.bearerTokens[staticCfg.AsProxyKey(i)] = token
				if err := addAssetsToCache(ctx, rclient, staticCfg.Namespace, ep.VMScrapeParams.ProxyClientConfig.TLSConfig, ssCache); err != nil {
					return fmt.Errorf("cannot add proxy tlsAsset: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sos.scss, sos.scssBroken, err = forEachCollectSkipNotFound(sos.scss, func(scrapeConfig *vmv1beta1.VMScrapeConfig) error {
		if err := loadSecretsToCacheFrom(ctx, rclient, &scrapeConfig.Spec.EndpointAuth, scrapeConfig.AsMapKey("", 0), scrapeConfig.Namespace, ssCache); err != nil {
			return err
		}
		if scrapeConfig.Spec.VMScrapeParams != nil && scrapeConfig.Spec.VMScrapeParams.ProxyClientConfig != nil {
			ba, token, err := loadProxySecrets(ctx, rclient, scrapeConfig.Spec.VMScrapeParams.ProxyClientConfig, scrapeConfig.Namespace, ssCache.nsSecretCache)
			if err != nil {
				return fmt.Errorf("could not generate proxy auth for VMScrapeConfig %s. %w", scrapeConfig.Name, err)
			}
			if ba != nil {
				ssCache.baSecrets[scrapeConfig.AsProxyKey("", 0)] = ba
			}
			ssCache.bearerTokens[scrapeConfig.AsProxyKey("", 0)] = token
			if err := addAssetsToCache(ctx, rclient, scrapeConfig.Namespace, scrapeConfig.Spec.VMScrapeParams.ProxyClientConfig.TLSConfig, ssCache); err != nil {
				return fmt.Errorf("cannot add proxy tlsAsset: %w", err)
			}
		}
		for i, hc := range scrapeConfig.Spec.HTTPSDConfigs {
			if hc.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, hc.BasicAuth, scrapeConfig.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate basicAuth for httpSDConfig %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.baSecrets[scrapeConfig.AsMapKey("httpsd", i)] = credentials
			}
			if hc.Authorization != nil && hc.Authorization.Credentials != nil {
				secretValue, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, hc.Authorization.Credentials, buildCacheKey(scrapeConfig.Namespace, hc.Authorization.Credentials.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate authorization for httpSDConfig %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.authorizationSecrets[scrapeConfig.AsMapKey("httpsd", i)] = secretValue
			}
			if hc.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, hc.ProxyClientConfig, scrapeConfig.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate proxy auth for httpSDConfig %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				if ba != nil {
					ssCache.baSecrets[scrapeConfig.AsProxyKey("httpsd", i)] = ba
				}
				ssCache.bearerTokens[scrapeConfig.AsProxyKey("httpsd", i)] = token
			}
		}
		for i, kc := range scrapeConfig.Spec.KubernetesSDConfigs {
			if kc.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, kc.BasicAuth, scrapeConfig.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate basicAuth for kubernetesSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.baSecrets[scrapeConfig.AsMapKey("kubesd", i)] = credentials
			}
			if kc.Authorization != nil && kc.Authorization.Credentials != nil {
				secretValue, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, kc.Authorization.Credentials, buildCacheKey(scrapeConfig.Namespace, kc.Authorization.Credentials.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate authorization for kubernetesSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.authorizationSecrets[scrapeConfig.AsMapKey("kubesd", i)] = secretValue
			}
			if kc.OAuth2 != nil {
				oauth2, err := k8stools.LoadOAuthSecrets(ctx, rclient, kc.OAuth2, scrapeConfig.Namespace, ssCache.nsSecretCache, ssCache.nsCMCache)
				if err != nil {
					return fmt.Errorf("could not generate oauth2 for kubernetesSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.oauth2Secrets[scrapeConfig.AsMapKey("kubesd", i)] = oauth2
			}
			if kc.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, kc.ProxyClientConfig, scrapeConfig.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate proxy auth for kubernetesSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				if ba != nil {
					ssCache.baSecrets[scrapeConfig.AsProxyKey("kubesd", i)] = ba
				}
				ssCache.bearerTokens[scrapeConfig.AsProxyKey("kubesd", i)] = token
			}
		}
		for i, cc := range scrapeConfig.Spec.ConsulSDConfigs {
			if cc.TokenRef != nil {
				token, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, cc.TokenRef, buildCacheKey(scrapeConfig.Namespace, cc.TokenRef.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate token for consulSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.bearerTokens[scrapeConfig.AsMapKey("consulsd", i)] = token
			}
			if cc.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, cc.BasicAuth, scrapeConfig.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate basicAuth for consulSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.baSecrets[scrapeConfig.AsMapKey("consulsd", i)] = credentials
			}
			if cc.Authorization != nil && cc.Authorization.Credentials != nil {
				secretValue, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, cc.Authorization.Credentials, buildCacheKey(scrapeConfig.Namespace, cc.Authorization.Credentials.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate authorization for consulSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.authorizationSecrets[scrapeConfig.AsMapKey("consulsd", i)] = secretValue
			}
			if cc.OAuth2 != nil {
				oauth2, err := k8stools.LoadOAuthSecrets(ctx, rclient, cc.OAuth2, scrapeConfig.Namespace, ssCache.nsSecretCache, ssCache.nsCMCache)
				if err != nil {
					return fmt.Errorf("could not generate oauth2 for consulSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.oauth2Secrets[scrapeConfig.AsMapKey("consulsd", i)] = oauth2
			}
			if cc.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, cc.ProxyClientConfig, scrapeConfig.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate proxy auth for consulSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				if ba != nil {
					ssCache.baSecrets[scrapeConfig.AsProxyKey("consulsd", i)] = ba
				}
				ssCache.bearerTokens[scrapeConfig.AsProxyKey("consulsd", i)] = token
			}
		}
		for i, ec := range scrapeConfig.Spec.EC2SDConfigs {
			if ec.AccessKey != nil {
				token, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, ec.AccessKey, buildCacheKey(scrapeConfig.Namespace, ec.AccessKey.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate token for consulSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.authorizationSecrets[scrapeConfig.AsMapKey("ec2sdAccess", i)] = token
			}
			if ec.SecretKey != nil {
				token, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, ec.SecretKey, buildCacheKey(scrapeConfig.Namespace, ec.SecretKey.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate token for ec2SDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.authorizationSecrets[scrapeConfig.AsMapKey("ec2sdSecret", i)] = token
			}
		}
		for i, ac := range scrapeConfig.Spec.AzureSDConfigs {
			if ac.ClientSecret != nil {
				token, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, ac.ClientSecret, buildCacheKey(scrapeConfig.Namespace, ac.ClientSecret.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate token for azureSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.oauth2Secrets[scrapeConfig.AsMapKey("azuresd", i)] = &k8stools.OAuthCreds{ClientSecret: token}
			}
		}
		for i, oc := range scrapeConfig.Spec.OpenStackSDConfigs {
			if oc.Password != nil {
				token, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, oc.Password, buildCacheKey(scrapeConfig.Namespace, oc.Password.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not read password for openStackSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.authorizationSecrets[scrapeConfig.AsMapKey("openstacksd_password", i)] = token
			}
			if oc.ApplicationCredentialSecret != nil {
				token, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, oc.ApplicationCredentialSecret, buildCacheKey(scrapeConfig.Namespace, oc.ApplicationCredentialSecret.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not read applicationCredentialSecret for openStackSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.authorizationSecrets[scrapeConfig.AsMapKey("openstacksd_app", i)] = token
			}
		}
		for i, dc := range scrapeConfig.Spec.DigitalOceanSDConfigs {
			if dc.Authorization != nil && dc.Authorization.Credentials != nil {
				secretValue, err := k8stools.GetCredFromSecret(ctx, rclient, scrapeConfig.Namespace, dc.Authorization.Credentials, buildCacheKey(scrapeConfig.Namespace, dc.Authorization.Credentials.Name), ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate authorization for digitalOceanSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.authorizationSecrets[scrapeConfig.AsMapKey("digitaloceansd", i)] = secretValue
			}
			if dc.OAuth2 != nil {
				oauth2, err := k8stools.LoadOAuthSecrets(ctx, rclient, dc.OAuth2, scrapeConfig.Namespace, ssCache.nsSecretCache, ssCache.nsCMCache)
				if err != nil {
					return fmt.Errorf("could not generate oauth2 for digitalOceanSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				ssCache.oauth2Secrets[scrapeConfig.AsMapKey("digitaloceansd", i)] = oauth2
			}
			if dc.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, dc.ProxyClientConfig, scrapeConfig.Namespace, ssCache.nsSecretCache)
				if err != nil {
					return fmt.Errorf("could not generate proxy auth for digitalOceanSDConfigs %d in VMScrapeConfig %s. %w", i, scrapeConfig.Name, err)
				}
				if ba != nil {
					ssCache.baSecrets[scrapeConfig.AsProxyKey("digitaloceansd", i)] = ba
				}
				ssCache.bearerTokens[scrapeConfig.AsProxyKey("digitaloceansd", i)] = token
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// load apiserver basic auth secret
	// no need to filter out misconfiguration
	// it's VMAgent owner responsibility
	if apiserverConfig != nil {
		if apiserverConfig.BasicAuth != nil {
			credentials, err := k8stools.LoadBasicAuthSecret(ctx, rclient, vmagentCRNamespace, apiserverConfig.BasicAuth, ssCache.nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for apiserver config. %w", err)
			}
			ssCache.baSecrets["apiserver"] = &credentials
		}
		if apiserverConfig.Authorization != nil {
			secretValue, err := k8stools.GetCredFromSecret(ctx, rclient, vmagentCRNamespace, apiserverConfig.Authorization.Credentials, buildCacheKey(vmagentCRNamespace, "apiserver"), ssCache.nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch authorization secret for apiserver config: %w", err)
			}
			ssCache.authorizationSecrets["apiserver"] = secretValue
		}
		if err := addAssetsToCache(ctx, rclient, vmagentCRNamespace, apiserverConfig.TLSConfig, ssCache); err != nil {
			return nil, fmt.Errorf("cannot add tls asset for apiServerConfig %w", err)
		}
	}

	// load basic auth for remote write configuration
	// no need to filter out misconfiguration
	// it's VMAgent owner responsibility
	for _, rws := range remoteWriteSpecs {
		if rws.BasicAuth != nil {
			credentials, err := k8stools.LoadBasicAuthSecret(ctx, rclient, vmagentCRNamespace, rws.BasicAuth, ssCache.nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for remote write spec %s config. %w", rws.URL, err)
			}
			ssCache.baSecrets[rws.AsMapKey()] = &credentials
		}
		if rws.OAuth2 != nil {
			oauth2, err := k8stools.LoadOAuthSecrets(ctx, rclient, rws.OAuth2, vmagentCRNamespace, ssCache.nsSecretCache, ssCache.nsCMCache)
			if err != nil {
				return nil, fmt.Errorf("cannot load oauth2 creds for :%s, ns: %s, err: %w", "remoteWrite", vmagentCRNamespace, err)
			}
			ssCache.oauth2Secrets[rws.AsMapKey()] = oauth2
		}
		if rws.BearerTokenSecret != nil && rws.BearerTokenSecret.Name != "" {
			token, err := k8stools.GetCredFromSecret(ctx, rclient, vmagentCRNamespace, rws.BearerTokenSecret, buildCacheKey(vmagentCRNamespace, rws.BearerTokenSecret.Name), ssCache.nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("cannot get bearer token for remoteWrite: %w", err)
			}
			ssCache.bearerTokens[rws.AsMapKey()] = token
		}
		if err := addAssetsToCache(ctx, rclient, vmagentCRNamespace, rws.TLSConfig, ssCache); err != nil {
			return nil, fmt.Errorf("cannot add asset for remote write target: %w", err)
		}

	}

	return ssCache, nil
}

func loadBasicAuthSecretFromAPI(ctx context.Context, rclient client.Client, basicAuth *vmv1beta1.BasicAuth, ns string, cache map[string]*corev1.Secret) (*k8stools.BasicAuthCredentials, error) {
	var username string
	var password string
	var err error

	if username, err = k8stools.GetCredFromSecret(ctx, rclient, ns, &basicAuth.Username, ns+"/"+basicAuth.Username.Name, cache); err != nil {
		return nil, err
	}

	if password, err = k8stools.GetCredFromSecret(ctx, rclient, ns, &basicAuth.Password, ns+"/"+basicAuth.Password.Name, cache); err != nil {
		return nil, err
	}

	return &k8stools.BasicAuthCredentials{Username: username, Password: password}, nil
}

func buildCacheKey(ns, keyName string) string {
	return fmt.Sprintf("%s/%s", ns, keyName)
}

func loadProxySecrets(ctx context.Context, rclient client.Client, proxyCfg *vmv1beta1.ProxyAuth, ns string, cache map[string]*corev1.Secret) (ba *k8stools.BasicAuthCredentials, token string, err error) {
	if proxyCfg.BasicAuth != nil {
		ba, err = loadBasicAuthSecretFromAPI(ctx, rclient, proxyCfg.BasicAuth, ns, cache)
		if err != nil {
			err = fmt.Errorf("cannot load basic auth proxy secret: %w", err)
			return
		}

	}
	if proxyCfg.BearerToken != nil {
		token, err = k8stools.GetCredFromSecret(
			ctx,
			rclient,
			ns,
			proxyCfg.BearerToken,
			buildCacheKey(ns, proxyCfg.BearerToken.Name),
			cache,
		)
		if err != nil {
			err = fmt.Errorf("cannot load bearer token proxy secret: %w", err)
			return
		}
	}
	return
}

func loadAdditionalScrapeConfigsSecret(ctx context.Context, rclient client.Client, additionalScrapeConfigs *corev1.SecretKeySelector, namespace string) ([]byte, error) {
	if additionalScrapeConfigs != nil {
		var s corev1.Secret
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: additionalScrapeConfigs.Name}, &s); err != nil {
			if errors.IsNotFound(err) {
				return nil, fmt.Errorf("cannot find secret with additional config for vmagent, secret: %s, namespace: %s", additionalScrapeConfigs.Name, namespace)
			}
			return nil, err
		}
		if c, ok := s.Data[additionalScrapeConfigs.Key]; ok {
			return c, nil
		}
		if additionalScrapeConfigs.Optional == nil || !*additionalScrapeConfigs.Optional {
			return nil, fmt.Errorf("secret %v could not be found", additionalScrapeConfigs.Name)
		}
	}
	return nil, nil
}

func testForArbitraryFSAccess(e vmv1beta1.EndpointAuth) error {
	if e.BearerTokenFile != "" {
		return fmt.Errorf("it accesses file system via bearer token file which VMAgent specification prohibits")
	}
	if e.BasicAuth != nil && e.BasicAuth.PasswordFile != "" {
		return fmt.Errorf("it accesses file system via basicAuth password file which VMAgent specification prohibits")
	}

	if e.OAuth2 != nil && e.OAuth2.ClientSecretFile != "" {
		return fmt.Errorf("it accesses file system via oauth2 client secret file which VMAgent specification prohibits")
	}

	tlsConf := e.TLSConfig
	if tlsConf == nil {
		return nil
	}

	if err := e.TLSConfig.Validate(); err != nil {
		return err
	}

	if tlsConf.CAFile != "" || tlsConf.CertFile != "" || tlsConf.KeyFile != "" {
		return fmt.Errorf("it accesses file system via tls config which VMAgent specification prohibits")
	}

	return nil
}

func gzipConfig(buf *bytes.Buffer, conf []byte) error {
	w := gzip.NewWriter(buf)
	defer w.Close()
	if _, err := w.Write(conf); err != nil {
		return err
	}
	return nil
}

func setScrapeIntervalToWithLimit(ctx context.Context, dst *vmv1beta1.EndpointScrapeParams, vmagentCR *vmv1beta1.VMAgent) {
	if dst.ScrapeInterval == "" {
		dst.ScrapeInterval = dst.Interval
	}

	originInterval, minIntervalStr, maxIntervalStr := dst.ScrapeInterval, vmagentCR.Spec.MinScrapeInterval, vmagentCR.Spec.MaxScrapeInterval
	if originInterval == "" || (minIntervalStr == nil && maxIntervalStr == nil) {
		// fast path
		return
	}
	originDurationMs, err := metricsql.DurationValue(originInterval, 0)
	if err != nil {
		logger.WithContext(ctx).Error(err, fmt.Sprintf("cannot parse duration value during limiting interval, using original value: %s", originInterval))
		return
	}

	if minIntervalStr != nil {
		parsedMinMs, err := metricsql.DurationValue(*minIntervalStr, 0)
		if err != nil {
			logger.WithContext(ctx).Error(err, fmt.Sprintf("cannot parse minScrapeInterval: %s, using original value: %s", *minIntervalStr, originInterval))
			return
		}
		if parsedMinMs >= originDurationMs {
			dst.ScrapeInterval = *minIntervalStr
			return
		}
	}
	if maxIntervalStr != nil {
		parsedMaxMs, err := metricsql.DurationValue(*maxIntervalStr, 0)
		if err != nil {
			logger.WithContext(ctx).Error(err, fmt.Sprintf("cannot parse maxScrapeInterval: %s, using origin value: %s", *maxIntervalStr, originInterval))
			return
		}
		if parsedMaxMs < originDurationMs {
			dst.ScrapeInterval = *maxIntervalStr
			return
		}
	}
}

const (
	defaultScrapeInterval          = "30s"
	kubernetesSDRoleEndpoint       = "endpoints"
	kubernetesSDRoleService        = "service"
	kubernetesSDRoleEndpointSlices = "endpointslices"
	kubernetesSDRolePod            = "pod"
	kubernetesSDRoleIngress        = "ingress"
	kubernetesSDRoleNode           = "node"
)

var invalidLabelCharRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func generateConfig(
	ctx context.Context,
	cr *vmv1beta1.VMAgent,
	sos *scrapeObjects,
	secretsCache *scrapesSecretsCache,
	additionalScrapeConfigs []byte,
) ([]byte, error) {
	cfg := yaml.MapSlice{}
	if !config.IsClusterWideAccessAllowed() && cr.IsOwnsServiceAccount() {
		logger.WithContext(ctx).Info("Setting discovery for the single namespace only." +
			"Since operator launched with set WATCH_NAMESPACE param. " +
			"Set custom ServiceAccountName property for VMAgent if needed.")
		cr.Spec.IgnoreNamespaceSelectors = true
	}

	if cr.Spec.ScrapeInterval == "" {
		cr.Spec.ScrapeInterval = defaultScrapeInterval
	}

	globalItems := yaml.MapSlice{
		{Key: "scrape_interval", Value: cr.Spec.ScrapeInterval},
		{Key: "external_labels", Value: buildExternalLabels(cr)},
	}
	if cr.Spec.ScrapeTimeout != "" {
		globalItems = append(globalItems, yaml.MapItem{
			Key:   "scrape_timeout",
			Value: cr.Spec.ScrapeTimeout,
		})
	}

	cfg = append(cfg, yaml.MapItem{Key: "global", Value: globalItems})

	apiserverConfig := cr.Spec.APIServerConfig

	var scrapeConfigs []yaml.MapSlice
	for _, ss := range sos.sss {
		for i, ep := range ss.Spec.Endpoints {
			scrapeConfigs = append(scrapeConfigs,
				generateServiceScrapeConfig(
					ctx,
					cr,
					ss,
					ep, i,
					apiserverConfig,
					secretsCache,
					cr.Spec.VMAgentSecurityEnforcements,
				))
		}
	}
	for _, identifier := range sos.pss {
		for i, ep := range identifier.Spec.PodMetricsEndpoints {
			scrapeConfigs = append(scrapeConfigs,
				generatePodScrapeConfig(
					ctx,
					cr,
					identifier, ep, i,
					apiserverConfig,
					secretsCache,
					cr.Spec.VMAgentSecurityEnforcements,
				))
		}
	}

	for i, identifier := range sos.prss {
		scrapeConfigs = append(scrapeConfigs,
			generateProbeConfig(
				ctx,
				cr,
				identifier,
				i,
				apiserverConfig,
				secretsCache,
				cr.Spec.VMAgentSecurityEnforcements,
			))
	}
	for i, identifier := range sos.nss {
		scrapeConfigs = append(scrapeConfigs,
			generateNodeScrapeConfig(
				ctx,
				cr,
				identifier,
				i,
				apiserverConfig,
				secretsCache,
				cr.Spec.VMAgentSecurityEnforcements,
			))
	}

	for _, identifier := range sos.stss {
		for i, ep := range identifier.Spec.TargetEndpoints {
			scrapeConfigs = append(scrapeConfigs,
				generateStaticScrapeConfig(
					ctx,
					cr,
					identifier,
					ep, i,
					secretsCache,
					cr.Spec.VMAgentSecurityEnforcements,
				))
		}
	}

	for _, identifier := range sos.scss {
		scrapeConfigs = append(scrapeConfigs,
			generateScrapeConfig(
				ctx,
				cr,
				identifier,
				secretsCache,
				cr.Spec.VMAgentSecurityEnforcements,
			))
	}

	var additionalScrapeConfigsYaml []yaml.MapSlice
	if err := yaml.Unmarshal(additionalScrapeConfigs, &additionalScrapeConfigsYaml); err != nil {
		return nil, fmt.Errorf("unmarshalling additional scrape configs failed: %w", err)
	}

	var inlineScrapeConfigsYaml []yaml.MapSlice
	if len(cr.Spec.InlineScrapeConfig) > 0 {
		if err := yaml.Unmarshal([]byte(cr.Spec.InlineScrapeConfig), &inlineScrapeConfigsYaml); err != nil {
			return nil, fmt.Errorf("unmarshalling  inline additional scrape configs failed: %w", err)
		}
	}
	additionalScrapeConfigsYaml = append(additionalScrapeConfigsYaml, inlineScrapeConfigsYaml...)
	cfg = append(cfg, yaml.MapItem{
		Key:   "scrape_configs",
		Value: append(scrapeConfigs, additionalScrapeConfigsYaml...),
	})

	return yaml.Marshal(cfg)
}

func buildConfigMeta(cr *vmv1beta1.VMAgent) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:            cr.PrefixedName(),
		Annotations:     cr.AnnotationsFiltered(),
		Labels:          cr.AllLabels(),
		Namespace:       cr.Namespace,
		OwnerReferences: cr.AsOwner(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
	}
}

func makeConfigSecret(cr *vmv1beta1.VMAgent, ssCache *scrapesSecretsCache) *corev1.Secret {
	s := &corev1.Secret{
		ObjectMeta: buildConfigMeta(cr),
		Data: map[string][]byte{
			vmagentGzippedFilename: {},
		},
	}
	for idx, rw := range cr.Spec.RemoteWrite {
		if rw.BearerTokenSecret != nil {
			token, ok := ssCache.bearerTokens[rw.AsMapKey()]
			if !ok {
				panic(fmt.Sprintf("bug, remoteWriteSpec bearerToken is missing: %s", rw.AsMapKey()))
			}
			s.Data[rw.AsSecretKey(idx, "bearerToken")] = []byte(token)
		}
		if rw.BasicAuth != nil && len(rw.BasicAuth.Password.Name) > 0 {
			ba, ok := ssCache.baSecrets[rw.AsMapKey()]
			if !ok {
				panic(fmt.Sprintf("bug, remoteWriteSpec basicAuth is missing: %s", rw.AsMapKey()))
			}
			s.Data[rw.AsSecretKey(idx, "basicAuthPassword")] = []byte(ba.Password)
		}
		if rw.OAuth2 != nil {
			oauth2, ok := ssCache.oauth2Secrets[rw.AsMapKey()]
			if !ok {
				panic(fmt.Sprintf("bug, remoteWriteSpec oauth2 is missing: %s", rw.AsMapKey()))
			}
			s.Data[rw.AsSecretKey(idx, "oauth2Secret")] = []byte(oauth2.ClientSecret)
		}
	}
	return s
}

func sanitizeLabelName(name string) string {
	return invalidLabelCharRE.ReplaceAllString(name, "_")
}

func stringMapToMapSlice(m map[string]string) yaml.MapSlice {
	res := yaml.MapSlice{}
	ks := make([]string, 0)

	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)

	for _, k := range ks {
		res = append(res, yaml.MapItem{Key: k, Value: m[k]})
	}

	return res
}

// honorLabels determines the value of honor_labels.
// if overrideHonorLabels is true and user tries to set the
// value to true, we want to set honor_labels to false.
func honorLabels(userHonorLabels, overrideHonorLabels bool) bool {
	if userHonorLabels && overrideHonorLabels {
		return false
	}
	return userHonorLabels
}

// honorTimestamps adds option to enforce honor_timestamps option in scrape_config.
// We want to disable honoring timestamps when user specified it or when global
// override is set. For backwards compatibility with prometheus <2.9.0 we don't
// set honor_timestamps when that option wasn't specified anywhere
func honorTimestamps(cfg yaml.MapSlice, userHonorTimestamps *bool, overrideHonorTimestamps bool) yaml.MapSlice {
	// Ensuring backwards compatibility by checking if user set any option
	if userHonorTimestamps == nil && !overrideHonorTimestamps {
		return cfg
	}

	honor := false
	if userHonorTimestamps != nil {
		honor = *userHonorTimestamps
	}

	return append(cfg, yaml.MapItem{Key: "honor_timestamps", Value: honor && !overrideHonorTimestamps})
}

func addAttachMetadata(dst yaml.MapSlice, am *vmv1beta1.AttachMetadata) yaml.MapSlice {
	if am == nil {
		return dst
	}
	if am.Node != nil && *am.Node {
		dst = append(dst, yaml.MapItem{
			Key: "attach_metadata",
			Value: yaml.MapSlice{
				yaml.MapItem{
					Key:   "node",
					Value: true,
				},
			},
		})
	}
	return dst
}

func addTLStoYaml(cfg yaml.MapSlice, namespace string, tls *vmv1beta1.TLSConfig, addDirect bool) yaml.MapSlice {
	if tls != nil {
		pathPrefix := path.Join(tlsAssetsDir, namespace)
		tlsConfig := yaml.MapSlice{
			{Key: "insecure_skip_verify", Value: tls.InsecureSkipVerify},
		}
		if tls.CAFile != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "ca_file", Value: tls.CAFile})
		} else if tls.CA.PrefixedName() != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "ca_file", Value: tls.BuildAssetPath(pathPrefix, tls.CA.PrefixedName(), tls.CA.Key())})
		}
		if tls.CertFile != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "cert_file", Value: tls.CertFile})
		} else if tls.Cert.PrefixedName() != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "cert_file", Value: tls.BuildAssetPath(pathPrefix, tls.Cert.PrefixedName(), tls.Cert.Key())})
		}
		if tls.KeyFile != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "key_file", Value: tls.KeyFile})
		} else if tls.KeySecret != nil {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "key_file", Value: tls.BuildAssetPath(pathPrefix, tls.KeySecret.Name, tls.KeySecret.Key)})
		}
		if tls.ServerName != "" {
			tlsConfig = append(tlsConfig, yaml.MapItem{Key: "server_name", Value: tls.ServerName})
		}
		if addDirect {
			cfg = append(cfg, tlsConfig...)
			return cfg
		}
		cfg = append(cfg, yaml.MapItem{Key: "tls_config", Value: tlsConfig})
	}
	return cfg
}

func addRelabelConfigs(dst []yaml.MapSlice, rcs []vmv1beta1.RelabelConfig) []yaml.MapSlice {
	for i := range rcs {
		rc := &rcs[i]
		if rc.IsEmpty() {
			continue
		}
		dst = append(dst, generateRelabelConfig(rc))
	}
	return dst
}

func generateRelabelConfig(rc *vmv1beta1.RelabelConfig) yaml.MapSlice {
	relabeling := yaml.MapSlice{}

	if len(rc.SourceLabels) > 0 {
		relabeling = append(relabeling, yaml.MapItem{Key: "source_labels", Value: rc.SourceLabels})
	}

	if rc.Separator != nil {
		relabeling = append(relabeling, yaml.MapItem{Key: "separator", Value: *rc.Separator})
	}

	if rc.TargetLabel != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "target_label", Value: rc.TargetLabel})
	}

	if len(rc.Regex) > 0 {
		// dirty hack to properly format regex
		if len(rc.Regex) == 1 {
			relabeling = append(relabeling, yaml.MapItem{Key: "regex", Value: rc.Regex[0]})
		} else {
			relabeling = append(relabeling, yaml.MapItem{Key: "regex", Value: rc.Regex})
		}
	}

	if rc.Modulus != uint64(0) {
		relabeling = append(relabeling, yaml.MapItem{Key: "modulus", Value: rc.Modulus})
	}

	if rc.Replacement != nil {
		relabeling = append(relabeling, yaml.MapItem{Key: "replacement", Value: *rc.Replacement})
	}

	if rc.Action != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "action", Value: rc.Action})
	}
	if len(rc.If) != 0 {
		relabeling = append(relabeling, yaml.MapItem{Key: "if", Value: rc.If})
	}
	if rc.Match != "" {
		relabeling = append(relabeling, yaml.MapItem{Key: "match", Value: rc.Match})
	}
	if len(rc.Labels) > 0 {
		sortKeys := make([]string, 0, len(rc.Labels))
		labels := make(yaml.MapSlice, 0, len(rc.Labels))
		for key := range rc.Labels {
			sortKeys = append(sortKeys, key)
		}
		sort.Strings(sortKeys)
		for idx := range sortKeys {
			key := sortKeys[idx]
			labels = append(labels, yaml.MapItem{Key: key, Value: rc.Labels[key]})
		}
		relabeling = append(relabeling, yaml.MapItem{Key: "labels", Value: labels})
	}

	return relabeling
}

// getNamespacesFromNamespaceSelector gets a list of namespaces to select based on
// the given namespace selector, the given default namespace, and whether to ignore namespace selectors
func getNamespacesFromNamespaceSelector(nsSelector *vmv1beta1.NamespaceSelector, namespace string, ignoreNamespaceSelectors bool) []string {
	switch {
	case ignoreNamespaceSelectors:
		return []string{namespace}
	case nsSelector.Any:
		return []string{}
	case len(nsSelector.MatchNames) == 0:
		return []string{namespace}
	default:
		return nsSelector.MatchNames
	}
}

func combineSelectorStr(kvs map[string]string) string {
	kvsSlice := make([]string, 0, len(kvs))
	for k, v := range kvs {
		kvsSlice = append(kvsSlice, fmt.Sprintf("%v=%v", k, v))
	}

	// Ensure we generate the same selector string for the same kvs,
	// regardless of Go map iteration order.
	sort.Strings(kvsSlice)

	return strings.Join(kvsSlice, ",")
}

func generateK8SSDConfig(namespaces []string, apiserverConfig *vmv1beta1.APIServerConfig, ssCache *scrapesSecretsCache, role string, am *vmv1beta1.AttachMetadata) yaml.MapItem {
	k8sSDConfig := yaml.MapSlice{
		{
			Key:   "role",
			Value: role,
		},
	}
	switch role {
	case kubernetesSDRoleEndpoint, kubernetesSDRoleEndpointSlices, kubernetesSDRolePod:
		k8sSDConfig = addAttachMetadata(k8sSDConfig, am)
	}
	if len(namespaces) != 0 {
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key: "namespaces",
			Value: yaml.MapSlice{
				{
					Key:   "names",
					Value: namespaces,
				},
			},
		})
	}

	if apiserverConfig != nil {
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key: "api_server", Value: apiserverConfig.Host,
		})

		if apiserverConfig.BasicAuth != nil {
			if s, ok := ssCache.baSecrets["apiserver"]; ok {
				k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
					Key: "basic_auth", Value: yaml.MapSlice{
						{Key: "username", Value: s.Username},
						{Key: "password", Value: s.Password},
					},
				})
			}
		}
		if apiserverConfig.Authorization != nil {
			k8sSDConfig = addAuthorizationConfigTo(k8sSDConfig, "apiserver", apiserverConfig.Authorization, ssCache.authorizationSecrets)
		}

		if apiserverConfig.BearerToken != "" {
			k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "bearer_token", Value: apiserverConfig.BearerToken})
		}

		if apiserverConfig.BearerTokenFile != "" {
			k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "bearer_token_file", Value: apiserverConfig.BearerTokenFile})
		}

		// config as well, make sure to path the right namespace here.
		k8sSDConfig = addTLStoYaml(k8sSDConfig, "", apiserverConfig.TLSConfig, false)
	}

	return yaml.MapItem{
		Key: "kubernetes_sd_configs",
		Value: []yaml.MapSlice{
			k8sSDConfig,
		},
	}
}

func enforceNamespaceLabel(relabelings []yaml.MapSlice, namespace, enforcedNamespaceLabel string) []yaml.MapSlice {
	if enforcedNamespaceLabel == "" {
		return relabelings
	}
	return append(relabelings, yaml.MapSlice{
		{Key: "target_label", Value: enforcedNamespaceLabel},
		{Key: "replacement", Value: namespace},
	})
}

func buildExternalLabels(p *vmv1beta1.VMAgent) yaml.MapSlice {
	m := map[string]string{}

	// Use "prometheus" external label name by default if field is missing.
	// in case of migration from prometheus to vmagent, it helps to have same labels
	// Do not add external label if field is set to empty string.
	prometheusExternalLabelName := "prometheus"
	if p.Spec.VMAgentExternalLabelName != nil {
		if *p.Spec.VMAgentExternalLabelName != "" {
			prometheusExternalLabelName = *p.Spec.VMAgentExternalLabelName
		} else {
			prometheusExternalLabelName = ""
		}
	}

	if prometheusExternalLabelName != "" {
		m[prometheusExternalLabelName] = fmt.Sprintf("%s/%s", p.Namespace, p.Name)
	}

	for n, v := range p.Spec.ExternalLabels {
		m[n] = v
	}
	return stringMapToMapSlice(m)
}

func buildVMScrapeParams(namespace, cacheKey string, cfg *vmv1beta1.VMScrapeParams, ssCache *scrapesSecretsCache) yaml.MapSlice {
	var r yaml.MapSlice
	if cfg == nil {
		return r
	}
	toYaml := func(key string, src any) {
		if src == nil || reflect.ValueOf(src).IsNil() {
			return
		}
		r = append(r, yaml.MapItem{Key: key, Value: src})
	}
	toYaml("scrape_align_interval", cfg.ScrapeAlignInterval)
	toYaml("stream_parse", cfg.StreamParse)
	toYaml("disable_compression", cfg.DisableCompression)
	toYaml("scrape_offset", cfg.ScrapeOffset)
	toYaml("no_stale_markers", cfg.DisableStaleMarkers)
	toYaml("disable_keepalive", cfg.DisableKeepAlive)
	if len(cfg.Headers) > 0 {
		r = append(r, yaml.MapItem{Key: "headers", Value: cfg.Headers})
	}
	if cfg.ProxyClientConfig != nil {
		r = append(r, buildProxyAuthConfig(namespace, cacheKey, cfg.ProxyClientConfig, ssCache)...)
	}
	return r
}

func addAuthorizationConfigTo(dst yaml.MapSlice, cacheKey string, cfg *vmv1beta1.Authorization, authorizationCache map[string]string) yaml.MapSlice {
	if cfg == nil {
		// fast path
		return dst
	}
	secretValue, ok := authorizationCache[cacheKey]
	if !ok && len(cfg.CredentialsFile) == 0 {
		return dst
	}
	var r yaml.MapSlice
	authType := cfg.Type
	if len(authType) == 0 {
		authType = "Bearer"
	}
	r = append(r, yaml.MapItem{Key: "type", Value: authType})
	if len(secretValue) > 0 {
		r = append(r, yaml.MapItem{Key: "credentials", Value: secretValue})
	} else {
		r = append(r, yaml.MapItem{Key: "credentials_file", Value: cfg.CredentialsFile})
	}

	dst = append(dst, yaml.MapItem{Key: "authorization", Value: r})
	return dst
}

func addOAuth2ConfigTo(dst yaml.MapSlice, cacheKey string, cfg *vmv1beta1.OAuth2, oauth2Cache map[string]*k8stools.OAuthCreds) yaml.MapSlice {
	cachedSecret := oauth2Cache[cacheKey]
	if cfg == nil || cachedSecret == nil {
		// fast path
		return dst
	}
	var r yaml.MapSlice
	if len(cachedSecret.ClientID) > 0 {
		r = append(r, yaml.MapItem{Key: "client_id", Value: cachedSecret.ClientID})
	}

	if cfg.ClientSecret != nil {
		r = append(r, yaml.MapItem{Key: "client_secret", Value: cachedSecret.ClientSecret})
	} else if cfg.ClientSecretFile != "" {
		r = append(r, yaml.MapItem{Key: "client_secret_file", Value: cfg.ClientSecretFile})
	}

	if len(cfg.Scopes) > 0 {
		r = append(r, yaml.MapItem{Key: "scopes", Value: cfg.Scopes})
	}
	if len(cfg.EndpointParams) > 0 {
		r = append(r, yaml.MapItem{Key: "endpoint_params", Value: cfg.EndpointParams})
	}
	if len(cfg.TokenURL) > 0 {
		r = append(r, yaml.MapItem{Key: "token_url", Value: cfg.TokenURL})
	}
	if len(r) == 0 {
		return dst
	}
	dst = append(dst, yaml.MapItem{Key: "oauth2", Value: r})
	return dst
}

func buildProxyAuthConfig(namespace, cacheKey string, proxyAuth *vmv1beta1.ProxyAuth, ssCache *scrapesSecretsCache) yaml.MapSlice {
	var r yaml.MapSlice
	if proxyAuth.BasicAuth != nil {
		var pa yaml.MapSlice
		if ba, ok := ssCache.baSecrets[cacheKey]; ok {
			pa = append(pa,
				yaml.MapItem{Key: "username", Value: ba.Username},
				yaml.MapItem{Key: "password", Value: ba.Password},
			)
		}
		if len(proxyAuth.BasicAuth.PasswordFile) > 0 {
			pa = append(pa, yaml.MapItem{Key: "password_file", Value: proxyAuth.BasicAuth.PasswordFile})
		}
		if len(pa) > 0 {
			r = append(r, yaml.MapItem{Key: "proxy_basic_auth", Value: pa})
		}
	}
	if proxyAuth.TLSConfig != nil {
		t := addTLStoYaml(yaml.MapSlice{}, namespace, proxyAuth.TLSConfig, true)
		if len(t) > 0 {
			r = append(r, yaml.MapItem{Key: "proxy_tls_config", Value: t})
		}
	}

	if proxyAuth.BearerToken != nil {
		if bt, ok := ssCache.bearerTokens[cacheKey]; ok {
			r = append(r, yaml.MapItem{Key: "proxy_bearer_token", Value: bt})
		}
	} else if len(proxyAuth.BearerTokenFile) > 0 {
		r = append(r, yaml.MapItem{Key: "proxy_bearer_token_file", Value: proxyAuth.BearerTokenFile})
	}
	return r
}

func addSelectorToRelabelingFor(relabelings []yaml.MapSlice, typeName string, selector metav1.LabelSelector) []yaml.MapSlice {
	// Exact label matches.
	var labelKeys []string
	for k := range selector.MatchLabels {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)

	for _, k := range labelKeys {
		relabelings = append(relabelings, yaml.MapSlice{
			{Key: "action", Value: "keep"},
			{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_label_%s", typeName, sanitizeLabelName(k))}},
			{Key: "regex", Value: selector.MatchLabels[k]},
		})
	}
	// Set based label matching. We have to map the valid relations
	// `In`, `NotIn`, `Exists`, and `DoesNotExist`, into relabeling rules.
	for _, exp := range selector.MatchExpressions {
		switch exp.Operator {
		case metav1.LabelSelectorOpIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_label_%s", typeName, sanitizeLabelName(exp.Key))}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpNotIn:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_label_%s", typeName, sanitizeLabelName(exp.Key))}},
				{Key: "regex", Value: strings.Join(exp.Values, "|")},
			})
		case metav1.LabelSelectorOpExists:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "keep"},
				{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_labelpresent_%s", typeName, sanitizeLabelName(exp.Key))}},
				{Key: "regex", Value: "true"},
			})
		case metav1.LabelSelectorOpDoesNotExist:
			relabelings = append(relabelings, yaml.MapSlice{
				{Key: "action", Value: "drop"},
				{Key: "source_labels", Value: []string{fmt.Sprintf("__meta_kubernetes_%s_labelpresent_%s", typeName, sanitizeLabelName(exp.Key))}},
				{Key: "regex", Value: "true"},
			})
		}
	}
	return relabelings
}

func addCommonScrapeParamsTo(cfg yaml.MapSlice, cs vmv1beta1.EndpointScrapeParams, se vmv1beta1.VMAgentSecurityEnforcements) yaml.MapSlice {
	hl := honorLabels(cs.HonorLabels, se.OverrideHonorLabels)
	cfg = append(cfg, yaml.MapItem{
		Key:   "honor_labels",
		Value: hl,
	})

	cfg = honorTimestamps(cfg, cs.HonorTimestamps, se.OverrideHonorTimestamps)

	if cs.ScrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: cs.ScrapeInterval})
	}
	if cs.ScrapeTimeout != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_timeout", Value: cs.ScrapeTimeout})
	}
	if cs.Path != "" {
		cfg = append(cfg, yaml.MapItem{Key: "metrics_path", Value: cs.Path})
	}
	if cs.ProxyURL != nil {
		cfg = append(cfg, yaml.MapItem{Key: "proxy_url", Value: cs.ProxyURL})
	}
	if cs.FollowRedirects != nil {
		cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: cs.FollowRedirects})
	}
	if len(cs.Params) > 0 {
		params := make(yaml.MapSlice, 0, len(cs.Params))
		paramIdxes := make([]string, len(cs.Params))
		var idxCnt int
		for k := range cs.Params {
			paramIdxes[idxCnt] = k
			idxCnt++
		}
		sort.Strings(paramIdxes)
		for _, k := range paramIdxes {
			params = append(params, yaml.MapItem{Key: k, Value: cs.Params[k]})
		}
		cfg = append(cfg, yaml.MapItem{Key: "params", Value: params})
	}
	if cs.Scheme != "" {
		// scheme may have uppercase format to be compatible with prometheus-operator objects
		// vmagent expects lower case format only
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: strings.ToLower(cs.Scheme)})
	}
	if cs.MaxScrapeSize != "" {
		cfg = append(cfg, yaml.MapItem{Key: "max_scrape_size", Value: cs.MaxScrapeSize})
	}
	if cs.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: cs.SampleLimit})
	}
	if cs.SeriesLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "series_limit", Value: cs.SeriesLimit})
	}
	return cfg
}

func addMetricRelabelingsTo(cfg yaml.MapSlice, src []*vmv1beta1.RelabelConfig, se vmv1beta1.VMAgentSecurityEnforcements) yaml.MapSlice {
	if len(src) == 0 {
		return cfg
	}
	var metricRelabelings []yaml.MapSlice
	for _, c := range src {
		if c.TargetLabel != "" && se.EnforcedNamespaceLabel != "" && c.TargetLabel == se.EnforcedNamespaceLabel {
			continue
		}
		relabeling := generateRelabelConfig(c)

		metricRelabelings = append(metricRelabelings, relabeling)
	}
	if len(metricRelabelings) == 0 {
		return cfg
	}
	cfg = append(cfg, yaml.MapItem{Key: "metric_relabel_configs", Value: metricRelabelings})
	return cfg
}

func addEndpointAuthTo(cfg yaml.MapSlice, ac vmv1beta1.EndpointAuth, key string, ssCache *scrapesSecretsCache) yaml.MapSlice {
	if ac.BearerTokenFile != "" {
		cfg = append(cfg, yaml.MapItem{Key: "bearer_token_file", Value: ac.BearerTokenFile})
	}

	if ac.BearerTokenSecret != nil && ac.BearerTokenSecret.Name != "" {
		if s, ok := ssCache.bearerTokens[key]; ok {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: s})
		}
	}
	if ac.BasicAuth != nil {
		var bac yaml.MapSlice
		if s, ok := ssCache.baSecrets[key]; ok {
			bac = append(bac,
				yaml.MapItem{Key: "username", Value: s.Username},
			)
			if len(s.Password) > 0 {
				bac = append(bac, yaml.MapItem{Key: "password", Value: s.Password})
			}
		}
		if len(ac.BasicAuth.PasswordFile) > 0 {
			bac = append(bac, yaml.MapItem{Key: "password_file", Value: ac.BasicAuth.PasswordFile})
		}
		if len(bac) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "basic_auth", Value: bac})
		}
	}
	cfg = addOAuth2ConfigTo(cfg, key, ac.OAuth2, ssCache.oauth2Secrets)
	cfg = addAuthorizationConfigTo(cfg, key, ac.Authorization, ssCache.authorizationSecrets)

	return cfg
}
