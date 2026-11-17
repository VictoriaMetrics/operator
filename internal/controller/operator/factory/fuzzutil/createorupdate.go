// Package fuzzutil holds helpers shared by fuzz tests of the factory packages.
package fuzzutil

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

// CreateOrUpdate fuzzes run against a fake client holding only RefObjects, panics are the only failure.
// Each seed runs with optional APIs (VPA, Gateway API) on and off.
func CreateOrUpdate[T client.Object](f *testing.F, seeds []string, newCR func() T, run func(context.Context, client.Client, T) error) {
	for _, seed := range seeds {
		f.Add([]byte(seed), true)
		f.Add([]byte(seed), false)
	}
	f.Fuzz(func(t *testing.T, data []byte, optionalAPIs bool) {
		cr := decodeCR(data, newCR)
		setOptionalAPIs(t, optionalAPIs)

		rclient := k8stools.GetTestClientWithObjects(RefObjects())
		build.AddDefaults(rclient.Scheme())
		rclient.Scheme().Default(cr)

		// fake clock lets rollout polling time out instantly
		synctest.Test(t, func(t *testing.T) {
			if err := run(t.Context(), rclient, cr.DeepCopyObject().(T)); err != nil {
				return
			}

			// second run over own result must be a no-op, e.g. no update flapping on unsorted maps
			c, actions := recordWrites(rclient)
			_ = run(t.Context(), c, cr.DeepCopyObject().(T))
			noWrites(t, *actions, "second CreateOrUpdate")
		})
	})
}

// recordWrites wraps c, so calls made through it are appended to the returned actions.
func recordWrites(c client.Client) (client.Client, *[]k8stools.ClientAction) {
	actions := &[]k8stools.ClientAction{}
	return interceptor.NewClient(c.(client.WithWatch), k8stools.NewActionRecordingInterceptor(actions, nil)), actions
}

// noWrites fails when actions hold anything but reads.
func noWrites(t testing.TB, actions []k8stools.ClientAction, what string) {
	var writes []string
	for _, a := range actions {
		if a.Verb != "Get" {
			writes = append(writes, fmt.Sprintf("%s %s %s", a.Verb, a.Kind, a.Resource))
		}
	}
	if len(writes) > 0 {
		t.Fatalf("%s after convergence wrote:\n%s", what, strings.Join(writes, "\n"))
	}
}

// decodeCR builds a CR from JSON data, or fills it from raw bytes when data isn't JSON.
func decodeCR[T client.Object](data []byte, newCR func() T) T {
	cr := newCR()
	if err := json.Unmarshal(data, cr); err != nil {
		fuzztest.New(data).Fill(cr)
	}
	cr.SetName(fuzzName)
	cr.SetNamespace(fuzzNamespace)
	return cr
}

// setOptionalAPIs toggles VPA and Gateway API support until the test ends.
func setOptionalAPIs(t *testing.T, on bool) {
	cfg := config.MustGetBaseConfig()
	defaultCfg := *cfg
	cfg.VPAAPIEnabled = on
	cfg.GatewayAPIEnabled = on
	t.Cleanup(func() { *cfg = defaultCfg })
}
