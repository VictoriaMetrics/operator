/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestValidateVLDistributed(t *testing.T) {
	type opts struct {
		cr    VLDistributed
		isErr bool
	}
	f := func(o opts) {
		t.Helper()
		err := o.cr.Validate()
		if o.isErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}

	// no zone name defined error
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				Zones: []VLDistributedZone{
					{
						VLCluster: VLDistributedZoneCluster{Name: "a"},
					},
				},
			},
		},
		isErr: true,
	})

	// duplicated zone names
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				Zones: []VLDistributedZone{
					{
						Name:      "zone-1",
						VLCluster: VLDistributedZoneCluster{Name: "a"},
					},
					{
						Name:      "zone-1",
						VLCluster: VLDistributedZoneCluster{Name: "b"},
					},
				},
			},
		},
		isErr: true,
	})

	// same vlcluster in two zones
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				Zones: []VLDistributedZone{
					{
						Name:      "zone-1",
						VLCluster: VLDistributedZoneCluster{Name: "a"},
					},
					{
						Name:      "zone-2",
						VLCluster: VLDistributedZoneCluster{Name: "a"},
					},
				},
			},
		},
		isErr: true,
	})

	// same vlsingle in two zones
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				BackendType: VLDistributedBackendTypeVLSingle,
				Zones: []VLDistributedZone{
					{
						Name:     "zone-1",
						VLSingle: &VLDistributedZoneSingle{Name: "a"},
					},
					{
						Name:     "zone-2",
						VLSingle: &VLDistributedZoneSingle{Name: "a"},
					},
				},
			},
		},
		isErr: true,
	})

	// backendType=VLSingle incompatible with vlcluster config in zone
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				BackendType: VLDistributedBackendTypeVLSingle,
				Zones: []VLDistributedZone{
					{
						Name:     "zone-1",
						VLSingle: &VLDistributedZoneSingle{Name: "single-a"},
						VLCluster: VLDistributedZoneCluster{Spec: vmv1.VLClusterSpec{
							VLInsert: &vmv1.VLInsert{},
						}},
					},
				},
			},
		},
		isErr: true,
	})

	// backendType=VLSingle incompatible with common vlcluster config
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				BackendType: VLDistributedBackendTypeVLSingle,
				ZoneCommon: VLDistributedZoneCommon{
					VLSingle: &VLDistributedZoneSingle{
						Spec: &vmv1.VLSingleSpec{},
					},
					VLCluster: VLDistributedZoneCluster{Spec: vmv1.VLClusterSpec{
						VLInsert: &vmv1.VLInsert{},
					}},
				},
				Zones: []VLDistributedZone{
					{
						Name: "zone-1",
					},
				},
			},
		},
		isErr: true,
	})

	// backendType=VLCluster (default) incompatible with common vlsingle config
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				ZoneCommon: VLDistributedZoneCommon{
					VLSingle: &VLDistributedZoneSingle{
						Spec: &vmv1.VLSingleSpec{},
					},
				},
				Zones: []VLDistributedZone{
					{
						Name: "zone-1",
					},
				},
			},
		},
		isErr: true,
	})

	// duplicated agent names are ignored when VLAgent is disabled for both zones
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				ZoneCommon: VLDistributedZoneCommon{
					VLAgent: VLDistributedZoneAgent{Spec: VLDistributedZoneAgentSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
					}},
				},
				Zones: []VLDistributedZone{
					{
						Name:      "zone-1",
						VLCluster: VLDistributedZoneCluster{Spec: vmv1.VLClusterSpec{VLInsert: &vmv1.VLInsert{}, VLSelect: &vmv1.VLSelect{}}},
						VLAgent:   VLDistributedZoneAgent{Name: "shared-agent"},
					},
					{
						Name:      "zone-2",
						VLCluster: VLDistributedZoneCluster{Spec: vmv1.VLClusterSpec{VLInsert: &vmv1.VLInsert{}, VLSelect: &vmv1.VLSelect{}}},
						VLAgent:   VLDistributedZoneAgent{Name: "shared-agent"},
					},
				},
			},
		},
		isErr: false,
	})

	// duplicated agent names still error when VLAgent is enabled
	f(opts{
		cr: VLDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VLDistributedSpec{
				Zones: []VLDistributedZone{
					{
						Name:      "zone-1",
						VLCluster: VLDistributedZoneCluster{Spec: vmv1.VLClusterSpec{VLInsert: &vmv1.VLInsert{}, VLSelect: &vmv1.VLSelect{}}},
						VLAgent:   VLDistributedZoneAgent{Name: "shared-agent"},
					},
					{
						Name:      "zone-2",
						VLCluster: VLDistributedZoneCluster{Spec: vmv1.VLClusterSpec{VLInsert: &vmv1.VLInsert{}, VLSelect: &vmv1.VLSelect{}}},
						VLAgent:   VLDistributedZoneAgent{Name: "shared-agent"},
					},
				},
			},
		},
		isErr: true,
	})
}

// TestVLAgentEnabled covers the zone/common replicaCount=0 precedence resolution.
func TestVLAgentEnabled(t *testing.T) {
	cr := &VLDistributed{
		Spec: VLDistributedSpec{
			ZoneCommon: VLDistributedZoneCommon{
				VLAgent: VLDistributedZoneAgent{Spec: VLDistributedZoneAgentSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
				}},
			},
		},
	}

	// no override: falls back to common (disabled)
	zone := &VLDistributedZone{Name: "zone-1"}
	assert.False(t, zone.VLAgentEnabled(cr))

	// zone override wins over common
	zone = &VLDistributedZone{Name: "zone-2", VLAgent: VLDistributedZoneAgent{Spec: VLDistributedZoneAgentSpec{
		CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
	}}}
	assert.True(t, zone.VLAgentEnabled(cr))

	// default is enabled when neither zone nor common set it
	cr.Spec.ZoneCommon.VLAgent.Spec.ReplicaCount = nil
	zone = &VLDistributedZone{Name: "zone-3"}
	assert.True(t, zone.VLAgentEnabled(cr))
}

func TestEnsureNoVLOwners(t *testing.T) {
	cr := &VLDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.String(),
			Kind:       "VLDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vdc",
			Namespace: "default",
			UID:       k8stypes.UID("owner-uid"),
		},
	}
	otherCR := &VLDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.String(),
			Kind:       "VLDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other",
			Namespace: "default",
			UID:       k8stypes.UID("other-uid"),
		},
	}

	vlc := &vmv1.VLCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vlc",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: otherCR.APIVersion,
					Kind:       otherCR.Kind,
					Name:       otherCR.Name,
					UID:        otherCR.UID,
				},
			},
		},
	}

	err := cr.Owns(vlc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is owned by other distributed resource")
}
