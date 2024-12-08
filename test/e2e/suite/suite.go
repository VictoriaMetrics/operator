package suite

import (
	"context"
	"os"
	"path/filepath"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/manager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"

	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// var cfg *rest.Config
var testEnv *envtest.Environment
var cancelManager context.CancelFunc
var stopped = make(chan struct{})

// GetClient returns kubernetes client for cluster connection
func GetClient() client.Client {
	err := vmv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	// prometheus operator scheme for client
	err = monitoringv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = promv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	build.AddDefaults(scheme.Scheme)
	//+kubebuilder:scaffold:scheme

	testEnv = &envtest.Environment{
		UseExistingCluster:       ptr.To(true),
		AttachControlPlaneOutput: true,
		ErrorIfCRDPathMissing:    false,
	}
	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())

	K8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(K8sClient).ToNot(BeNil())
	return K8sClient
}

// StopClient stop test env
func StopClient() {
	testEnv.Stop()
}

// InitOperatorProcess prepares operator process for usage
//
// Must be called once
func InitOperatorProcess() {
	l := zap.New(zap.WriteTo(GinkgoWriter), zap.Level(zapcore.DebugLevel))
	logf.SetLogger(l)

	By("bootstrapping test environment")

	wd, err := os.Getwd()
	Expect(err).ToNot(HaveOccurred())

	root := wd
	for {
		_, err := os.Stat(filepath.Join(root, "PROJECT"))
		Expect(err == nil || os.IsNotExist(err)).To(BeTrue())
		if err == nil {
			break
		}

		root = filepath.Dir(root)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(root, "config", "crd", "overlay"),
			filepath.Join(root, "hack", "crd", "prometheus"),
		},
		UseExistingCluster:       ptr.To(true),
		AttachControlPlaneOutput: true,
		ErrorIfCRDPathMissing:    true,
	}

	done := make(chan struct{})
	go func() {
		defer GinkgoRecover()
		defer close(done)

		var err error
		cfg, err := testEnv.Start()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg).ToNot(BeNil())

		// operator settings
		Expect(os.Setenv("VM_ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES", "true")).To(Succeed())
		Expect(os.Setenv("VM_PODWAITREADYTIMEOUT", "20s")).To(Succeed())
		Expect(os.Setenv("VM_PODWAITREADYINTERVALCHECK", "1s")).To(Succeed())
		Expect(os.Setenv("VM_APPREADYTIMEOUT", "50s")).To(Succeed())
		resourceEnvsPRefixes := []string{
			"VM_VMBACKUP_RESOURCE_REQUEST_",
			"VM_VMCLUSTERDEFAULT_VMSTORAGEDEFAULT_RESOURCE_REQUEST_",
			"VM_VMCLUSTERDEFAULT_VMSELECTDEFAULT_RESOURCE_REQUEST_",
			"VM_VMCLUSTERDEFAULT_VMINSERTDEFAULT_RESOURCE_REQUEST_",
			"VM_VMAGENTDEFAULT_RESOURCE_REQUEST_",
			"VM_VMALERTDEFAULT_RESOURCE_REQUEST_",
			"VM_VMSINGLEDEFAULT_RESOURCE_REQUEST_",
		}
		for _, minRequests := range resourceEnvsPRefixes {
			Expect(os.Setenv(minRequests+"CPU", "10m")).To(Succeed())
			Expect(os.Setenv(minRequests+"MEM", "10Mi")).To(Succeed())
		}

		// disable web servers because it fails to listen when running several test packages one after another
		// also web servers aren't very useful in tests
		os.Args = append(os.Args[:1],
			"--metrics-bind-address", "0",
			"--pprof-addr", "0",
			"--health-probe-bind-address", "0",
			"--controller.maxConcurrentReconciles", "30",
		)
		ctx, cancel := context.WithCancel(context.Background())
		go func(ctx context.Context) {
			defer GinkgoRecover()
			err := manager.RunManager(ctx)
			close(stopped)
			Expect(err).NotTo(HaveOccurred())
			Expect(testEnv.Stop()).ToNot(HaveOccurred())
		}(ctx)
		cancelManager = cancel
	}()

	Eventually(done, 60, 1).Should(BeClosed())
}

// ShutdownOperatorProcess stops operator process
// and cleanup resources
func ShutdownOperatorProcess() {
	By("tearing down the test environment")
	cancelManager()
	Eventually(stopped, 60, 2).Should(BeClosed())
}
