package finalize

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// OnVMDistributedClusterDelete removes all objects related to vmdistributedcluster component
func OnVMDistributedClusterDelete(ctx context.Context, rclient client.Client, obj *vmv1alpha1.VMDistributedCluster) error {
	// Remove created VMAgent
	vmagent := &vmv1beta1.VMAgent{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: obj.Spec.VMAgent.Name, Namespace: obj.Namespace}, vmagent); err == nil {
		if err := rclient.Delete(ctx, vmagent); err != nil {
			return err
		}
	}

	lbPrefixedName := obj.PrefixedName(vmv1beta1.ClusterComponentBalancer)
	// Remove vmauthlb deployment
	vmauthlb := &appsv1.Deployment{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: lbPrefixedName, Namespace: obj.Namespace}, vmauthlb); err == nil {
		if err := rclient.Delete(ctx, vmauthlb); err != nil {
			return err
		}
	}

	// Remove vmauthlb service
	vmauthlbService := &corev1.Service{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: obj.Spec.VMAuth.Name, Namespace: obj.Namespace}, vmauthlbService); err == nil {
		if err := rclient.Delete(ctx, vmauthlbService); err != nil {
			return err
		}
	}

	// Remove vmauthlb serviceaccount
	vmauthlbServiceAccount := &corev1.ServiceAccount{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: obj.GetServiceAccountName(), Namespace: obj.Namespace}, vmauthlbServiceAccount); err == nil {
		if err := rclient.Delete(ctx, vmauthlbServiceAccount); err != nil {
			return err
		}
	}

	// Remove vmauthlb secret
	vmauthlbSecret := &corev1.Secret{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: lbPrefixedName, Namespace: obj.Namespace}, vmauthlbSecret); err == nil {
		if err := rclient.Delete(ctx, vmauthlbSecret); err != nil {
			return err
		}
	}

	// Remove vmauthlb servicescraper
	vmauthlbServiceScraper := &vmv1beta1.VMServiceScrape{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: obj.Spec.VMAuth.Name, Namespace: obj.Namespace}, vmauthlbServiceScraper); err == nil {
		if err := rclient.Delete(ctx, vmauthlbServiceScraper); err != nil {
			return err
		}
	}

	// Remove vmauthlb PDB
	vmauthlbPDB := &policyv1.PodDisruptionBudget{}
	if err := rclient.Get(ctx, client.ObjectKey{Name: lbPrefixedName, Namespace: obj.Namespace}, vmauthlbPDB); err == nil {
		if err := rclient.Delete(ctx, vmauthlbPDB); err != nil {
			return err
		}
	}

	// Remove created VMClusters
	for _, vmclusterSpec := range obj.Spec.Zones.VMClusters {
		if vmclusterSpec.Ref != nil {
			continue
		}
		vmcluster := &vmv1beta1.VMCluster{}
		if err := rclient.Get(ctx, client.ObjectKey{Name: vmclusterSpec.Name, Namespace: obj.Namespace}, vmcluster); err == nil {
			if err := rclient.Delete(ctx, vmcluster); err != nil {
				return err
			}
		}
	}

	return nil
}
