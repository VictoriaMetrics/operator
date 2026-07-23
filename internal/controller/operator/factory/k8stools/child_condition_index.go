package k8stools

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// ObjectWithConditions is satisfied by any CRD type exposing StatusMetadata.Conditions.
type ObjectWithConditions interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}

// ChildConditionIndexField is the field-indexer name reconcile.StatusForChildObjects uses
// to find children carrying a specific parent's Applied condition without listing every
// object of that kind. Every CRD kind passed to StatusForChildObjects must have
// ChildConditionIndexerFunc registered under this name on the manager.
const ChildConditionIndexField = "status.conditions.type"

// ChildConditionIndexerFunc indexes an object by every condition Type it currently carries.
func ChildConditionIndexerFunc(obj client.Object) []string {
	sw, ok := obj.(ObjectWithConditions)
	if !ok {
		return nil
	}
	conds := sw.GetStatusMetadata().Conditions
	if len(conds) == 0 {
		return nil
	}
	keys := make([]string, 0, len(conds))
	for _, c := range conds {
		keys = append(keys, c.Type)
	}
	return keys
}
