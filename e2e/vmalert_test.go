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

func vmAlertCreateTest(t *testing.T, f *framework.Framework, ctx *framework.Context) error {
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}
	// create  custom resource
	exampleVmAlert := &operator.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmalert",
			Namespace: namespace,
		},
		Spec: operator.VMAlertSpec{
			ReplicaCount: pointer.Int32Ptr(1),
			NotifierURL:  "http://localhost",
			Datasource:   operator.VMAlertDatasourceSpec{URL: "http://localhost"},
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), exampleVmAlert, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	//wait for base config
	err = WaitForConfigMap(t, f.KubeClient, namespace, "vm-example-vmalert-rulefiles-0", retryInterval, timeout)
	if err != nil {
		return err
	}

	// wait for example-vmalert to reach 1 replicas
	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "vmalert-example-vmalert", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-vmalert", Namespace: namespace}, exampleVmAlert)
	if err != nil {
		return err
	}
	exampleVmAlert.Spec.ReplicaCount = pointer.Int32Ptr(1)
	exampleVmAlert.Spec.NotifierURL = "http://localhost:8123"
	exampleVmAlert.Spec.ExtraArgs = map[string]string{"loggerLevel": "ERROR"}
	err = f.Client.Update(goctx.TODO(), exampleVmAlert)
	if err != nil {
		return err
	}
	return e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "vmalert-example-vmalert", 1, retryInterval, timeout)
}

/*func vmAlertFullTest(t *testing.T, f *framework.Framework, ctx *framework.Context) error {
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}
	// create  custom resource
	basicAuthSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmalert-basicauth",
			Namespace: namespace,
		},
		StringData: map[string]string{"username": "admin", "password": "passw0rd"},
	}

	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), basicAuthSecret, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	basicAuthSelector :=  &operator.BasicAuth{
		Username: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{Name: "example-vmalert-basicauth"}, Key: "username"},
		Password: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{Name: "example-vmalert-basicauth"}, Key: "password"},
	}
	flushInt := "5m"
	lookback := "1h"
	exampleFullVmAlert := &operator.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmalert-full",
			Namespace: namespace,
		},
		Spec: operator.VMAlertSpec{
			ReplicaCount: pointer.Int32Ptr(1),
			NotifierURL:  "http://localhost",
			Datasource:   operator.VMAlertDatasourceSpec{
				URL: "http://localhost",
				BasicAuth: basicAuthSelector,
			},
			EvaluationInterval: "30s",
			RuleSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"managed-by": "vm-operator"}},
			RemoteWrite: &operator.VMAlertRemoteWriteSpec{
				URL: "http://localhost",
				BasicAuth: basicAuthSelector,
				Concurrency: pointer.Int32Ptr(12),
				FlushInterval: &flushInt,
				MaxBatchSize: pointer.Int32Ptr(20),
				MaxQueueSize: pointer.Int32Ptr(100),
			},
			RemoteRead: &operator.VMAlertRemoteReadSpec{
				URL: "http://localhost",
				BasicAuth: basicAuthSelector,
				Lookback: &lookback,
			},
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), exampleFullVmAlert, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	//wait for base config
	err = WaitForConfigMap(t, f.KubeClient, namespace, "vm-example-vmalert-full-rulefiles-0", retryInterval, timeout)
	if err != nil {
		return err
	}

	// wait for example-vmalert to reach 1 replicas
	return e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "vmalert-example-vmalert-full", 1, retryInterval, timeout)
}*/

func vmAlert(t *testing.T) {
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

	if err = vmAlertCreateTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}
	//if err = vmAlertFullTest(t, f, ctx); err != nil {
	//	t.Fatal(err)
	//}
}
