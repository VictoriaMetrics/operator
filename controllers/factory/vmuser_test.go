package factory

import (
	"context"
	"testing"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
)

func Test_genUserCfg(t *testing.T) {
	type args struct {
		user        *v1beta1.VMUser
		crdUrlCache map[string]string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "basic user cfg",
			args: args{
				user: &v1beta1.VMUser{
					Spec: v1beta1.VMUserSpec{
						Name:     pointer.StringPtr("user1"),
						UserName: pointer.StringPtr("basic"),
						Password: pointer.StringPtr("pass"),
						TargetRefs: []v1beta1.TargetRef{
							{
								Static: &v1beta1.StaticRef{
									URL: "http://vmselect",
								},
								Paths: []string{
									"/select/0/prometheus",
									"/select/0/graphite",
								},
							},
						},
					},
				},
			},
			want: `url_map:
- url_prefix: http://vmselect
  src_paths:
  - /select/0/prometheus
  - /select/0/graphite
name: user1
username: basic
password: pass
`,
		},
		{
			name: "with crd",
			args: args{
				user: &v1beta1.VMUser{
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user1"),
						BearerToken: pointer.StringPtr("secret-token"),
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "base",
									Namespace: "monitoring",
								},
								Paths: []string{
									"/api/v1/write",
									"/api/v1/targets",
									"/targets",
								},
							},
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMSingle",
									Namespace: "monitoring",
									Name:      "db",
								},
							},
						},
					},
				},
				crdUrlCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_map:
- url_prefix: http://vmagent-base.monitoring.svc:8429
  src_paths:
  - /api/v1/write
  - /api/v1/targets
  - /targets
- url_prefix: http://vmsingle-b.monitoring.svc:8429
  src_paths:
  - /.*
name: user1
bearer_token: secret-token
`,
		},
		{
			name: "with crd and custom suffix",
			args: args{
				user: &v1beta1.VMUser{
					Spec: v1beta1.VMUserSpec{
						BearerToken: pointer.StringPtr("secret-token"),
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "base",
									Namespace: "monitoring",
								},
								TargetPathSuffix: "/insert/0/prometheus?extra_label=key=value",
								Paths: []string{
									"/api/v1/write",
									"/api/v1/targets",
									"/targets",
								},
								Headers: []string{"baz: bar"},
							},
							{
								Static:           &v1beta1.StaticRef{URL: "http://vmcluster-remote.mydomain.com:8401"},
								TargetPathSuffix: "/insert/0/prometheus?extra_label=key=value",
								Paths: []string{
									"/",
								},
							},
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMSingle",
									Namespace: "monitoring",
									Name:      "db",
								},
							},
						},
					},
				},
				crdUrlCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_map:
- url_prefix: http://vmagent-base.monitoring.svc:8429/insert/0/prometheus?extra_label=key%3Dvalue
  src_paths:
  - /api/v1/write
  - /api/v1/targets
  - /targets
  headers:
  - 'baz: bar'
- url_prefix: http://vmcluster-remote.mydomain.com:8401/insert/0/prometheus?extra_label=key%3Dvalue
  src_paths:
  - /.*
- url_prefix: http://vmsingle-b.monitoring.svc:8429
  src_paths:
  - /.*
bearer_token: secret-token
`,
		},
		{
			name: "with one target",
			args: args{
				user: &v1beta1.VMUser{
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user1"),
						BearerToken: pointer.StringPtr("secret-token"),
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "base",
									Namespace: "monitoring",
								},
							},
						},
					},
				},
				crdUrlCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_prefix: http://vmagent-base.monitoring.svc:8429
name: user1
bearer_token: secret-token
`,
		},
		{
			name: "with target headers",
			args: args{
				user: &v1beta1.VMUser{
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user2"),
						BearerToken: pointer.StringPtr("secret-token"),
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "base",
									Namespace: "monitoring",
								},
								Headers: []string{"X-Scope-OrgID: abc", "X-Scope-Team: baz"},
							},
						},
					},
				},
				crdUrlCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_prefix: http://vmagent-base.monitoring.svc:8429
headers:
- 'X-Scope-OrgID: abc'
- 'X-Scope-Team: baz'
name: user2
bearer_token: secret-token
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := genUserCfg(tt.args.user, tt.args.crdUrlCache)
			if (err != nil) != tt.wantErr {
				t.Errorf("genUserCfg() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			szd, err := yaml.Marshal(got)
			if err != nil {
				t.Fatalf("cannot serialize resutl: %v", err)
			}
			assert.Equal(t, tt.want, string(szd))
		})
	}
}

func Test_genPassword(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name: "simple test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got1, err := genPassword()
			if (err != nil) != tt.wantErr {
				t.Errorf("genPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			got2, err := genPassword()
			if (err != nil) != tt.wantErr {
				t.Errorf("genPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got1 == got2 {
				t.Errorf("genPassword() passowrd cannot be the same, got1 = %v got2 %v", got1, got2)
			}
		})
	}
}

func Test_selectVMUserSecrets(t *testing.T) {
	type args struct {
		vmUsers []*v1beta1.VMUser
	}
	tests := []struct {
		name              string
		args              args
		wantToCreateNames []string
		wantExistNames    []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "want 1 updateSecret",
			args: args{
				vmUsers: []*v1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist",
							Namespace: "default",
						},
						Spec: v1beta1.VMUserSpec{BearerToken: pointer.StringPtr("some-bearer")},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "not-exist",
							Namespace: "default",
						},
						Spec: v1beta1.VMUserSpec{BearerToken: pointer.StringPtr("some-bearer")},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist", Namespace: "default"},
				},
			},
			wantExistNames:    []string{"vmuser-exist"},
			wantToCreateNames: []string{"vmuser-not-exist"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, got1, err := selectVMUserSecrets(context.TODO(), testClient, tt.args.vmUsers)
			if (err != nil) != tt.wantErr {
				t.Errorf("selectVMUserSecrets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			secretFound := func(src []v1.Secret, wantName string) bool {
				for i := range src {
					s := &src[i]
					if s.Name == wantName {
						return true
					}
				}
				return false
			}
			for _, wantCreateName := range tt.wantToCreateNames {
				if !secretFound(got, wantCreateName) {
					t.Fatalf("wanted secret name: %s not found at toCreateSecrets", wantCreateName)
				}
			}
			for _, wantExistName := range tt.wantExistNames {
				if !secretFound(got1, wantExistName) {
					t.Fatalf("wanted secret name: %s not found at existSecrets", wantExistName)
				}
			}
		})
	}
}

func Test_buildVMAuthConfig(t *testing.T) {
	type args struct {
		vmauth *v1beta1.VMAuth
	}
	tests := []struct {
		name              string
		args              args
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "default cfg",
			args: args{
				vmauth: &v1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: v1beta1.VMAuthSpec{SelectAllByDefault: true},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user1"),
						BearerToken: pointer.StringPtr("bearer"),
						TargetRefs: []v1beta1.TargetRef{
							{
								Static: &v1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: v1beta1.VMUserSpec{
						BearerToken: pointer.StringPtr("bearer-token-2"),
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&v1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix: http://some-static
  name: user1
  bearer_token: bearer
- url_prefix: http://vmagent-test.default.svc:8429
  bearer_token: bearer-token-2
`,
		},

		{
			name: "with password ref",
			args: args{
				vmauth: &v1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: v1beta1.VMAuthSpec{SelectAllByDefault: true},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user-1"),
						BearerToken: pointer.StringPtr("bearer"),
						TargetRefs: []v1beta1.TargetRef{
							{
								Static: &v1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user-2"),
						BearerToken: pointer.StringPtr("bearer-token-2"),
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-5",
						Namespace: "default",
					},
					Spec: v1beta1.VMUserSpec{
						Name:     pointer.StringPtr("user-5"),
						UserName: pointer.StringPtr("some-user"),
						PasswordRef: &v1.SecretKeySelector{
							Key: "password",
							LocalObjectReference: v1.LocalObjectReference{
								Name: "generated-secret",
							},
						},
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "generated-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"password": []byte(`generated-password`)},
				},
				&v1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix: http://some-static
  name: user-1
  bearer_token: bearer
- url_prefix: http://vmagent-test.default.svc:8429
  name: user-2
  bearer_token: bearer-token-2
- url_prefix: http://vmagent-test.default.svc:8429
  name: user-5
  username: some-user
  password: generated-password
`,
		},
		{
			name: "default cfg with empty selectors",
			args: args{
				vmauth: &v1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user-1"),
						BearerToken: pointer.StringPtr("bearer"),
						TargetRefs: []v1beta1.TargetRef{
							{
								Static: &v1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user-2"),
						BearerToken: pointer.StringPtr("bearer-token-2"),
						TargetRefs: []v1beta1.TargetRef{
							{
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&v1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix: http://localhost:8428
  name: default-user
  bearer_token: some-default-token
`,
		},
		{
			name: "vmauth ns selector",
			args: args{
				vmauth: &v1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: v1beta1.VMAuthSpec{SelectAllByDefault: false, UserNamespaceSelector: &metav1.LabelSelector{}},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "default"},
				},
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "monitoring"},
				},
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-11",
						Namespace: "default",
					},
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user-11"),
						BearerToken: pointer.StringPtr("bearer"),
						TargetRefs: []v1beta1.TargetRef{
							{
								Static: &v1beta1.StaticRef{URL: "http://some-static-15"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&v1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-15",
						Namespace: "monitoring",
					},
					Spec: v1beta1.VMUserSpec{
						Name:        pointer.StringPtr("user-15"),
						BearerToken: pointer.StringPtr("bearer-token-10"),
						TargetRefs: []v1beta1.TargetRef{
							{
								Static: nil,
								CRD: &v1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&v1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix: http://some-static-15
  name: user-11
  bearer_token: bearer
- url_prefix: http://vmagent-test.default.svc:8429
  name: user-15
  bearer_token: bearer-token-10
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := buildVMAuthConfig(context.TODO(), testClient, tt.args.vmauth)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildVMAuthConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, string(got))
			got, err = buildVMAuthConfig(context.TODO(), testClient, tt.args.vmauth)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildVMAuthConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, string(got))
		})
	}
}
