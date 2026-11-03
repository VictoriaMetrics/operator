package v1

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/randfill"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

const unsupportedKindPanic = "BUG unsupported"

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

// callMethods invokes every exported method of v with fuzzed arguments: UnmarshalJSON, DeepCopy etc
func callMethods(c *randfill.Filler, v any) {
	rv := reflect.ValueOf(v)
	if rv.IsNil() {
		return
	}

	for i := range rv.NumMethod() {
		m := rv.Method(i)
		args, ok := fuzzArgs(c, m.Type())
		if !ok {
			continue
		}
		callMethod(m, args)
	}
}

// callMethod ignores intentional panics on unsupported cluster components.
func callMethod(m reflect.Value, args []reflect.Value) {
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok && strings.HasPrefix(s, unsupportedKindPanic) {
				return
			}
			panic(r)
		}
	}()
	m.Call(args)
}

// fuzzArgs fills method arguments; skips methods taking interfaces, funcs or nil-unsafe pointers.
func fuzzArgs(c *randfill.Filler, mt reflect.Type) ([]reflect.Value, bool) {
	args := make([]reflect.Value, mt.NumIn())
	for i := range args {
		at := mt.In(i)
		switch {
		case at == clusterComponentType:
			var idx uint8
			c.Fill(&idx)
			args[i] = reflect.ValueOf(clusterComponents[int(idx)%len(clusterComponents)])
		case at.Kind() == reflect.Interface, at.Kind() == reflect.Func, at.Kind() == reflect.Chan:
			return nil, false
		case at.Kind() == reflect.Pointer:
			p := reflect.New(at.Elem())
			c.Fill(p.Interface())
			args[i] = p
		default:
			p := reflect.New(at)
			c.Fill(p.Interface())
			args[i] = p.Elem()
		}
	}
	return args, true
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
		callMethods(c, cr)
		if parts == nil {
			return
		}
		for _, p := range parts(cr) {
			callMethods(c, p)
		}
	})
}
