package controller

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autoscalingv1alpha1 "github.com/aayushsss1/promscale/api/v1alpha1"
)

func ptrToMetav1Time(t time.Time) *metav1.Time {
	mt := metav1.NewTime(t)
	return &mt
}

// setCondition upserts by Type and only bumps transition time on meaningful change.
func setCondition(conds *[]metav1.Condition, now time.Time, typ string, status metav1.ConditionStatus, reason, msg string) {
	c := metav1.Condition{
		Type:               typ,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.NewTime(now),
	}

	for i := range *conds {
		if (*conds)[i].Type == typ {
			prev := (*conds)[i]
			if prev.Status == c.Status && prev.Reason == c.Reason && prev.Message == c.Message {
				c.LastTransitionTime = prev.LastTransitionTime
			}
			(*conds)[i] = c
			return
		}
	}
	*conds = append(*conds, c)
}

func condStatus(ok bool) metav1.ConditionStatus {
	if ok {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func appendRecommendationBounded(status *autoscalingv1alpha1.InferenceScalerStatus, now time.Time, desired int32, maxEntries int) {
	status.Recommendations = append(status.Recommendations, autoscalingv1alpha1.ReplicaRecommendation{
		Timestamp:       metav1.NewTime(now),
		DesiredReplicas: desired,
	})
	if maxEntries > 0 && len(status.Recommendations) > maxEntries {
		status.Recommendations = status.Recommendations[len(status.Recommendations)-maxEntries:]
	}
}
