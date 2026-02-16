package watchnamespace

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
	"github.com/VictoriaMetrics/operator/test/e2e/suite/allure"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const excludedNamespace = "test-excluded"
const includedNamespace = "default"

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "e2e Controller WATCH_NAMESPACE Suite")
}

var (
	k8sClient client.Client

	_ = SynchronizedBeforeSuite(func() []byte {
		Expect(os.Setenv("WATCH_NAMESPACE", "default")).NotTo(HaveOccurred())
		return suite.InitOperatorProcess(excludedNamespace)
	}, func(data []byte) {
		Expect(os.Setenv("WATCH_NAMESPACE", "default")).NotTo(HaveOccurred())
		k8sClient = suite.GetClient(data)
	})

	_ = SynchronizedAfterSuite(func() {}, func() {
		suite.ShutdownOperatorProcess()
	})

	_ = AfterEach(suite.CollectK8SResources)
	_ = ReportAfterSuite("allure report", func(report Report) {
		_ = allure.FromGinkgoReport(report)
	})
)
