package build

import (
	"encoding/json"
	"testing"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func fuzzCommonWorkloadParams[T any](f *testing.F, build func(*T, *vmv1beta1.CommonAppsParams), seeds ...string) {
	for _, seed := range append([]string{
		`{}`,
		`{"params":{"replicaCount":0,"minReadySeconds":10,"revisionHistoryLimitCount":0,"nodeSelector":{"zone":"east"},"schedulerName":"custom","runtimeClassName":"sandbox"}}`,
		`{"params":{"hostAliases":[{"ip":"1.2.3.4","hostnames":["old.example"]}],"host_aliases":[{"ip":"5.6.7.8","hostnames":["new.example"]}],"hostNetwork":true,"dnsPolicy":"ClusterFirstWithHostNet","dnsConfig":{"nameservers":["8.8.8.8"]},"enableServiceLinks":false,"disableAutomountServiceAccountToken":true}}`,
		`{"params":{"useStrictSecurity":true,"terminationGracePeriodSeconds":0,"tolerations":[{"key":"dedicated","operator":"Exists"}],"imagePullSecrets":[{"name":"registry"}],"readinessGates":[{"conditionType":"example.com/ready"}],"topologySpreadConstraints":[{"maxSkew":1,"topologyKey":"zone","whenUnsatisfiable":"DoNotSchedule"}]}}`,
	}, seeds...) {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var input struct {
			Params  vmv1beta1.CommonAppsParams `json:"params"`
			Initial T                          `json:"initial"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			fuzztest.New(data).Fill(&input)
		}
		assertStableBuilder(t, func() (any, error) {
			build(&input.Initial, &input.Params)
			// Snapshot before the next in-place mutation; returning the pointer would alias both results.
			return json.Marshal(input.Initial)
		})
	})
}

func FuzzDeploymentAddCommonParams(f *testing.F) {
	fuzzCommonWorkloadParams(f, DeploymentAddCommonParams,
		`{"initial":{"spec":{"template":{"metadata":{"labels":{"app":"existing"},"annotations":{"owner":"team"}},"spec":{"containers":[{"name":"main","image":"example:latest"}]}}}},"params":{"nodeSelector":{"role":"worker"},"replicaCount":2}}`,
	)
}

func FuzzStatefulSetAddCommonParams(f *testing.F) {
	fuzzCommonWorkloadParams(f, StatefulSetAddCommonParams,
		`{"initial":{"spec":{"template":{"metadata":{"labels":{"app":"existing"}},"spec":{"containers":[{"name":"main","image":"example:latest"}]}}}},"params":{"schedulerName":"custom","replicaCount":3}}`,
	)
}

func FuzzDaemonSetAddCommonParams(f *testing.F) {
	fuzzCommonWorkloadParams(f, DaemonSetAddCommonParams,
		`{"initial":{"spec":{"template":{"metadata":{"annotations":{"owner":"team"}},"spec":{"containers":[{"name":"main","image":"example:latest"}]}}}},"params":{"useStrictSecurity":true,"minReadySeconds":5}}`,
	)
}

func FuzzPodTemplateAddCommonParams(f *testing.F) {
	fuzzCommonWorkloadParams(f, PodTemplateAddCommonParams,
		`{"initial":{"metadata":{"labels":{"app":"existing"},"annotations":{"owner":"team"}},"spec":{"containers":[{"name":"main","image":"example:latest"}]}},"params":{"nodeSelector":{"zone":"east"},"enableServiceLinks":true}}`,
	)
}
