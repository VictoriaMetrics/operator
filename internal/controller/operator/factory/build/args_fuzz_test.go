package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzLicenseBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"license":{"key":"enterprise-key"},"args":["-storageDataPath=/data"]}`,
		`{"license":{"keyRef":{"name":"license-secret","key":"license"},"forceOffline":true,"reloadInterval":"1h"},"mountDir":"/etc/licenses"}`,
		`{"license":{"key":"key","keyRef":{"name":"secret","key":"key"}},"volumes":[{"name":"extra","emptyDir":{}}],"mounts":[{"name":"extra","mountPath":"/extra"}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			License  *vmv1beta1.License   `json:"license"`
			Args     []string             `json:"args"`
			MountDir string               `json:"mountDir"`
			Volumes  []corev1.Volume      `json:"volumes"`
			Mounts   []corev1.VolumeMount `json:"mounts"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		LicenseVolumeTo(input.Volumes, input.Mounts, input.License, input.MountDir)
		LicenseArgsTo(input.Args, input.License, input.MountDir)
		LicenseDoubleDashArgsTo(input.Args, input.License, input.MountDir)
	})
}

func FuzzStreamAggrRelabelArgs(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"streamAggr":[{"dedupInterval":"1m","dropInputLabels":["pod","instance"]}]}`,
		`{"streamAggr":[{"rules":[{"match":"up","interval":"1m","outputs":["total"]}],"keepInput":true},{"dedupInterval":"30s"}]}`,
		`{"relabel":[{"inlineRelabelConfig":[{"targetLabel":"job","replacement":"vmagent"}]},null,{"relabelConfig":{"name":"relabel","key":"rules.yaml"}}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Args       []string                         `json:"args"`
			StreamAggr []*vmv1beta1.StreamAggrConfig    `json:"streamAggr"`
			Relabel    []*vmv1beta1.CommonRelabelParams `json:"relabel"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		streamKeys := make([]string, len(input.StreamAggr))
		for i := range streamKeys {
			streamKeys[i] = "stream.yaml"
		}
		relabelKeys := make([]string, len(input.Relabel))
		for i := range relabelKeys {
			relabelKeys[i] = "relabel.yaml"
		}
		StreamAggrArgsTo(input.Args, "streamAggr", streamKeys, input.StreamAggr...)
		RelabelArgsTo(input.Args, "remoteWrite.relabelConfig", relabelKeys, input.Relabel...)
	})
}

func FuzzGzipConfig(f *testing.F) {
	valid, err := GzipConfig([]byte("global:\n  scrape_interval: 30s\n"))
	if err != nil {
		f.Fatal(err)
	}
	empty, err := GzipConfig(nil)
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		nil,
		[]byte("plain text"),
		valid,
		empty,
		valid[:len(valid)/2],
		append(append([]byte{}, valid...), "trailing"...),
		{0x1f, 0x8b},
		{0x1f, 0x8b, 0x08, 0x00},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		GunzipConfig(data)
		GzipConfig(data)
	})
}
