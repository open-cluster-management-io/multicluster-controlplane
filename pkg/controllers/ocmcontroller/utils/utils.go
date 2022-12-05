// Copyright Contributors to the Open Cluster Management project
package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

// ContainsString to check string from a slice of strings.
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// ContainsString to remove string from a slice of strings.
func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

func ClusterIsOffLine(conditions []metav1.Condition) bool {
	return meta.IsStatusConditionPresentAndEqual(conditions, clusterapiv1.ManagedClusterConditionAvailable, metav1.ConditionUnknown)
}
