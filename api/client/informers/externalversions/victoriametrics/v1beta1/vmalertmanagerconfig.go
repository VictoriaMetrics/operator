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
// Code generated by informer-gen. DO NOT EDIT.

package v1beta1

import (
	"context"
	time "time"

	internalinterfaces "github.com/VictoriaMetrics/operator/api/client/informers/externalversions/internalinterfaces"
	v1beta1 "github.com/VictoriaMetrics/operator/api/client/listers/victoriametrics/v1beta1"
	versioned "github.com/VictoriaMetrics/operator/api/client/versioned"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/victoriametrics/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// VMAlertmanagerConfigInformer provides access to a shared informer and lister for
// VMAlertmanagerConfigs.
type VMAlertmanagerConfigInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1beta1.VMAlertmanagerConfigLister
}

type vMAlertmanagerConfigInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewVMAlertmanagerConfigInformer constructs a new informer for VMAlertmanagerConfig type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewVMAlertmanagerConfigInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredVMAlertmanagerConfigInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredVMAlertmanagerConfigInformer constructs a new informer for VMAlertmanagerConfig type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredVMAlertmanagerConfigInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.VictoriametricsV1beta1().VMAlertmanagerConfigs(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.VictoriametricsV1beta1().VMAlertmanagerConfigs(namespace).Watch(context.TODO(), options)
			},
		},
		&victoriametricsv1beta1.VMAlertmanagerConfig{},
		resyncPeriod,
		indexers,
	)
}

func (f *vMAlertmanagerConfigInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredVMAlertmanagerConfigInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *vMAlertmanagerConfigInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&victoriametricsv1beta1.VMAlertmanagerConfig{}, f.defaultInformer)
}

func (f *vMAlertmanagerConfigInformer) Lister() v1beta1.VMAlertmanagerConfigLister {
	return v1beta1.NewVMAlertmanagerConfigLister(f.Informer().GetIndexer())
}
