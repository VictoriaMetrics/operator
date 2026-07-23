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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

var (
	statusUpdateTTL = 60 * time.Minute
	statusExpireTTL = 3 * statusUpdateTTL
)

type objectWithStatus interface {
	client.Object
	GetStatusMetadata() *vmv1beta1.StatusMetadata
}

func childConditionType(parentObjectName string) string {
	if len(strings.Split(parentObjectName, ".")) != 3 {
		panic(fmt.Sprintf("BUG: unexpected format for parentObjectName=%q, want name.namespace.resource", parentObjectName))
	}
	return parentObjectName + vmv1beta1.ConditionDomainTypeAppliedSuffix
}

// StatusForChildObjects reconciles status sub-resources for the full, current set of
// child objects selected by a parent.
// Expects parentObjectName in the following form:
// NAME.NAMESPACE.RESOURCE
//
// for example vmalertmanager `main` at namespace `monitoring` must be in form:
// main.monitoring.vmalertmanager
//
// Since childObjects is the parent's complete current selection, any other child of the
// same kind still carrying this parent's condition has it released.
func StatusForChildObjects[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, parentObjectName string, childObjects []PT) error {
	typeName := childConditionType(parentObjectName)

	current := make(map[types.NamespacedName]struct{}, len(childObjects))
	for _, o := range childObjects {
		current[types.NamespacedName{Namespace: o.GetNamespace(), Name: o.GetName()}] = struct{}{}
	}
	releaseErr := releaseDroppedChildren[T, PT](ctx, rclient, typeName, current)

	var errs []error
	for _, childObject := range childObjects {
		syncErr, err := applyChildStatusCondition[T](ctx, rclient, childObject, parentObjectName, typeName)
		if err != nil {
			return err
		}
		if syncErr != nil {
			errs = append(errs, syncErr)
		}
	}
	if aggErr := utilerrors.NewAggregate(errs); aggErr != nil {
		logger.WithContext(ctx).Error(aggErr, fmt.Sprintf("%s skip config generation for resources", parentObjectName))
	}
	if releaseErr != nil {
		return fmt.Errorf("%s cannot release stale child status conditions: %w", parentObjectName, releaseErr)
	}
	return nil
}

// StatusForChildObject is a fast path for updating a single child object that triggered
// the reconcile, bypassing the release-on-drop tracking used by StatusForChildObjects
// since it doesn't represent the parent's complete current selection.
func StatusForChildObject[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, parentObjectName string, childObject PT) error {
	typeName := childConditionType(parentObjectName)
	syncErr, err := applyChildStatusCondition[T](ctx, rclient, childObject, parentObjectName, typeName)
	if err != nil {
		return err
	}
	if syncErr != nil {
		logger.WithContext(ctx).Error(syncErr, fmt.Sprintf("%s skip config generation for resource", parentObjectName))
	}
	return nil
}

func applyChildStatusCondition[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, childObject PT, parentObjectName, typeName string) (syncErr, err error) {
	// update current time on each cycle
	// due to possible throttling at API server
	ctm := metav1.Now()
	st := childObject.GetStatusMetadata()
	cond := vmv1beta1.Condition{
		Type:               typeName,
		Reason:             vmv1beta1.ConditionParsingReason,
		LastTransitionTime: ctm,
		LastUpdateTime:     ctm,
		ObservedGeneration: childObject.GetGeneration(),
	}
	if st.CurrentSyncError == "" {
		cond.Status = "True"
	} else {
		cond.Status = "False"
		cond.Message = st.CurrentSyncError
		syncErr = fmt.Errorf("parent=%s config=namespace/name=%s/%s error text: %s", parentObjectName, childObject.GetNamespace(), childObject.GetName(), st.CurrentSyncError)
	}
	err = updateChildStatusConditions[T](ctx, rclient, childObject, cond)
	return syncErr, err
}

// releaseDroppedChildren releases typeName's condition from every object of kind T that
// still carries it but isn't in current. It queries via ChildConditionIndexField instead of
// listing every object of kind T, so cost scales with how many children previously carried
// this parent's condition, not with the total population of that kind.
func releaseDroppedChildren[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, typeName string, current map[types.NamespacedName]struct{}) error {
	carriers, err := listChildrenWithCondition[T, PT](ctx, rclient, typeName)
	if err != nil {
		return fmt.Errorf("cannot list %T carrying condition=%q: %w", *new(T), typeName, err)
	}

	var errs []error
	for _, obj := range carriers {
		nsn := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		if _, stillSelected := current[nsn]; stillSelected {
			continue
		}
		if err := releaseChildStatusCondition[T, PT](ctx, rclient, nsn, typeName); err != nil {
			errs = append(errs, fmt.Errorf("cannot release stale status condition for child=%s: %w", nsn, err))
		}
	}
	return utilerrors.NewAggregate(errs)
}

// listChildrenWithCondition lists objects of kind T carrying a condition of type typeName,
// via ChildConditionIndexField; on any error (e.g. an uncached client without the index) it
// falls back to listing every object of kind T and filtering in-process.
func listChildrenWithCondition[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, typeName string) ([]PT, error) {
	items, err := listOfKind[T, PT](ctx, rclient, client.MatchingFields{k8stools.ChildConditionIndexField: typeName})
	if err == nil {
		return items, nil
	}

	all, err := listOfKind[T, PT](ctx, rclient)
	if err != nil {
		return nil, err
	}
	result := all[:0]
	for _, obj := range all {
		if hasCondition(obj.GetStatusMetadata().Conditions, typeName) {
			result = append(result, obj)
		}
	}
	return result, nil
}

// listOfKind lists every object of kind T visible to rclient matching opts, without callers
// needing to name T's corresponding List type explicitly.
func listOfKind[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, opts ...client.ListOption) ([]PT, error) {
	gvk, err := apiutil.GVKForObject(PT(new(T)), rclient.Scheme())
	if err != nil {
		return nil, fmt.Errorf("cannot resolve GVK: %w", err)
	}
	gvk.Kind += "List"
	listObj, err := rclient.Scheme().New(gvk)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve list type for %s: %w", gvk, err)
	}
	list, ok := listObj.(client.ObjectList)
	if !ok {
		return nil, fmt.Errorf("resolved list type %T for %s does not implement client.ObjectList", listObj, gvk)
	}
	if err := rclient.List(ctx, list, opts...); err != nil {
		return nil, fmt.Errorf("cannot list %s: %w", gvk, err)
	}
	items, err := apimeta.ExtractList(list)
	if err != nil {
		return nil, fmt.Errorf("cannot extract items from %s: %w", gvk, err)
	}
	result := make([]PT, 0, len(items))
	for _, it := range items {
		pt, ok := it.(PT)
		if !ok {
			return nil, fmt.Errorf("BUG: list item %T does not assert to %T", it, *new(PT))
		}
		result = append(result, pt)
	}
	return result, nil
}

func hasCondition(conds []vmv1beta1.Condition, condType string) bool {
	for _, c := range conds {
		if c.Type == condType {
			return true
		}
	}
	return false
}

func releaseChildStatusCondition[T any, PT interface {
	*T
	objectWithStatus
}](ctx context.Context, rclient client.Client, nsn types.NamespacedName, typeName string) error {
	return retryOnConflict(func() error {
		dst := PT(new(T))
		if err := rclient.Get(ctx, nsn, dst); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		st := dst.GetStatusMetadata()
		prevSt := st.DeepCopy()

		st.Conditions = removeConditionByType(st.Conditions, typeName)
		st.ObservedGeneration = dst.GetGeneration()
		writeAggregatedStatus(st, vmv1beta1.ConditionDomainTypeAppliedSuffix)
		if !reflect.DeepEqual(prevSt, st) {
			if err := rclient.Status().Update(ctx, dst); err != nil {
				if k8serrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("failed to release status condition of %T=%q: %w", dst, nsn, err)
			}
		}
		return nil
	})
}

func removeConditionByType(conds []vmv1beta1.Condition, condType string) []vmv1beta1.Condition {
	out := conds[:0]
	for _, c := range conds {
		if c.Type != condType {
			out = append(out, c)
		}
	}
	return out
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
			if k8serrors.IsNotFound(err) {
				return nil
			}
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
				if k8serrors.IsNotFound(err) {
					return nil
				}
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

// writeAggregatedStatus derives status from per-parent conditions; a child selected by
// multiple parents is only Failed if it fails on all of them, not just one.
func writeAggregatedStatus(stm *vmv1beta1.StatusMetadata, domainTypeSuffix string) {
	var appliedCount, failedCount int
	var errorMessages []string
	for _, c := range stm.Conditions {
		if !strings.HasSuffix(c.Type, domainTypeSuffix) {
			continue
		}
		if c.Status == "False" {
			failedCount++
			errorMessages = append(errorMessages, c.Message)
		} else {
			appliedCount++
		}
	}
	sort.Strings(errorMessages)
	stm.Reason = ""

	switch {
	case appliedCount == 0 && failedCount == 0:
		stm.UpdateStatus = vmv1beta1.UpdateStatusIgnored
	case appliedCount == 0:
		stm.UpdateStatus = vmv1beta1.UpdateStatusFailed
		stm.Reason = errorMessages[0]
	default:
		stm.UpdateStatus = vmv1beta1.UpdateStatusOperational
		if failedCount > 0 {
			stm.Reason = fmt.Sprintf("applied on %d parent(s), failed on %d: %s", appliedCount, failedCount, errorMessages[0])
		}
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
	GetStatusMetadata() *vmv1beta1.StatusMetadata
	DefaultStatusFields(ST)
}

// StatusWithMetadata defines
type StatusWithMetadata[T any] interface {
	DeepCopy() T
}

// UpdateStatus reconcile provided object status with given actualStatus status
func UpdateObjectStatus[T client.Object, ST StatusWithMetadata[STC], STC any](ctx context.Context, rclient client.Client, object ObjectWithDeepCopyAndStatus[T, ST, STC], actualStatus vmv1beta1.UpdateStatus, maybeErr error) error {
	currentStatus := object.GetStatus()
	prevStatus := currentStatus.DeepCopy()
	currMeta := object.GetStatusMetadata()
	newUpdateStatus := actualStatus

	switch actualStatus {
	case vmv1beta1.UpdateStatusExpanding, vmv1beta1.UpdateStatusOperational, vmv1beta1.UpdateStatusIgnored:
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
	// which is not desired behaviour
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
		return nil, fmt.Errorf("possible bug, cannot serialize patch specification as json: %w", err)
	}

	return client.RawPatch(types.JSONPatchType, data), nil

}
