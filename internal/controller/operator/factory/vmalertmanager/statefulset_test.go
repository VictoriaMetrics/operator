package vmalertmanager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func Test_addFeatureFlags(t *testing.T) {
	type opts struct {
		cr   *vmv1beta1.VMAlertmanager
		want []string
	}
	f := func(o opts) {
		t.Helper()
		assert.Equal(t, o.want, addFeatureFlags(nil, o.cr))
	}

	newCR := func(tag string, eventRecorder *vmv1beta1.VMAlertmanagerEventRecorder) *vmv1beta1.VMAlertmanager {
		return &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{Name: "test-am", Namespace: "default"},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					Image: vmv1beta1.Image{Tag: tag},
				},
				EventRecorder: eventRecorder,
			},
		}
	}

	// version below utf8MinVersion, no eventRecorder: no flags
	f(opts{
		cr:   newCR("v0.27.0", nil),
		want: nil,
	})

	// version at utf8MinVersion, no eventRecorder: utf8-strict-mode only
	f(opts{
		cr:   newCR("v0.28.0", nil),
		want: []string{"--enable-feature=utf8-strict-mode"},
	})

	// eventRecorder configured, version below eventRecorderMinVersion: event-recorder must not
	// be emitted, since that alertmanager build doesn't understand the flag and would fail to start
	f(opts{
		cr:   newCR("v0.28.0", &vmv1beta1.VMAlertmanagerEventRecorder{StdoutOutput: true}),
		want: []string{"--enable-feature=utf8-strict-mode"},
	})

	// eventRecorder configured, version at eventRecorderMinVersion: both features
	f(opts{
		cr:   newCR("v0.33.0", &vmv1beta1.VMAlertmanagerEventRecorder{StdoutOutput: true}),
		want: []string{"--enable-feature=utf8-strict-mode,event-recorder"},
	})

	// unparseable tag, no eventRecorder: utf8-strict-mode cannot be determined, nothing is emitted
	f(opts{
		cr:   newCR("latest", nil),
		want: nil,
	})

	// unparseable tag with eventRecorder: treated as the newest version, so event-recorder is emitted
	f(opts{
		cr:   newCR("latest", &vmv1beta1.VMAlertmanagerEventRecorder{StdoutOutput: true}),
		want: []string{"--enable-feature=event-recorder"},
	})

	// prerelease of eventRecorderMinVersion
	f(opts{
		cr:   newCR("v0.33.0-rc.0", &vmv1beta1.VMAlertmanagerEventRecorder{StdoutOutput: true}),
		want: []string{"--enable-feature=utf8-strict-mode,event-recorder"},
	})
}

func Test_alertmanagerAtLeast(t *testing.T) {
	newCR := func(tag string) *vmv1beta1.VMAlertmanager {
		return &vmv1beta1.VMAlertmanager{
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					Image: vmv1beta1.Image{Tag: tag},
				},
			},
		}
	}

	assert.False(t, alertmanagerAtLeast(newCR("v0.32.0"), eventRecorderMinVersion))
	assert.True(t, alertmanagerAtLeast(newCR("v0.33.0"), eventRecorderMinVersion))
	assert.True(t, alertmanagerAtLeast(newCR("v0.34.1"), eventRecorderMinVersion))
	assert.True(t, alertmanagerAtLeast(newCR("v0.33.0-rc.0"), eventRecorderMinVersion), "prerelease must be compared by its release version")
	assert.True(t, alertmanagerAtLeast(newCR("v0.34.0@sha256:abc"), stdoutOutputMinVersion), "digest must be ignored")
	assert.False(t, alertmanagerAtLeast(newCR("v0.33.1"), stdoutOutputMinVersion))
	assert.True(t, alertmanagerAtLeast(newCR("latest"), stdoutOutputMinVersion), "unparseable tag must be treated as the newest version")
}
