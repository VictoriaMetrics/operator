package vmauth

import (
	"context"
	"strings"
	"testing"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func Test_genUserCfg(t *testing.T) {
	type args struct {
		user        *victoriametricsv1beta1.VMUser
		crdURLCache map[string]string
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
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:     ptr.To("user1"),
						UserName: ptr.To("basic"),
						Password: ptr.To("pass"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{
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
- url_prefix:
  - http://vmselect
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
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
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
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMSingle",
									Namespace: "monitoring",
									Name:      "db",
								},
							},
						},
					},
				},
				crdURLCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_map:
- url_prefix:
  - http://vmagent-base.monitoring.svc:8429
  src_paths:
  - /api/v1/write
  - /api/v1/targets
  - /targets
- url_prefix:
  - http://vmsingle-b.monitoring.svc:8429
  src_paths:
  - /.*
name: user1
bearer_token: secret-token
`,
		},
		{
			name: "with crd and custom suffix",
			args: args{
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
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
								Static:           &victoriametricsv1beta1.StaticRef{URL: "http://vmcluster-remote.mydomain.com:8401"},
								TargetPathSuffix: "/insert/0/prometheus?extra_label=key=value",
								Paths: []string{
									"/",
								},
							},
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMSingle",
									Namespace: "monitoring",
									Name:      "db",
								},
							},
						},
					},
				},
				crdURLCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_map:
- url_prefix:
  - http://vmagent-base.monitoring.svc:8429/insert/0/prometheus?extra_label=key%3Dvalue
  src_paths:
  - /api/v1/write
  - /api/v1/targets
  - /targets
  headers:
  - 'baz: bar'
- url_prefix:
  - http://vmcluster-remote.mydomain.com:8401/insert/0/prometheus?extra_label=key%3Dvalue
  src_paths:
  - /.*
- url_prefix:
  - http://vmsingle-b.monitoring.svc:8429
  src_paths:
  - /.*
bearer_token: secret-token
`,
		},
		{
			name: "with one target",
			args: args{
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "base",
									Namespace: "monitoring",
								},
							},
						},
					},
				},
				crdURLCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_prefix:
- http://vmagent-base.monitoring.svc:8429
name: user1
bearer_token: secret-token
`,
		},
		{
			name: "with target headers",
			args: args{
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user2"),
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "base",
									Namespace: "monitoring",
								},
								Headers: []string{"X-Scope-OrgID: abc", "X-Scope-Team: baz"},
							},
						},
					},
				},
				crdURLCache: map[string]string{
					"VMAgent/monitoring/base": "http://vmagent-base.monitoring.svc:8429",
					"VMSingle/monitoring/db":  "http://vmsingle-b.monitoring.svc:8429",
				},
			},
			want: `url_prefix:
- http://vmagent-base.monitoring.svc:8429
headers:
- 'X-Scope-OrgID: abc'
- 'X-Scope-Team: baz'
name: user2
bearer_token: secret-token
`,
		},
		{
			name: "with ip filters and multiple targets",
			args: args{
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:     ptr.To("user1"),
						UserName: ptr.To("basic"),
						Password: ptr.To("pass"),
						IPFilters: victoriametricsv1beta1.VMUserIPFilters{
							AllowList: []string{"127.0.0.1"},
						},
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{
									URL: "http://vmselect",
								},
								Paths: []string{
									"/select/0/prometheus",
									"/select/0/graphite",
								},
							},
							{
								Static: &victoriametricsv1beta1.StaticRef{
									URL: "http://vminsert",
								},
								Paths: []string{
									"/insert/0/prometheus",
								},
							},
						},
					},
				},
			},
			want: `url_map:
- url_prefix:
  - http://vmselect
  src_paths:
  - /select/0/prometheus
  - /select/0/graphite
- url_prefix:
  - http://vminsert
  src_paths:
  - /insert/0/prometheus
name: user1
ip_filters:
  allow_list:
  - 127.0.0.1
username: basic
password: pass
`,
		},
		{
			name: "with headers and max concurrent",
			args: args{
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:                  ptr.To("user1"),
						UserName:              ptr.To("basic"),
						Password:              ptr.To("pass"),
						Headers:               []string{"H1:V1", "H2:V2"},
						ResponseHeaders:       []string{"RH1:V3", "RH2:V4"},
						MaxConcurrentRequests: ptr.To(400),
						RetryStatusCodes:      []int{502, 503},
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{
									URL: "http://vmselect",
								},
								Paths: []string{
									"/select/0/prometheus",
									"/select/0/graphite",
								},
								Headers:         []string{"H1:V2", "H2:V3"},
								ResponseHeaders: []string{"RH1:V6", "RH2:V7"},
							},
							{
								Static: &victoriametricsv1beta1.StaticRef{
									URL: "http://vminsert",
								},
								Paths: []string{
									"/insert/0/prometheus",
								},
							},
						},
					},
				},
			},
			want: `url_map:
- url_prefix:
  - http://vmselect
  src_paths:
  - /select/0/prometheus
  - /select/0/graphite
  headers:
  - H1:V2
  - H2:V3
  response_headers:
  - RH1:V6
  - RH2:V7
- url_prefix:
  - http://vminsert
  src_paths:
  - /insert/0/prometheus
name: user1
max_concurrent_requests: 400
retry_status_codes:
- 502
- 503
headers:
- H1:V1
- H2:V2
response_headers:
- RH1:V3
- RH2:V4
username: basic
password: pass
`,
		},
		{
			name: "with load_balancing_policy and drop_src_path_prefix_parts and tls_insecure_skip_verify",
			args: args{
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:                   ptr.To("user1"),
						UserName:               ptr.To("basic"),
						Password:               ptr.To("pass"),
						LoadBalancingPolicy:    ptr.To("first_available"),
						DropSrcPathPrefixParts: ptr.To(1),
						TLSInsecureSkipVerify:  true,
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{
									URL: "http://vmselect",
								},
								Paths: []string{
									"/select/0/prometheus",
									"/select/0/graphite",
								},
								LoadBalancingPolicy:    ptr.To("first_available"),
								DropSrcPathPrefixParts: ptr.To(2),
							},
							{
								Static: &victoriametricsv1beta1.StaticRef{
									URL: "http://vminsert",
								},
								Paths: []string{
									"/insert/0/prometheus",
								},
							},
						},
					},
				},
			},
			want: `url_map:
- url_prefix:
  - http://vmselect
  src_paths:
  - /select/0/prometheus
  - /select/0/graphite
  drop_src_path_prefix_parts: 2
  load_balancing_policy: first_available
- url_prefix:
  - http://vminsert
  src_paths:
  - /insert/0/prometheus
name: user1
load_balancing_policy: first_available
drop_src_path_prefix_parts: 1
tls_insecure_skip_verify: true
username: basic
password: pass
`,
		},
		{
			name: "with metric_labels",
			args: args{
				user: &victoriametricsv1beta1.VMUser{
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:     ptr.To("user1"),
						UserName: ptr.To("basic"),
						Password: ptr.To("pass"),
						MetricLabels: map[string]string{
							"foo": "bar",
							"buz": "qux",
						},
					},
				},
			},
			want: `url_map: []
name: user1
metric_labels:
  buz: qux
  foo: bar
username: basic
password: pass
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := genUserCfg(tt.args.user, tt.args.crdURLCache)
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
		vmUsers []*victoriametricsv1beta1.VMUser
	}
	tests := []struct {
		name                string
		args                args
		wantToCreateSecrets []string
		wantToUpdateSecrets []string
		wantErr             bool
		predefinedObjects   []runtime.Object
	}{
		{
			name: "want 1 updateSecret",
			args: args{
				vmUsers: []*victoriametricsv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "not-exist",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist", Namespace: "default"},
				},
			},
			wantToUpdateSecrets: []string{"vmuser-exist"},
			wantToCreateSecrets: []string{"vmuser-not-exist"},
		},
		{
			name: "want 1 updateSecret",
			args: args{
				vmUsers: []*victoriametricsv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "must-not-exist",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{
							BearerToken:           ptr.To("some-bearer"),
							DisableSecretCreation: true,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "not-exists-must-create",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exists",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exists", Namespace: "default"},
				},
			},
			wantToUpdateSecrets: []string{"vmuser-exists"},
			wantToCreateSecrets: []string{"vmuser-not-exists-must-create"},
		},
		{
			name: "want nothing",
			args: args{
				vmUsers: []*victoriametricsv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist-with-generated",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{
							GeneratePassword: true,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist-hardcoded",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-with-generated", Namespace: "default"},
					Data:       map[string][]byte{"username": []byte(`vmuser-exist-with-generated`), "password": []byte(`generated`)},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-hardcoded", Namespace: "default"},
					Data:       map[string][]byte{"bearerToken": []byte(`some-bearer`)},
				},
			},
			wantToUpdateSecrets: []string{},
			wantToCreateSecrets: []string{},
		},
		{
			name: "update secret value",
			args: args{
				vmUsers: []*victoriametricsv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist-with-generated",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{
							GeneratePassword: true,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist-to-update",
							Namespace: "default",
						},
						Spec: victoriametricsv1beta1.VMUserSpec{Password: ptr.To("some-new-password"), UserName: ptr.To("some-user")},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-with-generated", Namespace: "default"},
					Data:       map[string][]byte{"password": []byte(`generated`)},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-to-update", Namespace: "default"},
					Data:       map[string][]byte{"password": []byte(`some-old-password`), "username": []byte(`some-user`)},
				},
			},
			wantToUpdateSecrets: []string{"vmuser-exist-to-update"},
			wantToCreateSecrets: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, got1, err := addAuthCredentialsBuildSecrets(context.TODO(), testClient, tt.args.vmUsers)
			if (err != nil) != tt.wantErr {
				t.Errorf("selectVMUserSecrets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			secretFound := func(src []*v1.Secret, wantName string) bool {
				for i := range src {
					s := src[i]
					if s.Name == wantName {
						return true
					}
				}
				return false
			}
			joinSecretNames := func(src []*v1.Secret) string {
				var dst strings.Builder
				for _, s := range src {
					dst.WriteString(s.Name)
					dst.WriteString(",")
				}
				return dst.String()
			}
			if len(tt.wantToCreateSecrets) != len(got) {
				t.Fatalf("not expected count of want=%d and got=%d to creates secrets, got=%q", len(got), len(tt.wantToCreateSecrets), joinSecretNames(got))
			}
			if len(tt.wantToUpdateSecrets) != len(got1) {
				t.Fatalf("not expected count of want=%d and got=%d to update secrets, got=%q", len(got1), len(tt.wantToUpdateSecrets), joinSecretNames(got1))
			}
			for _, wantCreateName := range tt.wantToCreateSecrets {
				if !secretFound(got, wantCreateName) {
					t.Fatalf("wanted secret name: %s not found at toCreateSecrets", wantCreateName)
				}
			}
			for _, wantExistName := range tt.wantToUpdateSecrets {
				if !secretFound(got1, wantExistName) {
					t.Fatalf("wanted secret name: %s not found at existSecrets", wantExistName)
				}
			}
		})
	}
}

func Test_buildVMAuthConfig(t *testing.T) {
	type args struct {
		vmauth *victoriametricsv1beta1.VMAuth
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
				vmauth: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{SelectAllByDefault: true},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix:
  - http://some-static
  name: user1
  bearer_token: bearer
- url_prefix:
  - http://vmagent-test.default.svc:8429
  bearer_token: bearer-token-2
`,
		},
		{
			name: "with targetRef basicauth secret refs and headers",
			args: args{
				vmauth: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{SelectAllByDefault: true},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:     ptr.To("user-1"),
						UserName: ptr.To("some-user"),
						PasswordRef: &v1.SecretKeySelector{
							Key: "password",
							LocalObjectReference: v1.LocalObjectReference{
								Name: "generated-secret",
							},
						},
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static:  &victoriametricsv1beta1.StaticRef{URL: "http://some-static"},
								Paths:   []string{"/"},
								Headers: []string{"baz: bar"},
								TargetRefBasicAuth: &victoriametricsv1beta1.TargetRefBasicAuth{
									Username: v1.SecretKeySelector{
										Key: "username",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "backend-auth-secret",
										},
									},
									Password: v1.SecretKeySelector{
										Key: "password",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "backend-auth-secret",
										},
									},
								},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-15",
						Namespace: "monitoring",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user-15"),
						BearerToken: ptr.To("bearer-token-10"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: nil,
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "generated-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"password": []byte(`generated-password`), "token": []byte(`some-bearer-token`)},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-auth-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"password": []byte(`pass`), "username": []byte(`user`)},
				},
			},
			want: `users:
- url_prefix:
  - http://some-static
  headers:
  - 'baz: bar'
  - 'Authorization: Basic dXNlcjpwYXNz'
  name: user-1
  username: some-user
  password: generated-password
- url_prefix:
  - http://vmagent-test.default.svc:8429
  name: user-15
  bearer_token: bearer-token-10
`,
		},
		{
			name: "with secret refs",
			args: args{
				vmauth: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{SelectAllByDefault: true},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user-1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user-2"),
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-5",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:     ptr.To("user-5"),
						UserName: ptr.To("some-user"),
						PasswordRef: &v1.SecretKeySelector{
							Key: "password",
							LocalObjectReference: v1.LocalObjectReference{
								Name: "generated-secret",
							},
						},
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-10",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name: ptr.To("user-10"),
						TokenRef: &v1.SecretKeySelector{
							Key: "token",
							LocalObjectReference: v1.LocalObjectReference{
								Name: "generated-secret",
							},
						},
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
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
					Data: map[string][]byte{"password": []byte(`generated-password`), "token": []byte(`some-bearer-token`)},
				},
				&victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix:
  - http://some-static
  name: user-1
  bearer_token: bearer
- url_prefix:
  - http://vmagent-test.default.svc:8429
  name: user-10
  bearer_token: some-bearer-token
- url_prefix:
  - http://vmagent-test.default.svc:8429
  name: user-2
  bearer_token: bearer-token-2
- url_prefix:
  - http://vmagent-test.default.svc:8429
  name: user-5
  username: some-user
  password: generated-password
`,
		},
		{
			name: "default cfg with empty selectors",
			args: args{
				vmauth: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user-1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user-2"),
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMAgent{
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
				vmauth: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{SelectAllByDefault: false, UserNamespaceSelector: &metav1.LabelSelector{}},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "default"},
				},
				&v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "monitoring"},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-11",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user-11"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{URL: "http://some-static-15"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-15",
						Namespace: "monitoring",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user-15"),
						BearerToken: ptr.To("bearer-token-10"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: nil,
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix:
  - http://some-static-15
  name: user-11
  bearer_token: bearer
- url_prefix:
  - http://vmagent-test.default.svc:8429
  name: user-15
  bearer_token: bearer-token-10
`,
		},
		{
			name: "with un athorized access and ip_filter ",
			args: args{
				vmauth: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{
						SelectAllByDefault: true,
						UnauthorizedAccessConfig: []victoriametricsv1beta1.VMAuthUnauthorizedPath{
							{
								Paths:                  []string{"/", "/default"},
								URLs:                   []string{"http://route-1", "http://route-2"},
								Hosts:                  []string{"app1\\.my-host\\.com"},
								Headers:                []string{"TenantID: foobar", "X-Forwarded-For:"},
								ResponseHeaders:        []string{"Server:"},
								RetryStatusCodes:       []int{503, 500},
								LoadBalancingPolicy:    ptr.To("first_available"),
								DropSrcPathPrefixParts: ptr.To(1),
								IPFilters: victoriametricsv1beta1.VMUserIPFilters{
									DenyList: []string{
										"127.0.0.1", "192.168.0.0/16",
									},
								},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix:
  - http://some-static
  name: user1
  bearer_token: bearer
- url_prefix:
  - http://vmagent-test.default.svc:8429
  bearer_token: bearer-token-2
unauthorized_user:
  url_map:
  - url_prefix:
    - http://route-1
    - http://route-2
    src_paths:
    - /
    - /default
    src_hosts:
    - app1\.my-host\.com
    headers:
    - 'TenantID: foobar'
    - 'X-Forwarded-For:'
    response_headers:
    - 'Server:'
    retry_status_codes:
    - 503
    - 500
    load_balancing_policy: first_available
    drop_src_path_prefix_parts: 1
    ip_filters:
      deny_list:
      - 127.0.0.1
      - 192.168.0.0/16
`,
		},
		{
			name: "with disabled headers, max concurrent and response headers ",
			args: args{
				vmauth: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{
						SelectAllByDefault: true,
						UnauthorizedAccessConfig: []victoriametricsv1beta1.VMAuthUnauthorizedPath{
							{
								Paths: []string{"/", "/default"},
								URLs:  []string{"http://route-1", "http://route-2"},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:                  ptr.To("user1"),
						BearerToken:           ptr.To("bearer"),
						DisableSecretCreation: true,
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								Static: &victoriametricsv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						BearerToken:           ptr.To("bearer-token-2"),
						MaxConcurrentRequests: ptr.To(500),
						RetryStatusCodes:      []int{400, 500},
						ResponseHeaders:       []string{"H1:V1"},
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `users:
- url_prefix:
  - http://some-static
  name: user1
  bearer_token: bearer
- url_prefix:
  - http://vmagent-test.default.svc:8429
  max_concurrent_requests: 500
  retry_status_codes:
  - 400
  - 500
  response_headers:
  - H1:V1
  bearer_token: bearer-token-2
unauthorized_user:
  url_map:
  - url_prefix:
    - http://route-1
    - http://route-2
    src_paths:
    - /
    - /default
`,
		},
		{
			name: "with cluster discovery and auth",
			args: args{
				vmauth: &victoriametricsv1beta1.VMAuth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmauth",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMAuthSpec{
						SelectAllByDefault: true,
						UnauthorizedAccessConfig: []victoriametricsv1beta1.VMAuthUnauthorizedPath{
							{
								Paths: []string{"/", "/default"},
								URLs:  []string{"http://route-1", "http://route-2"},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-for-cluster",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						Name:                  ptr.To("user1"),
						BearerToken:           ptr.To("bearer"),
						DisableSecretCreation: true,
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Name:      "main-cluster",
									Kind:      "VMCluster/vmselect",
									Namespace: "default",
								},
								TargetRefBasicAuth: &victoriametricsv1beta1.TargetRefBasicAuth{
									Password: v1.SecretKeySelector{
										Key: "password",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "cluster-auth",
										},
									},
									Username: v1.SecretKeySelector{
										Key: "username",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "cluster-auth",
										},
									},
								},
							},
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Name:      "main-cluster",
									Kind:      "VMCluster/vminsert",
									Namespace: "default",
								},
								TargetRefBasicAuth: &victoriametricsv1beta1.TargetRefBasicAuth{
									Password: v1.SecretKeySelector{
										Key: "password",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "cluster-auth",
										},
									},
									Username: v1.SecretKeySelector{
										Key: "username",
										LocalObjectReference: v1.LocalObjectReference{
											Name: "cluster-auth",
										},
									},
								},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "main-cluster",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMClusterSpec{
						VMSelect: &victoriametricsv1beta1.VMSelect{ReplicaCount: ptr.To(int32(10))},
						VMInsert: &victoriametricsv1beta1.VMInsert{ReplicaCount: ptr.To(int32(5))},
					},
				},
				&victoriametricsv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: victoriametricsv1beta1.VMUserSpec{
						GeneratePassword:      true,
						MaxConcurrentRequests: ptr.To(500),
						RetryStatusCodes:      []int{400, 500},
						ResponseHeaders:       []string{"H1:V1"},
						LoadBalancingPolicy:   ptr.To("first_available"),
						MetricLabels: map[string]string{
							"team": "dev",
							"env":  "core",
						},
						TargetRefs: []victoriametricsv1beta1.TargetRef{
							{
								CRD: &victoriametricsv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&victoriametricsv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmuser-user-2",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"username": []byte(`vmuser-user-2`),
						"password": []byte(`generated-1`),
					},
				},
				&v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-auth",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"username": []byte(`some-1`),
						"password": []byte(`some-2`),
					},
				},
			},
			want: `users:
- url_prefix:
  - http://vmagent-test.default.svc:8429
  max_concurrent_requests: 500
  load_balancing_policy: first_available
  metric_labels:
    env: core
    team: dev
  retry_status_codes:
  - 400
  - 500
  response_headers:
  - H1:V1
  username: vmuser-user-2
  password: generated-1
- url_map:
  - url_prefix:
    - http://vmselect-main-cluster.default.svc:8481
    src_paths:
    - /vmui.*
    - /vmui/vmui
    - /graph
    - /prometheus/graph
    - /prometheus/vmui.*
    - /prometheus/api/v1/label.*
    - /graphite.*
    - /prometheus/api/v1/query.*
    - /prometheus/api/v1/rules
    - /prometheus/api/v1/alerts
    - /prometheus/api/v1/metadata
    - /prometheus/api/v1/rules
    - /prometheus/api/v1/series.*
    - /prometheus/api/v1/status.*
    - /prometheus/api/v1/export.*
    - /prometheus/federate
    - /prometheus/api/v1/admin/tsdb/delete_series
    - /admin/tenants
    - /api/v1/status/.*
    - /internal/resetRollupResultCache
    - /prometheus/api/v1/admin/.*
    headers:
    - 'Authorization: Basic c29tZS0xOnNvbWUtMg=='
  - url_prefix:
    - http://vminsert-main-cluster.default.svc:8480
    src_paths:
    - /newrelic/.*
    - /opentelemetry/.*
    - /prometheus/api/v1/write
    - /prometheus/api/v1/import.*
    - /influx/.*
    - /datadog/.*
    headers:
    - 'Authorization: Basic c29tZS0xOnNvbWUtMg=='
  name: user1
  bearer_token: bearer
unauthorized_user:
  url_map:
  - url_prefix:
    - http://route-1
    - http://route-2
    src_paths:
    - /
    - /default
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
			if !assert.Equal(t, tt.want, string(got)) {
				return
			}
			got2, err := buildVMAuthConfig(context.TODO(), testClient, tt.args.vmauth)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildVMAuthConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !assert.Equal(t, tt.want, string(got2)) {
				t.Fatal("idempodent check failed")
			}
		})
	}
}
