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
	"github.com/VictoriaMetrics/operator/test/e2e/suite/allure"
)

const eventualDeletionTimeout = 20
const eventualReadyTimeout = 60

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "e2e Controller Child objects")
}

var (
	k8sClient client.Client

	_ = SynchronizedBeforeSuite(
		func() {
			suite.InitOperatorProcess()
		},
		func() {
			k8sClient = suite.GetClient()
		})

	_ = SynchronizedAfterSuite(
		func() {
			suite.StopClient()
		},
		func() {
			suite.ShutdownOperatorProcess()
		})

	_ = AfterEach(suite.CollectK8SResources)
	_ = ReportAfterSuite("allure report", func(report Report) {
		_ = allure.FromGinkgoReport(report)
	})
)

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
