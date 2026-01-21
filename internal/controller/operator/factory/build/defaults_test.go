package build

import (
	"testing"

	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestAddEnterpriseTagToAppCommonDefaults(t *testing.T) {
	f := func(specVersion, defaultVersion string, hasLicense bool, wantVersion string) {
		t.Helper()
		cdp := &vmv1beta1.CommonDefaultableParams{
			Image: vmv1beta1.Image{
				Tag: specVersion,
			},
		}
		appDefaults := &config.ApplicationDefaults{
			Version: defaultVersion,
		}
		var license *vmv1beta1.License
		if hasLicense {
			license = &vmv1beta1.License{
				Key: ptr.To("license-key-value"),
			}
		}
		addDefaultsToCommonParams(cdp, license, appDefaults)
		if cdp.Image.Tag != wantVersion {
			t.Fatalf("unexpected spec version: (+%s,-%s)", cdp.Image.Tag, wantVersion)
		}
	}

	// preserve spec version
	f("v1.120.0", "", false, "v1.120.0")
	f("v1.120.0", "v1.119.0", false, "v1.120.0")
	f("v1.120.0", "v1.119.0", true, "v1.120.0")
	f("v1.120.0", "", true, "v1.120.0")
	f("v1.120.0-enterprise", "", true, "v1.120.0-enterprise")
	f("v1.120.0-enterprise-cluster", "", true, "v1.120.0-enterprise-cluster")

	// change default value
	f("", "v1.120.0", true, "v1.120.0-enterprise")
	f("", "v1.120.0-cluster", true, "v1.120.0-enterprise-cluster")

	// preserve enterprise version
	f("", "v1.120.0-enterprise-cluster", true, "v1.120.0-enterprise-cluster")
	f("", "v1.120.0-enterprise", true, "v1.120.0-enterprise")

	// preserve unsupported tag
	f("", "v1", true, "v1")
	f("", "1.120.0", true, "1.120.0")
	f("", "v1.120.0-rc1", true, "v1.120.0-rc1")
	f("", "v1.120.0-enterprise-rc1", true, "v1.120.0-enterprise-rc1")
	f("", "v1.120.0-enterprise-cluster-rc1", true, "v1.120.0-enterprise-cluster-rc1")
	f("", "v1.120.0@sha256xxx", true, "v1.120.0@sha256xxx")
	f("", "v1.120.0-enterprise@sha256xxx", true, "v1.120.0-enterprise@sha256xxx")
	f("", "v1.120.0-cluster@sha256xxx", true, "v1.120.0-cluster@sha256xxx")
	f("", "v1.120.0-enterprise-cluster@sha256xxx", true, "v1.120.0-enterprise-cluster@sha256xxx")
}
