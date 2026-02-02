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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InferenceScalerSpec defines the desired state of InferenceScaler.
type InferenceScalerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	TargetRef TargetReference `json:"targetRef,omitempty"`

	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default:=1
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	Prometheus PrometheusSpec `json:"prometheus"`

	Signals []ScalingSignal `json:"signals"`

	Behavior   *ScalingBehavior `json:"behavior,omitempty"`
	Safeguards *SafeguardsSpec  `json:"safeguards,omitempty"`
}

type TargetReference struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
}

type PrometheusSpec struct {
	URL string `json:"url"`

	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`
}

type ScalingBehavior struct {
	// +kubebuilder:default=15
	// +kubebuilder:validation:Minimum=5
	PollIntervalSeconds *int32 `json:"pollIntervalSeconds,omitempty"`

	ScaleUp   ScaleDirection `json:"scaleUp,omitempty"`
	ScaleDown ScaleDirection `json:"scaleDown,omitempty"`
}

type ScaleDirection struct {
	// +kubebuilder:validation:Minimum=1
	MaxStep *int32 `json:"maxStep,omitempty"`

	// +kubebuilder:validation:Minimum=0
	CooldownSeconds *int32 `json:"cooldownSeconds,omitempty"`

	// ScaleDown only: prevents flapping by using the max recommended replicas
	// observed in this lookback window.
	// +kubebuilder:validation:Minimum=0
	StabilizationWindowSeconds *int32 `json:"stabilizationWindowSeconds,omitempty"`
}

type SafeguardsSpec struct {
	Readiness ReadinessSafeguard `json:"readiness,omitempty"`

	// Hold | ScaleToMin | Error
	// +kubebuilder:validation:Enum=Hold;ScaleToMin;Error
	// +kubebuilder:default=Hold
	MissingMetricsPolicy string `json:"missingMetricsPolicy,omitempty"`
}

type ReadinessSafeguard struct {
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// Prevent scale-down for N seconds after a scale-up.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=0
	MinReadySecondsAfterScaleUp *int32 `json:"minReadySecondsAfterScaleUp,omitempty"`
}

type ScalingSignal struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// PromQL query expected to return a single scalar.
	// +kubebuilder:validation:MinLength=1
	Query string `json:"query"`

	Strategy SignalStrategy `json:"strategy"`
}

type SignalStrategy struct {
	// PerReplica | Threshold
	// +kubebuilder:validation:Enum=PerReplica;Threshold
	Type string `json:"type"`

	PerReplica *PerReplicaStrategy `json:"perReplica,omitempty"`
	Threshold  *ThresholdStrategy  `json:"threshold,omitempty"`
}

type PerReplicaStrategy struct {
	// Target value per replica (e.g., queue depth per pod).
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	Target string `json:"target"`
}

type ThresholdStrategy struct {
	// Above triggers the action if the metric is greater than this value.
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	Above *string `json:"above,omitempty"`

	// Below triggers the action if the metric is less than this value.
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	Below *string `json:"below,omitempty"`

	Action ThresholdAction `json:"action"`
}

type ThresholdAction struct {
	// ScaleUpBy | ScaleDownBy | Noop
	// +kubebuilder:validation:Enum=ScaleUpBy;ScaleDownBy;Noop
	Type string `json:"type"`

	// +kubebuilder:validation:Minimum=0
	ScaleUpBy *int32 `json:"scaleUpBy,omitempty"`

	// +kubebuilder:validation:Minimum=0
	ScaleDownBy *int32 `json:"scaleDownBy,omitempty"`
}

// InferenceScalerStatus defines the observed state of InferenceScaler.
type InferenceScalerStatus struct {
	// ObservedGeneration is the last generation of the spec that was processed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastPollTime is when the scaler last evaluated metrics and policy.
	LastPollTime *metav1.Time `json:"lastPollTime,omitempty"`

	// LastScaleTime is when the scaler last changed the target replicas.
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`

	// CurrentReplicas is the replica count currently set on the target.
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`

	// ReadyReplicas is the number of replicas that are ready to serve traffic.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// DesiredReplicas is the last desired replica count computed by PromScale.
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`

	// Signals records the last observed values for each configured signal.
	Signals []ObservedSignal `json:"signals,omitempty"`

	// Recommendations stores recent desired replica recommendations.
	// Used for stabilization windows to prevent flapping.
	Recommendations []ReplicaRecommendation `json:"recommendations,omitempty"`

	// Conditions represent the current state of the scaler using standard
	// Kubernetes condition semantics.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ObservedSignal captures the last observed value of a scaling signal.
type ObservedSignal struct {
	Name      string      `json:"name"`
	Value     string      `json:"value"`
	Timestamp metav1.Time `json:"timestamp"`
}

// ReplicaRecommendation represents a historical desired replica count computed by the scaling policy.
type ReplicaRecommendation struct {
	Timestamp       metav1.Time `json:"timestamp"`
	DesiredReplicas int32       `json:"desiredReplicas"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// InferenceScaler is the Schema for the inferencescalers API.
type InferenceScaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InferenceScalerSpec   `json:"spec,omitempty"`
	Status InferenceScalerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InferenceScalerList contains a list of InferenceScaler.
type InferenceScalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InferenceScaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InferenceScaler{}, &InferenceScalerList{})
}
