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
	"os/exec"
	"testing"
	"time"

	"github.com/VictoriaMetrics/operator/test/e2e/suite"
	"github.com/VictoriaMetrics/operator/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	eventualDeploymentAppReadyTimeout  = 60 * time.Second
	eventualStatefulsetAppReadyTimeout = 80 * time.Second
	eventualDeletionTimeout            = 30 * time.Second
	eventualDeploymentPodTimeout       = 10 * time.Second
	eventualExpandingTimeout           = 5 * time.Second
)

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	fmt.Fprintf(GinkgoWriter, "Starting vm-operator suite\n")
	RunSpecs(t, "e2e suite")
}

var (
	_ = Describe("operator", Ordered, func() {
		ns := "vm"
		BeforeAll(func() {
			By("installing the cert-manager")
			Expect(utils.InstallCertManager()).To(Succeed())

			By("creating manager namespace")
			cmd := exec.Command("kubectl", "create", "ns", ns)
			_, _ = utils.Run(cmd)
		})

		AfterAll(func() {
			By("uninstalling the cert-manager bundle")
			utils.UninstallCertManager()

			By("removing manager namespace")
			cmd := exec.Command("kubectl", "delete", "ns", ns)
			_, _ = utils.Run(cmd)
		})

		Context("Operator", func() {
			It("should run successfully", func() {
				var operatorPodName string
				var err error

				repo := "operator"
				image := fmt.Sprintf("victoriametrics/%s", repo)

				By("building the manager(Operator) image")
				cmd := exec.Command(
					"make",
					"load-kind",
				)
				_, err = utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())

				By("loading the the manager(Operator) image on Kind")
				err = utils.LoadImageToKindClusterWithName(image)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())

				By("installing CRDs")
				cmd = exec.Command(
					"make",
					"install",
				)
				_, err = utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())

				By("deploying the operator")
				cmd = exec.Command(
					"make",
					"deploy-kind",
					fmt.Sprintf("NAMESPACE=%s", ns),
				)
				_, err = utils.Run(cmd)
				ExpectWithOffset(1, err).NotTo(HaveOccurred())

				By("validating that the operator pod is running as expected")
				verifyControllerUp := func() error {
					// Get pod name

					cmd = exec.Command("kubectl", "get",
						"pods", "-l", "control-plane=vm-operator",
						"-o", "go-template={{ range .items }}"+
							"{{ if not .metadata.deletionTimestamp }}"+
							"{{ .metadata.name }}"+
							"{{ \"\\n\" }}{{ end }}{{ end }}",
						"-n", ns,
					)

					podOutput, err := utils.Run(cmd)
					ExpectWithOffset(2, err).NotTo(HaveOccurred())
					podNames := utils.GetNonEmptyLines(string(podOutput))
					if len(podNames) != 1 {
						return fmt.Errorf("expect 1 operator pods running, but got %d", len(podNames))
					}
					operatorPodName = podNames[0]
					ExpectWithOffset(2, operatorPodName).Should(ContainSubstring("operator"))

					// Validate pod status
					cmd = exec.Command("kubectl", "get",
						"pods", operatorPodName, "-o", "jsonpath={.status.phase}",
						"-n", ns,
					)
					status, err := utils.Run(cmd)
					ExpectWithOffset(2, err).NotTo(HaveOccurred())
					if string(status) != "Running" {
						return fmt.Errorf("operator pod in %s status", status)
					}

					// Undeploy
					cmd = exec.Command(
						"make", "undeploy-kind",
						fmt.Sprintf("NAMESPACE=%s", ns),
					)
					_, err = utils.Run(cmd)
					ExpectWithOffset(2, err).NotTo(HaveOccurred())
					return nil
				}
				EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).Should(Succeed())

			})
		})
	})

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

	//_ = AfterSuite()

	k8sClient client.Client
)
