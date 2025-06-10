package childobjects

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
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

func expectConditionOkFor(conds []vmv1beta1.Condition, typeCondtains string) error {
	for _, cond := range conds {
		if strings.Contains(cond.Type, typeCondtains) {
			if cond.Status == "True" {
				return nil
			}
			return fmt.Errorf("unexpected status=%q for type=%q with message=%q", cond.Status, cond.Type, cond.Message)
		}
	}
	return fmt.Errorf("expected condition not found")
}
