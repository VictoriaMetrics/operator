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
// Code generated by client-gen-v0.30. DO NOT EDIT.

package v1beta1

import (
	"context"
	"time"

	scheme "github.com/VictoriaMetrics/operator/api/client/versioned/scheme"
	v1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// VMAlertsGetter has a method to return a VMAlertInterface.
// A group's client should implement this interface.
type VMAlertsGetter interface {
	VMAlerts(namespace string) VMAlertInterface
}

// VMAlertInterface has methods to work with VMAlert resources.
type VMAlertInterface interface {
	Create(ctx context.Context, vMAlert *v1beta1.VMAlert, opts v1.CreateOptions) (*v1beta1.VMAlert, error)
	Update(ctx context.Context, vMAlert *v1beta1.VMAlert, opts v1.UpdateOptions) (*v1beta1.VMAlert, error)
	UpdateStatus(ctx context.Context, vMAlert *v1beta1.VMAlert, opts v1.UpdateOptions) (*v1beta1.VMAlert, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1beta1.VMAlert, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1beta1.VMAlertList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta1.VMAlert, err error)
	VMAlertExpansion
}

// vMAlerts implements VMAlertInterface
type vMAlerts struct {
	client rest.Interface
	ns     string
}

// newVMAlerts returns a VMAlerts
func newVMAlerts(c *OperatorV1beta1Client, namespace string) *vMAlerts {
	return &vMAlerts{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the vMAlert, and returns the corresponding vMAlert object, and an error if there is any.
func (c *vMAlerts) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1beta1.VMAlert, err error) {
	result = &v1beta1.VMAlert{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("vmalerts").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of VMAlerts that match those selectors.
func (c *vMAlerts) List(ctx context.Context, opts v1.ListOptions) (result *v1beta1.VMAlertList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1beta1.VMAlertList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("vmalerts").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested vMAlerts.
func (c *vMAlerts) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("vmalerts").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a vMAlert and creates it.  Returns the server's representation of the vMAlert, and an error, if there is any.
func (c *vMAlerts) Create(ctx context.Context, vMAlert *v1beta1.VMAlert, opts v1.CreateOptions) (result *v1beta1.VMAlert, err error) {
	result = &v1beta1.VMAlert{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("vmalerts").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(vMAlert).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a vMAlert and updates it. Returns the server's representation of the vMAlert, and an error, if there is any.
func (c *vMAlerts) Update(ctx context.Context, vMAlert *v1beta1.VMAlert, opts v1.UpdateOptions) (result *v1beta1.VMAlert, err error) {
	result = &v1beta1.VMAlert{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("vmalerts").
		Name(vMAlert.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(vMAlert).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *vMAlerts) UpdateStatus(ctx context.Context, vMAlert *v1beta1.VMAlert, opts v1.UpdateOptions) (result *v1beta1.VMAlert, err error) {
	result = &v1beta1.VMAlert{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("vmalerts").
		Name(vMAlert.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(vMAlert).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the vMAlert and deletes it. Returns an error if one occurs.
func (c *vMAlerts) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("vmalerts").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *vMAlerts) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("vmalerts").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched vMAlert.
func (c *vMAlerts) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta1.VMAlert, err error) {
	result = &v1beta1.VMAlert{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("vmalerts").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
