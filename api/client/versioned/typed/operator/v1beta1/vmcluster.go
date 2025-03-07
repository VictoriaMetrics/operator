/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
// Code generated by client-gen-v0.32. DO NOT EDIT.

package v1beta1

import (
	context "context"

	scheme "github.com/VictoriaMetrics/operator/api/client/versioned/scheme"
	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	gentype "k8s.io/client-go/gentype"
)

// VMClustersGetter has a method to return a VMClusterInterface.
// A group's client should implement this interface.
type VMClustersGetter interface {
	VMClusters(namespace string) VMClusterInterface
}

// VMClusterInterface has methods to work with VMCluster resources.
type VMClusterInterface interface {
	Create(ctx context.Context, vMCluster *operatorv1beta1.VMCluster, opts v1.CreateOptions) (*operatorv1beta1.VMCluster, error)
	Update(ctx context.Context, vMCluster *operatorv1beta1.VMCluster, opts v1.UpdateOptions) (*operatorv1beta1.VMCluster, error)
	// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
	UpdateStatus(ctx context.Context, vMCluster *operatorv1beta1.VMCluster, opts v1.UpdateOptions) (*operatorv1beta1.VMCluster, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*operatorv1beta1.VMCluster, error)
	List(ctx context.Context, opts v1.ListOptions) (*operatorv1beta1.VMClusterList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *operatorv1beta1.VMCluster, err error)
	VMClusterExpansion
}

// vMClusters implements VMClusterInterface
type vMClusters struct {
	*gentype.ClientWithList[*operatorv1beta1.VMCluster, *operatorv1beta1.VMClusterList]
}

// newVMClusters returns a VMClusters
func newVMClusters(c *OperatorV1beta1Client, namespace string) *vMClusters {
	return &vMClusters{
		gentype.NewClientWithList[*operatorv1beta1.VMCluster, *operatorv1beta1.VMClusterList](
			"vmclusters",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *operatorv1beta1.VMCluster { return &operatorv1beta1.VMCluster{} },
			func() *operatorv1beta1.VMClusterList { return &operatorv1beta1.VMClusterList{} },
		),
	}
}
