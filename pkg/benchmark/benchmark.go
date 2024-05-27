package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"performance/models"
	"performance/pkg/app"
	"performance/pkg/app/pretty_log"
	"performance/pkg/vm_management_system"
	"performance/pkg/vm_management_system/kubevirt"
	"performance/pkg/vm_management_system/opennebula"
	"strings"
	"sync"
	"time"
)

type Benchmark struct {
	Environment models.BenchmarkEnvironment
	VMMS        vm_management_system.VmManagementSystem
}

func NewBenchmark(environment models.BenchmarkEnvironment, vmms vm_management_system.VmManagementSystem) *Benchmark {
	return &Benchmark{
		Environment: environment,
		VMMS:        vmms,
	}
}

func Run(environments []models.BenchmarkEnvironment) (*models.BenchmarkResult, error) {
	pretty_log.TaskGroup("=== Running benchmark ===")

	vmmsMap := make(map[string]vm_management_system.VmManagementSystem)
	for _, environment := range environments {
		vmmsMap[environment.Name] = getVmManagementSystem(&environment)
		if vmmsMap[environment.Name] == nil {
			return nil, fmt.Errorf("unknown VM management system: %s", environment.Name)
		}

		// Ensure each environment has at least 2 worker nodes
		if len(environment.AzureEnvironment.WorkerNodes) < 2 {
			return nil, fmt.Errorf("every environment must have at least 2 worker nodes. %s has %d", environment.Name, len(environment.AzureEnvironment.WorkerNodes))
		}
	}

	var anyError error
	mut := sync.RWMutex{}

	// Install VM management systems if needed
	wg := sync.WaitGroup{}
	for _, environment := range environments {
		if environment.SkipInstallation {
			continue
		}

		e := environment
		wg.Add(1)
		go func(environment models.BenchmarkEnvironment) {
			defer wg.Done()

			vmms := vmmsMap[environment.Name]
			pretty_log.TaskGroup("[%s] Installing VM management system (Not benchmarked)", environment.Name)
			err := vmms.Install()
			if err != nil {
				mut.Lock()
				anyError = fmt.Errorf("failed to install VM management system for %s. details: %s", environment.Name, err.Error())
				mut.Unlock()
				return
			}
		}(e)
	}
	wg.Wait()

	if anyError != nil {
		return nil, anyError
	}

	// Setup VM management systems
	for _, environment := range environments {
		if environment.SkipBenchmark {
			continue
		}

		e := environment
		wg.Add(1)
		go func(environment models.BenchmarkEnvironment) {
			defer wg.Done()
			vmms := vmmsMap[environment.Name]

			id := pretty_log.BeginTask("[%s] Cleaning up before test", environment.Name)
			err := vmms.DeleteAllVMs()
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyError = err
				mut.Unlock()
				return
			}
			pretty_log.CompleteTask(id)

			id = pretty_log.BeginTask("[%s] Setting up VM management system", environment.Name)
			err = vmms.Setup()
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyError = err
				mut.Unlock()
				return
			}
			pretty_log.CompleteTask(id)

			// Ensure hosts are set up to default configuration (1 control node, 2 worker nodes)
			id = pretty_log.BeginTask("[%s] Ensuring default number of worker nodes are connected to VM management system", environment.Name)
			for i := 0; i < app.Config.Cluster.MinNodes; i++ {
				err = vmms.ConnectWorker(i)
				if err != nil {
					pretty_log.FailTask(id)
					mut.Lock()
					anyError = err
					mut.Unlock()
					return
				}
			}

			// Disconnect all other worker nodes
			for i := app.Config.Cluster.MinNodes; i < len(environment.AzureEnvironment.WorkerNodes); i++ {
				err = vmms.DisconnectWorker(i)
				if err != nil {
					pretty_log.FailTask(id)
					mut.Lock()
					anyError = err
					mut.Unlock()
					return
				}
			}

			err = vmms.CleanUp()
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyError = err
				mut.Unlock()
				return
			}

			pretty_log.CompleteTask(id)

			pretty_log.TaskResult("[%s] Setup complete. Waiting for other to start", environment.Name)
		}(e)
	}
	wg.Wait()

	if anyError != nil {
		return nil, anyError
	}

	// Run tests asynchronously
	for _, environment := range environments {
		if environment.SkipBenchmark {
			continue
		}

		e := environment
		wg.Add(1)
		go func(environment models.BenchmarkEnvironment) {
			defer wg.Done()

			vmms := vmmsMap[environment.Name]

			pretty_log.TaskGroup("[%s] Running benchmark", environment.Name)
			timeStart := time.Now()
			RunTests(environment.Name, NewBenchmark(environment, vmms).AllTests(), vmmsMap[environment.Name].CleanUp, SaveResult)
			timeEnd := time.Now()
			pretty_log.TaskGroup("[%s] Benchmark complete (%s)", environment.Name, timeEnd.Sub(timeStart).String())

			pretty_log.TaskGroup("[%s] Cleaning up after test", environment.Name)
			err := vmms.DeleteAllVMs()
			if err != nil {
				mut.Lock()
				anyError = err
				mut.Unlock()
				return
			}

			pretty_log.TaskGroup("[%s] Completed", environment.Name)
		}(e)
	}
	wg.Wait()

	if anyError != nil {
		return nil, anyError
	}

	pretty_log.TaskGroup("=== Benchmark complete ===")

	return nil, nil
}

// SaveResult saves the result of a test to a file.
// It saves the result to results/{vmm}-{group}-{name}-{date}.json
func SaveResult(vmm string, result models.TestResult) error {
	dir := app.Config.OutputDir
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		return err
	}

	bytes, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s_%s_%s_%s.json", strings.ToLower(vmm), strings.ToLower(result.Group), strings.ToLower(result.Name), time.Now().Format("2006-01-02-15-04-05"))), bytes, os.ModePerm)
}

func getVmManagementSystem(environment *models.BenchmarkEnvironment) vm_management_system.VmManagementSystem {
	switch environment.Name {
	case "OpenNebula":
		return opennebula.New(environment.AzureEnvironment)
	case "KubeVirt":
		return kubevirt.New(environment.AzureEnvironment)
	default:
		return nil
	}
}
