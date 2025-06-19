package vmagent

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/VictoriaMetrics/metricsql"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
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

var skipNon = func(_ error) bool {
	return false
}

func (so *scrapeObjects) mustValidateObjects(cr *vmv1beta1.VMAgent) {
	var err error
	so.sss, so.sssBroken, err = forEachCollectSkipOn(so.sss, so.sssBroken, func(ss *vmv1beta1.VMServiceScrape) error {
		if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
			for _, ep := range ss.Spec.Endpoints {
				if err := testForArbitraryFSAccess(ep.EndpointAuth); err != nil {
					return err
				}
			}
		}
		if _, err := metav1.LabelSelectorAsSelector(&ss.Spec.Selector); err != nil {
			return err
		}
		return nil
	}, skipNon)
	if err != nil {
		panic(fmt.Errorf("BUG: validation cannot return error for ServiceScrape: %s", err))
	}

	so.pss, so.pssBroken, err = forEachCollectSkipOn(so.pss, so.pssBroken, func(ps *vmv1beta1.VMPodScrape) error {
		if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
			for _, ep := range ps.Spec.PodMetricsEndpoints {
				if err := testForArbitraryFSAccess(ep.EndpointAuth); err != nil {
					return err
				}
			}
		}
		if _, err := metav1.LabelSelectorAsSelector(&ps.Spec.Selector); err != nil {
			return err
		}
		return nil
	}, skipNon)
	if err != nil {
		panic(fmt.Errorf("BUG: validation cannot return error for PodScrape: %s", err))
	}

	so.stss, so.stssBroken, err = forEachCollectSkipOn(so.stss, so.stssBroken, func(sts *vmv1beta1.VMStaticScrape) error {
		if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
			for _, ep := range sts.Spec.TargetEndpoints {
				if err := testForArbitraryFSAccess(ep.EndpointAuth); err != nil {
					return err
				}
			}
		}
		return nil
	}, skipNon)
	if err != nil {
		panic(fmt.Errorf("BUG: validation cannot return error for StaticScrape: %s", err))
	}

	so.nss, so.nssBroken, err = forEachCollectSkipOn(so.nss, so.nssBroken, func(ns *vmv1beta1.VMNodeScrape) error {
		if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
			if err := testForArbitraryFSAccess(ns.Spec.EndpointAuth); err != nil {
				return err
			}
		}
		return nil
	}, skipNon)
	if err != nil {
		panic(fmt.Errorf("BUG: validation cannot return error for NodeScrape: %s", err))
	}

	so.prss, so.prssBroken, err = forEachCollectSkipOn(so.prss, so.prssBroken, func(prs *vmv1beta1.VMProbe) error {
		if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
			if err := testForArbitraryFSAccess(prs.Spec.EndpointAuth); err != nil {
				return err
			}
		}
		if prs.Spec.Targets.Ingress != nil {
			_, err := metav1.LabelSelectorAsSelector(&prs.Spec.Targets.Ingress.Selector)
			if err != nil {
				return fmt.Errorf("cannot parse spec.selector: %w", err)
			}
		}
		return nil
	}, skipNon)
	if err != nil {
		panic(fmt.Errorf("BUG: validation cannot return error for ProbeScrape: %s", err))
	}

	so.scss, so.scssBroken, err = forEachCollectSkipOn(so.scss, so.scssBroken, func(scss *vmv1beta1.VMScrapeConfig) error {
		// TODO: @f41gh7 validate per configuration FS access
		if cr.Spec.ArbitraryFSAccessThroughSMs.Deny {
			if err := testForArbitraryFSAccess(scss.Spec.EndpointAuth); err != nil {
				return err
			}

		}
		return nil
	}, skipNon)
	if err != nil {
		panic(fmt.Errorf("BUG: validation cannot return error for scrapeConfig: %s", err))
	}
}

// CreateOrUpdateConfigurationSecret builds scrape configuration for VMAgent
func CreateOrUpdateConfigurationSecret(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent, childObject client.Object) error {
	var prevCR *vmv1beta1.VMAgent
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	ac := getAssetsCache(ctx, rclient, cr)
	if err := createOrUpdateConfigurationSecret(ctx, rclient, cr, prevCR, childObject, ac); err != nil {
		return err
	}
	return nil
}

func createOrUpdateConfigurationSecret(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAgent, childObject client.Object, ac *build.AssetsCache) error {
	if cr.Spec.IngestOnlyMode {
		return nil
	}
	// HACK: makeSpec could load content into ac and it must be called
	// before secret config reconcile
	if _, err := makeSpec(cr, ac); err != nil {
		return err
	}

	sss, err := selectServiceScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting ServiceScrapes failed: %w", err)
	}

	pScrapes, err := selectPodScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting PodScrapes failed: %w", err)
	}

	probes, err := selectVMProbes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting VMProbes failed: %w", err)
	}

	nodes, err := selectVMNodeScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting VMNodeScrapes failed: %w", err)
	}

	statics, err := selectStaticScrapes(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting VMStaticScrapes failed: %w", err)
	}

	scrapeConfigs, err := selectScrapeConfig(ctx, cr, rclient)
	if err != nil {
		return fmt.Errorf("selecting ScrapeConfigs failed: %w", err)
	}
	sos := &scrapeObjects{
		sss:  sss,
		pss:  pScrapes,
		prss: probes,
		nss:  nodes,
		stss: statics,
		scss: scrapeConfigs,
	}
	sos.mustValidateObjects(cr)

	var additionalScrapeConfigs []byte

	if cr.Spec.AdditionalScrapeConfigs != nil {
		sc, err := ac.LoadKeyFromSecret(cr.Namespace, cr.Spec.AdditionalScrapeConfigs)
		if err != nil {
			return fmt.Errorf("loading additional scrape configs from Secret failed: %w", err)
		}
		additionalScrapeConfigs = []byte(sc)
	}

	// Update secret based on the most recent configuration.
	generatedConfig, err := generateConfig(
		ctx,
		cr,
		sos,
		ac,
		additionalScrapeConfigs,
	)
	if err != nil {
		return fmt.Errorf("generating config for vmagent failed: %w", err)
	}

	for kind, secret := range ac.GetOutput() {
		var prevSecretMeta *metav1.ObjectMeta
		if prevCR != nil {
			prevSecretMeta = ptr.To(build.ResourceMeta(kind, prevCR))
		}
		if kind == build.SecretConfigResourceKind {
			// Compress config to avoid 1mb secret limit for a while
			var buf bytes.Buffer
			if err = gzipConfig(&buf, generatedConfig); err != nil {
				return fmt.Errorf("cannot gzip config for vmagent: %w", err)
			}
			secret.Data[vmagentGzippedFilename] = buf.Bytes()
		}
		secret.ObjectMeta = build.ResourceMeta(kind, cr)
		secret.Annotations = map[string]string{
			"generated": "true",
		}
		if err := reconcile.Secret(ctx, rclient, &secret, prevSecretMeta); err != nil {
			return err
		}
	}

	if err := updateStatusesForScrapeObjects(ctx, rclient, cr, sos, childObject); err != nil {
		return err
	}

	return nil
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
func forEachCollectSkipOn[T scrapeObjectWithStatus](src, srcBroken []T, apply func(s T) error, skipOn func(error) bool) ([]T, []T, error) {
	var cnt int
	for _, o := range src {
		if err := apply(o); err != nil {
			if skipOn(err) {
				st := o.GetStatusMetadata()
				st.CurrentSyncError = err.Error()
				srcBroken = append(srcBroken, o)
				continue
			}
			return nil, nil, err
		}
		src[cnt] = o
		cnt++
	}
	src = src[:cnt]
	return src, srcBroken, nil
}

// TODO: @f41gh7 validate VMScrapeParams
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

func setScrapeIntervalToWithLimit(ctx context.Context, dst *vmv1beta1.EndpointScrapeParams, cr *vmv1beta1.VMAgent) {
	if dst.ScrapeInterval == "" {
		dst.ScrapeInterval = dst.Interval
	}

	originInterval, minIntervalStr, maxIntervalStr := dst.ScrapeInterval, cr.Spec.MinScrapeInterval, cr.Spec.MaxScrapeInterval
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
	ac *build.AssetsCache,
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
		configLen := len(scrapeConfigs)
		for i, ep := range ss.Spec.Endpoints {
			s, err := generateServiceScrapeConfig(
				ctx,
				cr,
				ss,
				ep, i,
				apiserverConfig,
				ac,
				cr.Spec.VMAgentSecurityEnforcements,
			)
			if err != nil {
				scrapeConfigs = scrapeConfigs[:configLen]
				if build.IsNotFound(err) {
					break
				}
				return nil, err
			}
			scrapeConfigs = append(scrapeConfigs, s)
		}
	}
	for _, identifier := range sos.pss {
		configLen := len(scrapeConfigs)
		for i, ep := range identifier.Spec.PodMetricsEndpoints {
			s, err := generatePodScrapeConfig(
				ctx,
				cr,
				identifier, ep, i,
				apiserverConfig,
				ac,
				cr.Spec.VMAgentSecurityEnforcements,
			)
			if err != nil {
				scrapeConfigs = scrapeConfigs[:configLen]
				if build.IsNotFound(err) {
					break
				}
				return nil, err
			}
			scrapeConfigs = append(scrapeConfigs, s)
		}
	}

	for i, identifier := range sos.prss {
		s, err := generateProbeConfig(
			ctx,
			cr,
			identifier,
			i,
			apiserverConfig,
			ac,
			cr.Spec.VMAgentSecurityEnforcements,
		)
		if err != nil {
			if build.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		scrapeConfigs = append(scrapeConfigs, s)
	}
	for _, identifier := range sos.nss {
		s, err := generateNodeScrapeConfig(
			ctx,
			cr,
			identifier,
			apiserverConfig,
			ac,
			cr.Spec.VMAgentSecurityEnforcements,
		)
		if err != nil {
			if build.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		scrapeConfigs = append(scrapeConfigs, s)
	}

	for _, identifier := range sos.stss {
		configLen := len(scrapeConfigs)
		for i, ep := range identifier.Spec.TargetEndpoints {
			s, err := generateStaticScrapeConfig(
				ctx,
				cr,
				identifier,
				ep, i,
				ac,
				cr.Spec.VMAgentSecurityEnforcements,
			)
			if err != nil {
				scrapeConfigs = scrapeConfigs[:configLen]
				if build.IsNotFound(err) {
					break
				}
				return nil, err
			}
			scrapeConfigs = append(scrapeConfigs, s)
		}
	}

	for _, identifier := range sos.scss {
		s, err := generateScrapeConfig(
			ctx,
			cr,
			identifier,
			ac,
			cr.Spec.VMAgentSecurityEnforcements,
		)
		if err != nil {
			if build.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		scrapeConfigs = append(scrapeConfigs, s)
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

func addRelabelConfigs(dst []yaml.MapSlice, rcs []*vmv1beta1.RelabelConfig) []yaml.MapSlice {
	for i := range rcs {
		rc := rcs[i]
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

type generateK8SSDConfigOptions struct {
	namespaces          []string
	apiServerConfig     *vmv1beta1.APIServerConfig
	role                string
	attachMetadata      *vmv1beta1.AttachMetadata
	shouldAddSelectors  bool
	selectors           metav1.LabelSelector
	mustUseNodeSelector bool
	namespace           string
}

func generateK8SSDConfig(ac *build.AssetsCache, opts generateK8SSDConfigOptions) (yaml.MapSlice, error) {
	k8sSDConfig := yaml.MapSlice{
		{
			Key:   "role",
			Value: opts.role,
		},
	}
	switch opts.role {
	case kubernetesSDRoleEndpoint, kubernetesSDRoleEndpointSlices, kubernetesSDRolePod:
		k8sSDConfig = addAttachMetadata(k8sSDConfig, opts.attachMetadata)
	}
	if len(opts.namespaces) != 0 {
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key: "namespaces",
			Value: yaml.MapSlice{
				{
					Key:   "names",
					Value: opts.namespaces,
				},
			},
		})
	}

	if opts.apiServerConfig != nil {
		apiserverConfig := opts.apiServerConfig
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key: "api_server", Value: apiserverConfig.Host,
		})

		if apiserverConfig.BasicAuth != nil {
			cfg, err := ac.BasicAuthToYAML(opts.namespace, apiserverConfig.BasicAuth)
			if err != nil {
				return nil, fmt.Errorf("could not generate basicAuth for apiserver config: %w", err)
			}
			if len(cfg) > 0 {
				k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "basic_auth", Value: cfg})
			}
		}

		if apiserverConfig.BearerTokenFile != "" {
			k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "bearer_token_file", Value: apiserverConfig.BearerTokenFile})
		} else if apiserverConfig.BearerToken != "" {
			k8sSDConfig = append(k8sSDConfig, yaml.MapItem{Key: "bearer_token", Value: apiserverConfig.BearerToken})
		}

		if apiserverConfig.Authorization != nil {
			cfg, err := ac.AuthorizationToYAML(opts.namespace, apiserverConfig.Authorization)
			if err != nil {
				return nil, fmt.Errorf("cannot fetch authorization secret for apiserver config: %w", err)
			}
			if len(cfg) > 0 {
				k8sSDConfig = append(k8sSDConfig, cfg...)
			}
		}

		// config as well, make sure to path the right namespace here.
		if apiserverConfig.TLSConfig != nil {
			cfg, err := ac.TLSToYAML(opts.namespace, "", apiserverConfig.TLSConfig)
			if err != nil {
				return nil, fmt.Errorf("cannot add tls asset for apiServerConfig: %w", err)
			}
			if len(cfg) > 0 {
				k8sSDConfig = append(k8sSDConfig, cfg...)
			}
		}
	}

	var selectors []yaml.MapSlice

	isEmptySelectors := len(opts.selectors.MatchLabels)+len(opts.selectors.MatchExpressions) == 0
	switch {
	case opts.mustUseNodeSelector:
		var selector yaml.MapSlice
		selector = append(selector, yaml.MapItem{
			Key:   "role",
			Value: kubernetesSDRolePod,
		})
		selector = append(selector, yaml.MapItem{
			Key:   "field",
			Value: "spec.nodeName=" + kubeNodeEnvTemplate,
		})
		selectors = append(selectors, selector)

	case opts.shouldAddSelectors && !isEmptySelectors:
		var selector yaml.MapSlice
		selector = append(selector, yaml.MapItem{
			Key:   "role",
			Value: opts.role,
		})
		labelSelector, err := metav1.LabelSelectorAsSelector(&opts.selectors)
		if err != nil {
			panic(fmt.Sprintf("BUG: unexpected error, selectors must be already validated: %q: %s", opts.selectors.String(), err))
		}
		selector = append(selector, yaml.MapItem{
			Key:   "label",
			Value: labelSelector.String(),
		})
		selectors = append(selectors, selector)

		// special case, given roles create additional watchers for
		// pod and services roles
		if opts.role == kubernetesSDRoleEndpoint || opts.role == kubernetesSDRoleEndpointSlices {
			for _, role := range []string{kubernetesSDRolePod, kubernetesSDRoleService} {
				selectors = append(selectors, yaml.MapSlice{
					{
						Key:   "role",
						Value: role,
					},
					{
						Key:   "label",
						Value: labelSelector.String(),
					},
				})
			}
		}
	}
	if len(selectors) > 0 {
		k8sSDConfig = append(k8sSDConfig, yaml.MapItem{
			Key:   "selectors",
			Value: selectors,
		})
	}

	return yaml.MapSlice{
		{
			Key: "kubernetes_sd_configs",
			Value: []yaml.MapSlice{
				k8sSDConfig,
			},
		},
	}, nil
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

func buildVMScrapeParams(namespace string, cfg *vmv1beta1.VMScrapeParams, ac *build.AssetsCache) (yaml.MapSlice, error) {
	var r yaml.MapSlice
	if cfg == nil {
		return r, nil
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
		if c, err := ac.ProxyAuthToYAML(namespace, cfg.ProxyClientConfig); err != nil {
			return nil, err
		} else if len(c) > 0 {
			r = append(r, c...)
		}
	}
	return r, nil
}

func addSelectorToRelabelingFor(relabelings []yaml.MapSlice, typeName string, selector metav1.LabelSelector, mustSkipAdd bool) []yaml.MapSlice {
	if mustSkipAdd {
		return relabelings
	}
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

func addEndpointAuthTo(cfg yaml.MapSlice, ea *vmv1beta1.EndpointAuth, namespace string, ac *build.AssetsCache) (yaml.MapSlice, error) {
	if c, err := ac.TLSToYAML(namespace, "", ea.TLSConfig); err != nil {
		return nil, err
	} else if len(c) > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "tls_config", Value: c})
	}
	if ea.BearerTokenFile != "" {
		cfg = append(cfg, yaml.MapItem{Key: "bearer_token_file", Value: ea.BearerTokenFile})
	} else if ea.BearerTokenSecret != nil && ea.BearerTokenSecret.Name != "" {
		if secret, err := ac.LoadKeyFromSecret(namespace, ea.BearerTokenSecret); err != nil {
			return nil, err
		} else {
			cfg = append(cfg, yaml.MapItem{Key: "bearer_token", Value: secret})
		}
	}
	if ea.BasicAuth != nil {
		if c, err := ac.BasicAuthToYAML(namespace, ea.BasicAuth); err != nil {
			return nil, err
		} else if len(c) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "basic_auth", Value: c})
		}
	}
	if c, err := ac.OAuth2ToYAML(namespace, ea.OAuth2); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	if c, err := ac.AuthorizationToYAML(namespace, ea.Authorization); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	return cfg, nil
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAgent) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.SecretConfigResourceKind: {
			MountDir:   vmAgentConfDir,
			SecretName: build.ResourceName(build.SecretConfigResourceKind, cr),
		},
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}
