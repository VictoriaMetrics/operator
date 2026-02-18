package suite

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
	"github.com/onsi/gomega/format"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	promv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/manager"
)

var (
	testEnv       *envtest.Environment
	cancelManager context.CancelFunc
	stopped       = make(chan struct{})
)

func GetClient(data []byte) client.Client {
	var cfg rest.Config
	dec := gob.NewDecoder(bytes.NewReader(data))
	Expect(dec.Decode(&cfg)).To(Succeed())
	// operator settings
	envs := map[string]string{
		"VM_CONTAINERREGISTRY":                           "quay.io",
		"VM_VMALERTMANAGER_ALERTMANAGERDEFAULTBASEIMAGE": "prometheus/alertmanager",
		"VM_ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES":   "true",
		"VM_GATEWAY_API_ENABLED":                         "true",
		"VM_PODWAITREADYTIMEOUT":                         "20s",
		"VM_PODWAITREADYINTERVALCHECK":                   "1s",
		"VM_APPREADYTIMEOUT":                             "50s",
	}
	resourceEnvsPrefixes := []string{
		"VMBACKUP",
		"VMCLUSTERDEFAULT_VMSTORAGEDEFAULT",
		"VMCLUSTERDEFAULT_VMSELECTDEFAULT",
		"VMCLUSTERDEFAULT_VMINSERTDEFAULT",
		"VLCLUSTERDEFAULT_VLSTORAGEDEFAULT",
		"VLCLUSTERDEFAULT_VLSELECTDEFAULT",
		"VLCLUSTERDEFAULT_VLINSERTDEFAULT",
		"VTCLUSTERDEFAULT_STORAGE",
		"VTCLUSTERDEFAULT_SELECT",
		"VTCLUSTERDEFAULT_INSERT",
		"VMAGENTDEFAULT",
		"VMAUTHDEFAULT",
		"VMALERTDEFAULT",
		"VMSINGLEDEFAULT",
		"VLAGENTDEFAULT",
		"VLSINGLEDEFAULT",
		"VTSINGLEDEFAULT",
	}
	resources := map[string]string{
		"CPU": "10m",
		"MEM": "20Mi",
	}
	for _, prefix := range resourceEnvsPrefixes {
		for _, t := range []string{"LIMIT", "REQUEST"} {
			for rn, rv := range resources {
				envName := fmt.Sprintf("VM_%s_RESOURCE_%s_%s", prefix, t, rn)
				envs[envName] = rv
			}
		}
	}

	for en, ev := range envs {
		Expect(os.Setenv(en, ev)).ToNot(HaveOccurred())
	}
	Expect(vmv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(vmv1beta1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(vmv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(monitoringv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(promv1alpha1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(gwapiv1.Install(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(apiextensionsv1.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	build.AddDefaults(scheme.Scheme)
	k8sClient, err := client.New(&cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())
	return k8sClient
}

// InitOperatorProcess prepares operator process for usage, must be called once
func InitOperatorProcess(extraNamespaces ...string) []byte {
	format.MaxLength = 50000
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

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(root, "config", "crd", "overlay"),
			filepath.Join(root, "hack", "crd", "prometheus"),
			filepath.Join(root, "hack", "crd", "gatewayapi"),
		},
		UseExistingCluster:       ptr.To(true),
		AttachControlPlaneOutput: true,
		ErrorIfCRDPathMissing:    true,
	}

	suiteConfig, _ := GinkgoConfiguration()
	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	Expect(enc.Encode(cfg)).To(Succeed())
	k8sClient := GetClient(buf.Bytes())
	Expect(isLocalHost(cfg.Host)).To(BeTrue())
	namespaces := extraNamespaces
	for i := range suiteConfig.ParallelTotal {
		namespaces = append(namespaces, fmt.Sprintf("default-%d", i+1))
	}
	for _, ns := range namespaces {
		testNamespace := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}
		err := k8sClient.Create(context.Background(), &testNamespace)
		Expect(err == nil || k8serrors.IsAlreadyExists(err)).To(BeTrue(), "got unexpected namespace creation error: %v", err)
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
	}(ctx)
	cancelManager = cancel
	return buf.Bytes()
}

// ShutdownOperatorProcess stops operator process
// and cleanup resources
func ShutdownOperatorProcess() {
	By("tearing down the test environment")
	cancelManager()
	Eventually(stopped, 60, 2).Should(BeClosed())
	Expect(testEnv.Stop()).ToNot(HaveOccurred())

}

func isLocalHost(host string) bool {
	ips, err := getLocalIPs()
	if err != nil {
		return false
	}

	for _, ip := range ips {
		if strings.Contains(host, ip) {
			return true
		}
	}
	return false
}

// GetLocalIPs returns a list of local IP addresses
func getLocalIPs() ([]string, error) {
	var ips []string

	// Add loopback IPs
	loopbackIPs := []string{"127.0.0.1", "::1"}
	ips = append(ips, loopbackIPs...)

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, iface := range interfaces {
		// Skip loopback, down, or interfaces without addresses
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			// Check if the address is IP
			if v, ok := addr.(*net.IPNet); ok {
				// Skip loopback and link-local addresses
				if v.IP.IsLoopback() || v.IP.IsLinkLocalUnicast() || v.IP.IsLinkLocalMulticast() {
					continue
				}
				// Only include IPv4 addresses
				if v.IP.To4() != nil {
					ips = append(ips, v.IP.String())
				}
			}
		}
	}

	return ips, nil
}
