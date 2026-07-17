package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

var testLicense = &License{Key: ptr.To("test-license-key")}

func TestVMSingle_Validate(t *testing.T) {
	f := func(spec VMSingleSpec, wantErr bool) {
		t.Helper()
		r := &VMSingle{
			Spec: spec,
		}
		if wantErr {
			assert.Error(t, r.Validate())
		} else {
			assert.NoError(t, r.Validate())
		}
	}

	// no scrape classes
	f(VMSingleSpec{}, false)

	// single default scrape class
	f(VMSingleSpec{
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "default", Default: ptr.To(true)},
				{Name: "other"},
			},
		},
	}, false)

	// multiple default scrape classes
	f(VMSingleSpec{
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "default", Default: ptr.To(true)},
				{Name: "other", Default: ptr.To(true)},
			},
		},
	}, true)

	// duplicated scrape class names
	f(VMSingleSpec{
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "cls"},
				{Name: "cls"},
			},
		},
	}, true)

	// downsampling without license
	f(VMSingleSpec{
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
		},
	}, true)

	// downsampling with valid config
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
		},
	}, false)

	// downsampling with filter and dedupInterval
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules:         []DownsamplingRule{{Filter: `{env="prod"}`, Periods: []DownsamplingPeriod{{Offset: "90d", Interval: "1h"}}}},
			DedupInterval: "1m",
		},
	}, false)

	// downsampling - multiple periods per rule
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{
				{Offset: "30d", Interval: "10m"},
				{Offset: "180d", Interval: "1h"},
				{Offset: "1y", Interval: "6h"},
			}}},
		},
	}, false)

	// downsampling - duplicate filter
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{
				{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}},
				{Periods: []DownsamplingPeriod{{Offset: "180d", Interval: "1h"}}},
			},
		},
	}, true)

	// downsampling - different filters are ok
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{
				{Filter: `{env="prod"}`, Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}},
				{Filter: `{env="dev"}`, Periods: []DownsamplingPeriod{{Offset: "35d", Interval: "7m"}}},
			},
		},
	}, false)

	// downsampling - period intervals not multiples of each other within one rule
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{
				{Offset: "30d", Interval: "10m"},
				{Offset: "35d", Interval: "7m"},
			}}},
		},
	}, true)

	// downsampling - offset not a multiple of interval
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "1d", Interval: "7m"}}}},
		},
	}, true)

	// downsampling - invalid interval
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "bad"}}}},
		},
	}, true)

	// downsampling - invalid filter
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules: []DownsamplingRule{{Filter: "not-a-filter", Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
		},
	}, true)

	// downsampling - invalid dedupInterval
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules:         []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
			DedupInterval: "bad",
		},
	}, true)

	// downsampling - period interval not a multiple of dedupInterval
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules:         []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
			DedupInterval: "7m",
		},
	}, true)

	// downsampling - period interval is a multiple of dedupInterval
	f(VMSingleSpec{
		License: testLicense,
		Downsampling: &DownsamplingConfig{
			Rules:         []DownsamplingRule{{Periods: []DownsamplingPeriod{{Offset: "30d", Interval: "10m"}}}},
			DedupInterval: "5m",
		},
	}, false)

	// retention filters without license
	f(VMSingleSpec{
		RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "3d"}},
	}, true)

	// retention filters with valid config
	f(VMSingleSpec{
		License:          testLicense,
		RetentionPeriod:  "30",
		RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "3d"}},
	}, false)

	// retention filters - invalid filter
	f(VMSingleSpec{
		License:          testLicense,
		RetentionFilters: &RetentionFiltersConfig{{Filter: "not-a-filter", Retention: "3d"}},
	}, true)

	// retention filters - invalid retention
	f(VMSingleSpec{
		License:          testLicense,
		RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "bad"}},
	}, true)

	// retention filters - retention exceeds retentionPeriod
	f(VMSingleSpec{
		License:          testLicense,
		RetentionPeriod:  "30d",
		RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "1y"}},
	}, true)

	// retention filters - retention equal to retentionPeriod is ok
	f(VMSingleSpec{
		License:          testLicense,
		RetentionPeriod:  "1y",
		RetentionFilters: &RetentionFiltersConfig{{Filter: `{env="dev"}`, Retention: "1y"}},
	}, false)
}

func TestVMSingle_PrefixedName(t *testing.T) {
	cr := &VMSingle{}
	cr.Name = "myapp"
	assert.Equal(t, "vmsingle-myapp", cr.PrefixedName())
}
