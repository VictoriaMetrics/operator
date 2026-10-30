package vmauth

import (
	"bytes"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		var spec vmv1beta1.VMUserSpec
		if json.Unmarshal(data, &spec) != nil {
			return
		}
		render := func() ([]byte, error) {
			user := &vmv1beta1.VMUser{ObjectMeta: metav1.ObjectMeta{Name: "user", Namespace: "default"}, Spec: *spec.DeepCopy()}
			pos := &parsedObjects{users: build.NewChildObjects("vmuser", []*vmv1beta1.VMUser{user}, []string{"default/user"})}
			return pos.generateConfig(&vmv1beta1.VMAuth{ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "default"}}, map[string]string{}, build.NewAssetsCache(t.Context(), nil, nil))
		}
		a, err := render()
		b, err2 := render()
		if (err == nil) != (err2 == nil) || !bytes.Equal(a, b) {
			t.Fatalf("non-idempotent VMUser config: %q (%v) != %q (%v)", a, err, b, err2)
		}
	})
}
