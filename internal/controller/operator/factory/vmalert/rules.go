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
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// CreateOrUpdateRuleConfigMaps conditionally selects vmrules and stores content at configmaps.
// Alerting rules are dropped when hasNotifiers is false, since vmalert would have nowhere to
// send them; recording rules are unaffected.
func CreateOrUpdateRuleConfigMaps(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, childCR *vmv1beta1.VMRule, hasNotifiers bool) ([]string, error) {
	if cr.IsUnmanaged() {
		return nil, nil
	}
	return reconcileVMAlertConfig(ctx, rclient, cr, childCR, hasNotifiers)
}

func reconcileConfigsData(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, files []ruleFile) ([]string, error) {
	newConfigMaps, err := makeRulesConfigMaps(cr, files)
	if err != nil {
		return nil, fmt.Errorf("cannot build rule configmaps for vmalert: %w", err)
	}
	var needReload bool
	var newConfigMapNames []string
	owner := cr.AsOwner()
	for i := range newConfigMaps {
		cm := &newConfigMaps[i]
		if updated, err := reconcile.ConfigMap(ctx, rclient, cm, nil, &owner); err != nil {
			return nil, err
		} else if updated {
			needReload = true
		}
		newConfigMapNames = append(newConfigMapNames, cm.Name)
	}
	if needReload {
		logger.WithContext(ctx).Info("triggering pod config reload by changing annotation")
		if err := k8stools.UpdatePodAnnotations(ctx, rclient, cr.PodLabels(), cr.Namespace); err != nil {
			logger.WithContext(ctx).Error(err, "failed to update vmalert pod cm-sync annotation")
		}
	}
	return newConfigMapNames, nil
}

func reconcileVMAlertConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, childCR *vmv1beta1.VMRule, hasNotifiers bool) ([]string, error) {
	pos, files, err := selectRules(ctx, rclient, cr, hasNotifiers)
	if err != nil {
		return nil, err
	}
	cmNames, err := reconcileConfigsData(ctx, rclient, cr, files)
	if err != nil {
		return nil, err
	}
	parentObject := fmt.Sprintf("%s.%s.vmalert", cr.Name, cr.Namespace)
	if childCR != nil {
		if o := pos.rules.Get(childCR); o != nil {
			if err := reconcile.StatusForChildObject(ctx, rclient, parentObject, o); err != nil {
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

type ruleFile struct {
	key    string
	groups []vmv1beta1.RuleGroup
}

// selectRules selects rule files for cr; a VMRule left with no groups contributes no file.
func selectRules(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, hasNotifiers bool) (*parsedObjects, []ruleFile, error) {
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
	var files []ruleFile
	pos.rules.ForEachCollectSkipInvalid(func(rule *vmv1beta1.VMRule) error {
		if !build.MustSkipRuntimeValidation() {
			if err := rule.Validate(); err != nil {
				return err
			}
		}
		var groups []vmv1beta1.RuleGroup
		for _, group := range rule.Spec.Groups {
			if cr.Spec.EnforcedNamespaceLabel != "" {
				for j := range group.Rules {
					if group.Rules[j].Labels == nil {
						group.Rules[j].Labels = map[string]string{}
					}
					group.Rules[j].Labels[cr.Spec.EnforcedNamespaceLabel] = rule.Namespace
				}
			}
			if !hasNotifiers {
				before := len(group.Rules)
				group.Rules = dropAlertingRules(group.Rules)
				if len(group.Rules) != before {
					logger.WithContext(ctx).Info("ignoring alerting rules: vmalert has no notifiers configured",
						"vmrule", rule.Name, "group", group.Name)
				}
				if len(group.Rules) == 0 {
					continue
				}
			}
			groups = append(groups, group)
		}
		if len(groups) == 0 {
			return nil
		}
		files = append(files, ruleFile{key: ruleFileKey(rule.Namespace, rule.Name), groups: groups})
		return nil
	})
	pos.rules.UpdateMetrics(ctx)
	return pos, files, nil
}

// ruleFileKey is safe as both a ConfigMap key and filename; "." can't collide since namespace
// names never contain one.
func ruleFileKey(namespace, name string) string {
	return namespace + "." + name + ".yaml"
}

func dropAlertingRules(rules []vmv1beta1.Rule) []vmv1beta1.Rule {
	filtered := make([]vmv1beta1.Rule, 0, len(rules))
	for _, r := range rules {
		if r.Alert != "" {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

type compressedRuleFile struct {
	key        string
	compressed []byte
}

func compressRuleFiles(files []ruleFile) ([]compressedRuleFile, error) {
	result := make([]compressedRuleFile, 0, len(files))
	for _, f := range files {
		data, err := yaml.Marshal(vmv1beta1.VMRuleSpec{Groups: f.groups})
		if err != nil {
			return nil, fmt.Errorf("cannot marshal rule groups for %s: %w", f.key, err)
		}
		compressed, err := build.GzipConfig(data)
		if err != nil {
			return nil, fmt.Errorf("cannot compress rule groups for %s: %w", f.key, err)
		}
		result = append(result, compressedRuleFile{key: f.key, compressed: compressed})
	}
	return result, nil
}

// packCompressedFiles packs files in order into buckets whose summed size stays within limit.
func packCompressedFiles(files []compressedRuleFile, limit int) ([][]compressedRuleFile, error) {
	var result [][]compressedRuleFile
	var current []compressedRuleFile
	var currentSize int
	for _, f := range files {
		if len(f.compressed) > limit {
			return nil, fmt.Errorf("single item compressed size %d exceeds limit %d", len(f.compressed), limit)
		}
		if len(current) > 0 && currentSize+len(f.compressed) > limit {
			result = append(result, current)
			current = nil
			currentSize = 0
		}
		current = append(current, f)
		currentSize += len(f.compressed)
	}
	if len(current) > 0 {
		result = append(result, current)
	}
	return result, nil
}

func makeRulesConfigMaps(cr *vmv1beta1.VMAlert, files []ruleFile) ([]corev1.ConfigMap, error) {
	compressed, err := compressRuleFiles(files)
	if err != nil {
		return nil, err
	}
	buckets, err := packCompressedFiles(compressed, config.MustGetBaseConfig().ConfigDataBudgetBytes)
	if err != nil {
		return nil, fmt.Errorf("cannot pack rule files into configmap buckets: %w", err)
	}
	if len(buckets) == 0 {
		buckets = [][]compressedRuleFile{{}}
	}
	cms := make([]corev1.ConfigMap, 0, len(buckets))
	for i, bucket := range buckets {
		binData := make(map[string][]byte, len(bucket))
		for _, f := range bucket {
			binData[f.key] = f.compressed
		}
		cms = append(cms, corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            ruleConfigMapName(cr.Name) + "-" + strconv.Itoa(i),
				Namespace:       cr.Namespace,
				Labels:          cr.FinalLabels(),
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			},
			BinaryData: binData,
		})
	}
	return cms, nil
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
			uniqRules := sets.New[uint64]()
			rules := make([]vmv1beta1.Rule, 0, len(grp.Rules))
			for _, rule := range grp.Rules {
				ruleID := calculateRuleID(rule)
				if uniqRules.Has(ruleID) {
					logger.WithContext(ctx).Info(fmt.Sprintf("duplicate rule=%q found at group=%q for vmrule=%q", rule.Expr, grp.Name, vmRule.Name))
				} else {
					uniqRules.Insert(ruleID)
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
