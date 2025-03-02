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
// Code generated by client-gen-v0.31. DO NOT EDIT.

package v1beta1

import (
	"context"

	scheme "github.com/VictoriaMetrics/operator/api/client/versioned/scheme"
	v1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	gentype "k8s.io/client-go/gentype"
)

// VMPodScrapesGetter has a method to return a VMPodScrapeInterface.
// A group's client should implement this interface.
type VMPodScrapesGetter interface {
	VMPodScrapes(namespace string) VMPodScrapeInterface
}

// VMPodScrapeInterface has methods to work with VMPodScrape resources.
type VMPodScrapeInterface interface {
	Create(ctx context.Context, vMPodScrape *v1beta1.VMPodScrape, opts v1.CreateOptions) (*v1beta1.VMPodScrape, error)
	Update(ctx context.Context, vMPodScrape *v1beta1.VMPodScrape, opts v1.UpdateOptions) (*v1beta1.VMPodScrape, error)
	// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
	UpdateStatus(ctx context.Context, vMPodScrape *v1beta1.VMPodScrape, opts v1.UpdateOptions) (*v1beta1.VMPodScrape, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1beta1.VMPodScrape, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1beta1.VMPodScrapeList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta1.VMPodScrape, err error)
	VMPodScrapeExpansion
}

// vMPodScrapes implements VMPodScrapeInterface
type vMPodScrapes struct {
	*gentype.ClientWithList[*v1beta1.VMPodScrape, *v1beta1.VMPodScrapeList]
}

// newVMPodScrapes returns a VMPodScrapes
func newVMPodScrapes(c *OperatorV1beta1Client, namespace string) *vMPodScrapes {
	return &vMPodScrapes{
		gentype.NewClientWithList[*v1beta1.VMPodScrape, *v1beta1.VMPodScrapeList](
			"vmpodscrapes",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *v1beta1.VMPodScrape { return &v1beta1.VMPodScrape{} },
			func() *v1beta1.VMPodScrapeList { return &v1beta1.VMPodScrapeList{} }),
	}
}
