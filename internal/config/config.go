package config

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	opConf   *BaseOperatorConf
	initConf sync.Once
)

const prefixVar = "VM"
const UnLimitedResource = "unlimited"

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
	PSPAutoCreateEnabled bool `default:"true"`
	VMAlertDefault       struct {
		Image               string `default:"victoriametrics/vmalert"`
		Version             string `default:"v1.58.0"`
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
		Version             string `default:"v1.58.0"`
		ConfigReloadImage   string `default:"quay.io/prometheus-operator/prometheus-config-reloader:v0.48.1"`
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
		Version             string `default:"v1.58.0"`
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
			Version  string `default:"v1.58.0-cluster"`
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
			Version      string `default:"v1.58.0-cluster"`
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
			Version  string `default:"v1.58.0-cluster"`
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
		AlertmanagerDefaultBaseImage string `default:"quay.io/prometheus/alertmanager"`
		AlertManagerVersion          string `default:"v0.20.0"`
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
		Version             string `default:"v1.58.0-enterprise"`
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
		Version             string `default:"v1.60.0"`
		ConfigReloadImage   string `default:"quay.io/prometheus-operator/prometheus-config-reloader:v0.48.1"`
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
		PodMonitor     bool `default:"true"`
		ServiceScrape  bool `default:"true"`
		PrometheusRule bool `default:"true"`
		Probe          bool `default:"true"`
	}
	EnabledPrometheusConverterOwnerReferences bool `default:"false"`

	Host                      string `default:"0.0.0.0"`
	ListenAddress             string `default:"0.0.0.0"`
	DefaultLabels             string `default:"managed-by=vm-operator"`
	Labels                    Labels `ignored:"true"`
	LogLevel                  string
	LogFormat                 string
	ClusterDomainName         string        `default:"cluster.local"`
	PodWaitReadyTimeout       time.Duration `default:"80s"`
	PodWaitReadyIntervalCheck time.Duration `default:"5s"`
	PodWaitReadyInitDelay     time.Duration `default:"10s"`
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

type Namespaces struct {
	// allow list/deny list for common custom resources
	AllowList, DenyList map[string]struct{}
	// allow list for prometheus/alertmanager custom resources

}
