package vmagent

import (
	"context"
	"fmt"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"gopkg.in/yaml.v2"
)

func generateScrapeConfig(
	ctx context.Context,
	cr *victoriametricsv1beta1.VMAgent,
	sc *victoriametricsv1beta1.VMScrapeConfig,
	ssCache *scrapesSecretsCache,
	enforcedNamespaceLabel string,
) yaml.MapSlice {
	jobName := fmt.Sprintf("scrapeConfig/%s/%s", sc.Namespace, sc.Name)
	hl := honorLabels(sc.Spec.HonorLabels, cr.Spec.OverrideHonorLabels)
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: jobName,
		},
		{
			Key:   "honor_labels",
			Value: hl,
		},
	}
	cfg = honorTimestamps(cfg, sc.Spec.HonorTimestamps, cr.Spec.OverrideHonorTimestamps)

	if sc.Spec.MetricsPath != nil {
		cfg = append(cfg, yaml.MapItem{Key: "metrics_path", Value: *sc.Spec.MetricsPath})
	}

	scrapeInterval := limitScrapeInterval(ctx, sc.Spec.ScrapeInterval, cr.Spec.MinScrapeInterval, cr.Spec.MaxScrapeInterval)
	if sc.Spec.ScrapeInterval != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_interval", Value: scrapeInterval})
	}
	if sc.Spec.ScrapeTimeout != "" {
		cfg = append(cfg, yaml.MapItem{Key: "scrape_timeout", Value: sc.Spec.ScrapeTimeout})
	}
	if len(sc.Spec.Params) > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "params", Value: sc.Spec.Params})
	}
	if sc.Spec.Scheme != nil {
		cfg = append(cfg, yaml.MapItem{Key: "scheme", Value: strings.ToLower(*sc.Spec.Scheme)})
	}
	cfg = append(cfg, buildVMScrapeParams(cr.Namespace, sc.AsProxyKey("", 0), sc.Spec.VMScrapeParams, ssCache)...)

	if sc.Spec.FollowRedirects != nil {
		cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: sc.Spec.FollowRedirects})
	}
	if sc.Spec.ProxyURL != nil {
		cfg = append(cfg, yaml.MapItem{Key: "proxy_url", Value: sc.Spec.ProxyURL})
	}
	if sc.Spec.BasicAuth != nil {
		var bac yaml.MapSlice
		if s, ok := ssCache.baSecrets[sc.AsMapKey("", 0)]; ok {
			bac = append(bac,
				yaml.MapItem{Key: "username", Value: s.Username},
			)
			if s.Password != "" {
				bac = append(bac,
					yaml.MapItem{Key: "password", Value: s.Password},
				)
			} else if len(sc.Spec.BasicAuth.PasswordFile) > 0 {
				bac = append(bac, yaml.MapItem{Key: "password_file", Value: sc.Spec.BasicAuth.PasswordFile})
			}
		}
		if len(bac) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "basic_auth", Value: bac})
		}
	}
	cfg = addAuthorizationConfig(cfg, sc.AsMapKey("", 0), sc.Spec.Authorization, ssCache.authorizationSecrets)
	cfg = addOAuth2Config(cfg, sc.AsMapKey("", 0), sc.Spec.OAuth2, ssCache.oauth2Secrets)
	cfg = addTLStoYaml(cfg, sc.Namespace, sc.Spec.TLSConfig, false)

	if sc.Spec.SampleLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "sample_limit", Value: sc.Spec.SampleLimit})
	}
	if sc.Spec.SeriesLimit > 0 {
		cfg = append(cfg, yaml.MapItem{Key: "series_limit", Value: sc.Spec.SeriesLimit})
	}

	var relabelings []yaml.MapSlice
	for _, c := range sc.Spec.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}
	for _, trc := range cr.Spec.ScrapeConfigRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, sc.Namespace, enforcedNamespaceLabel)
	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})

	if sc.Spec.MetricRelabelConfigs != nil {
		var metricRelabelings []yaml.MapSlice
		for _, c := range sc.Spec.MetricRelabelConfigs {
			if c.TargetLabel != "" && enforcedNamespaceLabel != "" && c.TargetLabel == enforcedNamespaceLabel {
				continue
			}
			relabeling := generateRelabelConfig(c)
			metricRelabelings = append(metricRelabelings, relabeling)
		}
		cfg = append(cfg, yaml.MapItem{Key: "metric_relabel_configs", Value: metricRelabelings})
	}

	// build staticConfig
	if len(sc.Spec.StaticConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.StaticConfigs))
		for i, config := range sc.Spec.StaticConfigs {
			configs[i] = []yaml.MapItem{
				{
					Key:   "targets",
					Value: config.Targets,
				},
				{
					Key:   "labels",
					Value: config.Labels,
				},
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "static_configs",
			Value: configs,
		})
	}

	// build fileSDConfig
	if len(sc.Spec.FileSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.FileSDConfigs))
		for i, config := range sc.Spec.FileSDConfigs {
			configs[i] = []yaml.MapItem{
				{
					Key:   "files",
					Value: config.Files,
				},
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "file_sd_configs",
			Value: configs,
		})
	}

	// build httpSDConfig
	if len(sc.Spec.HTTPSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.HTTPSDConfigs))
		for i, config := range sc.Spec.HTTPSDConfigs {
			configs[i] = []yaml.MapItem{
				{
					Key:   "url",
					Value: config.URL,
				},
			}

			if config.BasicAuth != nil {
				var bac yaml.MapSlice
				if s, ok := ssCache.baSecrets[sc.AsMapKey("httpsd", i)]; ok {
					bac = append(bac,
						yaml.MapItem{Key: "username", Value: s.Username},
						yaml.MapItem{Key: "password", Value: s.Password},
					)
				}
				if len(config.BasicAuth.PasswordFile) > 0 {
					bac = append(bac, yaml.MapItem{Key: "password_file", Value: config.BasicAuth.PasswordFile})
				}
				if len(bac) > 0 {
					configs[i] = append(configs[i], yaml.MapItem{Key: "basic_auth", Value: bac})
				}
			}
			configs[i] = addAuthorizationConfig(configs[i], sc.AsMapKey("httpsd", i), config.Authorization, ssCache.authorizationSecrets)
			if config.TLSConfig != nil {
				configs[i] = addTLStoYaml(configs[i], sc.Namespace, config.TLSConfig, false)
			}
			if config.ProxyURL != nil {
				configs[i] = append(configs[i], yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
			}
			if config.ProxyClientConfig != nil {
				configs[i] = append(configs[i], buildProxyAuthConfig(sc.Namespace, sc.AsMapKey("httpsd", i), config.ProxyClientConfig, ssCache)...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "http_sd_configs",
			Value: configs,
		})
	}

	// build kubernetesSDConfig
	if len(sc.Spec.KubernetesSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.KubernetesSDConfigs))
		for i, config := range sc.Spec.KubernetesSDConfigs {
			if config.APIServer != nil {
				configs[i] = []yaml.MapItem{
					{
						Key:   "api_server",
						Value: config.APIServer,
					},
				}
			}
			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "role",
				Value: config.Role,
			})

			if config.BasicAuth != nil {
				var bac yaml.MapSlice
				if s, ok := ssCache.baSecrets[sc.AsMapKey("kubesd", i)]; ok {
					bac = append(bac,
						yaml.MapItem{Key: "username", Value: s.Username},
						yaml.MapItem{Key: "password", Value: s.Password},
					)
				}
				if len(config.BasicAuth.PasswordFile) > 0 {
					bac = append(bac, yaml.MapItem{Key: "password_file", Value: config.BasicAuth.PasswordFile})
				}
				if len(bac) > 0 {
					configs[i] = append(configs[i], yaml.MapItem{Key: "basic_auth", Value: bac})
				}
			}
			configs[i] = addAuthorizationConfig(configs[i], sc.AsMapKey("kubesd", i), config.Authorization, ssCache.authorizationSecrets)
			if config.TLSConfig != nil {
				configs[i] = addTLStoYaml(configs[i], sc.Namespace, config.TLSConfig, false)
			}
			configs[i] = addOAuth2Config(configs[i], sc.AsMapKey("kubesd", i), config.OAuth2, ssCache.oauth2Secrets)
			if config.ProxyURL != nil {
				configs[i] = append(configs[i], yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
			}
			if config.ProxyClientConfig != nil {
				configs[i] = append(configs[i], buildProxyAuthConfig(sc.Namespace, sc.AsMapKey("kubesd", i), config.ProxyClientConfig, ssCache)...)
			}

			if config.FollowRedirects != nil {
				cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: config.FollowRedirects})
			}
			if config.Namespaces != nil {
				namespaces := []yaml.MapItem{
					{
						Key:   "names",
						Value: config.Namespaces.Names,
					},
				}

				if config.Namespaces.IncludeOwnNamespace != nil {
					namespaces = append(namespaces, yaml.MapItem{
						Key:   "own_namespace",
						Value: config.Namespaces.IncludeOwnNamespace,
					})
				}

				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "namespaces",
					Value: namespaces,
				})
			}

			if config.AttachMetadata.Node != nil {
				switch config.Role {
				case "pod", "endpoints", "endpointslice":
					configs[i] = append(configs[i], yaml.MapItem{
						Key:   "attach_metadata",
						Value: *config.AttachMetadata.Node,
					})
				}
			}

			selectors := make([][]yaml.MapItem, len(config.Selectors))
			for i, s := range config.Selectors {
				selectors[i] = []yaml.MapItem{
					{
						Key:   "role",
						Value: strings.ToLower(string(s.Role)),
					},
					{
						Key:   "label",
						Value: s.Label,
					},
					{
						Key:   "field",
						Value: s.Field,
					},
				}
			}

			if len(selectors) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "selectors",
					Value: selectors,
				})
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "kubernetes_sd_configs",
			Value: configs,
		})
	}

	// build consulSDConfig
	if len(sc.Spec.ConsulSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.ConsulSDConfigs))
		for i, config := range sc.Spec.ConsulSDConfigs {
			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "server",
				Value: config.Server,
			})

			if config.TokenRef != nil && config.TokenRef.Name != "" {
				if s, ok := ssCache.bearerTokens[sc.AsMapKey("consulsd", i)]; ok {
					configs[i] = append(configs[i], yaml.MapItem{Key: "bearer_token", Value: s})
				}
			}

			if config.Datacenter != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "datacenter",
					Value: config.Datacenter,
				})
			}

			if config.Namespace != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "namespace",
					Value: config.Namespace,
				})
			}

			if config.Partition != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "partition",
					Value: config.Partition,
				})
			}

			if config.Scheme != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "scheme",
					Value: strings.ToLower(*config.Scheme),
				})
			}

			if len(config.Services) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "services",
					Value: config.Services,
				})
			}

			if len(config.Tags) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "tags",
					Value: config.Tags,
				})
			}

			if config.TagSeparator != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "tag_separator",
					Value: config.TagSeparator,
				})
			}

			if len(config.NodeMeta) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "node_meta",
					Value: stringMapToMapSlice(config.NodeMeta),
				})
			}

			if config.AllowStale != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "allow_stale",
					Value: config.AllowStale,
				})
			}

			if config.BasicAuth != nil {
				var bac yaml.MapSlice
				if s, ok := ssCache.baSecrets[sc.AsMapKey("consulsd", i)]; ok {
					bac = append(bac,
						yaml.MapItem{Key: "username", Value: s.Username},
						yaml.MapItem{Key: "password", Value: s.Password},
					)
				}
				if len(config.BasicAuth.PasswordFile) > 0 {
					bac = append(bac, yaml.MapItem{Key: "password_file", Value: config.BasicAuth.PasswordFile})
				}
				if len(bac) > 0 {
					configs[i] = append(configs[i], yaml.MapItem{Key: "basic_auth", Value: bac})
				}
			}
			configs[i] = addAuthorizationConfig(configs[i], sc.AsMapKey("consulsd", i), config.Authorization, ssCache.authorizationSecrets)
			configs[i] = addOAuth2Config(configs[i], sc.AsMapKey("consulsd", i), config.OAuth2, ssCache.oauth2Secrets)
			if config.ProxyURL != nil {
				configs[i] = append(configs[i], yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
			}
			if config.ProxyClientConfig != nil {
				configs[i] = append(configs[i], buildProxyAuthConfig(sc.Namespace, sc.AsMapKey("consulsd", i), config.ProxyClientConfig, ssCache)...)
			}

			if config.FollowRedirects != nil {
				cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: config.FollowRedirects})
			}

			if config.TLSConfig != nil {
				configs[i] = addTLStoYaml(configs[i], sc.Namespace, config.TLSConfig, false)
			}
		}

		cfg = append(cfg, yaml.MapItem{
			Key:   "consul_sd_configs",
			Value: configs,
		})
	}

	// build dNSSDConfig
	if len(sc.Spec.DNSSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.DNSSDConfigs))
		for i, config := range sc.Spec.DNSSDConfigs {
			configs[i] = []yaml.MapItem{
				{
					Key:   "names",
					Value: config.Names,
				},
			}

			if config.Type != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "type",
					Value: config.Type,
				})
			}

			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "dns_sd_configs",
			Value: configs,
		})
	}

	// build eC2SDConfig
	if len(sc.Spec.EC2SDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.EC2SDConfigs))
		for i, config := range sc.Spec.EC2SDConfigs {
			if config.Region != nil {
				configs[i] = []yaml.MapItem{
					{
						Key:   "region",
						Value: config.Region,
					},
				}
			}

			if config.AccessKey != nil {
				if s, ok := ssCache.authorizationSecrets[sc.AsMapKey("ec2sdAccess", i)]; ok {
					configs[i] = append(configs[i], yaml.MapItem{Key: "access_key", Value: s})
				}
			}
			if config.SecretKey != nil {
				if s, ok := ssCache.authorizationSecrets[sc.AsMapKey("ec2sdSecret", i)]; ok {
					configs[i] = append(configs[i], yaml.MapItem{Key: "secret_key", Value: s})
				}
			}

			if config.RoleARN != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "role_arn",
					Value: config.RoleARN,
				})
			}
			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}

			if config.Filters != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "filters",
					Value: config.Filters,
				})
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "ec2_sd_configs",
			Value: configs,
		})
	}

	// build azureSDConfig
	if len(sc.Spec.AzureSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.AzureSDConfigs))
		for i, config := range sc.Spec.AzureSDConfigs {
			if config.Environment != nil {
				configs[i] = []yaml.MapItem{
					{
						Key:   "environment",
						Value: config.Environment,
					},
				}
			}

			if config.AuthenticationMethod != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "authentication_method",
					Value: config.AuthenticationMethod,
				})
			}

			if config.SubscriptionID != "" {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "subscription_id",
					Value: config.SubscriptionID,
				})
			}

			if config.TenantID != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "tenant_id",
					Value: config.TenantID,
				})
			}

			if config.ClientID != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "client_id",
					Value: config.ClientID,
				})
			}

			if config.ClientSecret != nil {
				if s, ok := ssCache.oauth2Secrets[sc.AsMapKey("azuresd", i)]; ok {
					configs[i] = append(configs[i], yaml.MapItem{Key: "client_secret", Value: s.ClientSecret})
				}
			}

			if config.ResourceGroup != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "resource_group",
					Value: config.ResourceGroup,
				})
			}
			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "azure_sd_configs",
			Value: configs,
		})
	}

	// build gceSDConfig
	if len(sc.Spec.GCESDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.GCESDConfigs))
		for i, config := range sc.Spec.GCESDConfigs {
			configs[i] = []yaml.MapItem{
				{
					Key:   "project",
					Value: config.Project,
				},
			}

			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "zone",
				Value: config.Zone,
			})

			if config.Filter != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "filter",
					Value: config.Filter,
				})
			}
			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}

			if config.TagSeparator != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "tag_separator",
					Value: config.TagSeparator,
				})
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "gce_sd_configs",
			Value: configs,
		})
	}

	// build openStackSDConfig
	if len(sc.Spec.OpenStackSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.OpenStackSDConfigs))
		for i, config := range sc.Spec.OpenStackSDConfigs {
			configs[i] = []yaml.MapItem{
				{
					Key:   "role",
					Value: strings.ToLower(config.Role),
				},
			}

			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "region",
				Value: config.Region,
			})

			if config.IdentityEndpoint != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "identity_endpoint",
					Value: config.IdentityEndpoint,
				})
			}

			if config.Username != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "username",
					Value: config.Username,
				})
			}

			if config.UserID != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "userid",
					Value: config.UserID,
				})
			}

			if config.Password != nil {
				if s, ok := ssCache.authorizationSecrets[sc.AsMapKey("openstacksd_password", i)]; ok {
					configs[i] = append(configs[i], yaml.MapItem{Key: "password", Value: s})
				}
			}

			if config.DomainName != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "domain_name",
					Value: config.DomainName,
				})
			}

			if config.DomainID != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "domain_id",
					Value: config.DomainID,
				})
			}

			if config.ProjectName != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "project_name",
					Value: config.ProjectName,
				})
			}

			if config.ProjectID != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "project_id",
					Value: config.ProjectID,
				})
			}

			if config.ApplicationCredentialName != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "application_credential_name",
					Value: config.ApplicationCredentialName,
				})
			}

			if config.ApplicationCredentialID != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "application_credential_id",
					Value: config.ApplicationCredentialID,
				})
			}

			if config.ApplicationCredentialSecret != nil {
				if s, ok := ssCache.authorizationSecrets[sc.AsMapKey("openstacksd_app", i)]; ok {
					configs[i] = append(configs[i], yaml.MapItem{Key: "application_credential_secret", Value: s})
				}
			}

			if config.AllTenants != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "all_tenants",
					Value: config.AllTenants,
				})
			}

			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}

			if config.Availability != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "availability",
					Value: config.Availability,
				})
			}

			if config.TLSConfig != nil {
				configs[i] = addTLStoYaml(configs[i], sc.Namespace, config.TLSConfig, false)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "openstack_sd_configs",
			Value: configs,
		})
	}

	// build digitalOceanSDConfig
	if len(sc.Spec.DigitalOceanSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(sc.Spec.DigitalOceanSDConfigs))
		for i, config := range sc.Spec.DigitalOceanSDConfigs {
			configs[i] = addAuthorizationConfig(configs[i], sc.AsMapKey("digitaloceansd", i), config.Authorization, ssCache.authorizationSecrets)
			configs[i] = addOAuth2Config(configs[i], sc.AsMapKey("digitaloceansd", i), config.OAuth2, ssCache.oauth2Secrets)
			if config.ProxyURL != nil {
				configs[i] = append(configs[i], yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
			}
			if config.ProxyClientConfig != nil {
				configs[i] = append(configs[i], buildProxyAuthConfig(sc.Namespace, sc.AsMapKey("digitaloceansd", i), config.ProxyClientConfig, ssCache)...)
			}

			if config.FollowRedirects != nil {
				cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: config.FollowRedirects})
			}

			if config.TLSConfig != nil {
				configs[i] = addTLStoYaml(configs[i], sc.Namespace, config.TLSConfig, false)
			}

			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "digitalocean_sd_configs",
			Value: configs,
		})
	}
	return cfg
}
