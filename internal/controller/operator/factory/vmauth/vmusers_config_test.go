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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_genUserCfg(t *testing.T) {

	type opts struct {
		user              *vmv1beta1.VMUser
		crdURLCache       map[string]string
		predefinedObjects []runtime.Object
		want              string
	}
	f := func(o opts) {
		t.Helper()
		cr := &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-auth",
				Namespace: "default",
			},
		}
		ctx := context.TODO()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, cr)
		got, err := genUserCfg(o.user, o.crdURLCache, cr, ac)
		assert.NoError(t, err)
		szd, err := yaml.Marshal(got)
		assert.NoError(t, err)
		assert.Equal(t, o.want, string(szd))
	}

	// with vmcluster crd and specific tenant suffix
	f(opts{
		user: &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				Name:        ptr.To("user1"),
				BearerToken: ptr.To("secret-token"),
				TargetRefs: []vmv1beta1.TargetRef{
					{
						CRD: &vmv1beta1.CRDRef{
							Kind:      "VMCluster/vminsert",
							Name:      "vminsert",
							Namespace: "monitoring",
						},
						TargetPathSuffix: "/insert/1",
					},
					{
						CRD: &vmv1beta1.CRDRef{
							Kind:      "VMCluster/vmselect",
							Namespace: "monitoring",
							Name:      "vmselect",
						},
						TargetPathSuffix: "/select/1",
					},
				},
			},
		},
		crdURLCache: map[string]string{
			"VMCluster/vminsert/monitoring/vminsert": "http://vminsert.monitoring.svc:8481",
			"VMCluster/vmselect/monitoring/vmselect": "http://vmselect.monitoring.svc:8482",
		},
		want: `url_map:
- url_prefix:
  - http://vminsert.monitoring.svc:8481/insert/1
  src_paths:
  - /newrelic/.*
  - /opentelemetry/.*
  - /prometheus/api/v1/write
  - /prometheus/api/v1/import.*
  - /influx/.*
  - /datadog/.*
- url_prefix:
  - http://vmselect.monitoring.svc:8482/select/1
  src_paths:
  - /vmui.*
  - /vmui.*
  - /graph.*
  - /prometheus/graph.*
  - /prometheus/vmui.*
  - /prometheus/api/v1/label.*
  - /prometheus/api/v1/query.*
  - /prometheus/api/v1/rules
  - /prometheus/api/v1/alerts
  - /prometheus/api/v1/metadata
  - /prometheus/api/v1/series.*
  - /prometheus/api/v1/status.*
  - /prometheus/api/v1/export.*
  - /prometheus/federate
  - /admin/tenants
  - /api/v1/status/.*
  - /api/v1/rules
  - /internal/resetRollupResultCache
  - /prometheus/api/v1/admin/.*
  - /prometheus.*-debug
  - /prometheus/prettify-query
  - /prometheus/api/v1/notifiers
  - /prometheus/api/v1/query_exemplars
name: user1
bearer_token: secret-token
`,
	})

	// with cluster CRD and empty targetSuffix
	f(opts{
		user: &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				Name:        ptr.To("user1"),
				BearerToken: ptr.To("secret-token"),
				TargetRefs: []vmv1beta1.TargetRef{
					{
						CRD: &vmv1beta1.CRDRef{
							Kind:      "VMCluster/vminsert",
							Name:      "vminsert",
							Namespace: "monitoring",
						},
					},
					{
						CRD: &vmv1beta1.CRDRef{
							Kind:      "VMCluster/vmselect",
							Namespace: "monitoring",
							Name:      "vmselect",
						},
						QueryArgs: []vmv1beta1.QueryArg{
							{
								Name:   "foo",
								Values: []string{"bar"},
							},
						},
					},
				},
			},
		},
		crdURLCache: map[string]string{
			"VMCluster/vminsert/monitoring/vminsert": "http://vminsert.monitoring.svc:8481",
			"VMCluster/vmselect/monitoring/vmselect": "http://vmselect.monitoring.svc:8482",
		},
		want: `url_map:
- url_prefix:
  - http://vminsert.monitoring.svc:8481
  src_paths:
  - /insert/.*
- url_prefix:
  - http://vmselect.monitoring.svc:8482?foo=bar
  src_paths:
  - /select/.*
  - /admin/.*
name: user1
bearer_token: secret-token
`,
	})
	// with cluster CRD and mixed targetSuffix
	f(opts{
		user: &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				Name:        ptr.To("user1"),
				BearerToken: ptr.To("secret-token"),
				TargetRefs: []vmv1beta1.TargetRef{
					{
						CRD: &vmv1beta1.CRDRef{
							Kind:      "VMCluster/vminsert",
							Name:      "vminsert",
							Namespace: "monitoring",
						},
						Paths:            []string{"/"},
						TargetPathSuffix: "/insert/1",
					},
					{
						CRD: &vmv1beta1.CRDRef{
							Kind:      "VMCluster/vmselect",
							Namespace: "monitoring",
							Name:      "vmselect",
						},
					},
				},
			},
		},
		crdURLCache: map[string]string{
			"VMCluster/vminsert/monitoring/vminsert": "http://vminsert.monitoring.svc:8481",
			"VMCluster/vmselect/monitoring/vmselect": "http://vmselect.monitoring.svc:8482",
		},
		want: `url_map:
- url_prefix:
  - http://vminsert.monitoring.svc:8481/insert/1
  src_paths:
  - /.*
- url_prefix:
  - http://vmselect.monitoring.svc:8482
  src_paths:
  - /select/.*
  - /admin/.*
name: user1
bearer_token: secret-token
`,
	})

	// basic user cfg
	f(opts{
		user: &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				Name:     ptr.To("user1"),
				Username: ptr.To("basic"),
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
	})

	// with crd
	f(opts{
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
	})

	// with crd and custom suffix
	f(opts{
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
	})

	// with one target
	f(opts{
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
		want: `url_prefix:
- http://vmagent-base.monitoring.svc:8429
name: user1
bearer_token: secret-token
`,
	})

	// with target headers
	f(opts{
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
		want: `url_prefix:
- http://vmagent-base.monitoring.svc:8429
headers:
- 'X-Scope-OrgID: abc'
- 'X-Scope-Team: baz'
name: user2
bearer_token: secret-token
`,
	})

	// with ip filters and multiple targets
	f(opts{
		user: &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				Name:     ptr.To("user1"),
				Username: ptr.To("basic"),
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
	})

	// with headers and max concurrent
	f(opts{
		user: &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				Name:     ptr.To("user1"),
				Username: ptr.To("basic"),
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
	})

	// with all URLMapCommon options and tls_insecure_skip_verify
	f(opts{
		user: &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				Name:     ptr.To("user1"),
				Username: ptr.To("basic"),
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
	})

	// with metric_labels
	f(opts{
		user: &vmv1beta1.VMUser{
			Spec: vmv1beta1.VMUserSpec{
				Name:     ptr.To("user1"),
				Username: ptr.To("basic"),
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
		want: `url_prefix:
- http://localhost:8435
name: user1
metric_labels:
  buz: qux
  foo: bar
username: basic
password: pass
`,
	})

	// with vl crd refs
	f(opts{
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
							Kind:      "VLCluster/vlselect",
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
  - http://vlselect-main-cluster.monitoring.svc:9401
  src_paths:
  - /select/.*
name: user1
bearer_token: secret-token
`,
	})
}

func Test_genPassword(t *testing.T) {
	f := func() {
		t.Helper()
		got1, err := genPassword()
		assert.NoError(t, err)
		got2, err := genPassword()
		assert.NoError(t, err)
		assert.NotEqual(t, got1, got2)
	}

	// simple test
	f()
}

func Test_selectVMUserSecrets(t *testing.T) {
	type opts struct {
		wantToCreateSecrets []string
		wantToUpdateSecrets []string
		predefinedObjects   []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		testClient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		cr := &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmauth",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAuthSpec{
				UserNamespaceSelector: &metav1.LabelSelector{},
			},
		}
		pos, err := selectUsers(ctx, testClient, cr)
		assert.NoError(t, err)
		ac := getAssetsCache(ctx, testClient, cr)
		got, got1, err := pos.addAuthCredentialsBuildSecrets(ac)
		assert.NoError(t, err)
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
		if len(o.wantToCreateSecrets) != len(got) {
			t.Errorf("not expected count of want=%d and got=%d to create secrets, got=%q", len(o.wantToCreateSecrets), len(got), joinSecretNames(got))
		}
		if len(o.wantToUpdateSecrets) != len(got1) {
			t.Errorf("not expected count of want=%d and got=%d to update secrets, got=%q", len(o.wantToUpdateSecrets), len(got1), joinSecretNames(got1))
		}
		for _, wantCreateName := range o.wantToCreateSecrets {
			if !secretFound(got, wantCreateName) {
				t.Errorf("wanted secret name: %s not found at toCreateSecrets", wantCreateName)
			}
		}
		for _, wantExistName := range o.wantToUpdateSecrets {
			if !secretFound(got1, wantExistName) {
				t.Errorf("wanted secret name: %s not found at existSecrets", wantExistName)
			}
		}
	}

	// want 1 updateSecret
	f(opts{
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "exist",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
			},
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-exist",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer-1")},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist", Namespace: "default"},
			},
		},
		wantToUpdateSecrets: []string{"vmuser-exist"},
		wantToCreateSecrets: []string{"vmuser-not-exist"},
	})

	// want 1 updateSecret
	f(opts{
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "must-not-exist",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{
					BearerToken:           ptr.To("some-bearer"),
					DisableSecretCreation: true,
				},
			},
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-exists-must-create",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
			},
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "exists",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{BearerToken: ptr.To("some-bearer")},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmuser-exists",
					Namespace: "default",
				},
			},
		},
		wantToUpdateSecrets: []string{"vmuser-exists"},
		wantToCreateSecrets: []string{"vmuser-not-exists-must-create"},
	})

	// want nothing
	f(opts{
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "exist-with-generated",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{
					GeneratePassword: true,
				},
			},
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "exist-hardcoded",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-with-generated", Namespace: "default"},
				Data:       map[string][]byte{"username": []byte(`vmuser-exist-with-generated`), "password": []byte(`generated`)},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "vmuser-exist-hardcoded", Namespace: "default"},
				Data:       map[string][]byte{"bearerToken": []byte(`some-bearer`)},
			},
		},
	})

	// update secret value
	f(opts{
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "exist-with-generated",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{
					GeneratePassword: true,
				},
			},
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "exist-to-update",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{Password: ptr.To("some-new-password"), Username: ptr.To("some-user")},
			},
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
	})
}

func Test_buildConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAuth
		want              string
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.TODO()
		rand.Shuffle(len(o.predefinedObjects), func(i, j int) {
			o.predefinedObjects[i], o.predefinedObjects[j] = o.predefinedObjects[j], o.predefinedObjects[i]
		})
		testClient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		// fetch exist users for vmauth.
		pos, err := selectUsers(ctx, testClient, o.cr)
		assert.NoError(t, err)
		ac := getAssetsCache(ctx, testClient, o.cr)

		got, err := pos.buildConfig(ctx, testClient, o.cr, ac)
		assert.NoError(t, err)
		if !assert.Equal(t, o.want, string(got)) {
			return
		}
		// fetch exist users for vmauth.
		pos, err = selectUsers(ctx, testClient, o.cr)
		assert.NoError(t, err)
		got2, err := pos.buildConfig(ctx, testClient, o.cr, ac)
		assert.NoError(t, err)
		assert.Equal(t, o.want, string(got2))
	}

	// simple cfg
	f(opts{
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
			&vmv1beta1.VMUser{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "user-3",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{
					BearerToken: ptr.To("bearer-token-3"),
					TargetRefs: []vmv1beta1.TargetRef{
						{
							CRD: &vmv1beta1.CRDRef{
								Kind:      "VLSingle",
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
					Name:      "user-4",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{
					BearerToken: ptr.To("bearer-token-4"),
					TargetRefs: []vmv1beta1.TargetRef{
						{
							CRD: &vmv1beta1.CRDRef{
								Kind:      "VTCluster/vtselect",
								Name:      "test",
								Namespace: "default",
							},
							Paths: []string{"/"},
						},
					},
				},
			},
			&vmv1.VTCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: vmv1.VTClusterSpec{
					Select: &vmv1.VTSelect{},
				},
			},
			&vmv1.VLSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
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
- url_prefix:
  - http://vlsingle-test.default.svc:9428
  bearer_token: bearer-token-3
- url_prefix:
  - http://vtselect-test.default.svc:10471
  bearer_token: bearer-token-4
`,
	})

	// simple cfg with duplicated users
	f(opts{
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
	})

	// with targetRef basicauth secret refs and headers
	f(opts{
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
					Username: ptr.To("some-user"),
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
	})

	// with secret refs
	f(opts{
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
					Username: ptr.To("some-user"),
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
	})

	// default cfg with empty selectors
	f(opts{
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
	})

	// vmauth ns selector
	f(opts{
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
	})

	// with full unauthorized access and ip_filter
	f(opts{
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
	})

	// with disabled headers, max concurrent and response headers
	f(opts{
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
	})

	// with cluster discovery and auth
	f(opts{
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
    - /select/.*
    - /admin/.*
    headers:
    - 'Authorization: Basic c29tZS0xOnNvbWUtMg=='
  - url_prefix:
    - http://vminsert-main-cluster.default.svc:8480
    src_paths:
    - /insert/.*
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
	})

	// with duplicated users and broken links
	f(opts{
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
	})

	// with full unauthorizedUserAccessSpec
	f(opts{
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
							QueryArgs: []vmv1beta1.QueryArg{
								{
									Name:   "foo",
									Values: []string{"bar"},
								},
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
  - http://vmagent-test.default.svc:8429?foo=bar
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
	})

	// with unsorted duplicates
	f(opts{
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
  - http://some-static-3
  bearer_token: bearer-3
- url_prefix:
  - http://some-static-1
  bearer_token: bearer-1
`,
	})

	// with vlsingle and vlagent refs
	f(opts{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "logs-refs",
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
							CRD: &vmv1beta1.CRDRef{
								Kind:      "VLSingle",
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
					Name:      "user-2",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMUserSpec{
					BearerToken: ptr.To("bearer-token-2"),
					TargetRefs: []vmv1beta1.TargetRef{
						{
							CRD: &vmv1beta1.CRDRef{
								Kind:      "VLAgent",
								Name:      "test",
								Namespace: "default",
							},
							Paths: []string{"/"},
						},
					},
				},
			},
			&vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
			&vmv1.VLSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
		},
		want: `users:
- url_prefix:
  - http://vlsingle-test.default.svc:9428
  name: user1
  bearer_token: bearer
- url_prefix:
  - http://vlagent-test.default.svc:9429
  bearer_token: bearer-token-2
`,
	})

	// with vlcluster refs
	f(opts{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "logs-cluster-refs",
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
							CRD: &vmv1beta1.CRDRef{
								Kind:      "VLCluster/vlinsert",
								Name:      "test",
								Namespace: "default",
							},
							Paths: []string{"/insert.*"},
						},
						{
							CRD: &vmv1beta1.CRDRef{
								Kind:      "VLCluster/vlselect",
								Name:      "test",
								Namespace: "default",
							},
							Paths: []string{"/select.*"},
						},
						{
							CRD: &vmv1beta1.CRDRef{
								Kind:      "VLCluster/vlstorage",
								Name:      "test",
								Namespace: "default",
							},
							Paths: []string{"/internal.*"},
						},
					},
				},
			},
			&vmv1.VLCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: vmv1.VLClusterSpec{
					VLInsert:  &vmv1.VLInsert{},
					VLSelect:  &vmv1.VLSelect{},
					VLStorage: &vmv1.VLStorage{},
				},
			},
		},
		want: `users:
- url_map:
  - url_prefix:
    - http://vlinsert-test.default.svc:9481
    src_paths:
    - /insert.*
  - url_prefix:
    - http://vlselect-test.default.svc:9471
    src_paths:
    - /select.*
  - url_prefix:
    - http://vlstorage-test.default.svc:9491
    src_paths:
    - /internal.*
  name: user1
  bearer_token: bearer
`,
	})
}
