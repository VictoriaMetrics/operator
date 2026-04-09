package upgrade

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
	"github.com/VictoriaMetrics/operator/test/e2e/suite/allure"
)

func TestUpgrade(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	RunSpecs(t, "Upgrade Suite", suiteConfig, reporterConfig)
}

var (
	k8sClient client.WithWatch
	ctx       = context.Background()

	_ = SynchronizedBeforeSuite(func() []byte {
		return suite.InitTestEnv()
	}, func(data []byte) {
		k8sClient = suite.GetClient(data)
	})

	_ = SynchronizedAfterSuite(func() {}, func() {
		suite.ShutdownTestEnv()
	})

	_ = ReportAfterSuite("allure report", func(report Report) {
		_ = allure.FromGinkgoReport(report)
	})
)
