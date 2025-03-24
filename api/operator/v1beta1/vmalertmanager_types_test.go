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
			Expect(am.Validate()).NotTo(Succeed())
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
			Expect(am.Validate()).To(Succeed())
		})
	})

	Context("When creating VMAlertmanager under Conversion Webhook", func() {
		It("Should get the converted version of VMAlertmanager", func() {
			// TODO(user): Add your logic here
		})
	})
})
