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
	} {
		f.Add([]byte(seed))
	}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		f.Fatal(err)
	}
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	f.Fuzz(func(t *testing.T, data []byte) {
		var spec vmv1beta1.VMUserSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			fuzztest.New(data).Fuzz(&spec)
		}
		render := func() ([]byte, error) {
			user := &vmv1beta1.VMUser{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "default"}, Spec: *spec.DeepCopy()}
			pos := &parsedObjects{users: build.NewChildObjects("vmuser", []*vmv1beta1.VMUser{user}, []string{"default/user"})}
			return pos.generateConfig(&vmv1beta1.VMAuth{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "default"}}, map[string]string{}, build.NewAssetsCache(t.Context(), client, nil))
		}
		a, err := render()
		b, err2 := render()
		if (err == nil) != (err2 == nil) || !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent VMUser config: %q (%v) != %q (%v)", a, err, b, err2)
		}
	})
}
