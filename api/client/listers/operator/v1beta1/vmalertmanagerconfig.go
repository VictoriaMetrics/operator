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
// Code generated by lister-gen-v0.32. DO NOT EDIT.

package v1beta1

import (
	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	labels "k8s.io/apimachinery/pkg/labels"
	listers "k8s.io/client-go/listers"
	cache "k8s.io/client-go/tools/cache"
)

// VMAlertmanagerConfigLister helps list VMAlertmanagerConfigs.
// All objects returned here must be treated as read-only.
type VMAlertmanagerConfigLister interface {
	// List lists all VMAlertmanagerConfigs in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*operatorv1beta1.VMAlertmanagerConfig, err error)
	// VMAlertmanagerConfigs returns an object that can list and get VMAlertmanagerConfigs.
	VMAlertmanagerConfigs(namespace string) VMAlertmanagerConfigNamespaceLister
	VMAlertmanagerConfigListerExpansion
}

// vMAlertmanagerConfigLister implements the VMAlertmanagerConfigLister interface.
type vMAlertmanagerConfigLister struct {
	listers.ResourceIndexer[*operatorv1beta1.VMAlertmanagerConfig]
}

// NewVMAlertmanagerConfigLister returns a new VMAlertmanagerConfigLister.
func NewVMAlertmanagerConfigLister(indexer cache.Indexer) VMAlertmanagerConfigLister {
	return &vMAlertmanagerConfigLister{listers.New[*operatorv1beta1.VMAlertmanagerConfig](indexer, operatorv1beta1.Resource("vmalertmanagerconfig"))}
}

// VMAlertmanagerConfigs returns an object that can list and get VMAlertmanagerConfigs.
func (s *vMAlertmanagerConfigLister) VMAlertmanagerConfigs(namespace string) VMAlertmanagerConfigNamespaceLister {
	return vMAlertmanagerConfigNamespaceLister{listers.NewNamespaced[*operatorv1beta1.VMAlertmanagerConfig](s.ResourceIndexer, namespace)}
}

// VMAlertmanagerConfigNamespaceLister helps list and get VMAlertmanagerConfigs.
// All objects returned here must be treated as read-only.
type VMAlertmanagerConfigNamespaceLister interface {
	// List lists all VMAlertmanagerConfigs in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*operatorv1beta1.VMAlertmanagerConfig, err error)
	// Get retrieves the VMAlertmanagerConfig from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*operatorv1beta1.VMAlertmanagerConfig, error)
	VMAlertmanagerConfigNamespaceListerExpansion
}

// vMAlertmanagerConfigNamespaceLister implements the VMAlertmanagerConfigNamespaceLister
// interface.
type vMAlertmanagerConfigNamespaceLister struct {
	listers.ResourceIndexer[*operatorv1beta1.VMAlertmanagerConfig]
}
