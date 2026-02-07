package controller

import (
	"time"

	autoscalingv1alpha1 "github.com/aayushsss1/promscale/api/v1alpha1"
)

type behaviorDecision struct {
	finalDesired   int32
	blocked        bool
	blockReason    string
	scalingLimited bool
}

func getMinMax(spec autoscalingv1alpha1.InferenceScalerSpec) (int32, int32) {
	minR := int32(1)
	if spec.MinReplicas != nil {
		minR = *spec.MinReplicas
	}
	maxR := spec.MaxReplicas
	if maxR < minR {
		maxR = minR
	}
	return minR, maxR
}

func pollIntervalSeconds(spec autoscalingv1alpha1.InferenceScalerSpec) int32 {
	if spec.Behavior != nil && spec.Behavior.PollIntervalSeconds != nil && *spec.Behavior.PollIntervalSeconds > 0 {
		return *spec.Behavior.PollIntervalSeconds
	}
	return 15
}

// Infer last scaling direction using last desired/current.
func inferLastScaleWasUp(st autoscalingv1alpha1.InferenceScalerStatus) bool {
	return st.DesiredReplicas > st.CurrentReplicas
}

func applyBehavior(now time.Time, current int32, rawDesired int32, spec autoscalingv1alpha1.InferenceScalerSpec, status autoscalingv1alpha1.InferenceScalerStatus) behaviorDecision {

	minR, maxR := getMinMax(spec)

	desired := clampInt32(rawDesired, minR, maxR)
	limited := desired != rawDesired

	if desired == current {
		return behaviorDecision{finalDesired: desired, scalingLimited: limited}
	}

	scaleUp := desired > current

	// Extract lastScaleTime
	var lastScaleTime *time.Time
	if status.LastScaleTime != nil {
		t := status.LastScaleTime.Time
		lastScaleTime = &t
	}

	// Cooldown (direction-aware)
	if lastScaleTime != nil && spec.Behavior != nil {
		var cd *int32
		if scaleUp {
			cd = spec.Behavior.ScaleUp.CooldownSeconds
		} else {
			cd = spec.Behavior.ScaleDown.CooldownSeconds
		}
		if cd != nil && *cd > 0 {
			if now.Sub(*lastScaleTime) < time.Duration(*cd)*time.Second {
				return behaviorDecision{
					finalDesired:   desired,
					blocked:        true,
					blockReason:    "cooldown_active",
					scalingLimited: limited,
				}
			}
		}
	}

	// Readiness safeguard (block scale-down shortly after scale-up)
	readinessEnabled := true
	minReadyAfterUp := int32(60)

	if spec.Safeguards != nil {
		if spec.Safeguards.Readiness.Enabled != nil {
			readinessEnabled = *spec.Safeguards.Readiness.Enabled
		}
		if spec.Safeguards.Readiness.MinReadySecondsAfterScaleUp != nil {
			minReadyAfterUp = *spec.Safeguards.Readiness.MinReadySecondsAfterScaleUp
		}
	}

	if readinessEnabled && !scaleUp && lastScaleTime != nil && minReadyAfterUp > 0 {
		lastWasUp := inferLastScaleWasUp(status)
		if lastWasUp {
			wait := time.Duration(minReadyAfterUp) * time.Second
			if now.Sub(*lastScaleTime) < wait {
				return behaviorDecision{
					finalDesired:   desired,
					blocked:        true,
					blockReason:    "readiness_guard_active",
					scalingLimited: limited,
				}
			}
		}
	}

	// Stabilization window (scale-down only)
	// Use the MAX recommendation in the lookback window to avoid flapping down too quickly.
	if !scaleUp && spec.Behavior != nil &&
		spec.Behavior.ScaleDown.StabilizationWindowSeconds != nil &&
		*spec.Behavior.ScaleDown.StabilizationWindowSeconds > 0 {

		window := time.Duration(*spec.Behavior.ScaleDown.StabilizationWindowSeconds) * time.Second
		cutoff := now.Add(-window)

		maxRec := desired
		found := false
		for _, rr := range status.Recommendations {
			if rr.Timestamp.Time.After(cutoff) {
				found = true
				if rr.DesiredReplicas > maxRec {
					maxRec = rr.DesiredReplicas
				}
			}
		}

		if found && maxRec > desired {
			desired = maxRec
			limited = true
		}
	}

	// MaxStep limiting (direction-aware)
	if spec.Behavior != nil {
		var maxStep *int32
		if scaleUp {
			maxStep = spec.Behavior.ScaleUp.MaxStep
		} else {
			maxStep = spec.Behavior.ScaleDown.MaxStep
		}

		if maxStep != nil && *maxStep > 0 {
			delta := desired - current
			if delta < 0 {
				delta = -delta
			}
			if delta > *maxStep {
				if scaleUp {
					desired = current + *maxStep
				} else {
					desired = current - *maxStep
					if desired < 0 {
						desired = 0
					}
				}
				limited = true
			}
		}
	}

	return behaviorDecision{
		finalDesired:   desired,
		blocked:        false,
		scalingLimited: limited,
	}
}
