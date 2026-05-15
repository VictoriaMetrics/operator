package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
	k8stypes "k8s.io/apimachinery/pkg/types"
	watchapi "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/utils"
)

// ExpectObjectStatus perform assert on given object status
//
//nolint:dupl,lll
func ExpectObjectStatus(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name k8stypes.NamespacedName,
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

// WatchUntilStatusSeen reads events from w until it observes an object with the given name,
// observedGeneration >= minGen, and target status, or the context deadline is reached.
// Start the watch before triggering the action, then pass the post-action generation as minGen
// to avoid matching stale pre-action ADDED events that share the same name and status.
func WatchUntilStatusSeen(ctx context.Context, w watchapi.Interface, name string, minGen int64, targetStatus vmv1beta1.UpdateStatus) error {
	for {
		select {
		case event, ok := <-w.ResultChan():
			if !ok {
				return fmt.Errorf("watch closed before observing status %q for %q", targetStatus, name)
			}
			if event.Type == watchapi.Error {
				return fmt.Errorf("watch error: %v", event.Object)
			}
			obj, ok := event.Object.(client.Object)
			if !ok || obj.GetName() != name {
				continue
			}
			jsD, err := json.Marshal(obj)
			if err != nil {
				continue
			}
			type statusHolder struct {
				Status struct {
					vmv1beta1.StatusMetadata `json:",inline"`
				} `json:"status"`
			}
			var sh statusHolder
			if err := json.Unmarshal(jsD, &sh); err != nil {
				continue
			}
			if sh.Status.UpdateStatus == targetStatus && sh.Status.ObservedGeneration >= minGen {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for status %q for object %q: %w", targetStatus, name, ctx.Err())
		}
	}
}

func CollectK8SResources() {
	if !CurrentSpecReport().Failed() {
		return
	}
	err := utils.RunCrustGather(context.Background(), 10*time.Minute)
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Crust report generation failed: %v\n", err)
		Expect(err).NotTo(HaveOccurred())
	}
}
