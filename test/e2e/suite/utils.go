package suite

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operator "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// ExpectObjectStatus perform assert on given object status
//
//nolint:dupl,lll
func ExpectObjectStatus(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName,
	status operator.UpdateStatus) error {
	if err := rclient.Get(ctx, name, object); err != nil {
		return err
	}
	jsD, err := json.Marshal(object)
	if err != nil {
		return err
	}
	type objectStatus struct {
		Status struct {
			operator.StatusMetadata `json:",inline"`
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
		return fmt.Errorf("not expected object status=%q", obs.Status.UpdateStatus)
	}

	return nil
}
