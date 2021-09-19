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

//func mustDeleteObject(client client.Client, obj client.Object) error {
//	if !Expect(func() error {
//		err := client.Delete(context.Background(), obj)
//		if err != nil {
//			return err
//		}
//		return nil
//	}).To(BeNil()) {
//		return fmt.Errorf("unexpected not nil resut : %s", obj.GetName())
//	}
//	if !Eventually(func() string {
//		err := client.Get(context.Background(), types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj)
//		if errors.IsNotFound(err) {
//			return ""
//		}
//		if err == nil {
//			err = fmt.Errorf("expect object to be deleted: %s", obj.GetName())
//		}
//		return err.Error()
//	}, 30).Should(BeEmpty()) {
//		return fmt.Errorf("unexpected not nil resutl for : %s", obj.GetName())
//	}
//	return nil
//}
