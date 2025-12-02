package config

import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
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
	prefixVar         = "VM_"
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

	opNamespace   []string
	initNamespace sync.Once
	// this values are used by env expand logic here:
	// https://github.com/caarlos0/env/blob/0669e014efa158a19b690c908bf39a3081a9fdc9/env.go#L607
	//
	// DO NOT FORGET TO MODIFY VERSIONS IN BaseOperatorConf
	defaultEnvs = map[string]string{
		"VM_METRICS_VERSION":  "v1.131.0",
		"VM_LOGS_VERSION":     "v1.36.1",
		"VM_ANOMALY_VERSION":  "v1.26.1",
		"VM_TRACES_VERSION":   "v0.5.0",
		"VM_OPERATOR_VERSION": getVersion("v0.65.0"),
	}
)

func getEnvOpts() env.Options {
	envOpts := env.Options{
		DefaultValueTagName:   "default",
		PrefixTagName:         "prefix",
		UseFieldNameByDefault: true,
		Prefix:                prefixVar,
		Environment:           env.ToMap(os.Environ()),
	}
	for n, ov := range defaultEnvs {
		if v, ok := envOpts.Environment[n]; !ok || v == "" {
			envOpts.Environment[n] = ov
		}
	}
	return envOpts
}

// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
// which specifies the Namespace to watch.
// An empty value means the operator is running with cluster scope.
var WatchNamespaceEnvVar = "WATCH_NAMESPACE"

// ApplicationDefaults is useful for generic default building
// uses the same memory as application default at config
type ApplicationDefaults struct {
	Image               string
	Version             string
	ConfigReloadImage   string
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
	ConfigReloaderCPU    string
	ConfigReloaderMemory string
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
	// this values are printed when -printDefaults is called
	//
	// DO NOT FORGET TO MODIFY VERSIONS IN defaultEnvs

	MetricsVersion string `default:"v1.131.0" env:"METRICS_VERSION"`
	LogsVersion    string `default:"v1.36.1" env:"LOGS_VERSION"`
	AnomalyVersion string `default:"v1.25.2" env:"ANOMALY_VERSION"`
	GatewayAPIEnabled bool   `default:"false" env:"GATEWAY_API_ENABLED"`

	// enables custom config reloader for vmauth and vmagent,
	// it should speed-up config reloading process.
	UseVMConfigReloader bool `default:"true" env:"USECUSTOMCONFIGRELOADER"`
	// container registry name prefix, e.g. docker.io
	ContainerRegistry     string `default:"" env:"CONTAINERREGISTRY"`
	VMConfigReloaderImage string `default:"victoriametrics/operator:config-reloader-${VM_OPERATOR_VERSION}" env:"CUSTOMCONFIGRELOADERIMAGE,expand"`
	PSPAutoCreateEnabled  bool   `default:"false" env:"PSPAUTOCREATEENABLED"`
	EnableTCP6            bool   `default:"false" env:"ENABLETCP6"`

	// defines global resource.limits.cpu for all config-reloader containers
	ConfigReloaderLimitCPU string `default:"unlimited"`
	// defines global resource.limits.memory for all config-reloader containers
	ConfigReloaderLimitMemory string `default:"unlimited"`
	// defines global resource.requests.cpu for all config-reloader containers
	ConfigReloaderRequestCPU string `default:""`
	// defines global resource.requests.memory for all config-reloader containers
	ConfigReloaderRequestMemory string `default:""`

	VLogsDefault struct {
		Image               string `default:"victoriametrics/victoria-logs"`
		Version             string `env:",expand" default:"${VM_LOGS_VERSION}"`
		ConfigReloadImage   string `env:"-"`
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
		ConfigReloaderCPU    string `env:"-"`
		ConfigReloaderMemory string `env:"-"`
	} `prefix:"VLOGSDEFAULT_"`

	VLAgentDefault struct {
		Image               string `default:"victoriametrics/vlagent"`
		Version             string `env:",expand" default:"${VM_LOGS_VERSION}"`
		ConfigReloadImage   string `env:"-"`
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
		ConfigReloaderCPU    string `env:"-"`
		ConfigReloaderMemory string `env:"-"`
	} `prefix:"VLAGENTDEFAULT_"`

	VLSingleDefault struct {
		Image               string `default:"victoriametrics/victoria-logs"`
		Version             string `env:",expand" default:"${VM_LOGS_VERSION}"`
		ConfigReloadImage   string `env:"-"`
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
		ConfigReloaderCPU    string `env:"-"`
		ConfigReloaderMemory string `env:"-"`
	} `prefix:"VLSINGLEDEFAULT_"`

	VTSingleDefault struct {
		Image               string `default:"victoriametrics/victoria-traces"`
		Version             string `env:",expand" default:"${VM_TRACES_VERSION}"`
		ConfigReloadImage   string `env:"-"`
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
		ConfigReloaderCPU    string `env:"-"`
		ConfigReloaderMemory string `env:"-"`
	} `prefix:"VTSINGLEDEFAULT_"`

	VMAlertDefault struct {
		Image               string `default:"victoriametrics/vmalert"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}"`
		ConfigReloadImage   string `default:"jimmidyson/configmap-reload:v0.3.0" env:"CONFIGRELOADIMAGE"`
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
		// Deprecated:: use VM_CONFIG_RELOADER_REQUEST_CPU instead
		ConfigReloaderCPU string `default:"10m" env:"CONFIGRELOADERCPU"`
		// Deprecated:: use VM_CONFIG_RELOADER_REQUEST_MEMORY instead
		ConfigReloaderMemory string `default:"25Mi" env:"CONFIGRELOADERMEMORY"`
	} `prefix:"VMALERTDEFAULT_"`

	VMServiceScrapeDefault struct {
		// Use endpointslices instead of endpoints as discovery role
		// for vmservicescrape when generate scrape config for vmagent.
		EnforceEndpointSlices bool `default:"false" env:"ENFORCEENDPOINTSLICES"`
	} `prefix:"VMSERVICESCRAPEDEFAULT_"`

	VMAgentDefault struct {
		Image               string `default:"victoriametrics/vmagent"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}"`
		ConfigReloadImage   string `default:"quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1" env:"CONFIGRELOADIMAGE"`
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
		// Deprecated:: use VM_CONFIG_RELOADER_REQUEST_CPU instead
		ConfigReloaderCPU string `default:"10m" env:"CONFIGRELOADERCPU"`
		// Deprecated:: use VM_CONFIG_RELOADER_REQUEST_MEMORY instead
		ConfigReloaderMemory string `default:"25Mi" env:"CONFIGRELOADERMEMORY"`
	} `prefix:"VMAGENTDEFAULT_"`

	VMAnomalyDefault struct {
		Image               string `default:"victoriametrics/vmanomaly"`
		Version             string `env:",expand" default:"${VM_ANOMALY_VERSION}"`
		ConfigReloadImage   string `default:"quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1" env:"CONFIGRELOADIMAGE"`
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
		// Deprecated: use VM_CONFIG_RELOADER_REQUEST_CPU instead
		ConfigReloaderCPU string `default:"10m" env:"CONFIGRELOADERCPU"`
		// Deprecated: use VM_CONFIG_RELOADER_REQUEST_MEMORY instead
		ConfigReloaderMemory string `default:"25Mi" env:"CONFIGRELOADERMEMORY"`
	} `prefix:"VMANOMALYDEFAULT_"`

	VMSingleDefault struct {
		Image               string `default:"victoriametrics/victoria-metrics"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}"`
		ConfigReloadImage   string `env:"-"`
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
		ConfigReloaderCPU    string `env:"-"`
		ConfigReloaderMemory string `env:"-"`
	} `prefix:"VMSINGLEDEFAULT_"`

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
	} `prefix:"VMCLUSTERDEFAULT_"`

	VMAlertManager struct {
		ConfigReloaderImage string `default:"jimmidyson/configmap-reload:v0.3.0" env:"CONFIGRELOADERIMAGE"`
		// Deprecated: use VM_CONFIG_RELOADER_REQUEST_CPU instead
		ConfigReloaderCPU string `default:"10m" env:"CONFIGRELOADERCPU"`
		// Deprecated: use VM_CONFIG_RELOADER_REQUEST_MEMORY instead
		ConfigReloaderMemory         string `default:"25Mi" env:"CONFIGRELOADERMEMORY"`
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
	} `prefix:"VMALERTMANAGER_"`

	DisableSelfServiceScrapeCreation bool `default:"false" env:"DISABLESELFSERVICESCRAPECREATION"`
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
	} `prefix:"VMBACKUP_"`
	VMAuthDefault struct {
		Image               string `default:"victoriametrics/vmauth"`
		Version             string `env:",expand" default:"${VM_METRICS_VERSION}"`
		ConfigReloadImage   string `default:"quay.io/prometheus-operator/prometheus-config-reloader:v0.82.1" env:"CONFIGRELOADIMAGE"`
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
		// Deprecated: use VM_CONFIG_RELOADER_REQUEST_CPU instead
		ConfigReloaderCPU string `default:"10m" env:"CONFIGRELOADERCPU"`
		// Deprecated: use VM_CONFIG_RELOADER_REQUEST_MEMORY instead
		ConfigReloaderMemory string `default:"25Mi" env:"CONFIGRELOADERMEMORY"`
	} `prefix:"VMAUTHDEFAULT_"`

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
	} `prefix:"VLCLUSTERDEFAULT_"`

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
	} `prefix:"VTCLUSTERDEFAULT_"`

	EnabledPrometheusConverter struct {
		PodMonitor         bool `default:"true" env:"PODMONITOR"`
		ServiceScrape      bool `default:"true" env:"SERVICESCRAPE"`
		PrometheusRule     bool `default:"true" env:"PROMETHEUSRULE"`
		Probe              bool `default:"true" env:"PROBE"`
		AlertmanagerConfig bool `default:"true" env:"ALERTMANAGERCONFIG"`
		ScrapeConfig       bool `default:"true" env:"SCRAPECONFIG"`
	} `prefix:"ENABLEDPROMETHEUSCONVERTER_"`
	FilterChildLabelPrefixes      []string `default:"" env:"FILTERCHILDLABELPREFIXES"`
	FilterChildAnnotationPrefixes []string `default:"" env:"FILTERCHILDANNOTATIONPREFIXES"`
	// adds compare-options and sync-options for prometheus objects converted by operator.
	// It helps to properly use converter with ArgoCD
	PrometheusConverterAddArgoCDIgnoreAnnotations bool `default:"false" env:"PROMETHEUSCONVERTERADDARGOCDIGNOREANNOTATIONS"`
	EnabledPrometheusConverterOwnerReferences     bool `default:"false" env:"ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES"`
	// allows filtering for converted labels, labels with matched prefix will be ignored
	FilterPrometheusConverterLabelPrefixes []string `default:"" env:"FILTERPROMETHEUSCONVERTERLABELPREFIXES"`
	// allows filtering for converted annotations, annotations with matched prefix will be ignored
	FilterPrometheusConverterAnnotationPrefixes []string `default:"" env:"FILTERPROMETHEUSCONVERTERANNOTATIONPREFIXES"`
	// Defines domain name suffix for in-cluster addresses
	// most known ClusterDomainName is .cluster.local
	ClusterDomainName string `default:"" env:"CLUSTERDOMAINNAME"`
	// Defines deadline for deployment/statefulset
	// to transit into ready state
	// to wait for transition to ready state
	AppReadyTimeout time.Duration `default:"80s" env:"APPREADYTIMEOUT"`
	// Defines single pod deadline
	// to wait for transition to ready state
	PodWaitReadyTimeout time.Duration `default:"80s" env:"PODWAITREADYTIMEOUT"`
	// Defines poll interval for pods ready check
	// at statefulset rollout update
	PodWaitReadyIntervalCheck time.Duration `default:"5s" env:"PODWAITREADYINTERVALCHECK"`
	// configures force resync interval for VMAgent, VMAlert, VMAlertmanager and VMAuth.
	ForceResyncInterval time.Duration `default:"60s" env:"FORCERESYNCINTERVAL"`
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
	EnableStrictSecurity bool `default:"false" env:"ENABLESTRICTSECURITY"`
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
		if err := c.Validate(); err != nil {
			panic(err)
		}
		opConf = &c
	})
	return opConf
}

var validNamespaceRegex = regexp.MustCompile(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`)

func getWatchNamespaces() ([]string, error) {
	wns, _ := os.LookupEnv(WatchNamespaceEnvVar)
	if len(wns) > 0 {
		nss := strings.Split(wns, ",")
		// validate namespace with regexp
		for _, ns := range nss {
			if !validNamespaceRegex.MatchString(ns) {
				return nil, fmt.Errorf("incorrect namespace name=%q for env var=%q with value: %q must match regex: %q", ns, WatchNamespaceEnvVar, wns, validNamespaceRegex.String())
			}
		}

		return nss, nil
	}
	return nil, nil
}

// MustGetWatchNamespaces returns a list of namespaces to be watched by operator
// Operator don't perform any cluster wide API calls if namespaces not empty
// in case of empty list it performs only clusterwide api calls
func MustGetWatchNamespaces() []string {
	initNamespace.Do(func() {
		nss, err := getWatchNamespaces()
		if err != nil {
			panic(err)
		}
		opNamespace = nss
	})

	return opNamespace
}

// IsClusterWideAccessAllowed checks if cluster wide access for components is needed
func IsClusterWideAccessAllowed() bool {
	return len(MustGetWatchNamespaces()) == 0
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
	metric := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "config parameter"}, []string{"name", "is_set", "value"})

	boolTagAsMetricWithPrefix := func(namePrefix, fieldName string, t reflect.Type, value bool) {
		v, ok := t.FieldByName(fieldName)
		if !ok {
			panic(fmt.Sprintf("BUG: fieldName: %q is missing at : %s", fieldName, t.Name()))
		}
		dvS := v.Tag.Get("default")
		dv, err := strconv.ParseBool(dvS)
		if err != nil {
			panic(fmt.Sprintf("BUG: unexpected boolean value: %q for fieldName: %q", dvS, fieldName))
		}
		name := v.Tag.Get("env")
		if len(name) == 0 {
			panic(fmt.Sprintf("BUG: env tag for fieldName: %q cannot be empty", fieldName))
		}
		metric.WithLabelValues(prefixVar+namePrefix+name, strconv.FormatBool(dv != value), strconv.FormatBool(value)).Set(1)
	}
	boolTagAsMetric := func(fieldName string, t reflect.Type, value bool) {
		boolTagAsMetricWithPrefix("", fieldName, t, value)
	}

	strTagAsMetric := func(fieldName string, t reflect.Type, value string) {
		v, ok := t.FieldByName(fieldName)
		if !ok {
			panic(fmt.Sprintf("BUG: fieldName: %q is missing at : %s", fieldName, t.Name()))
		}
		dvS := v.Tag.Get("default")
		name := v.Tag.Get("env")
		if len(name) == 0 {
			panic(fmt.Sprintf("BUG: env tag for fieldName: %q cannot be empty", fieldName))
		}
		isSet := false
		if dvS != value && value == "" {
			value = dvS
			isSet = true
		}
		metric.WithLabelValues(prefixVar+name, strconv.FormatBool(isSet), value).Set(1)
	}

	durationTagAsMetric := func(fieldName string, t reflect.Type, value time.Duration) {
		v, ok := t.FieldByName(fieldName)
		if !ok {
			panic(fmt.Sprintf("BUG: fieldName: %q is missing at : %s", fieldName, t.Name()))
		}
		dvS := v.Tag.Get("default")
		dv, err := time.ParseDuration(dvS)
		if err != nil {
			panic(fmt.Sprintf("BUG: unexpected duration value: %q for fieldName: %q", dvS, fieldName))
		}
		name := v.Tag.Get("env")
		if len(name) == 0 {
			panic(fmt.Sprintf("BUG: env tag for fieldName: %q cannot be empty", fieldName))
		}
		isSet := false
		if dv != value && value == 0 {
			value = dv
			isSet = true
		}
		metric.WithLabelValues(prefixVar+name, strconv.FormatBool(isSet), value.String()).Set(1)
	}

	cfgRootElems := reflect.TypeOf(cfg).Elem()
	strTagAsMetric("ContainerRegistry", cfgRootElems, cfg.ContainerRegistry)
	strTagAsMetric("ClusterDomainName", cfgRootElems, cfg.ClusterDomainName)

	boolTagAsMetric("EnableStrictSecurity", cfgRootElems, cfg.EnableStrictSecurity)
	boolTagAsMetric("UseVMConfigReloader", cfgRootElems, cfg.UseVMConfigReloader)
	boolTagAsMetric("EnableTCP6", cfgRootElems, cfg.EnableTCP6)

	boolTagAsMetric("EnabledPrometheusConverterOwnerReferences", cfgRootElems, cfg.EnabledPrometheusConverterOwnerReferences)
	cfgPromElems := reflect.TypeOf(&cfg.EnabledPrometheusConverter).Elem()
	boolTagAsMetricWithPrefix("ENABLEDPROMETHEUSCONVERTER_", "PodMonitor", cfgPromElems, cfg.EnabledPrometheusConverter.PodMonitor)
	boolTagAsMetricWithPrefix("ENABLEDPROMETHEUSCONVERTER_", "ServiceScrape", cfgPromElems, cfg.EnabledPrometheusConverter.ServiceScrape)
	boolTagAsMetricWithPrefix("ENABLEDPROMETHEUSCONVERTER_", "ScrapeConfig", cfgPromElems, cfg.EnabledPrometheusConverter.ScrapeConfig)
	boolTagAsMetricWithPrefix("ENABLEDPROMETHEUSCONVERTER_", "Probe", cfgPromElems, cfg.EnabledPrometheusConverter.Probe)
	boolTagAsMetricWithPrefix("ENABLEDPROMETHEUSCONVERTER_", "PrometheusRule", cfgPromElems, cfg.EnabledPrometheusConverter.PrometheusRule)
	boolTagAsMetricWithPrefix("ENABLEDPROMETHEUSCONVERTER_", "AlertmanagerConfig", cfgPromElems, cfg.EnabledPrometheusConverter.AlertmanagerConfig)

	durationTagAsMetric("AppReadyTimeout", cfgRootElems, cfg.AppReadyTimeout)
	durationTagAsMetric("ForceResyncInterval", cfgRootElems, cfg.ForceResyncInterval)
	durationTagAsMetric("PodWaitReadyIntervalCheck", cfgRootElems, cfg.PodWaitReadyIntervalCheck)
	durationTagAsMetric("PodWaitReadyTimeout", cfgRootElems, cfg.PodWaitReadyTimeout)

	strTagAsMetric("AnomalyVersion", cfgRootElems, cfg.AnomalyVersion)
	strTagAsMetric("LogsVersion", cfgRootElems, cfg.LogsVersion)
	strTagAsMetric("MetricsVersion", cfgRootElems, cfg.MetricsVersion)
	strTagAsMetric("VMConfigReloaderImage", cfgRootElems, cfg.VMConfigReloaderImage)

	r.MustRegister(metric)
}
