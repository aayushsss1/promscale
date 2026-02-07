package controller

import (
	"context"
	"math"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autoscalingv1alpha1 "github.com/aayushsss1/promscale/api/v1alpha1"
)

type signalEvalResult struct {
	rawDesired     int32
	observed       []autoscalingv1alpha1.ObservedSignal
	metricsOK      bool
	metricsErrHint string
}

// evaluateSignals queries Prometheus for each signal and computes a raw desired replica count.
// NOTE: This function assumes Prometheus is reachable (controller checks readiness first).
func evaluateSignals(ctx context.Context, now time.Time, spec autoscalingv1alpha1.InferenceScalerSpec, current int32) signalEvalResult {

	res := signalEvalResult{
		rawDesired: 0,
		observed:   make([]autoscalingv1alpha1.ObservedSignal, 0, len(spec.Signals)),
		metricsOK:  true,
	}

	timeout := 5 * time.Second
	if spec.Prometheus.TimeoutSeconds != nil && *spec.Prometheus.TimeoutSeconds > 0 {
		timeout = time.Duration(*spec.Prometheus.TimeoutSeconds) * time.Second
	}

	// Track additive effect of threshold signals separately.
	thresholdDelta := int32(0)

	// check if per replica signal is present
	hadPerReplica := false

	for _, sig := range spec.Signals {
		val, err := queryPrometheusScalar(ctx, spec.Prometheus.URL, sig.Query, timeout)
		if err != nil {
			res.metricsOK = false
			res.metricsErrHint = "one or more signals failed"
			continue
		}

		// Store observed signal in status as string
		valStr := strconv.FormatFloat(val, 'f', -1, 64)
		res.observed = append(res.observed, autoscalingv1alpha1.ObservedSignal{
			Name:      sig.Name,
			Value:     valStr,
			Timestamp: metav1.NewTime(now),
		})

		switch sig.Strategy.Type {
		case "PerReplica":
			if sig.Strategy.PerReplica == nil {
				res.metricsOK = false
				res.metricsErrHint = "invalid perReplica strategy"
				continue
			}
			hadPerReplica = true
			target, err := strconv.ParseFloat(sig.Strategy.PerReplica.Target, 64)
			if err != nil || target <= 0 {
				res.metricsOK = false
				res.metricsErrHint = "invalid perReplica.target"
				continue
			}

			// desired = ceil(metric / target)
			rec := int32(math.Ceil(val / target))
			if rec < 1 {
				rec = 1
			}
			res.rawDesired = max(rec, res.rawDesired)

		case "Threshold":
			if sig.Strategy.Threshold == nil {
				res.metricsOK = false
				res.metricsErrHint = "invalid threshold strategy"
				continue
			}

			th := sig.Strategy.Threshold
			trigger := false

			if th.Above != nil {
				above, err := strconv.ParseFloat(*th.Above, 64)
				if err != nil {
					res.metricsOK = false
					res.metricsErrHint = "invalid threshold.above"
					continue
				}
				if val > above {
					trigger = true
				}
			}

			if th.Below != nil {
				below, err := strconv.ParseFloat(*th.Below, 64)
				if err != nil {
					res.metricsOK = false
					res.metricsErrHint = "invalid threshold.below"
					continue
				}
				if val < below {
					trigger = true
				}
			}

			if !trigger {
				continue
			}

			switch th.Action.Type {
			case "ScaleUpBy":
				if th.Action.ScaleUpBy != nil && *th.Action.ScaleUpBy > 0 {
					thresholdDelta += *th.Action.ScaleUpBy
				}
			case "ScaleDownBy":
				if th.Action.ScaleDownBy != nil && *th.Action.ScaleDownBy > 0 {
					thresholdDelta -= *th.Action.ScaleDownBy
				}
			case "Noop":
				// do nothing
			default:
				res.metricsOK = false
				res.metricsErrHint = "unknown threshold.action.type"
			}

		default:
			res.metricsOK = false
			res.metricsErrHint = "unknown signal strategy type"
		}
	}

	// Apply threshold delta after evaluating all signals
	// This keeps PerReplica max behavior intact while allowing Threshold nudges

	base := res.rawDesired

	if !hadPerReplica {
		base = current
	}

	raw := base + thresholdDelta
	if raw < 0 {
		raw = 0
	}
	res.rawDesired = raw

	if res.metricsErrHint == "" {
		if res.metricsOK {
			res.metricsErrHint = "all signals queried successfully"
		} else {
			res.metricsErrHint = "metrics unavailable"
		}
	}
	return res
}
