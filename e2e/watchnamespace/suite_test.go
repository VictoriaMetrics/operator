package watchnamespace

import (
	"context"
	"os"
	"testing"

	"github.com/VictoriaMetrics/operator/e2e/suite"
	"github.com/VictoriaMetrics/operator/internal/config"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const excludedNamespace = "test-excluded"
const includedNamespace = "default"

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "e2e Controller WATCH_NAMESPACE Suite")
}

var k8sClient client.Client
var _ = BeforeSuite(func() {
	var err error
	err = os.Setenv(config.WatchNamespaceEnvVar, "default")
	Expect(err).NotTo(HaveOccurred())

	suite.Before()
	k8sClient = suite.K8sClient

	testNamespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: excludedNamespace,
		},
	}
	err = k8sClient.Create(context.Background(), &testNamespace)
	Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue(), "got unexpected namespace creation error: %v", err)
}, 60)

var _ = AfterSuite(suite.After)
