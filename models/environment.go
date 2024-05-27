package models

import (
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
)

type ControlNode struct {
	VM         armcompute.VirtualMachine
	InternalIP string
	PublicIP   string
}

type WorkerNode struct {
	VM         armcompute.VirtualMachine
	InternalIP string
	PublicIP   string
}

type AzureEnvironment struct {
	ResourceGroup string
	ControlNode   ControlNode
	WorkerNodes   []WorkerNode
}

type BenchmarkEnvironment struct {
	Name             string
	AzureEnvironment *AzureEnvironment

	SkipInstallation bool
	SkipBenchmark    bool
}
