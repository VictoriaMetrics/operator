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

	// unparseable tag: addFeatureFlags cannot determine support for any feature,
	// so none are emitted (pre-existing fallback, unrelated to eventRecorder)
	f(opts{
		cr:   newCR("latest", &vmv1beta1.VMAlertmanagerEventRecorder{StdoutOutput: true}),
		want: nil,
	})
}

func Test_eventRecorderSupported(t *testing.T) {
	newCR := func(tag string) *vmv1beta1.VMAlertmanager {
		return &vmv1beta1.VMAlertmanager{
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					Image: vmv1beta1.Image{Tag: tag},
				},
			},
		}
	}

	assert.False(t, eventRecorderSupported(newCR("v0.32.0")))
	assert.True(t, eventRecorderSupported(newCR("v0.33.0")))
	assert.True(t, eventRecorderSupported(newCR("v0.34.1")))
	assert.True(t, eventRecorderSupported(newCR("latest")), "unparseable tag must be treated as supported")
}
