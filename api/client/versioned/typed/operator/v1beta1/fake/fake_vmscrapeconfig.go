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

package fake

import (
	operatorv1beta1 "github.com/VictoriaMetrics/operator/api/client/versioned/typed/operator/v1beta1"
	v1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	gentype "k8s.io/client-go/gentype"
)

// fakeVMScrapeConfigs implements VMScrapeConfigInterface
type fakeVMScrapeConfigs struct {
	*gentype.FakeClientWithList[*v1beta1.VMScrapeConfig, *v1beta1.VMScrapeConfigList]
	Fake *FakeOperatorV1beta1
}

func newFakeVMScrapeConfigs(fake *FakeOperatorV1beta1, namespace string) operatorv1beta1.VMScrapeConfigInterface {
	return &fakeVMScrapeConfigs{
		gentype.NewFakeClientWithList[*v1beta1.VMScrapeConfig, *v1beta1.VMScrapeConfigList](
			fake.Fake,
			namespace,
			v1beta1.SchemeGroupVersion.WithResource("vmscrapeconfigs"),
			v1beta1.SchemeGroupVersion.WithKind("VMScrapeConfig"),
			func() *v1beta1.VMScrapeConfig { return &v1beta1.VMScrapeConfig{} },
			func() *v1beta1.VMScrapeConfigList { return &v1beta1.VMScrapeConfigList{} },
			func(dst, src *v1beta1.VMScrapeConfigList) { dst.ListMeta = src.ListMeta },
			func(list *v1beta1.VMScrapeConfigList) []*v1beta1.VMScrapeConfig {
				return gentype.ToPointerSlice(list.Items)
			},
			func(list *v1beta1.VMScrapeConfigList, items []*v1beta1.VMScrapeConfig) {
				list.Items = gentype.FromPointerSlice(items)
			},
		),
		fake,
	}
}
