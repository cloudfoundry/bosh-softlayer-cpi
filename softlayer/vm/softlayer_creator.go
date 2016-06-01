package vm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"text/template"
	"time"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	boshsys "github.com/cloudfoundry/bosh-utils/system"
	boshuuid "github.com/cloudfoundry/bosh-utils/uuid"

	bslcommon "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/common"
	bslcstem "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/stemcell"
	datatypes "github.com/maximilien/softlayer-go/data_types"
	sl "github.com/maximilien/softlayer-go/softlayer"

	common "github.com/cloudfoundry/bosh-softlayer-cpi/common"
	util "github.com/cloudfoundry/bosh-softlayer-cpi/util"
)

const SOFTLAYER_VM_CREATOR_LOG_TAG = "SoftLayerVMCreator"

type SoftLayerCreator struct {
	softLayerClient        sl.Client
	agentEnvServiceFactory AgentEnvServiceFactory

	agentOptions  AgentOptions
	logger        boshlog.Logger
	uuidGenerator boshuuid.Generator
	fs            boshsys.FileSystem
	vmFinder      Finder
}

func NewSoftLayerCreator(softLayerClient sl.Client, agentEnvServiceFactory AgentEnvServiceFactory, agentOptions AgentOptions, logger boshlog.Logger, uuidGenerator boshuuid.Generator, fs boshsys.FileSystem, vmFinder Finder) SoftLayerCreator {
	bslcommon.TIMEOUT = 120 * time.Minute
	bslcommon.POLLING_INTERVAL = 5 * time.Second

	return SoftLayerCreator{
		softLayerClient:        softLayerClient,
		agentEnvServiceFactory: agentEnvServiceFactory,
		agentOptions:           agentOptions,
		logger:                 logger,
		uuidGenerator:          uuidGenerator,
		fs:                     fs,
		vmFinder:               vmFinder,
	}
}

func (c SoftLayerCreator) CreateByBPS(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	hardwareId, err := c.CreateBaremetal(cloudProps.VmNamePrefix, cloudProps.BaremetalStemcell, cloudProps.BaremetalNetbootImage)
	if err != nil {
		return SoftLayerHardware{}, bosherr.WrapError(err, "Create baremetal error")
	}

	hardware, found, err := c.vmFinder.Find(hardwareId)
	if err != nil || !found {
		return SoftLayerHardware{}, bosherr.WrapErrorf(err, "Cannot find hardware with id: %d.", hardwareId)
	}

	softlayerFileService := NewSoftlayerFileService(util.GetSshClient(), c.logger, c.uuidGenerator, c.fs)
	agentEnvService := c.agentEnvServiceFactory.New(softlayerFileService, strconv.Itoa(hardwareId))

	// Update mbus url setting
	mbus, err := c.parseMbusURL(c.agentOptions.Mbus, cloudProps.BoshIp)
	if err != nil {
		return SoftLayerHardware{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
	}
	c.agentOptions.Mbus = mbus
	// Update blobstore setting
	switch c.agentOptions.Blobstore.Provider {
	case BlobstoreTypeDav:
		davConf := DavConfig(c.agentOptions.Blobstore.Options)
		c.updateDavConfig(&davConf, cloudProps.BoshIp)
	}

	agentEnv := CreateAgentUserData(agentID, cloudProps, networks, env, c.agentOptions)
	if err != nil {
		return SoftLayerHardware{}, bosherr.WrapErrorf(err, "Cannot agent env for virtual guest with id: %d.", hardwareId)
	}

	err = agentEnvService.Update(agentEnv)
	if err != nil {
		return SoftLayerHardware{}, bosherr.WrapError(err, "Updating VM's agent env")
	}

	if len(c.agentOptions.VcapPassword) > 0 {
		err = hardware.SetVcapPassword(c.agentOptions.VcapPassword)
		if err != nil {
			return SoftLayerHardware{}, bosherr.WrapError(err, "Updating VM's vcap password")
		}
	}

	return hardware, nil
}

func (c SoftLayerCreator) CreateBySoftlayer(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	virtualGuestTemplate, err := CreateVirtualGuestTemplate(stemcell, cloudProps)
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Creating virtual guest template")
	}

	virtualGuestService, err := c.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Creating VirtualGuestService from SoftLayer client")
	}

	virtualGuest, err := virtualGuestService.CreateObject(virtualGuestTemplate)
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Creating VirtualGuest from SoftLayer client")
	}

	if cloudProps.EphemeralDiskSize == 0 {
		err = bslcommon.WaitForVirtualGuestLastCompleteTransaction(c.softLayerClient, virtualGuest.Id, "Service Setup")
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Waiting for VirtualGuest `%d` has Service Setup transaction complete", virtualGuest.Id)
		}
	} else {
		err = bslcommon.AttachEphemeralDiskToVirtualGuest(c.softLayerClient, virtualGuest.Id, cloudProps.EphemeralDiskSize, c.logger)
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapError(err, fmt.Sprintf("Attaching ephemeral disk to VirtualGuest `%d`", virtualGuest.Id))
		}
	}

	vm, found, err := c.vmFinder.Find(virtualGuest.Id)
	if err != nil || !found {
		return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Cannot find virtual guest with id: %d.", virtualGuest.Id)
	}

	softlayerFileService := NewSoftlayerFileService(util.GetSshClient(), c.logger, c.uuidGenerator, c.fs)
	agentEnvService := c.agentEnvServiceFactory.New(softlayerFileService, strconv.Itoa(vm.ID()))

	if len(cloudProps.BoshIp) == 0 {
		// update /etc/hosts file of bosh-init vm
		c.updateEtcHostsOfBoshInit(fmt.Sprintf("%s  %s", vm.GetPrimaryBackendIP(), vm.GetFullyQualifiedDomainName()))
		// Update mbus url setting for bosh director: construct mbus url with new director ip
		mbus, err := c.parseMbusURL(c.agentOptions.Mbus, vm.GetPrimaryBackendIP())
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
		}
		c.agentOptions.Mbus = mbus
	} else {
		// Update mbus url setting
		mbus, err := c.parseMbusURL(c.agentOptions.Mbus, cloudProps.BoshIp)
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
		}
		c.agentOptions.Mbus = mbus
		// Update blobstore setting
		switch c.agentOptions.Blobstore.Provider {
		case BlobstoreTypeDav:
			davConf := DavConfig(c.agentOptions.Blobstore.Options)
			c.updateDavConfig(&davConf, cloudProps.BoshIp)
		}
	}

	agentEnv := CreateAgentUserData(agentID, cloudProps, networks, env, c.agentOptions)
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Cannot agent env for virtual guest with id: %d.", vm.ID())
	}

	err = agentEnvService.Update(agentEnv)
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Updating VM's agent env")
	}

	if len(c.agentOptions.VcapPassword) > 0 {
		err = vm.SetVcapPassword(c.agentOptions.VcapPassword)
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Updating VM's vcap password")
		}
	}

	return vm, nil
}

func (c SoftLayerCreator) CreateByOSReload(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	virtualGuestService, err := c.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Creating VirtualGuestService from SoftLayer client")
	}

	var virtualGuest datatypes.SoftLayer_Virtual_Guest

	if common.IsPrivateSubnet(net.ParseIP(networks.First().IP)) {
		virtualGuest, err = virtualGuestService.GetObjectByPrimaryBackendIpAddress(networks.First().IP)
	} else {
		virtualGuest, err = virtualGuestService.GetObjectByPrimaryIpAddress(networks.First().IP)
	}

	if err != nil || virtualGuest.Id == 0 {
		return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Could not find virtual guest by ip address: %s", networks.First().IP)
	}

	c.logger.Info(SOFTLAYER_VM_CREATOR_LOG_TAG, fmt.Sprintf("OS reload on the server id %d with stemcell %d", virtualGuest.Id, stemcell.ID()))

	vm, found, err := c.vmFinder.Find(virtualGuest.Id)
	if err != nil || !found {
		return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Cannot find virtual guest with id: %d", virtualGuest.Id)
	}

	bslcommon.TIMEOUT = 4 * time.Hour
	err = vm.ReloadOS(stemcell)
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Failed to reload OS")
	}

	if cloudProps.EphemeralDiskSize == 0 {
		err = bslcommon.WaitForVirtualGuestLastCompleteTransaction(c.softLayerClient, vm.ID(), "Service Setup")
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Waiting for VirtualGuest `%d` has Service Setup transaction complete", vm.ID())
		}
	} else {
		err = bslcommon.AttachEphemeralDiskToVirtualGuest(c.softLayerClient, vm.ID(), cloudProps.EphemeralDiskSize, c.logger)
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapError(err, fmt.Sprintf("Attaching ephemeral disk to VirtualGuest `%d`", vm.ID()))
		}
	}

	softlayerFileService := NewSoftlayerFileService(util.GetSshClient(), c.logger, c.uuidGenerator, c.fs)
	agentEnvService := c.agentEnvServiceFactory.New(softlayerFileService, strconv.Itoa(vm.ID()))

	if len(cloudProps.BoshIp) == 0 {
		// update /etc/hosts file of bosh-init vm
		c.updateEtcHostsOfBoshInit(fmt.Sprintf("%s  %s", vm.GetPrimaryBackendIP(), vm.GetFullyQualifiedDomainName()))
		// Update mbus url setting for bosh director: construct mbus url with new director ip
		mbus, err := c.parseMbusURL(c.agentOptions.Mbus, vm.GetPrimaryBackendIP())
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
		}
		c.agentOptions.Mbus = mbus
	} else {
		// Update mbus url setting
		mbus, err := c.parseMbusURL(c.agentOptions.Mbus, cloudProps.BoshIp)
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
		}
		c.agentOptions.Mbus = mbus
		// Update blobstore setting
		switch c.agentOptions.Blobstore.Provider {
		case BlobstoreTypeDav:
			davConf := DavConfig(c.agentOptions.Blobstore.Options)
			c.updateDavConfig(&davConf, cloudProps.BoshIp)
		}
	}

	agentEnv := CreateAgentUserData(agentID, cloudProps, networks, env, c.agentOptions)
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapErrorf(err, "Cannot agent env for virtual guest with id: %d", vm.ID())
	}

	err = agentEnvService.Update(agentEnv)
	if err != nil {
		return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Updating VM's agent env")
	}

	if len(c.agentOptions.VcapPassword) > 0 {
		err = vm.SetVcapPassword(c.agentOptions.VcapPassword)
		if err != nil {
			return SoftLayerVirtualGuest{}, bosherr.WrapError(err, "Updating VM's vcap password")
		}
	}
	return vm, nil
}

// Private methods
func (c SoftLayerCreator) updateDavConfig(config *DavConfig, directorIP string) (err error) {
	url := (*config)["endpoint"].(string)
	mbus, err := c.parseMbusURL(url, directorIP)
	if err != nil {
		return bosherr.WrapError(err, "Parsing Mbus URL")
	}

	(*config)["endpoint"] = mbus

	return nil
}

func (c SoftLayerCreator) parseMbusURL(mbusURL string, primaryBackendIpAddress string) (string, error) {
	parsedURL, err := url.Parse(mbusURL)
	if err != nil {
		return "", bosherr.WrapError(err, "Parsing Mbus URL")
	}
	var username, password, port string
	_, port, _ = net.SplitHostPort(parsedURL.Host)
	userInfo := parsedURL.User
	if userInfo != nil {
		username = userInfo.Username()
		password, _ = userInfo.Password()
		return fmt.Sprintf("%s://%s:%s@%s:%s", parsedURL.Scheme, username, password, primaryBackendIpAddress, port), nil
	}

	return fmt.Sprintf("%s://%s:%s", parsedURL.Scheme, primaryBackendIpAddress, port), nil
}

func (c SoftLayerCreator) updateEtcHostsOfBoshInit(record string) (err error) {
	buffer := bytes.NewBuffer([]byte{})
	t := template.Must(template.New("etc-hosts").Parse(ETC_HOSTS_TEMPLATE))

	err = t.Execute(buffer, record)
	if err != nil {
		return bosherr.WrapError(err, "Generating config from template")
	}

	err = c.fs.WriteFile("/etc/hosts", buffer.Bytes())
	if err != nil {
		return bosherr.WrapError(err, "Writing to /etc/hosts")
	}

	return nil
}

const ETC_HOSTS_TEMPLATE = `127.0.0.1 localhost
{{.}}
`

func (c SoftLayerCreator) appendRecordToEtcHosts(record string) (err error) {
	file, err := os.OpenFile("/etc/hosts", os.O_RDWR|os.O_APPEND, 0666)
	if err != nil {
		return bosherr.WrapError(err, "Failed to open file /etc/hosts")
	}

	defer file.Close()

	_, err = fmt.Fprintf(file, "\n %s", record)
	if err != nil {
		return bosherr.WrapErrorf(err, "Failed to append new record to /etc/hosts")
	}

	return nil
}

func (c SoftLayerCreator) CreateBaremetal(server_name string, stemcell string, netboot_image string) (server_id int, err error) {
	body, err := util.CallBPS("PUT", "/baremetal/spec/"+server_name+"/"+stemcell+"/"+netboot_image, "")
	if err != nil {
		return 0, bosherr.WrapErrorf(err, "Faled to call BPS")
	}

	c.logger.Info(SOFTLAYER_VM_CREATOR_LOG_TAG, fmt.Sprintf("Returns from BPS: %s", string(body)))

	var result map[string]interface{}

	if err = json.Unmarshal(body, &result); err != nil {
		return 0, err
	}
	if result["status"].(float64) != 200 {
		return 0, bosherr.Errorf("Error: " + string(body))
	}
	data := result["data"].(map[string]interface{})
	task_id := strconv.FormatFloat(data["task_id"].(float64), 'f', 0, 32)
	for {
		time.Sleep(5 * time.Second)
		if body, err = util.CallBPS("GET", "/task/"+task_id+"/json/task", ""); err != nil {
			return 0, bosherr.WrapErrorf(err, "Faled to call BPS")
		}
		c.logger.Info(SOFTLAYER_VM_CREATOR_LOG_TAG, fmt.Sprintf("Returns from BPS: %s", string(body)))
		if err = json.Unmarshal(body, &result); err != nil {
			return 0, bosherr.WrapErrorf(err, "Faled to call BPS")
		}
		data := result["data"].(map[string]interface{})
		info := data["info"].(map[string]interface{})
		if info["status"] == nil {
			continue
		}
		state := info["status"].(string)
		if state == "failed" {
			return 0, bosherr.Errorf("Failed to install the stemcell: " + string(body))
		}
		if state == "completed" {
			if body, err = util.CallBPS("GET", "/task/"+task_id+"/json/server", ""); err != nil {
				return 0, bosherr.WrapErrorf(err, "Faled to call BPS")
			}
			if err = json.Unmarshal(body, &result); err != nil {
				return 0, bosherr.WrapErrorf(err, "Faled to call BPS")
			}
			data = result["data"].(map[string]interface{})
			info = data["info"].(map[string]interface{})
			return int(info["id"].(float64)), nil
		}
	}
}
