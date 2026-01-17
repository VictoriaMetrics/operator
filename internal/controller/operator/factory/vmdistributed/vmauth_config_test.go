package VMDistributed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestCreateOrUpdateVMAuthLB(t *testing.T) {
	t.Run("should do nothing if VMAuth name is empty", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.VMAuth.Name = ""
		rclient := data.trackingClient
		ctx := context.Background()

		err := createOrUpdateVMAuthLB(ctx, rclient, data.cr, []*vmv1beta1.VMCluster{data.vmcluster1, data.vmcluster2})
		assert.NoError(t, err)
		assert.Empty(t, rclient.Actions)
	})

	t.Run("should create VMAuth if it does not exist", func(t *testing.T) {
		data := beforeEach()
		data.vmcluster1.Spec.VMSelect.Port = "8481"
		data.vmcluster2.Spec.VMSelect.Port = "8481"
		data.cr.Spec.VMAuth.Name = "vmauth-lb"
		data.cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{
			LogLevel: "INFO",
		}
		rclient := data.trackingClient
		ctx := context.Background()

		clusters := []*vmv1beta1.VMCluster{data.vmcluster1, data.vmcluster2}
		err := createOrUpdateVMAuthLB(ctx, rclient, data.cr, clusters)
		assert.NoError(t, err)

		// Check actions
		var createAction *action
		for _, a := range rclient.Actions {
			if a.Method == "Create" {
				if _, ok := a.Object.(*vmv1beta1.VMAuth); ok {
					createAction = &a
					break
				}
			}
		}
		require.NotNil(t, createAction, "VMAuth should be created")

		createdVMAuth := createAction.Object.(*vmv1beta1.VMAuth)
		assert.Equal(t, "vmauth-lb", createdVMAuth.Name)
		assert.Equal(t, "default", createdVMAuth.Namespace)
		assert.Equal(t, "INFO", createdVMAuth.Spec.LogLevel)

		require.NotNil(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec)
		require.Len(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs, 2)

		firstTargetRef := createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[0]
		assert.Equal(t, ptr.To(0), firstTargetRef.DropSrcPathPrefixParts)
		assert.Equal(t, ptr.To("first_available"), firstTargetRef.LoadBalancingPolicy)
		assert.Equal(t, []int{500, 502, 503}, firstTargetRef.RetryStatusCodes)
		assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, firstTargetRef.Paths)
		require.NotNil(t, firstTargetRef.CRD)
		assert.Equal(t, "VMCluster/vmselect", firstTargetRef.CRD.Kind)
		assert.Equal(t, data.vmcluster1.Name, firstTargetRef.CRD.Name)
		assert.Equal(t, data.vmcluster1.Namespace, firstTargetRef.CRD.Namespace)

		secondTargetRef := createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[1]
		assert.Equal(t, ptr.To(0), secondTargetRef.DropSrcPathPrefixParts)
		assert.Equal(t, ptr.To("first_available"), secondTargetRef.LoadBalancingPolicy)
		assert.Equal(t, []int{500, 502, 503}, secondTargetRef.RetryStatusCodes)
		assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, secondTargetRef.Paths)
		require.NotNil(t, secondTargetRef.CRD)
		assert.Equal(t, "VMCluster/vmselect", secondTargetRef.CRD.Kind)
		assert.Equal(t, data.vmcluster2.Name, secondTargetRef.CRD.Name)
		assert.Equal(t, data.vmcluster2.Namespace, secondTargetRef.CRD.Namespace)

		// Verify OwnerReference
		assert.NotEmpty(t, createdVMAuth.OwnerReferences)
		assert.Equal(t, data.cr.Name, createdVMAuth.OwnerReferences[0].Name)
	})

	t.Run("should update VMAuth if spec changes", func(t *testing.T) {
		data := beforeEach()
		data.vmcluster1.Spec.VMSelect.Port = "8481"
		data.cr.Spec.VMAuth.Name = "vmauth-lb"

		// Create existing VMAuth with different spec
		existingVMAuth := &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmauth-lb",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAuthSpec{
				LogLevel: "ERROR",
			},
		}

		rclient := data.trackingClient
		// Need to create it in the client
		err := rclient.Create(context.Background(), existingVMAuth)
		require.NoError(t, err)
		rclient.Actions = []action{} // Clear create action

		data.cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{
			LogLevel: "INFO",
		}

		ctx := context.Background()
		clusters := []*vmv1beta1.VMCluster{data.vmcluster1}
		err = createOrUpdateVMAuthLB(ctx, rclient, data.cr, clusters)
		assert.NoError(t, err)

		// Check for update action
		var updateAction *action
		for _, a := range rclient.Actions {
			if a.Method == "Update" {
				if _, ok := a.Object.(*vmv1beta1.VMAuth); ok {
					updateAction = &a
					break
				}
			}
		}
		require.NotNil(t, updateAction, "VMAuth should be updated")

		updatedVMAuth := updateAction.Object.(*vmv1beta1.VMAuth)
		assert.Equal(t, "INFO", updatedVMAuth.Spec.LogLevel)
	})

	t.Run("should not update VMAuth if spec matches", func(t *testing.T) {
		data := beforeEach()
		data.vmcluster1.Spec.VMSelect.Port = "8481"
		data.cr.Spec.VMAuth.Name = "vmauth-lb"
		data.cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{
			LogLevel: "INFO",
		}

		// Construct the expected VMAuth to exist already
		ctx := context.Background()
		clusters := []*vmv1beta1.VMCluster{data.vmcluster1}

		// We first let the function create it to get the exact state
		rclient := data.trackingClient
		err := createOrUpdateVMAuthLB(ctx, rclient, data.cr, clusters)
		require.NoError(t, err)

		rclient.Actions = []action{} // Clear actions

		// Run again
		err = createOrUpdateVMAuthLB(ctx, rclient, data.cr, clusters)
		assert.NoError(t, err)

		// Should contain no updates
		for _, a := range rclient.Actions {
			if a.Method == "Update" {
				if _, ok := a.Object.(*vmv1beta1.VMAuth); ok {
					t.Fatalf("Unexpected update action on VMAuth")
				}
			}
		}
	})

	t.Run("should adopt existing VMAuth if owner reference is missing", func(t *testing.T) {
		data := beforeEach()
		data.vmcluster1.Spec.VMSelect.Port = "8481"
		data.cr.Spec.VMAuth.Name = "vmauth-lb"
		data.cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{
			LogLevel: "INFO",
		}

		rclient := data.trackingClient

		// Create VMAuth without owner ref
		existingVMAuth := &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmauth-lb",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAuthSpec{
				// We intentionally don't set a full spec here,
				// so the update will populate the missing fields (like Port)
				// AND set the owner ref.
				LogLevel: "INFO",
			},
		}
		err := rclient.Create(context.Background(), existingVMAuth)
		require.NoError(t, err)
		rclient.Actions = []action{}

		ctx := context.Background()
		clusters := []*vmv1beta1.VMCluster{data.vmcluster1}
		err = createOrUpdateVMAuthLB(ctx, rclient, data.cr, clusters)
		assert.NoError(t, err)

		// Expect Update action
		var updateAction *action
		for _, a := range rclient.Actions {
			if a.Method == "Update" {
				if _, ok := a.Object.(*vmv1beta1.VMAuth); ok {
					updateAction = &a
					break
				}
			}
		}
		require.NotNil(t, updateAction, "VMAuth should be updated with owner ref")

		updatedVMAuth := updateAction.Object.(*vmv1beta1.VMAuth)
		assert.NotEmpty(t, updatedVMAuth.OwnerReferences)
		assert.Equal(t, data.cr.Name, updatedVMAuth.OwnerReferences[0].Name)
	})

	t.Run("should use load balancer service URL if RequestsLoadBalancer is enabled", func(t *testing.T) {
		data := beforeEach()
		data.cr.Spec.VMAuth.Name = "vmauth-lb"

		data.vmcluster1.Spec.VMSelect.Port = "8481"
		data.vmcluster2.Spec.VMSelect.Port = "8481"

		// Enable load balancer for vmcluster1
		data.vmcluster1.Spec.RequestsLoadBalancer.Enabled = true
		data.vmcluster1.Spec.RequestsLoadBalancer.Spec.Port = "8427"

		rclient := data.trackingClient
		ctx := context.Background()

		clusters := []*vmv1beta1.VMCluster{data.vmcluster1, data.vmcluster2}
		err := createOrUpdateVMAuthLB(ctx, rclient, data.cr, clusters)
		assert.NoError(t, err)

		var createAction *action
		for _, a := range rclient.Actions {
			if a.Method == "Create" {
				if _, ok := a.Object.(*vmv1beta1.VMAuth); ok {
					createAction = &a
					break
				}
			}
		}
		require.NotNil(t, createAction, "VMAuth should be created")

		createdVMAuth := createAction.Object.(*vmv1beta1.VMAuth)
		require.NotNil(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec)
		require.Len(t, createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs, 2)

		firstTargetRef := createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[0]
		assert.Equal(t, ptr.To(0), firstTargetRef.DropSrcPathPrefixParts)
		assert.Equal(t, ptr.To("first_available"), firstTargetRef.LoadBalancingPolicy)
		assert.Equal(t, []int{500, 502, 503}, firstTargetRef.RetryStatusCodes)
		assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, firstTargetRef.Paths)
		require.NotNil(t, firstTargetRef.CRD)
		assert.Equal(t, "VMCluster/vmselect", firstTargetRef.CRD.Kind)
		assert.Equal(t, data.vmcluster1.Name, firstTargetRef.CRD.Name)
		assert.Equal(t, data.vmcluster1.Namespace, firstTargetRef.CRD.Namespace)

		secondTargetRef := createdVMAuth.Spec.UnauthorizedUserAccessSpec.TargetRefs[1]
		assert.Equal(t, ptr.To(0), secondTargetRef.DropSrcPathPrefixParts)
		assert.Equal(t, ptr.To("first_available"), secondTargetRef.LoadBalancingPolicy)
		assert.Equal(t, []int{500, 502, 503}, secondTargetRef.RetryStatusCodes)
		assert.Equal(t, []string{"/select/.+", "/admin/tenants"}, secondTargetRef.Paths)
		require.NotNil(t, secondTargetRef.CRD)
		assert.Equal(t, "VMCluster/vmselect", secondTargetRef.CRD.Kind)
		assert.Equal(t, data.vmcluster2.Name, secondTargetRef.CRD.Name)
		assert.Equal(t, data.vmcluster2.Namespace, secondTargetRef.CRD.Namespace)
	})
}
