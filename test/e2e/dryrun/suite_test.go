package dryrun

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
	"github.com/VictoriaMetrics/operator/test/e2e/suite/allure"
)

const namespace = "test-dry-run"

func TestDryRun(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "e2e Operator dry-run Suite")
}

var (
	k8sClient client.WithWatch
	k8sCfg    rest.Config

	_ = SynchronizedBeforeSuite(func() []byte {
		return suite.InitTestEnv(namespace)
	}, func(data []byte) {
		k8sClient = suite.GetClient(data)
		k8sCfg = suite.GetRestConfig(data)
	})

	_ = SynchronizedAfterSuite(func() {}, func() {
		suite.ShutdownTestEnv()
	})

	_ = AfterEach(suite.CollectK8SResources)
	_ = ReportAfterSuite("allure report", func(report Report) {
		_ = allure.FromGinkgoReport(report)
	})
)
