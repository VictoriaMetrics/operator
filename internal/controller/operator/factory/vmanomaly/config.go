package vmanomaly

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

type config struct {
	schedulers []any
	models     []any
	reader     any
	writer     any
}

const defaultAnomalyConfig = `
writer:
  class: noop
reader:
  class: noop
`

func selectModels(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) ([]any, error) {
	var keys []string
	var models []any
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		ObjectSelector:    cr.Spec.ModelSelectors.Object,
		NamespaceSelector: cr.Spec.ModelSelectors.Namespace,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyAutoTunedModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyAutoTunedModel failed: %w", err)
	}
	keysLen := len(keys)
	logger.SelectedObjects(ctx, "VMAnomalyAutoTunedModel", keysLen, 0, keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyHoltWintersModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyHoltWintersModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyHoltWintersModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyIsolationForestModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyIsolationForestModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyIsolationForestMultivariateModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyIsolationForestMultivariateModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyIsolationForestMultivariateModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyIsolationForestMultivariateModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyMadModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyMadModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyMadModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyOnlineMadModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyOnlineMadModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyOnlineMadModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyOnlineQuantileModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyOnlineQuantileModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyOnlineQuantileModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyOnlineZScoreModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyOnlineZScoreModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyOnlineZScoreModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyProphetModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyProphetModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyProphetModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyRollingQuantileModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyRollingQuantileModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyRollingQuantileModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyStdModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyStdModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyStdModel", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyZScoreModelList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			models = append(models, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyZScoreModel failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyZScoreModel", len(keys)-keysLen, 0, keys[keysLen:])
	build.OrderByKeys(models, keys)
	return models, nil
}

func selectSchedulers(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) ([]any, error) {
	var keys []string
	var schedulers []any
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		ObjectSelector:    cr.Spec.SchedulerSelectors.Object,
		NamespaceSelector: cr.Spec.SchedulerSelectors.Namespace,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyOneoffSchedulerList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			schedulers = append(schedulers, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyOneoffScheduler failed: %w", err)
	}
	keysLen := len(keys)
	logger.SelectedObjects(ctx, "VMAnomalyOneoffScheduler", keysLen, 0, keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyPeriodicSchedulerList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			schedulers = append(schedulers, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyPeriodicScheduler failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyPeriodicScheduler", len(keys)-keysLen, 0, keys[keysLen:])
	keysLen = len(keys)

	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyBacktestingSchedulerList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			schedulers = append(schedulers, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyBacktestingScheduler failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyBacktestingScheduler", len(keys)-keysLen, 0, keys[keysLen:])
	build.OrderByKeys(schedulers, keys)
	return schedulers, nil
}

func selectReaders(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) ([]any, error) {
	var keys []string
	var readers []any
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		ObjectSelector:    cr.Spec.ReaderSelectors.Object,
		NamespaceSelector: cr.Spec.ReaderSelectors.Namespace,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyVMReaderList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			readers = append(readers, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyVMReader failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyVMReader", len(keys), 0, keys)
	build.OrderByKeys(readers, keys)
	return readers, nil
}

func selectWriters(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) ([]any, error) {
	var keys []string
	var writers []any
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		ObjectSelector:    cr.Spec.WriterSelectors.Object,
		NamespaceSelector: cr.Spec.WriterSelectors.Namespace,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(l *vmv1.VMAnomalyVMWriterList) {
		for i := range l.Items {
			item := &l.Items[i]
			if !item.GetDeletionTimestamp().IsZero() {
				continue
			}
			writers = append(writers, item)
			keys = append(keys, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
		}
	}); err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyVMWriter failed: %w", err)
	}
	logger.SelectedObjects(ctx, "VMAnomalyVMWriter", len(keys), 0, keys)
	build.OrderByKeys(writers, keys)
	return writers, nil
}

// CreateOrUpdateConfig - check if secret with config exist,
// if not create with predefined or user value.
func CreateOrUpdateConfig(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly, childObject client.Object) error {
	var prevCR *vmv1.VMAnomaly
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}

	if err := createOrUpdateConfig(ctx, rclient, cr, prevCR, childObject); err != nil {
		return err
	}
	return nil
}

func createOrUpdateConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly, childObject client.Object) error {
	c := &config{}
	schedulers, err := selectSchedulers(ctx, rclient, cr)
	if err != nil {
		return err
	}
	c.schedulers = schedulers

	models, err := selectModels(ctx, rclient, cr)
	if err != nil {
		return err
	}
	c.models = models

	readers, err := selectReaders(ctx, rclient, cr)
	if err != nil {
		return err
	}
	if len(readers) != 1 {
		return fmt.Errorf("failed to get exactly one reader, got %d", len(readers))
	}
	c.reader = readers[0]

	writers, err := selectWriters(ctx, rclient, cr)
	if err != nil {
		return err
	}
	c.writer = writers[0]

	assetsCache := k8stools.NewAssetsCache(ctx, rclient, tlsAssetsDir)
	var config []byte
	switch {
	case cr.Spec.ConfigSecret != nil:
		secret, err := assetsCache.LoadKeyFromSecret(cr.Namespace, cr.Spec.ConfigSecret)
		if err != nil {
			return fmt.Errorf("cannot fetch secret content for anomaly config secret, err: %w", err)
		}
		config = []byte(secret)
	case cr.Spec.ConfigRawYaml != "":
		config = []byte(cr.Spec.ConfigRawYaml)
	}

	newSecretConfig := &corev1.Secret{
		ObjectMeta: *buildConfgSecretMeta(cr),
		Data: map[string][]byte{
			secretConfigKey: config,
		}}

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = buildConfgSecretMeta(prevCR)
	}

	if err := reconcile.Secret(ctx, rclient, newSecretConfig, prevSecretMeta); err != nil {
		return err
	}

	return nil
}
