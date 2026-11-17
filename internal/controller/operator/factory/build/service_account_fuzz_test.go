package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzServiceAccountBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"container":{"volumeMounts":[{"name":"data","mountPath":"/data"}]},"volumes":[{"name":"data","emptyDir":{}}]}`,
		`{"cr":{"metadata":{"labels":{"team":"ops"},"annotations":{"owner":"operator"}},"spec":{"serviceAccountName":"external"}}}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"container":{"name":"main"}}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"container":{"volumeMounts":[{"name":"existing","mountPath":"/var/run/secrets/kubernetes.io/serviceaccount"}]},"volumes":[{"name":"kube-api-access","emptyDir":{}}]}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"container":{"volumeMounts":[{"name":"custom","mountPath":"/var/run/secrets/kubernetes.io/serviceaccount","readOnly":false}]}}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"container":{"volumeMounts":[{"name":"kube-api-access","mountPath":"/other"}]}}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"volumes":[{"name":"kube-api-access","secret":{"secretName":"token"}}]}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"container":{"volumeMounts":[{"name":"a","mountPath":"/a"},{"name":"b","mountPath":"/b"}]},"volumes":[{"name":"a","emptyDir":{}},{"name":"b","emptyDir":{}}]}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"volumes":[{"name":"kube-api-access","emptyDir":{}},{"name":"kube-api-access","emptyDir":{}}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			CR        vmv1beta1.VMSingle `json:"cr"`
			Container corev1.Container   `json:"container"`
			Volumes   []corev1.Volume    `json:"volumes"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		input.CR.Name, input.CR.Namespace = "single", "default"

		// automount is derived from the CR, as callers do
		automount := input.CR.AutomountServiceAccountToken()
		params := &input.CR.Spec.CommonAppsParams
		AddServiceAccountTokenVolumeMount(&input.Container, automount)
		AddServiceAccountTokenVolume(input.Volumes, params)
		ServiceAccount(&input.CR)
	})
}
