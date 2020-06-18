package e2e

import (
	goctx "context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"testing"

	operator "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

var (
	alertmanagerTestConf = `
global:
  resolve_timeout: 5m
route:
  group_by: ['job']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  receiver: 'webhook'
receivers:
- name: 'webhook'
  webhook_configs:
  - url: 'http://alertmanagerwh:30500/'`
)

func vmAlertManagerCreateTest(t *testing.T, f *framework.Framework, ctx *framework.Context) error {
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}
	// create  custom resource
	exampleVmAlertManager := &operator.VMAlertmanager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmalertmanager",
			Namespace: namespace,
		},
		Spec: operator.VMAlertmanagerSpec{
			ReplicaCount: pointer.Int32Ptr(1),
			ConfigSecret: "alertmanager-conf",
		},
	}
	AlertManagerConfig := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "alertmanager-conf",
			Namespace: namespace,
		},
		StringData: map[string]string{
			"alertmanager.yaml": alertmanagerTestConf,
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), AlertManagerConfig, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	err = WaitForSecret(t, f.KubeClient, namespace, "alertmanager-conf", retryInterval, timeout)
	if err != nil {
		return err
	}

	err = f.Client.Create(goctx.TODO(), exampleVmAlertManager, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	// wait for example-vmalertmanager to reach 1 replicas
	err = WaitForSts(t, f.KubeClient, namespace, "vmalertmanager-example-vmalertmanager", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-vmalertmanager", Namespace: namespace}, exampleVmAlertManager)
	if err != nil {
		return err
	}

	//increase replicas count and update vmalertmanager
	exampleVmAlertManager.Spec.ReplicaCount = pointer.Int32Ptr(2)
	err = f.Client.Update(goctx.TODO(), exampleVmAlertManager)
	if err != nil {
		return err
	}
	return WaitForSts(t, f.KubeClient, namespace, "vmalertmanager-example-vmalertmanager", 2, retryInterval, timeout)
}

func vmAlertManager(t *testing.T) {
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

	if err = vmAlertManagerCreateTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}
}
