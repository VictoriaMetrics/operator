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
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VMSingle{Spec: VMSingleSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	// default — type prefix applied
	f("myapp", false, "vmsingle-myapp")

	// useLegacyNaming — CR name used as-is
	f("myapp", true, "myapp")
}

// TestVMSingle_IsUnmanaged is the VMSingle counterpart of TestVMAlert_IsUnmanaged.
func TestVMSingle_IsUnmanaged(t *testing.T) {
	f := func(cr VMSingle, want bool) {
		t.Helper()
		assert.Equal(t, want, cr.IsUnmanaged(nil))
	}

	f(VMSingle{Spec: VMSingleSpec{CommonScrapeParams: CommonScrapeParams{SelectAllByDefault: true}}}, false)
	f(VMSingle{}, true)
	f(VMSingle{
		Status: VMSingleStatus{ParsingSpecError: `json: unknown field "foo"`},
		Spec:   VMSingleSpec{CommonScrapeParams: CommonScrapeParams{SelectAllByDefault: true}},
	}, false)
	f(VMSingle{
		Status: VMSingleStatus{ParsingSpecError: "some other unrelated parse failure"},
		Spec:   VMSingleSpec{CommonScrapeParams: CommonScrapeParams{SelectAllByDefault: true}},
	}, true)
}

func TestVMSingle_SnapshotDeletePath(t *testing.T) {
	type opts struct {
		host          string
		port          string
		extraArgs     map[string]string
		httpListeners []HTTPListener
		want          string
	}
	f := func(o opts) {
		t.Helper()
		cr := VMSingle{
			Spec: VMSingleSpec{
				StandardAppsParams: StandardAppsParams{
					CommonAppsParams: CommonAppsParams{Port: o.port, ExtraArgs: o.extraArgs},
					HTTPListeners:    o.httpListeners,
				},
			},
		}
		got := cr.SnapshotDeletePath(o.host)
		assert.Equal(t, o.want, got)
	}

	// default delete path
	f(opts{
		host: "localhost",
		port: "8428",
		want: "http://localhost:8428/snapshot/delete",
	})

	// delete path with prefix
	f(opts{
		host:      "127.0.0.1",
		port:      "8428",
		extraArgs: map[string]string{httpPathPrefixFlag: "/pref-1", "other-flag": "other-value"},
		want:      "http://127.0.0.1:8428/pref-1/snapshot/delete",
	})

	// delete path with auth key
	f(opts{
		host:      "127.0.0.1",
		port:      "8428",
		extraArgs: map[string]string{httpPathPrefixFlag: "/pref-1", "other-flag": "other-value", snapshotAuthKeyFlag: "test"},
		want:      "http://127.0.0.1:8428/pref-1/snapshot/delete?authKey=test",
	})

	// primary listener's port overrides Port
	f(opts{
		host:          "localhost",
		port:          "8428",
		httpListeners: []HTTPListener{{Addr: ":9999", Primary: true}},
		want:          "http://localhost:9999/snapshot/delete",
	})
}

func TestVMSingle_SnapshotCreatePath(t *testing.T) {
	type opts struct {
		host      string
		port      string
		extraArgs map[string]string
		want      string
	}
	f := func(o opts) {
		t.Helper()
		cr := VMSingle{
			Spec: VMSingleSpec{
				StandardAppsParams: StandardAppsParams{
					CommonAppsParams: CommonAppsParams{Port: o.port, ExtraArgs: o.extraArgs},
				},
			},
		}
		got := cr.SnapshotCreatePath(o.host)
		assert.Equal(t, o.want, got)
	}

	// base ok
	f(opts{
		host: "localhost",
		port: "8429",
		want: "http://localhost:8429/snapshot/create",
	})

	// with prefix
	f(opts{
		host: "127.0.0.1",
		port: "8429",
		extraArgs: map[string]string{
			"http.pathPrefix": "/prefix/custom",
		},
		want: "http://127.0.0.1:8429/prefix/custom/snapshot/create",
	})

	// with prefix and auth key
	f(opts{
		host: "localhost",
		port: "8429",
		extraArgs: map[string]string{
			"http.pathPrefix": "/prefix/custom",
			"snapshotAuthKey": "some-auth-key",
		},
		want: "http://localhost:8429/prefix/custom/snapshot/create?authKey=some-auth-key",
	})
}
