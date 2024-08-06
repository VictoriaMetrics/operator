/*


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

package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("VMAlertmanager Webhook", func() {
	var am *VMAlertmanager
	BeforeEach(func() {
		am = &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-suite",
				Namespace: "test",
			},
			Spec: VMAlertmanagerSpec{},
		}
	})
	Context("When creating VMAlertmanager under Validating Webhook", func() {
		It("Should deny config file with bad syntax", func() {
			am.Spec.ConfigRawYaml = `
global:
 resolve_timeout: 10m
 group_wait: 1s
          `
			Expect(am.sanityCheck()).NotTo(Succeed())
		})

		It("Should allow with correct config syntax", func() {
			am.Spec.ConfigRawYaml = `
   global:
     resolve_timeout: 5m
   route:
     group_wait: 10s
     group_interval: 2m
     group_by: ["alertgroup", "resource_id"]
     repeat_interval: 12h
     receiver: 'blackhole'
   receivers:
     # by default route to dev/null
     - name: blackhole
          `
			Expect(am.sanityCheck()).To(Succeed())
		})
	})

	Context("When creating VMAlertmanager under Conversion Webhook", func() {
		It("Should get the converted version of VMAlertmanager", func() {
			// TODO(user): Add your logic here
		})
	})
})
