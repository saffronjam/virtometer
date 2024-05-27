package azure

import (
	"context"
	"errors"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
)

func (c *Client) GetVM(ctx context.Context, name, resourceGroup string) (*armcompute.VirtualMachine, error) {
	resp, err := c.VirtualMachinesClient.Get(ctx, resourceGroup, name, nil)

	if err != nil {
		return nil, err
	}

	return &resp.VirtualMachine, nil
}

func (c *Client) CreateVM(ctx context.Context, name, resourceGroup, nicID, diskName, username, password string, authorizedKeys []string) (*armcompute.VirtualMachine, error) {
	var publicKeys []*armcompute.SSHPublicKey
	for _, key := range authorizedKeys {
		publicKeys = append(publicKeys, &armcompute.SSHPublicKey{
			KeyData: to.Ptr(key),
			Path:    to.Ptr("/home/" + username + "/.ssh/authorized_keys"),
		})
	}

	pResp, err := c.VirtualMachinesClient.BeginCreateOrUpdate(ctx, resourceGroup, name, armcompute.VirtualMachine{
		Location: to.Ptr(c.Location),
		Identity: &armcompute.VirtualMachineIdentity{
			Type: to.Ptr(armcompute.ResourceIdentityTypeNone),
		},
		Properties: &armcompute.VirtualMachineProperties{
			StorageProfile: &armcompute.StorageProfile{
				ImageReference: &armcompute.ImageReference{
					// ubuntu 22.04
					Publisher: to.Ptr("Canonical"),
					Offer:     to.Ptr("0001-com-ubuntu-server-jammy"),
					SKU:       to.Ptr("22_04-lts-gen2"),
					Version:   to.Ptr("latest"),
				},
				OSDisk: &armcompute.OSDisk{
					Name:         to.Ptr(diskName),
					CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
					Caching:      to.Ptr(armcompute.CachingTypesReadWrite),
					ManagedDisk: &armcompute.ManagedDiskParameters{
						StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardLRS),
					},
				},
			},
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypesStandardD4SV3),
			},
			OSProfile: &armcompute.OSProfile{ //
				ComputerName:  to.Ptr(name),
				AdminUsername: to.Ptr(username),
				AdminPassword: to.Ptr(password),
				LinuxConfiguration: &armcompute.LinuxConfiguration{
					SSH:                           &armcompute.SSHConfiguration{PublicKeys: publicKeys},
					DisablePasswordAuthentication: to.Ptr(false),
				},
			},
			NetworkProfile: &armcompute.NetworkProfile{
				NetworkInterfaces: []*armcompute.NetworkInterfaceReference{
					{
						ID: to.Ptr(nicID),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		return nil, err
	}

	resp, err := pResp.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &resp.VirtualMachine, nil
}

func (c *Client) DeleteVM(ctx context.Context, name, resourceGroup string) error {
	pResp, err := c.VirtualMachinesClient.BeginDelete(ctx, resourceGroup, name, nil)
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
