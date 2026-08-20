package aws

import (
	"context"
	"fmt"
	"legacy-messenger-control-plane/internal/ports"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

var _ ports.EC2Port = (*EC2Client)(nil)

type EC2Client struct {
	client *ec2.Client
}

func NewEC2Client(
	ctx context.Context,
	region string,
) (*EC2Client, error) {

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to load aws config: %w",
			err,
		)
	}

	return &EC2Client{
		client: ec2.NewFromConfig(awsCfg),
	}, nil
}

func (c *EC2Client) GetPrivateIP(
	ctx context.Context,
	instanceID string,
) (string, error) {

	output, err := c.client.DescribeInstances(
		ctx,
		&ec2.DescribeInstancesInput{
			InstanceIds: []string{
				instanceID,
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf(
			"failed to describe EC2 instance: %w",
			err,
		)
	}

	if len(output.Reservations) == 0 ||
		len(output.Reservations[0].Instances) == 0 {
		return "", fmt.Errorf(
			"EC2 instance not found: %s",
			instanceID,
		)
	}

	privateIP := aws.ToString(
		output.Reservations[0].Instances[0].PrivateIpAddress,
	)

	if privateIP == "" {
		return "", fmt.Errorf(
			"EC2 private IP is empty: %s",
			instanceID,
		)
	}

	return privateIP, nil
}
