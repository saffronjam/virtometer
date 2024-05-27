package benchmark

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"performance/models"
	"performance/pkg/app"
	"performance/pkg/app/pretty_log"
	"performance/utils"
	"strconv"
	"sync"
	"time"
)

const (
	TimerStarted        = "started"
	TimerFinished       = "finished"
	TimerVmRunning      = "vm-running"
	TimerScaleUpStart   = "scaled-up-start"
	TimerScaleUpEnd     = "scaled-up-end"
	TimerScaleDownStart = "scaled-down-start"
	TimerScaleDownEnd   = "scaled-down-end"
	TimerMigrationStart = "migration-start"
	TimerMigrationEnd   = "migration-end"
)

func (b *Benchmark) AllTests() []models.TestDefinition {
	return []models.TestDefinition{
		{
			Name:     "CreateEachType",
			Func:     b.CreateEachType,
			RunCount: 20,
			Disabled: true,
		},
		{
			Name:     "CreateMany",
			Func:     b.CreateMany,
			RunCount: 20,
			Disabled: true,
		},
		{
			Name:     "LiveMigrate",
			Func:     b.LiveMigrate,
			RunCount: 10,
			Disabled: true,
		},
		{
			Name:     "LiveMigrateMany",
			Func:     b.LiveMigrateMany,
			RunCount: 10,
			Disabled: true,
		},
		{
			Name:     "ScaleCluster",
			Func:     b.ScaleCluster,
			RunCount: 10,
			Disabled: false,
		},
		{
			Name:     "ScaleClusterWithVMs",
			Func:     b.ScaleClusterWithVMs,
			RunCount: 10,
			Disabled: true,
		},
	}
}

func RunTests(vmm string, tests []models.TestDefinition, afterTest func() error, saveTest func(vmm string, result models.TestResult) error) {
	results := make(map[string][]models.TestResult)

	for idx, test := range tests {
		if test.Disabled {
			continue
		}

		if test.RunCount == 0 {
			test.RunCount = 1
		}

		for n := 0; n < test.RunCount; n++ {
			pretty_log.TaskGroup("[%s] Running test %d/%d (Run %d/%d): %s", vmm, idx+1, len(tests), n+1, test.RunCount, test.Name)

			res := test.Func()
			for _, r := range res {
				if r.Err != nil {
					pretty_log.TaskResultBad("[%s] Test %s failed: %s", vmm, r.Name, r.Err.Error())
				}
			}

			pretty_log.TaskGroup("[%s] Running After-test callback", vmm)
			err := afterTest()
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to run After-test callback: %s", vmm, err.Error())
				return
			}

			if _, ok := results[test.Name]; !ok {
				results[test.Name] = make([]models.TestResult, 0)
			}

			groupResults := results[test.Name]
			groupResults = append(groupResults, res...)
			results[test.Name] = groupResults

			for _, result := range res {
				if result.Err != nil {
					continue
				}

				err := saveTest(vmm, result)
				if err != nil {
					pretty_log.TaskResultBad("[%s] Failed to save test results: %s", vmm, err.Error())
					return
				}
			}
		}
	}
}

// StartMetricScrapers starts the metric scrapers on all nodes.
// It also deletes the previous output.json file.
func (b *Benchmark) StartMetricScrapers() error {
	mut := sync.RWMutex{}
	wg := sync.WaitGroup{}
	var anyErr error

	// Delete /home/ + config.Config.Azure.Username + /run and /home/ + config.Config.Azure.Username + /output.json
	commands := []string{
		// 1. Stop the scraper
		"rm -f /home/" + app.Config.Azure.Username + "/run",
		// 2. Sleep to ensure the scraper is stopped
		"sleep 1",
		// 3. Delete old data
		"rm -f /home/" + app.Config.Azure.Username + "/output.json",
		// 4. Start the scraper
		"echo \"\" > /home/" + app.Config.Azure.Username + "/run",
	}

	ips := []string{b.Environment.AzureEnvironment.ControlNode.PublicIP}
	for _, worker := range b.Environment.AzureEnvironment.WorkerNodes {
		ips = append(ips, worker.PublicIP)
	}

	// Start the metric scrapers
	for _, ip := range ips {
		i := ip
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()

			for _, command := range commands {
				_, err := utils.SshCommand(ip, []string{command})
				if err != nil {
					mut.Lock()
					anyErr = err
					mut.Unlock()
					return
				}
			}
		}(i)
	}

	wg.Wait()

	return anyErr
}

// StopMetricScrapers stops the metric scrapers on all nodes and downloads the output.json file from each node.
func (b *Benchmark) StopMetricScrapers() ([]models.NodeMetrics, error) {
	var res []models.NodeMetrics
	mut := sync.RWMutex{}
	wg := sync.WaitGroup{}
	var anyErr error

	stopCommands := []string{
		// 1. Stop the scraper
		"rm -f /home/" + app.Config.Azure.Username + "/run",
		// 2. Sleep to ensure the scraper is stopped
		"sleep 1",
	}

	cleanUpCommands := []string{
		// 3. Delete old data
		"rm -f /home/" + app.Config.Azure.Username + "/output.json",
	}

	ips := []string{b.Environment.AzureEnvironment.ControlNode.PublicIP}
	for _, worker := range b.Environment.AzureEnvironment.WorkerNodes {
		ips = append(ips, worker.PublicIP)
	}

	for _, ip := range ips {
		i := ip
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()

			for _, command := range stopCommands {
				_, err := utils.SshCommand(ip, []string{command})
				if err != nil {
					mut.Lock()
					anyErr = err
					mut.Unlock()
					return
				}
			}

			workerMetrics, err := GetMetrics(ip)
			if err != nil {
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}

			mut.Lock()
			res = append(res, workerMetrics...)
			mut.Unlock()

			// Clean up
			for _, command := range cleanUpCommands {
				_, err := utils.SshCommand(ip, []string{command})
				if err != nil {
					mut.Lock()
					anyErr = err
					mut.Unlock()
					return
				}

			}
		}(i)
	}

	wg.Wait()

	if anyErr != nil {
		return nil, anyErr
	}

	return res, nil
}

func GetMetrics(ip string) ([]models.NodeMetrics, error) {
	outputFile := "scripts/output-" + fmt.Sprintf("%d", rand.Intn(10000)) + ".json"

	err := utils.SshDownload(ip, "/home/"+app.Config.Azure.Username+"/output.json", outputFile)
	if err != nil {
		return nil, err
	}

	file, err := os.ReadFile(outputFile)
	if err != nil {
		return nil, err
	}

	var metrics []models.NodeMetrics
	err = json.Unmarshal(file, &metrics)
	if err != nil {
		return nil, err
	}

	err = os.Remove(outputFile)
	if err != nil {
		return nil, err
	}

	return metrics, nil
}

func (b *Benchmark) CreateEachType() []models.TestResult {
	vms := map[string]*models.VM{
		"tiny":   TinyVM(),
		"small":  SmallVM(),
		"medium": MediumVM(),
		"large":  LargeVM(),
	}

	var res []models.TestResult
	for name, vm := range vms {
		testRes := models.TestResult{
			Name:  "create-" + name,
			Group: "create-vm",
		}

		pretty_log.TaskGroup("[%s] Setting up metrics for %s VM", b.Environment.Name, name)
		err := b.StartMetricScrapers()
		if err != nil {
			pretty_log.TaskResultBad("[%s] Failed to set up metrics for %s VM: %s", b.Environment.Name, name, err.Error())
			testRes.Err = err
			res = append(res, testRes)
			continue
		}

		// Collect metrics for 10 seconds before starting
		time.Sleep(10 * time.Second)

		start := time.Now()

		pretty_log.TaskGroup("[%s] Creating %s VM", b.Environment.Name, name)
		err = b.VMMS.CreateVM(vm)
		if err != nil {
			pretty_log.TaskResultBad("[%s] Failed to create %s VM: %s", b.Environment.Name, name, err.Error())
			testRes.Err = err
			res = append(res, testRes)
			continue
		}

		pretty_log.TaskGroup("[%s] Waiting for %s VM to be running", b.Environment.Name, name)
		err = b.VMMS.WaitForRunningVM(vm.Name)
		if err != nil {
			pretty_log.TaskResultBad("[%s] Failed to wait for %s VM to be running: %s", b.Environment.Name, name, err.Error())
			testRes.Err = err
			res = append(res, testRes)
			continue
		}
		running := time.Now()

		pretty_log.TaskGroup("[%s] Deleting %s VM", b.Environment.Name, name)
		err = b.VMMS.DeleteVM(vm.Name)
		if err != nil {
			pretty_log.TaskResultBad("[%s] Failed to delete %s VM: %s", b.Environment.Name, name, err.Error())
			testRes.Err = err
			res = append(res, testRes)
			continue
		}

		end := time.Now()

		// Collect metrics for 10 seconds after stopping
		time.Sleep(10 * time.Second)

		pretty_log.TaskGroup("[%s] Getting metrics for %s VM", b.Environment.Name, name)
		metrics, err := b.StopMetricScrapers()
		if err != nil {
			pretty_log.TaskResultBad("[%s] Failed to get metrics for %s VM: %s", b.Environment.Name, name, err.Error())
			testRes.Err = err
			res = append(res, testRes)
			continue
		}

		testRes.Timers = map[string]time.Time{
			TimerStarted:   start,
			TimerFinished:  end,
			TimerVmRunning: running,
		}
		testRes.Metrics = metrics

		res = append(res, testRes)
	}

	return res
}

func (b *Benchmark) CreateMany() []models.TestResult {
	n := 20

	errResp := func(err error) []models.TestResult {
		return []models.TestResult{
			{
				Name:  "create-many",
				Group: "create-vm",
				Err:   err,
			},
		}
	}

	vms := make([]*models.VM, n)
	for i := 0; i < len(vms); i++ {
		vms[i] = TinyVM()
	}

	pretty_log.TaskGroup("[%s] Setting up metrics for %d tiny VMs", b.Environment.Name, n)
	err := b.StartMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	// Collect metrics for 10 seconds before starting
	time.Sleep(10 * time.Second)

	pretty_log.TaskGroup("[%s] Creating %d tiny VMs", b.Environment.Name, n)
	wg := sync.WaitGroup{}
	mut := sync.RWMutex{}
	var anyErr error

	start := time.Now()
	for _, vm := range vms {
		wg.Add(1)
		go func(vm *models.VM) {
			defer wg.Done()
			err := b.VMMS.CreateVM(vm)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to create VM %s: %s", b.Environment.Name, vm.Name, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(vm)
	}
	wg.Wait()

	if anyErr != nil {
		return errResp(anyErr)
	}

	pretty_log.TaskGroup("[%s] Waiting for %d tiny VMs to be running", b.Environment.Name, n)
	for _, vm := range vms {
		wg.Add(1)
		go func(vm *models.VM) {
			defer wg.Done()
			err := b.VMMS.WaitForRunningVM(vm.Name)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to wait for VM %s to be running: %s", b.Environment.Name, vm.Name, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(vm)
	}
	wg.Wait()
	running := time.Now()

	if anyErr != nil {
		return errResp(anyErr)
	}

	pretty_log.TaskGroup("[%s] Deleting %d tiny VMs", b.Environment.Name, n)
	for _, vm := range vms {
		wg.Add(1)

		go func(vm *models.VM) {
			defer wg.Done()
			err := b.VMMS.DeleteVM(vm.Name)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to delete VM %s: %s", b.Environment.Name, vm.Name, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(vm)
	}
	wg.Wait()
	end := time.Now()

	if anyErr != nil {
		return errResp(anyErr)
	}

	// Collect metrics for 10 seconds after stopping
	time.Sleep(10 * time.Second)

	pretty_log.TaskGroup("[%s] Getting metrics for %d tiny VMs", b.Environment.Name, n)
	metrics, err := b.StopMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	return []models.TestResult{
		{
			Name:     "create-many",
			Group:    "create-vm",
			Metadata: map[string]string{"n": strconv.Itoa(n)},
			Timers:   map[string]time.Time{TimerStarted: start, TimerFinished: end, TimerVmRunning: running},
			Metrics:  metrics,
		},
	}
}

func (b *Benchmark) LiveMigrate() []models.TestResult {
	vm := LargeVM()

	errResp := func(err error) []models.TestResult {
		return []models.TestResult{
			{
				Name:  "live-migrate",
				Group: "live-migrate",
				Err:   err,
			},
		}
	}

	pretty_log.TaskGroup("[%s] Setting up metrics for live migration", b.Environment.Name)
	err := b.StartMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	// Collect metrics for 10 seconds before starting
	time.Sleep(10 * time.Second)

	start := time.Now()
	pretty_log.TaskGroup("[%s] Creating a VM", b.Environment.Name)
	err = b.VMMS.CreateVM(vm, 0)
	if err != nil {
		return errResp(err)
	}

	pretty_log.TaskGroup("[%s] Waiting for VM to be running", b.Environment.Name)
	err = b.VMMS.WaitForRunningVM(vm.Name)
	if err != nil {
		return errResp(err)
	}

	migrationStart := time.Now()

	pretty_log.TaskGroup("[%s] Live migrating VM", b.Environment.Name)
	err = b.VMMS.MigrateVM(vm.Name, 1)
	if err != nil {
		return errResp(err)
	}

	migrationEnd := time.Now()

	pretty_log.TaskGroup("[%s] Deleting VM", b.Environment.Name)
	err = b.VMMS.DeleteVM(vm.Name)
	if err != nil {
		return errResp(err)
	}

	end := time.Now()

	// Collect metrics for 10 seconds after stopping
	time.Sleep(10 * time.Second)

	pretty_log.TaskGroup("[%s] Getting metrics for live migration", b.Environment.Name)
	metrics, err := b.StopMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	return []models.TestResult{
		{
			Name:    "live-migrate",
			Group:   "live-migrate",
			Metrics: metrics,
			Timers: map[string]time.Time{
				TimerStarted:        start,
				TimerFinished:       end,
				TimerMigrationStart: migrationStart,
				TimerMigrationEnd:   migrationEnd,
			},
		},
	}
}

func (b *Benchmark) LiveMigrateMany() []models.TestResult {
	n := 10

	errResp := func(err error) []models.TestResult {
		return []models.TestResult{
			{
				Name:  "live-migrate-many",
				Group: "live-migrate",
				Err:   err,
			},
		}
	}

	pretty_log.TaskGroup("[%s] Setting up metrics for live migration of %d VMs", b.Environment.Name, n)
	err := b.StartMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	// Collect metrics for 10 seconds before starting
	time.Sleep(10 * time.Second)

	pretty_log.TaskGroup("[%s] Creating %d tiny VMs", b.Environment.Name, n)
	vms := make([]*models.VM, n)
	for i := 0; i < len(vms); i++ {
		vms[i] = TinyVM()
	}

	wg := sync.WaitGroup{}
	mut := sync.RWMutex{}
	var anyErr error

	start := time.Now()

	for _, vm := range vms {
		wg.Add(1)
		go func(vm *models.VM) {
			defer wg.Done()
			err := b.VMMS.CreateVM(vm, 0)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to create VM %s: %s", b.Environment.Name, vm.Name, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(vm)
	}
	wg.Wait()

	if anyErr != nil {
		return errResp(anyErr)
	}

	for _, vm := range vms {
		wg.Add(1)
		go func(vm *models.VM) {
			defer wg.Done()
			err := b.VMMS.WaitForRunningVM(vm.Name)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to wait for VM %s to be running: %s", b.Environment.Name, vm.Name, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(vm)
	}
	wg.Wait()

	if anyErr != nil {
		return errResp(anyErr)
	}

	migrationStart := time.Now()

	pretty_log.TaskGroup("[%s] Live migrating %d VMs", b.Environment.Name, n)
	for _, vm := range vms {
		wg.Add(1)
		go func(vm *models.VM) {
			defer wg.Done()
			err := b.VMMS.MigrateVM(vm.Name, 1)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to migrate VM %s: %s", b.Environment.Name, vm.Name, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(vm)
	}
	wg.Wait()

	if anyErr != nil {
		return errResp(anyErr)
	}

	migrationEnd := time.Now()

	pretty_log.TaskGroup("[%s] Deleting %d VMs", b.Environment.Name, n)
	for _, vm := range vms {
		wg.Add(1)
		go func(vm *models.VM) {
			defer wg.Done()
			err := b.VMMS.DeleteVM(vm.Name)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to delete VM %s: %s", b.Environment.Name, vm.Name, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(vm)
	}
	wg.Wait()

	if anyErr != nil {
		return errResp(anyErr)
	}

	end := time.Now()

	// Collect metrics for 10 seconds after stopping
	time.Sleep(10 * time.Second)

	pretty_log.TaskGroup("[%s] Getting metrics for live migration of %d VMs", b.Environment.Name, n)
	metrics, err := b.StopMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	return []models.TestResult{
		{
			Name:    "live-migrate-many",
			Group:   "live-migrate",
			Metrics: metrics,
			Timers: map[string]time.Time{
				TimerStarted:        start,
				TimerFinished:       end,
				TimerMigrationStart: migrationStart,
				TimerMigrationEnd:   migrationEnd,
			},
		},
	}
}

func (b *Benchmark) ScaleCluster() []models.TestResult {
	scaleDownTo := 2
	scaleUpTo := len(b.Environment.AzureEnvironment.WorkerNodes)

	errResp := func(err error) []models.TestResult {
		return []models.TestResult{
			{
				Name:  "scale-cluster",
				Group: "scale-cluster",
				Err:   err,
			},
		}
	}

	pretty_log.TaskGroup("[%s] Setting up metrics for scaling cluster", b.Environment.Name)
	err := b.StartMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	// Collect for 10 seconds before starting
	time.Sleep(10 * time.Second)

	wg := sync.WaitGroup{}
	mut := sync.RWMutex{}
	var anyErr error

	start := time.Now()
	scaleUpStart := time.Now()

	pretty_log.TaskGroup("[%s] Scaling cluster up to %d nodes", b.Environment.Name, scaleUpTo)
	for i := scaleDownTo; i < scaleUpTo; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err = b.VMMS.ConnectWorker(i)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to connect worker %d: %s", b.Environment.Name, i, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(i)
	}
	wg.Wait()

	if anyErr != nil {
		return errResp(err)
	}

	scaleUpEnd := time.Now()
	scaleDownStart := time.Now()

	pretty_log.TaskGroup("[%s] Scaling cluster down to %d nodes", b.Environment.Name, scaleDownTo)
	for i := scaleUpTo - 1; i >= scaleDownTo; i-- {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err = b.VMMS.DisconnectWorker(i)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to disconnect worker %d: %s", b.Environment.Name, i, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(i)
	}
	wg.Wait()

	scaleDownEnd := time.Now()

	if anyErr != nil {
		return errResp(err)
	}

	end := time.Now()

	// Collect metrics for 10 seconds before stopping
	time.Sleep(10 * time.Second)

	pretty_log.TaskGroup("[%s] Getting metrics for scaling cluster", b.Environment.Name)
	metrics, err := b.StopMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	return []models.TestResult{
		{
			Name:    "scale-cluster",
			Group:   "scale-cluster",
			Metrics: metrics,
			Timers: map[string]time.Time{
				TimerStarted:        start,
				TimerFinished:       end,
				TimerScaleUpStart:   scaleUpStart,
				TimerScaleUpEnd:     scaleUpEnd,
				TimerScaleDownStart: scaleDownStart,
				TimerScaleDownEnd:   scaleDownEnd,
			},
		},
	}
}

func (b *Benchmark) ScaleClusterWithVMs() []models.TestResult {
	scaleDownTo := 2
	scaleUpTo := len(b.Environment.AzureEnvironment.WorkerNodes)

	errResp := func(err error) []models.TestResult {
		return []models.TestResult{
			{
				Name:  "scale-cluster-with-vms",
				Group: "scale-cluster",
				Err:   err,
			},
		}
	}

	pretty_log.TaskGroup("[%s] Setting up metrics for scaling cluster with VMs", b.Environment.Name)
	err := b.StartMetricScrapers()
	if err != nil {
		return errResp(err)
	}

	// Collect metrics for 10 seconds before starting
	time.Sleep(10 * time.Second)

	wg := sync.WaitGroup{}
	mut := sync.RWMutex{}
	var anyErr error

	start := time.Now()
	scaleUpStart := time.Now()

	pretty_log.TaskGroup("[%s] Scaling cluster up to %d nodes", b.Environment.Name, scaleUpTo)
	for i := scaleDownTo; i < scaleUpTo; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err = b.VMMS.ConnectWorker(i)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to connect worker %d: %s", b.Environment.Name, i, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(i)
	}
	wg.Wait()

	if anyErr != nil {
		return errResp(anyErr)
	}

	scaleUpEnd := time.Now()

	// Create a tiny VM on each worker node
	pretty_log.TaskGroup("[%s] Creating a tiny VM on each worker node", b.Environment.Name)
	for i := scaleDownTo; i < scaleUpTo; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			vm := TinyVM()
			err = b.VMMS.CreateVM(vm, i)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to create VM %s: %s", b.Environment.Name, vm.Name, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(i)
	}
	wg.Wait()

	if anyErr != nil {
		return errResp(anyErr)
	}

	scaleDownStart := time.Now()

	pretty_log.TaskGroup("[%s] Scaling cluster down to %d nodes", b.Environment.Name, scaleDownTo)
	for i := scaleUpTo - 1; i >= scaleDownTo; i-- {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err = b.VMMS.DisconnectWorker(i)
			if err != nil {
				pretty_log.TaskResultBad("[%s] Failed to disconnect worker %d: %s", b.Environment.Name, i, err.Error())
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
		}(i)
	}
	wg.Wait()

	scaleDownEnd := time.Now()

	if anyErr != nil {
		return errResp(anyErr)
	}

	pretty_log.TaskGroup("[%s] Deleting VMs", b.Environment.Name)
	err = b.VMMS.DeleteAllVMs()
	if err != nil {
		return errResp(err)
	}

	end := time.Now()

	// Collect metrics for 10 seconds before stopping
	time.Sleep(10 * time.Second)

	pretty_log.TaskGroup("[%s] Getting metrics for scaling cluster", b.Environment.Name)
	metrics, err := b.StopMetricScrapers()
	if err != nil {
		return errResp(anyErr)
	}

	return []models.TestResult{
		{
			Name:    "scale-cluster-with-vms",
			Group:   "scale-cluster",
			Metrics: metrics,
			Timers: map[string]time.Time{
				TimerStarted:        start,
				TimerFinished:       end,
				TimerScaleUpStart:   scaleUpStart,
				TimerScaleUpEnd:     scaleUpEnd,
				TimerScaleDownStart: scaleDownStart,
				TimerScaleDownEnd:   scaleDownEnd,
			},
		},
	}
}
