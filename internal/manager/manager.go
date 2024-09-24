package manager

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	vmcontroller "github.com/VictoriaMetrics/operator/internal/controller/operator"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
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
	// +kubebuilder:scaffold:imports
)

const defaultMetricsAddr = ":8080"
const defaultWebhookPort = 9443

var (
	startTime  = time.Now()
	appVersion = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "vm_app_version", Help: "version of application", ConstLabels: map[string]string{"version": buildinfo.Version}}, func() float64 {
		return 1.0
	})
	uptime = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "vm_app_uptime_seconds", Help: "uptime"}, func() float64 {
		return time.Since(startTime).Seconds()
	})
	startedAt = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "vm_app_start_timestamp", Help: "unixtimestamp"}, func() float64 {
		return float64(startTime.Unix())
	})
	scheme              = runtime.NewScheme()
	setupLog            = ctrl.Log.WithName("setup")
	leaderElect         = flag.Bool("leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	enableWebhooks      = flag.Bool("webhook.enable", false, "adds webhook server, you must mount cert and key or use cert-manager")
	webhookPort         = flag.Int("webhook.port", defaultWebhookPort, "port to start webhook server on")
	disableCRDOwnership = flag.Bool("controller.disableCRDOwnership", false, "disables CRD ownership add to cluster wide objects, must be disabled for clusters, lower than v1.16.0")
	webhooksDir         = flag.String("webhook.certDir", "/tmp/k8s-webhook-server/serving-certs/", "root directory for webhook cert and key")
	webhookCertName     = flag.String("webhook.certName", "tls.crt", "name of webhook server Tls certificate inside tls.certDir")
	webhookKeyName      = flag.String("webhook.keyName", "tls.key", "name of webhook server Tls key inside tls.certDir")
	tlsEnable           = flag.Bool("tls.enable", false, "enables secure tls (https) for metrics webserver.")
	tlsCertsDir         = flag.String("tls.certDir", "/tmp/k8s-metrics-server/serving-certs", "root directory for metrics webserver cert, key and mTLS CA.")
	tlsCertName         = flag.String("tls.certName", "tls.crt", "name of metric server Tls certificate inside tls.certDir. Default - ")
	tlsKeyName          = flag.String("tls.keyName", "tls.key", "name of metric server Tls key inside tls.certDir. Default - tls.key")
	mtlsEnable          = flag.Bool("mtls.enable", false, "Whether to require valid client certificate for https requests to the corresponding -metrics-bind-address. This flag works only if -tls.enable flag is set. ")
	mtlsCAFile          = flag.String("mtls.CAName", "clietCA.crt", "Optional name of TLS Root CA for verifying client certificates at the corresponding -metrics-bind-address when -mtls.enable is enabled. "+
		"By default the host system TLS Root CA is used for client certificate verification. ")
	metricsBindAddress            = flag.String("metrics-bind-address", defaultMetricsAddr, "The address the metric endpoint binds to.")
	pprofAddr                     = flag.String("pprof-addr", ":8435", "The address for pprof/debug API. Empty value disables server")
	probeAddr                     = flag.String("health-probe-bind-address", ":8081", "The address the probes (health, ready) binds to.")
	defaultKubernetesMinorVersion = flag.Uint64("default.kubernetesVersion.minor", 21, "Minor version of kubernetes server, if operator cannot parse actual kubernetes response")
	defaultKubernetesMajorVersion = flag.Uint64("default.kubernetesVersion.major", 1, "Major version of kubernetes server, if operator cannot parse actual kubernetes response")
	printDefaults                 = flag.Bool("printDefaults", false, "print all variables with their default values and exit")
	printFormat                   = flag.String("printFormat", "table", "output format for --printDefaults. Can be table, json, yaml or list")
	promCRDResyncPeriod           = flag.Duration("controller.prometheusCRD.resyncPeriod", 0, "Configures resync period for prometheus CRD converter. Disabled by default")
	clientQPS                     = flag.Int("client.qps", 5, "defines K8s client QPS")
	clientBurst                   = flag.Int("client.burst", 10, "defines K8s client burst")
	wasCacheSynced                = uint32(0)
	disableCacheForObjects        = flag.String("controller.disableCacheFor", "", "disables client for cache for API resources. Supported objects - namespace,pod,secret,configmap,deployment,statefulset")
	disableSecretKeySpaceTrim     = flag.Bool("disableSecretKeySpaceTrim", false, "disables trim of space at Secret/Configmap value content. It's a common mistake to put new line to the base64 encoded secret value.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	utilruntime.Must(metav1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(promv1.AddToScheme(scheme))
	build.AddDefaults(scheme)
	// +kubebuilder:scaffold:scheme
}

func RunManager(ctx context.Context) error {
	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	opts := zap.Options{
		StacktraceLevel: zapcore.PanicLevel,
	}
	opts.BindFlags(flag.CommandLine)
	vmcontroller.BindFlags(flag.CommandLine)
	flag.Parse()

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
	sink := zap.New(zap.UseFlagOptions(&opts)).GetSink()
	l := logger.New(sink)
	logf.SetLogger(l)

	buildinfo.Init()

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

	r := metrics.Registry
	r.MustRegister(appVersion, uptime, startedAt)
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

	config := ctrl.GetConfigOrDie()
	config.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(*clientQPS), *clientBurst)

	co, err := getClientCacheOptions(*disableCacheForObjects)
	if err != nil {
		return fmt.Errorf("cannot build cache options for manager: %w", err)
	}
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Logger: ctrl.Log.WithName("manager"),
		Scheme: scheme,
		Metrics: metricsserver.Options{
			SecureServing: *tlsEnable,
			BindAddress:   *metricsBindAddress,
			CertDir:       *tlsCertsDir,
			CertName:      *tlsCertName,
			KeyName:       *tlsKeyName,
			TLSOpts:       configureTLS(),
			ExtraHandlers: map[string]http.Handler{},
		},
		HealthProbeBindAddress: *probeAddr,
		PprofBindAddress:       *pprofAddr,
		ReadinessEndpointName:  "/ready",
		LivenessEndpointName:   "/health",
		// port for webhook
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:     *webhookPort,
			CertDir:  *webhooksDir,
			CertName: *webhookCertName,
			KeyName:  *webhookKeyName,
		}),
		LeaderElection:   *leaderElect,
		LeaderElectionID: "57410f0d.victoriametrics.com",
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

	if *enableWebhooks {
		if err = addWebhooks(mgr); err != nil {
			l.Error(err, "cannot register webhooks")
			return err
		}
	}
	vmv1beta1.SetLabelAndAnnotationPrefixes(baseConfig.FilterChildLabelPrefixes, baseConfig.FilterChildAnnotationPrefixes)

	if err = (&vmcontroller.VMAgentReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAgent"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAgent")
		return err
	}
	if err = (&vmcontroller.VMAlertReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAlert"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlert")
		return err
	}
	if err = (&vmcontroller.VMAlertmanagerReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAlertmanager"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlertmanager")
		return err
	}
	if err = (&vmcontroller.VMPodScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMPodScrape"),
		OriginScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMPodScrape")
		return err
	}
	if err = (&vmcontroller.VMRuleReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMRule"),
		OriginScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMRule")
		return err
	}
	if err = (&vmcontroller.VMServiceScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMServiceScrape"),
		OriginScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMServiceScrape")
		return err
	}
	if err = (&vmcontroller.VMSingleReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMSingle"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMSingle")
		return err
	}
	if err = (&vmcontroller.VLogsReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VLogs"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VLogs")
		return err
	}
	if err = (&vmcontroller.VMClusterReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMCluster"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMCluster")
		return err
	}
	if err = (&vmcontroller.VMProbeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMProbe"),
		OriginScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMProbe")
		return err
	}
	if err = (&vmcontroller.VMNodeScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMNodeScrape"),
		OriginScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMNodeScrape")
		return err
	}
	if err = (&vmcontroller.VMStaticScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMStaticScrape"),
		OriginScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMStaticScrape")
		return err
	}
	if err = (&vmcontroller.VMScrapeConfigReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMScrapeConfig"),
		OriginScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMScrapeConfig")
		return err
	}

	if err = (&vmcontroller.VMAuthReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAuthReconciler"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAuth")
		return err
	}

	if err = (&vmcontroller.VMUserReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMUserReconciler"),
		OriginScheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMUser")
		return err
	}
	if err = (&vmcontroller.VMAlertmanagerConfigReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAlertmanagerConfigReconciler"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlertmanager")
		return err
	}
	// +kubebuilder:scaffold:builder
	setupLog.Info("starting vmconverter clients")

	baseClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "cannot build promClient")
		return err
	}

	k8stools.SetSpaceTrim(*disableSecretKeySpaceTrim)
	k8sServerVersion, err := baseClient.ServerVersion()
	if err != nil {
		return fmt.Errorf("cannot get kubernetes server version: %w", err)
	}
	if err := k8stools.SetKubernetesVersionWithDefaults(k8sServerVersion, *defaultKubernetesMinorVersion, *defaultKubernetesMajorVersion); err != nil {
		// log error and do nothing, because we are using sane default values.
		setupLog.Error(err, "cannot parse kubernetes version, using default flag values")
	}

	setupLog.Info("using kubernetes server version", "version", k8sServerVersion.String())
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
		setupLog.Error(err, "problem running manager")
		return err
	}

	setupLog.Info("gracefully stopped")
	return nil
}

func addWebhooks(mgr ctrl.Manager) error {
	f := func(objs []client.Object) error {
		var err error
		for _, obj := range objs {
			if err = ctrl.NewWebhookManagedBy(mgr).For(obj).Complete(); err != nil {
				return err
			}
		}
		return nil
	}
	return f([]client.Object{
		&vmv1beta1.VMAgent{},
		&vmv1beta1.VMAlert{},
		&vmv1beta1.VMSingle{},
		&vmv1beta1.VMCluster{},
		&vmv1beta1.VLogs{},
		&vmv1beta1.VMAlertmanager{},
		&vmv1beta1.VMAlertmanagerConfig{},
		&vmv1beta1.VMAuth{},
		&vmv1beta1.VMUser{},
		&vmv1beta1.VMRule{},
	})
}

func configureTLS() []func(*tls.Config) {
	var opts []func(*tls.Config)
	if *mtlsEnable {
		if !*tlsEnable {
			panic("-tls.enable flag must be set before using mtls.enable")
		}
		opts = append(opts, func(cfg *tls.Config) {
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
			if *mtlsCAFile != "" {
				cp := x509.NewCertPool()
				caFile := path.Join(*tlsCertsDir, *mtlsCAFile)
				caPEM, err := os.ReadFile(caFile)
				if err != nil {
					panic(fmt.Sprintf("cannot read tlsCAFile=%q: %s", caFile, err))
				}
				if !cp.AppendCertsFromPEM(caPEM) {
					panic(fmt.Sprintf("cannot parse data for tlsCAFile=%q: %s", caFile, caPEM))
				}
				cfg.ClientCAs = cp
			}
		})
	}
	return opts
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

var cacheClientObjectsByName = map[string]client.Object{
	"secret":      &corev1.Secret{},
	"configmap":   &corev1.ConfigMap{},
	"namespace":   &corev1.Namespace{},
	"pod":         &corev1.Pod{},
	"deployment":  &appsv1.Deployment{},
	"statefulset": &appsv1.StatefulSet{},
}
