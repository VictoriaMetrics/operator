package factory

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

var invalidDNS1123Characters = regexp.MustCompile("[^-a-z0-9]+")

func CreateOrUpdateConfigurationSecret(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client, c *config.BaseOperatorConf) error {
	// If no service or pod scrape selectors are configured, the user wants to
	// manage configuration themselves. Do create an empty Secret if it doesn't
	// exist.
	l := log.WithValues("vmagent", cr.Name, "namespace", cr.Namespace)

	if cr.Spec.ServiceScrapeSelector == nil && cr.Spec.PodScrapeSelector == nil && cr.Spec.VMProbeSelector == nil {
		l.Info("neither ServiceScrape nor PodScrape nor VMProbe selector specified, leaving configuration unmanaged")

		s, err := makeEmptyConfigurationSecret(cr, c)
		if err != nil {
			return fmt.Errorf("generating empty config secret failed: %w", err)
		}
		err = rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: s.Name}, &v1.Secret{})
		if errors.IsNotFound(err) {
			if err := rclient.Create(ctx, s); err != nil && !errors.IsAlreadyExists(err) {
				return fmt.Errorf("creating empty config file failed: %w", err)
			}
		}
		if !errors.IsNotFound(err) && err != nil {
			return err
		}

		return nil
	}

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

	SecretsInNS := &v1.SecretList{}
	err = rclient.List(ctx, SecretsInNS)
	if err != nil {
		return fmt.Errorf("cannot list secrets at vmagent namespace: %w", err)
	}

	basicAuthSecrets, err := loadBasicAuthSecrets(ctx, rclient, smons, cr.Spec.APIServerConfig, nil, SecretsInNS)
	if err != nil {
		return fmt.Errorf("cannot load basic secrets for ServiceMonitors: %w", err)
	}

	bearerTokens, err := loadBearerTokensFromSecrets(ctx, rclient, smons, nil, SecretsInNS)
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

	namespaces := []string{}

	//list namespaces matched by  namespaceselector
	//for each namespace apply list with  selector
	//combine result
	if cr.Spec.ServiceScrapeNamespaceSelector == nil {
		namespaces = append(namespaces, cr.Namespace)
	} else if cr.Spec.ServiceScrapeNamespaceSelector.MatchExpressions == nil && cr.Spec.ServiceScrapeNamespaceSelector.MatchLabels == nil {
		namespaces = nil
	} else {
		log.Info("namspace selector for serviceScrapes", "selector", cr.Spec.ServiceScrapeNamespaceSelector.String())
		nsSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.ServiceScrapeNamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert rulenamespace selector: %w", err)
		}
		namespaces, err = selectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot select namespaces for rule match: %w", err)
		}
	}

	// if namespaces isn't nil, then nameSpaceSelector is defined
	// but scrapeSelector maybe be nil and we must set it to catch all value
	if namespaces != nil && cr.Spec.ServiceScrapeSelector == nil {
		cr.Spec.ServiceScrapeSelector = &metav1.LabelSelector{}
	}
	servMonSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.ServiceScrapeSelector)
	if err != nil {
		return nil, fmt.Errorf("cannot convert ServiceScrapeSelector to labelSelector: %w", err)
	}

	servScrapesCombined := []victoriametricsv1beta1.VMServiceScrape{}

	//list all namespaces for rules with selector
	if namespaces == nil {
		log.Info("listing all namespaces for serviceScrapes", "vmagent", cr.Name)
		servMons := &victoriametricsv1beta1.VMServiceScrapeList{}
		err = rclient.List(ctx, servMons, &client.ListOptions{LabelSelector: servMonSelector})
		if err != nil {
			return nil, fmt.Errorf("cannot list rules from all namespaces: %w", err)
		}
		servScrapesCombined = append(servScrapesCombined, servMons.Items...)

	} else {
		for _, ns := range namespaces {
			log.Info("listing namespace for serviceScrapes", "ns", ns, "vmagent", cr.Name)
			listOpts := &client.ListOptions{Namespace: ns, LabelSelector: servMonSelector}
			servMons := &victoriametricsv1beta1.VMServiceScrapeList{}
			err = rclient.List(ctx, servMons, listOpts)
			if err != nil {
				return nil, fmt.Errorf("cannot list rules at namespace: %s, err: %w", ns, err)
			}
			servScrapesCombined = append(servScrapesCombined, servMons.Items...)

		}
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

	namespaces := []string{}

	// list namespaces matched by  namespaceSelector
	// for each namespace apply list with  selector
	// combine result

	if cr.Spec.PodScrapeNamespaceSelector == nil {
		namespaces = append(namespaces, cr.Namespace)
	} else if cr.Spec.PodScrapeNamespaceSelector.MatchExpressions == nil && cr.Spec.PodScrapeNamespaceSelector.MatchLabels == nil {
		namespaces = nil
	} else {
		log.Info("selector for podScrape", "vmagent", cr.Name, "selector", cr.Spec.PodScrapeNamespaceSelector.String())
		nsSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.PodScrapeNamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert podScrapeNamespaceSelector to labelSelector: %w", err)
		}
		namespaces, err = selectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot select namespaces for podScrape match: %w", err)
		}
	}

	// if namespaces isn't nil, then nameSpaceSelector is defined
	//but scrapeSelector maybe be nil and we have to set it to catch all value
	if namespaces != nil && cr.Spec.PodScrapeSelector == nil {
		cr.Spec.PodScrapeSelector = &metav1.LabelSelector{}
	}
	podScrapeSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.PodScrapeSelector)
	if err != nil {
		return nil, fmt.Errorf("cannot convert podScrapeSelector to label selector: %w", err)
	}

	podScrapesCombined := []victoriametricsv1beta1.VMPodScrape{}

	//list all namespaces for rules with selector
	if namespaces == nil {
		log.Info("listing all namespaces for podScrapes")
		servMons := &victoriametricsv1beta1.VMPodScrapeList{}
		err = rclient.List(ctx, servMons, &client.ListOptions{LabelSelector: podScrapeSelector})
		if err != nil {
			return nil, fmt.Errorf("cannot list podScrapes from all namespaces: %w", err)
		}
		podScrapesCombined = append(podScrapesCombined, servMons.Items...)

	} else {
		for _, ns := range namespaces {
			listOpts := &client.ListOptions{Namespace: ns, LabelSelector: podScrapeSelector}
			servMons := &victoriametricsv1beta1.VMPodScrapeList{}
			err = rclient.List(ctx, servMons, listOpts)
			if err != nil {
				return nil, fmt.Errorf("cannot list podscrapes at namespace: %s, err: %w", ns, err)
			}
			podScrapesCombined = append(podScrapesCombined, servMons.Items...)

		}
	}

	log.Info("filtering namespaces to select PodScrapes from",
		"namespace", cr.Namespace, "vmagent", cr.Name)
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

	namespaces := []string{}

	// list namespaces matched by  namespaceSelector
	// for each namespace apply list with  selector
	// combine result
	if cr.Spec.VMProbeNamespaceSelector == nil {
		namespaces = append(namespaces, cr.Namespace)
	} else if cr.Spec.VMProbeNamespaceSelector.MatchExpressions == nil && cr.Spec.PodScrapeNamespaceSelector.MatchLabels == nil {
		namespaces = nil
	} else {
		log.Info("selector for VMProbe", "vmagent", cr.Name, "selector", cr.Spec.PodScrapeNamespaceSelector.String())
		nsSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.VMProbeNamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert VMProbeNamespaceSelector to labelSelector: %w", err)
		}
		namespaces, err = selectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot select namespaces for VMprobe match: %w", err)
		}
	}

	// if namespaces isn't nil, then nameSpaceSelector is defined
	//but probeSelector maybe be nil and we have to set it to catch all value
	if namespaces != nil && cr.Spec.VMProbeSelector == nil {
		cr.Spec.VMProbeSelector = &metav1.LabelSelector{}
	}
	probeSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.VMProbeSelector)
	if err != nil {
		return nil, fmt.Errorf("cannot convert probeSelector to label selector: %w", err)
	}

	probesCombined := []victoriametricsv1beta1.VMProbe{}

	//list all namespaces for probes with selector
	if namespaces == nil {
		log.Info("listing all namespaces for probes")
		vmProbes := &victoriametricsv1beta1.VMProbeList{}
		err = rclient.List(ctx, vmProbes, &client.ListOptions{LabelSelector: probeSelector})
		if err != nil {
			return nil, fmt.Errorf("cannot list VMProbes from all namespaces: %w", err)
		}
		probesCombined = append(probesCombined, vmProbes.Items...)

	} else {
		for _, ns := range namespaces {
			listOpts := &client.ListOptions{Namespace: ns, LabelSelector: probeSelector}
			vmProbes := &victoriametricsv1beta1.VMProbeList{}
			err = rclient.List(ctx, vmProbes, listOpts)
			if err != nil {
				return nil, fmt.Errorf("cannot list podscrapes at namespace: %s, err: %w", ns, err)
			}
			probesCombined = append(probesCombined, vmProbes.Items...)

		}
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

	log.Info("selected PodScrapes", "podscrapes", strings.Join(probesList, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func loadBasicAuthSecrets(
	ctx context.Context,
	rclient client.Client,
	mons map[string]*victoriametricsv1beta1.VMServiceScrape,
	apiserverConfig *victoriametricsv1beta1.APIServerConfig,
	remoteWriteSpecs []victoriametricsv1beta1.VMAgentRemoteWriteSpec,
	SecretsInPromNS *v1.SecretList,
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

	// load apiserver basic auth secret
	if apiserverConfig != nil && apiserverConfig.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(apiserverConfig.BasicAuth, SecretsInPromNS)
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
		credentials, err := loadBasicAuthSecret(rws.BasicAuth, SecretsInPromNS)
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
	remoteWriteSpecs []victoriametricsv1beta1.VMAgentRemoteWriteSpec,
	SecretsInPromNS *v1.SecretList,
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

	// load basic auth for remote write configuration
	for _, rws := range remoteWriteSpecs {
		if rws.BearerTokenSecret == nil {
			continue
		}
		for _, secret := range SecretsInPromNS.Items {
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

func CreateVMServiceScrapeFromService(ctx context.Context, rclient client.Client, service *v1.Service, filterPortNames ...string) error {
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
		})
	}
	scrapeSvc := &victoriametricsv1beta1.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:            service.Name,
			Namespace:       service.Namespace,
			OwnerReferences: service.OwnerReferences,
		},
		Spec: victoriametricsv1beta1.VMServiceScrapeSpec{
			Selector:  metav1.LabelSelector{MatchLabels: service.Spec.Selector},
			Endpoints: endPoints,
		},
	}
	err := rclient.Create(ctx, scrapeSvc)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}
