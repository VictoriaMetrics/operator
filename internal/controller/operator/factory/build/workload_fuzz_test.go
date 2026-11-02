package build

import (
	"encoding/json"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

// workloadTarget describes a builder under test and how to wrap a pod template into its initial workload JSON.
type workloadTarget[T any] struct {
	build func(*T, *vmv1beta1.CommonAppsParams)
	wrap  string
}

const (
	workloadPodTemplate = `{"metadata":{"labels":{"app":"existing"},"annotations":{"owner":"team"}},"spec":{"containers":[{"name":"main","image":"example:latest"}]}}`
	workloadAutomount   = `{"spec":{"containers":[{"name":"main"}],"automountServiceAccountToken":true}}`
	workloadUserSecCtx  = `{"spec":{"containers":[{"name":"main"}],"securityContext":{"runAsUser":1}}}`
)

// workloadSeeds are {pod template, params, common labels/annotations enabled} triples.
var workloadSeeds = []struct {
	template string
	params   string
	common   bool
}{
	{`{}`, `{}`, false},
	{`{}`, `{"hostAliases":[{"ip":"1.2.3.4","hostnames":["old.example"]}],"host_aliases":[{"ip":"5.6.7.8","hostnames":["new.example"]}],"hostNetwork":true,"dnsPolicy":"ClusterFirstWithHostNet","dnsConfig":{"nameservers":["8.8.8.8"]},"enableServiceLinks":false}`, false},
	{`{}`, `{"hostAliases":[{"ip":"1.2.3.4","hostnames":["old.example"]}]}`, false},
	{`{}`, `{"useStrictSecurity":true,"terminationGracePeriodSeconds":0,"tolerations":[{"key":"dedicated","operator":"Exists"}],"imagePullSecrets":[{"name":"registry"}],"readinessGates":[{"conditionType":"example.com/ready"}],"topologySpreadConstraints":[{"maxSkew":1,"topologyKey":"zone","whenUnsatisfiable":"DoNotSchedule"}]}`, false},
	{`{}`, `{"affinity":{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"zone","operator":"Exists"}]}]}}},"priorityClassName":"high"}`, false},
	{`{}`, `{"useStrictSecurity":true,"securityContext":{"runAsUser":1,"fsGroup":2,"privileged":true}}`, false},
	{`{}`, `{"disableAutomountServiceAccountToken":true}`, false},
	{`{}`, `{"nodeSelector":{"zone":"east"}}`, true},
	{workloadAutomount, `{}`, false},
	{workloadAutomount, `{"disableAutomountServiceAccountToken":true}`, false},
	{workloadPodTemplate, `{}`, true},
	{workloadPodTemplate, `{"replicaCount":0,"minReadySeconds":10,"revisionHistoryLimitCount":0,"nodeSelector":{"zone":"east"},"schedulerName":"custom","runtimeClassName":"sandbox"}`, false},
	{workloadUserSecCtx, `{}`, false},
}

func fuzzCommonWorkloadParams[T any](f *testing.F, tgt workloadTarget[T]) {
	for _, s := range workloadSeeds {
		f.Add([]byte(fmt.Sprintf(`{"initial":`+tgt.wrap+`,"params":%s,"common":%t}`, s.template, s.params, s.common)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Params  vmv1beta1.CommonAppsParams `json:"params"`
			Initial T                          `json:"initial"`
			Common  bool                       `json:"common"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}

		cfg := config.MustGetBaseConfig()
		labels, annotations := cfg.CommonLabels, cfg.CommonAnnotations
		t.Cleanup(func() { cfg.CommonLabels, cfg.CommonAnnotations = labels, annotations })
		cfg.CommonLabels, cfg.CommonAnnotations = nil, nil
		if input.Common {
			cfg.CommonLabels = map[string]string{"app": "common", "common": "label"}
			cfg.CommonAnnotations = map[string]string{"owner": "common", "common": "annotation"}
		}

		tgt.build(&input.Initial, &input.Params)
	})
}

func FuzzDeploymentAddCommonParams(f *testing.F) {
	fuzzCommonWorkloadParams(f, workloadTarget[appsv1.Deployment]{
		build: DeploymentAddCommonParams,
		wrap:  `{"spec":{"template":%s}}`,
	})
}

func FuzzStatefulSetAddCommonParams(f *testing.F) {
	fuzzCommonWorkloadParams(f, workloadTarget[appsv1.StatefulSet]{
		build: StatefulSetAddCommonParams,
		wrap:  `{"spec":{"template":%s}}`,
	})
}

func FuzzDaemonSetAddCommonParams(f *testing.F) {
	fuzzCommonWorkloadParams(f, workloadTarget[appsv1.DaemonSet]{
		build: DaemonSetAddCommonParams,
		wrap:  `{"spec":{"template":%s}}`,
	})
}

func FuzzPodTemplateAddCommonParams(f *testing.F) {
	fuzzCommonWorkloadParams(f, workloadTarget[corev1.PodTemplateSpec]{
		build: PodTemplateAddCommonParams,
		wrap:  `%s`,
	})
}
