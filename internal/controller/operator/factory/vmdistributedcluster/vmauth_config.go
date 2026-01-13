package vmdistributedcluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

func createOrUpdateVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1alpha1.VMDistributedCluster, vmClusters []*vmv1beta1.VMCluster) error {
	if cr.Spec.VMAuth.Name == "" {
		return nil
	}

	var vmSelectURLs []string
	for _, cluster := range vmClusters {
		url := fmt.Sprintf("http://vmselect-%s.%s.svc:8481/", cluster.Name, cluster.Namespace)
		vmSelectURLs = append(vmSelectURLs, url)
	}
	sort.Strings(vmSelectURLs)

	vmAuthSpec := vmv1beta1.VMAuthSpec{}
	if cr.Spec.VMAuth.Spec != nil {
		src := cr.Spec.VMAuth.Spec
		vmAuthSpec.PodMetadata = src.PodMetadata
		vmAuthSpec.ServiceSpec = src.AdditionalServiceSpec
		vmAuthSpec.ServiceScrapeSpec = src.ServiceScrapeSpec
		vmAuthSpec.LogFormat = src.LogFormat
		vmAuthSpec.LogLevel = src.LogLevel
		vmAuthSpec.License = src.License
		vmAuthSpec.PodDisruptionBudget = src.PodDisruptionBudget
		vmAuthSpec.UpdateStrategy = src.UpdateStrategy
		vmAuthSpec.RollingUpdate = src.RollingUpdate
		vmAuthSpec.CommonApplicationDeploymentParams = src.CommonApplicationDeploymentParams
		vmAuthSpec.CommonDefaultableParams = src.CommonDefaultableParams
		vmAuthSpec.EmbeddedProbes = src.EmbeddedProbes
		vmAuthSpec.CommonConfigReloaderParams = src.CommonConfigReloaderParams
	}

	vmAuthSpec.UnauthorizedUserAccessSpec = &vmv1beta1.VMAuthUnauthorizedUserAccessSpec{
		URLMap: []vmv1beta1.UnauthorizedAccessConfigURLMap{
			{
				URLMapCommon: vmv1beta1.URLMapCommon{
					DropSrcPathPrefixParts: ptr.To(0),
					LoadBalancingPolicy:    ptr.To("first_available"),
					RetryStatusCodes:       []int{500, 502, 503},
				},
				SrcPaths:  []string{"/select/.+", "/admin/tenants"},
				URLPrefix: vmv1beta1.StringOrArray(vmSelectURLs),
			},
		},
	}

	newVMAuth := &vmv1beta1.VMAuth{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Spec.VMAuth.Name,
			Namespace: cr.Namespace,
		},
		Spec: vmAuthSpec,
	}

	currentVMAuth := &vmv1beta1.VMAuth{}
	err := rclient.Get(ctx, types.NamespacedName{Name: newVMAuth.Name, Namespace: newVMAuth.Namespace}, currentVMAuth)
	if err != nil {
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

	currentVMAuth.Spec = vmAuthSpec
	return rclient.Update(ctx, currentVMAuth)
}
