package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type promAPIResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []any `json:"value"`
		} `json:"result"`
	} `json:"data"`
	ErrorType string `json:"errorType,omitempty"`
	Error     string `json:"error,omitempty"`
}

// pingPrometheus checks if Prometheus is present and ready.
func pingPrometheus(ctx context.Context, baseURL string, timeout time.Duration) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid prometheus url: %w", err)
	}
	u.Path = "/-/ready"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("prometheus not ready: http %d", resp.StatusCode)
	}
	return nil
}

// queryPrometheusScalar executes /api/v1/query and expects a single scalar-like result.
func queryPrometheusScalar(ctx context.Context, baseURL, promQL string, timeout time.Duration) (float64, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return 0, fmt.Errorf("invalid prometheus url: %w", err)
	}
	u.Path = "/api/v1/query"
	q := u.Query()
	q.Set("query", promQL)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("prometheus http %d: %s", resp.StatusCode, string(body))
	}

	var parsed promAPIResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, fmt.Errorf("parse prometheus response: %w", err)
	}
	if parsed.Status != "success" {
		if parsed.Error != "" {
			return 0, fmt.Errorf("prometheus error (%s): %s", parsed.ErrorType, parsed.Error)
		}
		return 0, errors.New("prometheus query failed")
	}

	if len(parsed.Data.Result) == 0 {
		return 0, errors.New("empty result")
	}
	if len(parsed.Data.Result[0].Value) < 2 {
		return 0, errors.New("invalid result format")
	}

	valStr, ok := parsed.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, errors.New("result value not string")
	}
	f, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float: %w", err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("nan/inf result")
	}
	return f, nil
}

func isPrometheusReady(ctx context.Context, baseURL string, timeout time.Duration) (bool, string) {
	if err := pingPrometheus(ctx, baseURL, timeout); err != nil {
		return false, err.Error()
	}
	return true, "prometheus is ready"
}
