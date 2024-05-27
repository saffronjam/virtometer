package opennebula

import (
	"encoding/json"
	"fmt"
	"performance/models"
	"performance/pkg/app"
	"performance/pkg/app/pretty_log"
	"performance/pkg/vm_management_system"
	"performance/utils"
	"strconv"
	"strings"
	"sync"
	"time"
)

type OpenNebula struct {
	Environment *models.AzureEnvironment

	vm_management_system.VmManagementSystem

	DefaultTemplateID int
}

func New(environment *models.AzureEnvironment) *OpenNebula {
	return &OpenNebula{
		Environment: environment,
	}
}

func (o *OpenNebula) Install() error {
	pretty_log.TaskGroup("[OpenNebula] Installing control node")

	// Commands to set up OpenNebula
	controlCommandGroups := [][]string{
		{"sudo apt-get update"},
		{"sudo apt-get -y install gnupg wget apt-transport-https"},
		{"curl -fsSL https://downloads.opennebula.io/repo/repo2.key | sudo gpg --batch --yes --dearmor -o /etc/apt/trusted.gpg.d/opennebula.gpg"},
		{"sudo echo \"deb https://downloads.opennebula.org/repo/6.8/Ubuntu/22.04 stable opennebula\" | sudo tee /etc/apt/sources.list.d/opennebula.list"},
		{"sudo apt-get update"},
		{"sudo apt-get install -y opennebula opennebula-sunstone opennebula-gate opennebula-flow"},
		{"sudo ufw disable"},
		{"sudo systemctl start opennebula opennebula-sunstone"},
		{"sudo systemctl enable opennebula opennebula-sunstone"},
	}

	for idx, cmdGroup := range controlCommandGroups {
		id := pretty_log.BeginTask("[OpenNebula] - Command (%d/%d): %s", idx+1, len(controlCommandGroups), strings.Join(cmdGroup, " && "))

		_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, cmdGroup)
		if err != nil {
			pretty_log.FailTask(id)
			return err
		}
		pretty_log.CompleteTask(id)
	}

	pretty_log.TaskGroup("[OpenNebula] Installing worker nodes")

	// Commands to set up OpenNebula on worker nodes
	workerCommandGroups := [][]string{
		{"curl -fsSL https://downloads.opennebula.io/repo/repo2.key | sudo gpg --batch --yes --dearmor -o /etc/apt/trusted.gpg.d/opennebula.gpg"},
		{"sudo echo \"deb https://downloads.opennebula.org/repo/6.8/Ubuntu/22.04 stable opennebula\" | sudo tee /etc/apt/sources.list.d/opennebula.list"},
		{"sudo apt-get update"},
		{"sudo apt-get install -y opennebula-node"},
		{"sudo systemctl restart libvirtd.service"},
	}

	if app.Config.Cluster.MaxNodes > 0 {
		var anyErr error
		mut := sync.RWMutex{}
		wg := sync.WaitGroup{}

		for idx, worker := range o.Environment.WorkerNodes {
			workerIdx := idx
			ip := worker.PublicIP

			wg.Add(1)
			go func(workerIdx int, ip string) {
				defer wg.Done()
				for jdx, cmdGroup := range workerCommandGroups {
					id := pretty_log.BeginTask("[OpenNebula] - Command (%d/%d) [Worker: %d]: %s", jdx+1, len(workerCommandGroups), workerIdx, strings.Join(cmdGroup, " && "))
					_, err := utils.SshCommand(ip, cmdGroup)
					if err != nil {
						pretty_log.FailTask(id)
						mut.Lock()
						anyErr = err
						mut.Unlock()
						return
					}
					pretty_log.CompleteTask(id)
				}
			}(workerIdx, ip)
		}
		wg.Wait()

		if anyErr != nil {
			return anyErr
		}
	} else {
		pretty_log.TaskResult("[OpenNebula] No worker nodes to setup")
	}

	return nil
}

func (o *OpenNebula) Setup() error {
	// Download image
	id := pretty_log.BeginTask("[OpenNebula] Setting up image if not present")
	installIfNotPresent := "if sudo oneimage list --list NAME | grep -w " + app.Config.OpenNebula.Image.Name + " | wc -l | grep -q ^0$; then\n  sudo oneimage create -d 1 <<EOF\nNAME=\"" + app.Config.OpenNebula.Image.Name + "\"\nPATH=\"" + app.Config.OpenNebula.Image.URL + "\"\nEOF\nelse\n  echo 'Image already exists.'\nfi\nexit 0"
	res, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{installIfNotPresent})
	if err != nil {
		pretty_log.FailTask(id)
		return err
	}
	pretty_log.CompleteTask(id)
	pretty_log.TaskResultList(res)

	// Create template
	id = pretty_log.BeginTask("[OpenNebula] Setting up template if not present")

	installIfNotPresent = "if sudo onetemplate list --list NAME | grep -w " + app.Config.OpenNebula.Template.Name + " | wc -l | grep -q ^0$; then\n  sudo onetemplate create <<EOF\nNAME=\"" + app.Config.OpenNebula.Template.Name + "\"\nDISK=[\n  IMAGE=\"" + app.Config.OpenNebula.Image.Name + "\"\n]\nGRAPHICS=[\n  TYPE=\"VNC\",\n  LISTEN=\"0.0.0.0\"\n]\nEOF\nelse\n  echo 'Template already exists.'\nfi\nexit 0"
	res, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{installIfNotPresent})
	if err != nil {
		pretty_log.FailTask(id)
		return err
	}
	pretty_log.CompleteTask(id)
	pretty_log.TaskResultList(res)

	// Get template ID
	id = pretty_log.BeginTask("[OpenNebula] Getting template ID")
	// Parse as int
	getTemplateID := "sudo onetemplate list --json | jq '.VMTEMPLATE_POOL.VMTEMPLATE | select(.NAME==\"" + app.Config.OpenNebula.Template.Name + "\") | .ID' | tr -d '\"'"
	res, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{getTemplateID})
	if err != nil {
		pretty_log.FailTask(id)
		return err
	}

	templateID, err := strconv.ParseInt(strings.TrimSuffix(res[0], "\n"), 10, 64)
	if err != nil {
		pretty_log.FailTask(id)
		return err
	}
	o.DefaultTemplateID = int(templateID)
	pretty_log.CompleteTask(id)

	pretty_log.TaskResult(" - Template ID: %d", o.DefaultTemplateID)

	return nil
}

func (o *OpenNebula) GetVM(name string) *models.VM {
	getCmd := "sudo onevm list --list NAME --json | jq '.VM_POOL.VM[] | select(.NAME==\"" + name + "\") | {name: .NAME, specs: {cpu: (.TEMPLATE.CPU | tonumber), ram: (.TEMPLATE.MEMORY | tonumber), diskSize: (.TEMPLATE.DISK[0].SIZE | tonumber)}}"
	outputList, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{getCmd})
	if err != nil {
		return nil
	}

	// Parse output as json into a VM
	var vm models.VM
	err = json.Unmarshal([]byte(outputList[0]), &vm)
	if err != nil {
		return nil
	}

	// Disk diskSize is in MB, convert to GB
	vm.Specs.DiskSize = vm.Specs.DiskSize / 1024

	return &vm
}

func (o *OpenNebula) ListVMs() []models.VM {
	listCmd := "sudo onevm list --list NAME --json | jq '[.VM_POOL.VM[] | {name: .NAME, specs: {cpu: (.TEMPLATE.CPU | tonumber), ram: (.TEMPLATE.MEMORY | tonumber), diskSize: (.TEMPLATE.DISK[0]?.SIZE | tonumber // 0)}}]'"
	outputList, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{listCmd})
	if err != nil {
		return nil
	}

	// Parse output as json into []VM
	var vms []models.VM
	err = json.Unmarshal([]byte(outputList[0]), &vms)
	if err != nil {
		return nil
	}

	// Disk diskSize is in MB, convert to GB
	for i := range vms {
		vms[i].Specs.DiskSize = vms[i].Specs.DiskSize / 1024
	}

	return vms
}

func (o *OpenNebula) CreateVM(vm *models.VM, hostIdx ...int) error {
	var host *int
	if len(hostIdx) > 0 {
		host = &hostIdx[0]
	}

	var hold string
	if host != nil {
		hold = "--hold"
	} else {
		hold = ""
	}

	createCmd := fmt.Sprintf("sudo onetemplate instantiate %d --name %s %s <<EOF\nCPU=\"0.1\"\nVCPU=\"%d\"\nMEMORY=\"%d\"\nDISK=[SIZE=\"%d\",\nIMAGE=\"cirros\"]\nEOF", o.DefaultTemplateID, vm.Name, hold, vm.Specs.CPU, vm.Specs.RAM, vm.Specs.DiskSize*1024)

	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{createCmd})
	if err != nil {
		return err
	}

	if host != nil {
		// Move VM to host
		moveCmd := fmt.Sprintf("sudo onevm deploy %s %s", vm.Name, o.Environment.WorkerNodes[*host].InternalIP)
		_, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{moveCmd})
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *OpenNebula) DeleteVM(name string) error {
	deleteCmd := "sudo onevm terminate --hard " + name
	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{deleteCmd})
	if err != nil {
		return err
	}

	return o.WaitForDeletedVM(name)
}

func (o *OpenNebula) MigrateVM(name string, hostIdx int) error {
	migrateCmd := "sudo onevm migrate --live " + name + " " + o.Environment.WorkerNodes[hostIdx].InternalIP
	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{migrateCmd})
	if err != nil {

		return err
	}
	return o.WaitForRunningVmOnHost(name, o.Environment.WorkerNodes[hostIdx].InternalIP)
}

func (o *OpenNebula) ConnectWorker(workerIdx int) error {
	exists, err := o.hostExists(o.Environment.WorkerNodes[workerIdx].InternalIP)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	commands := []string{
		"ssh-keyscan " + o.Environment.WorkerNodes[workerIdx].InternalIP + " | sudo tee -a /var/lib/one/.ssh/known_hosts",
		"sudo cat /var/lib/one/.ssh/id_rsa.pub | ssh " + o.Environment.WorkerNodes[workerIdx].InternalIP + " -o StrictHostKeyChecking=no \"sudo tee -a /var/lib/one/.ssh/authorized_keys > /dev/null\"",
		"sudo onehost create " + o.Environment.WorkerNodes[workerIdx].InternalIP + " -i kvm -v kvm 2>&1 | tee /dev/stderr | grep -q -e 'NAME is already taken' -e 'ID:' && exit 0 || exit 1",
	}

	for _, cmd := range commands {
		_, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{cmd})
		if err != nil {
			return err
		}
	}

	return o.WaitForRunningHost(o.Environment.WorkerNodes[workerIdx].InternalIP)
}

func (o *OpenNebula) DisconnectWorker(workerIdx int) error {
	exists, err := o.hostExists(o.Environment.WorkerNodes[workerIdx].InternalIP)
	if err != nil {
		return err
	}

	if !exists {
		return nil
	}

	// Flush host
	_, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{"sudo onehost flush " + o.Environment.WorkerNodes[workerIdx].InternalIP})
	if err != nil {
		return err
	}

	// Wait for VMs to be migrated off the host
	err = o.WaitForEmptyHost(o.Environment.WorkerNodes[workerIdx].InternalIP)
	if err != nil {
		return err
	}

	// Delete host
	_, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{"sudo onehost delete " + o.Environment.WorkerNodes[workerIdx].InternalIP + " | { grep -q \"not found\" || [ -z \"$(cat)\" ]; } && exit 0 || exit 1"})
	if err != nil {
		return err
	}

	return nil
}

func (o *OpenNebula) CleanUp() error {
	// Nothing to do
	return nil
}

func (o *OpenNebula) WaitForRunningVM(name string) error {
	runningCmd := "sudo onevm list --list NAME --json | jq '.VM_POOL.VM[] | select(.NAME == \"" + name + "\") | .LCM_STATE' | tr -d '\"'"

	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{runningCmd})
		if len(res) == 0 {
			continue
		}

		runningState := "3"
		if strings.Contains(res[0], runningState) {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for VM %s to be running", name)
		}
	}
}

func (o *OpenNebula) WaitForRunningVmOnHost(name, host string) error {
	correctHostCmd := "sudo onevm list --list NAME --json | jq '.VM_POOL.VM[] | select(.NAME == \"" + name + "\") | .HISTORY_RECORDS.HISTORY | select(.HOSTNAME == \"" + host + "\") | .HOSTNAME' | tr -d '\"'"

	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		err := o.WaitForRunningVM(name)
		if err != nil {
			return err
		}

		attemptsLeft--
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{correctHostCmd})
		if len(res) == 0 {
			continue
		}

		if strings.Contains(res[0], host) {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for VM %s to be running", name)
		}
	}
}

func (o *OpenNebula) WaitForDeletedVM(name string) error {
	deletedCmd := "sudo onevm list --list NAME --json | jq '.VM_POOL.VM[] | select(.NAME == \"" + name + "\")'"

	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{deletedCmd})
		if len(res) == 0 {
			continue
		}

		if len(strings.TrimSuffix(res[0], "\n")) == 0 || strings.Contains(res[0], "Cannot iterate over null") {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for VM %s to be deleted", name)
		}
	}
}

func (o *OpenNebula) WaitForRunningHost(host string) error {
	runningCmd1 := "sudo onehost list --json | jq '.HOST_POOL.HOST[] | select(.NAME == \"" + host + "\") | .STATE' | tr -d '\"'"
	runningCmd2 := "sudo onehost list --json | jq '.HOST_POOL.HOST | select(.NAME == \"" + host + "\") | .STATE' | tr -d '\"'"

	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{runningCmd1})
		if err != nil || (len(res) > 0 && strings.Contains(res[0], "Cannot index string with string")) {
			res, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{runningCmd2})
			if err != nil {
				continue
			}
		}

		if len(res) == 0 {
			continue
		}

		runningState := "2"
		if strings.Contains(res[0], runningState) {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for host %s to be running", host)
		}
	}
}

func (o *OpenNebula) WaitForEmptyHost(host string) error {
	emptyCmd := "sudo onehost show " + host + " --json | jq '.HOST.VMS'"
	attemptsLeft := 1000

	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{emptyCmd})
		if err == nil && strings.Contains(res[0], "{}") {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for host %s to be empty", host)
		}
	}
}

func (o *OpenNebula) DeleteAllVMs() error {
	listAndDeleteCmd := "sudo onevm list --list NAME --json | jq '.VM_POOL.VM[].ID' | xargs -I {} sudo onevm terminate --hard {}"
	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{listAndDeleteCmd})
	return err
}

func (o *OpenNebula) hostExists(name string) (bool, error) {
	command1 := "sudo onehost list --json | jq '.HOST_POOL.HOST | select(.NAME==\"" + name + "\") | .ID'"
	command2 := "sudo onehost list --json | jq '.HOST_POOL.HOST[] | select(.NAME==\"" + name + "\") | .ID'"

	out, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{command1})
	if err != nil {
		out, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{command2})
		if err != nil {
			return false, err
		}
	}

	return len(out) > 0, nil
}
