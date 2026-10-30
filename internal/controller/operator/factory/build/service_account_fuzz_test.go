package build

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzServiceAccountBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"cr":{"metadata":{"labels":{"team":"ops"},"annotations":{"owner":"operator"}},"spec":{"serviceAccountName":"external"}}}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"container":{"name":"main"}}`,
		`{"cr":{"spec":{"disableAutomountServiceAccountToken":true}},"container":{"volumeMounts":[{"name":"existing","mountPath":"/var/run/secrets/kubernetes.io/serviceaccount"}]},"volumes":[{"name":"kube-api-access","emptyDir":{}}]}`,
		`{"container":{"volumeMounts":[{"name":"data","mountPath":"/data"}]},"volumes":[{"name":"data","emptyDir":{}}],"automount":true}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var input struct {
			CR        vmv1beta1.VMSingle `json:"cr"`
			Container corev1.Container   `json:"container"`
			Volumes   []corev1.Volume    `json:"volumes"`
			Automount bool               `json:"automount"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			return
		}
		input.CR.Name, input.CR.Namespace = "single", "default"
		assertStableBuilder(t, func() (any, error) {
			container := input.Container.DeepCopy()
			AddServiceAccountTokenVolumeMount(container, input.Automount)
			volumes := AddServiceAccountTokenVolume(input.Volumes, &input.CR.Spec.CommonAppsParams)
			return struct {
				Account   *corev1.ServiceAccount
				Container *corev1.Container
				Volumes   []corev1.Volume
			}{ServiceAccount(&input.CR), container, volumes}, nil
		})
	})
}
