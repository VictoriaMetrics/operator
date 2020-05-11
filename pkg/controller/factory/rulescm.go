package factory

import (
	"context"
	"fmt"
	monitoringv1 "github.com/VictoriaMetrics/operator/pkg/apis/monitoring/v1"
	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/pkg/apis/victoriametrics/v1beta1"
	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	//"k8s.io/apimachinery/pkg/util/intstr"
	//"k8s.io/client-go/tools/cache"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const labelPrometheusName = "prometheus-name"

// The maximum `Data` size of a ConfigMap seems to differ between
// environments. This is probably due to different meta data sizes which count
// into the overall maximum size of a ConfigMap. Thereby lets leave a
// large buffer.
var maxConfigMapDataSize = int(float64(v1.MaxSecretSize) * 0.5)

var (
	managedByOperatorLabel      = "managed-by"
	managedByOperatorLabelValue = "prometheus-operator"
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

func CreateOrUpdateRuleConfigMaps(p *victoriametricsv1beta1.VmAlert, kclient kubernetes.Interface, rclient client.Client, l logr.Logger) ([]string, error) {
	cClient := kclient.CoreV1().ConfigMaps(p.Namespace)

	newRules, err := SelectRules(p, rclient, l)
	if err != nil {
		return nil, err
	}

	currentConfigMapList, err := cClient.List(prometheusRulesConfigMapSelector(p.Name))
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
		l.Info("no PrometheusRule changes",
			"namespace", p.Namespace,
			"prometheus", p.Name,
		)
		currentConfigMapNames := []string{}
		for _, cm := range currentConfigMaps {
			currentConfigMapNames = append(currentConfigMapNames, cm.Name)
		}
		return currentConfigMapNames, nil
	}

	newConfigMaps, err := makeRulesConfigMaps(p, newRules)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make rules ConfigMaps")
	}

	newConfigMapNames := []string{}
	for _, cm := range newConfigMaps {
		newConfigMapNames = append(newConfigMapNames, cm.Name)
	}

	if len(currentConfigMaps) == 0 {
		l.Info("no PrometheusRule configmap found, creating new one",
			"namespace", p.Namespace,
			"prometheus", p.Name,
		)
		for _, cm := range newConfigMaps {
			_, err = cClient.Create(&cm)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to create ConfigMap '%v'", cm.Name)
			}
		}
		return newConfigMapNames, nil
	}

	// Simply deleting old ConfigMaps and creating new ones for now. Could be
	// replaced by logic that only deletes obsolete ConfigMaps in the future.
	for _, cm := range currentConfigMaps {
		err := cClient.Delete(cm.Name, &metav1.DeleteOptions{})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to delete current ConfigMap '%v'", cm.Name)
		}
	}

	l.Info("updating PrometheusRule",
		"namespace", p.Namespace,
		"prometheus", p.Name,
	)
	for _, cm := range newConfigMaps {
		_, err = cClient.Create(&cm)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create new ConfigMap '%v'", cm.Name)
		}
	}

	return newConfigMapNames, nil
}

func prometheusRulesConfigMapSelector(prometheusName string) metav1.ListOptions {
	return metav1.ListOptions{LabelSelector: fmt.Sprintf("%v=%v", labelPrometheusName, prometheusName)}
}

func selectNamespaces(rclient client.Client, selector labels.Selector) ([]string, error) {
	matchedNs := []string{}
	ns := &v1.NamespaceList{}

	if err := rclient.List(context.TODO(), ns, &client.ListOptions{LabelSelector: selector}); err != nil {
		return nil, err
	}

	for _, n := range ns.Items {
		matchedNs = append(matchedNs, n.Name)
	}

	return matchedNs, nil
}

func SelectRules(p *victoriametricsv1beta1.VmAlert, rclient client.Client, l logr.Logger) (map[string]string, error) {
	rules := map[string]string{}
	namespaces := []string{}

	//what can we do?
	//list namespaces matched by rule nameselector
	//for each namespace apply list with rule selector...
	//combine result

	if p.Spec.RuleNamespaceSelector == nil {
		namespaces = append(namespaces, p.Namespace)
	} else if p.Spec.RuleNamespaceSelector.MatchExpressions == nil && p.Spec.RuleNamespaceSelector.MatchLabels == nil {
		namespaces = nil
	} else {
		nsSelector, err := metav1.LabelSelectorAsSelector(p.Spec.RuleNamespaceSelector)
		if err != nil {
			return nil, errors.Wrap(err, "cannot convert rulenamspace selector")
		}
		namespaces, err = selectNamespaces(rclient, nsSelector)
		if err != nil {
			return nil, errors.Wrap(err, "cannot select namespaces for rule match")
		}
	}
	//here we use trick
	//if namespaces isnt nil, then namespaceselector is defined
	//but general selector maybe be nil
	if namespaces != nil && p.Spec.RuleSelector == nil {
		p.Spec.RuleSelector = &metav1.LabelSelector{}
	}

	ruleSelector, err := metav1.LabelSelectorAsSelector(p.Spec.RuleSelector)
	if err != nil {
		return rules, errors.Wrap(err, "convert rule label selector to selector")
	}
	promRules := []*monitoringv1.PrometheusRule{}

	//list all namespaces for rules with selector
	if namespaces == nil {
		l.Info("listing all namespaces for rules")
		ruleNs := &monitoringv1.PrometheusRuleList{}
		err = rclient.List(context.TODO(), ruleNs, &client.ListOptions{LabelSelector: ruleSelector})
		if err != nil {
			l.Error(err, "cannot list rules")
			return nil, err
		}
		promRules = append(promRules, ruleNs.Items...)

	} else {
		for _, ns := range namespaces {
			listOpts := &client.ListOptions{Namespace: ns, LabelSelector: ruleSelector}
			ruleNs := &monitoringv1.PrometheusRuleList{}
			err = rclient.List(context.TODO(), ruleNs, listOpts)
			if err != nil {
				l.Error(err, "cannot list rules")
				return nil, err
			}
			promRules = append(promRules, ruleNs.Items...)

		}
	}

	for _, pRule := range promRules {
		content, err := generateContent(pRule.Spec, p.Spec.EnforcedNamespaceLabel, pRule.Namespace)
		if err != nil {
			l.WithValues("rule", pRule.Name).Error(err, "cannot generate content for rule")
			return nil, err
		}
		rules[fmt.Sprintf("%v-%v.yaml", pRule.Namespace, pRule.Name)] = content
	}

	ruleNames := []string{}
	for name := range rules {
		ruleNames = append(ruleNames, name)
	}
	//TODO replace it with crd?
	rules["default-vmalert.yaml"] = defAlert

	l.Info("selected Rules",
		"rules", strings.Join(ruleNames, ","),
		"namespace", p.Namespace,
		"prometheus", p.Name,
	)

	return rules, nil
}

func generateContent(promRule monitoringv1.PrometheusRuleSpec, enforcedNsLabel, ns string) (string, error) {
	if enforcedNsLabel != "" {
		fmt.Printf("it`s bad sign \n \n")
		for gi, group := range promRule.Groups {
			group.PartialResponseStrategy = ""
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
				//	return "", errors.Wrap(err, "failed to inject labels to expression")
				//}

				//				promRule.Groups[gi].Rules[ri].Expr = intstr.FromString(parsedExpr.String())
			}
		}
	}
	content, err := yaml.Marshal(promRule)
	if err != nil {
		return "", errors.Wrap(err, "failed to unmarshal content")
	}
	return string(content), nil
}

// makeRulesConfigMaps takes a Prometheus configuration and rule files and
// returns a list of Kubernetes ConfigMaps to be later on mounted into the
// Prometheus instance.
// If the total size of rule files exceeds the Kubernetes ConfigMap limit,
// they are split up via the simple first-fit [1] bin packing algorithm. In the
// future this can be replaced by a more sophisticated algorithm, but for now
// simplicity should be sufficient.
// [1] https://en.wikipedia.org/wiki/Bin_packing_problem#First-fit_algorithm
func makeRulesConfigMaps(p *victoriametricsv1beta1.VmAlert, ruleFiles map[string]string) ([]v1.ConfigMap, error) {
	//check if none of the rule files is too large for a single ConfigMap
	for filename, file := range ruleFiles {
		if len(file) > maxConfigMapDataSize {
			return nil, errors.Errorf(
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
		cm := makeRulesConfigMap(p, bucket)
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

func makeRulesConfigMap(p *victoriametricsv1beta1.VmAlert, ruleFiles map[string]string) v1.ConfigMap {
	boolTrue := true

	labels := map[string]string{labelPrometheusName: p.Name}
	for k, v := range managedByOperatorLabels {
		labels[k] = v
	}

	return v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   prometheusRuleConfigMapName(p.Name),
			Labels: labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         p.APIVersion,
					BlockOwnerDeletion: &boolTrue,
					Controller:         &boolTrue,
					Kind:               p.Kind,
					Name:               p.Name,
					UID:                p.UID,
				},
			},
		},
		Data: ruleFiles,
	}
}

func prometheusRuleConfigMapName(prometheusName string) string {
	return "prometheus-" + prometheusName + "-rulefiles"
}
