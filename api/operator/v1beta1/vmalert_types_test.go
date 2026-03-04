package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVMAlert_ValidateOk(t *testing.T) {
	f := func(spec VMAlertSpec) {
		t.Helper()
		cr := VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ok",
				Namespace: "default",
			},
			Spec: spec,
		}
		assert.NoError(t, cr.Validate())
	}
	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "http://some-url"},
		CommonApplicationDeploymentParams: CommonApplicationDeploymentParams{
			ExtraArgs: map[string]string{"notifier.blackhole": "true"},
		},
	})

	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "http://some-url"},
		Notifier: &VMAlertNotifierSpec{
			URL: "http://some",
		},
	})

	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "http://some-url"},
		Notifier: &VMAlertNotifierSpec{
			URL: "http://some",
		},

		Notifiers: []VMAlertNotifierSpec{
			{URL: "http://some-other"},
		},
	})

	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "http://some-url"},
		NotifierConfigRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
			Key:                  "config.yaml",
		},
	})
}

func TestVMAlert_ValidateFail(t *testing.T) {
	f := func(spec VMAlertSpec) {
		t.Helper()
		cr := VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ok",
				Namespace: "default",
			},
			Spec: spec,
		}
		assert.Error(t, cr.Validate())
	}
	// no notifier config
	f(VMAlertSpec{Notifier: &VMAlertNotifierSpec{
		URL: "some-url",
	}})

	// notifier url is missing
	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "some-url"},
		Notifier:   &VMAlertNotifierSpec{},
	})

	// incorrect notifier url syntax
	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "some-url"},
		Notifier: &VMAlertNotifierSpec{
			URL: "httpps://some-Bad-Synax^$",
		},
	})

	// spec.notifier + spec.notifierConfig
	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "some-url"},
		Notifier: &VMAlertNotifierSpec{
			URL: "http://some",
		},
		NotifierConfigRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
			Key:                  "config.yaml",
		},
	})

	// spec.notifiers + spec.notifierConfig
	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "some-url"},
		Notifiers: []VMAlertNotifierSpec{
			{URL: "http://some"},
		},
		NotifierConfigRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
			Key:                  "config.yaml",
		},
	})
}
