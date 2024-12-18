package reconcile

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

const (
	// TODO: @f41gh7 make configurable
	statusUpdateTTL = 7 * time.Minute
	statusExpireTTL = 20 * time.Minute
)

type objectWithStatus interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}

// StatusForChildObjects reconciles status sub-resources
// Expects parentObjectName in the following form:
// NAME.NAMESPACE.RESOURCE
//
// for example vmalertmanager `main` at namespace `monitoring` must be in form:
// main.monitoring.vmalertmanager
func StatusForChildObjects[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, parentObjectName string, childObjects []PT) error {
	var errors []string

	n := strings.Split(parentObjectName, ".")
	if len(n) != 3 {
		panic(fmt.Sprintf("BUG: unexpected format for parentObjectName=%q, want name.namespace.resource", parentObjectName))
	}
	typeName := parentObjectName + vmv1beta1.ConditionDomainTypeAppliedSuffix
	ctm := metav1.Now()
	for _, childObject := range childObjects {
		st := childObject.GetStatusMetadata()
		currCound := vmv1beta1.Condition{
			Type:               typeName,
			Reason:             vmv1beta1.ConditionParsingReason,
			LastTransitionTime: ctm,
			LastUpdateTime:     ctm,
			ObservedGeneration: childObject.GetGeneration(),
		}
		if st.CurrentSyncError == "" {
			currCound.Status = "True"
		} else {
			currCound.Status = "False"
			currCound.Message = st.CurrentSyncError
			errors = append(errors, fmt.Sprintf("parent=%s config=namespace/name=%s/%s error text: %s", parentObjectName, childObject.GetNamespace(), childObject.GetName(), st.CurrentSyncError))
		}
		if err := updateChildStatusConditions[T](ctx, rclient, childObject, currCound); err != nil {
			return err
		}
	}
	if len(errors) > 0 {
		logger.WithContext(ctx).Error(fmt.Errorf("%s have errors", parentObjectName), fmt.Sprintf("skip config generation for resources: %s", strings.Join(errors, ",")))
	}
	return nil
}

func updateChildStatusConditions[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, childObject PT, currCond vmv1beta1.Condition) error {
	nsn := types.NamespacedName{
		Namespace: childObject.GetNamespace(),
		Name:      childObject.GetName(),
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		dst := PT(new(T))
		if err := rclient.Get(ctx, nsn, dst); err != nil {
			return err
		}
		st := dst.GetStatusMetadata()
		prevSt := st.DeepCopy()

		st.Conditions = setConditionTo(st.Conditions, currCond)
		st.Conditions = removeStaleConditionsBySuffix(st.Conditions, vmv1beta1.ConditionDomainTypeAppliedSuffix)
		st.ObservedGeneration = dst.GetGeneration()
		writeAggregatedStatus(st, vmv1beta1.ConditionDomainTypeAppliedSuffix)
		if !reflect.DeepEqual(prevSt, st) {
			if err := rclient.Status().Update(ctx, dst); err != nil {
				return fmt.Errorf("failed to patch status of broken VMAlertmanagerConfig=%q: %w", childObject.GetName(), err)
			}
		}
		return nil
	})
}

func setConditionTo(dst []vmv1beta1.Condition, cond vmv1beta1.Condition) []vmv1beta1.Condition {
	// update TTL with jitter in order to reduce load on kubernetes API server
	// jitter should cover configured resync period (60s default value)
	// it also reduce propbability of concurrent update requests
	jitter := jitterForDuration(2 * time.Minute)
	ttl := statusUpdateTTL + jitter
	for idx, c := range dst {
		if c.Type == cond.Type {
			if c.Status == cond.Status {
				cond.LastTransitionTime = c.LastTransitionTime
			}
			if time.Since(c.LastUpdateTime.Time) <= ttl {
				cond.LastUpdateTime = c.LastUpdateTime
			}
			dst[idx] = cond
			return dst
		}
	}
	dst = append(dst, cond)
	return dst
}

func removeStaleConditionsBySuffix(src []vmv1beta1.Condition, domainTypeSuffix string) []vmv1beta1.Condition {
	tmp := src[:0]
	// update TTL with jitter in order to reduce load on kubernetes API server
	// jitter should cover configured resync period (60s default value)
	// it also reduce propbability of concurrent update requests
	jitter := jitterForDuration(3 * time.Minute)
	ttl := statusExpireTTL + jitter
	for _, cond := range src {
		if strings.HasSuffix(cond.Type, domainTypeSuffix) {
			if time.Since(cond.LastUpdateTime.Time) > ttl {
				continue
			}
		}
		tmp = append(tmp, cond)
	}
	return tmp
}

func writeAggregatedStatus(stm *vmv1beta1.StatusMetadata, domainTypeSuffix string) {
	var errorMessages []string
	for _, c := range stm.Conditions {
		if strings.HasSuffix(c.Type, domainTypeSuffix) && c.Status == "False" {
			errorMessages = append(errorMessages, c.Message)
		}
	}
	sort.Strings(errorMessages)
	stm.UpdateStatus = vmv1beta1.UpdateStatusOperational
	stm.Reason = ""
	if len(errorMessages) > 0 {
		stm.UpdateStatus = vmv1beta1.UpdateStatusFailed
		stm.Reason = errorMessages[0]
	}
}

func jitterForDuration(d time.Duration) time.Duration {
	dv := d / 2
	p := float64(rand.Uint32()) / (1 << 32)
	return time.Duration(p * float64(dv))
}
