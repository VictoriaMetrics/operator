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
// Code generated by lister-gen-v0.30. DO NOT EDIT.

package v1beta1

import (
	v1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// VMPodScrapeLister helps list VMPodScrapes.
// All objects returned here must be treated as read-only.
type VMPodScrapeLister interface {
	// List lists all VMPodScrapes in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1beta1.VMPodScrape, err error)
	// VMPodScrapes returns an object that can list and get VMPodScrapes.
	VMPodScrapes(namespace string) VMPodScrapeNamespaceLister
	VMPodScrapeListerExpansion
}

// vMPodScrapeLister implements the VMPodScrapeLister interface.
type vMPodScrapeLister struct {
	indexer cache.Indexer
}

// NewVMPodScrapeLister returns a new VMPodScrapeLister.
func NewVMPodScrapeLister(indexer cache.Indexer) VMPodScrapeLister {
	return &vMPodScrapeLister{indexer: indexer}
}

// List lists all VMPodScrapes in the indexer.
func (s *vMPodScrapeLister) List(selector labels.Selector) (ret []*v1beta1.VMPodScrape, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.VMPodScrape))
	})
	return ret, err
}

// VMPodScrapes returns an object that can list and get VMPodScrapes.
func (s *vMPodScrapeLister) VMPodScrapes(namespace string) VMPodScrapeNamespaceLister {
	return vMPodScrapeNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// VMPodScrapeNamespaceLister helps list and get VMPodScrapes.
// All objects returned here must be treated as read-only.
type VMPodScrapeNamespaceLister interface {
	// List lists all VMPodScrapes in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1beta1.VMPodScrape, err error)
	// Get retrieves the VMPodScrape from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1beta1.VMPodScrape, error)
	VMPodScrapeNamespaceListerExpansion
}

// vMPodScrapeNamespaceLister implements the VMPodScrapeNamespaceLister
// interface.
type vMPodScrapeNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all VMPodScrapes in the indexer for a given namespace.
func (s vMPodScrapeNamespaceLister) List(selector labels.Selector) (ret []*v1beta1.VMPodScrape, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.VMPodScrape))
	})
	return ret, err
}

// Get retrieves the VMPodScrape from the indexer for a given namespace and name.
func (s vMPodScrapeNamespaceLister) Get(name string) (*v1beta1.VMPodScrape, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1beta1.Resource("vmpodscrape"), name)
	}
	return obj.(*v1beta1.VMPodScrape), nil
}
