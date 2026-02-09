package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	autoscalingv1alpha1 "github.com/aayushsss1/promscale/api/v1alpha1"
)

type agentSignal struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type agentRequest struct {
	Scaler          string        `json:"scaler"`
	Namespace       string        `json:"namespace"`
	CurrentReplicas int32         `json:"currentReplicas"`
	ReadyReplicas   int32         `json:"readyReplicas"`
	MinReplicas     int32         `json:"minReplicas"`
	MaxReplicas     int32         `json:"maxReplicas"`
	Signals         []agentSignal `json:"signals"`
}

type agentResponse struct {
	DesiredReplicas int32  `json:"desiredReplicas"`
	Reason          string `json:"reason"`
	Confidence      string `json:"confidence"`
}

// callAgent asks the in-cluster agent for a desired replica count
func callAgent(ctx context.Context, llm autoscalingv1alpha1.LLMSpec, scaler *autoscalingv1alpha1.InferenceScaler, current int32, ready int32, observed []autoscalingv1alpha1.ObservedSignal) (desired int32, reason string, confidence string, err error) {

	// build URL
	endpoint := strings.TrimRight(llm.Endpoint, "/")
	url := endpoint

	// min/max
	minR := int32(1)
	if scaler.Spec.MinReplicas != nil {
		minR = *scaler.Spec.MinReplicas
	}
	maxR := scaler.Spec.MaxReplicas

	requestBody := agentRequest{
		Scaler:          scaler.Name,
		Namespace:       scaler.Namespace,
		CurrentReplicas: current,
		ReadyReplicas:   ready,
		MinReplicas:     minR,
		MaxReplicas:     maxR,
		Signals:         make([]agentSignal, 0, len(observed)),
	}
	for _, s := range observed {
		requestBody.Signals = append(requestBody.Signals, agentSignal{Name: s.Name, Value: s.Value})
	}

	b, err := json.Marshal(requestBody)
	if err != nil {
		return 0, "", "", fmt.Errorf("marshal agent request: %w", err)
	}

	// timeout
	timeout := 3 * time.Second
	if llm.TimeoutSeconds != nil && *llm.TimeoutSeconds > 0 {
		timeout = time.Duration(*llm.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, "", "", fmt.Errorf("agent http %d: %s", resp.StatusCode, string(body))
	}

	var parsed agentResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, "", "", fmt.Errorf("parse agent response: %w (body=%s)", err, string(body))
	}

	// validate and clamp the output (LLM output is untrusted)
	desired = parsed.DesiredReplicas
	if desired < minR {
		desired = minR
	}
	if desired > maxR {
		desired = maxR
	}

	reason = strings.TrimSpace(parsed.Reason)
	if len(reason) > 255 {
		reason = reason[:255]
	}

	confidence = strings.TrimSpace(parsed.Confidence)
	if confidence == "" {
		confidence = "0"
	}

	// enforce minConfidence if configured
	if llm.MinConfidence != "" {
		minConfidence, err1 := strconv.ParseFloat(llm.MinConfidence, 64)
		llmConfidence, err2 := strconv.ParseFloat(confidence, 64)
		if err1 == nil && err2 == nil {
			if llmConfidence < minConfidence {
				return 0, reason, confidence, fmt.Errorf("agent confidence %v < minimum defined confidence %v", llmConfidence, minConfidence)
			}
		}
	}

	return desired, reason, confidence, nil
}
