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

//nolint:dupl
package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestValidateVMDistributed(t *testing.T) {
	type opts struct {
		cr     VMDistributed
		isErr  bool
		errMsg string
	}
	f := func(o opts) {
		t.Helper()
		err := o.cr.Validate()
		if o.isErr {
			if assert.Error(t, err) && o.errMsg != "" {
				assert.Contains(t, err.Error(), o.errMsg)
			}
		} else {
			assert.NoError(t, err)
		}
	}

	// no zone name defined error
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				Zones: []VMDistributedZone{
					{
						VMCluster: VMDistributedZoneCluster{Name: "a"},
					},
				},
			},
		},
		isErr: true,
	})

	// duplicated zone names
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				Zones: []VMDistributedZone{
					{
						Name:      "zone-1",
						VMCluster: VMDistributedZoneCluster{Name: "a", Spec: vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}, VMSelect: &vmv1beta1.VMSelect{}}},
					},
					{
						Name:      "zone-1",
						VMCluster: VMDistributedZoneCluster{Name: "b", Spec: vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}, VMSelect: &vmv1beta1.VMSelect{}}},
					},
				},
			},
		},
		isErr:  true,
		errMsg: "is duplicated, zone names must be unique",
	})

	// same vmcluster in two zones
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				Zones: []VMDistributedZone{
					{
						Name:      "zone-1",
						VMCluster: VMDistributedZoneCluster{Name: "a", Spec: vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}, VMSelect: &vmv1beta1.VMSelect{}}},
					},
					{
						Name:      "zone-2",
						VMCluster: VMDistributedZoneCluster{Name: "a", Spec: vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}, VMSelect: &vmv1beta1.VMSelect{}}},
					},
				},
			},
		},
		isErr:  true,
		errMsg: "is already added in a different zone",
	})

	// same vmsingle in two zones
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				BackendType: VMDistributedBackendTypeVMSingle,
				Zones: []VMDistributedZone{
					{
						Name:     "zone-1",
						VMSingle: &VMDistributedZoneSingle{Name: "a"},
					},
					{
						Name:     "zone-2",
						VMSingle: &VMDistributedZoneSingle{Name: "a"},
					},
				},
			},
		},
		isErr: true,
	})

	// backendType=VMSingle incompatible with vmcluster config in zone
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				BackendType: VMDistributedBackendTypeVMSingle,
				Zones: []VMDistributedZone{
					{
						Name:     "zone-1",
						VMSingle: &VMDistributedZoneSingle{Name: "single-a"},
						VMCluster: VMDistributedZoneCluster{Spec: vmv1beta1.VMClusterSpec{
							VMInsert: &vmv1beta1.VMInsert{},
						}},
					},
				},
			},
		},
		isErr: true,
	})

	// backendType=VMSingle incompatible with common vmcluster config
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				BackendType: VMDistributedBackendTypeVMSingle,
				ZoneCommon: VMDistributedZoneCommon{
					VMSingle: &VMDistributedZoneSingle{
						Spec: &vmv1beta1.VMSingleSpec{},
					},
					VMCluster: VMDistributedZoneCluster{Spec: vmv1beta1.VMClusterSpec{
						VMInsert: &vmv1beta1.VMInsert{},
					}},
				},
				Zones: []VMDistributedZone{
					{
						Name: "zone-1",
					},
				},
			},
		},
		isErr: true,
	})
	// backendType=VMCluster (default) incompatible with common vmsingle config
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				ZoneCommon: VMDistributedZoneCommon{
					VMSingle: &VMDistributedZoneSingle{
						Spec: &vmv1beta1.VMSingleSpec{},
					},
				},
				Zones: []VMDistributedZone{
					{
						Name: "zone-1",
					},
				},
			},
		},
		isErr: true,
	})

	// duplicated agent names are ignored when VMAgent is disabled for both zones
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				ZoneCommon: VMDistributedZoneCommon{
					VMAgent: VMDistributedZoneAgent{Spec: VMDistributedZoneAgentSpec{
						CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
					}},
				},
				Zones: []VMDistributedZone{
					{
						Name:      "zone-1",
						VMCluster: VMDistributedZoneCluster{Spec: vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}, VMSelect: &vmv1beta1.VMSelect{}}},
						VMAgent:   VMDistributedZoneAgent{Name: "shared-agent"},
					},
					{
						Name:      "zone-2",
						VMCluster: VMDistributedZoneCluster{Spec: vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}, VMSelect: &vmv1beta1.VMSelect{}}},
						VMAgent:   VMDistributedZoneAgent{Name: "shared-agent"},
					},
				},
			},
		},
		isErr: false,
	})

	// duplicated agent names still error when VMAgent is enabled
	f(opts{
		cr: VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: VMDistributedSpec{
				Zones: []VMDistributedZone{
					{
						Name:      "zone-1",
						VMCluster: VMDistributedZoneCluster{Spec: vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}, VMSelect: &vmv1beta1.VMSelect{}}},
						VMAgent:   VMDistributedZoneAgent{Name: "shared-agent"},
					},
					{
						Name:      "zone-2",
						VMCluster: VMDistributedZoneCluster{Spec: vmv1beta1.VMClusterSpec{VMInsert: &vmv1beta1.VMInsert{}, VMSelect: &vmv1beta1.VMSelect{}}},
						VMAgent:   VMDistributedZoneAgent{Name: "shared-agent"},
					},
				},
			},
		},
		isErr: true,
	})
}

// TestVMAgentEnabled covers the zone/common replicaCount=0 precedence resolution.
func TestVMAgentEnabled(t *testing.T) {
	cr := &VMDistributed{
		Spec: VMDistributedSpec{
			ZoneCommon: VMDistributedZoneCommon{
				VMAgent: VMDistributedZoneAgent{Spec: VMDistributedZoneAgentSpec{
					CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(0))},
				}},
			},
		},
	}

	// no override: falls back to common (disabled)
	zone := &VMDistributedZone{Name: "zone-1"}
	assert.False(t, zone.VMAgentEnabled(cr))

	// zone override wins over common
	zone = &VMDistributedZone{Name: "zone-2", VMAgent: VMDistributedZoneAgent{Spec: VMDistributedZoneAgentSpec{
		CommonAppsParams: vmv1beta1.CommonAppsParams{ReplicaCount: ptr.To(int32(1))},
	}}}
	assert.True(t, zone.VMAgentEnabled(cr))

	// default is enabled when neither zone nor common set it
	cr.Spec.ZoneCommon.VMAgent.Spec.ReplicaCount = nil
	zone = &VMDistributedZone{Name: "zone-3"}
	assert.True(t, zone.VMAgentEnabled(cr))
}

func TestEnsureNoVMOwners(t *testing.T) {
	cr := &VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vdc",
			Namespace: "default",
			UID:       k8stypes.UID("owner-uid"),
		},
	}
	otherCR := &VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other",
			Namespace: "default",
			UID:       k8stypes.UID("other-uid"),
		},
	}

	vmc := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc",
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

	err := cr.Owns(vmc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is owned by other distributed resource")
}
