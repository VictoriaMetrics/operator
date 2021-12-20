package manager

import (
	"context"
	"flag"
	"net/http"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/httpserver"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers"
	"github.com/VictoriaMetrics/operator/controllers/factory/crd"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/prometheus-operator/prometheus-operator/pkg/client/versioned"
	"github.com/spf13/pflag"
	v12 "k8s.io/api/apps/v1"
	"k8s.io/api/autoscaling/v2beta2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	scheme               = runtime.NewScheme()
	setupLog             = ctrl.Log.WithName("setup")
	enableLeaderElection = flag.Bool("enable-leader-election", false, "Enable leader election for controller manager. "+
		"Enabling this will ensure there is only one active controller manager.")
	enableWebhooks      = flag.Bool("webhook.enable", false, "adds webhook server, you must mount cert and key or use cert-manager")
	disableCRDOwnership = flag.Bool("controller.disableCRDOwnership", false, "disables CRD ownership add to cluster wide objects, must be disabled for clusters, lower than v1.16.0")
	webhooksDir         = flag.String("webhook.certDir", "/tmp/k8s-webhook-server/serving-certs/", "root directory for webhook cert and key")
	webhookCertName     = flag.String("webhook.certName", "tls.crt", "name of webhook server Tls certificate inside tls.certDir")
	webhookKeyName      = flag.String("webhook.keyName", "tls.key", "name of webhook server Tls key inside tls.certDir")
	metricsAddr         = flag.String("metrics-addr", ":8080", "The address the metric endpoint binds to.")
	listenAddr          = flag.String("http.listenAddr", ":8435", "http server listen addr - serves victoria-metrics http server + metrics.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(victoriametricsv1beta1.AddToScheme(scheme))
	utilruntime.Must(metav1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme

}

func RunManager(ctx context.Context) error {

	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()

	zap.UseFlagOptions(&opts)
	logger := zap.New(zap.UseFlagOptions(&opts))
	logf.SetLogger(logger)

	buildinfo.Init()

	// Use a zap logr.Logger implementation. If none of the zap
	// flags are configured (or if the zap flag set is not being
	// used), this defaults to a production zap logger.
	//
	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	klog.SetLogger(logger)
	ctrl.SetLogger(logger)

	setupLog.Info("Registering Components.")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                scheme,
		MetricsBindAddress:    *metricsAddr,
		Port:                  9443,
		LeaderElection:        *enableLeaderElection,
		LeaderElectionID:      "57410f0d.victoriametrics.com",
		ClientDisableCacheFor: []client.Object{&v1.Secret{}, &v1.ConfigMap{}, &v1.Pod{}, &v12.Deployment{}, &v12.StatefulSet{}, &v2beta2.HorizontalPodAutoscaler{}},
		Namespace:             config.MustGetWatchNamespace(),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}
	opNs := config.MustGetWatchNamespace()
	if opNs != "" {
		setupLog.Info("operator is running in single namespace mode", "namespace", opNs)
	}

	if !*disableCRDOwnership {
		initC, err := client.New(mgr.GetConfig(), client.Options{Scheme: scheme})
		if err != nil {
			return err
		}
		logger.Info("starting CRD ownership controller")
		if err := crd.Init(ctx, initC); err != nil {
			setupLog.Error(err, "unable to init crd data")
			return err
		}
	}

	if *enableWebhooks {
		if err = addWebhooks(mgr); err != nil {
			logger.Error(err, "cannot register webhooks")
			return err
		}
	}

	if err = (&controllers.VMAgentReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMAgent"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAgent")
		return err
	}
	if err = (&controllers.VMAlertReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMAlert"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlert")
		return err
	}
	if err = (&controllers.VMAlertmanagerReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMAlertmanager"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlertmanager")
		return err
	}
	if err = (&controllers.VMPodScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMPodScrape"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMPodScrape")
		return err
	}
	if err = (&controllers.VMRuleReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMRule"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMRule")
		return err
	}
	if err = (&controllers.VMServiceScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMServiceScrape"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMServiceScrape")
		return err
	}
	if err = (&controllers.VMSingleReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMSingle"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMSingle")
		return err
	}

	if err = (&controllers.VMClusterReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMCluster"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMCluster")
		return err
	}
	if err = (&controllers.VMProbeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMProbe"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMProbe")
		return err
	}
	if err = (&controllers.VMNodeScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMNodeScrape"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMNodeScrape")
		return err
	}
	if err = (&controllers.VMStaticScrapeReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMStaticScrape"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMStaticScrape")
		return err
	}

	if err = (&controllers.VMAuthReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMAuthReconciler"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAuth")
		return err
	}

	if err = (&controllers.VMUserReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMUserReconciler"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMUser")
		return err
	}
	if err = (&controllers.VMAlertmanagerConfigReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VMAlertmanagerConfigReconciler"),
		OriginScheme: mgr.GetScheme(),
		BaseConf:     config.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlertmanager")
		return err
	}
	// +kubebuilder:scaffold:builder
	setupLog.Info("starting vmconverter clients")

	prom, err := versioned.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "cannot build promClient")
		return err
	}
	converterController := controllers.NewConverterController(prom, mgr.GetClient(), config.MustGetBaseConfig())

	if err := mgr.Add(converterController); err != nil {
		setupLog.Error(err, "cannot add runnable")
		return err
	}
	if len(*listenAddr) > 0 {
		go httpserver.Serve(*listenAddr, requestHandler)
	}
	if err := controllers.StartWatchForVMUserSecretRefs(ctx, mgr.GetClient(), mgr.GetConfig()); err != nil {
		return err
	}
	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}
	if len(*listenAddr) > 0 {
		if err := httpserver.Stop(*listenAddr); err != nil {
			setupLog.Error(err, "failed to gracefully stop HTTP server")
		}
	}

	setupLog.Info("gracefully stopped")
	return nil

}

func addWebhooks(mgr ctrl.Manager) error {
	srv := mgr.GetWebhookServer()
	srv.CertDir = *webhooksDir
	srv.CertName = *webhookCertName
	srv.KeyName = *webhookKeyName

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
		&victoriametricsv1beta1.VMAgent{},
		&victoriametricsv1beta1.VMAlert{},
		&victoriametricsv1beta1.VMSingle{},
		&victoriametricsv1beta1.VMCluster{},
		&victoriametricsv1beta1.VMAlertmanager{},
		&victoriametricsv1beta1.VMAlertmanagerConfig{},
		&victoriametricsv1beta1.VMAuth{},
		&victoriametricsv1beta1.VMUser{},
	})

}

func requestHandler(w http.ResponseWriter, r *http.Request) bool {
	return false
}
