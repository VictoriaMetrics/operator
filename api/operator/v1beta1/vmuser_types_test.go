package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
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
					CRD:    &CRDRef{Name: "sm"},
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
						Name:      "some-1",
						Kind:      "VMSingle",
						Namespace: "",
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
						Name:      "some-1",
						Namespace: "some-ns",
						Kind:      "VMSingle",
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
