package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

// ConfigMap reconciles configmap object
func ConfigMap(ctx context.Context, rclient client.Client, newObj *corev1.ConfigMap, prevMeta *metav1.ObjectMeta, owner *metav1.OwnerReference) (bool, error) {
	nsn := types.NamespacedName{Name: newObj.Name, Namespace: newObj.Namespace}
	updated := true
	removeFinalizer := true
	err := retryOnConflict(func() error {
		var existingObj corev1.ConfigMap
		if err := rclient.Get(ctx, nsn, &existingObj); err != nil {
			if k8serrors.IsNotFound(err) {
				logger.WithContext(ctx).Info(fmt.Sprintf("creating new ConfigMap=%s", nsn.String()))
				return rclient.Create(ctx, newObj)
			}
			return fmt.Errorf("cannot get existing ConfigMap=%s: %w", nsn.String(), err)
		}
		if err := collectGarbage(ctx, rclient, &existingObj, removeFinalizer); err != nil {
			return err
		}
		metaChanged, err := mergeMeta(&existingObj, newObj, prevMeta, owner, removeFinalizer)
		if err != nil {
			return err
		}

		logMessageMetadata := []string{fmt.Sprintf("name=%s", nsn.String())}
		dataDiff := changedKeysWithSizes(newObj.Data, existingObj.Data, "data")
		needsUpdate := metaChanged || len(dataDiff) > 0

		binDataDiff := changedKeysWithSizes(newObj.BinaryData, existingObj.BinaryData, "binaryData")
		needsUpdate = needsUpdate || len(binDataDiff) > 0

		if !needsUpdate {
			updated = false
			return nil
		}
		existingObj.Data = newObj.Data
		existingObj.BinaryData = newObj.BinaryData
		logger.WithContext(ctx).Info(fmt.Sprintf("updating ConfigMap %s", strings.Join(logMessageMetadata, ", ")), "data_diff", dataDiff, "bin_data_diff", binDataDiff)
		return rclient.Update(ctx, &existingObj)
	})
	return updated, err
}

// changedKeysWithSizes returns the sorted keys whose values differ between
// desired and current, annotated with the value byte size on each side.
// Values themselves are never included: a single ConfigMap value can reach
// multiple megabytes, and logging it verbatim produces log lines that
// downstream log pipelines with per-line size limits cannot ingest.
func changedKeysWithSizes[V string | []byte](desired, current map[string]V, root string) []string {
	changed := make([]string, 0, len(desired))
	for key, desiredValue := range desired {
		currentValue, ok := current[key]
		switch {
		case !ok:
			changed = append(changed, fmt.Sprintf("%s['%s'](current=absent,desired=%dB)", root, key, len(desiredValue)))
		case string(currentValue) != string(desiredValue):
			changed = append(changed, fmt.Sprintf("%s['%s'](current=%dB,desired=%dB)", root, key, len(currentValue), len(desiredValue)))
		}
	}
	for key, currentValue := range current {
		if _, ok := desired[key]; !ok {
			changed = append(changed, fmt.Sprintf("%s['%s'](current=%dB,desired=absent)", root, key, len(currentValue)))
		}
	}
	sort.Strings(changed)
	return changed
}
