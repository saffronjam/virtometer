package azure

import (
	"context"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

func (c *Client) GetResourceGroup(ctx context.Context, name string) (*armresources.ResourceGroup, error) {
	resp, err := c.ResourceGroupsClient.Get(ctx, name, nil)
	if err != nil {
		return nil, err
	}

	return &resp.ResourceGroup, nil
}

func (c *Client) CreateResourceGroup(ctx context.Context, name string) (*armresources.ResourceGroup, error) {
	resp, err := c.ResourceGroupsClient.CreateOrUpdate(ctx, name, armresources.ResourceGroup{
		Location: to.Ptr(c.Location),
	}, nil)

	if err != nil {
		return nil, err
	}

	return &resp.ResourceGroup, nil
}

func (c *Client) DeleteResourceGroup(ctx context.Context, name string) error {
	pResp, err := c.ResourceGroupsClient.BeginDelete(ctx, name, nil)
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
