package factory

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"github.com/VictoriaMetrics/operator/conf"
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	monitoringv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1beta1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

func  CreateOrUpdateConfigurationSecret(p *monitoringv1beta1.VmAgent,rclient client.Client,kclient kubernetes.Interface, c *conf.BaseOperatorConf, l logr.Logger  ) error {
	// If no service or pod monitor selectors are configured, the user wants to
	// manage configuration themselves. Do create an empty Secret if it doesn't
	// exist.

	if p.Spec.ServiceMonitorSelector == nil && p.Spec.PodMonitorSelector == nil {
		l.Info("neither ServiceMonitor not PodMonitor selector specified, leaving configuration unmanaged", "prometheus", p.Name, "namespace", p.Namespace)

		s, err := makeEmptyConfigurationSecret(p, c)
		if err != nil {
			return errors.Wrap(err, "generating empty config secret failed")
		}
		sClient := kclient.CoreV1().Secrets(p.Namespace)
		_, err = sClient.Get(s.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if _, err := kclient.CoreV1().Secrets(p.Namespace).Create(s); err != nil && !apierrors.IsAlreadyExists(err) {
				return errors.Wrap(err, "creating empty config file failed")
			}
		}
		if !apierrors.IsNotFound(err) && err != nil {
			return err
		}

		return nil
	}

	smons, err := SelectServiceMonitors(p,rclient,l)
	if err != nil {
		return errors.Wrap(err, "selecting ServiceMonitors failed")
	}

	pmons, err := SelectPodMonitors(p,rclient,l)
	if err != nil {
		return errors.Wrap(err, "selecting PodMonitors failed")
	}

	sClient := kclient.CoreV1().Secrets(p.Namespace)
	SecretsInPromNS, err := sClient.List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	basicAuthSecrets, err := loadBasicAuthSecrets(smons, p.Spec.RemoteWrite, p.Spec.APIServerConfig, SecretsInPromNS,kclient)
	if err != nil {
		return err
	}

	bearerTokens, err := loadBearerTokensFromSecrets(smons,kclient)
	if err != nil {
		return err
	}

	additionalScrapeConfigs, err := loadAdditionalScrapeConfigsSecret(p.Spec.AdditionalScrapeConfigs, SecretsInPromNS)
	if err != nil {
		return errors.Wrap(err, "loading additional scrape configs from Secret failed")
	}

	// Update secret based on the most recent configuration.
	conf, err := generateConfig(
		p,
		smons,
		pmons,
		basicAuthSecrets,
		bearerTokens,
		additionalScrapeConfigs,
	)
	if err != nil {
		return errors.Wrap(err, "generating config failed")
	}

	s := makeConfigSecret(p, c)
	s.ObjectMeta.Annotations = map[string]string{
		"generated": "true",
	}

	// Compress config to avoid 1mb secret limit for a while
	var buf bytes.Buffer
	if err = gzipConfig(&buf, conf); err != nil {
		return errors.Wrap(err, "couldnt gzip config")
	}
	s.Data[configFilename] = buf.Bytes()

	curSecret, err := sClient.Get(s.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		l.Info("creating configuration")
		_, err = sClient.Create(s)
		return err
	}

	var (
		generatedConf             = s.Data[configFilename]
		curConfig, curConfigFound = curSecret.Data[configFilename]
	)
	if curConfigFound {
		if bytes.Equal(curConfig, generatedConf) {
			l.Info("updating Prometheus configuration secret skipped, no configuration change")
			return nil
		}
		l.Info("current Prometheus configuration has changed")
	} else {
		l.Info("no current Prometheus configuration secret found", "currentConfigFound", curConfigFound)
	}

	l.Info("updating Prometheus configuration secret")
	_, err = sClient.Update(s)
	return err
}


func  SelectServiceMonitors(p *monitoringv1beta1.VmAgent,rclient client.Client, l logr.Logger) (map[string]*monitoringv1.ServiceMonitor, error) {

	// Selectors (<namespace>/<name>) might overlap. Deduplicate them along the keyFunc.
	res := make(map[string]*monitoringv1.ServiceMonitor)

	// TODO logic is broken
	// currently it`s not possible to use both servicemon and namespace selector
	listOpts := &client.ListOptions{}

	servMonSelector, err := metav1.LabelSelectorAsSelector(p.Spec.ServiceMonitorSelector)
	if err != nil {
		return nil, err
	}

	// If 'ServiceMonitorNamespaceSelector' is nil only check own namespace.
	if p.Spec.ServiceMonitorNamespaceSelector == nil {
		listOpts.Namespace = p.Namespace
	} else {
		servMonSelector, err = metav1.LabelSelectorAsSelector(p.Spec.ServiceMonitorNamespaceSelector)
		if err != nil {
			return nil, err
		}
	}

	listOpts.LabelSelector = servMonSelector

	servMons := &monitoringv1.ServiceMonitorList{}

	err = rclient.List(context.TODO(), servMons,listOpts)
	if err != nil {
		return nil,err
	}

	for _, mon := range servMons.Items {
		m := mon.DeepCopy()
		res[mon.Namespace+"/"+mon.Name] = m
	}

	// If denied by Prometheus spec, filter out all service monitors that access
	// the file system.
	if p.Spec.ArbitraryFSAccessThroughSMs.Deny {
		for namespaceAndName, sm := range res {
			for _, endpoint := range sm.Spec.Endpoints {
				if err := testForArbitraryFSAccess(endpoint); err != nil {
					delete(res, namespaceAndName)
					l.Info("skipping servicemonitor",
						"error", err.Error(),
						"servicemonitor", namespaceAndName,
						"namespace", p.Namespace,
						"prometheus", p.Name,
					)
				}
			}
		}
	}

	serviceMonitors := []string{}
	for k := range res {
		serviceMonitors = append(serviceMonitors, k)
	}
	l.Info("selected ServiceMonitors", "servicemonitors", strings.Join(serviceMonitors, ","), "namespace", p.Namespace, "prometheus", p.Name)

	return res, nil
}

func SelectPodMonitors(p *monitoringv1beta1.VmAgent, rclient client.Client, l logr.Logger) (map[string]*monitoringv1.PodMonitor, error) {
	// Selectors might overlap. Deduplicate them along the keyFunc.
	res := make(map[string]*monitoringv1.PodMonitor)

	// TODO logic is broken
	// currently it`s not possible to use both servicemon and namespace selector
	listOpts := &client.ListOptions{}


	podMonSelector, err := metav1.LabelSelectorAsSelector(p.Spec.PodMonitorSelector)
	if err != nil {
		l.Error(err,"cannot find label for pod monitor select")
		return nil, err
	}

	// If 'PodMonitorNamespaceSelector' is nil only check own namespace.
	if p.Spec.PodMonitorNamespaceSelector == nil {
		listOpts.Namespace =  p.Namespace
	} else {
		podMonSelector, err = metav1.LabelSelectorAsSelector(p.Spec.PodMonitorNamespaceSelector)
		if err != nil {
			l.Error(err,"cannot find label for pod monitor namespace")

			return nil, err

		}

	}

	listOpts.LabelSelector = podMonSelector
	podMons := &monitoringv1.PodMonitorList{}

	err = rclient.List(context.TODO(), podMons, &client.ListOptions{LabelSelector: podMonSelector})
	if err != nil {
		l.Error(err, "cannot list pod monitors")
		return nil, err
	}

	l.Info("filtering namespaces to select PodMonitors from", "namespace", p.Namespace, "prometheus", p.Name)


	//TODO fix fsf
	for _, podMon := range podMons.Items {
		pm := podMon.DeepCopy()
			res[podMon.Namespace+"/"+podMon.Name] = pm
	}
	podMonitors := make([]string, 0)
	for key := range res {
		podMonitors = append(podMonitors, key)
	}

	l.Info("selected PodMonitors", "podmonitors", strings.Join(podMonitors, ","), "namespace", p.Namespace, "prometheus", p.Name)

	return res, nil
}

func  loadBasicAuthSecrets(
	mons map[string]*monitoringv1.ServiceMonitor,
	remoteWrites []monitoringv1.RemoteWriteSpec,
	apiserverConfig *monitoringv1.APIServerConfig,
	SecretsInPromNS *v1.SecretList,
	kclient kubernetes.Interface,
) (map[string]BasicAuthCredentials, error) {

	secrets := map[string]BasicAuthCredentials{}
	nsSecretCache := make(map[string]*v1.Secret)
	for _, mon := range mons {
		for i, ep := range mon.Spec.Endpoints {
			if ep.BasicAuth != nil {
				credentials, err := loadBasicAuthSecretFromAPI(ep.BasicAuth, kclient.CoreV1(), mon.Namespace, nsSecretCache)
				if err != nil {
					return nil, fmt.Errorf("could not generate basicAuth for servicemonitor %s. %s", mon.Name, err)
				}
				secrets[fmt.Sprintf("serviceMonitor/%s/%s/%d", mon.Namespace, mon.Name, i)] = credentials
			}

		}
	}

	for i, remote := range remoteWrites {
		if remote.BasicAuth != nil {
			credentials, err := loadBasicAuthSecret(remote.BasicAuth, SecretsInPromNS)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for remote_write config %d. %s", i, err)
			}
			secrets[fmt.Sprintf("remoteWrite/%d", i)] = credentials
		}
	}

	// load apiserver basic auth secret
	if apiserverConfig != nil && apiserverConfig.BasicAuth != nil {
		credentials, err := loadBasicAuthSecret(apiserverConfig.BasicAuth, SecretsInPromNS)
		if err != nil {
			return nil, fmt.Errorf("could not generate basicAuth for apiserver config. %s", err)
		}
		secrets["apiserver"] = credentials
	}

	return secrets, nil

}

func  loadBearerTokensFromSecrets(mons map[string]*monitoringv1.ServiceMonitor, kclient kubernetes.Interface ) (map[string]BearerToken, error) {
	tokens := map[string]BearerToken{}
	nsSecretCache := make(map[string]*v1.Secret)

	for _, mon := range mons {
		for i, ep := range mon.Spec.Endpoints {
			if ep.BearerTokenSecret.Name == "" {
				continue
			}

			sClient := kclient.CoreV1().Secrets(mon.Namespace)
			token, err := getCredFromSecret(
				sClient,
				ep.BearerTokenSecret,
				"bearertoken",
				mon.Namespace+"/"+ep.BearerTokenSecret.Name,
				nsSecretCache,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to extract endpoint bearertoken for servicemonitor %v from secret %v in namespace %v",
					mon.Name, ep.BearerTokenSecret.Name, mon.Namespace,
				)
			}

			tokens[fmt.Sprintf("serviceMonitor/%s/%s/%d", mon.Namespace, mon.Name, i)] = BearerToken(token)
		}
	}

	return tokens, nil
}

func loadBasicAuthSecret(basicAuth *monitoringv1.BasicAuth, s *v1.SecretList) (BasicAuthCredentials, error) {
	var username string
	var password string
	var err error

	for _, secret := range s.Items {

		if secret.Name == basicAuth.Username.Name {
			if username, err = extractCredKey(&secret, basicAuth.Username, "username"); err != nil {
				return BasicAuthCredentials{}, err
			}
		}

		if secret.Name == basicAuth.Password.Name {
			if password, err = extractCredKey(&secret, basicAuth.Password, "password"); err != nil {
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

func extractCredKey(secret *v1.Secret, sel v1.SecretKeySelector, cred string) (string, error) {
	if s, ok := secret.Data[sel.Key]; ok {
		return string(s), nil
	}
	return "", fmt.Errorf("secret %s key %q in secret %q not found", cred, sel.Key, sel.Name)
}

func getCredFromSecret(c corev1client.SecretInterface, sel v1.SecretKeySelector, cred string, cacheKey string, cache map[string]*v1.Secret) (_ string, err error) {
	var s *v1.Secret
	var ok bool

	if s, ok = cache[cacheKey]; !ok {
		if s, err = c.Get(sel.Name, metav1.GetOptions{}); err != nil {
			return "", fmt.Errorf("unable to fetch %s secret %q: %s", cred, sel.Name, err)
		}
		cache[cacheKey] = s
	}
	return extractCredKey(s, sel, cred)
}


func loadBasicAuthSecretFromAPI(basicAuth *monitoringv1.BasicAuth, c corev1client.CoreV1Interface, ns string, cache map[string]*v1.Secret) (BasicAuthCredentials, error) {
	var username string
	var password string
	var err error

	sClient := c.Secrets(ns)

	if username, err = getCredFromSecret(sClient, basicAuth.Username, "username", ns+"/"+basicAuth.Username.Name, cache); err != nil {
		return BasicAuthCredentials{}, err
	}

	if password, err = getCredFromSecret(sClient, basicAuth.Password, "password", ns+"/"+basicAuth.Password.Name, cache); err != nil {
		return BasicAuthCredentials{}, err
	}

	return BasicAuthCredentials{username: username, password: password}, nil
}

func  loadAdditionalScrapeConfigsSecret(additionalScrapeConfigs *v1.SecretKeySelector, s *v1.SecretList) ([]byte, error) {
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
//		l.Info(fmt.Sprintf("secret %v could not be found", additionalScrapeConfigs.Name))
	}
	return nil, nil
}

func testForArbitraryFSAccess(e monitoringv1.Endpoint) error {
	if e.BearerTokenFile != "" {
		return errors.New("it accesses file system via bearer token file which Prometheus specification prohibits")
	}

	tlsConf := e.TLSConfig
	if tlsConf == nil {
		return nil
	}

	if err := e.TLSConfig.Validate(); err != nil {
		return err
	}

	if tlsConf.CAFile != "" || tlsConf.CertFile != "" || tlsConf.KeyFile != "" {
		return errors.New("it accesses file system via tls config which Prometheus specification prohibits")
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

