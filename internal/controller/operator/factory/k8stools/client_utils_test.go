package k8stools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestMergePatchContainers(t *testing.T) {
	base := []corev1.Container{
		{Name: "app"},
	}
	patches := []corev1.Container{
		{
			Name: "sidecar",
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromString("offloader")},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       30,
			},
		},
	}
	out, err := MergePatchContainers(base, patches)
	assert.NoError(t, err)
	assert.Len(t, out, 2)

	sidecar := out[1]
	assert.Equal(t, "sidecar", sidecar.Name)
	assert.Equal(t, int32(30), sidecar.LivenessProbe.PeriodSeconds, "explicit value must not be overwritten")
	assert.Equal(t, int32(1), sidecar.LivenessProbe.TimeoutSeconds)
	assert.Equal(t, int32(1), sidecar.LivenessProbe.SuccessThreshold)
	assert.Equal(t, int32(3), sidecar.LivenessProbe.FailureThreshold)

	app := out[0]
	assert.Equal(t, "app", app.Name)
	assert.Nil(t, app.LivenessProbe)
	assert.Nil(t, app.ReadinessProbe)
	assert.Nil(t, app.StartupProbe)
}

func TestMergePatchContainers_PatchedContainerProbeDefaulted(t *testing.T) {
	base := []corev1.Container{
		{
			Name: "app",
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt(8080)},
				},
			},
		},
	}
	patches := []corev1.Container{
		{
			Name:  "app",
			Image: "custom-image:tag",
		},
	}
	out, err := MergePatchContainers(base, patches)
	assert.NoError(t, err)
	assert.Len(t, out, 1)
	assert.Equal(t, "custom-image:tag", out[0].Image)
	assert.Equal(t, int32(1), out[0].ReadinessProbe.TimeoutSeconds)
	assert.Equal(t, int32(10), out[0].ReadinessProbe.PeriodSeconds)
	assert.Equal(t, int32(1), out[0].ReadinessProbe.SuccessThreshold)
	assert.Equal(t, int32(3), out[0].ReadinessProbe.FailureThreshold)
}

func TestMergePatchContainers_DoesNotMutateCallerOwnedProbes(t *testing.T) {
	base := []corev1.Container{
		{
			Name: "app",
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt(8080)},
				},
			},
		},
	}
	patches := []corev1.Container{
		{
			Name: "sidecar",
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromString("offloader")},
				},
			},
		},
	}
	out, err := MergePatchContainers(base, patches)
	assert.NoError(t, err)
	assert.Len(t, out, 2)
	assert.NotSame(t, base[0].LivenessProbe, out[0].LivenessProbe)
	assert.NotSame(t, patches[0].LivenessProbe, out[1].LivenessProbe)
	assert.Zero(t, base[0].LivenessProbe.TimeoutSeconds)
	assert.Zero(t, patches[0].LivenessProbe.TimeoutSeconds)
}

func TestMergePatchContainers_AlreadySetProbeFieldsPreserved(t *testing.T) {
	base := []corev1.Container{
		{
			Name: "app",
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt(8080)},
				},
				PeriodSeconds:    5,
				FailureThreshold: 10,
				TimeoutSeconds:   7,
				SuccessThreshold: 1,
			},
		},
	}
	out, err := MergePatchContainers(base, nil)
	assert.NoError(t, err)
	assert.Equal(t, int32(5), out[0].LivenessProbe.PeriodSeconds)
	assert.Equal(t, int32(10), out[0].LivenessProbe.FailureThreshold)
	assert.Equal(t, int32(7), out[0].LivenessProbe.TimeoutSeconds)
	assert.Equal(t, int32(1), out[0].LivenessProbe.SuccessThreshold)
}
