package vmscrapes

import (
	"bytes"
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v2"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func FuzzRelabelConfig(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"sourceLabels":["job"],"targetLabel":"service","regex":"(.*)"}`,
		`{"source_labels":["__name__"],"target_label":"metric","action":"replace"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var rc vmv1beta1.RelabelConfig
		if json.Unmarshal(data, &rc) != nil {
			return
		}
		a, err := yaml.Marshal(generateRelabelConfig(&rc))
		if err != nil {
			t.Fatal(err)
		}
		b, err := yaml.Marshal(generateRelabelConfig(&rc))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent relabel config: %q != %q", a, b)
		}
	})
}

func FuzzStaticScrapeConfig(f *testing.F) {
	for _, seed := range []string{`{"targets":["localhost:8428"]}`, `{"targets":["host:9100"],"labels":{"job":"node"},"relabelConfigs":[{"targetLabel":"instance","replacement":"node"}]}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var ep vmv1beta1.TargetEndpoint
		if json.Unmarshal(data, &ep) != nil {
			return
		}
		render := func() ([]byte, error) {
			copy := ep.DeepCopy()
			cfg, err := generateStaticScrapeConfig(t.Context(), &vmv1beta1.CommonScrapeParams{}, &vmv1beta1.VMStaticScrape{}, copy, 0, build.NewAssetsCache(t.Context(), nil, nil))
			if err != nil {
				return nil, err
			}
			return yaml.Marshal(cfg)
		}
		a, err := render()
		b, err2 := render()
		if (err == nil) != (err2 == nil) || !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent static scrape: %q (%v) != %q (%v)", a, err, b, err2)
		}
	})
}
