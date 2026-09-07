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
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

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
