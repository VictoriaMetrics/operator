package v1beta1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func TestVMUser_Validate(t *testing.T) {
	type opts struct {
		cr      *VMUser
		wantErr bool
	}
	f := func(opts opts) {
		if err := opts.cr.Validate(); (err != nil) != opts.wantErr {
			t.Errorf("Validate() error = %v, wantErr %v", err, opts.wantErr)
		}
	}

	// invalid auths
	o := opts{
		cr: &VMUser{
			Spec: VMUserSpec{
				UserName:    ptr.To("user"),
				BearerToken: ptr.To("bearer"),
			},
		},
		wantErr: true,
	}
	f(o)

	// invalid ref
	o = opts{
		cr: &VMUser{
			Spec: VMUserSpec{
				UserName: ptr.To("some-user"),
				TargetRefs: []TargetRef{
					{
						CRD:    &CRDRef{Name: "sm"},
						Static: &StaticRef{URL: "some"},
					},
				},
			},
		},
		wantErr: true,
	}
	f(o)

	// invalid ref wo targets
	o = opts{
		cr: &VMUser{
			Spec: VMUserSpec{
				UserName: ptr.To("some-user"),
				TargetRefs: []TargetRef{
					{
						Paths: []string{"/some-path"},
					},
				},
			},
		},
		wantErr: true,
	}
	f(o)

	// invalid ref crd, bad empty ns
	o = opts{
		cr: &VMUser{
			Spec: VMUserSpec{
				UserName: ptr.To("some-user"),
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
		},
		wantErr: true,
	}
	f(o)

	// incorrect password
	o = opts{
		cr: &VMUser{
			Spec: VMUserSpec{
				UserName: ptr.To("some-user"),
				Password: ptr.To("some-password"),
				PasswordRef: &corev1.SecretKeySelector{
					Key: "some-key",
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "some-name",
					},
				},
			},
		},
		wantErr: true,
	}
	f(o)

	// correct crd target
	o = opts{
		cr: &VMUser{
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
		},
	}
	f(o)
}
