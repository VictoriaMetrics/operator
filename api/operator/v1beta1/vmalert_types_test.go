package v1beta1

import (
	"testing"

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
		if err := cr.Validate(); err != nil {
			t.Fatalf("unexpected validation error: %s", err)
		}
	}
	f(VMAlertSpec{
		Datasource: VMAlertDatasourceSpec{URL: "http://some-url"},
		CommonApplicationDeploymentParams: CommonApplicationDeploymentParams{
			ExtraArgs: map[string]ArgValue{"notifier.blackhole": []string{"true"}},
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
		if err := cr.Validate(); err == nil {
			t.Fatalf("expected configuration to fail")
		}
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
