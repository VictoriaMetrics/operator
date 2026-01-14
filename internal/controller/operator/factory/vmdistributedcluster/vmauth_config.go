package vmdistributedcluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

var defaultVMSelectPort = config.MustGetBaseConfig().VMClusterDefault.VMSelectDefault.Port

func createOrUpdateVMAuthLB(ctx context.Context, rclient client.Client, cr, _ *vmv1alpha1.VMDistributedCluster, vmClusters []*vmv1beta1.VMCluster) error {
	if cr.Spec.VMAuth.Name == "" {
		return nil
	}

	var vmSelectURLs []string
	for _, cluster := range vmClusters {
		vmHost := cluster.PrefixedName(vmv1beta1.ClusterComponentSelect)
		vmPort := cluster.Spec.VMSelect.Port
		if vmPort == "" {
			vmPort = defaultVMSelectPort
		}

		if cluster.Spec.RequestsLoadBalancer.Enabled && !cluster.Spec.RequestsLoadBalancer.DisableSelectBalancing {
			vmHost = cluster.PrefixedName(vmv1beta1.ClusterComponentBalancer)
			vmPort = cluster.Spec.RequestsLoadBalancer.Spec.Port
			if vmPort == "" {
				vmPort = defaultVMSelectPort
			}
		}
		url := fmt.Sprintf("http://%s.%s.svc:%s/", vmHost, cluster.Namespace, vmPort)
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

func WaitForVMAuthReady(ctx context.Context, rclient client.Client, vmAuth *vmv1beta1.VMAuth, readyDeadline *metav1.Duration) error {
	defaultReadyDeadline := time.Minute
	if readyDeadline != nil {
		defaultReadyDeadline = readyDeadline.Duration
	}

	var lastStatus vmv1beta1.UpdateStatus
	// Fetch VMAuth in a loop until it has UpdateStatusOperational status
	err := wait.PollUntilContextTimeout(ctx, time.Second, defaultReadyDeadline, true, func(ctx context.Context) (done bool, err error) {
		if err := rclient.Get(ctx, types.NamespacedName{Name: vmAuth.Name, Namespace: vmAuth.Namespace}, vmAuth); err != nil {
			if k8serrors.IsNotFound(err) {
				return false, nil
			}
			return false, nil
		}
		lastStatus = vmAuth.Status.UpdateStatus
		return vmAuth.GetGeneration() == vmAuth.Status.ObservedGeneration && vmAuth.Status.UpdateStatus == vmv1beta1.UpdateStatusOperational, nil
	})
	if err != nil {
		return fmt.Errorf("failed to wait for VMAuth %s/%s to be ready: %w, current status: %s", vmAuth.Namespace, vmAuth.Name, err, lastStatus)
	}

	return nil
}
