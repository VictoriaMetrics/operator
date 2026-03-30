package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
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

func TestSetTag(t *testing.T) {
	type opts struct {
		componentVersion string
		clusterVersion   string
		expected         string
	}
	f := func(o opts) {
		t.Helper()
		actual := setTag(o.componentVersion, o.clusterVersion)
		assert.Equal(t, o.expected, actual)
	}

	// both componentVersion and clusterVersion present
	f(opts{
		componentVersion: "v1.1.0",
		clusterVersion:   "v1.0.0",
		expected:         "v1.1.0",
	})

	// only componentVersion present
	f(opts{
		componentVersion: "v1.1.0",
		clusterVersion:   "",
		expected:         "v1.1.0",
	})

	// only clusterVersion present
	f(opts{
		componentVersion: "",
		clusterVersion:   "v1.0.0",
		expected:         "v1.0.0",
	})

	// none present
	f(opts{
		componentVersion: "",
		clusterVersion:   "",
		expected:         "",
	})
}

func TestClusterComponentVersionDefaults(t *testing.T) {
	type opts struct {
		clusterVersion   string
		componentVersion string
		imageTag         string
		expectedTag      string
	}

	crCreators := map[string]func(o opts) string{
		"VMCluster/VMSelect": func(o opts) string {
			cr := &vmv1beta1.VMCluster{
				Spec: vmv1beta1.VMClusterSpec{
					ClusterVersion: o.clusterVersion,
					VMSelect: &vmv1beta1.VMSelect{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVMClusterDefaults(cr)
			return cr.Spec.VMSelect.Image.Tag
		},
		"VTCluster/VTSelect": func(o opts) string {
			cr := &vmv1.VTCluster{
				Spec: vmv1.VTClusterSpec{
					ClusterVersion: o.clusterVersion,
					Select: &vmv1.VTSelect{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVTClusterDefaults(cr)
			return cr.Spec.Select.Image.Tag
		},
		"VLCluster/VLSelect": func(o opts) string {
			cr := &vmv1.VLCluster{
				Spec: vmv1.VLClusterSpec{
					ClusterVersion: o.clusterVersion,
					VLSelect: &vmv1.VLSelect{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVLClusterDefaults(cr)
			return cr.Spec.VLSelect.Image.Tag
		},
		"VMCluster/VMInsert": func(o opts) string {
			cr := &vmv1beta1.VMCluster{
				Spec: vmv1beta1.VMClusterSpec{
					ClusterVersion: o.clusterVersion,
					VMInsert: &vmv1beta1.VMInsert{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVMClusterDefaults(cr)
			return cr.Spec.VMInsert.Image.Tag
		},
		"VMCluster/VMStorage": func(o opts) string {
			cr := &vmv1beta1.VMCluster{
				Spec: vmv1beta1.VMClusterSpec{
					ClusterVersion: o.clusterVersion,
					VMStorage: &vmv1beta1.VMStorage{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVMClusterDefaults(cr)
			return cr.Spec.VMStorage.Image.Tag
		},
		"VTCluster/VTInsert": func(o opts) string {
			cr := &vmv1.VTCluster{
				Spec: vmv1.VTClusterSpec{
					ClusterVersion: o.clusterVersion,
					Insert: &vmv1.VTInsert{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVTClusterDefaults(cr)
			return cr.Spec.Insert.Image.Tag
		},
		"VTCluster/VTStorage": func(o opts) string {
			cr := &vmv1.VTCluster{
				Spec: vmv1.VTClusterSpec{
					ClusterVersion: o.clusterVersion,
					Storage: &vmv1.VTStorage{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVTClusterDefaults(cr)
			return cr.Spec.Storage.Image.Tag
		},
		"VLCluster/VLInsert": func(o opts) string {
			cr := &vmv1.VLCluster{
				Spec: vmv1.VLClusterSpec{
					ClusterVersion: o.clusterVersion,
					VLInsert: &vmv1.VLInsert{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVLClusterDefaults(cr)
			return cr.Spec.VLInsert.Image.Tag
		},
		"VLCluster/VLStorage": func(o opts) string {
			cr := &vmv1.VLCluster{
				Spec: vmv1.VLClusterSpec{
					ClusterVersion: o.clusterVersion,
					VLStorage: &vmv1.VLStorage{
						ComponentVersion: o.componentVersion,
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							Image: vmv1beta1.Image{
								Tag: o.imageTag,
							},
						},
					},
				},
			}
			addVLClusterDefaults(cr)
			return cr.Spec.VLStorage.Image.Tag
		},
		"VMCluster/RequestsLoadBalancer": func(o opts) string {
			cr := &vmv1beta1.VMCluster{
				Spec: vmv1beta1.VMClusterSpec{
					ClusterVersion: o.clusterVersion,
					RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
						Enabled: true,
						Spec: vmv1beta1.VMAuthLoadBalancerSpec{
							ComponentVersion: o.componentVersion,
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								Image: vmv1beta1.Image{
									Tag: o.imageTag,
								},
							},
						},
					},
				},
			}
			addVMClusterDefaults(cr)
			return cr.Spec.RequestsLoadBalancer.Spec.Image.Tag
		},
	}

	for _, creator := range crCreators {
		f := func(o opts) {
			t.Helper()
			actual := creator(o)
			if o.expectedTag != "" || o.imageTag != "" || o.clusterVersion != "" || o.componentVersion != "" {
				assert.Equal(t, o.expectedTag, actual)
			} else {
				assert.NotEmpty(t, actual)
			}
		}

		// clusterVersion only
		f(opts{
			clusterVersion: "v1.0.0",
			expectedTag:    "v1.0.0",
		})

		// componentVersion only
		f(opts{
			componentVersion: "v1.1.0",
			expectedTag:      "v1.1.0",
		})

		// both versions present, component takes precedence
		f(opts{
			clusterVersion:   "v1.0.0",
			componentVersion: "v1.1.0",
			expectedTag:      "v1.1.0",
		})

		// image tag takes highest precedence
		f(opts{
			clusterVersion:   "v1.0.0",
			componentVersion: "v1.1.0",
			imageTag:         "v1.2.0",
			expectedTag:      "v1.2.0",
		})
	}

	cfg := getCfg()
	crCreators = map[string]func(o opts) string{
		"VLCluster/RequestsLoadBalancer": func(o opts) string {
			cr := &vmv1.VLCluster{
				Spec: vmv1.VLClusterSpec{
					ClusterVersion: o.clusterVersion,
					RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
						Enabled: true,
						Spec: vmv1beta1.VMAuthLoadBalancerSpec{
							ComponentVersion: o.componentVersion,
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								Image: vmv1beta1.Image{
									Tag: o.imageTag,
								},
							},
						},
					},
				},
			}
			addVLClusterDefaults(cr)
			return cr.Spec.RequestsLoadBalancer.Spec.Image.Tag
		},
		"VTCluster/RequestsLoadBalancer": func(o opts) string {
			cr := &vmv1.VTCluster{
				Spec: vmv1.VTClusterSpec{
					ClusterVersion: o.clusterVersion,
					RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
						Enabled: true,
						Spec: vmv1beta1.VMAuthLoadBalancerSpec{
							ComponentVersion: o.componentVersion,
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								Image: vmv1beta1.Image{
									Tag: o.imageTag,
								},
							},
						},
					},
				},
			}
			addVTClusterDefaults(cr)
			return cr.Spec.RequestsLoadBalancer.Spec.Image.Tag
		},
	}

	for _, creator := range crCreators {
		f := func(o opts) {
			t.Helper()
			actual := creator(o)
			if o.expectedTag != "" || o.imageTag != "" || o.clusterVersion != "" || o.componentVersion != "" {
				assert.Equal(t, o.expectedTag, actual)
			} else {
				assert.NotEmpty(t, actual)
			}
		}

		// clusterVersion only
		f(opts{
			clusterVersion: "v1.0.0",
			expectedTag:    cfg.MetricsVersion,
		})

		// componentVersion only
		f(opts{
			componentVersion: "v1.1.0",
			expectedTag:      "v1.1.0",
		})

		// both versions present, component takes precedence
		f(opts{
			clusterVersion:   "v1.0.0",
			componentVersion: "v1.1.0",
			expectedTag:      "v1.1.0",
		})

		// image tag takes highest precedence
		f(opts{
			clusterVersion:   "v1.0.0",
			componentVersion: "v1.1.0",
			imageTag:         "v1.2.0",
			expectedTag:      "v1.2.0",
		})
	}
}
