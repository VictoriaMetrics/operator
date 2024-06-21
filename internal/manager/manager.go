package manager

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/factory/logger"
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
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

var (
	managerFlags = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	startTime    = time.Now()
	appVersion   = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "vm_app_version", Help: "version of application", ConstLabels: map[string]string{"version": buildinfo.Version}}, func() float64 {
		return 1.0
	})
	uptime = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "vm_app_uptime_seconds", Help: "uptime"}, func() float64 {
		return time.Since(startTime).Seconds()
	})
	startedAt = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "vm_app_start_timestamp", Help: "unixtimestamp"}, func() float64 {
		return float64(startTime.Unix())
	})
	scheme                        = runtime.NewScheme()
	setupLog                      = ctrl.Log.WithName("setup")
	leaderElect                   = managerFlags.Bool("leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	enableWebhooks                = managerFlags.Bool("webhook.enable", false, "adds webhook server, you must mount cert and key or use cert-manager")
	disableCRDOwnership           = managerFlags.Bool("controller.disableCRDOwnership", false, "disables CRD ownership add to cluster wide objects, must be disabled for clusters, lower than v1.16.0")
	webhooksDir                   = managerFlags.String("webhook.certDir", "/tmp/k8s-webhook-server/serving-certs/", "root directory for webhook cert and key")
	webhookCertName               = managerFlags.String("webhook.certName", "tls.crt", "name of webhook server Tls certificate inside tls.certDir")
	webhookKeyName                = managerFlags.String("webhook.keyName", "tls.key", "name of webhook server Tls key inside tls.certDir")
	metricsBindAddress            = managerFlags.String("metrics-bind-address", defaultMetricsAddr, "The address the metric endpoint binds to.")
	pprofAddr                     = managerFlags.String("pprof-addr", "localhost:8435", "The address for pprof/debug API. Empty value disables server")
	probeAddr                     = managerFlags.String("http.readyListenAddr", "localhost:8081", "The address the probes (health, ready) binds to.")
	defaultKubernetesMinorVersion = managerFlags.Uint64("default.kubernetesVersion.minor", 21, "Minor version of kubernetes server, if operator cannot parse actual kubernetes response")
	defaultKubernetesMajorVersion = managerFlags.Uint64("default.kubernetesVersion.major", 1, "Major version of kubernetes server, if operator cannot parse actual kubernetes response")
	printDefaults                 = managerFlags.Bool("printDefaults", false, "print all variables with their default values and exit")
	printFormat                   = managerFlags.String("printFormat", "table", "output format for --printDefaults. Can be table, json, yaml or list")
	promCRDResyncPeriod           = managerFlags.Duration("controller.prometheusCRD.resyncPeriod", 0, "Configures resync period for prometheus CRD converter. Disabled by default")
	wasCacheSynced                = uint32(0)
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(vmv1beta1.AddToScheme(scheme))
	utilruntime.Must(metav1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(promv1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func RunManager(ctx context.Context) error {
	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	opts := zap.Options{}
	opts.BindFlags(managerFlags)
	controller.BindFlags(managerFlags)

	pflag.CommandLine.AddGoFlagSet(managerFlags)

	pflag.Parse()

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
		setupLog.Info("operator configured with watching for subset of namespaces=%q, cluster wide access is disabled", strings.Join(watchNss, ","))
		watchNsCacheByName = make(map[string]cache.Config)
		for _, ns := range watchNss {
			watchNsCacheByName[ns] = cache.Config{}
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Logger:                 ctrl.Log.WithName("manager"),
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: *metricsBindAddress},
		HealthProbeBindAddress: *probeAddr,
		ReadinessEndpointName:  "/ready",
		LivenessEndpointName:   "/health",
		PprofBindAddress:       *pprofAddr,
		// port for webhook
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:     9443,
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
			Cache: &client.CacheOptions{DisableFor: []client.Object{
				&v1.Secret{}, &v1.ConfigMap{}, &v1.Pod{},
				&v1.Namespace{},
			}},
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

	if err = (&controller.VMAgentReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAgent"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAgent")
		return err
	}
	if err = (&controller.VMAlertReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAlert"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlert")
		return err
	}
	if err = (&controller.VMAlertmanagerReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAlertmanager"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlertmanager")
		return err
	}
	if err = (&controller.VMPodScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMPodScrape"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMPodScrape")
		return err
	}
	if err = (&controller.VMRuleReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMRule"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMRule")
		return err
	}
	if err = (&controller.VMServiceScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMServiceScrape"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMServiceScrape")
		return err
	}
	if err = (&controller.VMSingleReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMSingle"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMSingle")
		return err
	}

	if err = (&controller.VMClusterReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMCluster"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMCluster")
		return err
	}
	if err = (&controller.VMProbeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMProbe"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMProbe")
		return err
	}
	if err = (&controller.VMNodeScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMNodeScrape"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMNodeScrape")
		return err
	}
	if err = (&controller.VMStaticScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMStaticScrape"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMStaticScrape")
		return err
	}
	if err = (&controller.VMScrapeConfigReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMScrapeConfig"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMScrapeConfig")
		return err
	}

	if err = (&controller.VMAuthReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMAuthReconciler"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAuth")
		return err
	}

	if err = (&controller.VMUserReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controller").WithName("VMUserReconciler"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     baseConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMUser")
		return err
	}
	if err = (&controller.VMAlertmanagerConfigReconciler{
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
	converterController, err := controller.NewConverterController(ctx, baseClient, wc, *promCRDResyncPeriod, baseConfig)
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
		&vmv1beta1.VMAlertmanager{},
		&vmv1beta1.VMAlertmanagerConfig{},
		&vmv1beta1.VMAuth{},
		&vmv1beta1.VMUser{},
		&vmv1beta1.VMRule{},
	})
}
