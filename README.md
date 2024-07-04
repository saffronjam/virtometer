<!-- make center -->
<div align="center">
  <img src="img/cover.png" alt="cover" width="60%"/>
</div>

# 
A framework for benchmarking VM management systems using Azure.

| System | Support |
| --- | --- |
| OpenNebula | ✅ |
| KubeVirt | ✅ |
| OpenStack | ❌ |
| CloudStack | ❌ |


## Usage

```bash
$ go run main.go
```

Output will be saved in `result/` directory. 

## Configuration
The framework can be configured by specifying the configuration in `config.yaml` file following the structure below.

```go
type ConfigType struct {
	Azure struct {
		AuthLocation          string `yaml:"authLocation"`
		SubscriptionID        string `yaml:"subscriptionId"`
		ResourceGroupBaseName string `yaml:"resourceGroupBaseName"`
		// Username is the username for all the VMs created in Azure
		Username string `yaml:"username"`
		// Password is the password for all the VMs created in Azure
		Password string `yaml:"password"`
		// PublicKeys is a list of public keys that will be added to the VMs
		PublicKeys []string `yaml:"publicKeys"`
	} `yaml:"azure"`

	KubeVirt struct {
		Version string `yaml:"version"`
		CDI     struct {
			Version string `yaml:"version"`
		} `yaml:"cdi"`
		Image struct {
			URL string `yaml:"url"`
		} `yaml:"image"`
		Virtctl struct {
			Version string `yaml:"version"`
		} `yaml:"virtctl"`

		Disabled         bool `yaml:"disabled"`
		SkipNodeCreation bool `yaml:"skipNodeCreation"`
		SkipInstallation bool `yaml:"skipInstallation"`
		SkipBenchmark    bool `yaml:"skipBenchmark"`
		SkipDeletion     bool `yaml:"skipDeletion"`
	} `yaml:"kubevirt"`

	OpenNebula struct {
		Image struct {
			Name string `yaml:"name"`
			URL  string `yaml:"url"`
		} `yaml:"image"`
		Template struct {
			Name string `yaml:"name"`
		} `yaml:"template"`

		Disabled         bool `yaml:"disabled"`
		SkipNodeCreation bool `yaml:"skipNodeCreation"`
		SkipInstallation bool `yaml:"skipInstallation"`
		SkipBenchmark    bool `yaml:"skipBenchmark"`
		SkipDeletion     bool `yaml:"skipDeletion"`
	} `yaml:"opennebula"`

	Cluster struct {
		MinNodes int `yaml:"minNodes"`
		MaxNodes int `yaml:"maxNodes"`
	} `yaml:"cluster"`

	OutputDir string `yaml:"outputDir"`
}

```


## Design
The framework is designed to be modular and extensible by providing an interface that a VM managment system must implement. The interface provides a set of basic intructions that tests are based upon, and should be implemented as efficient as the VM management system allows. Refer to the existing implementations for examples.

The interface is defined as follows:

```go
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

	// DeleteAllVMs deletes all VMs in the environment.
	// It should be treated as a cleanup operation, and not be included in any benchmarking
	DeleteAllVMs() error
}
```

The framework is split into four phases to allow for a more modular approach to benchmarking where each phase can be run independently and be disabled when not needed. The framwwork communicates with each VM managment system over SSH and uses the local tool on each control-node to perform any VM-related tasks. 

## Implemenation

### Setup
| Role | CPU | RAM | Disk |
| --- | --- | --- | --- |
| Control Plane (Azure D4v3) | 4 | 16 GB | 100 GB |
| Worker 1 (Azure D4v3) | 4 | 16 GB | 100 GB |
| Worker 2 (Azure D4v3) | 4 | 16 GB | 100 GB |
| Scale-up Workers 3-10 (Azure D4v3) | 4 | 16 GB | 100 GB |

### Payload

The following metrics are collected during the benchmarking process:
| Metric | Unit | Description |
| --- | --- | --- |
| CPU utilization | % | The percentage of CPU used by the system |
| Memory utilization | % | The percentage of memory used by the system |
| Disk utilization | % | The percentage of disk used by the system |
| Task duration | s | The duration of a specific task |


The following VM types are used in the benchmarking process:
| Type | CPU | RAM | Disk Size |
| --- | --- | --- | --- |
| Type 1 | 1 | 128 MB | 1 GB |
| Type 2 | 1 | 1 GB | 5 GB |
| Type 3 | 2 | 2 GB | 10 GB |
| Type 4 | 4 | 4 GB | 20 GB |


The following tests are performed during the benchmarking process:
| Test | Purpose |
| --- | --- |
| Create each type of VM and delete it | Evaluate the basic functionality of the VM management systems without addressing potential load issues. |
| Create 20 type-1 VMs and then delete them | Evaluate how the VM management systems manage a relatively large burst of VMs. |
| Live migrate a type-4 VM | Evaluate how well the VM management systems execute a migration operation without addressing potential load issues. |
| Live migrate 10 type-1 VMs | Evaluate how well the VM management systems manage a relatively large burst of migration operations. |
| Scale up and down 2 to 10 workers without VMs | Evaluate how well the VM management systems scale up and down without any load. |
| Scale up and down 2 to 10 workers with a type-1 VM on each worker | Evaluate how the VM management systems handle scaling operations that incur migration operations. |

