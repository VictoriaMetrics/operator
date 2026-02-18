/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
	"github.com/VictoriaMetrics/operator/test/e2e/suite/allure"
)

var (
	eventualDeploymentAppReadyTimeout   = 60 * time.Second
	eventualStatefulsetAppReadyTimeout  = 80 * time.Second
	eventualDeletionTimeout             = 45 * time.Second
	eventualDeploymentPodTimeout        = 25 * time.Second
	eventualExpandingTimeout            = 25 * time.Second
	eventualDistributedExpandingTimeout = 5 * time.Minute
)

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting vm-operator suite\n")
	suiteConfig, reporterConfig := GinkgoConfiguration()
	RunSpecs(t, "End2End Suite", suiteConfig, reporterConfig)
}

var (
	k8sClient client.WithWatch

	_ = SynchronizedBeforeSuite(func() []byte {
		return suite.InitOperatorProcess()
	}, func(data []byte) {
		k8sClient = suite.GetClient(data)
	})

	_ = SynchronizedAfterSuite(func() {}, func() {
		suite.ShutdownOperatorProcess()
	})

	_ = AfterEach(suite.CollectK8SResources)
	_ = ReportAfterSuite("allure report", func(report Report) {
		_ = allure.FromGinkgoReport(report)
	})
)
