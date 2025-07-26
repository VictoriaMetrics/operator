package vmagent

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func generateScrapeConfig(
	ctx context.Context,
	cr *vmv1beta1.VMAgent,
	sc *vmv1beta1.VMScrapeConfig,
	ac *build.AssetsCache,
) (yaml.MapSlice, error) {
	se := cr.Spec.VMAgentSecurityEnforcements
	jobName := fmt.Sprintf("scrapeConfig/%s/%s", sc.Namespace, sc.Name)
	cfg := yaml.MapSlice{
		{
			Key:   "job_name",
			Value: jobName,
		},
	}

	setScrapeIntervalToWithLimit(ctx, &sc.Spec.EndpointScrapeParams, cr)

	cfg = addCommonScrapeParamsTo(cfg, sc.Spec.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice
	for _, c := range sc.Spec.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}
	for _, trc := range cr.Spec.ScrapeConfigRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, sc.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, sc.Spec.MetricRelabelConfigs, se)
	if c, err := buildVMScrapeParams(sc.Namespace, sc.Spec.VMScrapeParams, ac); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	cfg, err := addEndpointAuthTo(cfg, &sc.Spec.EndpointAuth, sc.Namespace, ac)
	if err != nil {
		return nil, err
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
				if c, err := ac.BasicAuthToYAML(sc.Namespace, config.BasicAuth); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "basic_auth", Value: c})
				}
			}
			if config.Authorization != nil {
				if c, err := ac.AuthorizationToYAML(sc.Namespace, config.Authorization); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}
			if config.TLSConfig != nil {
				if c, err := ac.TLSToYAML(sc.Namespace, "", config.TLSConfig); err != nil {
					return nil, err
				} else if len(c) > 0 {
					configs[i] = append(configs[i], yaml.MapItem{Key: "tls_config", Value: c})
				}
			}
			if config.ProxyURL != nil {
				configs[i] = append(configs[i], yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
			}
			if config.ProxyClientConfig != nil {
				if c, err := ac.ProxyAuthToYAML(sc.Namespace, config.ProxyClientConfig); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
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
				if c, err := ac.BasicAuthToYAML(sc.Namespace, config.BasicAuth); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "basic_auth", Value: c})
				}
			}
			if config.Authorization != nil {
				if c, err := ac.AuthorizationToYAML(sc.Namespace, config.Authorization); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}
			if config.TLSConfig != nil {
				if c, err := ac.TLSToYAML(sc.Namespace, "", config.TLSConfig); err != nil {
					return nil, err
				} else if len(c) > 0 {
					configs[i] = append(configs[i], yaml.MapItem{Key: "tls_config", Value: c})
				}
			}
			if config.OAuth2 != nil {
				if c, err := ac.OAuth2ToYAML(sc.Namespace, config.OAuth2); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}
			if config.ProxyURL != nil {
				configs[i] = append(configs[i], yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
			}
			if config.ProxyClientConfig != nil {
				if c, err := ac.ProxyAuthToYAML(sc.Namespace, config.ProxyClientConfig); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
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
					configs[i] = addAttachMetadata(configs[i], &config.AttachMetadata)
				}
			}

			selectors := make([][]yaml.MapItem, len(config.Selectors))
			for i, s := range config.Selectors {
				selectors[i] = []yaml.MapItem{
					{
						Key:   "role",
						Value: strings.ToLower(s.Role),
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
				if secret, err := ac.LoadKeyFromSecret(sc.Namespace, config.TokenRef); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "bearer_token", Value: secret})
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
			if len(config.Filter) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "filter",
					Value: config.Filter,
				})
			}

			if config.BasicAuth != nil {
				if c, err := ac.BasicAuthToYAML(sc.Namespace, config.BasicAuth); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "basic_auth", Value: c})
				}
			}
			if config.Authorization != nil {
				if c, err := ac.AuthorizationToYAML(sc.Namespace, config.Authorization); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}
			if config.OAuth2 != nil {
				if c, err := ac.OAuth2ToYAML(sc.Namespace, config.OAuth2); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}
			if config.ProxyURL != nil {
				configs[i] = append(configs[i], yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
			}
			if config.ProxyClientConfig != nil {
				if c, err := ac.ProxyAuthToYAML(sc.Namespace, config.ProxyClientConfig); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}

			if config.FollowRedirects != nil {
				cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: config.FollowRedirects})
			}

			if config.TLSConfig != nil {
				if c, err := ac.TLSToYAML(sc.Namespace, "", config.TLSConfig); err != nil {
					return nil, err
				} else if len(c) > 0 {
					configs[i] = append(configs[i], yaml.MapItem{Key: "tls_config", Value: c})
				}
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
				if secret, err := ac.LoadKeyFromSecret(sc.Namespace, config.AccessKey); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "access_key", Value: secret})
				}
			}
			if config.SecretKey != nil {
				if secret, err := ac.LoadKeyFromSecret(sc.Namespace, config.SecretKey); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "secret_key", Value: secret})
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
				if secret, err := ac.LoadKeyFromSecret(sc.Namespace, config.ClientSecret); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "client_secret", Value: secret})
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
				if secret, err := ac.LoadKeyFromSecret(sc.Namespace, config.Password); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "password", Value: secret})
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
				if secret, err := ac.LoadKeyFromSecret(sc.Namespace, config.ApplicationCredentialSecret); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], yaml.MapItem{Key: "application_credential_secret", Value: secret})
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
				if c, err := ac.TLSToYAML(sc.Namespace, "", config.TLSConfig); err != nil {
					return nil, err
				} else if len(c) > 0 {
					configs[i] = append(configs[i], yaml.MapItem{Key: "tls_config", Value: c})
				}
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
			if config.Authorization != nil {
				if c, err := ac.AuthorizationToYAML(sc.Namespace, config.Authorization); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}
			if config.OAuth2 != nil {
				if c, err := ac.OAuth2ToYAML(sc.Namespace, config.OAuth2); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}
			if config.ProxyURL != nil {
				configs[i] = append(configs[i], yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
			}
			if config.ProxyClientConfig != nil {
				if c, err := ac.ProxyAuthToYAML(sc.Namespace, config.ProxyClientConfig); err != nil {
					return nil, err
				} else {
					configs[i] = append(configs[i], c...)
				}
			}

			if config.FollowRedirects != nil {
				cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: config.FollowRedirects})
			}

			if config.TLSConfig != nil {
				if c, err := ac.TLSToYAML(sc.Namespace, "", config.TLSConfig); err != nil {
					return nil, err
				} else if len(c) > 0 {
					configs[i] = append(configs[i], yaml.MapItem{Key: "tls_config", Value: c})
				}
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
	return cfg, nil
}
