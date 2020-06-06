package e2e

import (
	"github.com/VictoriaMetrics/operator/pkg/apis"
	operator "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
	"time"

	f "github.com/operator-framework/operator-sdk/pkg/test"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Second * 60
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 30
)

func TestMain(m *testing.M) {
	f.MainEntry(m)
}

func addToSchemeCrds(t *testing.T) error {
	objs := []runtime.Object{
		&operator.VmSingleList{},
		&operator.VmSingle{},
		&operator.VmAgentList{},
		&operator.VmAgent{},
		&operator.VmAlert{},
		&operator.VmAlertList{},
	}

	for _, obj := range objs {
		err := framework.AddToFrameworkScheme(apis.AddToScheme, obj)
		if err != nil {
			t.Fatalf("failted to add custom resource to scheme: %v", err)
		}

	}
	return nil

}


func TestVmApps(t *testing.T) {

	err := addToSchemeCrds(t)
	if err != nil {
		t.Fatalf("failted to add custom resource to scheme: %v", err)
	}
	t.Run("vm", func(t *testing.T) {
		t.Run("VmSingle", vmSingle)
	})
	t.Run("vm", func(t *testing.T) {
		t.Run("VmALert", vmAlert)
	})
	t.Run("vm", func(t *testing.T) {
		t.Run("VmAgent", vmAgent)
	})


}