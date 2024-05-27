package vm_management_system

import (
	"performance/models"
)

type VmManagementSystem interface {
	// Install initializes the VM management system before any benchmarks are run.
	// This is an optional step and can be omitted if the VM management system is already installed.
	// It assumes the Azure environment is already set up.
	//
	// It does NOT connect the worker nodes to the control plane, since that is part of a benchmark.
	// Use ConnectWorker for that.
	Install() error

	// Setup initializes the VM management system for the benchmark.
	// This could include downloading required images and setting up templates.
	// It is done in a separate step, since it is a one-time operation.
	//
	// This function should be idempotent.
	Setup() error

	// GetVM returns the VM with the given name
	GetVM(name string) *models.VM
	// ListVMs returns a list of all VMs in the environment
	ListVMs() []models.VM
	// CreateVM creates a VM with the given specs.
	// It does not need to wait for the VM to be running
	CreateVM(vm *models.VM, hostIdx ...int) error
	// DeleteVM deletes the VM with the given name.
	// It should be synchronous and wait for the VM to be deleted
	DeleteVM(name string) error
	// MigrateVM migrates a VM to the given host
	MigrateVM(name string, hostIdx int) error

	// ConnectWorker connects a worker node to the control plane.
	// It should be synchronous and wait for the worker node to be connected.
	// It assumes that the worker with the given index (and a control node) is already installed
	ConnectWorker(workerIdx int) error
	// DisconnectWorker disconnects a worker node from the control plane.
	// It should be synchronous and wait for the worker node to be disconnected.
	DisconnectWorker(workerIdx int) error

	// CleanUp cleans up the VM management system after a single benchmark is run.
	// This is meant to remove any artifacts created during the benchmark.
	// It should be idempotent.
	CleanUp() error

	// WaitForRunningVM waits for the VM to be running
	WaitForRunningVM(name string) error
	// WaitForAccessibleVM waits for the VM to be accessible via SSH
	//WaitForAccessibleVM(name string) error

	// DeleteAllVMs deletes all VMs in the environment.
	// It should be treated as a cleanup operation, and not be included in any benchmarking
	DeleteAllVMs() error
}
