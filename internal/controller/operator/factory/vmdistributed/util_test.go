package vmdistributed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/podutil"
)

func TestMergeSpecs(t *testing.T) {
	t.Run("VMCluster", func(t *testing.T) {
		f := func(common, zone *vmv1beta1.VMClusterSpec, zoneName string, expected *vmv1beta1.VMClusterSpec) {
			t.Helper()
			got, err := podutil.MergeSpecs(common, zone, zoneName)
			assert.NoError(t, err)
			assert.Equal(t, expected, got)
		}

		// zone-specific retention period
		f(&vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		}, &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
		}, "zone-a", &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		})

		// %ZONE% templating
		f(&vmv1beta1.VMClusterSpec{
			RetentionPeriod: "1d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
					NodeSelector: map[string]string{
						"topology.kubernetes.io/zone": "%ZONE%",
					},
				},
			},
		}, &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
		}, "zone-a", &vmv1beta1.VMClusterSpec{
			RetentionPeriod: "30d",
			VMStorage: &vmv1beta1.VMStorage{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
					NodeSelector: map[string]string{
						"topology.kubernetes.io/zone": "zone-a",
					},
				},
			},
		})
	})

	t.Run("VMAgent", func(t *testing.T) {
		f := func(common, zone *vmv1alpha1.VMDistributedZoneAgentSpec, zoneName string, expected *vmv1alpha1.VMDistributedZoneAgentSpec) {
			t.Helper()
			got, err := podutil.MergeSpecs(common, zone, zoneName)
			assert.NoError(t, err)
			assert.Equal(t, expected, got)
		}

		f(&vmv1alpha1.VMDistributedZoneAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "%ZONE%",
				},
			},
		}, &vmv1alpha1.VMDistributedZoneAgentSpec{}, "zone-b", &vmv1alpha1.VMDistributedZoneAgentSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "zone-b",
				},
			},
		})
	})
}

func TestMergeRemoteWriteTargets(t *testing.T) {
	urlOf := func(rw *vmv1beta1.VMAgentRemoteWriteSpec) string { return rw.URL }
	f := func(common, zone []vmv1beta1.VMAgentRemoteWriteSpec, zoneName string, expected []vmv1beta1.VMAgentRemoteWriteSpec) {
		t.Helper()
		got, err := podutil.MergeRemoteWriteTargets(common, zone, zoneName, urlOf)
		assert.NoError(t, err)
		assert.Equal(t, expected, got)
	}
	fe := func(common, zone []vmv1beta1.VMAgentRemoteWriteSpec, zoneName, wantErr string) {
		t.Helper()
		_, err := podutil.MergeRemoteWriteTargets(common, zone, zoneName, urlOf)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), wantErr)
	}

	// both empty, must not panic on the nil slice
	f(nil, nil, "zone-a", nil)

	// common only
	f([]vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write"},
	}, nil, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write"},
	})

	// zone only
	f(nil, []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://b/write"},
	}, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://b/write"},
	})

	// matching url is deep-merged, zone wins field by field and common-only fields survive
	f([]vmv1beta1.VMAgentRemoteWriteSpec{
		{
			URL:               "https://a/write",
			SendTimeout:       ptr.To("30s"),
			BearerTokenSecret: &corev1.SecretKeySelector{Key: "token"},
			ForceVMProto:      true,
		},
	}, []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write", SendTimeout: ptr.To("2m")},
	}, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{
			URL:               "https://a/write",
			SendTimeout:       ptr.To("2m"),
			BearerTokenSecret: &corev1.SecretKeySelector{Key: "token"},
			ForceVMProto:      true,
		},
	})

	// nested structs are merged rather than replaced
	f([]vmv1beta1.VMAgentRemoteWriteSpec{
		{
			URL:       "https://a/write",
			TLSConfig: &vmv1beta1.TLSConfig{ServerName: "a.example.com"},
		},
	}, []vmv1beta1.VMAgentRemoteWriteSpec{
		{
			URL:       "https://a/write",
			TLSConfig: &vmv1beta1.TLSConfig{InsecureSkipVerify: true},
		},
	}, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{
			URL:       "https://a/write",
			TLSConfig: &vmv1beta1.TLSConfig{ServerName: "a.example.com", InsecureSkipVerify: true},
		},
	})

	// a slice of scalars inside a matched entry is replaced as a whole
	f([]vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write", Headers: []string{"h1: a", "h2: b"}},
	}, []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write", Headers: []string{"h3: c"}},
	}, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write", Headers: []string{"h3: c"}},
	})

	// a slice of structs inside a matched entry is merged element by element.
	// this mirrors how MergeSpecs already behaves for containers, volumes and the like,
	// see the note on MergeRemoteWriteTargets.
	f([]vmv1beta1.VMAgentRemoteWriteSpec{
		{
			URL: "https://a/write",
			InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
				{Action: "labeldrop", Regex: vmv1beta1.StringOrArray{"foo"}},
			},
		},
	}, []vmv1beta1.VMAgentRemoteWriteSpec{
		{
			URL: "https://a/write",
			InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
				{Action: "labelkeep"},
			},
		},
	}, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{
			URL: "https://a/write",
			InlineUrlRelabelConfig: []*vmv1beta1.RelabelConfig{
				{Action: "labelkeep", Regex: vmv1beta1.StringOrArray{"foo"}},
			},
		},
	})

	// %ZONE% in a common url is addressable from the zone list by the rendered url
	f([]vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/%ZONE%/write", SendTimeout: ptr.To("30s")},
	}, []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/zone-a/write", SendTimeout: ptr.To("2m")},
	}, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/zone-a/write", SendTimeout: ptr.To("2m")},
	})

	// ...and by the templated url as well, since both lists are rendered
	f([]vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/%ZONE%/write", SendTimeout: ptr.To("30s")},
	}, []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/%ZONE%/write", SendTimeout: ptr.To("2m")},
	}, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/zone-a/write", SendTimeout: ptr.To("2m")},
	})

	// merge and append together, common order is preserved and zone-only entries go last
	f([]vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write", SendTimeout: ptr.To("30s")},
		{URL: "https://b/write"},
	}, []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write", SendTimeout: ptr.To("2m")},
		{URL: "https://c/write"},
	}, "zone-a", []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write", SendTimeout: ptr.To("2m")},
		{URL: "https://b/write"},
		{URL: "https://c/write"},
	})

	// empty url
	fe([]vmv1beta1.VMAgentRemoteWriteSpec{{URL: ""}}, nil, "zone-a",
		"zoneCommon.vmagent.spec.additionalRemoteWriteTargets[0].url cannot be empty")
	fe(nil, []vmv1beta1.VMAgentRemoteWriteSpec{{URL: ""}}, "zone-a",
		"vmagent.spec.additionalRemoteWriteTargets[0].url cannot be empty")

	// duplicated urls within a single list
	fe([]vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write"},
		{URL: "https://a/write"},
	}, nil, "zone-a", "is duplicated")
	fe(nil, []vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/write"},
		{URL: "https://a/write"},
	}, "zone-a", "is duplicated")

	// a collision which only appears after %ZONE% is rendered
	fe([]vmv1beta1.VMAgentRemoteWriteSpec{
		{URL: "https://a/%ZONE%/write"},
		{URL: "https://a/zone-a/write"},
	}, nil, "zone-a", "is duplicated")
}
