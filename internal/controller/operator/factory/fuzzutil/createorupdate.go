// Package fuzzutil holds helpers shared by fuzz tests of the factory packages.
package fuzzutil

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	fuzzName      = "fuzz"
	fuzzNamespace = "default"
)

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

		rclient := k8stools.GetTestClientWithObjects(nil)
		build.AddDefaults(rclient.Scheme())
		rclient.Scheme().Default(cr)

		// fake clock lets rollout polling time out instantly
		synctest.Test(t, func(t *testing.T) {
			_ = run(t.Context(), rclient, cr)
		})
	})
}
