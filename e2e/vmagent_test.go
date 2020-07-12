package e2e

import (
	goctx "context"
	"fmt"
	"testing"

	operator "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

func vmAgentCreateTest(t *testing.T, f *framework.Framework, ctx *framework.Context) error {
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}
	// create  custom resource
	exampleVmAgent := &operator.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmagent",
			Namespace: namespace,
		},
		Spec: operator.VMAgentSpec{
			RemoteWrite: []operator.VMAgentRemoteWriteSpec{
				{URL: "http://localhost"},
			},
			ReplicaCount: pointer.Int32Ptr(1),
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), exampleVmAgent, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	//wait for config
	err = WaitForSecret(t, f.KubeClient, namespace, "vmagent-example-vmagent", retryInterval, timeout)
	if err != nil {
		return err
	}
	// wait for example-vmalert to reach 1 replicas
	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "vmagent-example-vmagent", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-vmagent", Namespace: namespace}, exampleVmAgent)
	if err != nil {
		return err
	}
	exampleVmAgent.Spec.ReplicaCount = pointer.Int32Ptr(2)
	exampleVmAgent.Spec.ExtraArgs = map[string]string{"loggerLevel": "ERROR"}
	err = f.Client.Update(goctx.TODO(), exampleVmAgent)
	if err != nil {
		return err
	}
	return e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "vmagent-example-vmagent", 2, retryInterval, timeout)
}

func vmAgent(t *testing.T) {
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()
	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}
	t.Log("Initialized cluster resources")
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		t.Fatal(err)
	}
	// get global framework variables
	f := framework.Global
	// wait for vm-operator to be ready
	err = e2eutil.WaitForOperatorDeployment(t, f.KubeClient, namespace, "vm-operator", 1, retryInterval, timeout)
	if err != nil {
		t.Fatal(err)
	}

	if err = vmAgentCreateTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}
}
