package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
)

// helperParts returns the CR and its nested specs with methods.
func helperParts(kind distributedKind) (any, func() []any) {
	if kind == vlDistributedKind {
		cr := &VLDistributed{}
		return cr, func() []any {
			c := &cr.Spec.ZoneCommon
			parts := []any{&cr.Spec, &c.VLAgent.Spec, &c.RemoteWrite}
			for i := range cr.Spec.Zones {
				z := &cr.Spec.Zones[i]
				parts = append(parts, z, &z.VLAgent.Spec, &z.RemoteWrite)
			}
			return parts
		}
	}

	cr := &VMDistributed{}
	return cr, func() []any {
		c := &cr.Spec.ZoneCommon
		parts := []any{&cr.Spec, &c.VMAgent.Spec, &c.RemoteWrite}
		for i := range cr.Spec.Zones {
			z := &cr.Spec.Zones[i]
			parts = append(parts, z, &z.VMAgent.Spec, &z.RemoteWrite)
		}
		return parts
	}
}

// crArg passes cr to methods of v accepting it; skips DeepCopyInto(self), it wipes slices.
func crArg(cr, v any) fuzztest.ArgFunc {
	crv := reflect.ValueOf(cr)
	self := reflect.TypeOf(v)
	return func(at reflect.Type) (reflect.Value, bool) {
		return crv, crv.Type().AssignableTo(at) && at != self
	}
}

func FuzzCRHelpers(f *testing.F) {
	for _, seed := range []struct {
		kind distributedKind
		data string
	}{
		{vmDistributedKind, `{}`},
		{vmDistributedKind, `{"metadata":{"name":"d"},"spec":{"vmauth":{"name":"auth"},"zoneCommon":{"vmcluster":{"name":"c-%ZONE%"},"vmagent":{"name":"a-%ZONE%","spec":{"replicaCount":1}},"remoteWrite":{"sendTimeout":"1m","headers":["a: b"]}},"zones":[{"name":"east"},{"name":"west","vmcluster":{"name":"w"},"vmagent":{"name":"wa"},"remoteWrite":{"maxDiskUsage":"1GB"}}]},"status":{"lastAppliedSpec":{}}}`},
		{vmDistributedKind, `{"metadata":{"name":"d"},"spec":{"backendType":"VMSingle","zoneCommon":{"vmsingle":{"name":"s-%ZONE%"}},"zones":[{"name":"east"},{"name":"west","vmsingle":{"name":"w"}}],"paused":true}}`},
		{vmDistributedKind, `{"spec":"bad"}`},
		{vmDistributedKind, `{"apiVersion":"operator.victoriametrics.com/v1alpha1","kind":"VMDistributed","metadata":{"name":"d","ownerReferences":[{"apiVersion":"operator.victoriametrics.com/v1alpha1","kind":"VMDistributed","name":"d"},{"apiVersion":"operator.victoriametrics.com/v1alpha1","kind":"VMDistributed","name":"other"}]}}`},
		{vlDistributedKind, `{}`},
		{vlDistributedKind, `{"metadata":{"name":"d"},"spec":{"vmauth":{"name":"auth"},"zoneCommon":{"vlcluster":{"name":"c-%ZONE%"},"vlagent":{"name":"a-%ZONE%","spec":{"replicaCount":1}},"remoteWrite":{"sendTimeout":"1m"}},"zones":[{"name":"east"},{"name":"west","vlcluster":{"name":"w"},"vlagent":{"name":"wa"}}]},"status":{"lastAppliedSpec":{}}}`},
		{vlDistributedKind, `{"metadata":{"name":"d"},"spec":{"backendType":"VLSingle","zoneCommon":{"vlsingle":{"name":"s-%ZONE%"}},"zones":[{"name":"east"},{"name":"west","vlsingle":{"name":"w"}}],"paused":true}}`},
		{vlDistributedKind, `{"spec":"bad"}`},
		{vlDistributedKind, `{"apiVersion":"operator.victoriametrics.com/v1alpha1","kind":"VLDistributed","metadata":{"name":"d","ownerReferences":[{"apiVersion":"operator.victoriametrics.com/v1alpha1","kind":"VLDistributed","name":"d"},{"apiVersion":"operator.victoriametrics.com/v1alpha1","kind":"VLDistributed","name":"other"}]}}`},
	} {
		f.Add(append([]byte{byte(seed.kind)}, seed.data...))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}

		cr, parts := helperParts(distributedKind(data[0] % uint8(distributedKindCount)))
		if err := json.Unmarshal(data[1:], cr); err != nil {
			fuzztest.New(data[1:]).MaxDepth(8).Fill(cr)
		}

		// fresh filler for method arguments
		c := fuzztest.New(data)
		for _, p := range parts() {
			fuzztest.CallMethods(c, p, crArg(cr, p))
		}
		fuzztest.CallMethods(c, cr, crArg(cr, cr))
	})
}
