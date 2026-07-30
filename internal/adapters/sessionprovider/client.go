package sessionprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"legacy-messenger-control-plane/internal/domain"
	"net/http"
	"strings"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(
	baseURL string,
	httpClient *http.Client,
) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *Client) NotifyTaskRunning(
	ctx context.Context,
	task domain.TaskRunningEvent,
) error {

	body := scaleOutRequest{
		TaskID:       task.TaskID,
		SessionCount: 100,
	}

	requestBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal scale-out request: %w", err)
	}

	requestURL := c.baseURL + "/scale-out"

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		requestURL,
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return fmt.Errorf("failed to create scale-out request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf(
			"failed to notify task running: taskID=%s: %w",
			task.TaskID,
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK ||
		resp.StatusCode >= http.StatusMultipleChoices {

		responseBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf(
				"scale-out notification failed: taskID=%s status=%s",
				task.TaskID,
				resp.Status,
			)
		}

		return fmt.Errorf(
			"scale-out notification failed: taskID=%s status=%s response=%s",
			task.TaskID,
			resp.Status,
			string(responseBody),
		)
	}

	return nil
}

type taskStopRequest struct {
	TaskID string
}

func (c *Client) NotifyTaskStopped(ctx context.Context, serviceName string, taskID string) error {

	body := taskStopRequest{
		TaskID: taskID,
	}

	requestBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal report stop request: %w", err)
	}

	requestURL := c.baseURL + "/stop"

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		requestURL,
		bytes.NewReader(requestBody),
	)

	if err != nil {
		return fmt.Errorf("failed to report stop request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf(
			"failed to notify task stop: taskID=%s: %w",
			taskID,
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK ||
		resp.StatusCode >= http.StatusMultipleChoices {

		responseBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf(
				"report stop notification failed: taskID=%s status=%s",
				taskID,
				resp.Status,
			)
		}

		return fmt.Errorf(
			"report stop notification failed: taskID=%s status=%s response=%s",
			taskID,
			resp.Status,
			string(responseBody),
		)
	}

	return nil
}
