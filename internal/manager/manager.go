package manager

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	"github.com/go-logr/logr"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap/zapcore"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	restmetrics "k8s.io/client-go/tools/metrics"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	vmcontroller "github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	webhookv1 "github.com/VictoriaMetrics/operator/internal/webhook/operator/v1"
	webhookv1beta1 "github.com/VictoriaMetrics/operator/internal/webhook/operator/v1beta1"
)

const defaultMetricsAddr = ":8080"
const defaultWebhookPort = 9443

var (
	managerFlags = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	startTime    = time.Now()
	appVersion   = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "vm_app_version",
		Help: "version of application",
		ConstLabels: map[string]string{
			"version":       buildinfo.Version,
			"short_version": buildinfo.ShortVersion(),
		},
	}, func() float64 {
		return 1.0
	})
	uptime = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "vm_app_uptime_seconds", Help: "uptime"}, func() float64 {
		return time.Since(startTime).Seconds()
	})
	startedAt = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "vm_app_start_timestamp", Help: "unixtimestamp"}, func() float64 {
		return float64(startTime.Unix())
	})
	clientQPSLimit = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "operator_rest_client_qps_limit", Help: "rest client max query per second limit"}, func() float64 {
		return float64(*clientQPS)
	})
	scheme                   = runtime.NewScheme()
	setupLog                 = ctrl.Log.WithName("setup")
	leaderElect              = managerFlags.Bool("leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	leaderElectNamespace     = managerFlags.String("leader-elect-namespace", "", "Defines optional namespace name in which the leader election resource will be created. By default, uses in-cluster namespace name.")
	leaderElectID            = managerFlags.String("leader-elect-id", "57410f0d.victoriametrics.com", "Defines the name of the resource that leader election will use for holding the leader lock.")
	leaderElectLeaseDuration = managerFlags.Duration("leader-elect-lease-duration", 15*time.Second, "Defines the duration that non-leader candidates will wait to force acquire leadership. This is measured against time of last observed ack.")
	leaderElectRenewDeadline = managerFlags.Duration("leader-elect-renew-deadline", 10*time.Second, "Defines the duration that the acting controlplane will retry refreshing leadership lock before giving up.")

	enableWebhook       = managerFlags.Bool("webhook.enable", false, "adds webhook server, you must mount cert and key or use cert-manager")
	webhookPort         = managerFlags.Int("webhook.port", defaultWebhookPort, "port to start webhook server on")
	disableCRDOwnership = managerFlags.Bool("controller.disableCRDOwnership", false, "disables CRD ownership add to cluster wide objects, must be disabled for clusters, lower than v1.16.0")
	webhookCertDir      = managerFlags.String("webhook.certDir", "/tmp/k8s-webhook-server/serving-certs/", "root directory for webhook cert and key")
	webhookCertName     = managerFlags.String("webhook.certName", "tls.crt", "name of webhook server Tls certificate inside tls.certDir")
	webhookCertKey      = managerFlags.String("webhook.keyName", "tls.key", "name of webhook server Tls key inside tls.certDir")
	tlsEnable           = managerFlags.Bool("tls.enable", false, "enables secure tls (https) for metrics webserver.")
	tlsCertDir          = managerFlags.String("tls.certDir", "/tmp/k8s-metrics-server/serving-certs", "root directory for metrics webserver cert, key and mTLS CA.")
	tlsCertName         = managerFlags.String("tls.certName", "tls.crt", "name of metric server Tls certificate inside tls.certDir. Default - ")
	tlsCertKey          = managerFlags.String("tls.keyName", "tls.key", "name of metric server Tls key inside tls.certDir. Default - tls.key")
	mtlsEnable          = managerFlags.Bool("mtls.enable", false, "Whether to require valid client certificate for https requests to the corresponding -metrics-bind-address. This flag works only if -tls.enable flag is set.")
	mtlsCAFile          = managerFlags.String("mtls.CAName", "clietCA.crt", "Optional name of TLS Root CA for verifying client certificates at the corresponding -metrics-bind-address when -mtls.enable is enabled. "+
		"By default the host system TLS Root CA is used for client certificate verification. ")
	metricsAddr                   = managerFlags.String("metrics-bind-address", defaultMetricsAddr, "The address the metric endpoint binds to.")
	pprofAddr                     = managerFlags.String("pprof-addr", ":8435", "The address for pprof/debug API. Empty value disables server")
	probeAddr                     = managerFlags.String("health-probe-bind-address", ":8081", "The address the probes (health, ready) binds to.")
	defaultKubernetesMinorVersion = managerFlags.Uint64("default.kubernetesVersion.minor", 21, "Minor version of kubernetes server, if operator cannot parse actual kubernetes response")
	defaultKubernetesMajorVersion = managerFlags.Uint64("default.kubernetesVersion.major", 1, "Major version of kubernetes server, if operator cannot parse actual kubernetes response")
	printDefaults                 = managerFlags.Bool("printDefaults", false, "print all variables with their default values and exit")
	printFormat                   = managerFlags.String("printFormat", "table", "output format for --printDefaults. Can be table, json, yaml or list")
	promCRDResyncPeriod           = managerFlags.Duration("controller.prometheusCRD.resyncPeriod", 0, "Configures resync period for prometheus CRD converter. Disabled by default")
	clientQPS                     = managerFlags.Int("client.qps", 50, "defines K8s client QPS. The value should be increased for the cluster with large number of objects > 10_000.")
	clientBurst                   = managerFlags.Int("client.burst", 100, "defines K8s client burst")
	wasCacheSynced                = uint32(0)
	disableCacheForObjects        = managerFlags.String("controller.disableCacheFor", "configmap,secret", "disables client for cache for API resources. Supported objects - namespace,pod,service,secret,configmap,deployment,statefulset")
	disableSecretKeySpaceTrim     = managerFlags.Bool("disableSecretKeySpaceTrim", false, "disables trim of space at Secret/Configmap value content. It's a common mistake to put new line to the base64 encoded secret value.")
	version                       = managerFlags.Bool("version", false, "Show operator version")
	disableControllerForCRD       = managerFlags.String("controller.disableReconcileFor", "", "disables reconcile controllers for given list of comma separated CRD names. For example - VMCluster,VMSingle,VMAuth."+
		"Note, child controllers still require parent object CRDs.")
	loggerJSONFields = managerFlags.String("loggerJSONFields", "", "Allows renaming fields in JSON formatted logs"+
		`Example: "ts:timestamp,msg:message" renames "ts" to "timestamp" and "msg" to "message".`+
		"Supported fields: ts, level, caller, msg")
	statusUpdateTTL = managerFlags.Duration("controller.statusLastUpdateTimeTTL", time.Hour, "Configures TTL for LastUpdateTime status.conditions fields. "+
		"It's used to detect stale parent objects on child objects. Like VMAlert->VMRule .status.Conditions.Type")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	utilruntime.Must(vmv1.AddToScheme(scheme))
	utilruntime.Must(metav1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(promv1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	build.AddDefaults(scheme)
	// +kubebuilder:scaffold:scheme
}

func RunManager(ctx context.Context) error {
	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	opts := zap.Options{
		StacktraceLevel: zapcore.PanicLevel,
	}

	opts.BindFlags(managerFlags)

	// If warn is still absent from zap-log-level commandline help explain why
	if f := managerFlags.Lookup("zap-log-level"); f != nil && !strings.Contains(f.Usage, "warn") {
		f.Usage += "\nNote: warn is missing by design due to warn level not being supported by controller-runtime\n" +
			"See: https://dave.cheney.net/2015/11/05/lets-talk-about-logging and https://github.com/kubernetes-sigs/controller-runtime/issues/2002 for more information."
	}

	vmcontroller.BindFlags(managerFlags)
	if err := managerFlags.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("cannot parse provided flags: %w", err)
	}
	if *version {
		fmt.Fprintf(flag.CommandLine.Output(), "%s\n", buildinfo.Version)
		os.Exit(0)
	}

	baseConfig := config.MustGetBaseConfig()
	if *printDefaults {
		err := baseConfig.PrintDefaults(*printFormat)
		if err != nil {
			setupLog.Error(err, "cannot print variables")
			os.Exit(1)
		}
		return nil
	}

	zap.UseFlagOptions(&opts)
	zapEncOpts := mustGetLoggerEncodingOpts()

	sink := zap.New(zap.UseFlagOptions(&opts), func(o *zap.Options) {
		o.EncoderConfigOptions = append(o.EncoderConfigOptions, zapEncOpts...)
	}).GetSink()
	l := logger.New(sink)
	logf.SetLogger(l)

	// Use a zap logr.Logger implementation. If none of the zap
	// flags are configured (or if the zap flag set is not being
	// used), this defaults to a production zap logger.
	//
	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	klog.SetLogger(l)
	ctrl.SetLogger(l)

	metricServerTLSOpts, err := getMetricsServerMTLSOpts()
	if err != nil {
		return fmt.Errorf("cannot setup metrics server TLS: %w", err)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		Port:     *webhookPort,
		CertDir:  *webhookCertDir,
		CertName: *webhookCertName,
		KeyName:  *webhookCertKey,
	})

	metricsServerOptions := metricsserver.Options{
		BindAddress:   *metricsAddr,
		SecureServing: *tlsEnable,
		CertDir:       *tlsCertDir,
		CertName:      *tlsCertName,
		KeyName:       *tlsCertKey,
		TLSOpts:       metricServerTLSOpts,
	}

	setupLog.Info(fmt.Sprintf("starting VictoriaMetrics operator build version: %s, short_version: %s", buildinfo.Version, buildinfo.ShortVersion()))
	r := metrics.Registry
	r.MustRegister(appVersion, uptime, startedAt, clientQPSLimit)
	mustAddRestClientMetrics(r)
	flagsAsMetrics(r, managerFlags)
	config.ConfigAsMetrics(r, baseConfig)

	setupLog.Info("Registering Components.")
	var watchNsCacheByName map[string]cache.Config
	watchNss := config.MustGetWatchNamespaces()
	if len(watchNss) > 0 {
		setupLog.Info("operator configured with watching for subset of namespaces, cluster wide access is disabled", "namespaces", strings.Join(watchNss, ","))
		watchNsCacheByName = make(map[string]cache.Config)
		for _, ns := range watchNss {
			watchNsCacheByName[ns] = cache.Config{}
		}
	}

	reconcile.InitDeadlines(baseConfig.PodWaitReadyIntervalCheck, baseConfig.AppReadyTimeout, baseConfig.PodWaitReadyTimeout)
	reconcile.SetStatusUpdateTTL(*statusUpdateTTL)
	config := ctrl.GetConfigOrDie()
	config.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(*clientQPS), *clientBurst)

	co, err := getClientCacheOptions(*disableCacheForObjects)
	if err != nil {
		return fmt.Errorf("cannot build cache options for manager: %w", err)
	}
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Logger:                  ctrl.Log.WithName("manager"),
		Scheme:                  scheme,
		Metrics:                 metricsServerOptions,
		HealthProbeBindAddress:  *probeAddr,
		PprofBindAddress:        *pprofAddr,
		ReadinessEndpointName:   "/ready",
		LivenessEndpointName:    "/health",
		WebhookServer:           webhookServer,
		LeaderElection:          *leaderElect,
		LeaderElectionNamespace: *leaderElectNamespace,
		LeaderElectionID:        *leaderElectID,
		LeaseDuration:           leaderElectLeaseDuration,
		RenewDeadline:           leaderElectRenewDeadline,
		Cache: cache.Options{
			DefaultNamespaces: watchNsCacheByName,
		},
		Client: client.Options{
			Cache: co,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	if err := mgr.AddReadyzCheck("ready", func(req *http.Request) error {
		wasSynced := atomic.LoadUint32(&wasCacheSynced)
		// fast path
		if wasSynced > 0 {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		ok := mgr.GetCache().WaitForCacheSync(ctx)
		if ok {
			atomic.StoreUint32(&wasCacheSynced, 1)
			return nil
		}
		return fmt.Errorf("controller sync cache in progress")
	}); err != nil {
		return fmt.Errorf("cannot register ready endpoint: %w", err)
	}

	// no-op
	if err := mgr.AddHealthzCheck("health", func(req *http.Request) error {
		return nil
	}); err != nil {
		return fmt.Errorf("cannot register health endpoint: %w", err)
	}

	if !*disableCRDOwnership && len(watchNss) == 0 {
		initC, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
		if err != nil {
			return err
		}
		l.Info("starting CRD ownership controller")
		if err := vmv1beta1.Init(ctx, initC); err != nil {
			setupLog.Error(err, "unable to init crd data")
			return err
		}
	}

	if *enableWebhook {
		if err := addWebhooks(mgr); err != nil {
			l.Error(err, "cannot register webhooks")
			return err
		}

		build.SetSkipRuntimeValidation(true)
	}

	if err := initControllers(mgr, ctrl.Log, baseConfig); err != nil {
		return err
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting vmconverter clients")

	baseClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("cannot create k8s-go-client instance: %w", err)
	}

	build.SetSpaceTrim(*disableSecretKeySpaceTrim)
	k8sServerVersion, err := baseClient.ServerVersion()
	if err != nil {
		return fmt.Errorf("cannot get kubernetes server version: %w", err)
	}
	if err := k8stools.SetKubernetesVersionWithDefaults(k8sServerVersion, *defaultKubernetesMinorVersion, *defaultKubernetesMajorVersion); err != nil {
		// log error and do nothing, because we are using sane default values.
		setupLog.Error(err, "cannot parse kubernetes version, using default flag values, fallback to default values")
	}

	setupLog.Info(fmt.Sprintf("using kubernetes server version: %s", k8sServerVersion.String()))
	wc, err := client.NewWithWatch(mgr.GetConfig(), client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("cannot setup watch client: %w", err)
	}
	converterController, err := vmcontroller.NewConverterController(ctx, baseClient, wc, *promCRDResyncPeriod, baseConfig)
	if err != nil {
		setupLog.Error(err, "cannot setup prometheus CRD converter: %w", err)
		return err
	}

	if err := mgr.Add(converterController); err != nil {
		setupLog.Error(err, "cannot add runnable")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("cannot start controller manager: %w", err)
	}

	setupLog.Info("gracefully stopped")
	return nil
}

func addWebhooks(mgr ctrl.Manager) error {
	f := func(setupWebhooks []func(ctrl.Manager) error) error {
		for _, setupWebhook := range setupWebhooks {
			if err := setupWebhook(mgr); err != nil {
				return err
			}
		}
		return nil
	}
	return f([]func(ctrl.Manager) error{
		webhookv1beta1.SetupVMAgentWebhookWithManager,
		webhookv1beta1.SetupVMAlertWebhookWithManager,
		webhookv1.SetupVMAnomalyWebhookWithManager,
		webhookv1beta1.SetupVMSingleWebhookWithManager,
		webhookv1beta1.SetupVMClusterWebhookWithManager,
		webhookv1beta1.SetupVLogsWebhookWithManager,
		webhookv1.SetupVLAgentWebhookWithManager,
		webhookv1.SetupVLSingleWebhookWithManager,
		webhookv1.SetupVLClusterWebhookWithManager,
		webhookv1.SetupVTSingleWebhookWithManager,
		webhookv1.SetupVTClusterWebhookWithManager,
		webhookv1beta1.SetupVMAlertmanagerWebhookWithManager,
		webhookv1beta1.SetupVMAlertmanagerConfigWebhookWithManager,
		webhookv1beta1.SetupVMAuthWebhookWithManager,
		webhookv1beta1.SetupVMUserWebhookWithManager,
		webhookv1beta1.SetupVMRuleWebhookWithManager,
		webhookv1beta1.SetupVMServiceScrapeWebhookWithManager,
		webhookv1beta1.SetupVMPodScrapeWebhookWithManager,
		webhookv1beta1.SetupVMNodeScrapeWebhookWithManager,
		webhookv1beta1.SetupVMScrapeConfigWebhookWithManager,
		webhookv1beta1.SetupVMStaticScrapeWebhookWithManager,
		webhookv1beta1.SetupVMProbeWebhookWithManager,
	})
}

func getClientCacheOptions(disabledCacheObjects string) (*client.CacheOptions, error) {
	var co client.CacheOptions
	if len(disabledCacheObjects) > 0 {
		objects := strings.Split(disabledCacheObjects, ",")
		for _, object := range objects {
			o, ok := cacheClientObjectsByName[object]
			if !ok {
				return nil, fmt.Errorf("not supported client object name=%q", object)
			}
			co.DisableFor = append(co.DisableFor, o)

		}
	}
	return &co, nil
}

// cacheClientObjectsByName defines objects for which cache could be disabled via flag controller.disableCacheFor
//
// It's needed to reduce memory usage by operator.
// Operator client caches objects in memory at the following conditions:
// 1) if controller Owns resource - explicit init
// 2) if resource is requested by code - lazy init
//
// by default cache is disabled for configmap and secrets,
// which may occupy a lot of memory.
var cacheClientObjectsByName = map[string]client.Object{
	"secret":      &corev1.Secret{},
	"configmap":   &corev1.ConfigMap{},
	"service":     &corev1.Service{},
	"namespace":   &corev1.Namespace{},
	"pod":         &corev1.Pod{},
	"deployment":  &appsv1.Deployment{},
	"statefulset": &appsv1.StatefulSet{},
}

// runtime-controller doesn't expose this metric
// due to high cardinality
var restClientLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "rest_client_request_duration_seconds"}, []string{"method", "api"})

type latencyMetricWrapper struct {
	collector *prometheus.HistogramVec
}

var apiLatencyPrefixAllowList = []string{
	"/apis/rbac.authorization.k8s.io/v1/",
	"/apis/operator.victoriametrics.com/",
	"/apis/apps/v1/",
	"/api/v1/",
}

func (lmw *latencyMetricWrapper) Observe(ctx context.Context, verb string, u url.URL, latency time.Duration) {
	apiPath := u.Path
	var shouldObserveReqLatency bool
	for _, allowedPrefix := range apiLatencyPrefixAllowList {
		if strings.HasPrefix(apiPath, allowedPrefix) {
			shouldObserveReqLatency = true
			break
		}
	}
	if !shouldObserveReqLatency {
		return
	}
	lmw.collector.WithLabelValues(verb, apiPath).Observe(latency.Seconds())
}

func mustAddRestClientMetrics(r metrics.RegistererGatherer) {
	// replace global go-client RequestLatency metric
	restmetrics.RequestLatency = &latencyMetricWrapper{collector: restClientLatency}
	r.MustRegister(restClientLatency)
}

type crdController interface {
	Init(client.Client, logr.Logger, *runtime.Scheme, *config.BaseOperatorConf)
	SetupWithManager(mgr ctrl.Manager) error
}

var controllersByName = map[string]crdController{
	"VMCluster":            &vmcontroller.VMClusterReconciler{},
	"VMAgent":              &vmcontroller.VMAgentReconciler{},
	"VMAnomaly":            &vmcontroller.VMAnomalyReconciler{},
	"VMAuth":               &vmcontroller.VMAuthReconciler{},
	"VMSingle":             &vmcontroller.VMSingleReconciler{},
	"VLAgent":              &vmcontroller.VLAgentReconciler{},
	"VLogs":                &vmcontroller.VLogsReconciler{},
	"VLSingle":             &vmcontroller.VLSingleReconciler{},
	"VLCluster":            &vmcontroller.VLClusterReconciler{},
	"VTSingle":             &vmcontroller.VTSingleReconciler{},
	"VTCluster":            &vmcontroller.VTClusterReconciler{},
	"VMAlertmanager":       &vmcontroller.VMAlertmanagerReconciler{},
	"VMAlert":              &vmcontroller.VMAlertReconciler{},
	"VMUser":               &vmcontroller.VMUserReconciler{},
	"VMRule":               &vmcontroller.VMRuleReconciler{},
	"VMAlertmanagerConfig": &vmcontroller.VMAlertmanagerConfigReconciler{},
	"VMServiceScrape":      &vmcontroller.VMServiceScrapeReconciler{},
	"VMPodScrape":          &vmcontroller.VMPodScrapeReconciler{},
	"VMProbe":              &vmcontroller.VMProbeReconciler{},
	"VMNodeScrape":         &vmcontroller.VMNodeScrapeReconciler{},
	"VMStaticScrape":       &vmcontroller.VMStaticScrapeReconciler{},
	"VMScrapeConfig":       &vmcontroller.VMScrapeConfigReconciler{},
}

func initControllers(mgr ctrl.Manager, l logr.Logger, bs *config.BaseOperatorConf) error {
	var disabledControllerNames map[string]struct{}
	if len(*disableControllerForCRD) > 0 {
		disabledControllerNames = make(map[string]struct{})
		names := strings.Split(*disableControllerForCRD, ",")
		for _, cn := range names {
			if _, ok := controllersByName[cn]; !ok {
				return fmt.Errorf("bad value=%q for flag=controller.disableReconcileFor. Expected name of reconcile controller", cn)
			}
			disabledControllerNames[cn] = struct{}{}
		}
	}
	for name, ct := range controllersByName {
		if _, ok := disabledControllerNames[name]; ok {
			l.Info("controller disabled by provided flag", "name", name, "controller.disableReconcileFor", *disableControllerForCRD)
			continue
		}
		ct.Init(mgr.GetClient(), l, mgr.GetScheme(), bs)
		if err := ct.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("cannot setup controller=%q: %w", name, err)
		}
	}
	return nil
}

func mustGetLoggerEncodingOpts() []zap.EncoderConfigOption {
	fieldRemaps := *loggerJSONFields
	if len(fieldRemaps) == 0 {
		return nil
	}

	var opts []zap.EncoderConfigOption
	fields := strings.Split(fieldRemaps, ",")
	for _, f := range fields {
		f = strings.TrimSpace(f)
		v := strings.Split(f, ":")
		if len(v) != 2 {
			errMsg := fmt.Sprintf("expected to have key:value format for fields=%q", f)
			panic(errMsg)
		}

		name, value := v[0], v[1]
		switch name {
		case "ts":
			opts = append(opts, func(ec *zapcore.EncoderConfig) {
				ec.TimeKey = value
			})
		case "level":
			opts = append(opts, func(ec *zapcore.EncoderConfig) {
				ec.LevelKey = value
			})
		case "caller":
			opts = append(opts, func(ec *zapcore.EncoderConfig) {
				ec.CallerKey = value
			})
		case "msg":
			opts = append(opts, func(ec *zapcore.EncoderConfig) {
				ec.MessageKey = value
			})
		default:
			errMsg := fmt.Sprintf("unexpected loggerJSONFields key=%q, supported keys: ts,level,caller,msg", name)
			panic(errMsg)
		}
	}
	return opts
}

func getMetricsServerMTLSOpts() ([]func(*tls.Config), error) {

	if !*mtlsEnable {
		return nil, nil
	}
	var opts []func(*tls.Config)

	var cp *x509.CertPool
	if len(*mtlsCAFile) > 0 {
		// CA file cannot be reloaded dynamically with golang std lib components
		// so load it once during init
		cp = x509.NewCertPool()
		caFile := path.Join(*tlsCertDir, *mtlsCAFile)
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read tlsCAFile=%q: %s", caFile, err)
		}
		if !cp.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("cannot parse data for tlsCAFile=%q: %s", caFile, caPEM)
		}
	}

	opts = append(opts, func(config *tls.Config) {
		config.ClientAuth = tls.RequireAndVerifyClientCert
		config.ClientCAs = cp
	})

	return opts, nil
}

func flagsAsMetrics(registry metrics.RegistererGatherer, flagSet *flag.FlagSet) {
	isSetMap := make(map[string]struct{})
	flagSet.Visit(func(f *flag.Flag) {
		isSetMap[f.Name] = struct{}{}
	})
	m := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "flag", Help: "defines provided flags for the operator"}, []string{"name", "value", "is_set"})
	flagSet.VisitAll(func(f *flag.Flag) {
		isSetStr := "false"
		if _, isSet := isSetMap[f.Name]; isSet {
			isSetStr = "true"
		}
		m.WithLabelValues(f.Name, f.Value.String(), isSetStr).Set(1)
	})
	registry.MustRegister(m)
}
