package vmauth

import (
	"context"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_genUserCfg(t *testing.T) {
	type args struct {
		user        *vmv1beta1.VMUser
		crdURLCache map[string]string
	}
	tests := []struct {
		name              string
		args              args
		want              string
		predefinedObjects []runtime.Object
		wantErr           bool
	}{
		{
			name: "basic user cfg",
			args: args{
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:     ptr.To("user1"),
						UserName: ptr.To("basic"),
						Password: ptr.To("pass"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{
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
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
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
								CRD: &vmv1beta1.CRDRef{
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
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
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
								URLMapCommon: vmv1beta1.URLMapCommon{
									RequestHeaders: []string{"baz: bar"},
								},
							},
							{
								Static:           &vmv1beta1.StaticRef{URL: "http://vmcluster-remote.mydomain.com:8401"},
								TargetPathSuffix: "/insert/0/prometheus?extra_label=key=value",
								Paths: []string{
									"/",
								},
							},
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VLogs",
									Namespace: "monitoring",
									Name:      "db",
								},
								Paths: []string{"/logs/v1.*"},
							},

							{
								CRD: &vmv1beta1.CRDRef{
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
					"VLogs/monitoring/db":     "http://vlogs-b.monitoring.svc:8482",
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
  - http://vlogs-b.monitoring.svc:8482
  src_paths:
  - /logs/v1.*
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
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
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
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user2"),
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "base",
									Namespace: "monitoring",
								},
								URLMapCommon: vmv1beta1.URLMapCommon{
									RequestHeaders: []string{"X-Scope-OrgID: abc", "X-Scope-Team: baz"},
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
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:     ptr.To("user1"),
						UserName: ptr.To("basic"),
						Password: ptr.To("pass"),
						VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
							IPFilters: vmv1beta1.VMUserIPFilters{
								AllowList: []string{"127.0.0.1"},
							},
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{
									URL: "http://vmselect",
								},
								Paths: []string{
									"/select/0/prometheus",
									"/select/0/graphite",
								},
							},
							{
								Static: &vmv1beta1.StaticRef{
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
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:     ptr.To("user1"),
						UserName: ptr.To("basic"),
						Password: ptr.To("pass"),
						VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
							Headers:               []string{"H1:V1", "H2:V2"},
							ResponseHeaders:       []string{"RH1:V3", "RH2:V4"},
							MaxConcurrentRequests: ptr.To(400),
							RetryStatusCodes:      []int{502, 503},
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{
									URL: "http://vmselect",
								},
								Paths: []string{
									"/select/0/prometheus",
									"/select/0/graphite",
								},
								URLMapCommon: vmv1beta1.URLMapCommon{
									RequestHeaders:  []string{"H1:V2", "H2:V3"},
									ResponseHeaders: []string{"RH1:V6", "RH2:V7"},
								},
							},
							{
								Static: &vmv1beta1.StaticRef{
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
headers:
- H1:V1
- H2:V2
response_headers:
- RH1:V3
- RH2:V4
retry_status_codes:
- 502
- 503
max_concurrent_requests: 400
username: basic
password: pass
`,
		},
		{
			name: "with all URLMapCommon options and tls_insecure_skip_verify",
			args: args{
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:     ptr.To("user1"),
						UserName: ptr.To("basic"),
						Password: ptr.To("pass"),
						VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
							LoadBalancingPolicy:    ptr.To("first_available"),
							DropSrcPathPrefixParts: ptr.To(1),
							TLSConfig: &vmv1beta1.TLSConfig{
								InsecureSkipVerify: true,
							},
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{
									URL: "http://vmselect",
								},
								Paths: []string{
									"/select/0/prometheus",
									"/select/0/graphite",
								},
								URLMapCommon: vmv1beta1.URLMapCommon{
									SrcQueryArgs:           []string{"foo=bar"},
									SrcHeaders:             []string{"H1:V1"},
									DiscoverBackendIPs:     ptr.To(true),
									RequestHeaders:         []string{"X-Scope-OrgID: abc"},
									ResponseHeaders:        []string{"RH1:V3"},
									RetryStatusCodes:       []int{502, 503},
									LoadBalancingPolicy:    ptr.To("first_available"),
									DropSrcPathPrefixParts: ptr.To(2),
								},
							},
							{
								Static: &vmv1beta1.StaticRef{
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
  discover_backend_ips: true
  src_headers:
  - H1:V1
  src_query_args:
  - foo=bar
  headers:
  - 'X-Scope-OrgID: abc'
  response_headers:
  - RH1:V3
  retry_status_codes:
  - 502
  - 503
  drop_src_path_prefix_parts: 2
  load_balancing_policy: first_available
- url_prefix:
  - http://vminsert
  src_paths:
  - /insert/0/prometheus
name: user1
tls_insecure_skip_verify: true
load_balancing_policy: first_available
drop_src_path_prefix_parts: 1
username: basic
password: pass
`,
		},
		{
			name: "with metric_labels",
			args: args{
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:     ptr.To("user1"),
						UserName: ptr.To("basic"),
						Password: ptr.To("pass"),
						MetricLabels: map[string]string{
							"foo": "bar",
							"buz": "qux",
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://localhost:8435"},
							},
						},
					},
				},
			},
			want: `url_prefix:
- http://localhost:8435
name: user1
metric_labels:
  buz: qux
  foo: bar
username: basic
password: pass
`,
		},
		{
			name: "with vl crd refs",
			args: args{
				user: &vmv1beta1.VMUser{
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("secret-token"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VLAgent",
									Name:      "collector",
									Namespace: "monitoring",
								},
								Paths: []string{
									"/insert/jsonline",
								},
							},
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VLSingle",
									Namespace: "monitoring",
									Name:      "db",
								},
							},
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VLCluster/vlinsert",
									Name:      "main-cluster",
									Namespace: "monitoring",
								},
								Paths: []string{
									"/insert/logstash",
								},
							},
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VLCluster/vlinsert",
									Name:      "main-cluster",
									Namespace: "monitoring",
								},
								Paths: []string{
									"/select/.*",
								},
							},
						},
					},
				},
				crdURLCache: map[string]string{
					"VLAgent/monitoring/collector":                "http://vlagent-base.monitoring.svc:9429",
					"VLSingle/monitoring/db":                      "http://vlsingle-db.monitoring.svc:9428",
					"VLCluster/vlinsert/monitoring/main-cluster":  "http://vlinsert-main-cluster.monitoring.svc:9401",
					"VLCluster/vlselect/monitoring/main-cluster":  "http://vlselect-main-cluster.monitoring.svc:9401",
					"VLCluster/vlstorage/monitoring/main-cluster": "http://vlstorage-main-cluster.monitoring.svc:9401",
				},
			},
			want: `url_map:
- url_prefix:
  - http://vlagent-base.monitoring.svc:9429
  src_paths:
  - /insert/jsonline
- url_prefix:
  - http://vlsingle-db.monitoring.svc:9428
  src_paths:
  - /.*
- url_prefix:
  - http://vlinsert-main-cluster.monitoring.svc:9401
  src_paths:
  - /insert/logstash
- url_prefix:
  - http://vlinsert-main-cluster.monitoring.svc:9401
  src_paths:
  - /select/.*
name: user1
bearer_token: secret-token
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-auth",
					Namespace: "default",
				},
			}
			ctx := context.TODO()
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			ac := getAssetsCache(ctx, fclient, cr)
			got, err := genUserCfg(tt.args.user, tt.args.crdURLCache, cr, ac)
			if (err != nil) != tt.wantErr {
				t.Errorf("genUserCfg() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			szd, err := yaml.Marshal(got)
			if err != nil {
				t.Fatalf("cannot serialize result: %v", err)
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
				t.Errorf("genPassword() password cannot be the same, got1 = %v got2 %v", got1, got2)
			}
		})
	}
}

func Test_selectVMUserSecrets(t *testing.T) {
	tests := []struct {
		name                string
		vmUsers             *skipableVMUsers
		wantToCreateSecrets []string
		wantToUpdateSecrets []string
		wantErr             bool
		predefinedObjects   []runtime.Object
	}{
		{
			name: "want 1 updateSecret",
			vmUsers: &skipableVMUsers{
				users: []*vmv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "not-exist",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist", Namespace: "default"},
				},
			},
			wantToUpdateSecrets: []string{"vmuser-exist"},
			wantToCreateSecrets: []string{"vmuser-not-exist"},
		},
		{
			name: "want 1 updateSecret",
			vmUsers: &skipableVMUsers{
				users: []*vmv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "must-not-exist",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{
							BearerToken:           ptr.To("some-bearer"),
							DisableSecretCreation: true,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "not-exists-must-create",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exists",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exists", Namespace: "default"},
				},
			},
			wantToUpdateSecrets: []string{"vmuser-exists"},
			wantToCreateSecrets: []string{"vmuser-not-exists-must-create"},
		},
		{
			name: "want nothing",
			vmUsers: &skipableVMUsers{
				users: []*vmv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist-with-generated",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{
							GeneratePassword: true,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist-hardcoded",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-with-generated", Namespace: "default"},
					Data:       map[string][]byte{"username": []byte(`vmuser-exist-with-generated`), "password": []byte(`generated`)},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-hardcoded", Namespace: "default"},
					Data:       map[string][]byte{"bearerToken": []byte(`some-bearer`)},
				},
			},
			wantToUpdateSecrets: []string{},
			wantToCreateSecrets: []string{},
		},
		{
			name: "update secret value",
			vmUsers: &skipableVMUsers{
				users: []*vmv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist-with-generated",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{
							GeneratePassword: true,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "exist-to-update",
							Namespace: "default",
						},
						Spec: vmv1beta1.VMUserSpec{Password: ptr.To("some-new-password"), UserName: ptr.To("some-user")},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-with-generated", Namespace: "default"},
					Data:       map[string][]byte{"password": []byte(`generated`)},
				},
				&corev1.Secret{
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
			ctx := context.TODO()
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			cr := &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
			}
			ac := getAssetsCache(ctx, testClient, cr)
			got, got1, err := addAuthCredentialsBuildSecrets(tt.vmUsers, ac)
			if (err != nil) != tt.wantErr {
				t.Errorf("selectVMUserSecrets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			secretFound := func(src []*corev1.Secret, wantName string) bool {
				for i := range src {
					s := src[i]
					if s.Name == wantName {
						return true
					}
				}
				return false
			}
			joinSecretNames := func(src []*corev1.Secret) string {
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

func Test_buildConfig(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1beta1.VMAuth
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "simple cfg",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
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
			name: "simple cfg with duplicated users",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "user-1",
						Namespace:         "default",
						CreationTimestamp: metav1.Time{Time: time.Unix(123, 0)},
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "user-1-duplicate",
						Namespace:         "default",
						CreationTimestamp: metav1.Time{Time: time.Unix(150, 0)},
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
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
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{SelectAllByDefault: true},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:     ptr.To("user-1"),
						UserName: ptr.To("some-user"),
						PasswordRef: &corev1.SecretKeySelector{
							Key: "password",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "generated-secret",
							},
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
								URLMapCommon: vmv1beta1.URLMapCommon{
									RequestHeaders: []string{"baz: bar"},
								},
								TargetRefBasicAuth: &vmv1beta1.TargetRefBasicAuth{
									Username: corev1.SecretKeySelector{
										Key: "username",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "backend-auth-secret",
										},
									},
									Password: corev1.SecretKeySelector{
										Key: "password",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "backend-auth-secret",
										},
									},
								},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-15",
						Namespace: "monitoring",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user-15"),
						BearerToken: ptr.To("bearer-token-10"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: nil,
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "generated-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"password": []byte(`generated-password`), "token": []byte(`some-bearer-token`)},
				},
				&corev1.Secret{
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
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{SelectAllByDefault: true},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user-1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user-2"),
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-5",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:     ptr.To("user-5"),
						UserName: ptr.To("some-user"),
						PasswordRef: &corev1.SecretKeySelector{
							Key: "password",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "generated-secret",
							},
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-10",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name: ptr.To("user-10"),
						TokenRef: &corev1.SecretKeySelector{
							Key: "token",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "generated-secret",
							},
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "generated-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"password": []byte(`generated-password`), "token": []byte(`some-bearer-token`)},
				},
				&vmv1beta1.VMAgent{
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
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user-1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user-2"),
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
			},
			want: `{}
`,
		},
		{
			name: "vmauth ns selector",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{SelectAllByDefault: false, UserNamespaceSelector: &metav1.LabelSelector{}},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "default"},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "monitoring"},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-11",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user-11"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static-15"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-15",
						Namespace: "monitoring",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user-15"),
						BearerToken: ptr.To("bearer-token-10"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: nil,
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
								Hosts: []string{"host.com"},
								URLMapCommon: vmv1beta1.URLMapCommon{
									// SrcQueryArgs&SrcHeaders here will be skipped cause there is only one default route
									SrcQueryArgs:        []string{"db=foo"},
									SrcHeaders:          []string{"TenantID: 123:456"},
									DiscoverBackendIPs:  ptr.To(true),
									RequestHeaders:      []string{"X-Scope-OrgID: abc"},
									ResponseHeaders:     []string{"X-Server-Hostname: a"},
									RetryStatusCodes:    []int{500, 502},
									LoadBalancingPolicy: ptr.To("first_available"),
								},
								TargetPathSuffix: "/prometheus?extra_label=key=value",
							},
						},
						VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
							DefaultURLs: []string{"https://default1:8888/unsupported_url_handler", "https://default2:8888/unsupported_url_handler"},
							TLSConfig: &vmv1beta1.TLSConfig{
								CA: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret-store",
										},
										Key: "ca",
									},
								},
								Cert: vmv1beta1.SecretOrConfigMap{
									Secret: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "secret-store",
										},
										Key: "cert",
									},
								},
								KeyFile:            "/path/to/tls/key",
								ServerName:         "foo.bar.com",
								InsecureSkipVerify: true,
							},
							IPFilters: vmv1beta1.VMUserIPFilters{
								AllowList: []string{"10.0.0.0/24", "1.2.3.4"},
								DenyList:  []string{"10.0.0.42"},
							},
							MaxConcurrentRequests: ptr.To(180),
						},
					},
				},
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "secret-store",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"cert": []byte("---PEM---"),
						"ca":   []byte("---PEM-CA"),
					},
				},
			},
			want: `users:
- url_prefix:
  - http://some-static-15
  name: user-11
  bearer_token: bearer
- url_prefix:
  - http://vmagent-test.default.svc:8429/prometheus?extra_label=key%3Dvalue
  headers:
  - 'X-Scope-OrgID: abc'
  response_headers:
  - 'X-Server-Hostname: a'
  discover_backend_ips: true
  retry_status_codes:
  - 500
  - 502
  load_balancing_policy: first_available
  name: user-15
  default_url:
  - https://default1:8888/unsupported_url_handler
  - https://default2:8888/unsupported_url_handler
  tls_insecure_skip_verify: true
  tls_ca_file: /opt/vmauth/config/default_secret-store_ca
  tls_cert_file: /opt/vmauth/config/default_secret-store_cert
  tls_key_file: /path/to/tls/key
  tls_server_name: foo.bar.com
  ip_filters:
    allow_list:
    - 10.0.0.0/24
    - 1.2.3.4
    deny_list:
    - 10.0.0.42
  max_concurrent_requests: 180
  bearer_token: bearer-token-10
`,
		},
		{
			name: "with full unauthorized access and ip_filter",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
					UnauthorizedAccessConfig: []vmv1beta1.UnauthorizedAccessConfigURLMap{
						{
							SrcPaths:  []string{"/api/v1/query", "/api/v1/query_range", "/api/v1/label/[^/]+/values"},
							SrcHosts:  []string{"app1.my-host.com"},
							URLPrefix: []string{"http://vmselect1:8481/select/42/prometheus", "http://vmselect2:8481/select/42/prometheus"},
							URLMapCommon: vmv1beta1.URLMapCommon{
								SrcQueryArgs:        []string{"db=foo"},
								SrcHeaders:          []string{"TenantID: 123:456"},
								DiscoverBackendIPs:  ptr.To(true),
								RequestHeaders:      []string{"X-Scope-OrgID: abc"},
								ResponseHeaders:     []string{"X-Server-Hostname: a"},
								RetryStatusCodes:    []int{500, 502},
								LoadBalancingPolicy: ptr.To("first_available"),
							},
						},
						{
							SrcPaths:  []string{"/app1/.*"},
							URLPrefix: []string{"http://app1-backend/"},
							URLMapCommon: vmv1beta1.URLMapCommon{
								DropSrcPathPrefixParts: ptr.To(1),
							},
						},
					},
					VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
						DefaultURLs: []string{"https://default1:8888/unsupported_url_handler", "https://default2:8888/unsupported_url_handler"},
						TLSConfig: &vmv1beta1.TLSConfig{
							CAFile:             "/path/to/tls/root/ca",
							CertFile:           "/path/to/tls/cert",
							KeyFile:            "/path/to/tls/key",
							ServerName:         "foo.bar.com",
							InsecureSkipVerify: true,
						},
						IPFilters: vmv1beta1.VMUserIPFilters{
							AllowList: []string{"192.168.0.1/24"},
							DenyList:  []string{"10.0.0.43"},
						},
						DiscoverBackendIPs:     ptr.To(false),
						Headers:                []string{"X-Scope-OrgID: cba"},
						ResponseHeaders:        []string{"X-Server-Hostname: b"},
						RetryStatusCodes:       []int{503},
						LoadBalancingPolicy:    ptr.To("least_loaded"),
						MaxConcurrentRequests:  ptr.To(150),
						DropSrcPathPrefixParts: ptr.To(2),
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
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
  - src_paths:
    - /api/v1/query
    - /api/v1/query_range
    - /api/v1/label/[^/]+/values
    src_hosts:
    - app1.my-host.com
    url_prefix:
    - http://vmselect1:8481/select/42/prometheus
    - http://vmselect2:8481/select/42/prometheus
    src_query_args:
    - db=foo
    src_headers:
    - 'TenantID: 123:456'
    headers:
    - 'X-Scope-OrgID: abc'
    response_headers:
    - 'X-Server-Hostname: a'
    discover_backend_ips: true
    retry_status_codes:
    - 500
    - 502
    load_balancing_policy: first_available
  - src_paths:
    - /app1/.*
    url_prefix:
    - http://app1-backend/
    drop_src_path_prefix_parts: 1
  default_url:
  - https://default1:8888/unsupported_url_handler
  - https://default2:8888/unsupported_url_handler
  tls_insecure_skip_verify: true
  tls_ca_file: /path/to/tls/root/ca
  tls_cert_file: /path/to/tls/cert
  tls_key_file: /path/to/tls/key
  tls_server_name: foo.bar.com
  ip_filters:
    allow_list:
    - 192.168.0.1/24
    deny_list:
    - 10.0.0.43
  headers:
  - 'X-Scope-OrgID: cba'
  response_headers:
  - 'X-Server-Hostname: b'
  discover_backend_ips: false
  retry_status_codes:
  - 503
  max_concurrent_requests: 150
  load_balancing_policy: least_loaded
  drop_src_path_prefix_parts: 2
`,
		},
		{
			name: "with disabled headers, max concurrent and response headers ",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
					UnauthorizedAccessConfig: []vmv1beta1.UnauthorizedAccessConfigURLMap{
						{
							SrcPaths:  []string{"/", "/default"},
							URLPrefix: []string{"http://route-1", "http://route-2"},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:                  ptr.To("user1"),
						BearerToken:           ptr.To("bearer"),
						DisableSecretCreation: true,
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-2"),
						VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
							MaxConcurrentRequests: ptr.To(500),
							RetryStatusCodes:      []int{400, 500},
							ResponseHeaders:       []string{"H1:V1"},
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
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
  response_headers:
  - H1:V1
  retry_status_codes:
  - 400
  - 500
  max_concurrent_requests: 500
  bearer_token: bearer-token-2
unauthorized_user:
  url_map:
  - src_paths:
    - /
    - /default
    url_prefix:
    - http://route-1
    - http://route-2
`,
		},
		{
			name: "with cluster discovery and auth",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
					UnauthorizedAccessConfig: []vmv1beta1.UnauthorizedAccessConfigURLMap{
						{
							SrcPaths:  []string{"/", "/default"},
							URLPrefix: []string{"http://route-1", "http://route-2"},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-for-cluster",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:                  ptr.To("user1"),
						BearerToken:           ptr.To("bearer"),
						DisableSecretCreation: true,
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Name:      "main-cluster",
									Kind:      "VMCluster/vmselect",
									Namespace: "default",
								},
								TargetRefBasicAuth: &vmv1beta1.TargetRefBasicAuth{
									Password: corev1.SecretKeySelector{
										Key: "password",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "cluster-auth",
										},
									},
									Username: corev1.SecretKeySelector{
										Key: "username",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "cluster-auth",
										},
									},
								},
							},
							{
								CRD: &vmv1beta1.CRDRef{
									Name:      "main-cluster",
									Kind:      "VMCluster/vminsert",
									Namespace: "default",
								},
								TargetRefBasicAuth: &vmv1beta1.TargetRefBasicAuth{
									Password: corev1.SecretKeySelector{
										Key: "password",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "cluster-auth",
										},
									},
									Username: corev1.SecretKeySelector{
										Key: "username",
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "cluster-auth",
										},
									},
								},
							},
						},
						VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
							DefaultURLs: []string{"https://default1:8888/unsupported_url_handler", "https://default2:8888/unsupported_url_handler"},
							TLSConfig: &vmv1beta1.TLSConfig{
								CAFile:             "/path/to/tls/root/ca",
								CertFile:           "/path/to/tls/cert",
								KeyFile:            "/path/to/tls/key",
								ServerName:         "foo.bar.com",
								InsecureSkipVerify: true,
							},
							IPFilters: vmv1beta1.VMUserIPFilters{
								AllowList: []string{"192.168.0.1/24"},
								DenyList:  []string{"10.0.0.43"},
							},
							DiscoverBackendIPs:     ptr.To(false),
							Headers:                []string{"X-Scope-OrgID: cba"},
							ResponseHeaders:        []string{"X-Server-Hostname: b"},
							RetryStatusCodes:       []int{503},
							LoadBalancingPolicy:    ptr.To("least_loaded"),
							MaxConcurrentRequests:  ptr.To(150),
							DropSrcPathPrefixParts: ptr.To(2),
						},
					},
				},
				&vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "main-cluster",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMClusterSpec{
						VMSelect: &vmv1beta1.VMSelect{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To(int32(10)),
							},
						},
						VMInsert: &vmv1beta1.VMInsert{
							CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
								ReplicaCount: ptr.To(int32(5)),
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						GeneratePassword: true,
						VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
							MaxConcurrentRequests: ptr.To(500),
							RetryStatusCodes:      []int{400, 500},
							ResponseHeaders:       []string{"H1:V1"},
							LoadBalancingPolicy:   ptr.To("first_available"),
						},
						MetricLabels: map[string]string{
							"team": "dev",
							"env":  "core",
						},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmuser-user-2",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"username": []byte(`vmuser-user-2`),
						"password": []byte(`generated-1`),
					},
				},
				&corev1.Secret{
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
  response_headers:
  - H1:V1
  retry_status_codes:
  - 400
  - 500
  max_concurrent_requests: 500
  load_balancing_policy: first_available
  metric_labels:
    env: core
    team: dev
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
  default_url:
  - https://default1:8888/unsupported_url_handler
  - https://default2:8888/unsupported_url_handler
  tls_insecure_skip_verify: true
  tls_ca_file: /path/to/tls/root/ca
  tls_cert_file: /path/to/tls/cert
  tls_key_file: /path/to/tls/key
  tls_server_name: foo.bar.com
  ip_filters:
    allow_list:
    - 192.168.0.1/24
    deny_list:
    - 10.0.0.43
  headers:
  - 'X-Scope-OrgID: cba'
  response_headers:
  - 'X-Server-Hostname: b'
  discover_backend_ips: false
  retry_status_codes:
  - 503
  max_concurrent_requests: 150
  load_balancing_policy: least_loaded
  drop_src_path_prefix_parts: 2
  bearer_token: bearer
unauthorized_user:
  url_map:
  - src_paths:
    - /
    - /default
    url_prefix:
    - http://route-1
    - http://route-2
`,
		},
		{
			name: "with duplicated users and broken links",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "user-1",
						Namespace:         "default",
						CreationTimestamp: metav1.Time{Time: time.Unix(123, 0)},
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "user-5-duplicate",
						Namespace:         "default",
						CreationTimestamp: metav1.Time{Time: time.Unix(150, 0)},
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "user-6-duplicate",
						Namespace:         "default",
						CreationTimestamp: metav1.Time{Time: time.Unix(135, 0)},
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},

				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-3-broken-crd-link",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-17"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test-not-found",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-3-missing-urls",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-15"),
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-10-non-exist-secret-ref",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						TokenRef: &corev1.SecretKeySelector{},
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{
									URL: "http://some",
								},
							},
						},
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
			name: "with full unauthorizedUserAccessSpec",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
					UnauthorizedUserAccessSpec: &vmv1beta1.VMAuthUnauthorizedUserAccessSpec{
						URLMap: []vmv1beta1.UnauthorizedAccessConfigURLMap{
							{
								SrcPaths:  []string{"/api/v1/query", "/api/v1/query_range", "/api/v1/label/[^/]+/values"},
								SrcHosts:  []string{"app1.my-host.com"},
								URLPrefix: []string{"http://vmselect1:8481/select/42/prometheus", "http://vmselect2:8481/select/42/prometheus"},
								URLMapCommon: vmv1beta1.URLMapCommon{
									SrcQueryArgs:        []string{"db=foo"},
									SrcHeaders:          []string{"TenantID: 123:456"},
									DiscoverBackendIPs:  ptr.To(true),
									RequestHeaders:      []string{"X-Scope-OrgID: abc"},
									ResponseHeaders:     []string{"X-Server-Hostname: a"},
									RetryStatusCodes:    []int{500, 502},
									LoadBalancingPolicy: ptr.To("first_available"),
								},
							},
							{
								SrcPaths:  []string{"/app1/.*"},
								URLPrefix: []string{"http://app1-backend/"},
								URLMapCommon: vmv1beta1.URLMapCommon{
									DropSrcPathPrefixParts: ptr.To(1),
								},
							},
						},
						MetricLabels: map[string]string{"label": "value"},
						URLPrefix:    []string{"http://some-url"},
						VMUserConfigOptions: vmv1beta1.VMUserConfigOptions{
							DefaultURLs: []string{"https://default1:8888/unsupported_url_handler", "https://default2:8888/unsupported_url_handler"},
							TLSConfig: &vmv1beta1.TLSConfig{
								CAFile:             "/path/to/tls/root/ca",
								CertFile:           "/path/to/tls/cert",
								KeyFile:            "/path/to/tls/key",
								ServerName:         "foo.bar.com",
								InsecureSkipVerify: true,
							},
							IPFilters: vmv1beta1.VMUserIPFilters{
								AllowList: []string{"192.168.0.1/24"},
								DenyList:  []string{"10.0.0.43"},
							},
							DiscoverBackendIPs:     ptr.To(false),
							Headers:                []string{"X-Scope-OrgID: cba"},
							ResponseHeaders:        []string{"X-Server-Hostname: b"},
							RetryStatusCodes:       []int{503},
							LoadBalancingPolicy:    ptr.To("least_loaded"),
							MaxConcurrentRequests:  ptr.To(150),
							DropSrcPathPrefixParts: ptr.To(2),
							DumpRequestOnErrors:    ptr.To(true),
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						Name:        ptr.To("user1"),
						BearerToken: ptr.To("bearer"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user-2",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-token-2"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								CRD: &vmv1beta1.CRDRef{
									Kind:      "VMAgent",
									Name:      "test",
									Namespace: "default",
								},
								Paths: []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMAgent{
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
  - src_paths:
    - /api/v1/query
    - /api/v1/query_range
    - /api/v1/label/[^/]+/values
    src_hosts:
    - app1.my-host.com
    url_prefix:
    - http://vmselect1:8481/select/42/prometheus
    - http://vmselect2:8481/select/42/prometheus
    src_query_args:
    - db=foo
    src_headers:
    - 'TenantID: 123:456'
    headers:
    - 'X-Scope-OrgID: abc'
    response_headers:
    - 'X-Server-Hostname: a'
    discover_backend_ips: true
    retry_status_codes:
    - 500
    - 502
    load_balancing_policy: first_available
  - src_paths:
    - /app1/.*
    url_prefix:
    - http://app1-backend/
    drop_src_path_prefix_parts: 1
  url_prefix: http://some-url
  metric_labels:
    label: value
  default_url:
  - https://default1:8888/unsupported_url_handler
  - https://default2:8888/unsupported_url_handler
  tls_insecure_skip_verify: true
  tls_ca_file: /path/to/tls/root/ca
  tls_cert_file: /path/to/tls/cert
  tls_key_file: /path/to/tls/key
  tls_server_name: foo.bar.com
  ip_filters:
    allow_list:
    - 192.168.0.1/24
    deny_list:
    - 10.0.0.43
  headers:
  - 'X-Scope-OrgID: cba'
  response_headers:
  - 'X-Server-Hostname: b'
  discover_backend_ips: false
  retry_status_codes:
  - 503
  max_concurrent_requests: 150
  load_balancing_policy: least_loaded
  drop_src_path_prefix_parts: 2
  dump_request_on_errors: true
`,
		},
		{
			name: "with unsorted duplicates",
			cr: &vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmauth",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAuthSpec{
					SelectAllByDefault: true,
				},
			},
			predefinedObjects: []runtime.Object{
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default-1",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-2"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static-2"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-1"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static-1"},
								Paths:  []string{"/"},
							},
						},
					},
				},
				&vmv1beta1.VMUser{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "user",
						Namespace: "default-2",
					},
					Spec: vmv1beta1.VMUserSpec{
						BearerToken: ptr.To("bearer-3"),
						TargetRefs: []vmv1beta1.TargetRef{
							{
								Static: &vmv1beta1.StaticRef{URL: "http://some-static-3"},
								Paths:  []string{"/"},
							},
						},
					},
				},

				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default-1",
					},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "default-2",
					},
				},
			},
			want: `users:
- url_prefix:
  - http://some-static-2
  bearer_token: bearer-2
- url_prefix:
  - http://some-static-1
  bearer_token: bearer-1
- url_prefix:
  - http://some-static-3
  bearer_token: bearer-3
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.TODO()
			testClient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			// fetch exist users for vmauth.
			sus, err := selectVMUsers(ctx, testClient, tt.cr)
			if err != nil {
				t.Fatalf("unexpected error at selectVMUsers: %s", err)
			}
			rand.Shuffle(len(sus.users), func(i, j int) {
				sus.users[i], sus.users[j] = sus.users[j], sus.users[i]
			})
			ac := getAssetsCache(ctx, testClient, tt.cr)

			got, err := buildConfig(ctx, testClient, tt.cr, sus, ac)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !assert.Equal(t, tt.want, string(got)) {
				return
			}
			// fetch exist users for vmauth.
			sus, err = selectVMUsers(ctx, testClient, tt.cr)
			if err != nil {
				t.Fatalf("unexpected error at selectVMUsers: %s", err)
			}
			got2, err := buildConfig(ctx, testClient, tt.cr, sus, ac)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !assert.Equal(t, tt.want, string(got2)) {
				t.Fatal("idempotent check failed")
			}
		})
	}
}
