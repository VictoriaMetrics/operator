package vmagent

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestGenerateScrapeConfig(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAgent
		sc                *vmv1beta1.VMScrapeConfig
		predefinedObjects []runtime.Object
		want              string
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ac := getAssetsCache(ctx, fclient, o.cr)
		sp := &o.cr.Spec.CommonScrapeParams
		got, err := generateScrapeConfig(ctx, sp, o.sc, ac)
		if err != nil {
			t.Errorf("cannot execute generateScrapeConfig, err: %e", err)
			return
		}
		gotBytes, err := yaml.Marshal(got)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !assert.Equal(t, o.want, string(gotBytes)) {
			t.Errorf("generateScrapeConfig(): %s", cmp.Diff(string(gotBytes), o.want))
		}
	}

	// basic static cfg with basic auth
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					MinScrapeInterval: ptr.To("30s"),
					MaxScrapeInterval: ptr.To("5m"),
				},
			},
		},
		sc: &vmv1beta1.VMScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "static-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMScrapeConfigSpec{
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					MaxScrapeSize:  "60KB",
					ScrapeInterval: "10s",
				},
				StaticConfigs: []vmv1beta1.StaticConfig{
					{
						Targets: []string{"http://test1.com", "http://test2.com"},
						Labels:  map[string]string{"bar": "baz"},
					},
				},
				EndpointAuth: vmv1beta1.EndpointAuth{
					BasicAuth: &vmv1beta1.BasicAuth{
						Username: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "ba-secret",
							},
							Key: "username",
						},
						Password: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "ba-secret",
							},
							Key: "password",
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ba-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("admin"),
					"password": []byte("dangerous"),
				},
			},
		},
		want: `job_name: scrapeConfig/default/static-1
honor_labels: false
scrape_interval: 30s
max_scrape_size: 60KB
relabel_configs: []
basic_auth:
  username: admin
  password: dangerous
static_configs:
- targets:
  - http://test1.com
  - http://test2.com
  labels:
    bar: baz
`,
	})

	// basic fileSDConfig
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				CommonScrapeParams: vmv1beta1.CommonScrapeParams{
					MinScrapeInterval: ptr.To("30s"),
					MaxScrapeInterval: ptr.To("5m"),
				},
			},
		},
		sc: &vmv1beta1.VMScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "file-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMScrapeConfigSpec{
				EndpointAuth: vmv1beta1.EndpointAuth{
					BasicAuth: &vmv1beta1.BasicAuth{
						Username: corev1.SecretKeySelector{
							Key: "username",
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "ba-secret",
							},
						},
						PasswordFile: "/var/run/secrets/password",
					},
				},
				EndpointScrapeParams: vmv1beta1.EndpointScrapeParams{
					ScrapeInterval: "10m",
				},
				FileSDConfigs: []vmv1beta1.FileSDConfig{
					{
						Files: []string{"test1.json", "test2.json"},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ba-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"username": []byte("user"),
				},
			},
		},
		want: `job_name: scrapeConfig/default/file-1
honor_labels: false
scrape_interval: 5m
relabel_configs: []
basic_auth:
  username: user
  password_file: /var/run/secrets/password
file_sd_configs:
- files:
  - test1.json
  - test2.json
`,
	})

	// basic httpSDConfig
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "httpsd-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMScrapeConfigSpec{
				HTTPSDConfigs: []vmv1beta1.HTTPSDConfig{
					{
						URL:      "http://www.test1.com",
						ProxyURL: ptr.To("http://www.proxy.com"),
					},
					{
						URL: "http://www.test2.com",
						Authorization: &vmv1beta1.Authorization{
							Type: "Bearer",
							Credentials: &corev1.SecretKeySelector{
								Key: "cred",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "auth-secret",
								},
							},
						},
						TLSConfig: &vmv1beta1.TLSConfig{
							CA: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									Key: "ca",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
								},
							},
							Cert: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									Key: "cert",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "tls-secret",
									},
								},
							},
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"ca":   []byte("ca-value"),
					"cert": []byte("cert-value"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"cred": []byte("auth-secret"),
				},
			},
		},
		want: `job_name: scrapeConfig/default/httpsd-1
honor_labels: false
relabel_configs: []
http_sd_configs:
- url: http://www.test1.com
  proxy_url: http://www.proxy.com
- url: http://www.test2.com
  authorization:
    credentials: auth-secret
    type: Bearer
  tls_config:
    ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
    cert_file: /etc/vmagent-tls/certs/default_tls-secret_cert
`,
	})

	// basic kubernetesSDConfig
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kubernetesSDConfig-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMScrapeConfigSpec{
				KubernetesSDConfigs: []vmv1beta1.KubernetesSDConfig{
					{
						APIServer:      ptr.To("http://127.0.0.1:6443"),
						Role:           "pod",
						AttachMetadata: vmv1beta1.AttachMetadata{Node: ptr.To(true)},
						Selectors: []vmv1beta1.K8SSelectorConfig{
							{
								Role:  "pod",
								Label: "app/instance",
								Field: "test",
							},
						},
						TLSConfig: &vmv1beta1.TLSConfig{
							InsecureSkipVerify: true,
						},
					},
					{
						APIServer: ptr.To("http://127.0.0.1:6443"),
						Role:      "node",
						Selectors: []vmv1beta1.K8SSelectorConfig{
							{
								Role:  "node",
								Label: "kubernetes.io/os",
								Field: "linux",
							},
						},
					},
				},
			},
		},
		want: `job_name: scrapeConfig/default/kubernetesSDConfig-1
honor_labels: false
relabel_configs: []
kubernetes_sd_configs:
- api_server: http://127.0.0.1:6443
  role: pod
  tls_config:
    insecure_skip_verify: true
  attach_metadata:
    node: true
  selectors:
  - role: pod
    label: app/instance
    field: test
- api_server: http://127.0.0.1:6443
  role: node
  selectors:
  - role: node
    label: kubernetes.io/os
    field: linux
`,
	})

	// mixed
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mixconfigs-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMScrapeConfigSpec{
				ConsulSDConfigs: []vmv1beta1.ConsulSDConfig{
					{
						Server:     "localhost:8500",
						TokenRef:   &corev1.SecretKeySelector{Key: "consul_token"},
						Datacenter: ptr.To("dc1"),
						NodeMeta:   map[string]string{"worker": "1"},
						Filter:     `filter=NodeMeta.os == "linux"`,
					},
				},
				DNSSDConfigs: []vmv1beta1.DNSSDConfig{
					{
						Names: []string{"vmagent-0.vmagent.default.svc.cluster.local"},
						Port:  ptr.To(8429),
					},
				},
				EC2SDConfigs: []vmv1beta1.EC2SDConfig{
					{
						Region: ptr.To("us-west-2"),
						Port:   ptr.To(9404),
						Filters: []*vmv1beta1.EC2Filter{{
							Name:   "instance-id",
							Values: []string{"i-98765432109876543", "i-12345678901234567"},
						}},
					},
				},
				AzureSDConfigs: []vmv1beta1.AzureSDConfig{
					{
						Environment:    ptr.To("AzurePublicCloud"),
						SubscriptionID: "1",
						TenantID:       ptr.To("u1"),
						ResourceGroup:  ptr.To("rg1"),
						Port:           ptr.To(80),
					},
				},
				GCESDConfigs: []vmv1beta1.GCESDConfig{
					{
						Project:      "eu-project",
						Zone:         vmv1beta1.StringOrArray{"zone-a"},
						TagSeparator: ptr.To("/"),
					},
					{
						Project:      "us-project",
						Zone:         vmv1beta1.StringOrArray{"zone-b", "zone-c"},
						TagSeparator: ptr.To("/"),
					},
				},
				OpenStackSDConfigs: []vmv1beta1.OpenStackSDConfig{
					{
						Role:             "instance",
						IdentityEndpoint: ptr.To("http://localhost:5000/v3"),
						Username:         ptr.To("user1"),
						UserID:           ptr.To("1"),
						Password: &corev1.SecretKeySelector{
							Key:                  "pass",
							LocalObjectReference: corev1.LocalObjectReference{Name: "ba-secret"},
						},
						ProjectName: ptr.To("poc"),
						AllTenants:  ptr.To(true),
						DomainName:  ptr.To("default"),
					},
				},
				DigitalOceanSDConfigs: []vmv1beta1.DigitalOceanSDConfig{
					{
						OAuth2: &vmv1beta1.OAuth2{
							Scopes:         []string{"scope-1"},
							TokenURL:       "http://some-token-url",
							EndpointParams: map[string]string{"timeout": "5s"},
							ClientID: vmv1beta1.SecretOrConfigMap{
								Secret: &corev1.SecretKeySelector{
									Key:                  "client-id",
									LocalObjectReference: corev1.LocalObjectReference{Name: "oauth-secret"},
								},
							},
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "access-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"bearer": []byte("bearer-value"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"client-id": []byte("some-id"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ba-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"pass": []byte("bearer-value"),
				},
			},
		},
		want: `job_name: scrapeConfig/default/mixconfigs-1
honor_labels: false
relabel_configs: []
consul_sd_configs:
- server: localhost:8500
  datacenter: dc1
  node_meta:
    worker: "1"
  filter: filter=NodeMeta.os == "linux"
dns_sd_configs:
- names:
  - vmagent-0.vmagent.default.svc.cluster.local
  port: 8429
ec2_sd_configs:
- region: us-west-2
  port: 9404
  filters:
  - name: instance-id
    values:
    - i-98765432109876543
    - i-12345678901234567
azure_sd_configs:
- environment: AzurePublicCloud
  subscription_id: "1"
  tenant_id: u1
  resource_group: rg1
  port: 80
gce_sd_configs:
- project: eu-project
  zone: zone-a
  tag_separator: /
- project: us-project
  zone:
  - zone-b
  - zone-c
  tag_separator: /
openstack_sd_configs:
- role: instance
  region: ""
  identity_endpoint: http://localhost:5000/v3
  username: user1
  userid: "1"
  password: bearer-value
  domain_name: default
  project_name: poc
  all_tenants: true
digitalocean_sd_configs:
- oauth2:
    client_id: some-id
    scopes:
    - scope-1
    endpoint_params:
      timeout: 5s
    token_url: http://some-token-url
`,
	})

	// basic nomadSDConfig
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nomadsd-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMScrapeConfigSpec{
				NomadSDConfigs: []vmv1beta1.NomadSDConfig{
					{
						Server:       "localhost:4646",
						Namespace:    ptr.To("default"),
						Region:       ptr.To("global"),
						TagSeparator: ptr.To(","),
						AllowStale:   ptr.To(true),
					},
				},
			},
		},
		want: `job_name: scrapeConfig/default/nomadsd-1
honor_labels: false
relabel_configs: []
nomad_sd_configs:
- server: localhost:4646
  namespace: default
  region: global
  tag_separator: ','
  allow_stale: true
`,
	})

	// configs with auth and empty type
	f(opts{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default-vmagent",
				Namespace: "default",
			},
		},
		sc: &vmv1beta1.VMScrapeConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sc-auth",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMScrapeConfigSpec{
				HTTPSDConfigs: []vmv1beta1.HTTPSDConfig{
					{
						URL: "http://www.test1.com",
						Authorization: &vmv1beta1.Authorization{
							Type: "Bearer",
							Credentials: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "auth-secret"},
								Key:                  "cred",
							},
						},
					},
					{
						URL: "http://www.test2.com",
						Authorization: &vmv1beta1.Authorization{
							Credentials: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "auth-secret"},
								Key:                  "cred",
							},
						},
					},
					{
						URL: "http://www.test3.com",
						Authorization: &vmv1beta1.Authorization{
							Type:            "Bearer",
							CredentialsFile: "file",
						},
					},
				},
				KubernetesSDConfigs: []vmv1beta1.KubernetesSDConfig{
					{
						Role: "endpoints",
						Authorization: &vmv1beta1.Authorization{
							Type: "Bearer",
							Credentials: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "auth"},
								Key:                  "cred",
							},
						},
					},
					{
						Role: "endpoints",
						Authorization: &vmv1beta1.Authorization{
							Credentials: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "auth-secret"},
								Key:                  "cred",
							},
						},
					},
					{
						Role: "endpoints",
						Authorization: &vmv1beta1.Authorization{
							Type:            "Bearer",
							CredentialsFile: "file",
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"cred": []byte("auth-secret"),
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"cred": []byte("auth-secret"),
				},
			},
		},
		want: `job_name: scrapeConfig/default/sc-auth
honor_labels: false
relabel_configs: []
http_sd_configs:
- url: http://www.test1.com
  authorization:
    credentials: auth-secret
    type: Bearer
- url: http://www.test2.com
  authorization:
    credentials: auth-secret
    type: Bearer
- url: http://www.test3.com
  authorization:
    credentials_file: file
    type: Bearer
kubernetes_sd_configs:
- role: endpoints
  authorization:
    credentials: auth-secret
    type: Bearer
- role: endpoints
  authorization:
    credentials: auth-secret
    type: Bearer
- role: endpoints
  authorization:
    credentials_file: file
    type: Bearer
`,
	})
}
