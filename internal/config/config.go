package config

import (
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	"github.com/caarlos0/env/v11"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	UnLimitedResource = "unlimited"
)

func getVersion(defaultVersion string) string {
	v := buildinfo.ShortVersion()
	if len(v) == 0 {
		return defaultVersion
	}
	return v
}

var (
	opConf   *BaseOperatorConf
	initConf sync.Once

	defaultEnvs = map[string]string{
		"VM_METRICS_VERSION":  "v1.134.0",
		"VM_LOGS_VERSION":     "v1.43.1",
		"VM_ANOMALY_VERSION":  "v1.28.5",
		"VM_TRACES_VERSION":   "v0.7.0",
		"VM_OPERATOR_VERSION": getVersion("v0.66.1"),
	}
)

func getEnvOpts() env.Options {
	envOpts := env.Options{
		DefaultValueTagName:   "default",
		PrefixTagName:         "prefix",
		UseFieldNameByDefault: true,
		Environment:           env.ToMap(os.Environ()),
	}
	for n, ov := range defaultEnvs {
		if v, ok := envOpts.Environment[n]; !ok || v == "" {
			envOpts.Environment[n] = ov
		}
	}
	return envOpts
}

// ApplicationDefaults is useful for generic default building
// uses the same memory as application default at config
type ApplicationDefaults struct {
	Image               string
	Version             string
	Port                string
	UseDefaultResources bool
	Resource            struct {
		Limit struct {
			Mem string
			Cpu string
		}
		Request struct {
			Mem string
			Cpu string
		}
	}
}

// Resource is useful for generic resource building
// uses the same memory layout as resources at config
type Resource struct {
	Limit struct {
		Mem string
		Cpu string
	}
	Request struct {
		Mem string
		Cpu string
	}
}

//genvars:true
type BaseOperatorConf struct {
	MetricsVersion    string `default:"${VM_METRICS_VERSION}" env:"VM_METRICS_VERSION,expand"`
	LogsVersion       string `default:"${VM_LOGS_VERSION}" env:"VM_LOGS_VERSION,expand"`
	AnomalyVersion    string `default:"${VM_ANOMALY_VERSION}" env:"VM_ANOMALY_VERSION,expand"`
	TracesVertsion    string `default:"${VM_TRACES_VERSION}" env:"VM_TRACES_VERSION,expand"`
	OperatorVersion   string `default:"${VM_OPERATOR_VERSION}" env:"VM_OPERATOR_VERSION,expand"`
	GatewayAPIEnabled bool   `default:"false" env:"VM_GATEWAY_API_ENABLED"`

	// Defines a list of namespaces to be watched by operator.
	// Operator don't perform any cluster wide API calls if namespaces not empty.
	// In case of empty list it performs only clusterwide api calls.
	WatchNamespaces []string `default:"" env:"WATCH_NAMESPACE"`

	// container registry name prefix, e.g. docker.io
	ContainerRegistry string `default:"" env:"VM_CONTAINERREGISTRY"`
	// Deprecated: use VM_CONFIG_RELOADER_IMAGE instead
	CustomConfigReloaderImage string `env:"VM_CUSTOMCONFIGRELOADERIMAGE"`
	ConfigReloaderImage       string `default:"victoriametrics/operator:config-reloader-${VM_OPERATOR_VERSION}" env:"VM_CONFIG_RELOADER_IMAGE,expand"`
	PSPAutoCreateEnabled      bool   `default:"false" env:"VM_PSPAUTOCREATEENABLED"`
	EnableTCP6                bool   `default:"false" env:"VM_ENABLETCP6"`

	// defines global resource.limits.cpu for all config-reloader containers
	ConfigReloaderLimitCPU string `default:"unlimited" env:"VM_CONFIG_RELOADER_LIMIT_CPU"`
	// defines global resource.limits.memory for all config-reloader containers
	ConfigReloaderLimitMemory string `default:"unlimited" env:"VM_CONFIG_RELOADER_LIMIT_MEMORY"`
	// defines global resource.requests.cpu for all config-reloader containers
	ConfigReloaderRequestCPU string `default:"10m" env:"VM_CONFIG_RELOADER_REQUEST_CPU"`
	// defines global resource.requests.memory for all config-reloader containers
	ConfigReloaderRequestMemory string `default:"25Mi" env:"VM_CONFIG_RELOADER_REQUEST_MEMORY"`

	VLogsDefault struct {
		Image               string `default:"victoriametrics/victoria-logs"`
		Version             string `env:",expand" default:"${VM_LOGS_VERSION}"`
		Port                string `default:"9428"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"1500Mi"`
				Cpu string `default:"1200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"150m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VLOGSDEFAULT_"`

	VLAgentDefault struct {
		Image               string `default:"victoriametrics/vlagent"`
		Version             string `env:",expand" default:"${VM_LOGS_VERSION}"`
		Port                string `default:"9429"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"200Mi"`
				Cpu string `default:"50m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VLAGENTDEFAULT_"`

	VLSingleDefault struct {
		Image               string `default:"victoriametrics/victoria-logs"`
		Version             string `env:",expand" default:"${VM_LOGS_VERSION}"`
		Port                string `default:"9428"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"1500Mi"`
				Cpu string `default:"1200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"150m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VLSINGLEDEFAULT_"`

	VTSingleDefault struct {
		Image               string `default:"victoriametrics/victoria-traces"`
		Version             string `env:",expand" default:"${VM_TRACES_VERSION}"`
		Port                string `default:"10428"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"1500Mi"`
				Cpu string `default:"1200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"150m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VTSINGLEDEFAULT_"`

	VMAlertDefault struct {
		Image               string `default:"victoriametrics/vmalert"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}"`
		Port                string `default:"8080"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"200Mi"`
				Cpu string `default:"50m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VMALERTDEFAULT_"`

	VMServiceScrapeDefault struct {
		// Use endpointslices instead of endpoints as discovery role
		// for vmservicescrape when generate scrape config for vmagent.
		EnforceEndpointSlices bool `default:"false" env:"ENFORCEENDPOINTSLICES"`
	} `prefix:"VM_VMSERVICESCRAPEDEFAULT_"`

	VMAgentDefault struct {
		Image               string `default:"victoriametrics/vmagent"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}"`
		Port                string `default:"8429"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"200Mi"`
				Cpu string `default:"50m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VMAGENTDEFAULT_"`

	VMAnomalyDefault struct {
		Image               string `default:"victoriametrics/vmanomaly"`
		Version             string `env:",expand" default:"${VM_ANOMALY_VERSION}"`
		Port                string `default:"8490"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"200Mi"`
				Cpu string `default:"50m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VMANOMALYDEFAULT_"`

	VMSingleDefault struct {
		Image               string `default:"victoriametrics/victoria-metrics"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}"`
		Port                string `default:"8429"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"1500Mi"`
				Cpu string `default:"1200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"150m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VMSINGLEDEFAULT_"`

	VMClusterDefault struct {
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		VMSelectDefault     struct {
			Image    string `default:"victoriametrics/vmselect"`
			Version  string `env:",expand" default:"${VM_METRICS_VERSION}-cluster"`
			Port     string `default:"8481"`
			Resource struct {
				Limit struct {
					Mem string `default:"1000Mi"`
					Cpu string `default:"500m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"500Mi"`
					Cpu string `default:"100m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"VMSELECTDEFAULT_"`
		VMStorageDefault struct {
			Image        string `default:"victoriametrics/vmstorage"`
			Version      string `env:",expand" default:"${VM_METRICS_VERSION}-cluster"`
			VMInsertPort string `default:"8400" env:"VMINSERTPORT"`
			VMSelectPort string `default:"8401" env:"VMSELECTPORT"`
			Port         string `default:"8482"`
			Resource     struct {
				Limit struct {
					Mem string `default:"1500Mi"`
					Cpu string `default:"1000m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"500Mi"`
					Cpu string `default:"250m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"VMSTORAGEDEFAULT_"`
		VMInsertDefault struct {
			Image    string `default:"victoriametrics/vminsert"`
			Version  string `env:",expand" default:"${VM_METRICS_VERSION}-cluster"`
			Port     string `default:"8480"`
			Resource struct {
				Limit struct {
					Mem string `default:"500Mi"`
					Cpu string `default:"500m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"200Mi"`
					Cpu string `default:"150m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"VMINSERTDEFAULT_"`
	} `prefix:"VM_VMCLUSTERDEFAULT_"`

	VMAlertManager struct {
		AlertmanagerDefaultBaseImage string `default:"prom/alertmanager" env:"ALERTMANAGERDEFAULTBASEIMAGE"`
		AlertManagerVersion          string `default:"v0.29.0" env:"ALERTMANAGERVERSION"`
		UseDefaultResources          bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource                     struct {
			Limit struct {
				Mem string `default:"256Mi"`
				Cpu string `default:"100m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"56Mi"`
				Cpu string `default:"30m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VMALERTMANAGER_"`

	DisableSelfServiceScrapeCreation bool `default:"false" env:"VM_DISABLESELFSERVICESCRAPECREATION"`
	VMBackup                         struct {
		Image               string `default:"victoriametrics/vmbackupmanager"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}-enterprise"`
		Port                string `default:"8300"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"500m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"200Mi"`
				Cpu string `default:"150m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VMBACKUP_"`
	VMAuthDefault struct {
		Image               string `default:"victoriametrics/vmauth"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}"`
		Port                string `default:"8427"`
		UseDefaultResources bool   `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem string `default:"300Mi"`
				Cpu string `default:"200m"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem string `default:"100Mi"`
				Cpu string `default:"50m"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
	} `prefix:"VM_VMAUTHDEFAULT_"`

	VLClusterDefault struct {
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		VLSelectDefault     struct {
			Image    string `default:"victoriametrics/victoria-logs"`
			Version  string `env:",expand" default:"${VM_LOGS_VERSION}"`
			Port     string `default:"9471"`
			Resource struct {
				Limit struct {
					Mem string `default:"1024Mi"`
					Cpu string `default:"1000m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"256Mi"`
					Cpu string `default:"100m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"VLSELECTDEFAULT_"`
		VLStorageDefault struct {
			Image    string `default:"victoriametrics/victoria-logs"`
			Version  string `env:",expand" default:"${VM_LOGS_VERSION}"`
			Port     string `default:"9491"`
			Resource struct {
				Limit struct {
					Mem string `default:"2048Mi"`
					Cpu string `default:"1000m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"512Mi"`
					Cpu string `default:"200m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"VLSTORAGEDEFAULT_"`
		VLInsertDefault struct {
			Image    string `default:"victoriametrics/victoria-logs"`
			Version  string `env:",expand" default:"${VM_LOGS_VERSION}"`
			Port     string `default:"9481"`
			Resource struct {
				Limit struct {
					Mem string `default:"1024Mi"`
					Cpu string `default:"1000m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"256Mi"`
					Cpu string `default:"100m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"VLINSERTDEFAULT_"`
	} `prefix:"VM_VLCLUSTERDEFAULT_"`

	VTClusterDefault struct {
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		SelectDefault       struct {
			Image    string `default:"victoriametrics/victoria-traces"`
			Version  string `env:",expand" default:"${VM_TRACES_VERSION}"`
			Port     string `default:"10471"`
			Resource struct {
				Limit struct {
					Mem string `default:"1024Mi"`
					Cpu string `default:"1000m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"256Mi"`
					Cpu string `default:"100m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"SELECT_"`
		StorageDefault struct {
			Image    string `default:"victoriametrics/victoria-traces"`
			Version  string `env:",expand" default:"${VM_TRACES_VERSION}"`
			Port     string `default:"10491"`
			Resource struct {
				Limit struct {
					Mem string `default:"2048Mi"`
					Cpu string `default:"1000m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"512Mi"`
					Cpu string `default:"200m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"STORAGE_"`
		InsertDefault struct {
			Image    string `default:"victoriametrics/victoria-traces"`
			Version  string `env:",expand" default:"${VM_TRACES_VERSION}"`
			Port     string `default:"10481"`
			Resource struct {
				Limit struct {
					Mem string `default:"1024Mi"`
					Cpu string `default:"1000m"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem string `default:"256Mi"`
					Cpu string `default:"100m"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
		} `prefix:"INSERT_"`
	} `prefix:"VM_VTCLUSTERDEFAULT_"`

	EnabledPrometheusConverter struct {
		PodMonitor         bool `default:"true" env:"PODMONITOR"`
		ServiceScrape      bool `default:"true" env:"SERVICESCRAPE"`
		PrometheusRule     bool `default:"true" env:"PROMETHEUSRULE"`
		Probe              bool `default:"true" env:"PROBE"`
		AlertmanagerConfig bool `default:"true" env:"ALERTMANAGERCONFIG"`
		ScrapeConfig       bool `default:"true" env:"SCRAPECONFIG"`
	} `prefix:"VM_ENABLEDPROMETHEUSCONVERTER_"`
	// adds compare-options and sync-options for prometheus objects converted by operator.
	// It helps to properly use converter with ArgoCD
	PrometheusConverterAddArgoCDIgnoreAnnotations bool `default:"false" env:"VM_PROMETHEUSCONVERTERADDARGOCDIGNOREANNOTATIONS"`
	EnabledPrometheusConverterOwnerReferences     bool `default:"false" env:"VM_ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES"`
	// allows filtering for converted labels, labels with matched prefix will be ignored
	FilterPrometheusConverterLabelPrefixes []string `default:"" env:"VM_FILTERPROMETHEUSCONVERTERLABELPREFIXES"`
	// allows filtering for converted annotations, annotations with matched prefix will be ignored
	FilterPrometheusConverterAnnotationPrefixes []string `default:"" env:"VM_FILTERPROMETHEUSCONVERTERANNOTATIONPREFIXES"`
	// Defines domain name suffix for in-cluster addresses
	// most known ClusterDomainName is .cluster.local
	ClusterDomainName string `default:"" env:"VM_CLUSTERDOMAINNAME"`
	// Defines deadline for deployment/statefulset
	// to transit into ready state
	// to wait for transition to ready state
	AppReadyTimeout time.Duration `default:"80s" env:"VM_APPREADYTIMEOUT"`
	// Defines single pod deadline
	// to wait for transition to ready state
	PodWaitReadyTimeout time.Duration `default:"80s" env:"VM_PODWAITREADYTIMEOUT"`
	// Defines poll interval for pods ready check
	// at statefulset rollout update
	PodWaitReadyIntervalCheck time.Duration `default:"5s" env:"VM_PODWAITREADYINTERVALCHECK"`
	// configures force resync interval for VMAgent, VMAlert, VMAlertmanager and VMAuth.
	ForceResyncInterval time.Duration `default:"60s" env:"VM_FORCERESYNCINTERVAL"`
	// EnableStrictSecurity will add default `securityContext` to pods and containers created by operator
	// Default PodSecurityContext include:
	// 1. RunAsNonRoot: true
	// 2. RunAsUser/RunAsGroup/FSGroup: 65534
	// '65534' refers to 'nobody' in all the used default images like alpine, busybox.
	// If you're using customize image, please make sure '65534' is a valid uid in there or specify SecurityContext.
	// 3. FSGroupChangePolicy: &onRootMismatch
	// If KubeVersion>=1.20, use `FSGroupChangePolicy="onRootMismatch"` to skip the recursive permission change
	// when the root of the volume already has the correct permissions
	// 4. SeccompProfile:
	//      type: RuntimeDefault
	// Use `RuntimeDefault` seccomp profile by default, which is defined by the container runtime,
	// instead of using the Unconfined (seccomp disabled) mode.
	//
	// Default container SecurityContext include:
	// 1. AllowPrivilegeEscalation: false
	// 2. ReadOnlyRootFilesystem: true
	// 3. Capabilities:
	//      drop:
	//        - all
	// turn off `EnableStrictSecurity` by default, see https://github.com/VictoriaMetrics/operator/issues/749 for details
	EnableStrictSecurity bool `default:"false" env:"VM_ENABLESTRICTSECURITY"`
}

// ResyncAfterDuration returns requeue duration for object period reconcile
// adds 10% jitter
func (boc *BaseOperatorConf) ResyncAfterDuration() time.Duration {
	if boc.ForceResyncInterval == 0 {
		return 0
	}
	d := boc.ForceResyncInterval
	dv := d / 10
	if dv > 10*time.Second {
		dv = 10 * time.Second
	}

	p := float64(rand.Int31()) / (1 << 32)

	return boc.ForceResyncInterval + time.Duration(p*float64(dv))
}

// Validate - validates config on best effort.
func (boc BaseOperatorConf) Validate() error {
	for _, ns := range boc.WatchNamespaces {
		if !validNamespaceRegex.MatchString(ns) {
			return fmt.Errorf("namespace=%q doesn't match regex=%q", ns, validNamespaceRegex.String())
		}
	}
	validateResource := func(name string, res Resource) error {
		if res.Request.Mem != UnLimitedResource {
			if _, err := resource.ParseQuantity(res.Request.Mem); err != nil {
				return fmt.Errorf("cannot parse resource request memory for %q: %w", name, err)
			}
		}
		if res.Request.Cpu != UnLimitedResource {
			if _, err := resource.ParseQuantity(res.Request.Cpu); err != nil {
				return fmt.Errorf("cannot parse resource request cpu for %q: %w", name, err)
			}
		}
		if res.Limit.Mem != UnLimitedResource {
			if _, err := resource.ParseQuantity(res.Limit.Mem); err != nil {
				return fmt.Errorf("cannot parse resource limit memory for %q: %w", name, err)
			}
		}
		if res.Limit.Cpu != UnLimitedResource {
			if _, err := resource.ParseQuantity(res.Limit.Cpu); err != nil {
				return fmt.Errorf("cannot parse resource limit cpu for %q: %w", name, err)
			}
		}
		return nil
	}

	if boc.ConfigReloaderLimitMemory != UnLimitedResource {
		if _, err := resource.ParseQuantity(boc.ConfigReloaderLimitMemory); err != nil {
			return fmt.Errorf("cannot parse global config-reloader resource limit memory: %w", err)
		}
	}
	if boc.ConfigReloaderLimitCPU != UnLimitedResource {
		if _, err := resource.ParseQuantity(boc.ConfigReloaderLimitCPU); err != nil {
			return fmt.Errorf("cannot parse global config-reloader resource limit cpu: %w", err)
		}
	}

	if len(boc.ConfigReloaderRequestMemory) > 0 && boc.ConfigReloaderRequestMemory != UnLimitedResource {
		if _, err := resource.ParseQuantity(boc.ConfigReloaderRequestMemory); err != nil {
			return fmt.Errorf("cannot parse global config-reloader resource request memory: %w", err)
		}
	}
	if len(boc.ConfigReloaderRequestCPU) > 0 && boc.ConfigReloaderRequestCPU != UnLimitedResource {
		if _, err := resource.ParseQuantity(boc.ConfigReloaderRequestCPU); err != nil {
			return fmt.Errorf("cannot parse global config-reloader resource request cpu: %w", err)
		}
	}

	if err := validateResource("vmagent", Resource(boc.VMAgentDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmalert", Resource(boc.VMAlertDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmalertmanager", Resource(boc.VMAlertManager.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmselect", Resource(boc.VMClusterDefault.VMSelectDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vminsert", Resource(boc.VMClusterDefault.VMInsertDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmstorage", Resource(boc.VMClusterDefault.VMStorageDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmsingle", Resource(boc.VMSingleDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmbackup", Resource(boc.VMBackup.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlogs", Resource(boc.VLogsDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlagent", Resource(boc.VLAgentDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmanomaly", Resource(boc.VMAnomalyDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlsingle", Resource(boc.VLSingleDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlselect", Resource(boc.VLClusterDefault.VLSelectDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlinsert", Resource(boc.VLClusterDefault.VLInsertDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlstorage", Resource(boc.VLClusterDefault.VLStorageDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vtsingle", Resource(boc.VTSingleDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vtselect", Resource(boc.VTClusterDefault.SelectDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vtinsert", Resource(boc.VTClusterDefault.InsertDefault.Resource)); err != nil {
		return err
	}
	if err := validateResource("vtstorage", Resource(boc.VTClusterDefault.StorageDefault.Resource)); err != nil {
		return err
	}
	return nil
}

// MustGetBaseConfig returns operator configuration with default values populated from env variables
func MustGetBaseConfig() *BaseOperatorConf {
	initConf.Do(func() {
		c, err := env.ParseAsWithOptions[BaseOperatorConf](getEnvOpts())
		if err != nil {
			panic(err)
		}
		if c.CustomConfigReloaderImage != "" {
			c.ConfigReloaderImage = c.CustomConfigReloaderImage
		}
		if err := c.Validate(); err != nil {
			panic(err)
		}
		opConf = &c
	})
	return opConf
}

// GetLocalhost returns localhost value depending on global configuration
func GetLocalhost() string {
	cfg := MustGetBaseConfig()
	if cfg.EnableTCP6 {
		return "localhost"
	}
	return "127.0.0.1"
}

var validNamespaceRegex = regexp.MustCompile(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`)

// IsClusterWideAccessAllowed checks if cluster wide access for components is needed
func IsClusterWideAccessAllowed() bool {
	cfg := MustGetBaseConfig()
	return len(cfg.WatchNamespaces) == 0
}

type Labels struct {
	LabelsString string
	LabelsMap    map[string]string
}

// Implement the flag.Value interface
func (labels *Labels) String() string {
	return labels.LabelsString
}

// Merge labels create a new map with labels merged.
func (labels *Labels) Merge(otherLabels map[string]string) map[string]string {
	mergedLabels := map[string]string{}

	for key, value := range otherLabels {
		mergedLabels[key] = value
	}

	for key, value := range labels.LabelsMap {
		mergedLabels[key] = value
	}
	return mergedLabels
}

// Set implements the flag.Set interface.
func (labels *Labels) Set(value string) error {
	m := map[string]string{}
	if value != "" {
		split := strings.Split(value, ",")
		for _, pair := range split {
			sp := strings.Split(pair, "=")
			m[sp[0]] = sp[1]
		}
	}
	labels.LabelsMap = m
	labels.LabelsString = value
	return nil
}

// ConfigAsMetrics exposes major configuration params as prometheus metrics
func ConfigAsMetrics(r metrics.RegistererGatherer, cfg *BaseOperatorConf) {
	opts := getEnvOpts()
	mapper := func(v string) string {
		return opts.Environment[v]
	}
	params, err := env.GetFieldParamsWithOptions(cfg, opts)
	if err != nil {
		panic(fmt.Sprintf("BUG: failed to get global variables: %s", err))
	}
	ms := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "config parameter"}, []string{"name", "is_set", "value"})
	for _, p := range params {
		_, isSet := os.LookupEnv(p.Key)
		value := p.DefaultValue
		if v, ok := opts.Environment[p.Key]; ok {
			value = v
		} else if p.Expand {
			value = os.Expand(value, mapper)
		}
		ms.WithLabelValues(p.Key, strconv.FormatBool(isSet), value).Set(1)
	}
	r.MustRegister(ms)
}
