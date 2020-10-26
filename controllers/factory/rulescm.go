package factory

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/ghodss/yaml"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const labelVMAlertName = "vmalert-name"

// The maximum `Data` size of a ConfigMap seems to differ between
// environments. This is probably due to different meta data sizes which count
// into the overall maximum size of a ConfigMap. Thereby lets leave a
// large buffer.
var maxConfigMapDataSize = int(float64(v1.MaxSecretSize) * 0.5)

var (
	managedByOperatorLabel      = "managed-by"
	managedByOperatorLabelValue = "vm-operator"
	managedByOperatorLabels     = map[string]string{
		managedByOperatorLabel: managedByOperatorLabelValue,
	}
)

var (
	defAlert = `
groups:
- name: vmAlertGroup
  rules:
     - alert: error writing to remote
       for: 1m
       expr: rate(vmalert_remotewrite_errors_total[1m]) > 0
       labels:
         host: "{{ $labels.instance }}"
       annotations:
         summary: " error writing to remote writer from vmaler{{ $value|humanize }}"
         description: "error writing to remote writer from vmaler {{$labels}}"
         back: "error rate is ok at vmalert "
`
)

func CreateOrUpdateRuleConfigMaps(ctx context.Context, cr *victoriametricsv1beta1.VMAlert, rclient client.Client) ([]string, error) {
	l := log.WithValues("reconcile", "rulesCm", "vmalert", cr.Name)
	newRules, err := SelectRules(ctx, cr, rclient)
	if err != nil {
		return nil, err
	}

	currentConfigMapList := &v1.ConfigMapList{}
	err = rclient.List(ctx, currentConfigMapList, rulesConfigMapSelector(cr.Name, cr.Namespace))
	if err != nil {
		return nil, err
	}
	currentConfigMaps := currentConfigMapList.Items

	currentRules := map[string]string{}
	for _, cm := range currentConfigMaps {
		for ruleFileName, ruleFile := range cm.Data {
			currentRules[ruleFileName] = ruleFile
		}
	}

	equal := reflect.DeepEqual(newRules, currentRules)
	if equal && len(currentConfigMaps) != 0 {
		l.Info("no Rule changes",
			"namespace", cr.Namespace,
			"vmalert", cr.Name,
		)
		currentConfigMapNames := []string{}
		for _, cm := range currentConfigMaps {
			currentConfigMapNames = append(currentConfigMapNames, cm.Name)
		}
		return currentConfigMapNames, nil
	}

	newConfigMaps, err := makeRulesConfigMaps(cr, newRules)
	if err != nil {
		return nil, fmt.Errorf("failted to make rules ConfigMaps: %w", err)
	}

	newConfigMapNames := []string{}
	for _, cm := range newConfigMaps {
		newConfigMapNames = append(newConfigMapNames, cm.Name)
	}

	if len(currentConfigMaps) == 0 {
		l.Info("no Rule configmap found, creating new one", "namespace", cr.Namespace,
			"vmalert", cr.Name,
		)
		for _, cm := range newConfigMaps {
			err := rclient.Create(ctx, &cm, &client.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("failed to create Configmap: %s, err: %w", cm.Name, err)
			}
		}
		return newConfigMapNames, nil
	}

	// Simply deleting old ConfigMaps and creating new ones for now. Could be
	// replaced by logic that only deletes obsolete ConfigMaps in the future.
	for _, cm := range currentConfigMaps {
		err := rclient.Delete(ctx, &cm, &client.DeleteOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to deleted current Configmap: %s, err: %w", cm.Name, err)
		}
	}

	log.Info("updating VMRules",
		"namespace", cr.Namespace,
		"vmalert", cr.Name,
	)
	for _, cm := range newConfigMaps {
		err = rclient.Create(ctx, &cm)
		if err != nil {
			return nil, fmt.Errorf("failed to create new Configmap: %s, err: %w", cm.Name, err)
		}
	}

	// trigger sync for configmap
	err = updatePodAnnotations(ctx, rclient, cr.PodLabels(), cr.Namespace)
	if err != nil {
		l.Error(err, "failed to update pod cm-sync annotation", "ns", cr.Namespace)
	}
	return newConfigMapNames, nil
}

func rulesConfigMapSelector(vmAlertName string, namespace string) client.ListOption {
	return &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{labelVMAlertName: vmAlertName}),
		Namespace:     namespace,
	}
}

func selectNamespaces(ctx context.Context, rclient client.Client, selector labels.Selector) ([]string, error) {
	matchedNs := []string{}
	ns := &v1.NamespaceList{}

	if err := rclient.List(ctx, ns, &client.ListOptions{LabelSelector: selector}); err != nil {
		return nil, err
	}

	for _, n := range ns.Items {
		matchedNs = append(matchedNs, n.Name)
	}
	log.Info("namespaced matched by selector", "ns", strings.Join(matchedNs, ","))

	return matchedNs, nil
}

func SelectRules(ctx context.Context, cr *victoriametricsv1beta1.VMAlert, rclient client.Client) (map[string]string, error) {
	rules := map[string]string{}
	namespaces := []string{}

	//use only object's namespace
	if cr.Spec.RuleNamespaceSelector == nil {
		namespaces = append(namespaces, cr.Namespace)
	} else if cr.Spec.RuleNamespaceSelector.MatchExpressions == nil && cr.Spec.RuleNamespaceSelector.MatchLabels == nil {
		// all namespaces matched
		namespaces = nil
	} else {
		//filter for specific namespaces
		nsSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.RuleNamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot convert ruleNamespace selector: %w", err)
		}
		namespaces, err = selectNamespaces(ctx, rclient, nsSelector)
		if err != nil {
			return nil, fmt.Errorf("cannot select namespaces for rule match: %w", err)
		}
	}

	//if namespaces isn't nil,then ruleselector cannot be nil
	//and we filter everything at specified namespaces
	if namespaces != nil && cr.Spec.RuleSelector == nil {
		cr.Spec.RuleSelector = &metav1.LabelSelector{}
	}

	ruleSelector, err := metav1.LabelSelectorAsSelector(cr.Spec.RuleSelector)
	if err != nil {
		return rules, fmt.Errorf("cannot convert rule label selector to selector: %w", err)
	}
	promRules := []*victoriametricsv1beta1.VMRule{}

	//list all namespaces for rules with selector
	if namespaces == nil {
		log.Info("listing all namespaces for rules")
		ruleNs := &victoriametricsv1beta1.VMRuleList{}
		err = rclient.List(ctx, ruleNs, &client.ListOptions{LabelSelector: ruleSelector})
		if err != nil {
			return nil, fmt.Errorf("cannot list rules from all namespaces: %w", err)
		}
		promRules = append(promRules, ruleNs.Items...)

	} else {
		for _, ns := range namespaces {
			listOpts := &client.ListOptions{Namespace: ns, LabelSelector: ruleSelector}
			ruleNs := &victoriametricsv1beta1.VMRuleList{}
			err = rclient.List(ctx, ruleNs, listOpts)
			if err != nil {
				return nil, fmt.Errorf("cannot list rules at namespace: %s, err: %w", ns, err)
			}
			promRules = append(promRules, ruleNs.Items...)

		}
	}

	for _, pRule := range promRules {
		content, err := generateContent(pRule.Spec, cr.Spec.EnforcedNamespaceLabel, pRule.Namespace)
		if err != nil {
			return nil, fmt.Errorf("cannot generate content for rule: %s, err :%w", pRule.Name, err)
		}
		rules[fmt.Sprintf("%v-%v.yaml", pRule.Namespace, pRule.Name)] = content
	}

	ruleNames := []string{}
	for name := range rules {
		ruleNames = append(ruleNames, name)
	}

	if len(rules) == 0 {
		//inject default rule
		rules["default-vmalert.yaml"] = defAlert
	}

	log.Info("selected Rules",
		"rules", strings.Join(ruleNames, ","),
		"namespace", cr.Namespace,
		"vmalert", cr.Name,
	)

	return rules, nil
}

func generateContent(promRule victoriametricsv1beta1.VMRuleSpec, enforcedNsLabel, ns string) (string, error) {
	if enforcedNsLabel != "" {
		log.Info("enforce ns label is partly supported, create issue for it")
		for gi, group := range promRule.Groups {
			for ri := range group.Rules {
				if len(promRule.Groups[gi].Rules[ri].Labels) == 0 {
					promRule.Groups[gi].Rules[ri].Labels = map[string]string{}
				}
				promRule.Groups[gi].Rules[ri].Labels[enforcedNsLabel] = ns
				//TODO its required by openshift, add it later?
				//expr := r.Expr.String()
				//parsedExpr, err := promql.ParseExpr(expr)
				//if err != nil {
				//	return "", errors.Wrap(err, "failed to parse promql expression")
				//}
				//err = injectproxy.SetRecursive(parsedExpr, []*labels.Matcher{{
				//	Name:  enforcedNsLabel,
				//	Type:  labels.MatchEqual,
				//	Value: ns,
				//}})
				//if err != nil {
				//	return "", fmt.Errorf("failed to inject labels to expression: %w",err)
				//}

				//				promRule.Groups[gi].Rules[ri].Expr = intstr.FromString(parsedExpr.String())
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
// returns a list of Kubernetes ConfigMaps to be later on mounted into the
// Prometheus instance.
// If the total size of rule files exceeds the Kubernetes ConfigMap limit,
// they are split up via the simple first-fit [1] bin packing algorithm. In the
// future this can be replaced by a more sophisticated algorithm, but for now
// simplicity should be sufficient.
// [1] https://en.wikipedia.org/wiki/Bin_packing_problem#First-fit_algorithm
func makeRulesConfigMaps(cr *victoriametricsv1beta1.VMAlert, ruleFiles map[string]string) ([]v1.ConfigMap, error) {
	//check if none of the rule files is too large for a single ConfigMap
	for filename, file := range ruleFiles {
		if len(file) > maxConfigMapDataSize {
			return nil, fmt.Errorf(
				"rule file '%v' is too large for a single Kubernetes ConfigMap",
				filename,
			)
		}
	}

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
		if bucketSize(buckets[currBucketIndex])+len(ruleFiles[filename]) > maxConfigMapDataSize {
			buckets = append(buckets, map[string]string{})
			currBucketIndex++
		}
		buckets[currBucketIndex][filename] = ruleFiles[filename]
	}

	ruleFileConfigMaps := []v1.ConfigMap{}
	for i, bucket := range buckets {
		cm := makeRulesConfigMap(cr, bucket)
		cm.Name = cm.Name + "-" + strconv.Itoa(i)
		ruleFileConfigMaps = append(ruleFileConfigMaps, cm)
	}

	return ruleFileConfigMaps, nil
}

func bucketSize(bucket map[string]string) int {
	totalSize := 0
	for _, v := range bucket {
		totalSize += len(v)
	}

	return totalSize
}

func makeRulesConfigMap(cr *victoriametricsv1beta1.VMAlert, ruleFiles map[string]string) v1.ConfigMap {
	ruleLabels := map[string]string{labelVMAlertName: cr.Name}
	for k, v := range managedByOperatorLabels {
		ruleLabels[k] = v
	}

	return v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            ruleConfigMapName(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          ruleLabels,
			OwnerReferences: cr.AsOwner(),
		},
		Data: ruleFiles,
	}
}

func ruleConfigMapName(vmName string) string {
	return "vm-" + vmName + "-rulefiles"
}
