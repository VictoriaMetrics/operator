package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
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

	// serviceSpec.useAsDefault with an explicit type must be rejected (default service is headless)
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-suite",
				Namespace: "test",
			},
			Spec: VMAlertmanagerSpec{
				ServiceSpec: &AdditionalServiceSpec{
					UseAsDefault: true,
					Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				},
			},
		},
		wantErr: true,
	})

	// serviceSpec.useAsDefault without an explicit type is allowed
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-suite",
				Namespace: "test",
			},
			Spec: VMAlertmanagerSpec{
				ServiceSpec: &AdditionalServiceSpec{
					UseAsDefault: true,
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Name: "http", Port: 9093}},
					},
				},
			},
		},
		wantErr: false,
	})

	// explicit type without useAsDefault creates a separate service - allowed
	f(opts{
		cr: &VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-suite",
				Namespace: "test",
			},
			Spec: VMAlertmanagerSpec{
				ServiceSpec: &AdditionalServiceSpec{
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				},
			},
		},
		wantErr: false,
	})
}

func TestVMAlertmanager_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VMAlertmanager{Spec: VMAlertmanagerSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vmalertmanager-myapp")
	f("myapp", true, "myapp")
}

// TestVMAlertmanager_IsUnmanaged is the VMAlertmanager counterpart of TestVMAlert_IsUnmanaged.
func TestVMAlertmanager_IsUnmanaged(t *testing.T) {
	f := func(cr VMAlertmanager, want bool) {
		t.Helper()
		assert.Equal(t, want, cr.IsUnmanaged())
	}

	f(VMAlertmanager{Spec: VMAlertmanagerSpec{SelectAllByDefault: true}}, false)
	f(VMAlertmanager{}, true)
	f(VMAlertmanager{
		Status: VMAlertmanagerStatus{ParsingSpecError: `json: unknown field "foo"`},
		Spec:   VMAlertmanagerSpec{SelectAllByDefault: true},
	}, false)
	f(VMAlertmanager{
		Status: VMAlertmanagerStatus{ParsingSpecError: "some other unrelated parse failure"},
		Spec:   VMAlertmanagerSpec{SelectAllByDefault: true},
	}, true)
}
