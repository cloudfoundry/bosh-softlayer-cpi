package vm

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"strconv"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"

	sl "github.com/maximilien/softlayer-go/softlayer"

	bslcommon "github.com/maximilien/bosh-softlayer-cpi/softlayer/common"
	bslcdisk "github.com/maximilien/bosh-softlayer-cpi/softlayer/disk"

	util "github.com/maximilien/bosh-softlayer-cpi/util"
	datatypes "github.com/maximilien/softlayer-go/data_types"
)

const (
	softLayerVMtag = "SoftLayerVM"
	ROOT_USER_NAME = "root"
)

const deleteVMLogTag = "Delete VM"

type SoftLayerVM struct {
	id int

	softLayerClient sl.Client
	agentEnvService AgentEnvService

	sshClient util.SshClient

	logger boshlog.Logger
}

func NewSoftLayerVM(id int, softLayerClient sl.Client, sshClient util.SshClient, agentEnvService AgentEnvService, logger boshlog.Logger) SoftLayerVM {
	bslcommon.TIMEOUT = 10 * time.Minute
	bslcommon.POLLING_INTERVAL = 10 * time.Second

	return SoftLayerVM{
		id: id,

		softLayerClient: softLayerClient,
		agentEnvService: agentEnvService,

		sshClient: sshClient,

		logger: logger,
	}
}

func (vm SoftLayerVM) ID() int { return vm.id }

func (vm SoftLayerVM) Delete() error {
	virtualGuestService, err := vm.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return bosherr.WrapError(err, "Creating SoftLayer VirtualGuestService from client")
	}
	
	vmCID := vm.ID()
	err = bslcommon.WaitForVirtualGuestToHaveNoRunningTransactions(vm.softLayerClient, vmCID, bslcommon.TIMEOUT, bslcommon.POLLING_INTERVAL)
	if err != nil {
		return bosherr.WrapError(err, fmt.Sprintf("Waiting for VirtualGuest `%d` to have no pending transactions before deleting vm", vmCID))
	}

	deleted, err := virtualGuestService.DeleteObject(vm.ID())
	if err != nil {
		return bosherr.WrapError(err, "Deleting SoftLayer VirtualGuest from client")
	}

	if !deleted {
		return bosherr.WrapError(nil, "Did not delete SoftLayer VirtualGuest from client")
	}
	
	totalTime := time.Duration(0)
	for totalTime < bslcommon.TIMEOUT {
		activeTransactions, err := virtualGuestService.GetActiveTransactions(vmCID)
		if err != nil {
			return bosherr.WrapError(err, "Getting active transactions from SoftLayer client")
		}

		if len(activeTransactions) > 0 {
			vm.logger.Info(deleteVMLogTag, "Delete VM transaction started", nil)
			break
		}

		totalTime += bslcommon.POLLING_INTERVAL
		time.Sleep(bslcommon.POLLING_INTERVAL)
	}

	if totalTime >= bslcommon.TIMEOUT {
		return bosherr.WrapError(err, "Waiting for DeleteVM transaction to start TIME OUT!")
	}

	totalTime = time.Duration(0)
	for totalTime < bslcommon.TIMEOUT {
		vm1, err := virtualGuestService.GetObject(vmCID)
		if err != nil || vm1.Id == 0 { 
			vm.logger.Info(deleteVMLogTag, "VM doesn't exist. Delete done", nil)
			break
		}
		
		activeTransaction, err := virtualGuestService.GetActiveTransaction(vmCID)
		if err != nil { 
			return bosherr.WrapError(err, "Getting active transactions from SoftLayer client")
		}

		averageTransactionDuration, _ := strconv.ParseFloat(activeTransaction.TransactionStatus.AverageDuration, 32)

		if averageTransactionDuration > 30 { // long transaction (for monthly charged)
			vm.logger.Info(deleteVMLogTag, "Deleting VM instance had been launched and it is a long transaction. Please check Softlayer Portal", nil)
			break
		}

		vm.logger.Info(deleteVMLogTag, "This is a short transaction, waiting for all active transactions to complete", nil)
		totalTime += bslcommon.POLLING_INTERVAL
		time.Sleep(bslcommon.POLLING_INTERVAL)
	}

	if totalTime >= bslcommon.TIMEOUT {
		return bosherr.WrapError(err, "After deleting a vm, waiting for active transactions to complete TIME OUT!")
	}


	return nil
}

func (vm SoftLayerVM) Reboot() error {
	virtualGuestService, err := vm.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return bosherr.WrapError(err, "Creating SoftLayer VirtualGuestService from client")
	}

	rebooted, err := virtualGuestService.RebootSoft(vm.ID())
	if err != nil {
		return bosherr.WrapError(err, "Rebooting (soft) SoftLayer VirtualGuest from client")
	}

	if !rebooted {
		return bosherr.WrapError(nil, "Did not reboot (soft) SoftLayer VirtualGuest from client")
	}

	return nil
}

func (vm SoftLayerVM) SetMetadata(vmMetadata VMMetadata) error {
	tags, err := vm.extractTagsFromVMMetadata(vmMetadata)
	if err != nil {
		return err
	}

	if len(tags) == 0 {
		return nil
	}

	//Check below needed since Golang strings.Split return [""] on strings.Split("", ",")
	if len(tags) == 1 && tags[0] == "" {
		return nil
	}

	virtualGuestService, err := vm.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return bosherr.WrapError(err, "Creating SoftLayer VirtualGuestService from client")
	}

	success, err := virtualGuestService.SetTags(vm.ID(), tags)
	if !success {
		return bosherr.WrapErrorf(err, "Settings tags on SoftLayer VirtualGuest `%d`", vm.ID())
	}

	if err != nil {
		return bosherr.WrapErrorf(err, "Settings tags on SoftLayer VirtualGuest `%d`", vm.ID())
	}

	return nil
}

func (vm SoftLayerVM) ConfigureNetworks(networks Networks) error {
	return NotSupportedError{}
}

func (vm SoftLayerVM) AttachDisk(disk bslcdisk.Disk) error {
	virtualGuest, volume, err := vm.fetchVMandIscsiVolume(vm.ID(), disk.ID())
	if err != nil {
		return err
	}

	_, err = vm.attachVolumeBasedOnShellScript(virtualGuest, volume)
	if err != nil {
		return bosherr.WrapErrorf(err, "Failed to attach volume with id %d to virtual guest with id: %d.", volume.Id, virtualGuest.Id)
	}

	return nil
}

func (vm SoftLayerVM) DetachDisk(disk bslcdisk.Disk) error {
	virtualGuest, volume, err := vm.fetchVMandIscsiVolume(vm.ID(), disk.ID())
	if err != nil {
		return err
	}

	err = vm.detachVolumeBasedOnShellScript(virtualGuest, volume)
	if err != nil {
		return bosherr.WrapErrorf(err, "Failed to detach volume with id %d from virtual guest with id: %d.", volume.Id, virtualGuest.Id)
	}

	return nil
}

// Private methods
func (vm SoftLayerVM) extractTagsFromVMMetadata(vmMetadata VMMetadata) ([]string, error) {
	tags := []string{}
	for key, value := range vmMetadata {
		if key == "tags" {
			stringValue, ok := value.(string)
			if !ok {
				return []string{}, bosherr.Errorf("Could not convert tags metadata value `%v` to string", value)
			}

			tags = vm.parseTags(stringValue)
		}
	}

	return tags, nil
}

func (vm SoftLayerVM) parseTags(value string) []string {
	return strings.Split(value, ",")
}

func (vm SoftLayerVM) fetchVMandIscsiVolume(vmId int, volumeId int) (datatypes.SoftLayer_Virtual_Guest, datatypes.SoftLayer_Network_Storage, error) {
	virtualGuestService, err := vm.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return datatypes.SoftLayer_Virtual_Guest{}, datatypes.SoftLayer_Network_Storage{}, bosherr.WrapError(err, "Can not get softlayer virtual guest service.")
	}

	networkStorageService, err := vm.softLayerClient.GetSoftLayer_Network_Storage_Service()
	if err != nil {
		return datatypes.SoftLayer_Virtual_Guest{}, datatypes.SoftLayer_Network_Storage{}, bosherr.WrapError(err, "Can not get network storage service.")
	}

	virtualGuest, err := virtualGuestService.GetObject(vmId)
	if err != nil {
		return datatypes.SoftLayer_Virtual_Guest{}, datatypes.SoftLayer_Network_Storage{}, bosherr.WrapErrorf(err, "Can not get virtual guest with id: %d", vmId)
	}

	volume, err := networkStorageService.GetIscsiVolume(volumeId)
	if err != nil {
		return datatypes.SoftLayer_Virtual_Guest{}, datatypes.SoftLayer_Network_Storage{}, bosherr.WrapErrorf(err, "Failed to get iSCSI volume with id: %d", volumeId)
	}

	return virtualGuest, volume, nil
}

func (vm SoftLayerVM) attachVolumeBasedOnShellScript(virtualGuest datatypes.SoftLayer_Virtual_Guest, volume datatypes.SoftLayer_Network_Storage) (string, error) {
	command := fmt.Sprintf(`
		export PATH=/etc/init.d:$PATH
		cp /etc/iscsi/iscsid.conf{,.save}
		sed '/^node.startup/s/^.*/node.startup = automatic/' -i /etc/iscsi/iscsid.conf
		sed '/^#node.session.auth.authmethod/s/#//' -i /etc/iscsi/iscsid.conf
		sed '/^#node.session.auth.username / {s/#//; s/ username/ %s/}' -i /etc/iscsi/iscsid.conf
		sed '/^#node.session.auth.password / {s/#//; s/ password/ %s/}' -i /etc/iscsi/iscsid.conf
		sed '/^#discovery.sendtargets.auth.username / {s/#//; s/ username/ %s/}' -i /etc/iscsi/iscsid.conf
		sed '/^#discovery.sendtargets.auth.password / {s/#//; s/ password/ %s/}' -i /etc/iscsi/iscsid.conf
		open-iscsi restart
		rm -r /etc/iscsi/send_targets
		open-iscsi stop
		open-iscsi start
		iscsiadm -m discovery -t sendtargets -p %s
		open-iscsi restart`,
		volume.Username,
		volume.Password,
		volume.Username,
		volume.Password,
		volume.ServiceResourceBackendIpAddress,
	)

	_, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.getRootPassword(virtualGuest), virtualGuest.PrimaryIpAddress, command)
	if err != nil {
		return "", err
	}

	_, deviceName, err := vm.findIscsiDeviceNameBasedOnShellScript(virtualGuest, volume)
	if err != nil {
		return "", err
	}

	return deviceName, nil
}

func (vm SoftLayerVM) detachVolumeBasedOnShellScript(virtualGuest datatypes.SoftLayer_Virtual_Guest, volume datatypes.SoftLayer_Network_Storage) error {
	targetName, _, err := vm.findIscsiDeviceNameBasedOnShellScript(virtualGuest, volume)
	command := fmt.Sprintf(`
		iscsiadm -m node -T %s -u
		iscsiadm -m node -o delete -T %s`,
		targetName,
		targetName,
	)

	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.getRootPassword(virtualGuest), virtualGuest.PrimaryIpAddress, command)

	return err
}

func (vm SoftLayerVM) findIscsiDeviceNameBasedOnShellScript(virtualGuest datatypes.SoftLayer_Virtual_Guest, volume datatypes.SoftLayer_Network_Storage) (targetName string, deviceName string, err error) {
	command := `
		sleep 1
		iscsiadm -m session -P3 | sed -n  "/Target:/s/Target: //p; /Attached scsi disk /{ s/Attached scsi disk //; s/State:.*//p}"`

	output, err := vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.getRootPassword(virtualGuest), virtualGuest.PrimaryIpAddress, command)
	if err != nil {
		return "", "", err
	}

	lines := strings.Split(strings.Trim(output, "\n"), "\n")

	for i := 0; i < len(lines); i += 2 {
		if strings.Contains(lines[i], strings.ToLower(volume.Username)) {
			return strings.Trim(lines[i], "\t"), strings.Trim(lines[i+1], "\t"), nil
		}
	}

	return "", "", errors.New(fmt.Sprintf("Can not find matched iSCSI device for user name: %s", volume.Username))
}

func (vm SoftLayerVM) getRootPassword(virtualGuest datatypes.SoftLayer_Virtual_Guest) string {
	passwords := virtualGuest.OperatingSystem.Passwords

	for _, password := range passwords {
		if password.Username == ROOT_USER_NAME {
			return password.Password
		}
	}

	return ""
}
