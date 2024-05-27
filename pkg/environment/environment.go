package environment

import (
	"context"
	"performance/models"
	"performance/pkg/app"
	"performance/pkg/app/pretty_log"
	"performance/pkg/azure"
	"performance/utils"
	"strconv"
	"sync"
	"time"
)

// Setup initializes a base environment in Azure
func Setup() ([]models.BenchmarkEnvironment, error) {
	client, err := azure.New(&azure.Opts{
		AuthLocation:   app.Config.Azure.AuthLocation,
		SubscriptionID: app.Config.Azure.SubscriptionID,
	})

	if err != nil {
		return nil, err
	}

	setupResult := make([]models.BenchmarkEnvironment, 0)

	wg := sync.WaitGroup{}
	mut := sync.RWMutex{}
	var anyErr error

	if !app.Config.OpenNebula.Disabled {
		wg.Add(1)
		go func() {
			defer wg.Done()

			prettyName := "OpenNebula"

			if app.Config.OpenNebula.SkipNodeCreation {
				pretty_log.TaskResult("[%s] Fetching environment", prettyName)
				opennebulaEnv, err := FetchAzureEnvironment(context.TODO(), client, prettyName, "opennebula")
				if err != nil {
					mut.Lock()
					anyErr = err
					mut.Unlock()
					return
				}

				setupResult = append(setupResult, models.BenchmarkEnvironment{
					Name:             "OpenNebula",
					AzureEnvironment: opennebulaEnv,
					SkipInstallation: app.Config.OpenNebula.SkipInstallation,
				})
			} else {
				id := pretty_log.BeginTask("[%s] Creating environment", prettyName)
				opennebulaEnv, err := SetupAzureEnvironment(context.TODO(), client, prettyName, "opennebula", app.Config.Cluster.MaxNodes)
				if err != nil {
					pretty_log.FailTask(id)
					mut.Lock()
					anyErr = err
					mut.Unlock()
					return
				}
				pretty_log.CompleteTask(id)

				setupResult = append(setupResult, models.BenchmarkEnvironment{
					Name:             prettyName,
					AzureEnvironment: opennebulaEnv,
					SkipInstallation: app.Config.OpenNebula.SkipInstallation,
				})
			}
		}()
	}

	if !app.Config.KubeVirt.Disabled {
		wg.Add(1)
		go func() {
			defer wg.Done()

			prettyName := "KubeVirt"

			if app.Config.KubeVirt.SkipNodeCreation {
				pretty_log.TaskResult("[%s] Fetching environment", prettyName)
				kubevirtEnv, err := FetchAzureEnvironment(context.TODO(), client, prettyName, "kubevirt")
				if err != nil {
					mut.Lock()
					anyErr = err
					mut.Unlock()
					return
				}

				setupResult = append(setupResult, models.BenchmarkEnvironment{
					Name:             prettyName,
					AzureEnvironment: kubevirtEnv,
					SkipInstallation: app.Config.KubeVirt.SkipInstallation,
					SkipBenchmark:    app.Config.KubeVirt.SkipBenchmark,
				})
			} else {
				id := pretty_log.BeginTask("[%s] Creating environment", prettyName)
				kubevirtEnv, err := SetupAzureEnvironment(context.TODO(), client, prettyName, "kubevirt", app.Config.Cluster.MaxNodes)
				if err != nil {
					pretty_log.FailTask(id)
					mut.Lock()
					anyErr = err
					mut.Unlock()
					return
				}
				pretty_log.CompleteTask(id)

				setupResult = append(setupResult, models.BenchmarkEnvironment{
					Name:             prettyName,
					AzureEnvironment: kubevirtEnv,
					SkipInstallation: app.Config.KubeVirt.SkipInstallation,
					SkipBenchmark:    app.Config.KubeVirt.SkipBenchmark,
				})
			}
		}()
	}

	wg.Wait()

	if anyErr != nil {
		return nil, anyErr
	}

	return setupResult, nil
}

// Shutdown cleans up the base environment in Azure and deletes all the resources
func Shutdown() error {
	client, err := azure.New(&azure.Opts{
		AuthLocation:   app.Config.Azure.AuthLocation,
		SubscriptionID: app.Config.Azure.SubscriptionID,
	})

	if err != nil {
		return err
	}

	if !app.Config.OpenNebula.SkipDeletion {
		id := pretty_log.BeginTask("Shutting down OpenNebula environment")
		err = deleteEnvironment(context.TODO(), client, "opennebula")
		if err != nil {
			pretty_log.FailTask(id)
			return err
		}
		pretty_log.CompleteTask(id)
	}

	if !app.Config.KubeVirt.SkipDeletion {
		id := pretty_log.BeginTask("Shutting down KubeVirt environment")
		err = deleteEnvironment(context.TODO(), client, "kubevirt")
		if err != nil {
			pretty_log.FailTask(id)
			return err
		}
		pretty_log.CompleteTask(id)
	}

	return nil
}

func FetchAzureEnvironment(ctx context.Context, client *azure.Client, prettyName, namePrefix string) (*models.AzureEnvironment, error) {
	rg := app.Config.Azure.ResourceGroupBaseName + "-" + namePrefix

	prefixed := func(name string) string {
		return namePrefix + "-" + name
	}

	id := pretty_log.BeginTask("[%s] Fetching resource group", prettyName)
	controlNIC, err := client.GetNIC(ctx, namePrefix+"-nic-1", rg)
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)

	id = pretty_log.BeginTask("[%s] Fetching public IP", prettyName)
	controlPublicIP, err := client.GetPublicIP(ctx, prefixed("ip"), rg)
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)

	id = pretty_log.BeginTask("[%s] Fetching control node VM", prettyName)
	controlVM, err := client.GetVM(ctx, prefixed("control-1"), rg)
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)

	workerNodes := make([]models.WorkerNode, app.Config.Cluster.MaxNodes)
	mut := sync.RWMutex{}
	var anyErr error
	wg := sync.WaitGroup{}

	for i := 0; i < app.Config.Cluster.MaxNodes; i++ {
		idx := i
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			id := pretty_log.BeginTask("[%s] Fetching worker node %d public IP", prettyName, idx+1)
			workerPublicIP, err := client.GetPublicIP(ctx, prefixed("worker-ip-"+strconv.Itoa(idx+1)), rg)
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
			pretty_log.CompleteTask(id)

			id = pretty_log.BeginTask("[%s] Fetching worker node %d NIC", prettyName, idx+1)
			workerNIC, err := client.GetNIC(ctx, "worker-nic-"+strconv.Itoa(idx+1), rg)
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
			pretty_log.CompleteTask(id)

			id = pretty_log.BeginTask("[%s] Fetching worker node %d VM", prettyName, idx+1)
			workerVM, err := client.GetVM(ctx, prefixed("worker-"+strconv.Itoa(idx+1)), rg)
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
			pretty_log.CompleteTask(id)

			mut.Lock()
			workerNodes[idx] = models.WorkerNode{
				VM:         *workerVM,
				InternalIP: *workerNIC.Properties.IPConfigurations[0].Properties.PrivateIPAddress,
				PublicIP:   *workerPublicIP.Properties.IPAddress,
			}
			mut.Unlock()
		}(idx)
	}
	wg.Wait()

	if anyErr != nil {
		return nil, anyErr
	}

	return &models.AzureEnvironment{
		ResourceGroup: rg,
		ControlNode: models.ControlNode{
			VM:         *controlVM,
			InternalIP: *controlNIC.Properties.IPConfigurations[0].Properties.PrivateIPAddress,
			PublicIP:   *controlPublicIP.Properties.IPAddress,
		},
		WorkerNodes: workerNodes,
	}, nil
}

func SetupAzureEnvironment(ctx context.Context, client *azure.Client, prettyName, namePrefix string, workers int) (*models.AzureEnvironment, error) {
	id := pretty_log.BeginTask("[%s] Creating resource group", prettyName)
	resourceGroup, err := client.CreateResourceGroup(ctx, app.Config.Azure.ResourceGroupBaseName+"-"+namePrefix)
	if err != nil {
		return nil, err
	}

	prefixed := func(name string) string {
		return namePrefix + "-" + name
	}

	pretty_log.CompleteTask(id)

	rg := *resourceGroup.Name

	id = pretty_log.BeginTask("[%s] Creating virtual network", prettyName)
	_, err = client.CreateVirtualNetwork(ctx, prefixed("vnet"), rg, "10.0.0.0/8")
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)

	id = pretty_log.BeginTask("[%s] Creating subnet", prettyName)
	subnet, err := client.CreateSubnet(ctx, prefixed("subnet"), rg, prefixed("vnet"), "10.1.0.0/16")
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)

	_, err = client.CreateNetworkSecurityGroup(ctx, prefixed("nsg"), rg)
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}

	id = pretty_log.BeginTask("[%s] Creating public IP", prettyName)
	controlPublicIP, err := client.CreatePublicIP(ctx, prefixed("ip"), rg)
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)
	pretty_log.TaskResult("[%s] Public IP: %s", prettyName, *controlPublicIP.Properties.IPAddress)

	id = pretty_log.BeginTask("[%s] Creating control node NIC", prettyName)
	controlNIC, err := client.CreateNIC(ctx, prefixed("nic-1"), rg, *subnet.ID, controlPublicIP.ID)
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)
	pretty_log.TaskResult("[%s] Internal IP: %s", prettyName, *controlNIC.Properties.IPConfigurations[0].Properties.PrivateIPAddress)

	id = pretty_log.BeginTask("[%s] Creating control node VM", prettyName)
	_, err = client.CreateVM(ctx, prefixed("control-1"), rg, *controlNIC.ID, prefixed("control-1"), app.Config.Azure.Username, app.Config.Azure.Password, app.Config.Azure.PublicKeys)
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)

	// Wait for VM to boot by sleeping
	id = pretty_log.BeginTask("[%s] Waiting for VM to boot (max 30 seconds)", prettyName)
	for i := 0; i < 30; i++ {
		res, _ := utils.SshCommand(*controlPublicIP.Properties.IPAddress, []string{"echo \"\""})
		if res != nil {
			break
		}

		time.Sleep(1 * time.Second)
	}
	pretty_log.CompleteTask(id)

	// Generate SSH key pair for control node non-interactively, 2048 bits, no passphrase, RSA. Don't overwrite
	id = pretty_log.BeginTask("[%s] Generating SSH key pair", prettyName)
	_, _ = utils.SshCommand(*controlPublicIP.Properties.IPAddress, []string{"ssh-keygen -t rsa -b 2048 -N \"\" -f /home/" + app.Config.Azure.Username + "/.ssh/id_rsa <<< n 1> /dev/null 2> /dev/null"})
	// TODO: Fix me, always returns an error
	//if err != nil {
	//	pretty_log.FailTask(id)
	//	return nil, err
	//}
	publicKey, err := utils.SshCommand(*controlPublicIP.Properties.IPAddress, []string{"cat /home/" + app.Config.Azure.Username + "/.ssh/id_rsa.pub"})
	if err != nil {
		pretty_log.FailTask(id)
		return nil, err
	}
	pretty_log.CompleteTask(id)

	workerNodes := make([]models.WorkerNode, workers)
	wg := sync.WaitGroup{}
	mut := sync.Mutex{}
	var anyErr error

	for idx := 0; idx < workers; idx++ {
		i := idx
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			id := pretty_log.BeginTask("[%s] Creating worker node %d public IP", prettyName, idx+1)
			workerPublicIP, err := client.CreatePublicIP(ctx, prefixed("worker-ip-"+strconv.Itoa(idx+1)), rg)
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
			pretty_log.CompleteTask(id)
			pretty_log.TaskResult("[%s] Public IP: %s", prettyName, *workerPublicIP.Properties.IPAddress)

			id = pretty_log.BeginTask("[%s] Creating worker node %d NIC", prettyName, idx+1)
			workerNIC, err := client.CreateNIC(ctx, "worker-nic-"+strconv.Itoa(idx+1), rg, *subnet.ID, workerPublicIP.ID)
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
			pretty_log.CompleteTask(id)
			pretty_log.TaskResult("[%s] Internal IP: %s", prettyName, *workerNIC.Properties.IPConfigurations[0].Properties.PrivateIPAddress)

			id = pretty_log.BeginTask("[%s] Creating worker node %d VM", prettyName, idx+1)
			vm, err := client.CreateVM(ctx, prefixed("worker-"+strconv.Itoa(idx+1)), rg, *workerNIC.ID, prefixed("worker-"+strconv.Itoa(idx+1)), app.Config.Azure.Username, app.Config.Azure.Password,
				append(app.Config.Azure.PublicKeys, publicKey[0]))
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyErr = err
				mut.Unlock()
				return
			}
			pretty_log.CompleteTask(id)

			workerNodes[idx] = models.WorkerNode{
				VM:         *vm,
				InternalIP: *workerNIC.Properties.IPConfigurations[0].Properties.PrivateIPAddress,
				PublicIP:   *workerPublicIP.Properties.IPAddress,
			}
		}(i)
	}
	wg.Wait()

	if anyErr != nil {
		return nil, anyErr
	}

	// Sleep for a bit to allow the VMs to boot
	time.Sleep(30 * time.Second)

	// Collect all IPs
	ips := []string{*controlPublicIP.Properties.IPAddress}
	for _, worker := range workerNodes {
		ips = append(ips, worker.PublicIP)
	}

	// Install base packages on all nodes
	controlCommandGroups := []string{
		"sudo apt-get update",
		"sudo apt-get install jq sysstat -y",
	}

	for idx, ip := range ips {
		idx2 := idx
		ip2 := ip
		wg.Add(1)
		go func(idx int, ip string) {
			defer wg.Done()

			id := pretty_log.BeginTask("[%s] Setting up base packages (node %d/%d)", prettyName, idx+1, len(ips))
			for _, cmd := range controlCommandGroups {
				_, err = utils.SshCommand(ip, []string{cmd})
				if err != nil {
					pretty_log.FailTask(id)
					mut.Lock()
					anyErr = err
					mut.Unlock()
					return
				}
			}
			pretty_log.CompleteTask(id)
		}(idx2, ip2)
	}
	wg.Wait()

	if anyErr != nil {
		return nil, anyErr
	}

	// Setup metrics scraping
	systemdService := `[Unit]
Description=Metrics scraping
[Service]
WorkingDirectory=/home/` + app.Config.Azure.Username + `
ExecStart=/bin/bash /home/` + app.Config.Azure.Username + `/metrics.sh
Restart=always
[Install]
WantedBy=multi-user.target`

	commands := []string{
		"echo \"" + systemdService + "\" | sudo tee /etc/systemd/system/metrics.service > /dev/null",
		"sudo systemctl daemon-reload",
		"sudo systemctl enable metrics.service",
		"sudo systemctl restart metrics.service",
	}

	// Load file from script and cat into each node. Then set up a systemd service to run the script indefinitely
	for idx, ip := range ips {
		idx2 := idx
		ip2 := ip

		wg.Add(1)
		go func(idx int, ip string) {
			defer wg.Done()

			id := pretty_log.BeginTask("[%s] Setting up metrics scraping (node %d/%d)", prettyName, idx+1, len(ips))
			err = utils.SshUpload(ip, "scripts/metrics.sh", "/home/"+app.Config.Azure.Username+"/metrics.sh")
			if err != nil {
				pretty_log.FailTask(id)
				mut.Lock()
				anyErr = err
				mut.Unlock()
			}

			for _, cmd := range commands {
				_, err = utils.SshCommand(ip, []string{cmd})
				if err != nil {
					pretty_log.FailTask(id)
					mut.Lock()
					anyErr = err
					mut.Unlock()
				}
			}

			pretty_log.CompleteTask(id)
		}(idx2, ip2)
	}
	wg.Wait()

	return &models.AzureEnvironment{
		ResourceGroup: rg,
		ControlNode: models.ControlNode{
			InternalIP: *controlNIC.Properties.IPConfigurations[0].Properties.PrivateIPAddress,
			PublicIP:   *controlPublicIP.Properties.IPAddress,
		},
		WorkerNodes: workerNodes,
	}, nil
}

func deleteEnvironment(ctx context.Context, client *azure.Client, namePrefix string) error {
	rg := app.Config.Azure.ResourceGroupBaseName + "-" + namePrefix

	id := pretty_log.BeginTask("Deleting resource group (including all resources)")
	err := client.DeleteResourceGroup(ctx, rg)
	if err != nil {
		return err
	}
	pretty_log.CompleteTask(id)

	return nil
}
