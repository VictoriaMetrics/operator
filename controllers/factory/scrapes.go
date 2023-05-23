package factory

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"

	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/utils"
	"github.com/VictoriaMetrics/metricsql"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	vmagentSecretFetchErrsTotal prometheus.Counter
)

func init() {
	vmagentSecretFetchErrsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "operator_vmagent_config_fetch_secret_errors_total",
		Help: "Indicates if user defined objects contain missing link for secret",
	})
	prometheus.MustRegister(vmagentSecretFetchErrsTotal)
}

type scrapesSecretsCache struct {
	bearerTokens         map[string]string
	baSecrets            map[string]*BasicAuthCredentials
	oauth2Secrets        map[string]*oauthCreds
	authorizationSecrets map[string]string
}

// CreateOrUpdateConfigurationSecret builds scrape configuration for VMAgent
func CreateOrUpdateConfigurationSecret(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client, c *config.BaseOperatorConf) (*scrapesSecretsCache, error) {

	sScrapes, err := SelectServiceScrapes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting ServiceScrapes failed: %w", err)
	}

	pScrapes, err := SelectPodScrapes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting PodScrapes failed: %w", err)
	}

	probes, err := SelectVMProbes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting VMProbes failed: %w", err)
	}

	nodes, err := SelectVMNodeScrapes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting VMNodeScrapes failed: %w", err)
	}

	statics, err := SelectStaticScrapes(ctx, cr, rclient)
	if err != nil {
		return nil, fmt.Errorf("selecting PodScrapes failed: %w", err)
	}

	ssCache, err := loadScrapeSecrets(ctx, rclient, sScrapes, nodes, pScrapes, probes, statics, cr.Spec.APIServerConfig, cr.Spec.RemoteWrite, cr.Namespace)
	if err != nil {
		if _, ok := err.(*utils.ErrGroup); !ok {
			return nil, fmt.Errorf("cannot load scrape target secrets for api server or remote writes: %w", err)
		}
		vmagentSecretFetchErrsTotal.Inc()
		log.Error(err, "found invalid secret references at objects, excluding it from configuration")
	}
	assets, err := loadTLSAssets(ctx, rclient, cr, sScrapes, pScrapes, probes, nodes, statics)
	if err != nil {
		if _, ok := err.(*utils.ErrGroup); !ok {
			return nil, fmt.Errorf("cannot load tls assets for api server or remote writes: %w", err)
		}
		vmagentSecretFetchErrsTotal.Inc()
		log.Error(err, "cannot load tls assets for targets, excluding it from configuration")
	}
	if err := createOrUpdateTlsAssets(ctx, cr, rclient, assets); err != nil {
		return nil, fmt.Errorf("cannot create tls assets secret for vmagent: %w", err)
	}

	additionalScrapeConfigs, err := loadAdditionalScrapeConfigsSecret(ctx, rclient, cr.Spec.AdditionalScrapeConfigs, cr.Namespace)
	if err != nil {
		return nil, fmt.Errorf("loading additional scrape configs from Secret failed: %w", err)
	}

	// Update secret based on the most recent configuration.
	generatedConfig, err := generateConfig(
		cr,
		sScrapes,
		pScrapes,
		probes,
		nodes,
		statics,
		ssCache,
		additionalScrapeConfigs,
	)
	if err != nil {
		return nil, fmt.Errorf("generating config for vmagent failed: %w", err)
	}

	s := makeConfigSecret(cr, c, ssCache)
	s.ObjectMeta.Annotations = map[string]string{
		"generated": "true",
	}

	// Compress config to avoid 1mb secret limit for a while
	var buf bytes.Buffer
	if err = gzipConfig(&buf, generatedConfig); err != nil {
		return nil, fmt.Errorf("cannot gzip config for vmagent: %w", err)
	}
	s.Data[vmagentGzippedFilename] = buf.Bytes()

	curSecret := &corev1.Secret{}
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: s.Name}, curSecret); err != nil {
		if errors.IsNotFound(err) {
			log.Info("creating new configuration secret for vmagent")
			return ssCache, rclient.Create(ctx, s)
		}
		return nil, fmt.Errorf("cannot ")
	}

	s.Annotations = labels.Merge(curSecret.Annotations, s.Annotations)
	s.Finalizers = victoriametricsv1beta1.MergeFinalizers(curSecret, victoriametricsv1beta1.FinalizerName)
	return ssCache, rclient.Update(ctx, s)
}

func SelectServiceScrapes(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client) (map[string]*victoriametricsv1beta1.VMServiceScrape, error) {

	res := make(map[string]*victoriametricsv1beta1.VMServiceScrape)

	var servScrapesCombined []victoriametricsv1beta1.VMServiceScrape

	namespaces, objSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.ServiceScrapeNamespaceSelector, cr.Spec.ServiceScrapeSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := visitObjectsWithSelector(ctx, rclient, namespaces, &victoriametricsv1beta1.VMServiceScrapeList{}, objSelector, cr.Spec.SelectAllByDefault, func(list client.ObjectList) {
		l := list.(*victoriametricsv1beta1.VMServiceScrapeList)
		for _, item := range l.Items {
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			servScrapesCombined = append(servScrapesCombined, item)
		}
	}); err != nil {
		return nil, err
	}

	for _, servScrape := range servScrapesCombined {
		m := servScrape.DeepCopy()
		res[servScrape.Namespace+"/"+servScrape.Name] = m
	}

	// filter out all service scrapes that access
	// the file system.
	if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
		for namespaceAndName, sm := range res {
			for _, endpoint := range sm.Spec.Endpoints {
				if err := testForArbitraryFSAccess(endpoint); err != nil {
					delete(res, namespaceAndName)
					log.Info("skipping vmservicescrape",
						"error", err.Error(),
						"vmservicescrape", namespaceAndName,
						"namespace", cr.Namespace,
						"vmagent", cr.Name,
					)
				}
			}
		}
	}

	serviceScrapes := []string{}
	for k := range res {
		serviceScrapes = append(serviceScrapes, k)
	}
	log.Info("selected ServiceScrapes", "servicescrapes", strings.Join(serviceScrapes, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func SelectPodScrapes(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client) (map[string]*victoriametricsv1beta1.VMPodScrape, error) {

	res := make(map[string]*victoriametricsv1beta1.VMPodScrape)

	var podScrapesCombined []victoriametricsv1beta1.VMPodScrape

	namespaces, objSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.PodScrapeNamespaceSelector, cr.Spec.PodScrapeSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := visitObjectsWithSelector(ctx, rclient, namespaces, &victoriametricsv1beta1.VMPodScrapeList{}, objSelector, cr.Spec.SelectAllByDefault, func(list client.ObjectList) {
		l := list.(*victoriametricsv1beta1.VMPodScrapeList)
		for _, item := range l.Items {
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			podScrapesCombined = append(podScrapesCombined, item)
		}
	}); err != nil {
		return nil, err
	}

	for _, podScrape := range podScrapesCombined {
		pm := podScrape.DeepCopy()
		res[podScrape.Namespace+"/"+podScrape.Name] = pm
	}
	podScrapes := make([]string, 0)
	for key := range res {
		podScrapes = append(podScrapes, key)
	}

	log.Info("selected PodScrapes", "podscrapes", strings.Join(podScrapes, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func SelectVMProbes(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client) (map[string]*victoriametricsv1beta1.VMProbe, error) {

	res := make(map[string]*victoriametricsv1beta1.VMProbe)
	var probesCombined []victoriametricsv1beta1.VMProbe
	namespaces, objSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.ProbeNamespaceSelector, cr.Spec.ProbeSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := visitObjectsWithSelector(ctx, rclient, namespaces, &victoriametricsv1beta1.VMProbeList{}, objSelector, cr.Spec.SelectAllByDefault, func(list client.ObjectList) {
		l := list.(*victoriametricsv1beta1.VMProbeList)
		for _, item := range l.Items {
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			probesCombined = append(probesCombined, item)
		}
	}); err != nil {
		return nil, err
	}

	for _, probe := range probesCombined {
		pm := probe.DeepCopy()
		res[probe.Namespace+"/"+probe.Name] = pm
	}
	probesList := make([]string, 0)
	for key := range res {
		probesList = append(probesList, key)
	}

	log.Info("selected VMProbes", "vmProbes", strings.Join(probesList, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func SelectVMNodeScrapes(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client) (map[string]*victoriametricsv1beta1.VMNodeScrape, error) {

	l := log.WithValues("vmagent", cr.Name)
	if !config.IsClusterWideAccessAllowed() && cr.IsOwnsServiceAccount() {
		l.Info("cannot use VMNodeScrape at operator in single namespace mode with default permissions. Create ServiceAccount for VMAgent manually if needed. Skipping config generation for it")
		return nil, nil
	}

	res := make(map[string]*victoriametricsv1beta1.VMNodeScrape)

	var nodesCombined []victoriametricsv1beta1.VMNodeScrape

	namespaces, objSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.NodeScrapeNamespaceSelector, cr.Spec.NodeScrapeSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := visitObjectsWithSelector(ctx, rclient, namespaces, &victoriametricsv1beta1.VMNodeScrapeList{}, objSelector, cr.Spec.SelectAllByDefault, func(list client.ObjectList) {
		l := list.(*victoriametricsv1beta1.VMNodeScrapeList)
		for _, item := range l.Items {
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			nodesCombined = append(nodesCombined, item)
		}
	}); err != nil {
		return nil, err
	}

	for _, node := range nodesCombined {
		pm := node.DeepCopy()
		res[node.Namespace+"/"+node.Name] = pm
	}
	nodesList := make([]string, 0)
	for key := range res {
		nodesList = append(nodesList, key)
	}

	l.Info("selected VMNodeScrapes", "VMNodeScrapes", strings.Join(nodesList, ","))

	return res, nil
}

func SelectStaticScrapes(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client) (map[string]*victoriametricsv1beta1.VMStaticScrape, error) {

	res := make(map[string]*victoriametricsv1beta1.VMStaticScrape)
	var staticScrapesCombined []victoriametricsv1beta1.VMStaticScrape

	namespaces, objSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.StaticScrapeNamespaceSelector, cr.Spec.StaticScrapeSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := visitObjectsWithSelector(ctx, rclient, namespaces, &victoriametricsv1beta1.VMStaticScrapeList{}, objSelector, cr.Spec.SelectAllByDefault, func(list client.ObjectList) {
		l := list.(*victoriametricsv1beta1.VMStaticScrapeList)
		for _, item := range l.Items {
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			staticScrapesCombined = append(staticScrapesCombined, item)
		}
	}); err != nil {
		return nil, err
	}

	for _, staticScrape := range staticScrapesCombined {
		pm := staticScrape.DeepCopy()
		res[staticScrape.Namespace+"/"+staticScrape.Name] = pm
	}
	staticScrapes := make([]string, 0)
	for key := range res {
		staticScrapes = append(staticScrapes, key)
	}

	log.Info("selected StaticScrapes", "staticScrapes", strings.Join(staticScrapes, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

// TODO: @f41gh7
// refactor it, use victoriametricsv1beta1.HTTPAuth for objects as embed struct
// it should remove boilerplate code
func loadScrapeSecrets(
	ctx context.Context,
	rclient client.Client,
	mons map[string]*victoriametricsv1beta1.VMServiceScrape,
	nodes map[string]*victoriametricsv1beta1.VMNodeScrape,
	pods map[string]*victoriametricsv1beta1.VMPodScrape,
	probes map[string]*victoriametricsv1beta1.VMProbe,
	statics map[string]*victoriametricsv1beta1.VMStaticScrape,
	apiserverConfig *victoriametricsv1beta1.APIServerConfig,
	remoteWriteSpecs []victoriametricsv1beta1.VMAgentRemoteWriteSpec,
	namespace string,
) (*scrapesSecretsCache, error) {

	oauth2Secret := make(map[string]*oauthCreds)
	authorizationSecrets := make(map[string]string)
	baSecrets := make(map[string]*BasicAuthCredentials)
	bearerSecrets := make(map[string]string)
	nsSecretCache := make(map[string]*corev1.Secret)
	nsCMCache := make(map[string]*corev1.ConfigMap)
	var errG utils.ErrGroup
	for key, mon := range mons {
		var epCnt int
		for i, ep := range mon.Spec.Endpoints {
			if ep.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, ep.BasicAuth, mon.Namespace, nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMServiceScrape: %w", err))
					continue
				}
				baSecrets[mon.AsMapKey(i)] = credentials
			}

			if ep.OAuth2 != nil {
				oauth2, err := loadOAuthSecrets(ctx, rclient, ep.OAuth2, mon.Namespace, nsSecretCache, nsCMCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMServiceScrape: %w", err))
					continue
				}
				oauth2Secret[mon.AsMapKey(i)] = oauth2
			}
			if ep.BearerTokenSecret != nil && ep.BearerTokenSecret.Name != "" {
				token, err := getCredFromSecret(ctx, rclient, mon.Namespace, ep.BearerTokenSecret, buildCacheKey(mon.Namespace, ep.BearerTokenSecret.Name), nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMServiceScrape: %w", err))
					continue
				}
				bearerSecrets[mon.AsMapKey(i)] = token
			}
			if ep.Authorization != nil && ep.Authorization.Credentials != nil {
				secretValue, err := getCredFromSecret(ctx, rclient, mon.Namespace, ep.Authorization.Credentials, buildCacheKey(mon.Namespace, ep.Authorization.Credentials.Name), nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMServiceScrape: %w", err))
					continue
				}
				authorizationSecrets[mon.AsMapKey(i)] = secretValue
			}
			if ep.VMScrapeParams != nil && ep.VMScrapeParams.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, ep.VMScrapeParams.ProxyClientConfig, mon.Namespace, nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMServiceScrape: %w", err))
					continue
				}
				if ba != nil {
					baSecrets[mon.AsProxyKey(i)] = ba
				}
				bearerSecrets[mon.AsProxyKey(i)] = token
			}
			mon.Spec.Endpoints[epCnt] = ep
			epCnt++
		}
		mon.Spec.Endpoints = mon.Spec.Endpoints[:epCnt]
		if len(mon.Spec.Endpoints) == 0 {
			delete(mons, key)
		}
	}

	for key, node := range nodes {
		onErr := func(err error) {
			delete(nodes, key)
			errG.Add(fmt.Errorf("cannot load secret for VMNodeScrape: %w", err))
		}
		if node.Spec.BasicAuth != nil {
			credentials, err := loadBasicAuthSecretFromAPI(ctx,
				rclient,
				node.Spec.BasicAuth,
				node.Namespace,
				nsSecretCache)
			if err != nil {
				onErr(err)
				continue
			}
			baSecrets[node.AsMapKey()] = credentials

		}
		if node.Spec.OAuth2 != nil {
			oauth2, err := loadOAuthSecrets(ctx, rclient, node.Spec.OAuth2, node.Namespace, nsSecretCache, nsCMCache)
			if err != nil {
				onErr(err)
				continue
			}
			oauth2Secret[node.AsMapKey()] = oauth2
		}
		if node.Spec.BearerTokenSecret != nil && node.Spec.BearerTokenSecret.Name != "" {
			token, err := getCredFromSecret(ctx, rclient, node.Namespace, node.Spec.BearerTokenSecret, buildCacheKey(node.Namespace, node.Spec.BearerTokenSecret.Name), nsSecretCache)
			if err != nil {
				onErr(err)
				continue
			}
			bearerSecrets[node.AsMapKey()] = token
		}
		if node.Spec.VMScrapeParams != nil && node.Spec.VMScrapeParams.ProxyClientConfig != nil {
			ba, token, err := loadProxySecrets(ctx, rclient, node.Spec.VMScrapeParams.ProxyClientConfig, node.Namespace, nsSecretCache)
			if err != nil {
				onErr(err)
				continue
			}
			if ba != nil {
				baSecrets[node.AsProxyKey()] = ba
			}
			bearerSecrets[node.AsProxyKey()] = token
		}
	}
	for key, pod := range pods {
		var epCnt int
		for i, ep := range pod.Spec.PodMetricsEndpoints {
			if ep.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, ep.BasicAuth, pod.Namespace, nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMPodScrape: %w", err))
					continue
				}
				baSecrets[pod.AsMapKey(i)] = credentials
			}
			if ep.OAuth2 != nil {
				oauth2, err := loadOAuthSecrets(ctx, rclient, ep.OAuth2, pod.Namespace, nsSecretCache, nsCMCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMPodScrape: %w", err))
					continue
				}
				oauth2Secret[pod.AsMapKey(i)] = oauth2
			}
			if ep.BearerTokenSecret != nil && ep.BearerTokenSecret.Name != "" {
				token, err := getCredFromSecret(ctx, rclient, pod.Namespace, ep.BearerTokenSecret, buildCacheKey(pod.Namespace, ep.BearerTokenSecret.Name), nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMPodScrape: %w", err))
					continue
				}
				bearerSecrets[pod.AsMapKey(i)] = token
			}
			if ep.VMScrapeParams != nil && ep.VMScrapeParams.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, ep.VMScrapeParams.ProxyClientConfig, pod.Namespace, nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMPodScrape: %w", err))
					continue
				}
				if ba != nil {
					baSecrets[pod.AsProxyKey(i)] = ba
				}
				bearerSecrets[pod.AsProxyKey(i)] = token
			}
			if ep.Authorization != nil && ep.Authorization.Credentials != nil {
				secretValue, err := getCredFromSecret(ctx, rclient, pod.Namespace, ep.Authorization.Credentials, buildCacheKey(pod.Namespace, ep.Authorization.Credentials.Name), nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("cannot load secret for VMPodScrape: %w", err))
					continue
				}
				authorizationSecrets[pod.AsMapKey(i)] = secretValue
			}
			pod.Spec.PodMetricsEndpoints[epCnt] = ep
			epCnt++
		}
		pod.Spec.PodMetricsEndpoints = pod.Spec.PodMetricsEndpoints[:epCnt]
		if len(pod.Spec.PodMetricsEndpoints) == 0 {
			delete(pods, key)
		}
	}

	for key, probe := range probes {
		onErr := func(err error) {
			delete(nodes, key)
			errG.Add(fmt.Errorf("cannot load secret for VMProbe: %w", err))
		}
		if probe.Spec.BasicAuth != nil {
			credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, probe.Spec.BasicAuth, probe.Namespace, nsSecretCache)
			if err != nil {
				onErr(fmt.Errorf("could not generate basicAuth for vmstaticScrape %s. %w", probe.Name, err))
				continue
			}
			baSecrets[probe.AsMapKey()] = credentials
		}
		if probe.Spec.OAuth2 != nil {
			oauth2, err := loadOAuthSecrets(ctx, rclient, probe.Spec.OAuth2, probe.Namespace, nsSecretCache, nsCMCache)
			if err != nil {
				onErr(err)
				continue
			}
			oauth2Secret[probe.AsMapKey()] = oauth2
		}
		if probe.Spec.BearerTokenSecret != nil && probe.Spec.BearerTokenSecret.Name != "" {
			token, err := getCredFromSecret(ctx, rclient, probe.Namespace, probe.Spec.BearerTokenSecret, buildCacheKey(probe.Namespace, probe.Spec.BearerTokenSecret.Name), nsSecretCache)
			if err != nil {
				onErr(err)
				continue
			}
			bearerSecrets[probe.AsMapKey()] = token
		}
		if probe.Spec.VMScrapeParams != nil && probe.Spec.VMScrapeParams.ProxyClientConfig != nil {
			ba, token, err := loadProxySecrets(ctx, rclient, probe.Spec.VMScrapeParams.ProxyClientConfig, probe.Namespace, nsSecretCache)
			if err != nil {
				onErr(err)
				continue
			}
			if ba != nil {
				baSecrets[probe.AsProxyKey()] = ba
			}
			bearerSecrets[probe.AsProxyKey()] = token
		}
		if probe.Spec.Authorization != nil && probe.Spec.Authorization.Credentials != nil {
			secretValue, err := getCredFromSecret(ctx, rclient, probe.Namespace, probe.Spec.Authorization.Credentials, buildCacheKey(probe.Namespace, probe.Spec.Authorization.Credentials.Name), nsSecretCache)
			if err != nil {
				onErr(err)
				continue
			}
			authorizationSecrets[probe.AsMapKey()] = secretValue
		}
	}

	for key, staticCfg := range statics {
		var epCnt int
		for i, ep := range staticCfg.Spec.TargetEndpoints {
			if ep.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, ep.BasicAuth, staticCfg.Namespace, nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("could not load secret for vmstaticScrape:  %w", err))
					continue
				}
				baSecrets[staticCfg.AsMapKey(i)] = credentials
			}
			if ep.OAuth2 != nil {
				oauth2, err := loadOAuthSecrets(ctx, rclient, ep.OAuth2, staticCfg.Namespace, nsSecretCache, nsCMCache)
				if err != nil {
					errG.Add(fmt.Errorf("could not load secret for vmstaticScrape:  %w", err))
					continue
				}
				oauth2Secret[staticCfg.AsMapKey(i)] = oauth2
			}
			if ep.BearerTokenSecret != nil && ep.BearerTokenSecret.Name != "" {
				token, err := getCredFromSecret(ctx, rclient, staticCfg.Namespace, ep.BearerTokenSecret, buildCacheKey(staticCfg.Namespace, ep.BearerTokenSecret.Name), nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("could not load secret for vmstaticScrape:  %w", err))
					continue
				}
				bearerSecrets[staticCfg.AsMapKey(i)] = token
			}
			if ep.VMScrapeParams != nil && ep.VMScrapeParams.ProxyClientConfig != nil {
				ba, token, err := loadProxySecrets(ctx, rclient, ep.VMScrapeParams.ProxyClientConfig, staticCfg.Namespace, nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("could not load secret for vmstaticScrape:  %w", err))
					continue
				}
				if ba != nil {
					baSecrets[staticCfg.AsProxyKey(i)] = ba
				}
				bearerSecrets[staticCfg.AsProxyKey(i)] = token
			}
			if ep.Authorization != nil && ep.Authorization.Credentials != nil {
				secretValue, err := getCredFromSecret(ctx, rclient, staticCfg.Namespace, ep.Authorization.Credentials, buildCacheKey(staticCfg.Namespace, ep.Authorization.Credentials.Name), nsSecretCache)
				if err != nil {
					errG.Add(fmt.Errorf("could not load secret for vmstaticScrape:  %w", err))
					continue
				}
				authorizationSecrets[staticCfg.AsMapKey(i)] = secretValue
			}
			staticCfg.Spec.TargetEndpoints[epCnt] = ep
			epCnt++
		}
		staticCfg.Spec.TargetEndpoints = staticCfg.Spec.TargetEndpoints[:epCnt]
		if len(staticCfg.Spec.TargetEndpoints) == 0 {
			delete(statics, key)
		}
	}

	// load apiserver basic auth secret
	// no need to filter out misconfiguration
	// it's VMAgent owner responsibility
	if apiserverConfig != nil {
		if apiserverConfig.BasicAuth != nil {
			credentials, err := loadBasicAuthSecret(ctx, rclient, namespace, apiserverConfig.BasicAuth)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for apiserver config. %w", err)
			}
			baSecrets["apiserver"] = &credentials
		}
		if apiserverConfig.Authorization != nil {
			secretValue, err := getCredFromSecret(ctx, rclient, namespace, apiserverConfig.Authorization.Credentials, buildCacheKey(namespace, "apiserver"), nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch authorization secret for apiserver config: %w", err)
			}
			authorizationSecrets["apiserver"] = secretValue
		}
	}

	// load basic auth for remote write configuration
	// no need to filter out misconfiguration
	// it's VMAgent owner responsibility
	for _, rws := range remoteWriteSpecs {
		if rws.BasicAuth != nil {
			credentials, err := loadBasicAuthSecret(ctx, rclient, namespace, rws.BasicAuth)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for remote write spec %s config. %w", rws.URL, err)
			}
			baSecrets[rws.AsMapKey()] = &credentials
		}
		if rws.OAuth2 != nil {
			oauth2, err := loadOAuthSecrets(ctx, rclient, rws.OAuth2, namespace, nsSecretCache, nsCMCache)

			if err != nil {
				return nil, fmt.Errorf("cannot load oauth2 creds for :%s, ns: %s, err: %w", "remoteWrite", namespace, err)
			}
			oauth2Secret[rws.AsMapKey()] = oauth2
		}
		if rws.BearerTokenSecret != nil && rws.BearerTokenSecret.Name != "" {
			token, err := getCredFromSecret(ctx, rclient, namespace, rws.BearerTokenSecret, buildCacheKey(namespace, rws.BearerTokenSecret.Name), nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("cannot get bearer token for remoteWrite: %w", err)
			}
			bearerSecrets[rws.AsMapKey()] = token
		}
	}

	return &scrapesSecretsCache{baSecrets: baSecrets, oauth2Secrets: oauth2Secret, bearerTokens: bearerSecrets, authorizationSecrets: authorizationSecrets}, errG.Err()
}

func loadBasicAuthSecret(ctx context.Context, rclient client.Client, ns string, basicAuth *victoriametricsv1beta1.BasicAuth) (BasicAuthCredentials, error) {
	var err error
	var bas corev1.Secret
	var bac BasicAuthCredentials
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: basicAuth.Username.Name}, &bas); err != nil {
		if errors.IsNotFound(err) {
			return BasicAuthCredentials{}, fmt.Errorf("basic auth username secret: %q not found", basicAuth.Username.Name)
		}
		return bac, err
	}
	if bac.username, err = extractCredKey(&bas, basicAuth.Username); err != nil {
		return bac, err
	}
	if len(basicAuth.Password.Name) == 0 {
		// fast path for empty password
		// it can be skipped or defined via password_file
		return bac, nil
	}
	if basicAuth.Username.Name != basicAuth.Password.Name {
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: basicAuth.Password.Name}, &bas); err != nil {
			if errors.IsNotFound(err) {
				return bac, fmt.Errorf("basic auth password secret: %q not found", basicAuth.Username.Name)
			}
			return bac, err
		}
	}
	if bac.password, err = extractCredKey(&bas, basicAuth.Password); err != nil {
		return bac, err
	}

	return bac, nil

}

func extractCredKey(secret *corev1.Secret, sel corev1.SecretKeySelector) (string, error) {
	if s, ok := secret.Data[sel.Key]; ok {
		return string(s), nil
	}
	return "", fmt.Errorf("secret key %q in secret %q not found", sel.Key, sel.Name)
}

func getCredFromSecret(
	ctx context.Context,
	rclient client.Client,
	ns string,
	sel *corev1.SecretKeySelector,
	cacheKey string,
	cache map[string]*corev1.Secret,
) (string, error) {
	var s *corev1.Secret
	var ok bool
	if sel == nil {
		return "", fmt.Errorf("BUG, secret key selector must be non nil for cache key: %s, ns: %s", cacheKey, ns)
	}
	if s, ok = cache[cacheKey]; !ok {
		s = &corev1.Secret{}
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: sel.Name}, s); err != nil {
			return "", fmt.Errorf("unable to fetch key from secret: %q for object: %q : %w", sel.Name, cacheKey, err)
		}
		cache[cacheKey] = s
	}
	v, err := extractCredKey(s, *sel)
	if err != nil {
		return "", fmt.Errorf("cannot find key: %q at secret: %q for object: %q", sel.Key, s.Name, cacheKey)
	}
	return v, nil
}

func getCredFromConfigMap(
	ctx context.Context,
	rclient client.Client,
	ns string,
	sel corev1.ConfigMapKeySelector,
	cacheKey string,
	cache map[string]*corev1.ConfigMap,
) (string, error) {
	var s *corev1.ConfigMap
	var ok bool

	if s, ok = cache[cacheKey]; !ok {
		s = &corev1.ConfigMap{}
		err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: sel.Name}, s)
		if err != nil {
			return "", fmt.Errorf("cannot get configmap: %s at namespace %s, err: %s", sel.Name, ns, err)
		}
		cache[cacheKey] = s
	}

	if a, ok := s.Data[sel.Key]; ok {
		return a, nil
	}
	return "", fmt.Errorf("key not found at configmap, key: %s, configmap %s ", sel.Key, sel.Name)
}

func loadBasicAuthSecretFromAPI(ctx context.Context, rclient client.Client, basicAuth *victoriametricsv1beta1.BasicAuth, ns string, cache map[string]*corev1.Secret) (*BasicAuthCredentials, error) {
	var username string
	var password string
	var err error

	if username, err = getCredFromSecret(ctx, rclient, ns, &basicAuth.Username, ns+"/"+basicAuth.Username.Name, cache); err != nil {
		return nil, err
	}

	if password, err = getCredFromSecret(ctx, rclient, ns, &basicAuth.Password, ns+"/"+basicAuth.Password.Name, cache); err != nil {
		return nil, err
	}

	return &BasicAuthCredentials{username: username, password: password}, nil
}

type oauthCreds struct {
	clientSecret string
	clientID     string
}

func buildCacheKey(ns, keyName string) string {
	return fmt.Sprintf("%s/%s", ns, keyName)
}

func loadProxySecrets(ctx context.Context, rclient client.Client, proxyCfg *victoriametricsv1beta1.ProxyAuth, ns string, cache map[string]*corev1.Secret) (ba *BasicAuthCredentials, token string, err error) {

	if proxyCfg.BasicAuth != nil {
		ba, err = loadBasicAuthSecretFromAPI(ctx, rclient, proxyCfg.BasicAuth, ns, cache)
		if err != nil {
			err = fmt.Errorf("cannot load basic auth proxy secret: %w", err)
			return
		}

	}
	if proxyCfg.BearerToken != nil {
		token, err = getCredFromSecret(
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

func loadOAuthSecrets(ctx context.Context, rclient client.Client, oauth2 *victoriametricsv1beta1.OAuth2, ns string, cache map[string]*corev1.Secret, cmCache map[string]*corev1.ConfigMap) (*oauthCreds, error) {
	var r oauthCreds
	if oauth2.ClientSecret != nil {
		s, err := getCredFromSecret(ctx, rclient, ns, oauth2.ClientSecret, buildCacheKey(ns, oauth2.ClientSecret.Name), cache)
		if err != nil {
			return nil, fmt.Errorf("cannot load oauth2 secret for: %s, err: %w", oauth2.ClientSecret.Name, err)
		}
		r.clientSecret = s
	}
	if oauth2.ClientID.Secret != nil {
		s, err := getCredFromSecret(ctx, rclient, ns, oauth2.ClientID.Secret, ns+"/"+oauth2.ClientID.Secret.Name, cache)
		if err != nil {
			return nil, fmt.Errorf("cannot load oauth2 secret for: %s, err: %w", oauth2.ClientID.Secret, err)
		}
		r.clientID = s
	} else if oauth2.ClientID.ConfigMap != nil {
		s, err := getCredFromConfigMap(ctx, rclient, ns, *oauth2.ClientID.ConfigMap, buildCacheKey(ns, oauth2.ClientID.ConfigMap.Name), cmCache)
		if err != nil {
			return nil, fmt.Errorf("cannot load oauth2 secret for: %s err: %w", oauth2.ClientID.ConfigMap.Name, err)
		}
		r.clientID = s
	}

	return &r, nil
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

func testForArbitraryFSAccess(e victoriametricsv1beta1.Endpoint) error {
	if e.BearerTokenFile != "" {
		return fmt.Errorf("it accesses file system via bearer token file which VMAgent specification prohibits")
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

func CreateVMServiceScrapeFromService(ctx context.Context, rclient client.Client, service *corev1.Service, serviceScrapeSpec *victoriametricsv1beta1.VMServiceScrapeSpec, metricPath string, filterPortNames ...string) error {
	endPoints := []victoriametricsv1beta1.Endpoint{}
	for _, servicePort := range service.Spec.Ports {
		var nameMatched bool
		for _, filter := range filterPortNames {
			if servicePort.Name == filter {
				nameMatched = true
				break
			}
		}
		if len(filterPortNames) > 0 && !nameMatched {
			continue
		}

		endPoints = append(endPoints, victoriametricsv1beta1.Endpoint{
			Port: servicePort.Name,
			Path: metricPath,
		})
	}

	var existVSS victoriametricsv1beta1.VMServiceScrape

	if serviceScrapeSpec == nil {
		serviceScrapeSpec = &victoriametricsv1beta1.VMServiceScrapeSpec{}
	}
	scrapeSvc := victoriametricsv1beta1.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:            service.Name,
			Namespace:       service.Namespace,
			OwnerReferences: service.OwnerReferences,
			Labels:          service.Labels,
			Annotations:     service.Annotations,
		},
		Spec: *serviceScrapeSpec,
	}
	// merge generated endpoints into user defined values by Port name
	// assume, that it must be unique.
	for _, generatedEP := range endPoints {
		var found bool
		for idx := range scrapeSvc.Spec.Endpoints {
			eps := &scrapeSvc.Spec.Endpoints[idx]
			if eps.Port == generatedEP.Port {
				found = true
				if eps.Path == "" {
					eps.Path = generatedEP.Path
				}
			}
		}
		if !found {
			scrapeSvc.Spec.Endpoints = append(scrapeSvc.Spec.Endpoints, generatedEP)
		}
	}
	// allow to manually define selectors
	// in some cases it may be useful
	// for instance when additional service created with extra pod ports
	if scrapeSvc.Spec.Selector.MatchLabels == nil && scrapeSvc.Spec.Selector.MatchExpressions == nil {
		scrapeSvc.Spec.Selector = metav1.LabelSelector{MatchLabels: service.Spec.Selector, MatchExpressions: []metav1.LabelSelectorRequirement{
			{Key: victoriametricsv1beta1.AdditionalServiceLabel, Operator: metav1.LabelSelectorOpDoesNotExist},
		}}
	}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, &existVSS)
	if err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, &scrapeSvc)
		}
		return err
	}
	updateIsNeeded := !equality.Semantic.DeepEqual(scrapeSvc.Spec, existVSS.Spec) || !equality.Semantic.DeepEqual(scrapeSvc.Labels, existVSS.Labels) || !equality.Semantic.DeepEqual(scrapeSvc.Annotations, existVSS.Annotations)
	existVSS.Spec = scrapeSvc.Spec
	existVSS.Labels = scrapeSvc.Labels
	existVSS.Annotations = scrapeSvc.Annotations
	if updateIsNeeded {
		return rclient.Update(ctx, &existVSS)
	}
	return nil
}

func limitScrapeInterval(origin string, minIntervalStr, maxIntervalStr *string) string {
	if origin == "" || (minIntervalStr == nil && maxIntervalStr == nil) {
		// fast path
		return origin
	}
	originDurationMs, err := metricsql.DurationValue(origin, 0)
	if err != nil {
		log.Error(err, "cannot parse duration value during limiting interval, using original value: %s", origin)
		return origin
	}

	if minIntervalStr != nil {
		parsedMinMs, err := metricsql.DurationValue(*minIntervalStr, 0)
		if err != nil {
			log.Error(err, "cannot parse minScrapeInterval: %s, using original value: %s", *minIntervalStr, origin)
			return origin
		}
		if parsedMinMs >= originDurationMs {
			return *minIntervalStr
		}
	}
	if maxIntervalStr != nil {
		parsedMaxMs, err := metricsql.DurationValue(*maxIntervalStr, 0)
		if err != nil {
			log.Error(err, "cannot parse maxScrapeInterval: %s, using origin value: %s", *maxIntervalStr, origin)
			return origin
		}
		if parsedMaxMs < originDurationMs {
			return *maxIntervalStr
		}
	}

	return origin
}
