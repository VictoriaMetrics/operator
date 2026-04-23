package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/internal/config"
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
		CommonAppsParams: CommonAppsParams{
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

func TestVMAlert_FinalLabels(t *testing.T) {
	type opts struct {
		cr           *VMAlert
		commonLabels map[string]string
		want         map[string]string
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonLabels = o.commonLabels
		assert.Equal(t, o.want, o.cr.FinalLabels())
	}

	// no common labels
	f(opts{
		cr: &VMAlert{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmalert",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
		},
	})
	// common labels added
	f(opts{
		cr:           &VMAlert{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmalert",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "platform",
		},
	})
	// common labels cannot override existing
	f(opts{
		cr:           &VMAlert{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"managed-by": "intruder", "team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmalert",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "platform",
		},
	})
	// common labels cannot override managedMetadata
	f(opts{
		cr: &VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec: VMAlertSpec{ManagedMetadata: &ManagedObjectsMetadata{Labels: map[string]string{"team": "backend"}}},
		},
		commonLabels: map[string]string{"team": "intruder", "env": "prod"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmalert",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "backend",
			"env":                         "prod",
		},
	})
}

func TestVMAlert_FinalAnnotations(t *testing.T) {
	type opts struct {
		cr                *VMAlert
		commonAnnotations map[string]string
		want              map[string]string
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonAnnotations = o.commonAnnotations
		assert.Equal(t, o.want, o.cr.FinalAnnotations())
	}

	// no annotations
	f(opts{cr: &VMAlert{ObjectMeta: metav1.ObjectMeta{Name: "test"}}, want: nil})
	// common annotations added
	f(opts{
		cr:                &VMAlert{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonAnnotations: map[string]string{"note": "managed-by-gitops"},
		want:              map[string]string{"note": "managed-by-gitops"},
	})
	// common annotations cannot override managedMetadata
	f(opts{
		cr: &VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec: VMAlertSpec{ManagedMetadata: &ManagedObjectsMetadata{Annotations: map[string]string{"note": "from-spec"}}},
		},
		commonAnnotations: map[string]string{"note": "intruder", "extra": "value"},
		want:              map[string]string{"note": "from-spec", "extra": "value"},
	})
}
