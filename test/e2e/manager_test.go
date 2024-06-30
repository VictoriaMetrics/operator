package e2e

import (
	"testing"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "e2e Controller Suite")
}

var (
	k8sClient client.Client
	_         = BeforeSuite(func() {
		suite.Before()
		k8sClient = suite.K8sClient
	})
)

var _ = AfterSuite(suite.After)
