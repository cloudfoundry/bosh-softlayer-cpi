package vm

import (
	"bytes"
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

	agentOptions AgentOptions
	logger       boshlog.Logger
	fs           boshsys.FileSystem
	sshClient    util.SshClient
}

func NewSoftLayerCreator(softLayerClient sl.Client, agentEnvServiceFactory AgentEnvServiceFactory, agentOptions AgentOptions, logger boshlog.Logger, fs boshsys.FileSystem, sshClient util.SshClient) SoftLayerCreator {
	bslcommon.TIMEOUT = 120 * time.Minute
	bslcommon.POLLING_INTERVAL = 5 * time.Second

	return SoftLayerCreator{
		softLayerClient:        softLayerClient,
		agentEnvServiceFactory: agentEnvServiceFactory,
		agentOptions:           agentOptions,
		logger:                 logger,
		fs:                     fs,
		sshClient:              sshClient,
	}
}

type sshClientWrapper struct {
	client   util.SshClient
	ip       string
	user     string
	password string
}

func (s *sshClientWrapper) Output(command string) ([]byte, error) {
	o, err := s.client.ExecCommand(s.user, s.password, s.ip, command)
	return []byte(o), err
}

func (c SoftLayerCreator) CreateBySoftlayer(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {

	virtualGuestTemplate, err := CreateVirtualGuestTemplate(stemcell, cloudProps)

	if err != nil {
		return SoftLayerVM{}, bosherr.WrapError(err, "Creating virtual guest template")
	}

	virtualGuestService, err := c.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapError(err, "Creating VirtualGuestService from SoftLayer client")
	}

	virtualGuest, err := virtualGuestService.CreateObject(virtualGuestTemplate)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapError(err, "Creating VirtualGuest from SoftLayer client")
	}

	if cloudProps.EphemeralDiskSize == 0 {
		err = bslcommon.WaitForVirtualGuestLastCompleteTransaction(c.softLayerClient, virtualGuest.Id, "Service Setup")
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapErrorf(err, "Waiting for VirtualGuest `%d` has Service Setup transaction complete", virtualGuest.Id)
		}
	} else {
		err = bslcommon.AttachEphemeralDiskToVirtualGuest(c.softLayerClient, virtualGuest.Id, cloudProps.EphemeralDiskSize, c.logger)
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapError(err, fmt.Sprintf("Attaching ephemeral disk to VirtualGuest `%d`", virtualGuest.Id))
		}
	}

	virtualGuest, err = bslcommon.GetObjectDetailsOnVirtualGuest(c.softLayerClient, virtualGuest.Id)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapErrorf(err, "Cannot get details from virtual guest with id: %d.", virtualGuest.Id)
	}

	softlayerFileService := NewSoftlayerFileService(c.sshClient, virtualGuest, c.logger)
	agentEnvService := c.agentEnvServiceFactory.New(softlayerFileService, strconv.Itoa(virtualGuest.Id))

	if len(cloudProps.BoshIp) == 0 {
		// update /etc/hosts file of bosh-init vm
		c.updateEtcHostsOfBoshInit(fmt.Sprintf("%s  %s", virtualGuest.PrimaryBackendIpAddress, virtualGuest.FullyQualifiedDomainName))
		// Update mbus url setting for bosh director: construct mbus url with new director ip
		mbus, err := c.parseMbusURL(c.agentOptions.Mbus, virtualGuest.PrimaryBackendIpAddress)
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
		}
		c.agentOptions.Mbus = mbus
	} else {
		// Update mbus url setting
		mbus, err := c.parseMbusURL(c.agentOptions.Mbus, cloudProps.BoshIp)
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
		}
		c.agentOptions.Mbus = mbus
		// Update blobstore setting
		switch c.agentOptions.Blobstore.Provider {
		case BlobstoreTypeDav:
			davConf := DavConfig(c.agentOptions.Blobstore.Options)
			c.updateDavConfig(&davConf, cloudProps.BoshIp)
		}
	}

	vm := NewSoftLayerVM(virtualGuest.Id, c.softLayerClient, c.sshClient, agentEnvService, c.logger)

	ubuntu := Ubuntu{
		SoftLayerClient: c.softLayerClient.GetHttpClient(),
		SSHClient: &sshClientWrapper{
			client:   c.sshClient,
			ip:       virtualGuest.PrimaryBackendIpAddress,
			user:     ROOT_USER_NAME,
			password: vm.getRootPassword(virtualGuest),
		},
		SoftLayerFileService: softlayerFileService,
	}

	err = ubuntu.ConfigureNetwork(networks, virtualGuest.Id)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapErrorf(err, "Failed to configure networking for virtual guest with id: %d.", virtualGuest.Id)
	}

	agentEnv := CreateAgentUserData(agentID, cloudProps, networks, env, c.agentOptions)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapErrorf(err, "Cannot agent env for virtual guest with id: %d.", virtualGuest.Id)
	}

	err = agentEnvService.Update(agentEnv)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapError(err, "Updating VM's agent env")
	}

	if len(c.agentOptions.VcapPassword) > 0 {
		err = c.SetVcapPassword(vm, virtualGuest, c.agentOptions.VcapPassword)
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapError(err, "Updating VM's vcap password")
		}
	}

	return vm, nil
}

func (c SoftLayerCreator) CreateByOSReload(agentID string, stemcell bslcstem.Stemcell, cloudProps VMCloudProperties, networks Networks, env Environment) (VM, error) {
	virtualGuestService, err := c.softLayerClient.GetSoftLayer_Virtual_Guest_Service()
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapError(err, "Creating VirtualGuestService from SoftLayer client")
	}

	var virtualGuest datatypes.SoftLayer_Virtual_Guest

	if common.IsPrivateSubnet(net.ParseIP(networks.First().IP)) {
		virtualGuest, err = virtualGuestService.GetObjectByPrimaryBackendIpAddress(networks.First().IP)
	} else {
		virtualGuest, err = virtualGuestService.GetObjectByPrimaryIpAddress(networks.First().IP)
	}

	if err != nil || virtualGuest.Id == 0 {
		return SoftLayerVM{}, bosherr.WrapErrorf(err, "Could not find virtual guest by ip address: %s", networks.First().IP)
	}

	c.logger.Info(SOFTLAYER_VM_CREATOR_LOG_TAG, fmt.Sprintf("OS reload on the server id %d with stemcell %d", virtualGuest.Id, stemcell.ID()))

	vm := NewSoftLayerVM(virtualGuest.Id, c.softLayerClient, c.sshClient, nil, c.logger)

	bslcommon.TIMEOUT = 4 * time.Hour
	err = vm.ReloadOS(stemcell)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapError(err, "Failed to reload OS")
	}

	if cloudProps.EphemeralDiskSize == 0 {
		err = bslcommon.WaitForVirtualGuestLastCompleteTransaction(c.softLayerClient, virtualGuest.Id, "Service Setup")
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapErrorf(err, "Waiting for VirtualGuest `%d` has Service Setup transaction complete", virtualGuest.Id)
		}
	} else {
		err = bslcommon.AttachEphemeralDiskToVirtualGuest(c.softLayerClient, virtualGuest.Id, cloudProps.EphemeralDiskSize, c.logger)
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapError(err, fmt.Sprintf("Attaching ephemeral disk to VirtualGuest `%d`", virtualGuest.Id))
		}
	}

	virtualGuest, err = bslcommon.GetObjectDetailsOnVirtualGuest(c.softLayerClient, virtualGuest.Id)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapErrorf(err, "Cannot get details from virtual guest with id: %d.", virtualGuest.Id)
	}

	softlayerFileService := NewSoftlayerFileService(c.sshClient, virtualGuest, c.logger)
	agentEnvService := c.agentEnvServiceFactory.New(softlayerFileService, strconv.Itoa(virtualGuest.Id))

	if len(cloudProps.BoshIp) == 0 {
		// update /etc/hosts file of bosh-init vm
		c.updateEtcHostsOfBoshInit(fmt.Sprintf("%s  %s", virtualGuest.PrimaryBackendIpAddress, virtualGuest.FullyQualifiedDomainName))
		// Update mbus url setting for bosh director: construct mbus url with new director ip
		mbus, err := c.parseMbusURL(c.agentOptions.Mbus, virtualGuest.PrimaryBackendIpAddress)
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
		}
		c.agentOptions.Mbus = mbus
	} else {
		// Update mbus url setting
		mbus, err := c.parseMbusURL(c.agentOptions.Mbus, cloudProps.BoshIp)
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapErrorf(err, "Cannot construct mbus url.")
		}
		c.agentOptions.Mbus = mbus
		// Update blobstore setting
		switch c.agentOptions.Blobstore.Provider {
		case BlobstoreTypeDav:
			davConf := DavConfig(c.agentOptions.Blobstore.Options)
			c.updateDavConfig(&davConf, cloudProps.BoshIp)
		}
	}

	ubuntu := Ubuntu{
		SoftLayerClient: c.softLayerClient.GetHttpClient(),
		SSHClient: &sshClientWrapper{
			client:   c.sshClient,
			ip:       virtualGuest.PrimaryBackendIpAddress,
			user:     ROOT_USER_NAME,
			password: vm.getRootPassword(virtualGuest),
		},
		SoftLayerFileService: softlayerFileService,
	}

	err = ubuntu.ConfigureNetwork(networks, virtualGuest.Id)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapErrorf(err, "Failed to configure networking for virtual guest with id: %d.", virtualGuest.Id)
	}

	agentEnv := CreateAgentUserData(agentID, cloudProps, networks, env, c.agentOptions)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapErrorf(err, "Cannot agent env for virtual guest with id: %d.", virtualGuest.Id)
	}

	err = agentEnvService.Update(agentEnv)
	if err != nil {
		return SoftLayerVM{}, bosherr.WrapError(err, "Updating VM's agent env")
	}

	if len(c.agentOptions.VcapPassword) > 0 {
		err = c.SetVcapPassword(vm, virtualGuest, c.agentOptions.VcapPassword)
		if err != nil {
			return SoftLayerVM{}, bosherr.WrapError(err, "Updating VM's vcap password")
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

func (c SoftLayerCreator) SetVcapPassword(vm SoftLayerVM, virtualGuest datatypes.SoftLayer_Virtual_Guest, encryptedPwd string) (err error) {
	command := fmt.Sprintf("usermod -p '%s' vcap", c.agentOptions.VcapPassword)
	_, err = vm.sshClient.ExecCommand(ROOT_USER_NAME, vm.getRootPassword(virtualGuest), virtualGuest.PrimaryBackendIpAddress, command)
	if err != nil {
		return bosherr.WrapError(err, "Shelling out to usermod vcap")
	}
	return
}
