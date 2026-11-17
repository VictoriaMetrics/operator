package vmauth

import (
	"bytes"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

func FuzzVMUserConfig(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"username":"alice","password":"secret","default_url":["http://backend"]}`,
		`{"targetRefs":[{"static":{"url":"http://backend"},"paths":["/api/v1/.*"]}]}`,
		`{"targetRefs":[{"static":{"urls":["http://a","http://b"]},"hosts":["example.org"]}]}`,
		`{"targetRefs":[{}]}`,
		`{"bearerToken":"t","name":"n","metric_labels":{"a":"b"},"targetRefs":[{"static":{"url":"http://a"},"target_path_suffix":"/p?x=1","query_args":[{"name":"q","values":["v"]}],"headers":["h:v"],"response_headers":["r:v"],"discover_backend_ips":true,"retry_status_codes":[502],"load_balancing_policy":"first_available","drop_src_path_prefix_parts":1}]}`,
		`{"jwt":{"skipVerify":true,"matchClaims":{"a":"b"},"oidc":{"issuer":"http://i"},"defaultVMAccessClaim":{"metricsAccountID":0,"metricsProjectID":1,"metricsExtraFilters":["f"],"metricsExtraLabels":["l=v"],"logsAccountID":2,"logsProjectID":3,"logsExtraFilters":["f"],"logsExtraStreamFilters":["s"]}},"targetRefs":[{"static":{"url":"http://a"}}]}`,
		`{"jwt":{"publicKeys":["key"]},"targetRefs":[{"static":{"url":"http://a"}}]}`,
		`{"default_url":["http://d"],"tlsConfig":{"insecureSkipVerify":true,"serverName":"s"},"ip_filters":{"allow_list":["10.0.0.0/8"],"deny_list":["1.1.1.1"]},"headers":["h:v"],"response_headers":["r:v"],"discover_backend_ips":true,"retry_status_codes":[503],"max_concurrent_requests":5,"load_balancing_policy":"least_loaded","drop_src_path_prefix_parts":2,"dump_request_on_errors":true,"access_log":{"filters":{"skip_status_codes":[200]}},"targetRefs":[{"static":{"url":"http://a"}}]}`,
		`{"targetRefs":[{"crd":{"kind":"VMCluster/vminsert","name":"vm","namespace":"default"}},{"crd":{"kind":"VMCluster/vmselect","objects":[{"name":"vm","namespace":"default"}]}},{"crd":{"kind":"VLCluster/vlinsert","name":"vm","namespace":"default"},"paths":["/","/x"]},{"crd":{"kind":"VMCluster/vminsert","name":"vm","namespace":"default"},"target_path_suffix":"/x"},{"crd":{"kind":"VMCluster/vmselect","name":"vm","namespace":"default"},"target_path_suffix":"/x"},{"static":{"url":"http://a"},"paths":["/a","/b"],"src_headers":["h:v"],"src_query_args":["q=v"]}]}`,
		`{"targetRefs":[{"crd":{"kind":"VMCluster/vminsert","name":"missing","namespace":"default"}}]}`,
		`{"targetRefs":[{"static":{"url":"http://a"},"target_path_suffix":"%zz"}]}`,
	} {
		f.Add([]byte(seed))
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		f.Fatal(err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	objURLs := map[string]string{
		"VMCluster/vminsert/default/vm": "http://vminsert",
		"VMCluster/vmselect/default/vm": "http://vmselect",
		"VLCluster/vlinsert/default/vm": "http://vlinsert",
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var spec vmv1beta1.VMUserSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			fuzztest.New(data).Fill(&spec)
		}
		render := func() ([]byte, error) {
			user := &vmv1beta1.VMUser{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "default"}, Spec: *spec.DeepCopy()}
			pos := &parsedObjects{users: build.NewChildObjects("vmuser", []*vmv1beta1.VMUser{user}, []string{"default/user"})}
			return pos.generateConfig(&vmv1beta1.VMAuth{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "default"}}, objURLs, build.NewAssetsCache(t.Context(), client, nil))
		}
		a, err := render()
		b, err2 := render()
		if (err == nil) != (err2 == nil) || err != nil && err.Error() != err2.Error() || !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent VMUser config: %q (%v) != %q (%v)", a, err, b, err2)
		}
	})
}
