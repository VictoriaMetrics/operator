package vmdistributed

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

func createOrUpdateVMAuthLB(ctx context.Context, rclient client.Client, cr *vmv1alpha1.VMDistributed, vmClusters []*vmv1beta1.VMCluster) error {
	if cr.Spec.VMAuth.Name == "" {
		return nil
	}

	var targetRefs []vmv1beta1.TargetRef
	for _, cluster := range vmClusters {
		targetRefs = append(targetRefs, vmv1beta1.TargetRef{
			URLMapCommon: vmv1beta1.URLMapCommon{
				DropSrcPathPrefixParts: ptr.To(0),
				LoadBalancingPolicy:    ptr.To("first_available"),
				RetryStatusCodes:       []int{500, 502, 503},
			},
			Paths: []string{"/select/.+", "/admin/tenants"},
			CRD: &vmv1beta1.CRDRef{
				Kind:      "VMCluster/vmselect",
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
		})
	}
	slices.SortFunc(targetRefs, func(a, b vmv1beta1.TargetRef) int {
		return cmp.Compare(a.CRD.Name, b.CRD.Name)
	})

	vmAuthSpec := cr.Spec.VMAuth.Spec.DeepCopy()
	if vmAuthSpec == nil {
		vmAuthSpec = &vmv1beta1.VMAuthSpec{}
	}

	vmAuthSpec.SelectAllByDefault = true
	vmAuthSpec.UnauthorizedUserAccessSpec = &vmv1beta1.VMAuthUnauthorizedUserAccessSpec{
		TargetRefs: targetRefs,
	}

	newVMAuth := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.VMAuth.Name,
			Namespace: cr.Namespace,
		},
		Spec: *vmAuthSpec,
	}

	currentVMAuth := &vmv1beta1.VMAuth{}
	nsn := types.NamespacedName{Name: newVMAuth.Name, Namespace: newVMAuth.Namespace}
	if err := rclient.Get(ctx, nsn, currentVMAuth); err != nil {
		if k8serrors.IsNotFound(err) {
			if _, err := setOwnerRefIfNeeded(cr, newVMAuth, rclient.Scheme()); err != nil {
				return err
			}
			logger.WithContext(ctx).Info("creating VMAuth", "vmauth", newVMAuth.Name)
			return rclient.Create(ctx, newVMAuth)
		}
		return fmt.Errorf("failed to get VMAuth: %w", err)
	}

	ownerRefChanged, err := setOwnerRefIfNeeded(cr, currentVMAuth, rclient.Scheme())
	if err != nil {
		return err
	}

	if !ownerRefChanged && reflect.DeepEqual(currentVMAuth.Spec, newVMAuth.Spec) {
		return nil
	}

	currentVMAuth.Spec = *vmAuthSpec
	return rclient.Update(ctx, currentVMAuth)
}

func waitForVMAuthReady(ctx context.Context, rclient client.Client, vmAuth *vmv1beta1.VMAuth, readyDeadline *metav1.Duration) error {
	defaultReadyDeadline := time.Minute
	if readyDeadline != nil {
		defaultReadyDeadline = readyDeadline.Duration
	}

	var lastStatus vmv1beta1.UpdateStatus
	// Fetch VMAuth in a loop until it has UpdateStatusOperational status
	nsn := types.NamespacedName{Name: vmAuth.Name, Namespace: vmAuth.Namespace}
	err := wait.PollUntilContextTimeout(ctx, time.Second, defaultReadyDeadline, true, func(ctx context.Context) (done bool, err error) {
		if err := rclient.Get(ctx, nsn, vmAuth); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, nil
		}
		lastStatus = vmAuth.Status.UpdateStatus
		return vmAuth.GetGeneration() == vmAuth.Status.ObservedGeneration && vmAuth.Status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for VMAuth %s to be ready: %w, current status: %s", nsn.String(), err, lastStatus)
	}

	return nil
}
