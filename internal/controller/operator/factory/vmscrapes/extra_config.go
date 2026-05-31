package vmscrapes

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
)

type extraConfigOwner interface {
	PrefixedName() string
	GetNamespace() string
	FinalLabels() map[string]string
	AsOwner() metav1.OwnerReference
}

// ExtraConfigSecretName returns the name of the Nth overflow secret for cr.
func ExtraConfigSecretName(cr extraConfigOwner, idx int) string {
	return fmt.Sprintf("%s-sc-%d", cr.PrefixedName(), idx)
}

// BuildExtraConfigSecret creates a Secret with gzip-compressed YAML for one overflow bucket.
func BuildExtraConfigSecret(cr extraConfigOwner, idx int, data []byte) *corev1.Secret {
	owner := cr.AsOwner()
	lbls := cr.FinalLabels()
	lbls[ExtraConfigLabelKey] = cr.PrefixedName()
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            ExtraConfigSecretName(cr, idx),
			Namespace:       cr.GetNamespace(),
			Labels:          lbls,
			Annotations:     map[string]string{"generated": "true"},
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Data: map[string][]byte{
			ExtraConfigFilename: data,
		},
	}
}

// RemoveStaleExtraConfigSecrets deletes overflow Secrets with index > activeCount.
func RemoveStaleExtraConfigSecrets(ctx context.Context, rclient client.Client, cr extraConfigOwner, activeCount int) error {
	var list corev1.SecretList
	if err := rclient.List(ctx, &list,
		client.InNamespace(cr.GetNamespace()),
		client.MatchingLabels{ExtraConfigLabelKey: cr.PrefixedName()},
	); err != nil {
		return fmt.Errorf("listing stale extra scrape config secrets: %w", err)
	}
	activePrefix := cr.PrefixedName() + "-sc-"
	for i := range list.Items {
		s := &list.Items[i]
		if strings.HasPrefix(s.Name, activePrefix) {
			if idx, err := strconv.Atoi(s.Name[len(activePrefix):]); err == nil && idx <= activeCount {
				continue
			}
		}
		if err := finalize.SafeDelete(ctx, rclient, s); err != nil {
			return err
		}
	}
	return nil
}

// Shared constants for overflow scrape config Secrets (VMAgent and VMSingle).
const (
	// ExtraConfigFilename is the key in each overflow Secret holding the gzip-compressed job list.
	ExtraConfigFilename = "jobs.yaml"
	// ExtraConfigLabelKey is set on overflow Secrets for label-selector-based cleanup.
	ExtraConfigLabelKey = "operator.victoriametrics.com/scrape-config-extra"
	// ExtraConfigOutDir is where the config-reloader writes decompressed extra job files.
	ExtraConfigOutDir = "/etc/vm/sc-files"
	// ExtraConfigRawDirFmt is the mount path format for read-only extra Secret volumes.
	ExtraConfigRawDirFmt = "/etc/vm/sc-raw-%d"
	// ExtraConfigFilesGlob is added to the main scrape config when overflow is active.
	ExtraConfigFilesGlob = "/etc/vm/sc-files/*/jobs.yaml"
)

// PackJobsIntoBuckets packs scrape jobs into gzip-compressed buckets within limit bytes each.
// Fast path: one compression check; if everything fits, return a single bucket.
// Slow path: estimate bucket count from total compressed size, pre-split by job count,
// recurse on each chunk. Recursion terminates because each chunk is strictly smaller.
func PackJobsIntoBuckets(jobs []yaml.MapSlice, limit int) ([][]yaml.MapSlice, error) {
	data, err := yaml.Marshal(jobs)
	if err != nil {
		return nil, fmt.Errorf("marshalling scrape jobs: %w", err)
	}
	compressed, err := build.GzipConfig(data)
	if err != nil {
		return nil, fmt.Errorf("compressing scrape jobs: %w", err)
	}
	if len(compressed) <= limit {
		return [][]yaml.MapSlice{jobs}, nil
	}
	if len(jobs) == 1 {
		return nil, fmt.Errorf("single scrape job exceeds compressed bucket size limit (%d > %d bytes)", len(compressed), limit)
	}

	// Estimate number of buckets with 50% headroom (per-bucket compression may be worse).
	numBuckets := (len(compressed)*3/2 + limit - 1) / limit
	jobsPerBucket := (len(jobs) + numBuckets - 1) / numBuckets

	var result [][]yaml.MapSlice
	for i := 0; i < len(jobs); i += jobsPerBucket {
		end := i + jobsPerBucket
		if end > len(jobs) {
			end = len(jobs)
		}
		sub, err := PackJobsIntoBuckets(jobs[i:end], limit)
		if err != nil {
			return nil, err
		}
		result = append(result, sub...)
	}
	return result, nil
}
