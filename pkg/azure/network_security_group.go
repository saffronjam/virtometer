package azure

import (
	"context"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v2"
)

func (c *Client) GetNetworkSecurityGroup(ctx context.Context, name, resourceGroup string) (*armnetwork.SecurityGroup, error) {
	resp, err := c.SecurityGroupsClient.Get(ctx, resourceGroup, name, nil)

	if err != nil {
		return nil, err
	}

	return &resp.SecurityGroup, nil
}

func (c *Client) CreateNetworkSecurityGroup(ctx context.Context, name, resourceGroup string) (*armnetwork.SecurityGroup, error) {
	pResp, err := c.SecurityGroupsClient.BeginCreateOrUpdate(ctx, resourceGroup, name, armnetwork.SecurityGroup{
		Location: to.Ptr(c.Location),
	}, nil)

	if err != nil {
		return nil, err
	}

	resp, err := pResp.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &resp.SecurityGroup, nil
}

func (c *Client) DeleteNetworkSecurityGroup(ctx context.Context, name, resourceGroup string) error {
	pResp, err := c.SecurityGroupsClient.BeginDelete(ctx, resourceGroup, name, nil)
	if err != nil {
		var respError *azcore.ResponseError
		if errors.As(err, &respError) {
			if respError.StatusCode == 404 {
				return nil
			}
		}

		return err
	}

	_, err = pResp.PollUntilDone(ctx, nil)
	if err != nil {
		return err
	}

	return nil
}
