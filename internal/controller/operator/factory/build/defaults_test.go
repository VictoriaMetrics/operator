package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestAddEnterpriseTagToAppCommonDefaults(t *testing.T) {
	type opts struct {
		specVersion, defaultVersion, wantVersion string
		cp                                       *commonParams
	}
	f := func(o opts) {
		t.Helper()
		cdp := &vmv1beta1.CommonAppsParams{
			Image: vmv1beta1.Image{
				Tag: o.specVersion,
			},
		}
		appDefaults := &config.ApplicationDefaults{
			Version: o.defaultVersion,
		}
		addDefaultsToCommonParams(cdp, o.cp, appDefaults)
		assert.Equal(t, o.wantVersion, cdp.Image.Tag, "unexpected spec version")
	}

	// preserve spec version
	f(opts{
		specVersion: "v1.120.0",
		wantVersion: "v1.120.0",
	})
	f(opts{
		specVersion:    "v1.120.0",
		defaultVersion: "v1.119.0",
		wantVersion:    "v1.120.0",
	})
	f(opts{
		specVersion:    "v1.120.0",
		defaultVersion: "v1.119.0",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0",
	})
	f(opts{
		specVersion: "v1.120.0",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0",
	})
	f(opts{
		specVersion: "v1.120.0-enterprise",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise",
	})
	f(opts{
		specVersion: "v1.120.0-enterprise-cluster",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise-cluster",
	})

	// change default value
	f(opts{
		defaultVersion: "v1.120.0",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise",
	})
	f(opts{
		defaultVersion: "v1.120.0-cluster",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise-cluster",
	})

	// preserve enterprise version
	f(opts{
		defaultVersion: "v1.120.0-enterprise-cluster",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise-cluster",
	})
	f(opts{
		defaultVersion: "v1.120.0-enterprise",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise",
	})

	// preserve unsupported tag
	f(opts{
		defaultVersion: "v1",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1",
	})
	f(opts{
		defaultVersion: "1.120.0",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "1.120.0",
	})
	f(opts{
		defaultVersion: "v1.120.0-rc1",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-rc1",
	})
	f(opts{
		defaultVersion: "v1.120.0-enterprise-rc1",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise-rc1",
	})
	f(opts{
		defaultVersion: "v1.120.0-enterprise-cluster-rc1",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise-cluster-rc1",
	})
	f(opts{
		defaultVersion: "v1.120.0@sha256xxx",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0@sha256xxx",
	})
	f(opts{
		defaultVersion: "v1.120.0-enterprise@sha256xxx",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise@sha256xxx",
	})
	f(opts{
		defaultVersion: "v1.120.0-cluster@sha256xxx",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-cluster@sha256xxx",
	})
	f(opts{
		defaultVersion: "v1.120.0-enterprise-cluster@sha256xxx",
		cp: &commonParams{
			license: &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			},
		},
		wantVersion: "v1.120.0-enterprise-cluster@sha256xxx",
	})
}
