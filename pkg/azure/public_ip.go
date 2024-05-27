package azure

import (
	"context"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v2"
)

func (c *Client) GetPublicIP(ctx context.Context, name, resourceGroup string) (*armnetwork.PublicIPAddress, error) {
	resp, err := c.PublicIpAddressesClient.Get(ctx, resourceGroup, name, nil)

	if err != nil {
		return nil, err
	}

	return &resp.PublicIPAddress, nil
}

func (c *Client) CreatePublicIP(ctx context.Context, name, resourceGroup string) (*armnetwork.PublicIPAddress, error) {
	pResp, err := c.PublicIpAddressesClient.BeginCreateOrUpdate(ctx, resourceGroup, name, armnetwork.PublicIPAddress{
		Location: to.Ptr(c.Location),
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodStatic),
		},
	}, nil)

	if err != nil {
		return nil, err
	}

	resp, err := pResp.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &resp.PublicIPAddress, nil
}

func (c *Client) DeletePublicIP(ctx context.Context, name, resourceGroup string) error {
	pResp, err := c.PublicIpAddressesClient.BeginDelete(ctx, resourceGroup, name, nil)
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
