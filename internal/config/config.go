package config

import (
	"strings"
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
)

var (
	opConf   *BaseOperatorConf
	initConf sync.Once
)

const prefixVar = "VM"

//genvars:true
type BaseOperatorConf struct {
	VMAlertDefault struct {
		Image    string `default:"victoriametrics/vmalert"`
		Version  string `default:"v1.46.0"`
		Port     string `default:"8080"`
		Resource struct {
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
		Image             string `default:"victoriametrics/vmagent"`
		Version           string `default:"v1.46.0"`
		ConfigReloadImage string `default:"quay.io/coreos/prometheus-config-reloader:v0.42.0"`
		Port              string `default:"8429"`
		Resource          struct {
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
		Image    string `default:"victoriametrics/victoria-metrics"`
		Version  string `default:"v1.46.0"`
		Port     string `default:"8429"`
		Resource struct {
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
		VMSelectDefault struct {
			Image    string `default:"victoriametrics/vmselect"`
			Version  string `default:"v1.46.0-cluster"`
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
			Version      string `default:"v1.46.0-cluster"`
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
			Version  string `default:"v1.46.0-cluster"`
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
	}

	DisableSelfServiceScrapeCreation bool `default:"false"`
	VMBackup                         struct {
		Image    string `default:"victoriametrics/vmbackuper"`
		Version  string `default:"v1.0.0"`
		Port     string `default:"8300"`
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
		LogLevel  string `default:"INFO"`
		LogFormat string
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
	(*labels).LabelsMap = m
	(*labels).LabelsString = value
	return nil
}

type Namespaces struct {
	// allow list/deny list for common custom resources
	AllowList, DenyList map[string]struct{}
	// allow list for prometheus/alertmanager custom resources

}
