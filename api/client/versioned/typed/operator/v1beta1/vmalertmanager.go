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

// VMAlertmanagersGetter has a method to return a VMAlertmanagerInterface.
// A group's client should implement this interface.
type VMAlertmanagersGetter interface {
	VMAlertmanagers(namespace string) VMAlertmanagerInterface
}

// VMAlertmanagerInterface has methods to work with VMAlertmanager resources.
type VMAlertmanagerInterface interface {
	Create(ctx context.Context, vMAlertmanager *operatorv1beta1.VMAlertmanager, opts v1.CreateOptions) (*operatorv1beta1.VMAlertmanager, error)
	Update(ctx context.Context, vMAlertmanager *operatorv1beta1.VMAlertmanager, opts v1.UpdateOptions) (*operatorv1beta1.VMAlertmanager, error)
	// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
	UpdateStatus(ctx context.Context, vMAlertmanager *operatorv1beta1.VMAlertmanager, opts v1.UpdateOptions) (*operatorv1beta1.VMAlertmanager, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*operatorv1beta1.VMAlertmanager, error)
	List(ctx context.Context, opts v1.ListOptions) (*operatorv1beta1.VMAlertmanagerList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *operatorv1beta1.VMAlertmanager, err error)
	VMAlertmanagerExpansion
}

// vMAlertmanagers implements VMAlertmanagerInterface
type vMAlertmanagers struct {
	*gentype.ClientWithList[*operatorv1beta1.VMAlertmanager, *operatorv1beta1.VMAlertmanagerList]
}

// newVMAlertmanagers returns a VMAlertmanagers
func newVMAlertmanagers(c *OperatorV1beta1Client, namespace string) *vMAlertmanagers {
	return &vMAlertmanagers{
		gentype.NewClientWithList[*operatorv1beta1.VMAlertmanager, *operatorv1beta1.VMAlertmanagerList](
			"vmalertmanagers",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *operatorv1beta1.VMAlertmanager { return &operatorv1beta1.VMAlertmanager{} },
			func() *operatorv1beta1.VMAlertmanagerList { return &operatorv1beta1.VMAlertmanagerList{} },
		),
	}
}
