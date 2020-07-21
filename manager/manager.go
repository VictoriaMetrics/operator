package manager

import (
	"context"
	"flag"
	"github.com/VictoriaMetrics/operator/conf"
	"github.com/coreos/prometheus-operator/pkg/client/versioned"
	"github.com/operator-framework/operator-sdk/pkg/log/zap"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(victoriametricsv1beta1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func RunManager(ctx context.Context) error {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	pflag.CommandLine.AddFlagSet(zap.FlagSet())

	// Add flags registered by imported packages (e.g. glog and
	// controller-runtime)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	pflag.Parse()

	// Use a zap logr.Logger implementation. If none of the zap
	// flags are configured (or if the zap flag set is not being
	// used), this defaults to a production zap logger.
	//
	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	logf.SetLogger(zap.Logger())
	flag.Parse()

	ctrl.SetLogger(zap.Logger())

	setupLog.Info("Registering Components.")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "57410f0d.victoriametrics.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	if err = (&controllers.VMAgentReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("VMAgent"),
		Scheme:   mgr.GetScheme(),
		BaseConf: conf.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAgent")
		return err
	}
	if err = (&controllers.VMAlertReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("VMAlert"),
		Scheme:   mgr.GetScheme(),
		BaseConf: conf.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlert")
		return err
	}
	if err = (&controllers.VMAlertmanagerReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("VMAlertmanager"),
		Scheme:   mgr.GetScheme(),
		BaseConf: conf.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMAlertmanager")
		return err
	}
	if err = (&controllers.VMPodScrapeReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("VMPodScrape"),
		Scheme:   mgr.GetScheme(),
		BaseConf: conf.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMPodScrape")
		return err
	}
	if err = (&controllers.VMRuleReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("VMRule"),
		Scheme:   mgr.GetScheme(),
		BaseConf: conf.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMRule")
		return err
	}
	if err = (&controllers.VMServiceScrapeReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("VMServiceScrape"),
		Scheme:   mgr.GetScheme(),
		BaseConf: conf.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMServiceScrape")
		return err
	}
	if err = (&controllers.VMSingleReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("VMSingle"),
		Scheme:   mgr.GetScheme(),
		BaseConf: conf.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMSingle")
		return err
	}

	if err = (&controllers.VMClusterReconciler{
		Client:   mgr.GetClient(),
		Log:      ctrl.Log.WithName("controllers").WithName("VMCluster"),
		Scheme:   mgr.GetScheme(),
		BaseConf: conf.MustGetBaseConfig(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VMCluster")
		return err
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting vmconverter clients")

	prom, err := versioned.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "cannot build promClient")
		return err
	}
	converterController := controllers.NewConvertorController(prom, mgr.GetClient())

	errG := &errgroup.Group{}
	converterController.Run(ctx, errG, conf.MustGetBaseConfig())
	setupLog.Info("vmconverter was started")

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx.Done()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}
	setupLog.Info("waiting for converter stop")
	//safe to ignore
	err = errG.Wait()
	if err != nil {
		return err
	}
	setupLog.Info("gracefully stopped")
	return nil

}
