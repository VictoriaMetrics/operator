package e2e

import (
	"testing"

	"github.com/VictoriaMetrics/operator/e2e/suite"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"e2e Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var k8sClient client.Client
var _ = BeforeSuite(func() {
	suite.Before()
	k8sClient = suite.K8sClient
})

var _ = AfterSuite(suite.After)
