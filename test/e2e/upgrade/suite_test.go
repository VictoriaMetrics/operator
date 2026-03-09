package upgrade

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

var (
	k8sClient client.WithWatch
	ctx       = context.Background()
)

func TestUpgrade(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteConfig, reporterConfig := GinkgoConfiguration()
	RunSpecs(t, "Upgrade Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	By("bootstrapping upgrade test environment")
	data := suite.InitTestEnv("upgrade-1", "upgrade-2", "upgrade-3", "upgrade-4")
	k8sClient = suite.GetClient(data)
})

var _ = AfterSuite(func() {
	By("tearing down the upgrade test environment")
	suite.ShutdownTestEnv()
})
