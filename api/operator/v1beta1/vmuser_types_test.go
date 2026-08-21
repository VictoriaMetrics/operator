package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestVMUser_AsKey(t *testing.T) {
	f := func(cr *VMUser, hide bool, want string) {
		t.Helper()
		assert.Equal(t, want, cr.AsKey(hide))
	}

	// bearerToken
	f(&VMUser{
		ObjectMeta: metav1.ObjectMeta{Name: "user-1", Namespace: "ns"},
		Spec:       VMUserSpec{BearerToken: ptr.To("some-token")},
	}, false, "ns/bearerToken:some-token")

	// basicAuth with username
	f(&VMUser{
		ObjectMeta: metav1.ObjectMeta{Name: "user-1", Namespace: "ns"},
		Spec:       VMUserSpec{Username: ptr.To("some-user")},
	}, false, "ns/basicAuth:some-user")

	// basicAuth without username falls back to cr.Name
	f(&VMUser{
		ObjectMeta: metav1.ObjectMeta{Name: "user-1", Namespace: "ns"},
	}, false, "ns/basicAuth:user-1")

	// jwt users with different names must produce different keys
	f(&VMUser{
		ObjectMeta: metav1.ObjectMeta{Name: "awesome-team-logs-user", Namespace: "harbor"},
		Spec:       VMUserSpec{JWT: &VMUserJWT{}},
	}, false, "harbor/jwt:awesome-team-logs-user")

	f(&VMUser{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-access-user", Namespace: "harbor"},
		Spec:       VMUserSpec{JWT: &VMUserJWT{}},
	}, false, "harbor/jwt:custom-access-user")

	// hide has no effect on jwt key, since it's based on the CR name, not a secret
	f(&VMUser{
		ObjectMeta: metav1.ObjectMeta{Name: "awesome-team-logs-user", Namespace: "harbor"},
		Spec:       VMUserSpec{JWT: &VMUserJWT{}},
	}, true, "harbor/jwt:awesome-team-logs-user")
}

func TestVMUser_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VMUser{Spec: VMUserSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vmuser-myapp")
	f("myapp", true, "myapp")
}
