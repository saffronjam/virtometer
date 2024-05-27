package kubevirt

import (
	"fmt"
	"performance/models"
	"performance/pkg/app"
	"performance/pkg/app/pretty_log"
	"performance/pkg/vm_management_system"
	"performance/utils"
	"strings"
	"sync"
	"time"
)

func New(environment *models.AzureEnvironment) *KubeVirt {
	return &KubeVirt{
		Environment: environment,
	}
}

type KubeVirt struct {
	Environment *models.AzureEnvironment

	vm_management_system.VmManagementSystem

	token         string
	nodeSelectors map[string]string
}

var mut sync.RWMutex

func (o *KubeVirt) Install() error {
	pretty_log.TaskGroup("[KubeVirt] Install control node")

	// Commands to set up KubeVirt with K3s on control node
	controlCommandGroups := [][]string{
		{"sudo apt-get update"},
		{"sudo apt-get install nfs-common -y"},
		{"curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=v1.28.7+k3s1 INSTALL_K3S_EXEC=\"server --tls-san " + o.Environment.ControlNode.InternalIP + " --advertise-address " + o.Environment.ControlNode.InternalIP + " --write-kubeconfig-mode=644 --disable=traefik --disable=servicelb\" sh -"},
		{"sleep 3"},
		{"mkdir -p /home/" + app.Config.Azure.Username + "/.kube && sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config && sudo chown " + app.Config.Azure.Username + " /home/" + app.Config.Azure.Username + "/.kube/config && sudo chmod 600 /home/" + app.Config.Azure.Username + "/.kube/config"},
	}

	for idx, cmdGroup := range controlCommandGroups {
		id := pretty_log.BeginTask("[KubeVirt] - Command (%d/%d): %s", idx+1, len(controlCommandGroups), strings.Join(cmdGroup, " && "))
		_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, cmdGroup)
		if err != nil {
			pretty_log.FailTask(id)
			return err
		}

		pretty_log.CompleteTask(id)
	}

	id := pretty_log.BeginTask("[KubeVirt] Fetching token from control node")
	outputList, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{"sudo cat /var/lib/rancher/k3s/server/node-token"})
	if err != nil {
		pretty_log.FailTask(id)
		return err
	}
	o.token = strings.TrimSuffix(outputList[0], "\n")
	pretty_log.CompleteTask(id)

	pretty_log.TaskGroup("[KubeVirt] Install worker nodes")

	// Commands to set up KubeVirt with K3s on worker nodes
	workerCommandGroups := [][]string{
		{"sudo apt-get update"},
		{"sudo apt-get install nfs-common -y"},
		{"curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=v1.28.7+k3s1 K3S_URL=https://" + o.Environment.ControlNode.InternalIP + ":6443 K3S_TOKEN=" + o.token + " sh -"},
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
					id := pretty_log.BeginTask("[KubeVirt] - Command (%d/%d) for [Worker %d]: %s", jdx+1, len(workerCommandGroups), workerIdx+1, strings.Join(cmdGroup, " && "))
					_, err = utils.SshCommand(ip, cmdGroup)
					if err != nil {
						pretty_log.FailTask(id)
						mut.Lock()
						anyErr = err
						mut.Unlock()
						return
					}
					pretty_log.CompleteTask(id)
				}

				// Label the node accordingly. We don't need to label the other workers, since they will be created and removed dynamically
				if workerIdx < app.Config.Cluster.MinNodes {
					id := pretty_log.BeginTask("[KubeVirt] Labeling [Worker %d] with type=%s", workerIdx+1, "base")
					err = o.labelHost(*o.Environment.WorkerNodes[workerIdx].VM.Name, "type", "base")
					if err != nil {
						pretty_log.FailTask(id)
						mut.Lock()
						anyErr = err
						mut.Unlock()
						return
					}
					pretty_log.CompleteTask(id)
				}

				// Disconnect non-base workers
				if workerIdx >= app.Config.Cluster.MinNodes {
					id := pretty_log.BeginTask("[KubeVirt] Disconnecting [Worker %d]", workerIdx+1)
					err = o.DisconnectWorker(workerIdx)
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
		pretty_log.TaskResult("[KubeVirt] No worker nodes to setup")
	}

	// Install KubeVirt on control node
	pretty_log.TaskGroup("[KubeVirt] Installing KubeVirt and virtctl on control node")
	controlCommandGroups = [][]string{
		{"kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/" + app.Config.KubeVirt.Version + "/kubevirt-operator.yaml > /dev/null"},
		{"kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/" + app.Config.KubeVirt.Version + "/kubevirt-cr.yaml > /dev/null"},
		{"kubectl apply -f https://github.com/kubevirt/containerized-data-importer/releases/download/" + app.Config.KubeVirt.CDI.Version + "/cdi-operator.yaml > /dev/null"},
		{"kubectl apply -f https://github.com/kubevirt/containerized-data-importer/releases/download/" + app.Config.KubeVirt.CDI.Version + "/cdi-cr.yaml > /dev/null"},
		{"wget https://github.com/kubevirt/kubevirt/releases/download/" + app.Config.KubeVirt.Virtctl.Version + "/virtctl-" + app.Config.KubeVirt.Virtctl.Version + "-linux-amd64 -O virtctl && sudo install virtctl /usr/local/bin/virtctl && rm virtctl"},
	}

	for idx, cmdGroup := range controlCommandGroups {
		id = pretty_log.BeginTask("[KubeVirt] - Command (%d/%d): %s", idx+1, len(controlCommandGroups), strings.Join(cmdGroup, " && "))

		_, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, cmdGroup)
		if err != nil {
			pretty_log.FailTask(id)
			return err
		}

		pretty_log.CompleteTask(id)
	}

	// Wait for KubeVirt to be deployed
	id = pretty_log.BeginTask("[KubeVirt] Waiting for KubeVirt to be deployed (max 300 seconds)")
	for i := 0; i < 300; i++ {
		// kubectl get kubevirt.kubevirt.io/kubevirt -n kubevirt -o=jsonpath="{.status.phase}" == Deployed
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{"kubectl get kubevirt.kubevirt.io/kubevirt -n kubevirt -o=jsonpath=\"{.status.phase}\""})
		if res != nil && res[0] == "Deployed" {
			break
		}

		time.Sleep(1 * time.Second)
	}
	pretty_log.CompleteTask(id)

	pretty_log.TaskGroup("[KubeVirt] Set up NFS, mounts and CSI Driver on control node")
	nfsServerIP := o.Environment.ControlNode.InternalIP
	nfsPaths := map[string]string{
		"disks":     "/mnt/nfs/disks",
		"snapshots": "/mnt/nfs/snapshots",
	}

	volumeSnapshotClassCRDs := `
apiVersion: helm.cattle.io/v1
kind: HelmChart
metadata:
  name: snapshot-controller
  namespace: kube-system
spec:
  repo: https://rke2-charts.rancher.io
  chart: rke2-snapshot-controller
---
apiVersion: helm.cattle.io/v1
kind: HelmChart
metadata:
  name: snapshot-controller-crd
  namespace: kube-system
spec:
  repo: https://rke2-charts.rancher.io
  chart: rke2-snapshot-controller-crd
---
apiVersion: helm.cattle.io/v1
kind: HelmChart
metadata:
  name: snapshot-validation-webhook
  namespace: kube-system
spec:
  repo: https://rke2-charts.rancher.io
  chart: rke2-snapshot-validation-webhook
`

	storageClass := fmt.Sprintf(`
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: vm-disks
provisioner: nfs.csi.k8s.io
parameters:
  server: %s
  share: %s
`, nfsServerIP, nfsPaths["disks"])

	nfsCommands := [][]string{
		// NFS and mounts
		{"sudo apt-get update -y"},
		{"sudo apt-get install nfs-kernel-server -y"},
		{"sudo mkdir -p /mnt/nfs /mnt/nfs/disks /mnt/nfs/snapshots"},
		{"echo \"/mnt/nfs *(rw,sync,no_subtree_check,no_root_squash)\" | sudo tee /etc/exports > /dev/null"},
		{"sudo exportfs -a"},

		// CSI driver
		{"curl -skSL https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/v4.6.0/deploy/install-driver.sh | bash -s v4.6.0 --"},

		// Storage classes
		{"kubectl apply -f - <<EOF" + volumeSnapshotClassCRDs + "EOF"},
		{"kubectl get storageclass vm-disks &> /dev/null || kubectl apply -f - <<EOF " + storageClass + "EOF"},
	}

	for idx, cmdGroup := range nfsCommands {
		id = pretty_log.BeginTask("[KubeVirt] - Command (%d/%d): %s", idx+1, len(nfsCommands), strings.Join(cmdGroup, " && "))
		_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, cmdGroup)
		if err != nil {
			pretty_log.FailTask(id)
			return err
		}

		pretty_log.CompleteTask(id)
	}

	return nil
}

func (o *KubeVirt) Setup() error {
	pretty_log.TaskGroup("[KubeVirt] Nothing to setup")
	return nil
}

func (o *KubeVirt) GetVM(name string) *models.VM {
	// Parse out CPU cores, RAM, and disk size to a VM struct { name: string, specs: { cpu: int, ram: int, diskSize: int } }. Use jq to parse the output of the command.
	getVmCommand := "kubectl get vm " + name + " -o json | jq '{name: .metadata.name, specs: {cpu: .spec.template.spec.domain.cpu.cores, ram: .spec.template.spec.domain.resources.requests.memory, diskSize: .spec.dataVolumeTemplates[0].spec.pvc.resources.requests.storage}}'"
	res, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{getVmCommand})
	if err != nil {
		return nil
	}

	vm, err := utils.ParseSshOutput[models.VM](res)
	if err != nil {
		return nil
	}

	return vm
}

func (o *KubeVirt) ListVMs() []models.VM {
	// Parse out a list of VMs to []VM structs { name: string, specs: { cpu: int, ram: int, diskSize: int } }. Use jq to parse the output of the command.
	listVmsCommand := "kubectl get vms -o json | jq '[.items[] | {name: .metadata.name, specs: {cpu: .spec.template.spec.domain.cpu.cores, ram: .spec.template.spec.domain.resources.requests.memory, diskSize: .spec.dataVolumeTemplates[0].spec.pvc.resources.requests.storage}}]'"
	res, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{listVmsCommand})
	if err != nil {
		return nil
	}

	vms, err := utils.ParseSshOutput[[]models.VM](res)
	if err != nil {
		return nil
	}

	return *vms
}

func (o *KubeVirt) CreateVM(vm *models.VM, hostIdx ...int) error {
	var host *int
	if len(hostIdx) > 0 {
		host = &hostIdx[0]
	}

	var affinity100Key string
	var affinity100Value string
	var affinity50Key string
	var affinity50Value string

	if host != nil {
		randomLabel := o.setRandomVmLabels(vm.Name)
		affinity100Key = randomLabel
		affinity100Value = randomLabel
		affinity50Key = "type"
		affinity50Value = "base"

		// Label the host
		err := o.labelHost(*o.Environment.WorkerNodes[*host].VM.Name, randomLabel, randomLabel)
		if err != nil {
			return err
		}

	} else {
		affinity100Key = "type"
		affinity100Value = "base"
		affinity50Key = "type"
		affinity50Value = "base"
	}

	// Label the host with idx=hostIdx[0] with the random label

	manifest := fmt.Sprintf(`
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: %s
spec:
  running: true
  template:
    spec:
      evictionStrategy: LiveMigrate
      affinity:
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            preference:
              matchExpressions:
              - key: %s
                operator: In
                values:
                - %s
          - weight: 1
            preference:
              matchExpressions:
              - key: %s
                operator: In
                values:
                - %s
      domain:
        devices:
          rng: {}
          disks:
          - disk:
              bus: virtio
            name: containerdisk
          - disk:
              bus: virtio
            name: emptydisk
          interfaces:
          - name: default
            masquerade: {}
        resources:
          requests:
            cpu: 100m
            memory: %dMi
          limits:
            cpu: %d
            memory: %dMi
      networks:
      - pod: {}
        name: default
      volumes:
      - name: emptydisk
        emptyDisk:
          capacity: %dGi
      - name: containerdisk
        containerDisk:
          image: %s
`, vm.Name, affinity100Key, affinity100Value, affinity50Key, affinity50Value, vm.Specs.RAM, vm.Specs.CPU, vm.Specs.RAM, vm.Specs.DiskSize, app.Config.KubeVirt.Image.URL)

	createVmCommand := "kubectl apply -f - <<EOF\n" + manifest + "\nEOF"
	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{createVmCommand})
	return err
}

func (o *KubeVirt) DeleteVM(name string) error {
	deleteVmCommand := "kubectl delete vm " + name
	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{deleteVmCommand})
	return err
}

func (o *KubeVirt) ConnectWorker(workerIdx int) error {
	_, err := utils.SshCommand(o.Environment.WorkerNodes[workerIdx].PublicIP, []string{"sudo systemctl restart k3s-agent"})
	if err != nil {
		return err
	}

	err = o.WaitForNodeLabel(*o.Environment.WorkerNodes[workerIdx].VM.Name, "kubevirt.io/schedulable", "true")
	if err != nil {
		return err
	}

	return nil
}

func (o *KubeVirt) DisconnectWorker(workerIdx int) error {
	// Check if node is already disconnected
	workerName := *o.Environment.WorkerNodes[workerIdx].VM.Name
	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{"kubectl get node " + workerName})
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") {
			return nil
		}

		return err
	}

	commands := []string{
		"kubectl drain " + workerName + " --delete-emptydir-data --force --ignore-daemonsets",
		"kubectl delete node " + workerName,
	}

	for _, cmd := range commands {
		_, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{cmd})
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *KubeVirt) MigrateVM(name string, hostIdx int) error {
	// Create migration object with the new host
	migrationManifest := fmt.Sprintf(`
apiVersion: kubevirt.io/v1
kind: VirtualMachineInstanceMigration
metadata:
  name: %s-migration
spec:
  vmiName: %s
`, name, name)

	createMigrationCommand := "kubectl apply -f - <<EOF\n" + migrationManifest + "\nEOF"
	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{createMigrationCommand})
	if err != nil {
		return err
	}

	// Label the host with idx=hostIdx with the VM's custom label to make it move
	randomLabel := o.getVmLabel(name)
	if randomLabel == "" {
		return fmt.Errorf("could not find label for VM %s", name)
	}

	err = o.labelHost(*o.Environment.WorkerNodes[hostIdx].VM.Name, randomLabel, randomLabel)
	if err != nil {
		return err
	}

	// Wait for the migration to finish
	err = o.WaitForFinishedMigration(name + "-migration")
	if err != nil {
		return err
	}

	// Delete the migration object
	deleteMigrationCommand := "kubectl delete vmim " + name + "-migration"
	_, err = utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{deleteMigrationCommand})
	if err != nil {
		return err
	}

	return nil
}

func (o *KubeVirt) CleanUp() error {
	// Ensure that the KubeVirt virt-handler DaemonSet is synced
	var workerThatShouldNotExist []string
	for idx, worker := range o.Environment.WorkerNodes {
		if idx >= app.Config.Cluster.MinNodes {
			workerThatShouldNotExist = append(workerThatShouldNotExist, *worker.VM.Name)
		}
	}

	// For every worker node, ensure that the virt-handler pod is synced
	for _, workerName := range workerThatShouldNotExist {
		getDaemonSetPodCommand := fmt.Sprintf("kubectl get pods -l kubevirt.io=virt-handler -n kubevirt --field-selector spec.nodeName=%s -o=jsonpath='{.items[0].metadata.name}'", workerName)
		res, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{getDaemonSetPodCommand})
		if err != nil {
			if strings.Contains(err.Error(), "array index out of bounds: index 0") {
				continue
			}

			return err
		}

		err = o.WaitForDeletedPod(res[0], "kubevirt")
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *KubeVirt) WaitForRunningVM(name string) error {
	runningCommand := "kubectl get vm " + name + " -o jsonpath='{.status.printableStatus}'"
	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{runningCommand})
		if len(res) == 0 {
			continue
		}

		if res[0] == "Running" {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for VM %s to be running", name)
		}
	}
}

func (o *KubeVirt) WaitForAccessibleVM(name string) error {
	runningCommand := "ssh -o PasswordAuthentication=no -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o 'ProxyCommand=virtctl port-forward --stdio=true vmi/" + name + " 22' cirros@vmi/" + name + " 'sudo echo hello' 2>&1 | grep 'Permission denied (publickey,password)' && exit 0 || exit 1"
	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{runningCommand})
		if len(res) == 0 {
			continue
		}

		if strings.Contains(res[0], "Permission denied (publickey,password)") {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for VM %s to be running", name)
		}
	}
}

func (o *KubeVirt) WaitForDeletedVM(name string) error {
	existsCommand := "kubectl get vm " + name
	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{existsCommand})
		if len(res) == 0 {
			continue
		}

		if res[0] == "" && strings.Contains(res[0], "NotFound") {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for VM %s to be running", name)
		}
	}
}

func (o *KubeVirt) WaitForDeletedPod(name, namespace string) error {
	existsCommand := "kubectl get pod " + name + " -n " + namespace
	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{existsCommand})
		if len(res) == 0 {
			continue
		}

		if strings.Contains(res[0], "NotFound") {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for pod %s to be deleted", name)
		}
	}
}

func (o *KubeVirt) WaitForFinishedMigration(name string) error {
	runningCommand := "kubectl get vmim " + name + " -o jsonpath='{.status.phase}'"
	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, _ := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{runningCommand})
		if len(res) == 0 {
			continue
		}

		if res[0] == "Succeeded" {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for VM %s to be running", name)
		}
	}
}

func (o *KubeVirt) WaitForNodeLabel(node, label, value string) error {
	// kubectl get nodes kubevirt-worker-3 -o=jsonpath='{.metadata.labels.kubevirt\.io/schedulable}'
	label = strings.ReplaceAll(label, ".", `\.`)
	runningCommand := fmt.Sprintf("kubectl get nodes %s -o=jsonpath='{.metadata.labels.%s}'", node, label)
	attemptsLeft := 1000
	for {
		time.Sleep(100 * time.Millisecond)
		attemptsLeft--
		res, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{runningCommand})
		if len(res) == 0 || err != nil {
			continue
		}

		if strings.Contains(res[0], value) {
			return nil
		}

		if attemptsLeft <= 0 {
			return fmt.Errorf("timeout waiting for node %s to have label %s=%s", node, label, value)
		}
	}
}

func (o *KubeVirt) DeleteAllVMs() error {
	deleteAllVmsCommand := "kubectl delete vms --all"
	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{deleteAllVmsCommand})
	return err
}

// labelHost labels the host with the given label and value
// If value is empty, the label is removed
func (o *KubeVirt) labelHost(host, label, value string) error {
	var labelHostCommand string
	if value == "" {
		labelHostCommand = fmt.Sprintf("kubectl label node %s %s- --overwrite", host, label)
	} else {
		labelHostCommand = fmt.Sprintf("kubectl label node %s %s=%s --overwrite", host, label, value)
	}

	_, err := utils.SshCommand(o.Environment.ControlNode.PublicIP, []string{labelHostCommand})
	return err
}

func (o *KubeVirt) setRandomVmLabels(vmName string) string {
	randomLabel := utils.RandomName("worker")
	mut.Lock()
	if o.nodeSelectors == nil {
		o.nodeSelectors = make(map[string]string)
	}
	o.nodeSelectors[vmName] = randomLabel
	mut.Unlock()

	return randomLabel
}

func (o *KubeVirt) getVmLabel(vmName string) string {
	mut.RLock()
	defer mut.RUnlock()
	if o.nodeSelectors == nil {
		return ""
	}

	return o.nodeSelectors[vmName]
}
