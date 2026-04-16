/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// JobDeletorSpec defines the desired state of JobDeletor

type JobDeletorSpec struct {
	// MaxAge defines how old a Job can be before deletion
	// Example values: "10m", "2h", "7d"
	// +kubebuilder:validation:Pattern=`^[0-9]+(s|m|h|d)$`
	MaxAge string `json:"maxAge"`

	// Namespaces is the list of namespaces to watch for Jobs
	// If empty, watches all namespaces
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// Selector allows filtering Jobs by labels
	// If empty, all Jobs are candidates for deletion
	// +optional
	Selector map[string]string `json:"selector,omitempty"`

	// CheckIntervalSeconds is how often to check for old Jobs (default: 3600)
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:default=3600
	// +optional
	CheckIntervalSeconds int32 `json:"checkIntervalSeconds,omitempty"`
}

// JobDeletorStatus defines the observed state of JobDeletor
type JobDeletorStatus struct {
	// Phase represents the current phase of the JobDeletor
	// +kubebuilder:validation:Enum=Starting;Active;Failed
	Phase string `json:"phase,omitempty"`

	// Message provides human-readable status information
	// +optional
	Message string `json:"message,omitempty"`

	// LastCheckTime is when we last checked for Jobs to delete
	// +optional
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`

	// JobsDeleted is the total number of Jobs deleted by this JobDeletor
	// +optional
	JobsDeleted int32 `json:"jobsDeleted,omitempty"`

	// LastDeletedJobs lists the most recent Jobs deleted (max 10)
	// +optional
	LastDeletedJobs []string `json:"lastDeletedJobs,omitempty"`

	// conditions represent the current state of the JobDeletor resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Jobs Deleted",type=integer,JSONPath=`.status.jobsDeleted`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// JobDeletor is the Schema for the jobdeletors API
type JobDeletor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   JobDeletorSpec   `json:"spec,omitempty"`
	Status JobDeletorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// JobDeletorList contains a list of JobDeletor
type JobDeletorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JobDeletor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&JobDeletor{}, &JobDeletorList{})
}
