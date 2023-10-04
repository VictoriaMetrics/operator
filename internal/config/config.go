package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/kelseyhightower/envconfig"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	opConf   *BaseOperatorConf
	initConf sync.Once

	opNamespace   string
	initNamespace sync.Once
)

const (
	prefixVar         = "VM"
	UnLimitedResource = "unlimited"
)

// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
// which specifies the Namespace to watch.
// An empty value means the operator is running with cluster scope.
var WatchNamespaceEnvVar = "WATCH_NAMESPACE"

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
	// enables custom config reloader for vmauth and vmagent,
	// it should speed-up config reloading process.
	UseCustomConfigReloader bool `default:"false"`
	// container registry name prefix, e.g. docker.io
	ContainerRegistry         string `default:""`
	CustomConfigReloaderImage string `default:"victoriametrics/operator:config-reloader-v0.38.0"`
	PSPAutoCreateEnabled      bool   `default:"false"`
	VMAlertDefault            struct {
		Image               string `default:"victoriametrics/vmalert"`
		Version             string `default:"v1.94.0"`
		Port                string `default:"8080"`
		UseDefaultResources bool   `default:"true"`
		Resource            struct {
			Limit struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"200m"`
			}
			Request struct {
				Mem string `default:"200Mi"`
				Cpu string `default:"50m"`
			}
		}
		ConfigReloaderCPU    string `default:"100m"`
		ConfigReloaderMemory string `default:"25Mi"`
		ConfigReloadImage    string `default:"jimmidyson/configmap-reload:v0.3.0"`
	}
	VMAgentDefault struct {
		Image               string `default:"victoriametrics/vmagent"`
		Version             string `default:"v1.94.0"`
		ConfigReloadImage   string `default:"quay.io/prometheus-operator/prometheus-config-reloader:v0.68.0"`
		Port                string `default:"8429"`
		UseDefaultResources bool   `default:"true"`
		Resource            struct {
			Limit struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"200m"`
			}
			Request struct {
				Mem string `default:"200Mi"`
				Cpu string `default:"50m"`
			}
		}
		ConfigReloaderCPU    string `default:"100m"`
		ConfigReloaderMemory string `default:"25Mi"`
	}

	VMSingleDefault struct {
		Image               string `default:"victoriametrics/victoria-metrics"`
		Version             string `default:"v1.94.0"`
		Port                string `default:"8429"`
		UseDefaultResources bool   `default:"true"`
		Resource            struct {
			Limit struct {
				Mem string `default:"1500Mi"`
				Cpu string `default:"1200m"`
			}
			Request struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"150m"`
			}
		}
		ConfigReloaderCPU    string `default:"100m"`
		ConfigReloaderMemory string `default:"25Mi"`
	}

	VMClusterDefault struct {
		UseDefaultResources bool `default:"true"`
		VMSelectDefault     struct {
			Image    string `default:"victoriametrics/vmselect"`
			Version  string `default:"v1.94.0-cluster"`
			Port     string `default:"8481"`
			Resource struct {
				Limit struct {
					Mem string `default:"1000Mi"`
					Cpu string `default:"500m"`
				}
				Request struct {
					Mem string `default:"500Mi"`
					Cpu string `default:"100m"`
				}
			}
		}
		VMStorageDefault struct {
			Image        string `default:"victoriametrics/vmstorage"`
			Version      string `default:"v1.94.0-cluster"`
			VMInsertPort string `default:"8400"`
			VMSelectPort string `default:"8401"`
			Port         string `default:"8482"`
			Resource     struct {
				Limit struct {
					Mem string `default:"1500Mi"`
					Cpu string `default:"1000m"`
				}
				Request struct {
					Mem string `default:"500Mi"`
					Cpu string `default:"250m"`
				}
			}
		}
		VMInsertDefault struct {
			Image    string `default:"victoriametrics/vminsert"`
			Version  string `default:"v1.94.0-cluster"`
			Port     string `default:"8480"`
			Resource struct {
				Limit struct {
					Mem string `default:"500Mi"`
					Cpu string `default:"500m"`
				}
				Request struct {
					Mem string `default:"200Mi"`
					Cpu string `default:"150m"`
				}
			}
		}
	}

	VMAlertManager struct {
		ConfigReloaderImage          string `default:"jimmidyson/configmap-reload:v0.3.0"`
		ConfigReloaderCPU            string `default:"100m"`
		ConfigReloaderMemory         string `default:"25Mi"`
		AlertmanagerDefaultBaseImage string `default:"prom/alertmanager"`
		AlertManagerVersion          string `default:"v0.25.0"`
		LocalHost                    string `default:"127.0.0.1"`
		UseDefaultResources          bool   `default:"true"`
		Resource                     struct {
			Limit struct {
				Mem string `default:"256Mi"`
				Cpu string `default:"100m"`
			}
			Request struct {
				Mem string `default:"56Mi"`
				Cpu string `default:"30m"`
			}
		}
	}

	DisableSelfServiceScrapeCreation bool `default:"false"`
	VMBackup                         struct {
		Image               string `default:"victoriametrics/vmbackupmanager"`
		Version             string `default:"v1.94.0-enterprise"`
		Port                string `default:"8300"`
		UseDefaultResources bool   `default:"true"`
		Resource            struct {
			Limit struct {
				Mem string `default:"500Mi"`
				Cpu string `default:"500m"`
			}
			Request struct {
				Mem string `default:"200Mi"`
				Cpu string `default:"150m"`
			}
		}
		LogLevel  string `default:"INFO"`
		LogFormat string
	}
	VMAuthDefault struct {
		Image               string `default:"victoriametrics/vmauth"`
		Version             string `default:"v1.94.0"`
		ConfigReloadImage   string `default:"quay.io/prometheus-operator/prometheus-config-reloader:v0.68.0"`
		Port                string `default:"8427"`
		UseDefaultResources bool   `default:"true"`
		Resource            struct {
			Limit struct {
				Mem string `default:"300Mi"`
				Cpu string `default:"200m"`
			}
			Request struct {
				Mem string `default:"100Mi"`
				Cpu string `default:"50m"`
			}
		}
		ConfigReloaderCPU    string `default:"100m"`
		ConfigReloaderMemory string `default:"25Mi"`
	}

	EnabledPrometheusConverter struct {
		PodMonitor         bool `default:"true"`
		ServiceScrape      bool `default:"true"`
		PrometheusRule     bool `default:"true"`
		Probe              bool `default:"true"`
		AlertmanagerConfig bool `default:"true"`
	}
	FilterChildLabelPrefixes      []string `default:""`
	FilterChildAnnotationPrefixes []string `default:""`
	// adds compare-options and sync-options for prometheus objects converted by operator
	// it helps to properly use converter with ArgoCD
	PrometheusConverterAddArgoCDIgnoreAnnotations bool `default:"false"`
	EnabledPrometheusConverterOwnerReferences     bool `default:"false"`
	// allows filtering for converted labels, labels with matched prefix will be ignored
	FilterPrometheusConverterLabelPrefixes []string `default:""`
	// allows filtering for converted annotations, annotations with matched prefix will be ignored
	FilterPrometheusConverterAnnotationPrefixes []string `default:""`
	Host                                        string   `default:"0.0.0.0"`
	ListenAddress                               string   `default:"0.0.0.0"`
	DefaultLabels                               string   `default:"managed-by=vm-operator"`
	Labels                                      Labels   `ignored:"true"`
	LogLevel                                    string
	LogFormat                                   string
	ClusterDomainName                           string        `default:""`
	PodWaitReadyTimeout                         time.Duration `default:"80s"`
	PodWaitReadyIntervalCheck                   time.Duration `default:"5s"`
	PodWaitReadyInitDelay                       time.Duration `default:"10s"`
	// configures force resync interval for VMAgent, VMAlert, VMAlertmanager and VMAuth.
	ForceResyncInterval time.Duration `default:"60s"`
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
	EnableStrictSecurity bool `default:"false"`
}

// Validate - validates config on best effort.
func (boc BaseOperatorConf) Validate() error {
	validateResource := func(name string, res Resource) error {
		if res.Request.Mem != UnLimitedResource {
			if _, err := resource.ParseQuantity(res.Request.Mem); err != nil {
				return fmt.Errorf("cannot parse resource request memory for %q, err :%w", name, err)
			}
		}
		if res.Request.Cpu != UnLimitedResource {
			if _, err := resource.ParseQuantity(res.Request.Cpu); err != nil {
				return fmt.Errorf("cannot parse resource request cpu for %q, err :%w", name, err)
			}
		}
		if res.Limit.Mem != UnLimitedResource {
			if _, err := resource.ParseQuantity(res.Limit.Mem); err != nil {
				return fmt.Errorf("cannot parse resource limit memory for %q, err :%w", name, err)
			}
		}
		if res.Limit.Cpu != UnLimitedResource {
			if _, err := resource.ParseQuantity(res.Limit.Cpu); err != nil {
				return fmt.Errorf("cannot parse resource limit cpu for %q, err :%w", name, err)
			}
		}
		return nil
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

	return nil
}

// PrintDefaults prints default values for all config variables.
// format can be one of: table, list, json, yaml.
func (boc BaseOperatorConf) PrintDefaults(format string) error {
	tabs := tabwriter.NewWriter(os.Stdout, 1, 0, 4, ' ', 0)

	formatter := "unknown"
	switch format {
	case "table":
		formatter = envconfig.DefaultTableFormat
	case "list":
		formatter = envconfig.DefaultListFormat
	case "json":
		formatter = `{{$last := (len (slice . 1))}}{
{{range $index, $item := .}}	'{{usage_key $item}}': '{{usage_default $item}}'{{ if lt $index $last}},{{end}}
{{end}}}`
	case "yaml":
		formatter = `{{range $index, $item := .}}{{usage_key $item}}: '{{usage_default $item}}'
{{end}}`
	default:
		return fmt.Errorf("unknown print format %q", format)
	}

	err := envconfig.Usagef(prefixVar, &boc, tabs, formatter)
	_ = tabs.Flush()
	return err
}

func MustGetBaseConfig() *BaseOperatorConf {
	initConf.Do(func() {
		c := &BaseOperatorConf{}
		err := envconfig.Process(prefixVar, c)
		if err != nil {
			panic(err)
		}
		if c.DefaultLabels != "" {
			defL := Labels{}
			err := defL.Set(c.DefaultLabels)
			if err != nil {
				panic(err)
			}
			c.Labels = defL
		}
		if err := c.Validate(); err != nil {
			panic(err)
		}
		opConf = c
	})
	return opConf
}

var validNamespaceRegex = regexp.MustCompile(`[a-z0-9]([-a-z0-9]*[a-z0-9])?`)

func getWatchNamespace() (string, error) {
	wns, _ := os.LookupEnv(WatchNamespaceEnvVar)
	if len(wns) > 0 {
		// validate namespace with regexp
		if !validNamespaceRegex.MatchString(wns) {
			return "", fmt.Errorf("incorrect value: %s for env var %s, it must match regex: %s", wns, WatchNamespaceEnvVar, validNamespaceRegex.String())
		}
	}
	return wns, nil
}

// MustGetWatchNamespace returns the Namespace the operator should be watching for changes
func MustGetWatchNamespace() string {
	initNamespace.Do(func() {
		wns, err := getWatchNamespace()
		if err != nil {
			panic(err)
		}
		opNamespace = wns
	})

	return opNamespace
}

// IsClusterWideAccessAllowed checks if cluster wide access for components is needed
func IsClusterWideAccessAllowed() bool {
	return MustGetWatchNamespace() == ""
}

func MustGetNamespaceListOptions() *client.ListOptions {
	return &client.ListOptions{
		Namespace: MustGetWatchNamespace(),
	}
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
		splited := strings.Split(value, ",")
		for _, pair := range splited {
			sp := strings.Split(pair, "=")
			m[sp[0]] = sp[1]
		}
	}
	labels.LabelsMap = m
	labels.LabelsString = value
	return nil
}
