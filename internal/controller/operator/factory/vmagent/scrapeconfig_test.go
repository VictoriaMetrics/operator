package vmagent

import (
	"context"
	"testing"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestGenerateScrapeConfig(t *testing.T) {
	type args struct {
		cr                    vmv1beta1.VMAgent
		m                     *vmv1beta1.VMScrapeConfig
		ssCache               *scrapesSecretsCache
		enforceNamespaceLabel string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "basic static cfg with basic auth",
			args: args{
				cr: vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						MinScrapeInterval: ptr.To("30s"),
						MaxScrapeInterval: ptr.To("5m"),
					},
				},
				m: &vmv1beta1.VMScrapeConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "static-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMScrapeConfigSpec{
						MaxScrapeSize:  "60KB",
						ScrapeInterval: "10s",
						StaticConfigs: []vmv1beta1.StaticConfig{
							{
								Targets: []string{"http://test1.com", "http://test2.com"},
								Labels:  map[string]string{"bar": "baz"},
							},
						},
						BasicAuth: &vmv1beta1.BasicAuth{
							Username: corev1.SecretKeySelector{Key: "username"},
							Password: corev1.SecretKeySelector{Key: "password"},
						},
					},
				},
				ssCache: &scrapesSecretsCache{
					baSecrets: map[string]*k8stools.BasicAuthCredentials{
						"scrapeConfig/default/static-1//0": {
							Password: "dangerous",
							Username: "admin",
						},
					},
				},
			},
			want: `job_name: scrapeConfig/default/static-1
honor_labels: false
scrape_interval: 30s
basic_auth:
  username: admin
  password: dangerous
max_scrape_size: 60KB
relabel_configs: []
static_configs:
- targets:
  - http://test1.com
  - http://test2.com
  labels:
    bar: baz
`,
		},
		{
			name: "basic fileSDConfig",
			args: args{
				cr: vmv1beta1.VMAgent{
					Spec: vmv1beta1.VMAgentSpec{
						MinScrapeInterval: ptr.To("30s"),
						MaxScrapeInterval: ptr.To("5m"),
					},
				},
				m: &vmv1beta1.VMScrapeConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "file-1",
						Namespace: "default",
					},
					Spec: vmv1beta1.VMScrapeConfigSpec{
						ScrapeInterval: "10m",
						FileSDConfigs: []vmv1beta1.FileSDConfig{
							{
								Files: []string{"test1.json", "test2.json"},
							},
						},
						BasicAuth: &vmv1beta1.BasicAuth{
							Username:     corev1.SecretKeySelector{Key: "username"},
							PasswordFile: "/var/run/secrets/password",
						},
					},
				},
				ssCache: &scrapesSecretsCache{
					baSecrets: map[string]*k8stools.BasicAuthCredentials{
						"scrapeConfig/default/file-1//0": {
							Username: "user",
						},
					},
				},
			},
			want: `job_name: scrapeConfig/default/file-1
honor_labels: false
scrape_interval: 5m
basic_auth:
  username: user
  password_file: /var/run/secrets/password
relabel_configs: []
file_sd_configs:
- files:
  - test1.json
  - test2.json
`,
		},
		{
			name: "basic httpSDConfig",
			args: args{
				cr: vmv1beta1.VMAgent{},
				m: &vmv1beta1.VMScrapeConfig{
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
									Type:        "Bearer",
									Credentials: &corev1.SecretKeySelector{Key: "cred"},
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
									Cert: vmv1beta1.SecretOrConfigMap{Secret: &corev1.SecretKeySelector{Key: "cert"}},
								},
							},
						},
					},
				},
				ssCache: &scrapesSecretsCache{
					baSecrets: map[string]*k8stools.BasicAuthCredentials{
						"scrapeConfig/default/file-1//0": {
							Username: "user",
						},
					},
					authorizationSecrets: map[string]string{
						"scrapeConfig/default/httpsd-1/httpsd/1": "auth-secret",
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
    type: Bearer
    credentials: auth-secret
  tls_config:
    insecure_skip_verify: false
    ca_file: /etc/vmagent-tls/certs/default_tls-secret_ca
`,
		},
		{
			name: "basic kubernetesSDConfig",
			args: args{
				cr: vmv1beta1.VMAgent{},
				m: &vmv1beta1.VMScrapeConfig{
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
				ssCache: &scrapesSecretsCache{},
			},
			want: `job_name: scrapeConfig/default/kubernetesSDConfig-1
honor_labels: false
relabel_configs: []
kubernetes_sd_configs:
- api_server: http://127.0.0.1:6443
  role: pod
  tls_config:
    insecure_skip_verify: true
  attach_metadata: true
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
		},
		{
			name: "mixed",
			args: args{
				cr: vmv1beta1.VMAgent{},
				m: &vmv1beta1.VMScrapeConfig{
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
								Zone:         "zone-a",
								TagSeparator: ptr.To("/"),
							},
						},
						OpenStackSDConfigs: []vmv1beta1.OpenStackSDConfig{
							{
								Role:             "instance",
								IdentityEndpoint: ptr.To("http://localhost:5000/v3"),
								Username:         ptr.To("user1"),
								UserID:           ptr.To("1"),
								Password:         &corev1.SecretKeySelector{Key: "pass"},
								ProjectName:      ptr.To("poc"),
								AllTenants:       ptr.To(true),
								DomainName:       ptr.To("default"),
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
											Key:                  "bearer",
											LocalObjectReference: corev1.LocalObjectReference{Name: "access-secret"},
										},
									},
								},
							},
						},
					},
				},
				ssCache: &scrapesSecretsCache{
					oauth2Secrets: map[string]*k8stools.OAuthCreds{
						"scrapeConfig/default/mixconfigs-1/digitaloceansd/0": {ClientSecret: "some-secret", ClientID: "some-id"},
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
openstack_sd_configs:
- role: instance
  region: ""
  identity_endpoint: http://localhost:5000/v3
  username: user1
  userid: "1"
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
		},
		{
			name: "configs with auth and empty type",
			args: args{
				cr: vmv1beta1.VMAgent{},
				m: &vmv1beta1.VMScrapeConfig{
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
								},
							},
							{
								URL: "http://www.test2.com",
								Authorization: &vmv1beta1.Authorization{
									Credentials: &corev1.SecretKeySelector{Key: "cred"},
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
								},
							},
							{
								Role: "endpoints",
								Authorization: &vmv1beta1.Authorization{
									Credentials: &corev1.SecretKeySelector{Key: "cred"},
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
				ssCache: &scrapesSecretsCache{
					authorizationSecrets: map[string]string{
						"scrapeConfig/default/sc-auth/httpsd/1": "auth-secret",
						"scrapeConfig/default/sc-auth/kubesd/1": "auth-secret",
					},
				},
			},
			want: `job_name: scrapeConfig/default/sc-auth
honor_labels: false
relabel_configs: []
http_sd_configs:
- url: http://www.test1.com
- url: http://www.test2.com
  authorization:
    type: Bearer
    credentials: auth-secret
- url: http://www.test3.com
  authorization:
    type: Bearer
    credentials_file: file
kubernetes_sd_configs:
- role: endpoints
- role: endpoints
  authorization:
    type: Bearer
    credentials: auth-secret
- role: endpoints
  authorization:
    type: Bearer
    credentials_file: file
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateScrapeConfig(context.Background(), &tt.args.cr, tt.args.m, tt.args.ssCache, tt.args.enforceNamespaceLabel)
			gotBytes, err := yaml.Marshal(got)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !assert.Equal(t, tt.want, string(gotBytes)) {
				t.Errorf("generateScrapeConfig() = \n%v, want \n%v", string(gotBytes), tt.want)
			}
		})
	}
}
