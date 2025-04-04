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

// fakeVMClusters implements VMClusterInterface
type fakeVMClusters struct {
	*gentype.FakeClientWithList[*v1beta1.VMCluster, *v1beta1.VMClusterList]
	Fake *FakeOperatorV1beta1
}

func newFakeVMClusters(fake *FakeOperatorV1beta1, namespace string) operatorv1beta1.VMClusterInterface {
	return &fakeVMClusters{
		gentype.NewFakeClientWithList[*v1beta1.VMCluster, *v1beta1.VMClusterList](
			fake.Fake,
			namespace,
			v1beta1.SchemeGroupVersion.WithResource("vmclusters"),
			v1beta1.SchemeGroupVersion.WithKind("VMCluster"),
			func() *v1beta1.VMCluster { return &v1beta1.VMCluster{} },
			func() *v1beta1.VMClusterList { return &v1beta1.VMClusterList{} },
			func(dst, src *v1beta1.VMClusterList) { dst.ListMeta = src.ListMeta },
			func(list *v1beta1.VMClusterList) []*v1beta1.VMCluster { return gentype.ToPointerSlice(list.Items) },
			func(list *v1beta1.VMClusterList, items []*v1beta1.VMCluster) {
				list.Items = gentype.FromPointerSlice(items)
			},
		),
		fake,
	}
}
