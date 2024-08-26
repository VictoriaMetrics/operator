package build

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
)

// AddDefaultsToSTS sets default values to statefulset spec
func AddDefaultsToSTS(sts *appsv1.StatefulSetSpec) {
	if sts.UpdateStrategy.Type == "" {
		sts.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	}
	if sts.PodManagementPolicy == "" {
		sts.PodManagementPolicy = appsv1.ParallelPodManagement
		if sts.MinReadySeconds > 0 || sts.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType {
			sts.PodManagementPolicy = appsv1.OrderedReadyPodManagement
		}
	}
	if sts.RevisionHistoryLimit == nil {
		sts.RevisionHistoryLimit = ptr.To[int32](10)
	}

	if sts.PersistentVolumeClaimRetentionPolicy == nil {
		sts.PersistentVolumeClaimRetentionPolicy = &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
			WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
			WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
		}
	}
}
