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
	// fast path
	if cr.IsUnmanaged() {
		return nil, nil
	}
	return reconcileVMAlertConfig(ctx, rclient, cr, childCR, hasNotifiers)
}

func reconcileConfigsData(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, groups []vmv1beta1.RuleGroup) ([]string, error) {
	newConfigMaps, err := makeRulesConfigMaps(cr, groups)
	if err != nil {
		return nil, fmt.Errorf("cannot build rule configmaps for vmalert: %w", err)
	}
	sort.Slice(newConfigMaps, func(i, j int) bool {
		return newConfigMaps[i].Name < newConfigMaps[j].Name
	})
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
	pos, data, err := selectRules(ctx, rclient, cr, hasNotifiers)
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

// selectRules selects rule groups for cr. When hasNotifiers is false, alerting rules are
// dropped from each group (vmalert has nowhere to send them); groups left with no rules are
// skipped entirely. Recording rules are always kept.
func selectRules(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlert, hasNotifiers bool) (*parsedObjects, []vmv1beta1.RuleGroup, error) {
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
	var groups []vmv1beta1.RuleGroup
	pos.rules.ForEachCollectSkipInvalid(func(rule *vmv1beta1.VMRule) error {
		if !build.MustSkipRuntimeValidation() {
			if err := rule.Validate(); err != nil {
				return err
			}
		}
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
		return nil
	})
	pos.rules.UpdateMetrics(ctx)
	return pos, groups, nil
}

// dropAlertingRules returns rules with alerting rules removed, keeping recording rules.
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

// rulesFilename is the single BinaryData key in each rule ConfigMap bucket.
const rulesFilename = "rules.yaml"

// packRuleGroups packs groups into buckets that each fit within limit bytes when gzip-compressed.
// Groups with the same name must not appear in the same bucket (vmalert requires unique group names
// within a single file). The algorithm first assigns groups to name-unique batches via greedy
// first-fit, then splits any oversized batch further with build.PackItems (subsets preserve
// name-uniqueness).
func packRuleGroups(groups []vmv1beta1.RuleGroup, limit int) ([][]vmv1beta1.RuleGroup, error) {
	// Phase 1: greedy first-fit into name-unique batches.
	var batches [][]vmv1beta1.RuleGroup
	batchNames := []sets.Set[string]{}
	for _, g := range groups {
		placed := false
		for i := range batches {
			if !batchNames[i].Has(g.Name) {
				batches[i] = append(batches[i], g)
				batchNames[i].Insert(g.Name)
				placed = true
				break
			}
		}
		if !placed {
			batches = append(batches, []vmv1beta1.RuleGroup{g})
			batchNames = append(batchNames, sets.New(g.Name))
		}
	}
	// Phase 2: split any oversized batch by size; subsets of a name-unique batch are also name-unique.
	var result [][]vmv1beta1.RuleGroup
	for _, batch := range batches {
		sub, err := build.PackItems(batch, limit, 150)
		if err != nil {
			return nil, err
		}
		result = append(result, sub...)
	}
	return result, nil
}

// makeRulesConfigMaps packs rule groups into gzip-compressed ConfigMap buckets using
// build.PackItems with 50% headroom. Each bucket is stored as one "rules.yaml" BinaryData entry.
// Always returns at least one ConfigMap so VMAlert has a valid mount target.
func makeRulesConfigMaps(cr *vmv1beta1.VMAlert, groups []vmv1beta1.RuleGroup) ([]corev1.ConfigMap, error) {
	buckets, err := packRuleGroups(groups, config.MustGetBaseConfig().ConfigDataBudgetBytes)
	if err != nil {
		return nil, fmt.Errorf("cannot pack rule groups into configmap buckets: %w", err)
	}
	cms := make([]corev1.ConfigMap, 0, len(buckets))
	for i, bucket := range buckets {
		data, err := yaml.Marshal(vmv1beta1.VMRuleSpec{Groups: bucket})
		if err != nil {
			return nil, fmt.Errorf("cannot marshal rule groups for configmap %d: %w", i, err)
		}
		compressed, err := build.GzipConfig(data)
		if err != nil {
			return nil, fmt.Errorf("cannot compress rule groups for configmap %d: %w", i, err)
		}
		cms = append(cms, corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            ruleConfigMapName(cr.Name) + "-" + strconv.Itoa(i),
				Namespace:       cr.Namespace,
				Labels:          cr.FinalLabels(),
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			},
			BinaryData: map[string][]byte{rulesFilename: compressed},
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
