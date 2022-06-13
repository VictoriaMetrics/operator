package v1beta1

import (
	"context"
	"fmt"

	metav1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDName int

const (
	Agent CRDName = iota
	Alert
	Single
	Cluster
	Auth
	AlertManager
)

func (c CRDName) String() string {
	return []string{"vmagents.operator.victoriametrics.com", "vmalerts.operator.victoriametrics.com", "vmsingles.operator.victoriametrics.com", "vmclusters.operator.victoriametrics.com", "vmauths.operator.victoriametrics.com", "vmalertmanagers.operator.victoriametrics.com"}[c]
}

type crdInfo struct {
	uuid       types.UID
	kind       string
	apiVersion string
}

var crdCache map[CRDName]*crdInfo

func Init(ctx context.Context, rclient client.Client) error {
	crdCache = make(map[CRDName]*crdInfo)
	var crds metav1.CustomResourceDefinitionList
	if err := rclient.List(ctx, &crds); err != nil {
		return fmt.Errorf("cannot list CRDs during init: %w", err)
	}
	for _, item := range crds.Items {

		var n CRDName
		switch item.Name {
		case "vmagents.operator.victoriametrics.com":
			n = Agent
		case "vmalerts.operator.victoriametrics.com":
			n = Alert
		case "vmsingles.operator.victoriametrics.com":
			n = Single
		case "vmclusters.operator.victoriametrics.com":
			n = Cluster
		case "vmauths.operator.victoriametrics.com":
			n = Auth
		case "vmalertmanagers.operator.victoriametrics.com":
			n = AlertManager
		default:
			continue
		}
		crdCache[n] = &crdInfo{
			uuid:       item.UID,
			apiVersion: metav1.SchemeGroupVersion.String(),
			kind:       "CustomResourceDefinition",
		}
	}
	return nil
}

func GetCRDAsOwner(name CRDName) []v1.OwnerReference {
	crdData := crdCache[name]
	if crdData == nil {
		return nil
	}
	return []v1.OwnerReference{
		{
			Name:       name.String(),
			UID:        crdData.uuid,
			Kind:       "CustomResourceDefinition",
			APIVersion: crdData.apiVersion,
		},
	}
}
