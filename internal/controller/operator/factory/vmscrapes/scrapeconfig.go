package vmscrapes

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
	sp *vmv1beta1.CommonScrapeParams,
	sc *vmv1beta1.VMScrapeConfig,
	ac *build.AssetsCache,
) (yaml.MapSlice, error) {
	spec := &sc.Spec
	se := &sp.CommonScrapeSecurityEnforcements
	jobName := fmt.Sprintf("scrapeConfig/%s/%s", sc.Namespace, sc.Name)
	cfg := yaml.MapSlice{{
		Key:   "job_name",
		Value: jobName,
	}}

	scrapeClass := getScrapeClass(spec.ScrapeClassName, sp)
	if scrapeClass != nil {
		mergeEndpointAuthWithScrapeClass(&spec.EndpointAuth, scrapeClass)
		mergeEndpointRelabelingsWithScrapeClass(&spec.EndpointRelabelings, scrapeClass)
	}

	setScrapeIntervalToWithLimit(ctx, &spec.EndpointScrapeParams, sp)

	cfg = addCommonScrapeParamsTo(cfg, spec.EndpointScrapeParams, se)

	var relabelings []yaml.MapSlice
	for _, c := range spec.RelabelConfigs {
		relabelings = append(relabelings, generateRelabelConfig(c))
	}
	for _, trc := range sp.ScrapeConfigRelabelTemplate {
		relabelings = append(relabelings, generateRelabelConfig(trc))
	}
	// Because of security risks, whenever enforcedNamespaceLabel is set, we want to append it to the
	// relabel_configs as the last relabeling, to ensure it overrides any other relabelings.
	relabelings = enforceNamespaceLabel(relabelings, sc.Namespace, se.EnforcedNamespaceLabel)

	cfg = append(cfg, yaml.MapItem{Key: "relabel_configs", Value: relabelings})
	cfg = addMetricRelabelingsTo(cfg, spec.MetricRelabelConfigs, se)
	if c, err := buildVMScrapeParams(sc.Namespace, spec.VMScrapeParams, ac); err != nil {
		return nil, err
	} else {
		cfg = append(cfg, c...)
	}
	cfg, err := addEndpointAuthTo(cfg, &spec.EndpointAuth, sc.Namespace, ac)
	if err != nil {
		return nil, err
	}

	// build staticConfig
	if len(spec.StaticConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.StaticConfigs))
		for i, config := range spec.StaticConfigs {
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
	if len(spec.FileSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.FileSDConfigs))
		for i, config := range spec.FileSDConfigs {
			configs[i] = []yaml.MapItem{{
				Key:   "files",
				Value: config.Files,
			}}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "file_sd_configs",
			Value: configs,
		})
	}

	// build httpSDConfig
	if len(spec.HTTPSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.HTTPSDConfigs))
		for i, config := range spec.HTTPSDConfigs {
			configs[i] = []yaml.MapItem{{
				Key:   "url",
				Value: config.URL,
			}}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "http_sd_configs",
			Value: configs,
		})
	}

	// build kubernetesSDConfig
	if len(spec.KubernetesSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.KubernetesSDConfigs))
		for i, config := range spec.KubernetesSDConfigs {
			if config.APIServer != nil {
				configs[i] = []yaml.MapItem{{
					Key:   "api_server",
					Value: config.APIServer,
				}}
			}
			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "role",
				Value: config.Role,
			})

			if config.Namespaces != nil {
				namespaces := []yaml.MapItem{{
					Key:   "names",
					Value: config.Namespaces.Names,
				}}

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
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
			configs[i] = addAttachMetadata(configs[i], &config.AttachMetadata, config.Role)
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
	if len(spec.ConsulSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.ConsulSDConfigs))
		for i, config := range spec.ConsulSDConfigs {
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
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}

		cfg = append(cfg, yaml.MapItem{
			Key:   "consul_sd_configs",
			Value: configs,
		})
	}

	// build dNSSDConfig
	if len(spec.DNSSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.DNSSDConfigs))
		for i, config := range spec.DNSSDConfigs {
			configs[i] = []yaml.MapItem{{
				Key:   "names",
				Value: config.Names,
			}}

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
	if len(spec.EC2SDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.EC2SDConfigs))
		for i, config := range spec.EC2SDConfigs {
			if config.Region != nil {
				configs[i] = []yaml.MapItem{{
					Key:   "region",
					Value: config.Region,
				}}
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
	if len(spec.AzureSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.AzureSDConfigs))
		for i, config := range spec.AzureSDConfigs {
			if config.Environment != nil {
				configs[i] = []yaml.MapItem{{
					Key:   "environment",
					Value: config.Environment,
				}}
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
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "azure_sd_configs",
			Value: configs,
		})
	}

	// build gceSDConfig
	if len(spec.GCESDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.GCESDConfigs))
		for i, config := range spec.GCESDConfigs {
			configs[i] = []yaml.MapItem{{
				Key:   "project",
				Value: config.Project,
			}}

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
	if len(spec.OpenStackSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.OpenStackSDConfigs))
		for i, config := range spec.OpenStackSDConfigs {
			configs[i] = []yaml.MapItem{{
				Key:   "role",
				Value: strings.ToLower(config.Role),
			}}

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
	if len(spec.DigitalOceanSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.DigitalOceanSDConfigs))
		for i, config := range spec.DigitalOceanSDConfigs {
			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "digitalocean_sd_configs",
			Value: configs,
		})
	}

	// build hentznerSDConfig
	if len(spec.HetznerSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.HetznerSDConfigs))
		for i, config := range spec.HetznerSDConfigs {
			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "role",
				Value: strings.ToLower(config.Role),
			})
			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "hetzner_sd_configs",
			Value: configs,
		})
	}

	// build eurekaSDConfig
	if len(spec.EurekaSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.EurekaSDConfigs))
		for i, config := range spec.EurekaSDConfigs {
			if len(config.Server) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "server",
					Value: config.Server,
				})
			}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "eureka_sd_configs",
			Value: configs,
		})
	}

	// build puppetDBSDConfig
	if len(spec.PuppetDBSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.PuppetDBSDConfigs))
		for i, config := range spec.PuppetDBSDConfigs {
			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "url",
				Value: config.URL,
			}, yaml.MapItem{
				Key:   "query",
				Value: config.Query,
			})
			if config.IncludeParameters {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "include_parameters",
					Value: config.IncludeParameters,
				})
			}
			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "puppetdb_sd_configs",
			Value: configs,
		})
	}

	// build vultrSDConfig
	if len(spec.VultrSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.VultrSDConfigs))
		for i, config := range spec.VultrSDConfigs {
			if len(config.Label) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "label",
					Value: config.Label,
				})
			}
			if len(config.MainIP) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "main_ip",
					Value: config.MainIP,
				})
			}
			if len(config.Region) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "region",
					Value: config.Region,
				})
			}
			if len(config.FirewallGroupID) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "firewall_group_id",
					Value: config.FirewallGroupID,
				})
			}
			if len(config.Hostname) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "hostname",
					Value: config.Hostname,
				})
			}
			if config.Port != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "port",
					Value: config.Port,
				})
			}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "vultr_sd_configs",
			Value: configs,
		})
	}

	// build kumaSDConfig
	if len(spec.KumaSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.KumaSDConfigs))
		for i, config := range spec.KumaSDConfigs {
			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "server",
				Value: config.Server,
			})
			if len(config.ClientID) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "client_id",
					Value: config.ClientID,
				})
			}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "kuma_sd_configs",
			Value: configs,
		})
	}

	// build ovhcloudSDConfig
	if len(spec.OVHCloudSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.OVHCloudSDConfigs))
		for i, config := range spec.OVHCloudSDConfigs {
			svc := config.Service
			switch svc {
			case "VPS":
				svc = "vps"
			case "DedicatedServer":
				svc = "dedicated_server"
			}
			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "application_key",
				Value: config.ApplicationKey,
			}, yaml.MapItem{
				Key:   "service",
				Value: svc,
			})
			if secret, err := ac.LoadKeyFromSecret(sc.Namespace, config.ApplicationSecret); err != nil {
				return nil, err
			} else {
				configs[i] = append(configs[i], yaml.MapItem{Key: "application_secret", Value: secret})
			}
			if secret, err := ac.LoadKeyFromSecret(sc.Namespace, config.ConsumerKey); err != nil {
				return nil, err
			} else {
				configs[i] = append(configs[i], yaml.MapItem{Key: "consumer_key", Value: secret})
			}
			if len(config.Endpoint) > 0 {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "endpoint",
					Value: config.Endpoint,
				})
			}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}
		cfg = append(cfg, yaml.MapItem{
			Key:   "ovhcloud_sd_configs",
			Value: configs,
		})
	}

	// build nomadSDConfig
	if len(spec.NomadSDConfigs) > 0 {
		configs := make([][]yaml.MapItem, len(spec.NomadSDConfigs))
		for i, config := range spec.NomadSDConfigs {
			configs[i] = append(configs[i], yaml.MapItem{
				Key:   "server",
				Value: config.Server,
			})

			if config.Namespace != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "namespace",
					Value: config.Namespace,
				})
			}

			if config.Region != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "region",
					Value: config.Region,
				})
			}

			if config.TagSeparator != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "tag_separator",
					Value: config.TagSeparator,
				})
			}

			if config.AllowStale != nil {
				configs[i] = append(configs[i], yaml.MapItem{
					Key:   "allow_stale",
					Value: config.AllowStale,
				})
			}
			c, err := generateHTTPSDOptions(config.HTTPSDOptions, sc.Namespace, ac)
			if err != nil {
				return nil, err
			} else if len(c) > 0 {
				configs[i] = append(configs[i], c...)
			}
		}

		cfg = append(cfg, yaml.MapItem{
			Key:   "nomad_sd_configs",
			Value: configs,
		})
	}
	return cfg, nil
}

func generateHTTPSDOptions(config vmv1beta1.HTTPSDOptions, namespace string, ac *build.AssetsCache) ([]yaml.MapItem, error) {
	var cfg []yaml.MapItem
	if config.BasicAuth != nil {
		if c, err := ac.BasicAuthToYAML(namespace, config.BasicAuth); err != nil {
			return nil, err
		} else {
			cfg = append(cfg, yaml.MapItem{Key: "basic_auth", Value: c})
		}
	}
	if config.Authorization != nil {
		if c, err := ac.AuthorizationToYAML(namespace, config.Authorization); err != nil {
			return nil, err
		} else {
			cfg = append(cfg, c...)
		}
	}
	if config.OAuth2 != nil {
		if c, err := ac.OAuth2ToYAML(namespace, config.OAuth2); err != nil {
			return nil, err
		} else {
			cfg = append(cfg, c...)
		}
	}
	if config.ProxyURL != nil {
		cfg = append(cfg, yaml.MapItem{Key: "proxy_url", Value: config.ProxyURL})
	}
	if config.ProxyClientConfig != nil {
		if c, err := ac.ProxyAuthToYAML(namespace, config.ProxyClientConfig); err != nil {
			return nil, err
		} else {
			cfg = append(cfg, c...)
		}
	}

	if config.FollowRedirects != nil {
		cfg = append(cfg, yaml.MapItem{Key: "follow_redirects", Value: config.FollowRedirects})
	}

	if config.TLSConfig != nil {
		if c, err := ac.TLSToYAML(namespace, "", config.TLSConfig); err != nil {
			return nil, err
		} else if len(c) > 0 {
			cfg = append(cfg, yaml.MapItem{Key: "tls_config", Value: c})
		}
	}
	return cfg, nil
}
