package vmauth

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/fuzzutil"
)

func FuzzCreateOrUpdate(f *testing.F) {
	fuzzutil.CreateOrUpdate(f, []string{
		`{}`,
		`{"spec":{"unauthorizedUserAccessSpec":{"url_prefix":["http://backend"]}}}`,
		`{"spec":{"replicaCount":2,"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"vpa":{},"hpa":{"minReplicas":1,"maxReplicas":3},"ingress":{"class_name":"nginx","host":"a.example.com"}}}`,
		`{"spec":{"httpRoute":{"parentRefs":[{"name":"gw"}],"hostnames":["a.example.com"]},"selectAllByDefault":true,"configSecret":"cfg"}}`,
		`{"spec":{"replicaCount":2},"status":{"lastAppliedSpec":{"replicaCount":1}}}`,
		`{"spec":{"selectAllByDefault":true,"unauthorizedUserAccessSpec":{"targetRefs":[{"static":{"url":"http://a"}},{"crd":{"kind":"VMAgent","name":"ref","namespace":"default"}}],"url_map":[{"src_paths":["/a"],"url_prefix":["http://a"]}],"default_url":["http://d"],"ip_filters":{"deny_list":["1.1.1.1"]},"headers":["X-A:1"],"retry_status_codes":[502],"load_balancing_policy":"first_available","drop_src_path_prefix_parts":1,"max_concurrent_requests":5}}}`,
		`{"spec":{"userSelector":{"matchLabels":{"a":"b"}},"userNamespaceSelector":{}}}`,
		`{"spec":{"paused":true}}`,
	}, func() *vmv1beta1.VMAuth { return &vmv1beta1.VMAuth{} }, func(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
		if err := createUsers(ctx, rclient); err != nil {
			return err
		}
		return CreateOrUpdate(ctx, cr, rclient)
	})
}

// fuzzUsers are VMUsers and targets selected by VMAuth.
var fuzzUsers = []string{
	`{"metadata":{"name":"gen"},"spec":{"generatePassword":true,"targetRefs":[{"crd":{"kind":"VMAgent","name":"ref","namespace":"default"},"paths":["/api/v1/write"]},{"crd":{"kind":"VMCluster/vmselect","name":"ref","namespace":"default","objects":[{"name":"ref","namespace":"default","useExtraService":true}]},"target_path_suffix":"/select/0","targetRefBasicAuth":{"username":{"name":"ref","key":"key"},"password":{"name":"ref","key":"key"}}},{"static":{"urls":["http://a","http://b"]},"hosts":["h"],"headers":["X-A:1"],"query_args":[{"name":"q","values":["1"]}]}]}}`,
	`{"metadata":{"name":"token"},"spec":{"bearerToken":"t","targetRefs":[{"static":{"url":"http://a"}}],"default_url":["http://d"],"ip_filters":{"allow_list":["10.0.0.0/8"]}}}`,
	`{"metadata":{"name":"refs"},"spec":{"username":"u","passwordRef":{"name":"ref","key":"key"},"targetRefs":[{"static":{"url":"http://a"}}],"disable_secret_creation":true}}`,
	`{"metadata":{"name":"missing"},"spec":{"username":"m","targetRefs":[{"crd":{"kind":"VLSingle","name":"none","namespace":"default"}}]}}`,
}

// createUsers adds fuzzUsers and their targets to rclient.
func createUsers(ctx context.Context, rclient client.Client) error {
	objs := []client.Object{
		&vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{Name: fuzzutil.RefName, Namespace: "default"}},
		&vmv1beta1.VMCluster{ObjectMeta: metav1.ObjectMeta{Name: fuzzutil.RefName, Namespace: "default"}, Spec: vmv1beta1.VMClusterSpec{VMSelect: &vmv1beta1.VMSelect{}}},
	}
	for _, u := range fuzzUsers {
		user := &vmv1beta1.VMUser{}
		if err := json.Unmarshal([]byte(u), user); err != nil {
			return err
		}
		user.Namespace = "default"
		objs = append(objs, user)
	}
	for _, o := range objs {
		if err := rclient.Create(ctx, o); err != nil {
			return err
		}
	}
	return nil
}
