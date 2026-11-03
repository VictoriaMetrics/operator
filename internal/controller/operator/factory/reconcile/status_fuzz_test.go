package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const fuzzParent = "fuzz.default.vmalert"

// UpdateObjectStatus panics on other statuses
var fuzzUpdateStatuses = []vmv1beta1.UpdateStatus{
	vmv1beta1.UpdateStatusExpanding,
	vmv1beta1.UpdateStatusOperational,
	vmv1beta1.UpdateStatusIgnored,
	vmv1beta1.UpdateStatusPaused,
	vmv1beta1.UpdateStatusFailed,
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

type statusInput struct {
	Rules    []vmv1beta1.VMRule `json:"rules"`
	Selected uint8              `json:"selected"`
	Agent    vmv1beta1.VMAgent  `json:"agent"`
	Status   uint8              `json:"status"`
	Err      string             `json:"err"`
	Pods     []corev1.Pod       `json:"pods"`
	Hash     uint32             `json:"hash"`
	Body     string             `json:"body"`
	Code     int                `json:"code"`
}

func FuzzStatus(f *testing.F) {
	rule := `{"metadata":{"name":"r","generation":2},"status":{"updateStatus":"%s","observedGeneration":1,"conditions":[` +
		`{"type":"fuzz.default.vmalert.victoriametrics.com/Applied","status":"False","reason":"ConfigParsedAndApplied","message":"bad","lastUpdateTime":"2020-01-01T00:00:00Z"},` +
		`{"type":"other.default.vmagent.victoriametrics.com/Applied","status":"True","lastUpdateTime":"2020-01-01T00:00:00Z"},` +
		`{"type":"gone.default.vmagent.victoriametrics.com/Applied","status":"False","message":"old"}]}}`
	pod := `{"metadata":{"name":"p%d"},"status":{"podIP":"10.0.0.%d","phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`
	seeds := []string{
		`{}`,
		`{"rules":[{}],"selected":1}`,
		fmt.Sprintf(`{"rules":[%s,{"metadata":{"name":"b"}}],"selected":1,"status":4,"err":"boom"}`, fmt.Sprintf(rule, "failed")),
		fmt.Sprintf(`{"rules":[%s],"selected":0,"status":1}`, fmt.Sprintf(rule, "operational")),
		fmt.Sprintf(`{"agent":{"metadata":{"name":"a","generation":3},"status":{"updateStatus":"failed","reason":"x"}},"status":3,"pods":[%s,%s],"hash":42,"body":"configreloader_reload_content_hash{key=\"main\"} 42\n"}`,
			fmt.Sprintf(pod, 0, 1), fmt.Sprintf(pod, 1, 2)),
		fmt.Sprintf(`{"pods":[%s],"hash":1,"body":"configreloader_reload_content_hash{key=\"main\"} 2\n"}`, fmt.Sprintf(pod, 0, 1)),
		fmt.Sprintf(`{"pods":[%s],"body":"broken{","code":500}`, fmt.Sprintf(pod, 0, 1)),
		`{"pods":[{"metadata":{"name":"p"},"status":{"podIP":""}}]}`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var in statusInput
		if err := json.Unmarshal(data, &in); err != nil {
			fuzztest.New(data).Fill(&in)
		}

		// stub config-reloader metrics endpoint
		origTransport := http.DefaultTransport
		t.Cleanup(func() { http.DefaultTransport = origTransport })
		http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: in.Code, Body: io.NopCloser(strings.NewReader(in.Body))}, nil
		})
		if in.Code == 0 {
			in.Code = http.StatusOK
		}

		agent := in.Agent.DeepCopy()
		agent.Name, agent.Namespace = "fuzz", "default"
		agent.ResourceVersion, agent.ManagedFields, agent.DeletionTimestamp = "", nil, nil
		objs := []runtime.Object{agent.DeepCopy()}
		rules := make([]*vmv1beta1.VMRule, 0, len(in.Rules))
		for i := range in.Rules {
			r := in.Rules[i].DeepCopy()
			r.Name, r.Namespace = fmt.Sprintf("r%d", i), "default"
			r.ResourceVersion, r.ManagedFields, r.DeletionTimestamp, r.Finalizers = "", nil, nil, nil
			rules = append(rules, r)
			objs = append(objs, r.DeepCopy())
		}
		for i := range in.Pods {
			p := in.Pods[i].DeepCopy()
			p.Name, p.Namespace, p.Labels = fmt.Sprintf("p%d", i), "default", agent.SelectorLabels()
			p.ResourceVersion, p.ManagedFields, p.DeletionTimestamp, p.Finalizers = "", nil, nil, nil
			objs = append(objs, p)
		}
		var maybeErr error
		if in.Err != "" {
			maybeErr = errors.New(in.Err)
		}

		synctest.Test(t, func(t *testing.T) {
			// bound polling under fake clock
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			rclient := k8stools.GetTestClientWithObjects(objs)

			_ = StatusForChildObjects(ctx, rclient, fuzzParent, rules[:int(in.Selected)%(len(rules)+1)])
			for _, r := range rules {
				_ = StatusForChildObject(ctx, rclient, fuzzParent, r)
				_ = SyncConfigObjectStatus(ctx, rclient, r, maybeErr)
			}
			_ = SyncConfigObjectStatus(ctx, rclient, &vmv1beta1.VMRule{}, nil)
			_ = rclient.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, agent)
			_ = UpdateObjectStatus(ctx, rclient, agent, fuzzUpdateStatuses[int(in.Status)%len(fuzzUpdateStatuses)], maybeErr)
			_ = WaitForConfigReloadHash(ctx, rclient, agent, in.Hash)
		})
	})
}
