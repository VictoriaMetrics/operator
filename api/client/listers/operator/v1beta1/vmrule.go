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
// Code generated by lister-gen-v0.31. DO NOT EDIT.

package v1beta1

import (
	v1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/listers"
	"k8s.io/client-go/tools/cache"
)

// VMRuleLister helps list VMRules.
// All objects returned here must be treated as read-only.
type VMRuleLister interface {
	// List lists all VMRules in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1beta1.VMRule, err error)
	// VMRules returns an object that can list and get VMRules.
	VMRules(namespace string) VMRuleNamespaceLister
	VMRuleListerExpansion
}

// vMRuleLister implements the VMRuleLister interface.
type vMRuleLister struct {
	listers.ResourceIndexer[*v1beta1.VMRule]
}

// NewVMRuleLister returns a new VMRuleLister.
func NewVMRuleLister(indexer cache.Indexer) VMRuleLister {
	return &vMRuleLister{listers.New[*v1beta1.VMRule](indexer, v1beta1.Resource("vmrule"))}
}

// VMRules returns an object that can list and get VMRules.
func (s *vMRuleLister) VMRules(namespace string) VMRuleNamespaceLister {
	return vMRuleNamespaceLister{listers.NewNamespaced[*v1beta1.VMRule](s.ResourceIndexer, namespace)}
}

// VMRuleNamespaceLister helps list and get VMRules.
// All objects returned here must be treated as read-only.
type VMRuleNamespaceLister interface {
	// List lists all VMRules in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1beta1.VMRule, err error)
	// Get retrieves the VMRule from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1beta1.VMRule, error)
	VMRuleNamespaceListerExpansion
}

// vMRuleNamespaceLister implements the VMRuleNamespaceLister
// interface.
type vMRuleNamespaceLister struct {
	listers.ResourceIndexer[*v1beta1.VMRule]
}
