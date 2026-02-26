package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVMAlertmanagerValidate(t *testing.T) {
	type opts struct {
		cr      *VMAlertmanager
		wantErr bool
	}
	f := func(o opts) {
		t.Helper()
		if o.wantErr {
			assert.Error(t, o.cr.Validate())
		} else {
			assert.NoError(t, o.cr.Validate())
		}
	}

	// config file with bad syntax
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-suite",
				Namespace: "test",
			},
			Spec: VMAlertmanagerSpec{
				ConfigRawYaml: `
global:
 resolve_timeout: 10m
 group_wait: 1s`,
			},
		},
		wantErr: true,
	})

	// config with correct syntax
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-suite",
				Namespace: "test",
			},
			Spec: VMAlertmanagerSpec{
				ConfigRawYaml: `
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
  - name: blackhole`,
			},
		},
	})
}
