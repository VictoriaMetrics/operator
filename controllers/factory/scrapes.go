package factory

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"

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
	err = rclient.List(ctx, SecretsInNS)
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

	namespaces := []string{}

	//list namespaces matched by  namespaceselector
	//for each namespace apply list with  selector
	//combine result
	if cr.Spec.ServiceScrapeNamespaceSelector == nil {
		namespaces = append(namespaces, cr.Namespace)
	} else if cr.Spec.ServiceScrapeNamespaceSelector.MatchExpressions == nil && cr.Spec.ServiceScrapeNamespaceSelector.MatchLabels == nil {
		namespaces = nil
	} else {
		log.Info("namespace selector for serviceScrapes", "selector", cr.Spec.ServiceScrapeNamespaceSelector.String())
		nsSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.ServiceScrapeNamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert serviceNamespace selector: %w", err)
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

	//list all namespaces for pods with selector
	if namespaces == nil {
		log.Info("listing all namespaces for podScrapes")
		podScrapes := &victoriametricsv1beta1.VMPodScrapeList{}
		err = rclient.List(ctx, podScrapes, &client.ListOptions{LabelSelector: podScrapeSelector})
		if err != nil {
			return nil, fmt.Errorf("cannot list podScrapes from all namespaces: %w", err)
		}
		podScrapesCombined = append(podScrapesCombined, podScrapes.Items...)

	} else {
		for _, ns := range namespaces {
			listOpts := &client.ListOptions{Namespace: ns, LabelSelector: podScrapeSelector}
			podScrapes := &victoriametricsv1beta1.VMPodScrapeList{}
			err = rclient.List(ctx, podScrapes, listOpts)
			if err != nil {
				return nil, fmt.Errorf("cannot list podscrapes at namespace: %s, err: %w", ns, err)
			}
			podScrapesCombined = append(podScrapesCombined, podScrapes.Items...)

		}
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

	namespaces := []string{}

	// list namespaces matched by  namespaceSelector
	// for each namespace apply list with  selector
	// combine result
	if cr.Spec.ProbeNamespaceSelector == nil {
		namespaces = append(namespaces, cr.Namespace)
	} else if cr.Spec.ProbeNamespaceSelector.MatchExpressions == nil && cr.Spec.PodScrapeNamespaceSelector.MatchLabels == nil {
		namespaces = nil
	} else {
		log.Info("selector for VMProbe", "vmagent", cr.Name, "selector", cr.Spec.PodScrapeNamespaceSelector.String())
		nsSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.ProbeNamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert ProbeNamespaceSelector to labelSelector: %w", err)
		}
		namespaces, err = selectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot select namespaces for VMprobe match: %w", err)
		}
	}

	// if namespaces isn't nil, then nameSpaceSelector is defined
	//but probeSelector maybe be nil and we have to set it to catch all value
	if namespaces != nil && cr.Spec.ProbeSelector == nil {
		cr.Spec.ProbeSelector = &metav1.LabelSelector{}
	}
	probeSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.ProbeSelector)
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

	log.Info("selected VMProbes", "vmProbes", strings.Join(probesList, ","), "namespace", cr.Namespace, "vmagent", cr.Name)

	return res, nil
}

func SelectVMNodeScrapes(ctx context.Context, cr *victoriametricsv1beta1.VMAgent, rclient client.Client) (map[string]*victoriametricsv1beta1.VMNodeScrape, error) {

	res := make(map[string]*victoriametricsv1beta1.VMNodeScrape)
	namespaces := []string{}
	l := log.WithValues("vmagent", cr.Name, "namespace", cr.Namespace)

	// list namespaces matched by  namespaceSelector
	// for each namespace apply list with  selector
	// combine result
	if cr.Spec.NodeScrapeNamespaceSelector == nil {
		namespaces = append(namespaces, cr.Namespace)
	} else if cr.Spec.NodeScrapeNamespaceSelector.MatchExpressions == nil && cr.Spec.NodeScrapeNamespaceSelector.MatchLabels == nil {
		namespaces = nil
	} else {
		l.Info("namespace selector for VMNodeScrape", "selector", cr.Spec.NodeScrapeNamespaceSelector.String())
		nsSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.NodeScrapeNamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert NodeScrapeNamespaceSelector to labelSelector: %w", err)
		}
		namespaces, err = selectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot select namespaces for VMNodeScrape match: %w", err)
		}
	}

	// if namespaces isn't nil, then nameSpaceSelector is defined
	// but nodeSelector maybe be nil and we have to set it to catch all value
	if namespaces != nil && cr.Spec.NodeScrapeSelector == nil {
		cr.Spec.NodeScrapeSelector = &metav1.LabelSelector{}
	}
	nodeSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.NodeScrapeSelector)
	if err != nil {
		return nil, fmt.Errorf("cannot convert nodeScrapeSelector to label selector: %w", err)
	}

	var nodesCombined []victoriametricsv1beta1.VMNodeScrape

	//list all namespaces for nodes with selector
	if namespaces == nil {
		l.Info("listing all namespaces for VMNodeScrapes")
		nodeScrapes := &victoriametricsv1beta1.VMNodeScrapeList{}
		err = rclient.List(ctx, nodeScrapes, &client.ListOptions{LabelSelector: nodeSelector})
		if err != nil {
			return nil, fmt.Errorf("cannot list VMNodeScrapes at all namespaces: %w", err)
		}
		nodesCombined = append(nodesCombined, nodeScrapes.Items...)

	} else {
		for _, ns := range namespaces {
			listOpts := &client.ListOptions{Namespace: ns, LabelSelector: nodeSelector}
			nodeScrapes := &victoriametricsv1beta1.VMNodeScrapeList{}
			err = rclient.List(ctx, nodeScrapes, listOpts)
			if err != nil {
				return nil, fmt.Errorf("cannot list VMNodeScrapes at namespace: %s, err: %w", ns, err)
			}
			nodesCombined = append(nodesCombined, nodeScrapes.Items...)

		}
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

	namespaces := []string{}

	// list namespaces matched by  namespaceSelector
	// for each namespace apply list with  selector
	// combine result

	if cr.Spec.StaticScrapeNamespaceSelector == nil {
		namespaces = append(namespaces, cr.Namespace)
	} else if cr.Spec.StaticScrapeNamespaceSelector.MatchExpressions == nil && cr.Spec.StaticScrapeNamespaceSelector.MatchLabels == nil {
		namespaces = nil
	} else {
		log.Info("selector for staticScrape", "vmagent", cr.Name, "selector", cr.Spec.StaticScrapeNamespaceSelector.String())
		nsSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.StaticScrapeNamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert staticScrapeNamespaceSelector to labelSelector: %w", err)
		}
		namespaces, err = selectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot select namespaces for staticScrape match: %w", err)
		}
	}

	// if namespaces isn't nil, then nameSpaceSelector is defined
	// but scrapeSelector maybe be nil and we have to set it to catch all value
	if namespaces != nil && cr.Spec.StaticScrapeSelector == nil {
		cr.Spec.StaticScrapeSelector = &metav1.LabelSelector{}
	}
	staticScrapeSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.StaticScrapeSelector)
	if err != nil {
		return nil, fmt.Errorf("cannot convert staticScrapeSelector to label selector: %w", err)
	}

	staticScrapesCombined := []victoriametricsv1beta1.VMStaticScrape{}

	//list all namespaces for static cfg with selector
	if namespaces == nil {
		log.Info("listing all namespaces for staticScrapes")
		staticScrapes := &victoriametricsv1beta1.VMStaticScrapeList{}
		err = rclient.List(ctx, staticScrapes, &client.ListOptions{LabelSelector: staticScrapeSelector})
		if err != nil {
			return nil, fmt.Errorf("cannot list staticScrapes from all namespaces: %w", err)
		}
		staticScrapesCombined = append(staticScrapesCombined, staticScrapes.Items...)

	} else {
		for _, ns := range namespaces {
			listOpts := &client.ListOptions{Namespace: ns, LabelSelector: staticScrapeSelector}
			staticScrapes := &victoriametricsv1beta1.VMStaticScrapeList{}
			err = rclient.List(ctx, staticScrapes, listOpts)
			if err != nil {
				return nil, fmt.Errorf("cannot list staticScrapes at namespace: %s, err: %w", ns, err)
			}
			staticScrapesCombined = append(staticScrapesCombined, staticScrapes.Items...)

		}
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
	nodes map[string]*victoriametricsv1beta1.VMNodeScrape,
	pods map[string]*victoriametricsv1beta1.VMPodScrape,
	statics map[string]*victoriametricsv1beta1.VMStaticScrape,
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
	scrapeSvc := &victoriametricsv1beta1.VMServiceScrape{
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
	err := rclient.Create(ctx, scrapeSvc)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// need to update
			return updateServiceScrape(ctx, rclient, scrapeSvc)
		}
		return err
	}
	return nil

}

func updateServiceScrape(ctx context.Context, rclient client.Client, newServiceScrape *victoriametricsv1beta1.VMServiceScrape) error {
	existServiceScrape := &victoriametricsv1beta1.VMServiceScrape{}
	err := rclient.Get(ctx, types.NamespacedName{Name: newServiceScrape.Name, Namespace: newServiceScrape.Namespace}, existServiceScrape)
	if err != nil {
		return fmt.Errorf("cannot get VMServiceScrape for update: %w", err)
	}
	existServiceScrape.Spec = newServiceScrape.Spec
	existServiceScrape.Labels = newServiceScrape.Labels
	existServiceScrape.Annotations = newServiceScrape.Annotations
	return rclient.Update(ctx, existServiceScrape)
}
