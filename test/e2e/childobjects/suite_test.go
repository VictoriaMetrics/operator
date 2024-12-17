package childobjects

import (
	"testing"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const eventualDeletionTimeout = 20
const eventualReadyTimeout = 60

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "e2e Controller Child objects")
}

var k8sClient client.Client
var _ = SynchronizedBeforeSuite(
	func() {
		suite.InitOperatorProcess()
	},
	func() {
		k8sClient = suite.GetClient()
	})

var _ = SynchronizedAfterSuite(
	func() {
		suite.StopClient()
	},
	func() {
		suite.ShutdownOperatorProcess()
	})
