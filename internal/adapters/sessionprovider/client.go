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

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL,
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
