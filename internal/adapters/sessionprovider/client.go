package sessionprovider

import (
	"context"
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

	return nil
}
