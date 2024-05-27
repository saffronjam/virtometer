package azure

import (
	"context"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v2"
	"strings"
)

func (c *Client) GetVirtualNetwork(ctx context.Context, name string, resourceGroupName string) (*armnetwork.VirtualNetwork, error) {
	resp, err := c.VirtualNetworksClient.Get(ctx, resourceGroupName, name, nil)
	if err != nil {
		return nil, err
	}

	return &resp.VirtualNetwork, nil
}

func (c *Client) CreateVirtualNetwork(ctx context.Context, name string, resourceGroupName string, addressSpace string) (*armnetwork.VirtualNetwork, error) {
	pResp, err := c.VirtualNetworksClient.BeginCreateOrUpdate(ctx, resourceGroupName, name, armnetwork.VirtualNetwork{
		Location: to.Ptr(c.Location),
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{
				AddressPrefixes: []*string{
					to.Ptr(addressSpace),
				},
			},
		},
	}, nil)

	if err != nil {
		if strings.Contains(err.Error(), "is in use by") {
			resp, err := c.VirtualNetworksClient.Get(ctx, resourceGroupName, name, nil)
			if err != nil {
				return nil, err
			}

			return &resp.VirtualNetwork, nil
		}

		return nil, err
	}

	resp, err := pResp.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &resp.VirtualNetwork, nil
}

func (c *Client) DeleteVirtualNetwork(ctx context.Context, name string, resourceGroupName string) error {
	pResp, err := c.VirtualNetworksClient.BeginDelete(ctx, resourceGroupName, name, nil)
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
