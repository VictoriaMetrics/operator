package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type reader struct {
	Class                      string                 `yaml:"class"`
	DatasourceURL              string                 `yaml:"datasource_url"`
	SamplingPeriod             *duration              `yaml:"sampling_period"`
	QueryRangePath             string                 `yaml:"query_range_path,omitempty"`
	ExtraFilters               []string               `yaml:"extra_filters,omitempty"`
	QueryFromLastSeenTimestamp bool                   `yaml:"query_from_last_seen_timestamp,omitempty"`
	LatencyOffset              *duration              `yaml:"latency_offset,omitempty"`
	MaxPointsPerQuery          int                    `yaml:"max_points_per_query,omitempty"`
	Timezone                   time.Location          `yaml:"tz,omitempty"`
	DataRange                  []string               `yaml:"data_range,omitempty"`
	Queries                    map[string]readerQuery `yaml:"queries,omitempty"`
	ClientConfig               clientConfig           `yaml:",inline"`
}

func (r *reader) validate() error {
	if strings.ToLower(r.Class) == "noop" {
		return nil
	}
	if !slices.Contains([]string{"reader.vm.VmReader", "vm", "reader.synthetic.SyntheticVmReader", "synthetic_vm", "vlogs", "reader.vlogs.VLogsReader"}, r.Class) {
		return fmt.Errorf("anomaly reader class=%q is not supported", r.Class)
	}
	if len(r.Queries) == 0 {
		return fmt.Errorf("anomaly reader queries for class=%q are required", r.Class)
	}
	if r.SamplingPeriod == nil {
		return fmt.Errorf(`"sampling_period" is required`)
	}
	if len(r.DataRange) > 0 {
		if len(r.DataRange) == 2 {
			v1, err := strconv.ParseFloat(r.DataRange[0], 64)
			if err != nil {
				return fmt.Errorf(`cannot parse first value in "data_range": %w`, err)
			}
			v2, err := strconv.ParseFloat(r.DataRange[1], 64)
			if err != nil {
				return fmt.Errorf(`cannot parse second value in "data_range": %w`, err)
			}
			if v1 > v2 {
				return fmt.Errorf(`first value in "data_range" should be smaller than second`)
			}
		} else {
			return fmt.Errorf(`only two values are expected in "data_range", got %d`, len(r.DataRange))
		}
	}
	return nil
}

type readerQuery struct {
	Expr              string        `yaml:"expr"`
	Step              *duration     `yaml:"step,omitempty"`
	DataRange         []string      `yaml:"data_range,omitempty"`
	MaxPointsPerQuery int           `yaml:"max_points_per_query,omitempty"`
	TZ                time.Location `yaml:"tz,omitempty"`
	TenantID          string        `yaml:"tenant_id,omitempty"`
}
