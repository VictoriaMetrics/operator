package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestVMUser_Validate(t *testing.T) {
	f := func(cr *VMUser, wantErr bool) {
		t.Helper()
		if wantErr {
			assert.Error(t, cr.Validate())
		} else {
			assert.NoError(t, cr.Validate())
		}
	}

	// invalid auths
	f(&VMUser{
		Spec: VMUserSpec{
			Username:    ptr.To("user"),
			BearerToken: ptr.To("bearer"),
		},
	}, true)

	// invalid ref
	f(&VMUser{
		Spec: VMUserSpec{
			Username: ptr.To("some-user"),
			TargetRefs: []TargetRef{
				{
					CRD: &CRDRef{
						NamespacedName: NamespacedName{Name: "sm"},
					},
					Static: &StaticRef{URL: "some"},
				},
			},
		},
	}, true)

	// invalid ref wo targets
	f(&VMUser{
		Spec: VMUserSpec{
			Username: ptr.To("some-user"),
			TargetRefs: []TargetRef{
				{
					Paths: []string{"/some-path"},
				},
			},
		},
	}, true)

	// invalid ref crd, bad empty ns
	f(&VMUser{
		Spec: VMUserSpec{
			Username: ptr.To("some-user"),
			TargetRefs: []TargetRef{
				{
					CRD: &CRDRef{
						Kind: "VMSingle",
						NamespacedName: NamespacedName{
							Name:      "some-1",
							Namespace: "",
						},
					},
					Paths: []string{"/some-path"},
				},
			},
		},
	}, true)

	// incorrect password
	f(&VMUser{
		Spec: VMUserSpec{
			Username: ptr.To("some-user"),
			Password: ptr.To("some-password"),
			PasswordRef: &corev1.SecretKeySelector{
				Key: "some-key",
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "some-name",
				},
			},
		},
	}, true)

	// correct crd target
	f(&VMUser{
		Spec: VMUserSpec{
			TargetRefs: []TargetRef{
				{
					CRD: &CRDRef{
						Kind: "VMSingle",
						NamespacedName: NamespacedName{
							Name:      "some-1",
							Namespace: "some-ns",
						},
					},
					Paths: []string{"/"},
				},
				{
					Static: &StaticRef{
						URL: "http://some-url",
					},
					Paths: []string{"/targets"},
				},
			},
		},
	}, false)
}

func TestVMUser_FinalLabels(t *testing.T) {
	type opts struct {
		cr           *VMUser
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
		cr: &VMUser{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmuser",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
		},
	})
	// common labels added
	f(opts{
		cr:           &VMUser{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmuser",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "platform",
		},
	})
	// common labels cannot override existing
	f(opts{
		cr:           &VMUser{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"managed-by": "intruder", "team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmuser",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "platform",
		},
	})
	// common labels cannot override managedMetadata
	f(opts{
		cr: &VMUser{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VMUserSpec{ManagedMetadata: &ManagedObjectsMetadata{Labels: map[string]string{"team": "backend"}}},
		},
		commonLabels: map[string]string{"team": "intruder", "env": "prod"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmuser",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "backend",
			"env":                         "prod",
		},
	})
}

func TestVMUser_FinalAnnotations(t *testing.T) {
	type opts struct {
		cr                *VMUser
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
	f(opts{cr: &VMUser{ObjectMeta: metav1.ObjectMeta{Name: "test"}}, want: nil})
	// common annotations added
	f(opts{
		cr:                &VMUser{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonAnnotations: map[string]string{"note": "managed-by-gitops"},
		want:              map[string]string{"note": "managed-by-gitops"},
	})
	// common annotations cannot override managedMetadata
	f(opts{
		cr: &VMUser{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VMUserSpec{ManagedMetadata: &ManagedObjectsMetadata{Annotations: map[string]string{"note": "from-spec"}}},
		},
		commonAnnotations: map[string]string{"note": "intruder", "extra": "value"},
		want:              map[string]string{"note": "from-spec", "extra": "value"},
	})
}
