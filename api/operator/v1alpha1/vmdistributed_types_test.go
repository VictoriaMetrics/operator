package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestValidateVMDistributed(t *testing.T) {
	type opts struct {
		cr    VMDistributed
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
						VMCluster: VMDistributedZoneCluster{Name: "a"},
					},
					{
						Name:      "zone-1",
						VMCluster: VMDistributedZoneCluster{Name: "b"},
					},
				},
			},
		},
		isErr: true,
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
						VMCluster: VMDistributedZoneCluster{Name: "a"},
					},
					{
						Name:      "zone-2",
						VMCluster: VMDistributedZoneCluster{Name: "a"},
					},
				},
			},
		},
		isErr: true,
	})
}

func TestEnsureNoVMOwners(t *testing.T) {
	cr := &VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupVersion.String(),
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
			APIVersion: GroupVersion.String(),
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
