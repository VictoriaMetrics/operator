package reconcile

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

type stsPVCInput struct {
	New            *appsv1.StatefulSet            `json:"new"`
	Prev           *appsv1.StatefulSet            `json:"prev"`
	Existing       *appsv1.StatefulSet            `json:"existing"`
	PVCs           []corev1.PersistentVolumeClaim `json:"pvcs"`
	StorageClasses []storagev1.StorageClass       `json:"storageClasses"`
	Ignore         []int32                        `json:"ignore"`
	Live           int32                          `json:"live"`
}

// normalizeObj makes obj acceptable for the fake client
func normalizeObj(obj client.Object, name, ns string) {
	obj.SetName(name)
	obj.SetNamespace(ns)
	obj.SetResourceVersion("")
	obj.SetManagedFields(nil)
	obj.SetDeletionTimestamp(nil)
}

// cloneObjects gives each fake client its own copies
func cloneObjects(src []runtime.Object) []runtime.Object {
	dst := make([]runtime.Object, 0, len(src))
	for _, obj := range src {
		dst = append(dst, obj.DeepCopyObject())
	}
	return dst
}

func FuzzSTSPVCExpansion(f *testing.F) {
	const (
		sts      = `"selector":{"matchLabels":{"a":"b"}},"template":{"metadata":{"labels":{"a":"b"}}}`
		vct1     = `"volumeClaimTemplates":[{"metadata":{"name":"data"},"spec":{"resources":{"requests":{"storage":"1Gi"}}}}]`
		vct2     = `"volumeClaimTemplates":[{"metadata":{"name":"data","labels":{"x":"y"}},"spec":{"storageClassName":"sc","resources":{"requests":{"storage":"2Gi"}}}}]`
		pvcMeta  = `"metadata":{"name":"data-fuzz-0","labels":{"a":"b"}`
		pvc1     = `{` + pvcMeta + `},"spec":{"storageClassName":"sc","resources":{"requests":{"storage":"1Gi"}}},"status":{"capacity":{"storage":"1Gi"}}}`
		scExpand = `{"metadata":{"name":"sc"},"allowVolumeExpansion":true}`
	)
	seeds := []string{
		`{}`,
		`{"new":{"spec":{` + sts + `,` + vct2 + `}},"existing":{"spec":{` + sts + `,` + vct1 + `}},"prev":{"spec":{` + vct1 + `}}}`,
		`{"new":{"spec":{"replicas":1,` + sts + `,` + vct2 + `}},"existing":{"spec":{"replicas":1,` + sts + `,` + vct2 + `}},"pvcs":[` + pvc1 + `],"storageClasses":[` + scExpand + `],"live":1}`,
		`{"new":{"spec":{"replicas":1,` + sts + `,` + vct2 + `}},"pvcs":[` + pvc1 + `],"storageClasses":[{"metadata":{"name":"sc"}}]}`,
		`{"new":{"spec":{"replicas":1,` + sts + `,` + vct2 + `}},"pvcs":[{` + pvcMeta + `,"annotations":{"operator.victoriametrics.com/pvc-allow-volume-expansion":"true"}},"spec":{"resources":{"requests":{"storage":"1Gi"}}},` +
			`"status":{"capacity":{"storage":"1Gi"},"conditions":[{"type":"Resizing","status":"True"}]}}]}`,
		`{"new":{"spec":{"replicas":1,` + sts + `,` + vct1 + `}},"pvcs":[{` + pvcMeta + `,"annotations":{"operator.victoriametrics.com/pvc-allow-volume-expansion":"bad"}},"spec":{"resources":{"requests":{"storage":"3Gi"}}}}]}`,
		`{"new":{"spec":{"replicas":1,` + sts + `,` + vct2 + `}},"pvcs":[{"metadata":{"name":"data-fuzz-0","labels":{"a":"b"},"annotations":{"volume.beta.kubernetes.io/storage-class":""}},"spec":{"resources":{"requests":{"storage":"1Gi"}}}}],` +
			`"storageClasses":[{"metadata":{"name":"def","annotations":{"storageclass.kubernetes.io/is-default-class":"true"}},"allowVolumeExpansion":true}]}`,
		`{"new":{"spec":{"replicas":1,` + sts + `,"volumeClaimTemplates":[{"metadata":{"name":"data"},"spec":{"resources":{"requests":{"storage":"1Gi"}}}}]}},` +
			`"pvcs":[{` + pvcMeta + `},"spec":{"volumeAttributesClassName":"fast","resources":{"requests":{"storage":"1Gi"}}},"status":{"currentVolumeAttributesClassName":"fast"}}]}`,
		`{"new":{"spec":{"replicas":2,` + sts + `,"volumeClaimTemplates":[{"metadata":{"name":"data"},"spec":{"volumeAttributesClassName":"slow","resources":{"requests":{"storage":"2Gi"}}}}]}},` +
			`"pvcs":[` + pvc1 + `,{"metadata":{"name":"data-fuzz-1","labels":{"a":"b"}},"spec":{"resources":{"requests":{"storage":"1Gi"}}}},{"metadata":{"name":"bad","labels":{"a":"b"}}},{"metadata":{"name":"other-fuzz-0","labels":{"a":"b"}}}],"ignore":[1]}`,
		`{"new":{"spec":{"replicas":1,` + sts + `,` + vct1 + `}},"live":1}`,
		`{"new":{"spec":{"replicas":0,` + sts + `,` + vct1 + `}}}`,
		`{"new":{"spec":{"serviceName":"a",` + sts + `,` + vct1 + `}},"existing":{"spec":{"serviceName":"b",` + sts + `,` + vct1 + `}}}`,
		`{"new":{"spec":{` + sts + `,"volumeClaimTemplates":[{"metadata":{"name":"other"}}]}},"existing":{"spec":{` + sts + `,` + vct1 + `}}}`,
		`{"new":{"spec":{"podManagementPolicy":"Parallel",` + sts + `,` + vct1 + `}},"existing":{"spec":{` + sts + `,` + vct1 + `}}}`,
		`{"new":{"spec":{` + sts + `,"template":{"spec":{"volumes":[{"name":"data"}],"containers":[{"name":"c","volumeMounts":[{"name":"x"}]}]}},` + vct1 + `}}}`,
	}
	for _, seed := range seeds {
		f.Add(false, []byte(seed))
		f.Add(true, []byte(seed))
	}
	f.Fuzz(func(t *testing.T, flag bool, data []byte) {
		var in stsPVCInput
		if err := json.Unmarshal(data, &in); err != nil {
			fuzztest.New(data).Fill(&in)
		}
		if in.New == nil || in.New.Spec.Selector == nil {
			return
		}

		// flag toggles single namespace mode and VAC removal status scope
		cfg := config.MustGetBaseConfig()
		watch, major, minor := cfg.WatchNamespaces, k8stools.ServerMajorVersion, k8stools.ServerMinorVersion
		t.Cleanup(func() {
			cfg.WatchNamespaces, k8stools.ServerMajorVersion, k8stools.ServerMinorVersion = watch, major, minor
		})
		cfg.WatchNamespaces = nil
		k8stools.ServerMajorVersion = 1
		k8stools.ServerMinorVersion = 33
		if flag {
			cfg.WatchNamespaces = []string{"default"}
			k8stools.ServerMinorVersion = 34
		}

		var objects []runtime.Object
		for _, obj := range []*appsv1.StatefulSet{in.New, in.Prev, in.Existing} {
			if obj != nil {
				normalizeObj(obj, "fuzz", "default")
			}
		}
		if in.Existing != nil {
			objects = append(objects, in.Existing)
		}
		for i := range in.PVCs {
			p := &in.PVCs[i]
			if p.Name == "" {
				continue
			}
			normalizeObj(p, p.Name, "default")
			objects = append(objects, p)
		}
		for i := range in.StorageClasses {
			sc := &in.StorageClasses[i]
			if sc.Name == "" {
				continue
			}
			normalizeObj(sc, sc.Name, "")
			objects = append(objects, sc)
		}
		ignore := sets.New(in.Ignore...)
		var prevVCTs []corev1.PersistentVolumeClaim
		if in.Prev != nil {
			prevVCTs = in.Prev.Spec.VolumeClaimTemplates
		}

		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			if in.Existing != nil {
				isSTSRecreateRequired(ctx, in.Existing, in.New, prevVCTs)
			}

			// direct call reaches PVC updates without STS rollout
			rclient := k8stools.GetTestClientWithObjects(cloneObjects(objects))
			_ = updateSTSPVC(ctx, rclient, in.New.DeepCopy(), prevVCTs, in.Live, ignore)

			rclient = k8stools.GetTestClientWithObjects(cloneObjects(objects))
			_ = StatefulSet(ctx, rclient, in.New, in.Prev, nil, &StatefulSetOpts{IgnoreNotReadyPodOrdinals: ignore})
		})
	})
}
