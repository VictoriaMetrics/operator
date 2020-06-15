package e2e

import (
	"testing"
	"time"

	"github.com/VictoriaMetrics/operator/pkg/apis"
	operator "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	f "github.com/operator-framework/operator-sdk/pkg/test"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/runtime"
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
	t.Run("VmSingle", vmSingle)
	t.Run("VmALert", vmAlert)
	t.Run("VmAgent", vmAgent)
	t.Run("VmAlertManager", vmAlertManager)

}
