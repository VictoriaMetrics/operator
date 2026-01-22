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

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

var (
	eventualDeploymentAppReadyTimeout  = 60 * time.Second
	eventualStatefulsetAppReadyTimeout = 80 * time.Second
	eventualDeletionTimeout            = 45 * time.Second
	eventualDeploymentPodTimeout       = 25 * time.Second
	eventualExpandingTimeout           = 25 * time.Second
)

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting vm-operator suite\n")
	RunSpecs(t, "e2e suite")
}

var (
	_ = SynchronizedBeforeSuite(
		func() {
			suite.InitOperatorProcess()
		},
		func() {
			k8sClient = suite.GetClient()
		},
	)

	_ = SynchronizedAfterSuite(
		func() {
			suite.StopClient()
		},
		func() {
			suite.ShutdownOperatorProcess()
		},
	)

	_ = AfterEach(func() {
		if CurrentSpecReport().Failed() {
			err := utils.RunCrustGather(context.Background(), 10*time.Minute)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "crust-gather: %v", err)
			}
		}
	})

	_ = ReportAfterSuite("allure report", func(report types.Report) {
		parserConfig := parser.Config{}
		parserConfig.LabelsScraperOpts = append(parserConfig.LabelsScraperOpts, allureReport.WillAutoGenerateID(true))
		allureReports, err := convert.GinkgoToAllureReport([]types.Report{report}, parser.NewDefaultParser, parserConfig)
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "allure report: %v", err)
		}

		reportPath, err := filepath.Abs(allureReportPath)
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "allure report: %v", err)
		}

		if err := os.MkdirAll(reportPath, 0755); err != nil {
			panic(fmt.Sprintf("failed to create report dir: %v", err))
		}

		fileManager := fmngr.NewFileManager(reportPath)

		errs := convert.PrintAllureReports(allureReports, fileManager)
		if len(errs) > 0 {
			fmt.Fprintf(GinkgoWriter, "allure report: %v", errs)
		}
		GinkgoLogr.Info("Allure report generated", "path", reportPath)
	})

	// _ = AfterSuite()

	k8sClient client.Client
)
