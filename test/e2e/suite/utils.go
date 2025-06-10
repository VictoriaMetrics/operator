package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// ExpectObjectStatus perform assert on given object status
//
//nolint:dupl,lll
func ExpectObjectStatus(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName,
	status vmv1beta1.UpdateStatus) error {
	if err := rclient.Get(ctx, name, object); err != nil {
		return err
	}
	jsD, err := json.Marshal(object)
	if err != nil {
		return err
	}
	type objectStatus struct {
		Status struct {
			vmv1beta1.StatusMetadata `json:",inline"`
		} `json:"status"`
	}
	var obs objectStatus
	if err := json.Unmarshal(jsD, &obs); err != nil {
		return err
	}
	if object.GetGeneration() > obs.Status.ObservedGeneration {
		return fmt.Errorf("expected generation: %d be greater than: %d", obs.Status.ObservedGeneration, object.GetGeneration())
	}
	if obs.Status.UpdateStatus != status {
		var conds []string
		for _, cond := range obs.Status.Conditions {
			conds = append(conds, fmt.Sprintf("type=%s,message=%q,generation=%d,status=%q", cond.Type, cond.Message, cond.ObservedGeneration, cond.Status))
		}
		return fmt.Errorf("not expected object status=%q, reason=%q,conditions=%s", obs.Status.UpdateStatus, obs.Status.Reason, strings.Join(conds, ","))
	}

	return nil
}
