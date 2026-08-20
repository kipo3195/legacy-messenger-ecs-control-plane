package ports

import "context"

type EC2Port interface {
	GetPrivateIP(ctx context.Context, instanceID string) (string, error)
}
