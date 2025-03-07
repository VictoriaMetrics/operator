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

// fakeVMAgents implements VMAgentInterface
type fakeVMAgents struct {
	*gentype.FakeClientWithList[*v1beta1.VMAgent, *v1beta1.VMAgentList]
	Fake *FakeOperatorV1beta1
}

func newFakeVMAgents(fake *FakeOperatorV1beta1, namespace string) operatorv1beta1.VMAgentInterface {
	return &fakeVMAgents{
		gentype.NewFakeClientWithList[*v1beta1.VMAgent, *v1beta1.VMAgentList](
			fake.Fake,
			namespace,
			v1beta1.SchemeGroupVersion.WithResource("vmagents"),
			v1beta1.SchemeGroupVersion.WithKind("VMAgent"),
			func() *v1beta1.VMAgent { return &v1beta1.VMAgent{} },
			func() *v1beta1.VMAgentList { return &v1beta1.VMAgentList{} },
			func(dst, src *v1beta1.VMAgentList) { dst.ListMeta = src.ListMeta },
			func(list *v1beta1.VMAgentList) []*v1beta1.VMAgent { return gentype.ToPointerSlice(list.Items) },
			func(list *v1beta1.VMAgentList, items []*v1beta1.VMAgent) {
				list.Items = gentype.FromPointerSlice(items)
			},
		),
		fake,
	}
}
