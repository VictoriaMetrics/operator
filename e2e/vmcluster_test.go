package e2e

import (
	goctx "context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"testing"

	operator "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

func vmClusterComplete(t *testing.T, f *framework.Framework, ctx *framework.Context) error {
	namespace, err := ctx.GetOperatorNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}
	// create  custom resource
	exampleVmCluster := &operator.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmcluster",
			Namespace: namespace,
		},
		Spec: operator.VMClusterSpec{
			RetentionPeriod: "1",
			ReplicationFactor: pointer.Int32Ptr(1),
			VMStorage: &operator.VMStorage{
				ReplicaCount: pointer.Int32Ptr(1),
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU:resource.MustParse("50m"),
						v1.ResourceMemory: resource.MustParse("100Mi"),
					},
				},
				Storage: &operator.StorageSpec{
					VolumeClaimTemplate: operator.EmbeddedPersistentVolumeClaim{
						Spec: v1.PersistentVolumeClaimSpec{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{v1.ResourceStorage: resource.MustParse("1Gi") },
							},
						},
					},
				},

			},
			VMSelect: &operator.VMSelect{
				ReplicaCount: pointer.Int32Ptr(1),
				CacheMountPath: "cache-mount",
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU:resource.MustParse("50m"),
						v1.ResourceMemory: resource.MustParse("100Mi"),
					},
				},
			},
			VMInsert: &operator.VMInsert{
				ReplicaCount: pointer.Int32Ptr(1),
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceCPU:resource.MustParse("50m"),
						v1.ResourceMemory: resource.MustParse("100Mi"),
					},
				},

			},
		},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(goctx.TODO(), exampleVmCluster, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}
	// wait for example-vmsingle to reach 1 replicas
	err = WaitForSts(t, f.KubeClient, namespace, "vmstorage-example-vmcluster", 1, retryInterval, timeout)
	if err != nil {
		return err
	}
	err =  WaitForSts (t, f.KubeClient, namespace, "vmselect-example-vmcluster", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "vminsert-example-vmcluster", 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: "example-vmcluster", Namespace: namespace}, exampleVmCluster)

	if err != nil {
		return err
	}
	exampleVmCluster.Spec.VMStorage.ReplicaCount = pointer.Int32Ptr(2)
	exampleVmCluster.Spec.VMSelect.ReplicaCount = pointer.Int32Ptr(3)
	exampleVmCluster.Spec.VMInsert.ReplicaCount = pointer.Int32Ptr(3)
	exampleVmCluster.Spec.RetentionPeriod = "2"
	exampleVmCluster.Spec.VMStorage.ExtraArgs = map[string]string{"loggerLevel": "ERROR"}
	err = f.Client.Update(goctx.TODO(), exampleVmCluster)
	if err != nil {
		return err
	}
	err =  WaitForSts (t, f.KubeClient, namespace, "vmstorage-example-vmcluster", 2, retryInterval, timeout)
	if err != nil {
		return err
	}
	err =  WaitForSts (t, f.KubeClient, namespace, "vmselect-example-vmcluster", 3, retryInterval, timeout)
	if err != nil {
		return err
	}

	return e2eutil.WaitForDeployment(t, f.KubeClient, namespace, "vminsert-example-vmcluster", 3, retryInterval, timeout)
}

func vmCluster(t *testing.T) {
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

	if err = vmClusterComplete(t, f, ctx); err != nil {
		t.Fatal(err)
	}
}

