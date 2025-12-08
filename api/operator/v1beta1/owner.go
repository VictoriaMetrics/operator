package v1beta1

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDName int

const (
	VMAgentCRD CRDName = iota
	VLAgentCRD
)

func (c CRDName) String() string {
	return []string{
		"vmagents.operator.victoriametrics.com",
		"vlagents.operator.victoriametrics.com",
	}[c]
}

type crdInfo struct {
	uuid       types.UID
	kind       string
	apiVersion string
}

var crdCache map[CRDName]*crdInfo

func Init(ctx context.Context, rclient client.Client) error {
	crdCache = make(map[CRDName]*crdInfo)
	var crds apiextensionsv1.CustomResourceDefinitionList
	if err := rclient.List(ctx, &crds); err != nil {
		return fmt.Errorf("cannot list CRDs during init: %w", err)
	}
	for _, item := range crds.Items {

		var n CRDName
		switch item.Name {
		case "vmagents.operator.victoriametrics.com":
			n = VMAgentCRD
		case "vlagents.operator.victoriametrics.com":
			n = VLAgentCRD
		default:
			continue
		}
		crdCache[n] = &crdInfo{
			uuid:       item.UID,
			apiVersion: apiextensionsv1.SchemeGroupVersion.String(),
			kind:       "CustomResourceDefinition",
		}
	}
	return nil
}

// GetCRDAsOwner returns owner references with global CustomResourceDefinition object as owner
// useful for non-namespaced objects, like clusterRole
func GetCRDAsOwner(name CRDName) *metav1.OwnerReference {
	crdData := crdCache[name]
	if crdData == nil {
		return nil
	}
	return &metav1.OwnerReference{
		Name:       name.String(),
		UID:        crdData.uuid,
		Kind:       "CustomResourceDefinition",
		APIVersion: crdData.apiVersion,
	}
}
