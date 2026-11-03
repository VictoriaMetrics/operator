package v1

import (
	"encoding/json"
	"reflect"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/randfill"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

var (
	clusterComponents = []vmv1beta1.ClusterComponent{
		vmv1beta1.ClusterComponentCommon, vmv1beta1.ClusterComponentRoot, vmv1beta1.ClusterComponentInsert,
		vmv1beta1.ClusterComponentSelect, vmv1beta1.ClusterComponentBalancer, vmv1beta1.ClusterComponentStorage,
	}
	clusterComponentType = reflect.TypeFor[vmv1beta1.ClusterComponent]()
)

type helperKind byte

const (
	helperVMAnomaly helperKind = iota
	helperVTAgent
	helperVTSingle
	helperVLAgent
	helperVLSingle
	helperVMAnomalyConfig
	helperVTCluster
	helperVLCluster
)

// helperTargets returns the CR and its non-nil component specs.
func helperTargets(selector byte) (any, func(any) []any) {
	targets := [...]struct {
		cr    any
		parts func(any) []any
	}{
		helperVMAnomaly:       {&VMAnomaly{}, nil},
		helperVTAgent:         {&VTAgent{}, nil},
		helperVTSingle:        {&VTSingle{}, nil},
		helperVLAgent:         {&VLAgent{}, nil},
		helperVLSingle:        {&VLSingle{}, nil},
		helperVMAnomalyConfig: {&VMAnomalyConfig{}, nil},
		helperVTCluster: {&VTCluster{}, func(cr any) []any {
			s := cr.(*VTCluster).Spec
			return []any{s.Insert, s.Select, s.Storage}
		}},
		helperVLCluster: {&VLCluster{}, func(cr any) []any {
			s := cr.(*VLCluster).Spec
			return []any{s.VLInsert, s.VLSelect, s.VLStorage}
		}},
	}
	t := targets[int(selector)%len(targets)]
	return t.cr, t.parts
}

// clusterComponentArg picks a supported ClusterComponent instead of a random string.
func clusterComponentArg(c *randfill.Filler) fuzztest.ArgFunc {
	return func(at reflect.Type) (reflect.Value, bool) {
		if at != clusterComponentType {
			return reflect.Value{}, false
		}
		var idx uint8
		c.Fill(&idx)
		return reflect.ValueOf(clusterComponents[int(idx)%len(clusterComponents)]), true
	}
}

// overHardLimits reports checks Validate enforces even with skip-validation annotation.
func overHardLimits(cr any) bool {
	v, ok := cr.(interface {
		client.Object
		Validate() error
	})
	if !ok {
		return false
	}

	c := v.DeepCopyObject().(interface {
		client.Object
		Validate() error
	})
	c.SetAnnotations(map[string]string{vmv1beta1.SkipValidationAnnotation: vmv1beta1.SkipValidationValue})
	return c.Validate() != nil
}

func FuzzCRHelpers(f *testing.F) {
	for _, seed := range []struct {
		kind helperKind
		data string
	}{
		{helperVMAnomaly, `{}`},
		{helperVMAnomaly, `{"spec":{"shardCount":2,"server":{"port":"8490"},"serviceAccountName":"sa"},"status":{"lastAppliedSpec":{}}}`},
		{helperVTAgent, `{"spec":{"extraArgs":{"tls":"true"},"serviceSpec":{"metadata":{"name":"s"}}}}`},
		{helperVTSingle, `{"spec":{"port":"10428","useProxyProtocol":true}}`},
		{helperVLAgent, `{"spec":{"k8sCollector":{"enabled":true},"syslogSpec":{"tcpListeners":[{"listenPort":514}]}}}`},
		{helperVLSingle, `{"spec":{"storage":{},"retentionPeriod":"1w"},"metadata":{"annotations":{"a":"b"}}}`},
		{helperVMAnomalyConfig, `{"spec":{"reader":{"queries":{"q":{"expr":"up"}}}}}`},
		{helperVTCluster, `{"spec":{"insert":{"replicaCount":1},"select":{"replicaCount":1},"storage":{"replicaCount":2,"maintenanceInsertNodeIDs":[1],"maintenanceSelectNodeIDs":[0]},"requestsLoadBalancer":{"enabled":true}}}`},
		{helperVLCluster, `{"spec":{"vlinsert":{"replicaCount":1},"vlselect":{"replicaCount":1},"vlstorage":{"replicaCount":3,"maintenanceInsertNodeIDs":[2],"maintenanceSelectNodeIDs":[1]},"requestsLoadBalancer":{"enabled":true}}}`},
		{helperVLCluster, `{"spec":{"vlinsert":{"extraArgs":{"tls":"true"}}},"status":{"lastAppliedSpec":{"vlstorage":{}}}}`},
	} {
		f.Add(append([]byte{byte(seed.kind)}, seed.data...))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}

		cr, parts := helperTargets(data[0])
		if err := json.Unmarshal(data[1:], cr); err != nil {
			fuzztest.New(data[1:]).MaxDepth(8).Fill(cr)
		}

		if overHardLimits(cr) {
			return
		}

		// fresh filler for method arguments
		c := fuzztest.New(data)
		arg := clusterComponentArg(c)
		fuzztest.CallMethods(c, cr, arg)
		if parts == nil {
			return
		}
		for _, p := range parts(cr) {
			fuzztest.CallMethods(c, p, arg)
		}
	})
}
