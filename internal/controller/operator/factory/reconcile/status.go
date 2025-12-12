package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

var (
	statusUpdateTTL = 60 * time.Minute
	statusExpireTTL = 3 * statusUpdateTTL
)

// SetStatusUpdateTTL configures TTL for LastUpdateTime field
//
// Higher value decreases load on Kubernetes API server
func SetStatusUpdateTTL(v time.Duration) {
	statusExpireTTL = v
}

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
	for _, childObject := range childObjects {
		// update current time on each cycle
		// due to possible throttling at API server
		ctm := metav1.Now()
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
	return retryOnConflict(func() error {
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
	// it also reduce probability of concurrent update requests
	ttl := jitterForDuration(statusUpdateTTL)
	for idx, c := range dst {
		if c.Type == cond.Type {
			var forceLastTimeUpdate bool
			if c.Status == cond.Status {
				cond.LastTransitionTime = c.LastTransitionTime
			} else {
				forceLastTimeUpdate = true
			}
			if c.ObservedGeneration != cond.ObservedGeneration {
				forceLastTimeUpdate = true
			}
			if time.Since(c.LastUpdateTime.Time) <= ttl && !forceLastTimeUpdate {
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
	// it also reduce probability of concurrent update requests
	ttl := statusExpireTTL + jitterForDuration(statusUpdateTTL)
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

// adds 50% jitter to the given duration
func jitterForDuration(d time.Duration) time.Duration {
	dv := d / 2
	p := float64(rand.Uint32()) / (1 << 32)
	return d + time.Duration(p*float64(dv))
}

type ObjectWithDeepCopyAndStatus[T client.Object, ST StatusWithMetadata[STC], STC any] interface {
	client.Object
	DeepCopy() T
	GetStatus() ST
	DefaultStatusFields(ST)
}

// StatusWithMetadata defines
type StatusWithMetadata[T any] interface {
	DeepCopy() T
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}

// UpdateStatus reconcile provided object status with given actualStatus status
func UpdateObjectStatus[T client.Object, ST StatusWithMetadata[STC], STC any](ctx context.Context, rclient client.Client, object ObjectWithDeepCopyAndStatus[T, ST, STC], actualStatus vmv1beta1.UpdateStatus, maybeErr error) error {
	currentStatus := object.GetStatus()
	prevStatus := currentStatus.DeepCopy()
	currMeta := currentStatus.GetStatusMetadata()
	newUpdateStatus := actualStatus

	switch actualStatus {
	case vmv1beta1.UpdateStatusExpanding, vmv1beta1.UpdateStatusOperational:
		currMeta.Reason = ""
	case vmv1beta1.UpdateStatusPaused:
	case vmv1beta1.UpdateStatusFailed:
		if maybeErr != nil {
			currMeta.Reason = maybeErr.Error()
		}
	default:
		panic(fmt.Sprintf("BUG: not expected status=%q", actualStatus))
	}

	currMeta.ObservedGeneration = object.GetGeneration()
	// compare before send update request
	// it reduces load at kubernetes api-server
	if equality.Semantic.DeepEqual(currentStatus, prevStatus) && currMeta.UpdateStatus == actualStatus {
		return nil
	}
	currMeta.UpdateStatus = newUpdateStatus
	object.DefaultStatusFields(currentStatus)

	// make a deep copy before passing object to Patch function
	// it reload state of the object from API server
	// which is not desired behavior
	objectToUpdate := object.DeepCopy()
	pr, err := buildStatusPatch(currentStatus)
	if err != nil {
		return err
	}
	if err := rclient.Status().Patch(ctx, objectToUpdate, pr); err != nil {
		return fmt.Errorf("cannot update resource status with patch: %w", err)
	}
	// Update ResourceVersion in order to resolve future conflicts
	object.SetResourceVersion(objectToUpdate.GetResourceVersion())

	return nil
}

func buildStatusPatch(currentStatus any) (client.Patch, error) {
	type patch struct {
		OP    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	ops := []patch{
		{
			OP:    "replace",
			Path:  "/status",
			Value: currentStatus,
		},
	}
	data, err := json.Marshal(ops)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize patch specification as json :%w", err)
	}

	return client.RawPatch(types.JSONPatchType, data), nil

}
