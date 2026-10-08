package vmauth

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_newTargetObject(t *testing.T) {
	for _, kind := range []string{
		"VMAgent", "VMAlert", "VMSingle", "VLogs", "VMAlertmanager", "VMAlertManager",
		"VMAnomaly", "VLSingle", "VLAgent", "VTSingle", "VTAgent",
	} {
		if _, err := newTargetObject(kind); err != nil {
			t.Errorf("kind=%q: %s", kind, err)
		}
	}

	// Cluster kinds name their component after the slash, and it selects which service URL gets built.
	for kind, want := range map[string]string{
		"VMCluster/vmselect":  "vmselect",
		"VMCluster/vminsert":  "vminsert",
		"VMCluster/vmstorage": "vmstorage",
		"VLCluster/vlselect":  "vlselect",
		"VLCluster/vlinsert":  "vlinsert",
		"VLCluster/vlstorage": "vlstorage",
		"VTCluster/vtselect":  "vtselect",
		"VTCluster/vtinsert":  "vtinsert",
		"VTCluster/vtstorage": "vtstorage",
	} {
		obj, err := newTargetObject(kind)
		if err != nil {
			t.Errorf("kind=%q: %s", kind, err)
			continue
		}
		cw, ok := obj.(*clusterWithURL)
		if !ok {
			t.Errorf("kind=%q built %T, want *clusterWithURL", kind, obj)
			continue
		}
		if cw.component != want {
			t.Errorf("kind=%q built component %q, want %q", kind, cw.component, want)
		}
	}

	if _, err := newTargetObject("NotAKind"); err == nil {
		t.Error("want an error for an unsupported kind")
	}

	// Two calls for the same kind must not hand back the same object.
	first, _ := newTargetObject("VMSingle")
	second, _ := newTargetObject("VMSingle")
	if first == second {
		t.Error("two calls returned the same object")
	}
}

// Mirrors the operator resolving the same kind for several VMAuths at once.
func Test_updateCRDObjURLsConcurrent(t *testing.T) {
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{
		&vmv1beta1.VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "first", Namespace: "default"}},
		&vmv1beta1.VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "second", Namespace: "default"}},
	})
	var wg sync.WaitGroup
	errs := make(chan error, 256)
	for range 64 {
		for _, name := range []string{"first", "second"} {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				crd := &vmv1beta1.CRDRef{
					Kind:           "VMSingle",
					NamespacedName: vmv1beta1.NamespacedName{Name: name, Namespace: "default"},
				}
				objURLs := make(map[string]string)
				if err := updateCRDObjURLs(t.Context(), rclient, crd, objURLs); err != nil {
					errs <- fmt.Errorf("cannot resolve %q: %w", name, err)
					return
				}
				got := objURLs[crd.AsKey(crd.NamespacedName)]
				if want := "//vmsingle-" + name + "."; !strings.Contains(got, want) {
					errs <- fmt.Errorf("ref %q resolved to %q, want it to contain %q", name, got, want)
				}
			}(name)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
