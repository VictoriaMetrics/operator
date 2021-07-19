package factory

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/apimachinery/pkg/api/equality"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateOrUpdateConfigurationSecret(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client, c *config.BaseOperatorConf) error {

	smons, err := SelectServiceScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting ServiceScrapes failed: %w", err)
	}

	pmons, err := SelectPodScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting PodScrapes failed: %w", err)
	}

	probes, err := SelectVMProbes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting VMProbes failed: %w", err)
	}

	nodes, err := SelectVMNodeScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting VMNodeScrapes failed: %w", err)
	}

	statics, err := SelectStaticScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting PodScrapes failed: %w", err)
	}

	SecretsInNS := &v1.SecretList{}
	err = rclient.List(ctx, SecretsInNS, &client.ListOptions{Namespace: cr.Namespace})
	if err != nil {
		return fmt.Errorf("cannot list secrets at vmagent namespace: %w", err)
	}

	basicAuthSecrets, err := loadBasicAuthSecrets(ctx, rclient, smons, nodes, pmons, statics, cr.Spec.APIServerConfig, nil, SecretsInNS)
	if err != nil {
		return fmt.Errorf("cannot load basic secrets for ServiceMonitors: %w", err)
	}

	bearerTokens, err := loadBearerTokensFromSecrets(ctx, rclient, smons, nodes, pmons, statics, nil, SecretsInNS)
	if err != nil {
		return fmt.Errorf("cannot load bearer tokens from secrets for ServiceMonitors: %w", err)
	}

	additionalScrapeConfigs, err := loadAdditionalScrapeConfigsSecret(cr.Spec.AdditionalScrapeConfigs, SecretsInNS)
	if err != nil {
		return fmt.Errorf("loading additional scrape configs from Secret failed: %w", err)
	}

	// Update secret based on the most recent configuration.
	generatedConfig, err := generateConfig(
		cr,
		smons,
		pmons,
		probes,
		nodes,
		statics,
		basicAuthSecrets,
		bearerTokens,
		additionalScrapeConfigs,
	)
	if err != nil {
		return fmt.Errorf("generating config for vmagent failed: %w", err)
	}

	s := makeConfigSecret(cr, c)
	s.ObjectMeta.Annotations = map[string]string{
		"generated": "true",
	}

	// Compress config to avoid 1mb secret limit for a while
	var buf bytes.Buffer
	if err = gzipConfig(&buf, generatedConfig); err != nil {
		return fmt.Errorf("cannot gzip config for vmagent: %w", err)
	}
	s.Data[configFilename] = buf.Bytes()

	curSecret := &v1.Secret{}
	err = rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: s.Name}, curSecret)
	if errors.IsNotFound(err) {
		log.Info("creating new configuration secret for vmagent")
		return rclient.Create(ctx, s)
	}

	var (
		generatedConf             = s.Data[configFilename]
		curConfig, curConfigFound = curSecret.Data[configFilename]
	)
	if curConfigFound {
		if bytes.Equal(curConfig, generatedConf) {
			log.Info("updating VMAgent configuration secret skipped, no configuration change")
			return nil
		}
		log.Info("current VMAgent configuration has changed")
	} else {
		log.Info("no current VMAgent configuration secret found", "currentConfigFound", curConfigFound)
	}

	log.Info("updating VMAgent configuration secret")
	return rclient.Update(ctx, s)
}

func SelectServiceScrapes(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client) (map[string]*victoriametricsv1beta1.VMServiceScrape, error) {

	res := make(map[string]*victoriametricsv1beta1.VMServiceScrape)

	var servScrapesCombined []victoriametricsv1beta1.VMServiceScrape

	namespaces, objSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.ServiceScrapeNamespaceSelector, cr.Spec.ServiceScrapeSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := selectWithMerge(ctx, rclient, namespaces, &victoriametricsv1beta1.VMServiceScrapeList{}, objSelector, func(list client.ObjectList) {
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

	if err := selectWithMerge(ctx, rclient, namespaces, &victoriametricsv1beta1.VMPodScrapeList{}, objSelector, func(list client.ObjectList) {
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

	if err := selectWithMerge(ctx, rclient, namespaces, &victoriametricsv1beta1.VMProbeList{}, objSelector, func(list client.ObjectList) {
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

	log.Info("filtering namespaces to select vmProbes from",
		"namespace", cr.Namespace, "vmagent", cr.Name)
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
	res := make(map[string]*victoriametricsv1beta1.VMNodeScrape)

	var nodesCombined []victoriametricsv1beta1.VMNodeScrape

	namespaces, objSelector, err := getNSWithSelector(ctx, rclient, cr.Spec.NodeScrapeNamespaceSelector, cr.Spec.NodeScrapeSelector, cr.Namespace)
	if err != nil {
		return nil, err
	}

	if err := selectWithMerge(ctx, rclient, namespaces, &victoriametricsv1beta1.VMNodeScrapeList{}, objSelector, func(list client.ObjectList) {
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

	if err := selectWithMerge(ctx, rclient, namespaces, &victoriametricsv1beta1.VMStaticScrapeList{}, objSelector, func(list client.ObjectList) {
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

func loadBasicAuthSecrets(
	ctx context.Context,
	rclient client.Client,
	mons map[string]*victoriametricsv1beta1.VMServiceScrape,
	nodes map[string]*victoriametricsv1beta1.VMNodeScrape,
	pods map[string]*victoriametricsv1beta1.VMPodScrape,
	statics map[string]*victoriametricsv1beta1.VMStaticScrape,
	apiserverConfig *victoriametricsv1beta1.APIServerConfig,
	remoteWriteSpecs []victoriametricsv1beta1.VMAgentRemoteWriteSpec,
	secretsAtNS *v1.SecretList,
) (map[string]BasicAuthCredentials, error) {

	secrets := map[string]BasicAuthCredentials{}
	nsSecretCache := make(map[string]*v1.Secret)
	for _, mon := range mons {
		for i, ep := range mon.Spec.Endpoints {
			if ep.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, ep.BasicAuth, mon.Namespace, nsSecretCache)
				if err != nil {
					return nil, fmt.Errorf("could not generate basicAuth for vmservicescrape %s. %w", mon.Name, err)
				}
				secrets[fmt.Sprintf("serviceScrape/%s/%s/%d", mon.Namespace, mon.Name, i)] = credentials
			}

		}
	}

	for _, node := range nodes {
		if node.Spec.BasicAuth != nil {
			credentials, err := loadBasicAuthSecretFromAPI(ctx,
				rclient,
				node.Spec.BasicAuth,
				node.Namespace,
				nsSecretCache)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for vmNodeScrape %s. %w", node.Name, err)
			}
			secrets[node.AsMapKey()] = credentials
		}

	}
	for _, pod := range pods {
		for i, ep := range pod.Spec.PodMetricsEndpoints {
			if ep.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, ep.BasicAuth, pod.Namespace, nsSecretCache)
				if err != nil {
					return nil, fmt.Errorf("could not generate basicAuth for vmpodscrapee %s. %w", pod.Name, err)
				}
				secrets[fmt.Sprintf("podScrape/%s/%s/%d", pod.Namespace, pod.Name, i)] = credentials
			}

		}
	}

	for _, staticCfg := range statics {
		for i, ep := range staticCfg.Spec.TargetEndpoints {
			if ep.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ctx, rclient, ep.BasicAuth, staticCfg.Namespace, nsSecretCache)
				if err != nil {
					return nil, fmt.Errorf("could not generate basicAuth for vmstaticScrape %s. %w", staticCfg.Name, err)
				}
				secrets[staticCfg.AsKey(i)] = credentials
			}

		}
	}

	// load apiserver basic auth secret
	if apiserverConfig != nil && apiserverConfig.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(apiserverConfig.BasicAuth, secretsAtNS)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for apiserver config. %w", err)
		}
		secrets["apiserver"] = credentials
	}

	// load basic auth for remote write configuration
	for _, rws := range remoteWriteSpecs {
		if rws.BasicAuth == nil {
			continue
		}
		credentials, err := loadBasicAuthSecret(rws.BasicAuth, secretsAtNS)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for remote write spec %s config. %w", rws.URL, err)
		}
		secrets[fmt.Sprintf("remoteWriteSpec/%s", rws.URL)] = credentials
	}

	return secrets, nil
}

func loadBearerTokensFromSecrets(
	ctx context.Context,
	rclient client.Client,
	mons map[string]*victoriametricsv1beta1.VMServiceScrape,
	nodes map[string]*victoriametricsv1beta1.VMNodeScrape,
	pods map[string]*victoriametricsv1beta1.VMPodScrape,
	statics map[string]*victoriametricsv1beta1.VMStaticScrape,
	remoteWriteSpecs []victoriametricsv1beta1.VMAgentRemoteWriteSpec,
	secretsAtNS *v1.SecretList,
) (map[string]BearerToken, error) {
	tokens := map[string]BearerToken{}
	nsSecretCache := make(map[string]*v1.Secret)

	for _, mon := range mons {
		for i, ep := range mon.Spec.Endpoints {
			if ep.BearerTokenSecret.Name == "" {
				continue
			}

			token, err := getCredFromSecret(
				ctx,
				rclient,
				mon.Namespace,
				ep.BearerTokenSecret,
				mon.Namespace+"/"+ep.BearerTokenSecret.Name,
				nsSecretCache,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract endpoint bearertoken for vmservicescrape %v from secret %v in namespace %v",
					mon.Name, ep.BearerTokenSecret.Name, mon.Namespace,
				)
			}

			tokens[fmt.Sprintf("serviceScrape/%s/%s/%d", mon.Namespace, mon.Name, i)] = BearerToken(token)
		}
	}
	for _, pod := range pods {
		for i, ep := range pod.Spec.PodMetricsEndpoints {
			if ep.BearerTokenSecret.Name == "" {
				continue
			}

			token, err := getCredFromSecret(
				ctx,
				rclient,
				pod.Namespace,
				ep.BearerTokenSecret,
				pod.Namespace+"/"+ep.BearerTokenSecret.Name,
				nsSecretCache,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract endpoint bearertoken for vmpodscrape %v from secret %v in namespace %v",
					pod.Name, ep.BearerTokenSecret.Name, pod.Namespace,
				)
			}

			tokens[fmt.Sprintf("podScrape/%s/%s/%d", pod.Namespace, pod.Name, i)] = BearerToken(token)

		}
	}
	// load bearer tokens for nodeScrape
	for _, node := range nodes {
		if node.Spec.BearerTokenSecret.Name == "" {
			continue
		}
		token, err := getCredFromSecret(ctx,
			rclient,
			node.Namespace,
			node.Spec.BearerTokenSecret,
			node.Namespace+"/"+node.Spec.BearerTokenSecret.Name,
			nsSecretCache)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to extract endpoint bearertoken for VMNodeScrape %v from secret %v in namespace %v",
				node.Name, node.Spec.BearerTokenSecret.Name, node.Namespace,
			)
		}
		tokens[node.AsMapKey()] = BearerToken(token)

	}

	for _, staticCfg := range statics {
		for i, ep := range staticCfg.Spec.TargetEndpoints {
			if ep.BearerTokenSecret.Name == "" {
				continue
			}

			token, err := getCredFromSecret(
				ctx,
				rclient,
				staticCfg.Namespace,
				ep.BearerTokenSecret,
				staticCfg.Namespace+"/"+ep.BearerTokenSecret.Name,
				nsSecretCache,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract targetEndpoint bearertoken for vmstaticScrape %v from secret %v in namespace %v",
					staticCfg.Name, ep.BearerTokenSecret, staticCfg.Namespace,
				)
			}

			tokens[staticCfg.AsKey(i)] = BearerToken(token)

		}
	}
	// load basic auth for remote write configuration
	for _, rws := range remoteWriteSpecs {
		if rws.BearerTokenSecret == nil {
			continue
		}
		for _, secret := range secretsAtNS.Items {
			if secret.Name != rws.BearerTokenSecret.Name {
				continue
			}
			token, err := extractCredKey(&secret, *rws.BearerTokenSecret)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract bearertoken for remoteWriteSpec %s from secret %s. %w ",
					rws.URL, rws.BearerTokenSecret.Name, err,
				)
			}
			tokens[fmt.Sprintf("remoteWriteSpec/%s", rws.URL)] = BearerToken(token)
		}
	}

	return tokens, nil
}

func loadBasicAuthSecret(basicAuth *victoriametricsv1beta1.BasicAuth, s *v1.SecretList) (BasicAuthCredentials, error) {
	var username string
	var password string
	var err error

	for _, secret := range s.Items {

		if secret.Name == basicAuth.Username.Name {
			if username, err = extractCredKey(&secret, basicAuth.Username); err != nil {
				return BasicAuthCredentials{}, err
			}
		}

		if secret.Name == basicAuth.Password.Name {
			if password, err = extractCredKey(&secret, basicAuth.Password); err != nil {
				return BasicAuthCredentials{}, err
			}

		}
		if username != "" && password != "" {
			break
		}
	}

	if username == "" && password == "" {
		return BasicAuthCredentials{}, fmt.Errorf("basic auth username and password secret not found")
	}

	return BasicAuthCredentials{username: username, password: password}, nil

}

func extractCredKey(secret *v1.Secret, sel v1.SecretKeySelector) (string, error) {
	if s, ok := secret.Data[sel.Key]; ok {
		return string(s), nil
	}
	return "", fmt.Errorf("secret key %q in secret %q not found", sel.Key, sel.Name)
}

func getCredFromSecret(
	ctx context.Context,
	rclient client.Client,
	ns string,
	sel v1.SecretKeySelector,
	cacheKey string,
	cache map[string]*v1.Secret,
) (string, error) {
	var s *v1.Secret
	var ok bool

	if s, ok = cache[cacheKey]; !ok {
		s = &v1.Secret{}
		if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: sel.Name}, s); err != nil {
			return "", fmt.Errorf("unable to fetch key from secret%s: %w", sel.Name, err)
		}
		cache[cacheKey] = s
	}
	return extractCredKey(s, sel)
}

func getCredFromConfigMap(
	ctx context.Context,
	rclient client.Client,
	ns string,
	sel v1.ConfigMapKeySelector,
	cacheKey string,
	cache map[string]*v1.ConfigMap,
) (string, error) {
	var s *v1.ConfigMap
	var ok bool

	if s, ok = cache[cacheKey]; !ok {
		s = &v1.ConfigMap{}
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

func loadBasicAuthSecretFromAPI(ctx context.Context, rclient client.Client, basicAuth *victoriametricsv1beta1.BasicAuth, ns string, cache map[string]*v1.Secret) (BasicAuthCredentials, error) {
	var username string
	var password string
	var err error

	if username, err = getCredFromSecret(ctx, rclient, ns, basicAuth.Username, ns+"/"+basicAuth.Username.Name, cache); err != nil {
		return BasicAuthCredentials{}, err
	}

	if password, err = getCredFromSecret(ctx, rclient, ns, basicAuth.Password, ns+"/"+basicAuth.Password.Name, cache); err != nil {
		return BasicAuthCredentials{}, err
	}

	return BasicAuthCredentials{username: username, password: password}, nil
}

func loadAdditionalScrapeConfigsSecret(additionalScrapeConfigs *v1.SecretKeySelector, s *v1.SecretList) ([]byte, error) {
	if additionalScrapeConfigs != nil {
		for _, secret := range s.Items {
			if secret.Name == additionalScrapeConfigs.Name {
				if c, ok := secret.Data[additionalScrapeConfigs.Key]; ok {
					return c, nil
				}

				return nil, fmt.Errorf("key %v could not be found in Secret %v", additionalScrapeConfigs.Key, additionalScrapeConfigs.Name)
			}
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

func CreateVMServiceScrapeFromService(ctx context.Context, rclient client.Client, service *v1.Service, metricPath string, filterPortNames ...string) error {
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

	scrapeSvc := victoriametricsv1beta1.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:            service.Name,
			Namespace:       service.Namespace,
			OwnerReferences: service.OwnerReferences,
			Labels:          service.Labels,
			Annotations:     service.Annotations,
		},
		Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
			Selector:  metav1.LabelSelector{MatchLabels: service.Spec.Selector},
			Endpoints: endPoints,
		},
	}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, &existVSS)
	if err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, &scrapeSvc)
		}
		return err
	}
	existVSS.Spec = scrapeSvc.Spec
	existVSS.Labels = scrapeSvc.Labels
	existVSS.Annotations = labels.Merge(scrapeSvc.Annotations, existVSS.Annotations)
	if !equality.Semantic.DeepDerivative(scrapeSvc.Spec, existVSS.Spec) || !equality.Semantic.DeepDerivative(scrapeSvc.Labels, existVSS.Labels) || !equality.Semantic.DeepDerivative(scrapeSvc.Annotations, existVSS.Annotations) {
		return rclient.Update(ctx, &existVSS)
	}
	return nil
}
