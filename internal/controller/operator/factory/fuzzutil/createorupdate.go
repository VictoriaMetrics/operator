// Package fuzzutil holds helpers shared by fuzz tests of the factory packages.
package fuzzutil

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	fuzzName      = "fuzz"
	fuzzNamespace = "default"

	// RefName and RefKey name the predefined Secret and ConfigMap seeds can reference.
	RefName = "ref"
	RefKey  = "key"

	// refValue is a URL, so URL-typed references parse.
	refValue = "http://ref"
)

// RefObjects are predefined objects, so seeds can reach paths reading referenced assets.
func RefObjects() []runtime.Object {
	meta := metav1.ObjectMeta{Name: RefName, Namespace: fuzzNamespace}
	return []runtime.Object{
		&corev1.Secret{ObjectMeta: meta, Data: map[string][]byte{RefKey: []byte(refValue)}},
		&corev1.ConfigMap{ObjectMeta: meta, Data: map[string]string{RefKey: refValue}},
	}
}

// CreateOrUpdate fuzzes run against an empty fake client, panics are the only failure.
// Each seed runs with optional APIs (VPA, Gateway API) on and off.
func CreateOrUpdate[T client.Object](f *testing.F, seeds []string, newCR func() T, run func(context.Context, client.Client, T) error) {
	for _, seed := range seeds {
		f.Add([]byte(seed), true)
		f.Add([]byte(seed), false)
	}
	f.Fuzz(func(t *testing.T, data []byte, optionalAPIs bool) {
		cr := newCR()
		if err := json.Unmarshal(data, cr); err != nil {
			fuzztest.New(data).Fill(cr)
		}
		cr.SetName(fuzzName)
		cr.SetNamespace(fuzzNamespace)

		cfg := config.MustGetBaseConfig()
		defaultCfg := *cfg
		cfg.VPAAPIEnabled = optionalAPIs
		cfg.GatewayAPIEnabled = optionalAPIs
		t.Cleanup(func() { *cfg = defaultCfg })

		rclient := k8stools.GetTestClientWithObjects(RefObjects())
		build.AddDefaults(rclient.Scheme())
		rclient.Scheme().Default(cr)

		// fake clock lets rollout polling time out instantly
		synctest.Test(t, func(t *testing.T) {
			_ = run(t.Context(), rclient, cr)
		})
	})
}
