package config

import (
	"fmt"
	"maps"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo"
	"github.com/caarlos0/env/v11"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	UnlimitedQuantity = "unlimited"
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
		"VM_METRICS_VERSION":  "v1.147.0",
		"VM_LOGS_VERSION":     "v1.51.0",
		"VM_ANOMALY_VERSION":  "v1.29.7",
		"VM_TRACES_VERSION":   "v0.9.4",
		"VM_OPERATOR_VERSION": getVersion("v0.72.0"),
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

// Resource is useful for generic resource building
// uses the same memory layout as resources at config
type Resource struct {
	Limit struct {
		Mem              string
		Cpu              string
		EphemeralStorage string
	}
	Request struct {
		Mem              string
		Cpu              string
		EphemeralStorage string
	}
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
			Mem              string
			Cpu              string
			EphemeralStorage string
		}
		Request struct {
			Mem              string
			Cpu              string
			EphemeralStorage string
		}
	}
	TerminationGracePeriodSeconds int64
}

//genvars:true
type BaseOperatorConf struct {
	// Defines default image version for VictoriaMetrics components:
	// VMSingle, VMCluster (vmselect/vminsert/vmstorage), VMAgent, VMAlert, VMAuth, VMBackup.
	// Used as the image tag when no explicit version is set in the CR spec.
	MetricsVersion string `default:"${VM_METRICS_VERSION}" env:"VM_METRICS_VERSION,expand"`
	// Defines default image version for VictoriaLogs components:
	// VLogs, VLAgent, VLSingle, VLCluster (vlselect/vlinsert/vlstorage).
	// Used as the image tag when no explicit version is set in the CR spec.
	LogsVersion string `default:"${VM_LOGS_VERSION}" env:"VM_LOGS_VERSION,expand"`
	// Defines default image version for VMAnomaly.
	// Used as the image tag when no explicit version is set in the CR spec.
	AnomalyVersion string `default:"${VM_ANOMALY_VERSION}" env:"VM_ANOMALY_VERSION,expand"`
	// Defines default image version for VictoriaTraces components:
	// VTSingle, VTCluster (vtselect/vtinsert/vtstorage).
	// Used as the image tag when no explicit version is set in the CR spec.
	TracesVersion string `default:"${VM_TRACES_VERSION}" env:"VM_TRACES_VERSION,expand"`
	// Defines the operator's own version. Used for config-reloader image tag interpolation.
	OperatorVersion string `default:"${VM_OPERATOR_VERSION}" env:"VM_OPERATOR_VERSION,expand"`
	// Enables support for Kubernetes Gateway API.
	// When enabled, operator manages HTTPRoute resources for VMAuth ingress configuration.
	GatewayAPIEnabled bool `default:"false" env:"VM_GATEWAY_API_ENABLED"`
	// Enables support for VerticalPodAutoscaler API.
	// When enabled, operator can create and manage VPA objects for VM components.
	VPAAPIEnabled bool `default:"false" env:"VM_VPA_API_ENABLED"`

	// Defines a list of namespaces to be watched by operator.
	// Operator don't perform any cluster wide API calls if namespaces not empty.
	// In case of empty list it performs only clusterwide api calls.
	WatchNamespaces []string `default:"" env:"WATCH_NAMESPACE"`

	// Container registry name prefix prepended to all component images, e.g. docker.io.
	ContainerRegistry string `default:"" env:"VM_CONTAINERREGISTRY"`
	// Deprecated: use VM_CONFIG_RELOADER_IMAGE instead
	CustomConfigReloaderImage string `env:"VM_CUSTOMCONFIGRELOADERIMAGE"`
	// Deprecated: PodSecurityPolicy was removed in Kubernetes 1.25. This flag has no effect.
	PSPAutoCreateEnabled bool `default:"false" env:"VM_PSPAUTOCREATEENABLED"`
	// Enables IPv6 for TCP listeners of all VM components.
	EnableTCP6 bool `default:"false" env:"VM_ENABLETCP6"`
	// Overrides default loopback interface that will be used for all VM components
	Loopback string `env:"VM_LOOPBACK"`
	// Restores old VMBackup/VMRestore port names to avoid restarts during upgrades
	UseOldBackupRestorePortNames bool `default:"false" env:"VM_USE_OLD_BACKUP_RESTORE_PORT_NAMES"`
	// Controls whether a default preStop lifecycle hook is injected into applicable pods.
	// The hook sleeps for 15 seconds on termination, giving load balancers time to
	// remove the pod from rotation before the process shuts down.
	// Set to false before upgrading the operator to prevent rolling updates on existing resources.
	EnableDefaultPreStopHook bool `default:"true" env:"VM_ENABLE_DEFAULT_PRESTOP_HOOK"`
	// ConfigDataBudgetBytes sets the maximum number of bytes allowed for compressed data
	// in a generated Kubernetes Secret or ConfigMap (e.g. scrape-config overflow buckets,
	// vmalert rule ConfigMaps). When zero the budget is computed automatically from the
	// Kubernetes object size limit minus the JSON-encoded metadata overhead. Set a lower
	// value when a policy engine (e.g. Kyverno) injects additional labels or annotations
	// after reconcile, causing the auto-computed budget to be too large.
	ConfigDataBudgetBytes int `default:"524288" env:"VM_CONFIG_DATA_BUDGET_BYTES"`

	// defines global config reloader parameters
	ConfigReloader struct {
		// default image for all config-reloader containers
		Image    string `default:"victoriametrics/operator:config-reloader-${VM_OPERATOR_VERSION}" env:",expand"`
		Resource struct {
			Limit struct {
				// defines global resource.limits.memory for all config-reloader containers
				Mem string `default:"unlimited" env:"MEMORY"`
				// defines global resource.limits.cpu for all config-reloader containers
				Cpu string `default:"unlimited"`
				// defines global resource.limits.ephemeral-storage for all config-reloader containers
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				// defines global resource.requests.memory for all config-reloader containers
				Mem string `default:"25Mi" env:"MEMORY"`
				// defines global resource.requests.cpu for all config-reloader containers
				Cpu string `default:"10m"`
				// defines global resource.requests.ephemeral-storage for all config-reloader containers
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		}
	} `prefix:"VM_CONFIG_RELOADER_"`

	// VLogs defines default settings for VLogs (single-node VictoriaLogs) deployments.
	VLogs struct {
		// Default container image for VLogs.
		Image string `default:"victoriametrics/victoria-logs"`
		// Default image version. Inherits VM_LOGS_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_LOGS_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"9428"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"1500Mi"`
				Cpu              string `default:"1200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"150m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VLOGSDEFAULT_"`

	// VLAgent defines default settings for VLAgent deployments.
	VLAgent struct {
		// Default container image for VLAgent.
		Image string `default:"victoriametrics/vlagent"`
		// Default image version. Inherits VM_LOGS_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_LOGS_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"9429"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"200Mi"`
				Cpu              string `default:"50m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VLAGENTDEFAULT_"`

	// VLSingle defines default settings for VLSingle (single-node VictoriaLogs) deployments.
	VLSingle struct {
		// Default container image for VLSingle.
		Image string `default:"victoriametrics/victoria-logs"`
		// Default image version. Inherits VM_LOGS_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_LOGS_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"9428"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"1500Mi"`
				Cpu              string `default:"1200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"150m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VLSINGLEDEFAULT_"`

	// VTSingle defines default settings for VTSingle (single-node VictoriaTraces) deployments.
	VTSingle struct {
		// Default container image for VTSingle.
		Image string `default:"victoriametrics/victoria-traces"`
		// Default image version. Inherits VM_TRACES_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_TRACES_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"10428"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"1500Mi"`
				Cpu              string `default:"1200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"150m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VTSINGLEDEFAULT_"`

	// VMAlert defines default settings for VMAlert deployments.
	VMAlert struct {
		// Default container image for VMAlert.
		Image string `default:"victoriametrics/vmalert"`
		// Default image version. Inherits VM_METRICS_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_METRICS_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"8080"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"200Mi"`
				Cpu              string `default:"50m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VMALERTDEFAULT_"`

	VMServiceScrape struct {
		// Use endpointslices instead of endpoints as discovery role
		// for vmservicescrape when generate scrape config for vmagent.
		EnforceEndpointSlices bool `default:"false" env:"ENFORCEENDPOINTSLICES"`
	} `prefix:"VM_VMSERVICESCRAPEDEFAULT_"`

	// VMAgent defines default settings for VMAgent deployments.
	VMAgent struct {
		// Default container image for VMAgent.
		Image string `default:"victoriametrics/vmagent"`
		// Default image version. Inherits VM_METRICS_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_METRICS_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"8429"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"200Mi"`
				Cpu              string `default:"50m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VMAGENTDEFAULT_"`

	// VMAnomaly defines default settings for VMAnomaly deployments.
	VMAnomaly struct {
		// Default container image for VMAnomaly.
		Image string `default:"victoriametrics/vmanomaly"`
		// Default image version. Inherits VM_ANOMALY_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_ANOMALY_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"8490"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"200Mi"`
				Cpu              string `default:"50m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VMANOMALYDEFAULT_"`

	// VMSingle defines default settings for VMSingle (single-node VictoriaMetrics) deployments.
	VMSingle struct {
		// Default container image for VMSingle.
		Image string `default:"victoriametrics/victoria-metrics"`
		// Default image version. Inherits VM_METRICS_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_METRICS_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"8429"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"1500Mi"`
				Cpu              string `default:"1200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"150m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VMSINGLEDEFAULT_"`

	// VMCluster defines default settings for VMCluster deployments.
	VMCluster struct {
		// Whether to apply default resource requests and limits to all VMCluster components.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		// Select defines defaults for the vmselect component.
		Select struct {
			// Default container image for vmselect.
			Image string `default:"victoriametrics/vmselect"`
			// Default image version. Inherits VM_METRICS_VERSION with -cluster suffix if not set explicitly.
			Version string `env:",expand" default:"${VM_METRICS_VERSION}-cluster"`
			// Default HTTP listen port.
			Port string `default:"8481"`
			// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
			UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VMCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
			Resource            struct {
				Limit struct {
					Mem              string `default:"1000Mi"`
					Cpu              string `default:"500m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem              string `default:"500Mi"`
					Cpu              string `default:"100m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
			TerminationGracePeriodSeconds int64 `default:"30"`
		} `prefix:"VMSELECTDEFAULT_"`
		// Storage defines defaults for the vmstorage component.
		Storage struct {
			// Default port for vminsert → vmstorage communication.
			VMInsertPort string `default:"8400" env:"VMINSERTPORT"`
			// Default port for vmselect → vmstorage communication.
			VMSelectPort string `default:"8401" env:"VMSELECTPORT"`
			Common       struct {
				// Default container image for vmstorage.
				Image string `default:"victoriametrics/vmstorage"`
				// Default image version. Inherits VM_METRICS_VERSION with -cluster suffix if not set explicitly.
				Version string `env:",expand" default:"${VM_METRICS_VERSION}-cluster"`
				// Default HTTP listen port.
				Port string `default:"8482"`
				// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
				UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VMCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
				Resource            struct {
					Limit struct {
						Mem              string `default:"1500Mi"`
						Cpu              string `default:"1000m"`
						EphemeralStorage string `default:"unlimited"`
					} `prefix:"LIMIT_"`
					Request struct {
						Mem              string `default:"500Mi"`
						Cpu              string `default:"250m"`
						EphemeralStorage string `default:"unlimited"`
					} `prefix:"REQUEST_"`
				} `prefix:"RESOURCE_"`
				TerminationGracePeriodSeconds int64 `default:"30"`
			}
		} `prefix:"VMSTORAGEDEFAULT_"`
		// Insert defines defaults for the vminsert component.
		Insert struct {
			// Default container image for vminsert.
			Image string `default:"victoriametrics/vminsert"`
			// Default image version. Inherits VM_METRICS_VERSION with -cluster suffix if not set explicitly.
			Version string `env:",expand" default:"${VM_METRICS_VERSION}-cluster"`
			// Default HTTP listen port.
			Port string `default:"8480"`
			// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
			UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VMCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
			Resource            struct {
				Limit struct {
					Mem              string `default:"500Mi"`
					Cpu              string `default:"500m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem              string `default:"200Mi"`
					Cpu              string `default:"150m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
			TerminationGracePeriodSeconds int64 `default:"30"`
		} `prefix:"VMINSERTDEFAULT_"`
	} `prefix:"VM_VMCLUSTERDEFAULT_"`

	// VMAlertmanager defines default settings for VMAlertmanager deployments.
	VMAlertmanager struct {
		// Default container image for Alertmanager.
		Image string `default:"prom/alertmanager" env:"ALERTMANAGERDEFAULTBASEIMAGE"`
		// Default Alertmanager version.
		Version string `default:"v0.31.0" env:"ALERTMANAGERVERSION"`
		// Default HTTP listen port.
		Port string `default:"9093"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"256Mi"`
				Cpu              string `default:"100m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"56Mi"`
				Cpu              string `default:"30m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"120"`
	} `prefix:"VM_VMALERTMANAGER_"`

	// Disables automatic creation of VMServiceScrape objects for operator-managed components.
	// By default the operator creates a VMServiceScrape per component to enable self-monitoring.
	DisableSelfServiceScrapeCreation bool `default:"false" env:"VM_DISABLESELFSERVICESCRAPECREATION"`
	// VMBackup defines default settings for the VMBackupManager sidecar container.
	VMBackup struct {
		// Default container image for VMBackupManager.
		Image string `default:"victoriametrics/vmbackupmanager"`
		// Default image version. Inherits VM_METRICS_VERSION with -enterprise suffix if not set explicitly.
		Version string `env:",expand" default:"${VM_METRICS_VERSION}-enterprise"`
		// Default HTTP listen port.
		Port string `default:"8300"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"500Mi"`
				Cpu              string `default:"500m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"200Mi"`
				Cpu              string `default:"150m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VMBACKUP_"`
	// VMAuth defines default settings for VMAuth deployments.
	VMAuth struct {
		// Default container image for VMAuth.
		Image string `default:"victoriametrics/vmauth"`
		// Default image version. Inherits VM_METRICS_VERSION if not set explicitly.
		Version string `env:",expand" default:"${VM_METRICS_VERSION}"`
		// Default HTTP listen port.
		Port string `default:"8427"`
		// Whether to apply default resource requests and limits.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		Resource            struct {
			Limit struct {
				Mem              string `default:"300Mi"`
				Cpu              string `default:"200m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"LIMIT_"`
			Request struct {
				Mem              string `default:"100Mi"`
				Cpu              string `default:"50m"`
				EphemeralStorage string `default:"unlimited"`
			} `prefix:"REQUEST_"`
		} `prefix:"RESOURCE_"`
		TerminationGracePeriodSeconds int64 `default:"30"`
	} `prefix:"VM_VMAUTHDEFAULT_"`

	// VLCluster defines default settings for VLCluster deployments.
	VLCluster struct {
		// Whether to apply default resource requests and limits to all VLCluster components.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		// Select defines defaults for the vlselect component.
		Select struct {
			// Default container image for vlselect.
			Image string `default:"victoriametrics/victoria-logs"`
			// Default image version. Inherits VM_LOGS_VERSION if not set explicitly.
			Version string `env:",expand" default:"${VM_LOGS_VERSION}"`
			// Default HTTP listen port.
			Port string `default:"9471"`
			// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
			UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VLCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
			Resource            struct {
				Limit struct {
					Mem              string `default:"1024Mi"`
					Cpu              string `default:"1000m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem              string `default:"256Mi"`
					Cpu              string `default:"100m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
			TerminationGracePeriodSeconds int64 `default:"30"`
		} `prefix:"VLSELECTDEFAULT_"`
		// Storage defines defaults for the vlstorage component.
		Storage struct {
			// Default container image for vlstorage.
			Image string `default:"victoriametrics/victoria-logs"`
			// Default image version. Inherits VM_LOGS_VERSION if not set explicitly.
			Version string `env:",expand" default:"${VM_LOGS_VERSION}"`
			// Default HTTP listen port.
			Port string `default:"9491"`
			// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
			UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VLCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
			Resource            struct {
				Limit struct {
					Mem              string `default:"2048Mi"`
					Cpu              string `default:"1000m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem              string `default:"512Mi"`
					Cpu              string `default:"200m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
			TerminationGracePeriodSeconds int64 `default:"30"`
		} `prefix:"VLSTORAGEDEFAULT_"`
		// Insert defines defaults for the vlinsert component.
		Insert struct {
			// Default container image for vlinsert.
			Image string `default:"victoriametrics/victoria-logs"`
			// Default image version. Inherits VM_LOGS_VERSION if not set explicitly.
			Version string `env:",expand" default:"${VM_LOGS_VERSION}"`
			// Default HTTP listen port.
			Port string `default:"9481"`
			// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
			UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VLCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
			Resource            struct {
				Limit struct {
					Mem              string `default:"1024Mi"`
					Cpu              string `default:"1000m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem              string `default:"256Mi"`
					Cpu              string `default:"100m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
			TerminationGracePeriodSeconds int64 `default:"30"`
		} `prefix:"VLINSERTDEFAULT_"`
	} `prefix:"VM_VLCLUSTERDEFAULT_"`

	// VTCluster defines default settings for VTCluster deployments.
	VTCluster struct {
		// Whether to apply default resource requests and limits to all VTCluster components.
		UseDefaultResources bool `default:"true" env:"USEDEFAULTRESOURCES"`
		// Select defines defaults for the vtselect component.
		Select struct {
			// Default container image for vtselect.
			Image string `default:"victoriametrics/victoria-traces"`
			// Default image version. Inherits VM_TRACES_VERSION if not set explicitly.
			Version string `env:",expand" default:"${VM_TRACES_VERSION}"`
			// Default HTTP listen port.
			Port string `default:"10471"`
			// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
			UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VTCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
			Resource            struct {
				Limit struct {
					Mem              string `default:"1024Mi"`
					Cpu              string `default:"1000m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem              string `default:"256Mi"`
					Cpu              string `default:"100m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
			TerminationGracePeriodSeconds int64 `default:"30"`
		} `prefix:"SELECT_"`
		// Storage defines defaults for the vtstorage component.
		Storage struct {
			// Default container image for vtstorage.
			Image string `default:"victoriametrics/victoria-traces"`
			// Default image version. Inherits VM_TRACES_VERSION if not set explicitly.
			Version string `env:",expand" default:"${VM_TRACES_VERSION}"`
			// Default HTTP listen port.
			Port string `default:"10491"`
			// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
			UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VTCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
			Resource            struct {
				Limit struct {
					Mem              string `default:"2048Mi"`
					Cpu              string `default:"1000m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem              string `default:"512Mi"`
					Cpu              string `default:"200m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
			TerminationGracePeriodSeconds int64 `default:"30"`
		} `prefix:"STORAGE_"`
		// Insert defines defaults for the vtinsert component.
		Insert struct {
			// Default container image for vtinsert.
			Image string `default:"victoriametrics/victoria-traces"`
			// Default image version. Inherits VM_TRACES_VERSION if not set explicitly.
			Version string `env:",expand" default:"${VM_TRACES_VERSION}"`
			// Default HTTP listen port.
			Port string `default:"10481"`
			// Whether to apply default resource requests and limits. Inherits cluster-level setting if not set.
			UseDefaultResources bool `env:"USEDEFAULTRESOURCES,expand" default:"${VM_VTCLUSTERDEFAULT_USEDEFAULTRESOURCES}"`
			Resource            struct {
				Limit struct {
					Mem              string `default:"1024Mi"`
					Cpu              string `default:"1000m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"LIMIT_"`
				Request struct {
					Mem              string `default:"256Mi"`
					Cpu              string `default:"100m"`
					EphemeralStorage string `default:"unlimited"`
				} `prefix:"REQUEST_"`
			} `prefix:"RESOURCE_"`
			TerminationGracePeriodSeconds int64 `default:"30"`
		} `prefix:"INSERT_"`
	} `prefix:"VM_VTCLUSTERDEFAULT_"`

	EnabledPrometheusConverter struct {
		// Deprecated: use command-line flag with value -controller.disableReconcileFor=PodMonitor instead
		PodMonitor bool `default:"true" env:"PODMONITOR"`
		// Deprecated: use command-line flag with value -controller.disableReconcileFor=ServiceMonitor instead
		ServiceMonitor bool `default:"true" env:"SERVICESCRAPE"`
		// Deprecated: use command-line flag with value -controller.disableReconcileFor=PrometheusRule instead
		PrometheusRule bool `default:"true" env:"PROMETHEUSRULE"`
		// Deprecated: use command-line flag with value -controller.disableReconcileFor=Probe instead
		Probe bool `default:"true" env:"PROBE"`
		// Deprecated: use command-line flag with value -controller.disableReconcileFor=AlertmanagerConfig instead
		AlertmanagerConfig bool `default:"true" env:"ALERTMANAGERCONFIG"`
		// Deprecated: use command-line flag with value -controller.disableReconcileFor=ScrapeConfig instead
		ScrapeConfig bool `default:"true" env:"SCRAPECONFIG"`
	} `prefix:"VM_ENABLEDPROMETHEUSCONVERTER_"`
	// adds compare-options and sync-options for prometheus objects converted by operator.
	// It helps to properly use converter with ArgoCD
	PrometheusConverterAddArgoCDIgnoreAnnotations bool `default:"false" env:"VM_PROMETHEUSCONVERTERADDARGOCDIGNOREANNOTATIONS"`
	// Adds owner references to Kubernetes objects converted from Prometheus CRDs.
	// When enabled, converted objects are garbage-collected when the source Prometheus CRD is deleted.
	EnabledPrometheusConverterOwnerReferences bool `default:"false" env:"VM_ENABLEDPROMETHEUSCONVERTEROWNERREFERENCES"`
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
	AppWaitReadyTimeout time.Duration `default:"80s" env:"VM_APPREADYTIMEOUT"`
	// Defines poll interval for pods ready check
	// at statefulset rollout update
	PodWaitReadyInterval time.Duration `default:"5s" env:"VM_PODWAITREADYINTERVALCHECK"`
	// Defines single pod deadline
	// to wait for transition to ready state
	PodWaitReadyTimeout time.Duration `default:"80s" env:"VM_PODWAITREADYTIMEOUT"`
	// Defines poll interval for PVC ready check
	PVCWaitReadyInterval time.Duration `default:"5s" env:"VM_PVC_WAIT_READY_INTERVAL"`
	// Defines poll timeout for PVC ready check
	PVCWaitReadyTimeout time.Duration `default:"80s" env:"VM_PVC_WAIT_READY_TIMEOUT"`
	// Defines poll interval for VM CRs
	VMWaitReadyInterval time.Duration `default:"5s" env:"VM_WAIT_READY_INTERVAL"`
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

	// CommonLabels are added to every Kubernetes resource created by the operator.
	// They cannot override labels already set by the operator or via spec.managedMetadata.
	// Format: key=value,key2=value2
	CommonLabels map[string]string `default:"" env:"VM_COMMON_LABELS" envSeparator:"," envKeyValSeparator:"="`
	// CommonAnnotations are added to every Kubernetes resource created by the operator.
	// They cannot override annotations already set by the operator or via spec.managedMetadata.
	// Format: key=value,key2=value2
	CommonAnnotations map[string]string `default:"" env:"VM_COMMON_ANNOTATIONS" envSeparator:"," envKeyValSeparator:"="`
}

// ResyncAfterDuration returns requeue duration for object period reconcile
// adds 10% jitter
func (boc *BaseOperatorConf) ResyncAfterDuration() time.Duration {
	if boc.ForceResyncInterval == 0 {
		return 0
	}
	d := boc.ForceResyncInterval
	dv := min(d/10, 10*time.Second)

	p := float64(rand.Int31()) / (1 << 32)

	return boc.ForceResyncInterval + time.Duration(p*float64(dv))
}

// Validate - validates config on best effort.
func (boc BaseOperatorConf) validate() error {
	for _, ns := range boc.WatchNamespaces {
		if msgs := validation.IsDNS1123Label(ns); len(msgs) > 0 {
			return fmt.Errorf("namespace=%q is not a valid DNS label: %s", ns, msgs[0])
		}
	}
	validateResource := func(name string, res Resource) error {
		if res.Request.Mem != UnlimitedQuantity {
			if _, err := resource.ParseQuantity(res.Request.Mem); err != nil {
				return fmt.Errorf("cannot parse resource request memory for %q: %w", name, err)
			}
		}
		if res.Request.Cpu != UnlimitedQuantity {
			if _, err := resource.ParseQuantity(res.Request.Cpu); err != nil {
				return fmt.Errorf("cannot parse resource request cpu for %q: %w", name, err)
			}
		}
		if res.Request.EphemeralStorage != UnlimitedQuantity {
			if _, err := resource.ParseQuantity(res.Request.EphemeralStorage); err != nil {
				return fmt.Errorf("cannot parse resource request ephemeral storage for %q: %w", name, err)
			}
		}
		if res.Limit.Mem != UnlimitedQuantity {
			if _, err := resource.ParseQuantity(res.Limit.Mem); err != nil {
				return fmt.Errorf("cannot parse resource limit memory for %q: %w", name, err)
			}
		}
		if res.Limit.Cpu != UnlimitedQuantity {
			if _, err := resource.ParseQuantity(res.Limit.Cpu); err != nil {
				return fmt.Errorf("cannot parse resource limit cpu for %q: %w", name, err)
			}
		}
		if res.Limit.EphemeralStorage != UnlimitedQuantity {
			if _, err := resource.ParseQuantity(res.Limit.EphemeralStorage); err != nil {
				return fmt.Errorf("cannot parse resource limit ephemeral storage for %q: %w", name, err)
			}
		}
		return nil
	}
	if err := validateResource("config-reloader", Resource(boc.ConfigReloader.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmagent", Resource(boc.VMAgent.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmalert", Resource(boc.VMAlert.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmalertmanager", Resource(boc.VMAlertmanager.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmselect", Resource(boc.VMCluster.Select.Resource)); err != nil {
		return err
	}
	if err := validateResource("vminsert", Resource(boc.VMCluster.Insert.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmstorage", Resource(boc.VMCluster.Storage.Common.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmsingle", Resource(boc.VMSingle.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmbackup", Resource(boc.VMBackup.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlogs", Resource(boc.VLogs.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlagent", Resource(boc.VLAgent.Resource)); err != nil {
		return err
	}
	if err := validateResource("vmanomaly", Resource(boc.VMAnomaly.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlsingle", Resource(boc.VLSingle.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlselect", Resource(boc.VLCluster.Select.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlinsert", Resource(boc.VLCluster.Insert.Resource)); err != nil {
		return err
	}
	if err := validateResource("vlstorage", Resource(boc.VLCluster.Storage.Resource)); err != nil {
		return err
	}
	if err := validateResource("vtsingle", Resource(boc.VTSingle.Resource)); err != nil {
		return err
	}
	if err := validateResource("vtselect", Resource(boc.VTCluster.Select.Resource)); err != nil {
		return err
	}
	if err := validateResource("vtinsert", Resource(boc.VTCluster.Insert.Resource)); err != nil {
		return err
	}
	if err := validateResource("vtstorage", Resource(boc.VTCluster.Storage.Resource)); err != nil {
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
			c.ConfigReloader.Image = c.CustomConfigReloaderImage
		}
		if err := c.validate(); err != nil {
			panic(err)
		}
		opConf = &c
	})
	return opConf
}

// UseOldBackupRestorePortNames
func UseOldBackupRestorePortNames() bool {
	cfg := MustGetBaseConfig()
	return cfg.UseOldBackupRestorePortNames
}

// GetLocalhost returns localhost value depending on global configuration
func GetLocalhost() string {
	cfg := MustGetBaseConfig()
	if len(cfg.Loopback) > 0 {
		return cfg.Loopback
	}
	if cfg.EnableTCP6 {
		return "localhost"
	}
	return "127.0.0.1"
}

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

	maps.Copy(mergedLabels, otherLabels)

	maps.Copy(mergedLabels, labels.LabelsMap)
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
	rawEnvVars := make(map[string]string)
	var mapper func(v string) string
	mapper = func(v string) string {
		val := rawEnvVars[v]
		if val == "" {
			val = opts.Environment[v]
		}
		return os.Expand(val, mapper)
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
		rawEnvVars[p.Key] = value
		ms.WithLabelValues(p.Key, strconv.FormatBool(isSet), value).Set(1)
	}
	r.MustRegister(ms)
}
