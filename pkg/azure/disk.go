package azure

import (
	"context"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func (c *Client) DeleteDisk(ctx context.Context, resourceGroupName, diskName string) error {
	pResp, err := c.DisksClient.BeginDelete(ctx, resourceGroupName, diskName, nil)
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
