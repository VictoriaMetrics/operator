package build

import (
	"encoding/json"
	"testing"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func FuzzServiceBuilder(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"cr":{"spec":{"port":"8428"}}}`,
		`{"cr":{"spec":{"port":"http"}}}`,
		`{"cr":{"spec":{"port":"8428","serviceSpec":{"useAsDefault":true,"spec":{"ports":[{"name":"extra","port":9999}]}}}}}`,
		`{"cr":{"spec":{"port":"8428","serviceSpec":{"name":"custom","useAsDefault":true,"spec":{"type":"ClusterIP","ports":[{"name":"http","port":9000}],"selector":{"custom":"value"}}}}}}`,
		`{"cr":{"spec":{"port":"8428","serviceSpec":{"spec":{"ports":[]}}}}}`,
		`{"cr":{"spec":{"port":"8428","serviceSpec":{"useAsDefault":true,"labels":{"a":"b"},"spec":{"sessionAffinity":"ClientIP"}}}},"headless":true}`,
		`{"cr":{"spec":{"port":"8428","serviceSpec":{"useAsDefault":true,"spec":{"type":"NodePort"}}}},"headless":true,"nonHeadless":true}`,
		`{"cr":{"spec":{"port":"8428","serviceSpec":{"useAsDefault":true,"spec":{}}}},"headless":true,"nonHeadless":true}`,
		`{"cr":{"spec":{"port":"8428","serviceSpec":{"useAsDefault":true,"spec":{"clusterIP":"10.0.0.1"}}}},"headless":true}`,
		`{"cr":{"metadata":{"labels":{"app":"vm"}},"spec":{"serviceSpec":{"labels":{"environment":"test"},"annotations":{"owner":"operator"},"spec":{"ports":[{"name":"metrics","port":8428}]}}}}}`,
		`{"cr":{"metadata":{"labels":{"app":"vm"}},"spec":{"port":"8428","serviceSpec":{"labels":{"app":"override"}}}}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var cr vmv1beta1.VMSingle
		if err := json.Unmarshal(data, &cr); err != nil {
			fuzztest.New(data).Fill(&cr)
		}
		svc := Service(&cr, cr.Spec.Port, nil)
		AdditionalServiceFromDefault(svc, cr.Spec.ServiceSpec)
	})
}

func FuzzPolicyBuilders(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		// empty rules still produce no policy types
		`{"networkPolicy":{"ingress":[{}],"egress":[{}]}}`,
		`{"networkPolicy":{"ingress":[{"from":[{"podSelector":{"matchLabels":{"a":"b"}}}],"ports":[{"port":8428}]}]}}`,
		`{"networkPolicy":{"egress":[{"to":[{"ipBlock":{"cidr":"10.0.0.0/8"}}]}]}}`,
		`{"pdb":{"minAvailable":1}}`,
		`{"pdb":{"maxUnavailable":"50%"}}`,
		`{"pdb":{"minAvailable":1,"selectorLabels":{"app":"custom"},"unhealthyPodEvictionPolicy":"AlwaysAllow"}}`,
		`{"pdb":{"unhealthyPodEvictionPolicy":"IfHealthyBudget"}}`,
		`{"hpa":{"minReplicas":1,"maxReplicas":3}}`,
		`{"hpa":{"maxReplicas":5,"metrics":[{"type":"Resource","resource":{"name":"cpu","target":{"type":"Utilization","averageUtilization":80}}}],"behaviour":{"scaleDown":{"stabilizationWindowSeconds":60}}}}`,
		`{"vpa":{"updatePolicy":{"updateMode":"Auto"}}}`,
		`{"vpa":{"resourcePolicy":{"containerPolicies":[{"containerName":"*","mode":"Off"}]},"recommenders":[{"name":"custom"}]}}`,
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
		hpaTarget := autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "single", APIVersion: "apps/v1"}
		vpaTarget := autoscalingv1.CrossVersionObjectReference{Kind: "Deployment", Name: "single", APIVersion: "apps/v1"}
		NetworkPolicy(cr, &input.NetworkPolicy)
		PodDisruptionBudget(cr, &input.PDB)
		HPA(cr, hpaTarget, &input.HPA)
		VPA(cr, vpaTarget, &input.VPA)
	})
}
