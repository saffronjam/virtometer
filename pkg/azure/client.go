package azure

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

type Opts struct {
	AuthLocation   string
	SubscriptionID string
}

type Client struct {
	Location string

	ResourceGroupsClient    *armresources.ResourceGroupsClient
	VirtualNetworksClient   *armnetwork.VirtualNetworksClient
	SubnetsClient           *armnetwork.SubnetsClient
	SecurityGroupsClient    *armnetwork.SecurityGroupsClient
	PublicIpAddressesClient *armnetwork.PublicIPAddressesClient
	InterfacesClient        *armnetwork.InterfacesClient
	VirtualMachinesClient   *armcompute.VirtualMachinesClient
	DisksClient             *armcompute.DisksClient
}

func New(opts *Opts) (*Client, error) {
	client := &Client{
		Location: "swedencentral",
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}

	// Create factories
	clientFactory, err := armresources.NewClientFactory(opts.SubscriptionID, cred, nil)
	if err != nil {
		return nil, err
	}

	networkClientFactory, err := armnetwork.NewClientFactory(opts.SubscriptionID, cred, nil)
	if err != nil {
		return nil, err
	}

	computeClientFactory, err := armcompute.NewClientFactory(opts.SubscriptionID, cred, nil)
	if err != nil {
		return nil, err
	}

	// Assign clients
	client.ResourceGroupsClient = clientFactory.NewResourceGroupsClient()
	client.VirtualNetworksClient = networkClientFactory.NewVirtualNetworksClient()
	client.SubnetsClient = networkClientFactory.NewSubnetsClient()
	client.SecurityGroupsClient = networkClientFactory.NewSecurityGroupsClient()
	client.PublicIpAddressesClient = networkClientFactory.NewPublicIPAddressesClient()
	client.InterfacesClient = networkClientFactory.NewInterfacesClient()
	client.VirtualMachinesClient = computeClientFactory.NewVirtualMachinesClient()
	client.DisksClient = computeClientFactory.NewDisksClient()

	return client, nil
}
