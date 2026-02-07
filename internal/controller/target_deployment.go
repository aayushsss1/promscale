package controller

import (
	"context"
	"fmt"

	autoscalingv1alpha1 "github.com/aayushsss1/promscale/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func deploymentCurrentReplicas(dep *appsv1.Deployment) int32 {
	if dep.Spec.Replicas == nil {
		return 0
	}
	return *dep.Spec.Replicas
}

func deploymentReadyReplicas(dep *appsv1.Deployment) int32 {
	return dep.Status.ReadyReplicas
}

func patchDeploymentReplicas(ctx context.Context, c client.Client, dep *appsv1.Deployment, desired int32) error {
	patch := client.MergeFrom(dep.DeepCopy())
	dep.Spec.Replicas = &desired
	return c.Patch(ctx, dep, patch)
}

func getTargetDeployment(ctx context.Context, c client.Client, scaler *autoscalingv1alpha1.InferenceScaler) (*appsv1.Deployment, error) {
	ns := scaler.Spec.TargetRef.Namespace
	if ns == "" {
		ns = scaler.Namespace
	}
	key := types.NamespacedName{Namespace: ns, Name: scaler.Spec.TargetRef.Name}

	var dep appsv1.Deployment
	if err := c.Get(ctx, key, &dep); err != nil {
		return nil, err
	}

	if scaler.Spec.TargetRef.APIVersion != "apps/v1" || scaler.Spec.TargetRef.Kind != "Deployment" {
		return nil, fmt.Errorf("unsupported targetRef: %s %s", scaler.Spec.TargetRef.APIVersion, scaler.Spec.TargetRef.Kind)
	}

	return &dep, nil
}
