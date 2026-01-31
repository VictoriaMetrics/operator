package vmalert

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

var (
	managedByOperatorLabel      = "managed-by"
	managedByOperatorLabelValue = "vm-operator"
	managedByOperatorLabels     = map[string]string{
		managedByOperatorLabel: managedByOperatorLabelValue,
	}
)

// CreateOrUpdateRuleConfigMaps conditionally selects vmrules and stores content at configmaps
func CreateOrUpdateRuleConfigMaps(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, childCR *vmv1beta1.VMRule) ([]string, error) {
	// fast path
	if cr.IsUnmanaged() {
		return nil, nil
	}
	newRules, err := reconcileVMAlertConfig(ctx, rclient, cr, childCR)
	if err != nil {
		return nil, err
	}

	return newRules, nil
}

func reconcileConfigsData(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, newRules map[string]string) ([]string, error) {
	newConfigMaps := makeRulesConfigMaps(cr, newRules)
	sort.Slice(newConfigMaps, func(i, j int) bool {
		return newConfigMaps[i].Name < newConfigMaps[j].Name
	})
	var needReload bool
	newConfigMapNames := make([]string, 0, len(newConfigMaps))
	for i := range newConfigMaps {
		cm := &newConfigMaps[i]
		if updated, err := reconcile.ConfigMap(ctx, rclient, cm, nil); err != nil {
			return nil, err
		} else if updated {
			needReload = true
		}
		newConfigMapNames = append(newConfigMapNames, cm.Name)
	}
	if needReload {
		// trigger sync for configmap
		logger.WithContext(ctx).Info("triggering pod config reload by changing annotation")
		if err := k8stools.UpdatePodAnnotations(ctx, rclient, cr.PodLabels(), cr.Namespace); err != nil {
			logger.WithContext(ctx).Error(err, "failed to update vmalert pod cm-sync annotation")
		}
	}
	return newConfigMapNames, nil
}

func reconcileVMAlertConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, childCR *vmv1beta1.VMRule) ([]string, error) {
	pos, data, err := selectRules(ctx, rclient, cr)
	if err != nil {
		return nil, err
	}
	// perform config maps content update
	cmNames, err := reconcileConfigsData(ctx, rclient, cr, data)
	if err != nil {
		return nil, err
	}
	parentObject := fmt.Sprintf("%s.%s.vmalert", cr.Name, cr.Namespace)
	if childCR != nil {
		if o := pos.rules.Get(childCR); o != nil {
			// fast path update a single object that triggered event
			// it should be fast path for the most cases
			if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMRule{o}); err != nil {
				return nil, err
			}
			return cmNames, nil
		}
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, pos.rules.All()); err != nil {
		return nil, err
	}
	return cmNames, nil
}

type parsedObjects struct {
	rules *build.ChildObjects[*vmv1beta1.VMRule]
}

func selectRules(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert) (*parsedObjects, map[string]string, error) {
	var rules []*vmv1beta1.VMRule
	var nsn []string
	if !build.IsControllerDisabled("VMRule") {
		opts := &k8stools.SelectorOpts{
			SelectAll:         cr.Spec.SelectAllByDefault,
			ObjectSelector:    cr.Spec.RuleSelector,
			NamespaceSelector: cr.Spec.RuleNamespaceSelector,
			DefaultNamespace:  cr.Namespace,
		}
		if err := k8stools.VisitSelected(ctx, rclient, opts, func(list *vmv1beta1.VMRuleList) {
			for _, item := range list.Items {
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				rules = append(rules, item.DeepCopy())
				nsn = append(nsn, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			}
		}); err != nil {
			return nil, nil, err
		}
		if cr.NeedDedupRules() {
			logger.WithContext(ctx).Info("deduplicating vmalert rules")
			rules = deduplicateRules(ctx, rules)
		}
	}
	pos := &parsedObjects{rules: build.NewChildObjects("vmrule", rules, nsn)}
	data := make(map[string]string)
	pos.rules.ForEachCollectSkipInvalid(func(rule *vmv1beta1.VMRule) error {
		if !build.MustSkipRuntimeValidation() {
			if err := rule.Validate(); err != nil {
				return err
			}
		}
		content, err := generateContent(rule.Spec, cr.Spec.EnforcedNamespaceLabel, rule.Namespace)
		if err != nil {
			return err
		}
		data[rule.AsKey(false)] = content
		return nil
	})
	pos.rules.UpdateMetrics(ctx)
	return pos, data, nil
}

func generateContent(promRule vmv1beta1.VMRuleSpec, enforcedNsLabel, ns string) (string, error) {
	if enforcedNsLabel != "" {
		for gi, group := range promRule.Groups {
			for ri := range group.Rules {
				if len(promRule.Groups[gi].Rules[ri].Labels) == 0 {
					promRule.Groups[gi].Rules[ri].Labels = map[string]string{}
				}
				promRule.Groups[gi].Rules[ri].Labels[enforcedNsLabel] = ns
			}
		}
	}
	content, err := yaml.Marshal(promRule)
	if err != nil {
		return "", fmt.Errorf("cannot unmarshal context for cm rule generation: %w", err)
	}
	return string(content), nil
}

// makeRulesConfigMaps takes a VMAlert configuration and rule files and
// returns a list of Kubernetes ConfigMaps to be later on mounted
// If the total size of rule files exceeds the Kubernetes ConfigMap limit,
// they are split up via the simple first-fit [1] bin packing algorithm. In the
// future this can be replaced by a more sophisticated algorithm, but for now
// simplicity should be sufficient.
// [1] https://en.wikipedia.org/wiki/Bin_packing_problem#First-fit_algorithm
func makeRulesConfigMaps(cr *vmv1beta1.VMAlert, ruleFiles map[string]string) []corev1.ConfigMap {
	buckets := []map[string]string{
		{},
	}
	currBucketIndex := 0

	// To make bin packing algorithm deterministic, sort ruleFiles filenames and
	// iterate over filenames instead of ruleFiles map (not deterministic).
	fileNames := []string{}
	for n := range ruleFiles {
		fileNames = append(fileNames, n)
	}
	sort.Strings(fileNames)

	for _, filename := range fileNames {
		// If rule file doesn't fit into current bucket, create new bucket.
		if bucketSize(buckets[currBucketIndex])+len(ruleFiles[filename]) > vmv1beta1.MaxConfigMapDataSize {
			buckets = append(buckets, map[string]string{})
			currBucketIndex++
		}
		buckets[currBucketIndex][filename] = ruleFiles[filename]
	}

	ruleFileConfigMaps := make([]corev1.ConfigMap, 0, len(buckets))
	for i, bucket := range buckets {
		cm := makeRulesConfigMap(cr, bucket)
		cm.Name = cm.Name + "-" + strconv.Itoa(i)
		ruleFileConfigMaps = append(ruleFileConfigMaps, cm)
	}

	return ruleFileConfigMaps
}

func bucketSize(bucket map[string]string) int {
	totalSize := 0
	for _, v := range bucket {
		totalSize += len(v)
	}

	return totalSize
}

func makeRulesConfigMap(cr *vmv1beta1.VMAlert, ruleFiles map[string]string) corev1.ConfigMap {
	ruleLabels := map[string]string{"vmalert-name": cr.Name}
	for k, v := range managedByOperatorLabels {
		ruleLabels[k] = v
	}

	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            ruleConfigMapName(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          ruleLabels,
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Data: ruleFiles,
	}
}

func ruleConfigMapName(vmName string) string {
	return "vm-" + vmName + "-rulefiles"
}

// deduplicateRules - takes list of vmRules and modifies it
// by removing duplicates.
// possible duplicates:
// group name across single vmRule. group might include non-duplicate rules.
// rules in group, must include uniq combination of values.
func deduplicateRules(ctx context.Context, origin []*vmv1beta1.VMRule) []*vmv1beta1.VMRule {
	// deduplicate rules across groups.
	for _, vmRule := range origin {
		for i, grp := range vmRule.Spec.Groups {
			uniqRules := make(map[uint64]struct{})
			rules := make([]vmv1beta1.Rule, 0, len(grp.Rules))
			for _, rule := range grp.Rules {
				ruleID := calculateRuleID(rule)
				if _, ok := uniqRules[ruleID]; ok {
					logger.WithContext(ctx).Info(fmt.Sprintf("duplicate rule=%q found at group=%q for vmrule=%q", rule.Expr, grp.Name, vmRule.Name))
				} else {
					uniqRules[ruleID] = struct{}{}
					rules = append(rules, rule)
				}
			}
			grp.Rules = rules
			vmRule.Spec.Groups[i] = grp
		}
	}
	return origin
}

func calculateRuleID(r vmv1beta1.Rule) uint64 {
	h := fnv.New64a()
	h.Write([]byte(r.Expr)) //nolint:errcheck
	if r.Record != "" {
		h.Write([]byte("recording")) //nolint:errcheck
		h.Write([]byte(r.Record))    //nolint:errcheck
	} else {
		h.Write([]byte("alerting")) //nolint:errcheck
		h.Write([]byte(r.Alert))    //nolint:errcheck
	}
	kv := sortMap(r.Labels)
	for _, i := range kv {
		h.Write([]byte(i.key))   //nolint:errcheck
		h.Write([]byte(i.value)) //nolint:errcheck
		h.Write([]byte("\xff"))  //nolint:errcheck
	}
	return h.Sum64()
}

type item struct {
	key, value string
}

func sortMap(m map[string]string) []item {
	var kv []item
	for k, v := range m {
		kv = append(kv, item{key: k, value: v})
	}
	sort.Slice(kv, func(i, j int) bool {
		return kv[i].key < kv[j].key
	})
	return kv
}
