package podutil

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	fuzzMetric  = "vmagent_remotewrite_pending_data_bytes"
	fuzzBackend = "http://backend"
)

func fill(data []byte, v any) {
	if err := json.Unmarshal(data, v); err != nil {
		fuzztest.New(data).Fill(v)
	}
}

func FuzzMergeSpecs(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"name":"a","a":{"vmstorage":{"replicaCount":2,"extraArgs":{"zone":"%ZONE%"}}},"b":{"vmstorage":{"replicaCount":3},"vmselect":{}}}`))
	f.Add([]byte(`{"name":"\"","a":{"retentionPeriod":"%ZONE%"},"b":{"retentionPeriod":"1"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var in struct {
			Name string                  `json:"name"`
			A    vmv1beta1.VMClusterSpec `json:"a"`
			B    vmv1beta1.VMClusterSpec `json:"b"`
		}
		fill(data, &in)
		_, _ = MergeSpecs(&in.A, &in.B, in.Name)
	})
}

func FuzzVMAuthTargetRef(f *testing.F) {
	owner := `{"apiVersion":"v1","kind":"K","name":"o"}`
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"write":true,"owner":` + owner + `,"exclude":[0,5,-1],"paths":["/"],"objects":[{"metadata":{"name":"a","namespace":"ns","creationTimestamp":"2024-01-01T00:00:00Z","ownerReferences":[` + owner + `]}},{"metadata":{"name":"b"}},{"metadata":{"name":"c","creationTimestamp":"2024-01-01T00:00:00Z","ownerReferences":[` + owner + `]}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var in struct {
			Write   bool                  `json:"write"`
			Owner   metav1.OwnerReference `json:"owner"`
			Exclude []int                 `json:"exclude"`
			Paths   []string              `json:"paths"`
			Objects []vmv1beta1.VMAgent   `json:"objects"`
		}
		fill(data, &in)
		objs := make([]metav1.Object, len(in.Objects))
		for i := range in.Objects {
			objs[i] = &in.Objects[i]
		}
		if in.Write {
			_ = VMAuthWriteTargetRef(objs, "VMAgent", in.Paths, &in.Owner, in.Exclude...)
			return
		}
		_ = VMAuthReadTargetRef(objs, "VMCluster/vmselect", in.Paths, &in.Owner, in.Exclude...)
	})
}

func FuzzWaitForEmptyPQ(f *testing.F) {
	slice := func(addrType, ready, addrs string) string {
		return `{"metadata":{"name":"s` + addrType + `","labels":{"kubernetes.io/service-name":"vmagent-fuzz"}},"addressType":"` + addrType +
			`","ports":[{"name":"http","port":8429},{"name":"other","port":1}],"endpoints":[{"addresses":` + addrs + `,"conditions":{"ready":` + ready + `}}]}`
	}
	metrics := func(v string) string {
		b, _ := json.Marshal(fuzzMetric + `{url="` + fuzzBackend + `"} ` + v + "\n" + fuzzMetric + `{url="other"} 5`)
		return string(b)
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"body":` + metrics("0") + `,"slices":[` + slice("IPv4", "true", `["10.0.0.1",""]`) + `,` + slice("IPv6", "true", `["::1"]`) + `]}`))
	f.Add([]byte(`{"body":` + metrics("10") + `,"slices":[` + slice("IPv4", "false", `["10.0.0.1"]`) + `,` + slice("FQDN", "null", `["a.b"]`) + `]}`))
	f.Add([]byte(`{"body":"bad{","status":500,"slices":[` + slice("IPv4", "true", `["10.0.0.1"]`) + `,{"metadata":{"name":"noport","labels":{"kubernetes.io/service-name":"vmagent-fuzz"}},"addressType":"IPv4"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var in struct {
			Body   string                      `json:"body"`
			Status int                         `json:"status"`
			Agent  vmv1beta1.VMAgentSpec       `json:"agent"`
			Slices []discoveryv1.EndpointSlice `json:"slices"`
		}
		fill(data, &in)
		var objs []runtime.Object
		for i := range in.Slices {
			s := &in.Slices[i]
			s.Namespace = "default"
			s.ResourceVersion = ""
			s.ManagedFields = nil
			s.DeletionTimestamp = nil
			s.Finalizers = nil
			if s.Name == "" {
				continue
			}
			objs = append(objs, s)
		}
		status := in.Status
		if status < 100 || status > 999 {
			status = http.StatusOK
		}
		agent := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "fuzz", Namespace: "default", CreationTimestamp: metav1.Now()},
			Spec:       in.Agent,
		}
		opts := WaitForEmptyPQOpts{
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(in.Body))}, nil
			})},
			MetricName:     fuzzMetric,
			Dimension:      "url",
			CheckInterval:  time.Second,
			MatchesBackend: func(l, b string) bool { return l == b },
		}
		synctest.Test(t, func(t *testing.T) {
			rclient := k8stools.GetTestClientWithObjects(objs)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			WaitForEmptyPQ(ctx, rclient, []*vmv1beta1.VMAgent{agent, {}}, fuzzBackend, opts, 5*time.Second)
		})
	})
}
