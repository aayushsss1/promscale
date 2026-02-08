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

package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/aayushsss1/promscale/api/v1alpha1"
)

// InferenceScalerReconciler reconciles a InferenceScaler object
type InferenceScalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=autoscaling.mlop.io,resources=inferencescalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.mlop.io,resources=inferencescalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.mlop.io,resources=inferencescalers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;patch;update

func (r *InferenceScalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	now := time.Now()

	var scaler autoscalingv1alpha1.InferenceScaler
	if err := r.Get(ctx, req.NamespacedName, &scaler); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Poll interval
	interval := 15 * time.Second
	if scaler.Spec.Behavior != nil && scaler.Spec.Behavior.PollIntervalSeconds != nil && *scaler.Spec.Behavior.PollIntervalSeconds > 0 {
		interval = time.Duration(*scaler.Spec.Behavior.PollIntervalSeconds) * time.Second
	}

	// Validate target is Deployment
	if scaler.Spec.TargetRef.APIVersion != "apps/v1" || scaler.Spec.TargetRef.Kind != "Deployment" {
		scaler.Status.ObservedGeneration = scaler.Generation
		scaler.Status.LastPollTime = ptrToMetav1Time(now)

		setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionFalse, "unsupported_target", "Only apps/v1 Deployment targets are supported")

		_ = r.Status().Update(ctx, &scaler)
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	// Fetch target deployment
	dep, err := getTargetDeployment(ctx, r.Client, &scaler)
	if err != nil {
		scaler.Status.ObservedGeneration = scaler.Generation
		scaler.Status.LastPollTime = ptrToMetav1Time(now)
		setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionFalse,
			"target_not_found", fmt.Sprintf("Failed to get target Deployment: %v", err))

		_ = r.Status().Update(ctx, &scaler)
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	current := deploymentCurrentReplicas(dep)
	ready := deploymentReadyReplicas(dep)

	scaler.Status.CurrentReplicas = current
	scaler.Status.ReadyReplicas = ready
	scaler.Status.ObservedGeneration = scaler.Generation
	scaler.Status.LastPollTime = ptrToMetav1Time(now)

	// Prometheus timeout
	promTimeout := 5 * time.Second
	if scaler.Spec.Prometheus.TimeoutSeconds != nil && *scaler.Spec.Prometheus.TimeoutSeconds > 0 {
		promTimeout = time.Duration(*scaler.Spec.Prometheus.TimeoutSeconds) * time.Second
	}

	// Check Prometheus availability
	promOK, promMsg := isPrometheusReady(ctx, scaler.Spec.Prometheus.URL, promTimeout)
	setCondition(&scaler.Status.Conditions, now, "MetricsAvailable", condStatus(promOK), boolReason(promOK, "prometheus_ready", "prometheus_unreachable"), promMsg)

	if !promOK {
		policy := "Hold"
		if scaler.Spec.Safeguards != nil && scaler.Spec.Safeguards.MissingMetricsPolicy != "" {
			policy = scaler.Spec.Safeguards.MissingMetricsPolicy
		}

		switch policy {
		case "ScaleToMin":
			minR := int32(1)
			if scaler.Spec.MinReplicas != nil {
				minR = *scaler.Spec.MinReplicas
			}
			desired := minR

			scaler.Status.DesiredReplicas = desired
			appendRecommendationBounded(&scaler.Status, now, desired, 40)

			if desired != current {
				if err := patchDeploymentReplicas(ctx, r.Client, dep, desired); err != nil {
					log.Error(err, "failed to scale Deployment to minReplicas")
					setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionFalse,
						"scale_failed", fmt.Sprintf("Failed to patch replicas: %v", err))
					_ = r.Status().Update(ctx, &scaler)
					return ctrl.Result{}, err
				}
				scaler.Status.LastScaleTime = ptrToMetav1Time(now)
			}

			setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionTrue,
				"scaled_to_min", "Prometheus unavailable; applied ScaleToMin policy")

			_ = r.Status().Update(ctx, &scaler)
			return ctrl.Result{RequeueAfter: interval}, nil

		case "Error":
			setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionFalse,
				"metrics_unavailable", "Prometheus unavailable; MissingMetricsPolicy=Error")
			_ = r.Status().Update(ctx, &scaler)
			return ctrl.Result{RequeueAfter: interval}, fmt.Errorf("prometheus unavailable: %s", promMsg)

		default:
			scaler.Status.DesiredReplicas = current
			appendRecommendationBounded(&scaler.Status, now, current, 40)
			setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionFalse,
				"hold_missing_metrics", "Prometheus unavailable; holding replicas")
			_ = r.Status().Update(ctx, &scaler)
			return ctrl.Result{RequeueAfter: interval}, nil
		}
	}

	// Evaluate signals (deterministic)
	eval := evaluateSignals(ctx, now, scaler.Spec, current)
	scaler.Status.Signals = eval.observed

	setCondition(&scaler.Status.Conditions, now, "SignalsEvaluated", condStatus(eval.metricsOK), boolReason(eval.metricsOK, "signals_ok", "signals_error"), eval.metricsErrHint)

	rawDesired := eval.rawDesired

	// populate last decision baseline
	scaler.Status.LastDecision = &autoscalingv1alpha1.DecisionStatus{
		Mode:   "Deterministic",
		Source: "signals",
		Reason: "computed from signals",
		Time:   ptrToMetav1Time(now),
	}

	// If Decision is nil, default is deterministic
	mode := "Deterministic"
	if scaler.Spec.Decision != nil && scaler.Spec.Decision.Mode != "" {
		mode = scaler.Spec.Decision.Mode
	}

	if mode == "LLM" && scaler.Spec.Decision != nil && scaler.Spec.Decision.LLM != nil {
		desired, reason, confidence, err := callAgent(ctx, *scaler.Spec.Decision.LLM, &scaler, current, ready, eval.observed)
		if err != nil {
			// record failure in status
			scaler.Status.LastDecision = &autoscalingv1alpha1.DecisionStatus{
				Mode:       "LLM",
				Source:     "agent",
				Reason:     "agent call failed; applying fallback",
				AgentError: err.Error(),
				Time:       ptrToMetav1Time(now),
			}

			setCondition(&scaler.Status.Conditions, now, "LLMAvailable", metav1.ConditionFalse, "agent_error", err.Error())

			onErr := "FallbackToDeterministic"
			if scaler.Spec.Decision.LLM.OnError != "" {
				onErr = scaler.Spec.Decision.LLM.OnError
			}

			switch onErr {
			case "Hold":
				rawDesired = current
				scaler.Status.LastDecision.Source = "fallback"
				scaler.Status.LastDecision.Reason = "LLM failed, replica scaling held."
			case "Error":
				_ = r.Status().Update(ctx, &scaler)
				return ctrl.Result{RequeueAfter: interval}, err
			default: // FallbackToDeterministic
				rawDesired = eval.rawDesired
				scaler.Status.LastDecision.Source = "fallback"
				scaler.Status.LastDecision.Reason = "LLM failed falling back to deterministic approach using signals"
			}
		} else {
			rawDesired = desired

			scaler.Status.LastDecision = &autoscalingv1alpha1.DecisionStatus{
				Mode:       "LLM",
				Source:     "agent",
				Reason:     reason,
				Confidence: confidence,
				Time:       ptrToMetav1Time(now),
			}

			setCondition(&scaler.Status.Conditions, now, "LLMAvailable", metav1.ConditionTrue, "agent_ok", "LLM agent responded")
		}
	}

	scaler.Status.DesiredReplicas = rawDesired
	appendRecommendationBounded(&scaler.Status, now, rawDesired, 40)

	// Apply behavior rails for both LLM and Deterministic approach
	decision := applyBehavior(now, current, rawDesired, scaler.Spec, scaler.Status)
	scaler.Status.DesiredReplicas = decision.finalDesired

	setCondition(&scaler.Status.Conditions, now, "ScalingLimited", condStatus(decision.scalingLimited),
		boolReason(decision.scalingLimited, "limited", "not_limited"),
		boolMsg(decision.scalingLimited, "Scaling output limited by bounds/rails", "Scaling output not limited"))

	if decision.blocked {
		setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionFalse,
			decision.blockReason, "Scaling blocked by behavior safeguards")
		_ = r.Status().Update(ctx, &scaler)
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	if decision.finalDesired != current {
		if err := patchDeploymentReplicas(ctx, r.Client, dep, decision.finalDesired); err != nil {
			log.Error(err, "failed to patch deployment replicas")
			setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionFalse,
				"scale_failed", fmt.Sprintf("Failed to patch replicas: %v", err))
			_ = r.Status().Update(ctx, &scaler)
			return ctrl.Result{}, err
		}
		scaler.Status.LastScaleTime = ptrToMetav1Time(now)
		setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionTrue,
			"scaled", fmt.Sprintf("Scaled Deployment to %d", decision.finalDesired))
	} else {
		setCondition(&scaler.Status.Conditions, now, "ScalingActive", metav1.ConditionTrue,
			"no_change", "Desired replicas equals current replicas")
	}

	err = r.Status().Update(ctx, &scaler)

	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *InferenceScalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.InferenceScaler{}).
		Owns(&appsv1.Deployment{}).
		Named("inferencescaler").
		Complete(r)
}

// basic helper functions
func boolReason(ok bool, t, f string) string {
	if ok {
		return t
	}
	return f
}
func boolMsg(ok bool, t, f string) string {
	if ok {
		return t
	}
	return f
}
