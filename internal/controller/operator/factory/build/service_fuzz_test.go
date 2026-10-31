package build

import (
	"encoding/json"
	"testing"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func FuzzServiceBuilder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"spec":{"port":"8428"}}`,
		`{"spec":{"port":"8428","serviceSpec":{"useAsDefault":true,"spec":{"ports":[{"name":"extra","port":9999}]}}}}`,
		`{"spec":{"port":"8428","serviceSpec":{"name":"custom","useAsDefault":true,"spec":{"type":"ClusterIP","ports":[{"name":"http","port":9000}],"selector":{"custom":"value"}}}}}`,
		`{"metadata":{"labels":{"app":"vm"}},"spec":{"serviceSpec":{"labels":{"environment":"test"},"annotations":{"owner":"operator"},"spec":{"ports":[{"name":"metrics","port":8428}]}}}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var cr vmv1beta1.VMSingle
		if err := json.Unmarshal(data, &cr); err != nil {
			fuzztest.New(data).Fill(&cr)
		}
		cr.Name, cr.Namespace = "single", "default"
		assertStableBuilder(t, func() (any, error) {
			svc := Service(&cr, cr.Spec.Port, nil)
			return struct {
				Default    *corev1.Service
				Additional *corev1.Service
			}{svc, AdditionalServiceFromDefault(svc, cr.Spec.ServiceSpec)}, nil
		})
	})
}

func FuzzPolicyBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"networkPolicy":{"ingress":[{}],"egress":[{}]}}`,
		`{"pdb":{"minAvailable":1,"selectorLabels":{"app":"custom"},"unhealthyPodEvictionPolicy":"AlwaysAllow"}}`,
		`{"hpa":{"minReplicas":1,"maxReplicas":3},"vpa":{"updatePolicy":{"updateMode":"Auto"}}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			NetworkPolicy vmv1beta1.EmbeddedNetworkPolicy           `json:"networkPolicy"`
			PDB           vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"pdb"`
			HPA           vmv1beta1.EmbeddedHPA                     `json:"hpa"`
			VPA           vmv1beta1.EmbeddedVPA                     `json:"vpa"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		cr := &vmv1beta1.VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "single", Namespace: "default"}}
		assertStableBuilder(t, func() (any, error) {
			return []any{
				NetworkPolicy(cr, &input.NetworkPolicy),
				PodDisruptionBudget(cr, &input.PDB),
				HPA(cr, autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "single", APIVersion: "apps/v1"}, &input.HPA),
				VPA(cr, autoscalingv1.CrossVersionObjectReference{Kind: "Deployment", Name: "single", APIVersion: "apps/v1"}, &input.VPA),
			}, nil
		})
	})
}
