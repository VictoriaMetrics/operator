package deploy

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/VictoriaMetrics/operator/internal/podutil"
	"github.com/VictoriaMetrics/operator/test/utils"
)

// overlappingProcessMetrics: labelless go_*/process_* metrics emitted by both controller-runtime's default collectors and vmMetricsFilterProvider's vmmetrics.WritePrometheus; each must appear exactly once per scrape.
var overlappingProcessMetrics = []string{
	"go_goroutines",
	"go_threads",
	"go_info",
	"process_cpu_seconds_total",
	"process_resident_memory_bytes",
	"process_virtual_memory_bytes",
	"process_start_time_seconds",
}

// vmOnlyMetrics: names emitted only by vmmetrics.WritePrometheus, asserting VM enrichment actually ran, not just that nothing is duplicated.
var vmOnlyMetrics = []string{
	"process_io_read_bytes_total",
	"process_cpu_seconds_system_total",
	"go_cpu_count",
	"go_info_ext",
}

var (
	_ = Describe("operator in-cluster deployment", Ordered, func() {
		ns := "vm"
		BeforeAll(func() {
			// Run cert-manager install and CRD install in parallel — they are independent.
			var certErr, crdErr error
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				By("installing the cert-manager")
				certErr = utils.InstallCertManager()
			}()
			go func() {
				defer wg.Done()
				By("installing CRDs")
				cmd := exec.Command("make", "install")
				_, crdErr = utils.Run(cmd)
			}()
			wg.Wait()
			Expect(certErr).NotTo(HaveOccurred())
			Expect(crdErr).NotTo(HaveOccurred())

			By("creating manager namespace")
			cmd := exec.Command("kubectl", "create", "ns", ns)
			_, _ = utils.Run(cmd)
		})

		AfterAll(func() {
			// Delete ValidatingWebhookConfiguration first - prevents operator from hanging
			By("removing webhook configuration")
			cmd := exec.Command("kubectl", "delete", "validatingwebhookconfiguration",
				"vm-validating-webhook-configuration", "--ignore-not-found=true",
			)
			_, _ = utils.Run(cmd)

			By("undeploying the operator")
			cmd = exec.Command("make", "undeploy-kind",
				fmt.Sprintf("NAMESPACE=%s", ns),
			)
			_, _ = utils.Run(cmd)

			By("uninstalling the cert-manager bundle")
			utils.UninstallCertManager()

			By("removing manager namespace")
			cmd = exec.Command("kubectl", "delete", "ns", ns)
			_, _ = utils.Run(cmd)
		})

		Context("Operator", func() {
			It("should run successfully", func() {
				By("deploying the operator")
				var err error
				EventuallyWithOffset(1, func() error {
					cmd := exec.Command("make", "deploy-kind-no-build",
						fmt.Sprintf("NAMESPACE=%s", ns),
					)
					_, err = utils.Run(cmd)
					return err
				}, 2*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

				By("waiting for webhook certificate to be issued")
				certCmd := exec.Command("kubectl", "wait", "certificate/vm-serving-cert",
					"--for=condition=Ready",
					"--namespace", ns,
					"--timeout", "2m",
				)
				_, err = utils.Run(certCmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())

				By("validating that the operator pod is running as expected")
				verifyControllerUp := func() error {
					cmd := exec.Command("kubectl", "get",
						"pods", "-l", "control-plane=vm-operator",
						"-o", "go-template={{ range .items }}"+
							"{{ if not .metadata.deletionTimestamp }}"+
							"{{ .metadata.name }}"+
							"{{ \"\\n\" }}{{ end }}{{ end }}",
						"-n", ns,
					)
					podOutput, err := utils.Run(cmd)
					if err != nil {
						return err
					}
					podNames := utils.GetNonEmptyLines(string(podOutput))
					if len(podNames) != 1 {
						return fmt.Errorf("expect 1 operator pods running, but got %d", len(podNames))
					}
					operatorPodName := podNames[0]
					if !strings.Contains(operatorPodName, "operator") {
						return fmt.Errorf("pod name %q doesn't contain 'operator'", operatorPodName)
					}

					cmd = exec.Command("kubectl", "get",
						"pods", operatorPodName, "-o", "jsonpath={.status.phase}",
						"-n", ns,
					)
					status, err := utils.Run(cmd)
					if err != nil {
						return err
					}
					if string(status) != "Running" {
						return fmt.Errorf("operator pod in %s status", status)
					}
					return nil
				}
				EventuallyWithOffset(1, verifyControllerUp, 3*time.Minute, time.Second).ShouldNot(HaveOccurred())
			})

			It("should not expose duplicate metric families on /metrics", func() {
				By("locating the operator pod's IP")
				var podIP string
				EventuallyWithOffset(1, func() error {
					cmd := exec.Command("kubectl", "get", "pods",
						"-l", "control-plane=vm-operator",
						"-n", ns,
						"-o", "jsonpath={.items[0].status.podIP}",
					)
					out, err := utils.Run(cmd)
					if err != nil {
						return err
					}
					podIP = strings.TrimSpace(string(out))
					if podIP == "" {
						return fmt.Errorf("operator pod has no IP assigned yet")
					}
					return nil
				}, time.Minute, time.Second).ShouldNot(HaveOccurred())

				// empty Dimension requests every sample regardless of labels, so a duplicate surfaces as len(values[name]) > 1.
				By("scraping the operator's own /metrics endpoint and checking for duplicates")
				queries := make([]podutil.MetricQuery, 0, len(overlappingProcessMetrics)+len(vmOnlyMetrics))
				for _, name := range overlappingProcessMetrics {
					queries = append(queries, podutil.MetricQuery{Name: name})
				}
				for _, name := range vmOnlyMetrics {
					queries = append(queries, podutil.MetricQuery{Name: name})
				}

				metricsURL := fmt.Sprintf("http://%s:8080/metrics", podIP)
				var values map[string]map[string]float64
				EventuallyWithOffset(1, func() error {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					var err error
					values, err = podutil.FetchMetricsValues(ctx, http.DefaultClient, metricsURL, queries)
					return err
				}, time.Minute, time.Second).ShouldNot(HaveOccurred())

				for _, name := range overlappingProcessMetrics {
					ExpectWithOffset(1, values[name]).To(HaveLen(1),
						"metric %q is duplicated: controller-runtime's default collector and VictoriaMetrics/metrics both emitting it", name)
				}

				// vmOnlyMetrics can't pass vacuously if VM enrichment stops being appended entirely.
				By("checking that VictoriaMetrics' own metrics are present, not just non-duplicated")
				for _, name := range vmOnlyMetrics {
					ExpectWithOffset(1, values[name]).ToNot(BeEmpty(), "expected VM metric %q missing from /metrics", name)
				}
			})
		})
	})
)
