package client

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	vpsVm "bosh-softlayer-cpi/softlayer/vps_service/client/vm"

	"bosh-softlayer-cpi/softlayer/vps_service/models"
	"fmt"
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	"github.com/go-openapi/strfmt"
	"github.com/softlayer/softlayer-go/datatypes"
	"github.com/softlayer/softlayer-go/filter"
	"github.com/softlayer/softlayer-go/services"
	"github.com/softlayer/softlayer-go/session"
	"github.com/softlayer/softlayer-go/sl"
)

const (
	INSTANCE_DEFAULT_MASK = "id, globalIdentifier, hostname, hourlyBillingFlag, domain, fullyQualifiedDomainName, status.name, " +
		"powerState.name, activeTransaction, datacenter.name, account.id, " +
		"maxCpu, maxMemory, primaryIpAddress, primaryBackendIpAddress, " +
		"privateNetworkOnlyFlag, dedicatedAccountHostOnlyFlag, createDate, modifyDate, " +
		"billingItem[orderItem.order.userRecord.username, recurringFee], notes, tagReferences.tag.name"

	INSTANCE_DETAIL_MASK = "id, globalIdentifier, hostname, domain, fullyQualifiedDomainName, status.name, " +
		"powerState.name, activeTransaction, datacenter.name, " +
		"operatingSystem[softwareLicense[softwareDescription[name,version]],passwords[username,password]], " +
		" maxCpu, maxMemory, primaryIpAddress, primaryBackendIpAddress, " +
		"privateNetworkOnlyFlag, dedicatedAccountHostOnlyFlag, createDate, modifyDate, " +
		"billingItem[nextInvoiceTotalRecurringAmount, children[nextInvoiceTotalRecurringAmount]], notes, tagReferences.tag.name, networkVlans[id,vlanNumber,networkSpace], " +
		"primaryBackendNetworkComponent[primaryIpAddress, networkVlan[id,name,vlanNumber,primaryRouter], subnets[netmask,networkIdentifier]], primaryNetworkComponent[primaryIpAddress, networkVlan[id,name,vlanNumber,primaryRouter], subnets[netmask,networkIdentifier]]"

	NETWORK_DEFAULT_VLAN = "networkSpace"

	VOLUME_DEFAULT_MASK = "id,username,lunId,capacityGb,bytesUsed,serviceResource.datacenter.name,serviceResourceBackendIpAddress,activeTransactionCount,billingItem.orderItem.order[id,userRecord.username]"

	ALLOWD_HOST_DEFAULT_MASK = "id, name, credential[username, password]"

	VOLUME_DETAIL_MASK = "id,username,password,capacityGb,snapshotCapacityGb,parentVolume.snapshotSizeBytes,storageType.keyName," +
		"serviceResource.datacenter.name,serviceResourceBackendIpAddress,iops,lunId,activeTransactionCount," +
		"activeTransactions.transactionStatus.friendlyName,replicationPartnerCount,replicationStatus," +
		"replicationPartners[id,username,serviceResourceBackendIpAddress,serviceResource.datacenter.name,replicationSchedule.type.keyname]"

	IMAGE_DEFAULT_MASK = "id, name, globalIdentifier, imageType, accountId"

	IMAGE_DETAIL_MASK = "id,globalIdentifier,name,datacenter.name,status.name,transaction.transactionStatus.name,accountId,publicFlag,imageType,flexImageFlag,note,createDate,blockDevicesDiskSpaceTotal,children[blockDevicesDiskSpaceTotal,datacenter.name]"

	EPHEMERAL_DISK_CATEGORY_CODE = "guest_disk1"

	UPGRADE_VIRTUAL_SERVER_ORDER_TYPE = "SoftLayer_Container_Product_Order_Virtual_Guest_Upgrade"

	NETWORK_PERFORMANCE_STORAGE_PACKAGE_ID = 222

	SOFTLAYER_PUBLIC_EXCEPTION                      = "SoftLayer_Exception_Public"
	SOFTLAYER_OBJECTNOTFOUND_EXCEPTION              = "SoftLayer_Exception_ObjectNotFound"
	SOFTLAYER_BLOCKINGOPERATIONINPROGRESS_EXCEPTION = "SoftLayer_Exception_Network_Storage_BlockingOperationInProgress"
	SOFTLAYER_GROUP_ACCESSCONTROLERROR_EXCEPTION    = "SoftLayer_Exception_Network_Storage_Group_AccessControlError"
)

//go:generate counterfeiter -o fakes/fake_client_factory.go . ClientFactory
type ClientFactory interface {
	CreateClient() Client
}

type clientFactory struct {
	slClient *ClientManager
}

func NewClientFactory(slClient *ClientManager) ClientFactory {
	return &clientFactory{slClient}
}

func (factory *clientFactory) CreateClient() Client {
	return factory.slClient
}

func NewSoftLayerClientManager(session *session.Session, vps *vpsVm.Client) *ClientManager {
	return &ClientManager{
		services.GetVirtualGuestService(session),
		services.GetAccountService(session),
		services.GetProductPackageService(session),
		services.GetProductOrderService(session),
		services.GetNetworkStorageService(session),
		services.GetBillingItemService(session),
		services.GetLocationDatacenterService(session),
		services.GetNetworkVlanService(session),
		services.GetVirtualGuestBlockDeviceTemplateGroupService(session),
		services.GetSecuritySshKeyService(session),
		vps,
	}
}

//go:generate counterfeiter -o fakes/fake_client.go . Client
type Client interface {
	CancelInstance(id int) error
	CreateInstance(template *datatypes.Virtual_Guest) (*datatypes.Virtual_Guest, error)
	EditInstance(id int, template *datatypes.Virtual_Guest) (bool, error)
	GetInstance(id int, mask string) (*datatypes.Virtual_Guest, bool, error)
	GetInstanceByPrimaryBackendIpAddress(ip string) (*datatypes.Virtual_Guest, bool, error)
	GetInstanceByPrimaryIpAddress(ip string) (*datatypes.Virtual_Guest, bool, error)
	RebootInstance(id int, soft bool, hard bool) error
	ReloadInstance(id int, stemcellId int, sshKeyIds []int, hostname string, domain string) error
	UpgradeInstanceConfig(id int, cpu int, memory int, network int, privateCPU bool) error
	UpgradeInstance(id int, cpu int, memory int, network int, privateCPU bool, additional_diskSize int) (*datatypes.Container_Product_Order_Receipt, error)
	WaitInstanceUntilReady(id int, until time.Time) error
	WaitInstanceHasActiveTransaction(id int, until time.Time) error
	WaitInstanceHasNoneActiveTransaction(id int, until time.Time) error
	WaitVolumeProvisioningWithOrderId(orderId int, until time.Time) (*datatypes.Network_Storage, error)
	SetTags(id int, tags string) (bool, error)
	AttachSecondDiskToInstance(id int, diskSize int) error
	GetInstanceAllowedHost(id int) (*datatypes.Network_Storage_Allowed_Host, bool, error)
	AuthorizeHostToVolume(instance *datatypes.Virtual_Guest, volumeId int, until time.Time) (bool, error)
	DeauthorizeHostToVolume(instance *datatypes.Virtual_Guest, volumeId int, until time.Time) (bool, error)
	CreateVolume(location string, size int, iops int) (*datatypes.Network_Storage, error)
	OrderBlockVolume(storageType string, location string, size int, iops int) (*datatypes.Container_Product_Order_Receipt, error)
	CancelBlockVolume(volumeId int, reason string, immediate bool) (bool, error)
	GetBlockVolumeDetails(volumeId int, mask string) (*datatypes.Network_Storage, bool, error)
	GetBlockVolumeDetails2(volumeId int, mask string) (datatypes.Network_Storage, bool, error)
	GetNetworkStorageTarget(volumeId int, mask string) (string, bool, error)
	GetImage(imageId int, mask string) (*datatypes.Virtual_Guest_Block_Device_Template_Group, bool, error)
	GetVlan(id int, mask string) (*datatypes.Network_Vlan, bool, error)
	GetAllowedHostCredential(id int) (*datatypes.Network_Storage_Allowed_Host, bool, error)
	GetAllowedNetworkStorage(id int) ([]string, bool, error)
	CreateSshKey(label *string, key *string, fingerPrint *string) (*datatypes.Security_Ssh_Key, error)
	DeleteSshKey(id int) (bool, error)

	CreateInstanceFromVPS(template *datatypes.Virtual_Guest, stemcellID int, sshKeys []int) (*datatypes.Virtual_Guest, error)
	DeleteInstanceFromVPS(id int) error
}

type ClientManager struct {
	VirtualGuestService   services.Virtual_Guest
	AccountService        services.Account
	PackageService        services.Product_Package
	OrderService          services.Product_Order
	StorageService        services.Network_Storage
	BillingService        services.Billing_Item
	LocationService       services.Location_Datacenter
	NetworkVlanService    services.Network_Vlan
	ImageService          services.Virtual_Guest_Block_Device_Template_Group
	SecuritySshKeyService services.Security_Ssh_Key
	vpsService            *vpsVm.Client
}

func (c *ClientManager) GetInstance(id int, mask string) (*datatypes.Virtual_Guest, bool, error) {
	if mask == "" {
		mask = INSTANCE_DEFAULT_MASK
	}
	virtualGuest, err := c.VirtualGuestService.Id(id).Mask(mask).GetObject()
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return &datatypes.Virtual_Guest{}, false, nil
			}
			return &datatypes.Virtual_Guest{}, false, err
		}
	}

	return &virtualGuest, true, err
}

func (c *ClientManager) GetVlan(id int, mask string) (*datatypes.Network_Vlan, bool, error) {
	if mask == "" {
		mask = NETWORK_DEFAULT_VLAN
	}
	vlan, err := c.NetworkVlanService.Id(id).Mask(mask).GetObject()
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return &datatypes.Network_Vlan{}, false, nil
			}
			return &datatypes.Network_Vlan{}, false, err
		}
	}

	return &vlan, true, err
}

func (c *ClientManager) GetInstanceByPrimaryBackendIpAddress(ip string) (*datatypes.Virtual_Guest, bool, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("virtualGuests.primaryBackendIpAddress").Eq(ip))
	virtualguests, err := c.AccountService.Mask(INSTANCE_DEFAULT_MASK).Filter(filters.Build()).GetVirtualGuests()
	if err != nil {
		return &datatypes.Virtual_Guest{}, false, err
	}

	for _, virtualguest := range virtualguests {
		// Return the first instance (it can only be 1 instance with the same primary backend ip addresss)
		return &virtualguest, true, nil
	}

	return &datatypes.Virtual_Guest{}, false, err
}

func (c *ClientManager) GetInstanceByPrimaryIpAddress(ip string) (*datatypes.Virtual_Guest, bool, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("virtualGuests.primaryIpAddress").Eq(ip))
	virtualguests, err := c.AccountService.Mask(INSTANCE_DEFAULT_MASK).Filter(filters.Build()).GetVirtualGuests()
	if err != nil {
		return &datatypes.Virtual_Guest{}, false, err
	}

	for _, virtualguest := range virtualguests {
		// Return the first instance (it can only be 1 instance with the same primary ip addresss)
		return &virtualguest, true, nil
	}

	return &datatypes.Virtual_Guest{}, false, err
}

func (c *ClientManager) GetAllowedHostCredential(id int) (*datatypes.Network_Storage_Allowed_Host, bool, error) {
	allowedHost, err := c.VirtualGuestService.Id(id).Mask(ALLOWD_HOST_DEFAULT_MASK).GetAllowedHost()
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return &datatypes.Network_Storage_Allowed_Host{}, false, nil
			}
			return &datatypes.Network_Storage_Allowed_Host{}, false, err
		}
	}

	return &allowedHost, true, err
}

func (c *ClientManager) GetAllowedNetworkStorage(id int) ([]string, bool, error) {
	var storages = make([]string, 1)
	networkStorages, err := c.VirtualGuestService.Id(id).GetAllowedNetworkStorage()
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return storages, false, nil
			}
			return storages, false, err
		}
	}

	for _, networkStorage := range networkStorages {
		storages = append(storages, strconv.Itoa(*networkStorage.Id))
	}

	return storages, true, err
}

func (c *ClientManager) GetImage(imageId int, mask string) (*datatypes.Virtual_Guest_Block_Device_Template_Group, bool, error) {
	if mask == "" {
		mask = IMAGE_DETAIL_MASK
	}
	image, err := c.ImageService.Id(imageId).Mask(mask).GetObject()
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return &datatypes.Virtual_Guest_Block_Device_Template_Group{}, false, nil
			}
			return &datatypes.Virtual_Guest_Block_Device_Template_Group{}, false, err
		}
	}

	return &image, true, err
}

//Check the virtual server instance is ready for use
//param1: bool, indicate whether the instance is ready
//param2: error, any error may happen when getting the status of the instance
func (c *ClientManager) WaitInstanceUntilReady(id int, until time.Time) error {
	for {
		virtualGuest, found, err := c.GetInstance(id, "id, lastOperatingSystemReload[id,modifyDate], activeTransaction[id,transactionStatus.name], provisionDate, powerState.keyName")
		if err != nil {
			return err
		}
		if !found {
			return bosherr.WrapErrorf(err, "SoftLayer virtual guest '%d' does not exists", id)
		}

		lastReload := virtualGuest.LastOperatingSystemReload
		activeTxn := virtualGuest.ActiveTransaction
		provisionDate := virtualGuest.ProvisionDate

		// if lastReload != nil && lastReload.ModifyDate != nil {
		// 	fmt.Println("lastReload: ", (*lastReload.ModifyDate).Format(time.RFC3339))
		// }
		// if activeTxn != nil && activeTxn.TransactionStatus != nil && activeTxn.TransactionStatus.Name != nil {
		// 	fmt.Println("activeTxn: ", *activeTxn.TransactionStatus.Name)
		// }
		// if provisionDate != nil {
		// 	fmt.Println("provisionDate: ", (*provisionDate).Format(time.RFC3339))
		// }

		reloading := activeTxn != nil && lastReload != nil && *activeTxn.Id == *lastReload.Id
		if provisionDate != nil && !reloading {
			//fmt.Println("power state:", *virtualGuest.PowerState.KeyName)
			if *virtualGuest.PowerState.KeyName == "RUNNING" {
				return nil
			}
		}

		now := time.Now()
		if now.After(until) {
			return bosherr.Errorf("Power on virtual guest with id %d Time Out!", *virtualGuest.Id)
		}

		min := math.Min(float64(10.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *ClientManager) WaitInstanceHasActiveTransaction(id int, until time.Time) error {
	for {
		virtualGuest, found, err := c.GetInstance(id, "id, activeTransaction[id,transactionStatus.name]")
		if err != nil {
			return err
		}
		if !found {
			return bosherr.WrapErrorf(err, "SoftLayer virtual guest '%d' does not exist", id)
		}

		// if activeTxn != nil && activeTxn.TransactionStatus != nil && activeTxn.TransactionStatus.Name != nil {
		// 	fmt.Println("activeTxn: ", *activeTxn.TransactionStatus.Name)
		// }

		if virtualGuest.ActiveTransaction != nil {
			return nil
		}

		now := time.Now()
		if now.After(until) {
			return bosherr.Errorf("Wait instance with id of '%d' has active transaction time out", id)
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *ClientManager) WaitInstanceHasNoneActiveTransaction(id int, until time.Time) error {
	for {
		virtualGuest, found, err := c.GetInstance(id, "id, activeTransaction[id,transactionStatus.name]")
		if err != nil {
			return err
		}
		if !found {
			return bosherr.WrapErrorf(err, "SoftLayer virtual guest '%d' does not exist", id)
		}

		// if activeTxn != nil && activeTxn.TransactionStatus != nil && activeTxn.TransactionStatus.Name != nil {
		// 	fmt.Println("activeTxn: ", *activeTxn.TransactionStatus.Name)
		// }

		if virtualGuest.ActiveTransaction == nil {
			return nil
		}

		now := time.Now()
		if now.After(until) {
			return bosherr.Errorf("Waiting instance with id of '%d' has none active transaction time out", id)
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *ClientManager) CreateInstance(template *datatypes.Virtual_Guest) (*datatypes.Virtual_Guest, error) {
	virtualguest, err := c.VirtualGuestService.CreateObject(template)
	if err != nil {
		return &datatypes.Virtual_Guest{}, bosherr.WrapError(err, "Creating instance")
	}

	until := time.Now().Add(time.Duration(4) * time.Hour)
	if err := c.WaitInstanceUntilReady(*virtualguest.Id, until); err != nil {
		return &datatypes.Virtual_Guest{}, bosherr.WrapError(err, "Waiting until instance is ready")
	}

	return &virtualguest, nil
}

func (c *ClientManager) CreateInstanceFromVPS(template *datatypes.Virtual_Guest, stemcellID int, sshKeys []int) (*datatypes.Virtual_Guest, error) {
	reqFilter := &models.VMFilter{
		CPU:         int32(*template.StartCpus),
		MemoryMb:    int32(*template.MaxMemory),
		PrivateVlan: int32(*template.PrimaryBackendNetworkComponent.NetworkVlan.Id),
		PublicVlan:  int32(*template.PrimaryNetworkComponent.NetworkVlan.Id),
		State:       models.StateFree,
	}
	orderVmResp, err := c.vpsService.OrderVMByFilter(vpsVm.NewOrderVMByFilterParams().WithBody(reqFilter))
	if err != nil {
		_, ok := err.(*vpsVm.OrderVMByFilterNotFound)
		if !ok {
			return &datatypes.Virtual_Guest{}, bosherr.WrapError(err, "Ordering vm from pool")
		} else {
			// From createBySoftlayer implement run in cpi action
			virtualGuest, err := c.CreateInstance(template)
			if err != nil {
				return &datatypes.Virtual_Guest{}, bosherr.WrapError(err, "Creating VirtualGuest from SoftLayer client")
			}

			slPoolVm := &models.VM{
				Cid:         int32(*virtualGuest.Id),
				CPU:         int32(*template.StartCpus),
				MemoryMb:    int32(*template.MaxMemory),
				IP:          strfmt.IPv4(*template.PrimaryBackendIpAddress),
				Hostname:    *virtualGuest.Hostname,
				PrivateVlan: int32(*template.PrimaryBackendNetworkComponent.NetworkVlan.Id),
				PublicVlan:  int32(*template.PrimaryNetworkComponent.NetworkVlan.Id),
				State:       models.StateUsing,
			}
			_, err = c.vpsService.AddVM(vpsVm.NewAddVMParams().WithBody(slPoolVm))
			if err != nil {
				return &datatypes.Virtual_Guest{}, bosherr.WrapError(err, "Adding vm into pool")
			}

			return virtualGuest, nil
		}
	}
	var vm *models.VM
	var virtualGuestId int

	vm = orderVmResp.Payload.VM
	virtualGuestId = int((*vm).Cid)

	err = c.ReloadInstance(virtualGuestId, stemcellID, sshKeys, *template.Hostname, *template.Domain)
	if err != nil {
		return &datatypes.Virtual_Guest{}, bosherr.WrapError(err, "Reloading vm from pool")
	}

	virtualGuest, found, err := c.GetInstance(virtualGuestId, INSTANCE_DEFAULT_MASK)
	if err != nil {
		return &datatypes.Virtual_Guest{}, err
	}
	if !found {
		return &datatypes.Virtual_Guest{}, bosherr.WrapErrorf(err, "SoftLayer virtual guest '%d' does not exists", virtualGuestId)
	}

	deviceName := &models.VM{
		Cid:         int32(virtualGuestId),
		CPU:         int32(*virtualGuest.StartCpus),
		MemoryMb:    int32(*virtualGuest.MaxMemory),
		IP:          strfmt.IPv4(*virtualGuest.PrimaryBackendIpAddress),
		Hostname:    *virtualGuest.Hostname,
		PrivateVlan: int32(*virtualGuest.PrimaryBackendNetworkComponent.NetworkVlan.Id),
		PublicVlan:  int32(*virtualGuest.PrimaryNetworkComponent.NetworkVlan.Id),
		State:       models.StateUsing,
	}
	_, err = c.vpsService.UpdateVM(vpsVm.NewUpdateVMParams().WithBody(deviceName))
	if err != nil {
		return &datatypes.Virtual_Guest{}, bosherr.WrapErrorf(err, "Updating the hostname of vm %d in pool to using", virtualGuestId)
	}

	return virtualGuest, nil
}

func (c *ClientManager) EditInstance(id int, template *datatypes.Virtual_Guest) (bool, error) {
	_, err := c.VirtualGuestService.Id(id).EditObject(template)
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return false, nil
			}
			return false, err
		}
	}

	until := time.Now().Add(time.Duration(30) * time.Minute)
	if err := c.WaitInstanceUntilReady(id, until); err != nil {
		return false, bosherr.WrapError(err, "Waiting until instance is ready")
	}

	return true, err
}

func (c *ClientManager) RebootInstance(id int, soft bool, hard bool) error {
	var err error
	if soft == false && hard == false {
		_, err = c.VirtualGuestService.Id(id).RebootDefault()
	} else if soft == true && hard == false {
		_, err = c.VirtualGuestService.Id(id).RebootSoft()
	} else if soft == false && hard == true {
		_, err = c.VirtualGuestService.Id(id).RebootHard()
	} else {
		err = bosherr.Error("The reboot type is not existing")
	}
	return err
}

func (c *ClientManager) ReloadInstance(id int, stemcellId int, sshKeyIds []int, hostname string, domain string) error {
	var err error
	until := time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasNoneActiveTransaction(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has none active transaction before os_reload")
	}

	config := datatypes.Container_Hardware_Server_Configuration{
		ImageTemplateId: sl.Int(stemcellId),
	}

	if sshKeyIds[0] != 0 {
		config.SshKeyIds = sshKeyIds
	}

	_, err = c.VirtualGuestService.Id(id).ReloadOperatingSystem(sl.String("FORCE"), &config)
	if err != nil {
		return err
	}

	until = time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasActiveTransaction(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has active transaction after launching os_reload")
	}

	until = time.Now().Add(time.Duration(4) * time.Hour)
	if err = c.WaitInstanceUntilReady(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance is ready after os_reload")
	}

	succeed, err := c.EditInstance(id, &datatypes.Virtual_Guest{
		Hostname: sl.String(hostname),
		Domain:   sl.String(domain),
	})

	if err != nil {
		return bosherr.WrapError(err, "Editing VM hostname after OS Reload")
	}

	if !succeed {
		return bosherr.WrapError(err, "Failed to edit VM hostname after OS Reload")
	}

	return nil
}

func (c *ClientManager) CancelInstance(id int) error {
	var err error
	until := time.Now().Add(time.Duration(30) * time.Minute)
	if err = c.WaitInstanceHasNoneActiveTransaction(*sl.Int(id), until); err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return nil
			}
		}
		return bosherr.WrapError(err, "Waiting until instance has none active transaction before canceling")
	}

	resp, err := c.VirtualGuestService.Id(id).DeleteObject()
	if err != nil {
		return bosherr.WrapErrorf(err, "Deleting instance with id '%d'", id)
	}
	if !resp {
		return bosherr.WrapError(err, "Deleting instance with id '%d' failed")
	}

	return nil
}

func (c *ClientManager) DeleteInstanceFromVPS(id int) error {
	_, err := c.vpsService.GetVMByCid(vpsVm.NewGetVMByCidParams().WithCid(int32(id)))
	if err != nil {
		_, ok := err.(*vpsVm.GetVMByCidNotFound)
		if ok {
			virtualGuest, err := c.VirtualGuestService.Id(id).GetObject()
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Getting virtual guest %d details from SoftLayer", id))
			}

			slPoolVm := &models.VM{
				Cid:         int32(id),
				CPU:         int32(*virtualGuest.StartCpus),
				MemoryMb:    int32(*virtualGuest.MaxMemory),
				IP:          strfmt.IPv4(*virtualGuest.PrimaryBackendIpAddress),
				Hostname:    *virtualGuest.FullyQualifiedDomainName,
				PrivateVlan: int32(*virtualGuest.PrimaryBackendNetworkComponent.NetworkVlan.Id),
				PublicVlan:  int32(*virtualGuest.PrimaryNetworkComponent.NetworkVlan.Id),
				State:       models.StateFree,
			}
			_, err = c.vpsService.AddVM(vpsVm.NewAddVMParams().WithBody(slPoolVm))
			if err != nil {
				return bosherr.WrapError(err, fmt.Sprintf("Adding vm %d to pool", id))
			}
			return nil
		}
		return bosherr.WrapError(err, "Removing vm from pool")
	}

	free := models.VMState{
		State: models.StateFree,
	}
	_, err = c.vpsService.UpdateVMWithState(vpsVm.NewUpdateVMWithStateParams().WithBody(&free).WithCid(int32(id)))
	if err != nil {
		return bosherr.WrapErrorf(err, "Updating state of vm %d in pool to free", id)
	}

	return nil
}

func (c *ClientManager) UpgradeInstance(id int, cpu int, memory int, network int, privateCPU bool, additional_diskSize int) (*datatypes.Container_Product_Order_Receipt, error) {
	upgradeOptions := make(map[string]int)
	public := true
	if cpu != 0 {
		upgradeOptions["guest_core"] = cpu
	}
	if memory != 0 {
		upgradeOptions["ram"] = memory / 1024
	}
	if network != 0 {
		upgradeOptions["port_speed"] = network
	}
	if privateCPU == true {
		public = false
	}

	packageType := "VIRTUAL_SERVER_INSTANCE"
	productPackages, err := c.PackageService.
		Mask("id,name,description,isActive,type.keyName").
		Filter(filter.New(filter.Path("type.keyName").Eq(packageType)).Build()).
		GetAllObjects()
	if err != nil {
		return &datatypes.Container_Product_Order_Receipt{}, err
	}
	if len(productPackages) == 0 {
		return &datatypes.Container_Product_Order_Receipt{}, bosherr.Errorf("No package found for type: %s", packageType)
	}
	packageID := *productPackages[0].Id
	packageItems, err := c.PackageService.
		Id(packageID).
		Mask("description,capacity,prices[id,locationGroupId,categories]").
		GetItems()
	if err != nil {
		return &datatypes.Container_Product_Order_Receipt{}, err
	}
	var prices = make([]datatypes.Product_Item_Price, 0)
	for option, value := range upgradeOptions {
		priceID := getPriceIdForUpgrade(packageItems, option, value, public)
		if priceID == -1 {
			return &datatypes.Container_Product_Order_Receipt{},
				bosherr.Errorf("Unable to find %s option with %d", option, value)
		}
		prices = append(prices, datatypes.Product_Item_Price{Id: &priceID})
	}

	if additional_diskSize != 0 {
		diskItemPrice, err := c.getUpgradeItemPriceForSecondDisk(id, additional_diskSize)
		if err != nil {
			return &datatypes.Container_Product_Order_Receipt{}, err
		}
		prices = append(prices, *diskItemPrice)
	}

	if len(prices) == 0 {
		return &datatypes.Container_Product_Order_Receipt{}, bosherr.Error("Unable to find price for upgrade")
	}
	order := datatypes.Container_Product_Order{
		ComplexType: sl.String(UPGRADE_VIRTUAL_SERVER_ORDER_TYPE),
		Prices:      prices,
		Properties: []datatypes.Container_Product_Order_Property{
			{
				Name:  sl.String("MAINTENANCE_WINDOW"),
				Value: sl.String(time.Now().UTC().Format(time.RFC3339)),
			},
			{
				Name:  sl.String("NOTE_GENERAL"),
				Value: sl.String("Upgrade instance configuration."),
			},
		},
		VirtualGuests: []datatypes.Virtual_Guest{
			{
				Id: &id,
			},
		},
		PackageId: &packageID,
	}
	upgradeOrder := datatypes.Container_Product_Order_Virtual_Guest_Upgrade{
		Container_Product_Order_Virtual_Guest: datatypes.Container_Product_Order_Virtual_Guest{
			Container_Product_Order_Hardware_Server: datatypes.Container_Product_Order_Hardware_Server{
				Container_Product_Order: order,
			},
		},
	}
	orderReceipt, err := c.OrderService.PlaceOrder(&upgradeOrder, sl.Bool(false))
	if err != nil {
		return &datatypes.Container_Product_Order_Receipt{}, err
	}

	return &orderReceipt, nil
}

func (c *ClientManager) SetTags(id int, tags string) (bool, error) {
	_, err := c.VirtualGuestService.Id(id).SetTags(&tags)
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return false, nil
			}
			return false, err
		}
	}

	return true, err
}

func (c *ClientManager) GetInstanceAllowedHost(id int) (*datatypes.Network_Storage_Allowed_Host, bool, error) {
	mask := "id, name, credential[username, password]"
	allowedHost, err := c.VirtualGuestService.Id(id).Mask(mask).GetAllowedHost()
	if err != nil {
		return &datatypes.Network_Storage_Allowed_Host{}, false, err
	}

	if allowedHost.Id == nil {
		return &datatypes.Network_Storage_Allowed_Host{}, false, bosherr.Errorf("Unable to get allowed host with instance id: %d", id)
	}

	return &allowedHost, true, nil
}

func (c *ClientManager) getUpgradeItemPriceForSecondDisk(id int, diskSize int) (*datatypes.Product_Item_Price, error) {
	itemPrices, err := c.VirtualGuestService.Id(id).GetUpgradeItemPrices(sl.Bool(true))
	if err != nil {
		return &datatypes.Product_Item_Price{}, err
	}

	var currentDiskCapacity int
	var diskType string
	var currentItemPrice datatypes.Product_Item_Price

	diskTypeBool, err := c.VirtualGuestService.Id(id).GetLocalDiskFlag()
	if err != nil {
		return &datatypes.Product_Item_Price{}, err
	}

	if diskTypeBool {
		diskType = "(LOCAL)"
	} else {
		diskType = "(SAN)"
	}

	for _, itemPrice := range itemPrices {
		flag := false
		for _, category := range itemPrice.Item.Categories {
			if *category.CategoryCode == EPHEMERAL_DISK_CATEGORY_CODE {
				flag = true
				break
			}
		}

		if flag && strings.Contains(*itemPrice.Item.Description, diskType) {
			if int(*itemPrice.Item.Capacity) >= diskSize {
				if currentItemPrice.Id == nil || currentDiskCapacity >= int(*itemPrice.Item.Capacity) {
					currentItemPrice = itemPrice
					currentDiskCapacity = int(*itemPrice.Item.Capacity)
				}
			}
		}
	}

	if currentItemPrice.Id == nil {
		return &datatypes.Product_Item_Price{}, bosherr.Errorf("No proper %s disk for size %d", diskType, diskSize)
	}

	return &currentItemPrice, nil
}

func getPriceIdForUpgrade(packageItems []datatypes.Product_Item, option string, value int, public bool) int {
	for _, item := range packageItems {
		isPrivate := strings.HasPrefix(*item.Description, "Private")
		for _, price := range item.Prices {
			if price.LocationGroupId != nil {
				continue
			}
			if len(price.Categories) == 0 {
				continue
			}
			for _, category := range price.Categories {
				if item.Capacity != nil {
					if !(*category.CategoryCode == option && strconv.FormatFloat(float64(*item.Capacity), 'f', 0, 64) == strconv.Itoa(value)) {
						continue
					}
					if option == "guest_core" {
						if public && !isPrivate {
							return *price.Id
						} else if !public && isPrivate {
							return *price.Id
						}
					} else if option == "port_speed" {
						if strings.Contains(*item.Description, "Public") {
							return *price.Id
						}
					} else {
						return *price.Id
					}
				}
			}
		}
	}
	return -1
}

func (c *ClientManager) GetBlockVolumeDetails(volumeId int, mask string) (*datatypes.Network_Storage, bool, error) {
	if mask == "" {
		mask = VOLUME_DETAIL_MASK
	}
	volume, err := c.StorageService.Id(volumeId).Mask(mask).GetObject()
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return &datatypes.Network_Storage{}, false, nil
			}
			return &datatypes.Network_Storage{}, false, err
		}
	}

	return &volume, true, nil
}

func (c *ClientManager) GetBlockVolumeDetails2(volumeId int, mask string) (datatypes.Network_Storage, bool, error) {
	if mask == "" {
		mask = VOLUME_DETAIL_MASK
	}
	volumes, err := c.AccountService.Mask(mask).Filter(filter.Path("iscsiNetworkStorage.id").Eq(volumeId).Build()).GetIscsiNetworkStorage()
	if err != nil {
		return datatypes.Network_Storage{}, false, err
	}

	if len(volumes) == 0 {
		return datatypes.Network_Storage{}, false, bosherr.Errorf("Could not find volume with id %d", volumeId)
	}

	return volumes[0], true, nil
}

func (c *ClientManager) GetNetworkStorageTarget(volumeId int, mask string) (string, bool, error) {
	if mask == "" {
		mask = VOLUME_DETAIL_MASK
	}
	var targetPortal string
	connectionInfo, err := c.StorageService.Id(volumeId).Mask(mask).GetNetworkConnectionDetails()
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return targetPortal, false, nil
			}
			return targetPortal, false, err
		}
	}

	return *connectionInfo.IpAddress, true, nil
}

func (c *ClientManager) OrderBlockVolume(storageType string, location string, size int, iops int) (*datatypes.Container_Product_Order_Receipt, error) {
	locationId, err := c.GetLocationId(location)
	if err != nil {
		return &datatypes.Container_Product_Order_Receipt{}, bosherr.Error("Invalid datacenter name specified. Please provide the lower case short name (e.g.: dal09)")
	}
	baseTypeName := "SoftLayer_Container_Product_Order_Network_"
	var prices = make([]datatypes.Product_Item_Price, 0)
	productPacakge, err := c.GetPackage(storageType) //PENDING
	if err != nil {
		return &datatypes.Container_Product_Order_Receipt{}, err
	}

	if storageType == "performance_storage_iscsi" {
		complexType := baseTypeName + "PerformanceStorage_Iscsi"
		storagePrice, err := FindPerformancePrice(productPacakge, "performance_storage_iscsi")
		if err != nil {
			return &datatypes.Container_Product_Order_Receipt{}, err
		}
		prices = append(prices, storagePrice)
		spacePrice, err := FindPerformanceSpacePrice(productPacakge, size)
		if err != nil {
			return &datatypes.Container_Product_Order_Receipt{}, err
		}
		prices = append(prices, spacePrice)

		var iopsPrice datatypes.Product_Item_Price
		if iops == 0 {
			switch size {
			case 250:
				iopsPrice, err = c.selectMaximunIopsItemPriceIdOnSize(1000)
			case 500:
				iopsPrice, err = c.selectMaximunIopsItemPriceIdOnSize(1000)
			default:
				iopsPrice, err = c.selectMaximunIopsItemPriceIdOnSize(size)
			}
			if err != nil {
				return &datatypes.Container_Product_Order_Receipt{}, err
			}
		} else {
			iopsPrice, err = FindPerformanceIOPSPrice(productPacakge, size, iops)
			if err != nil {
				return &datatypes.Container_Product_Order_Receipt{}, err
			}
		}
		prices = append(prices, iopsPrice)

		order := datatypes.Container_Product_Order_Network_PerformanceStorage_Iscsi{
			OsFormatType: &datatypes.Network_Storage_Iscsi_OS_Type{
				KeyName: sl.String("LINUX"),
				Id:      sl.Int(12),
			},
			Container_Product_Order_Network_PerformanceStorage: datatypes.Container_Product_Order_Network_PerformanceStorage{
				Container_Product_Order: datatypes.Container_Product_Order{
					ComplexType: sl.String(complexType),
					PackageId:   productPacakge.Id,
					Prices:      prices,
					Quantity:    sl.Int(1),
					Location:    sl.String(strconv.Itoa(locationId)),
				},
			},
		}
		orderReceipt, err := c.OrderService.PlaceOrder(&order, sl.Bool(false))
		if err != nil {
			return &datatypes.Container_Product_Order_Receipt{}, err
		}

		return &orderReceipt, nil
	} else {
		return &datatypes.Container_Product_Order_Receipt{}, bosherr.Error("Block volume storage_type must be either Performance or Endurance")
	}
}

func (c *ClientManager) CreateVolume(location string, size int, iops int) (*datatypes.Network_Storage, error) {
	receipt, err := c.OrderBlockVolume("performance_storage_iscsi", location, size, iops)
	if err != nil {
		return &datatypes.Network_Storage{}, err
	}

	if receipt.OrderId == nil {
		return &datatypes.Network_Storage{}, bosherr.Errorf("No order id returned after placing order with size of '%d', iops of '%d', location of `%s`", size, iops, location)
	}

	until := time.Now().Add(time.Duration(1) * time.Hour)
	return c.WaitVolumeProvisioningWithOrderId(*receipt.OrderId, until)
}

func (c *ClientManager) WaitVolumeProvisioningWithOrderId(orderId int, until time.Time) (*datatypes.Network_Storage, error) {
	for {
		volumes, err := c.getIscsiNetworkStorageWithOrderId(orderId)
		if err != nil {
			return &datatypes.Network_Storage{}, bosherr.WrapErrorf(err, "Getting volumes with order id  of '%d'", orderId)
		}

		// if activeTxn != nil && activeTxn.TransactionStatus != nil && activeTxn.TransactionStatus.Name != nil {
		// 	fmt.Println("activeTxn: ", *activeTxn.TransactionStatus.Name)
		// }

		for _, volume := range volumes {
			return &volume, nil
		}

		now := time.Now()
		if now.After(until) {
			return &datatypes.Network_Storage{}, bosherr.Errorf("Waiting volume provisioning with order id of '%d' has time out", orderId)
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *ClientManager) getIscsiNetworkStorageWithOrderId(orderId int) ([]datatypes.Network_Storage, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("iscsiNetworkStorage.billingItem.orderItem.order.id").Eq(orderId))
	return c.AccountService.Mask(VOLUME_DEFAULT_MASK).Filter(filters.Build()).GetIscsiNetworkStorage()
}

func (c *ClientManager) GetPackage(categoryCode string) (datatypes.Product_Package, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("categories.categoryCode").Eq(categoryCode))
	filters = append(filters, filter.Path("statusCode").Eq("ACTIVE"))
	packages, err := c.PackageService.Mask("id,name,items[prices[categories],attributes]").Filter(filters.Build()).GetAllObjects()
	if err != nil {
		return datatypes.Product_Package{}, err
	}
	if len(packages) == 0 {
		return datatypes.Product_Package{}, bosherr.Errorf("No packages were found for %s ", categoryCode)
	}
	if len(packages) > 1 {
		return datatypes.Product_Package{}, bosherr.Errorf("More than one packages were found for %s", categoryCode)
	}
	return packages[0], nil
}

func (c *ClientManager) GetLocationId(location string) (int, error) {
	reqFilter := filter.New(filter.Path("name").Eq(location))
	datacenters, err := c.LocationService.Mask("longName,id,name").Filter(reqFilter.Build()).GetDatacenters()
	if err != nil {
		return 0, err
	}
	for _, datacenter := range datacenters {
		if *datacenter.Name == location {
			return *datacenter.Id, nil
		}
	}
	return 0, bosherr.Error("Invalid datacenter name specified")
}

func hasCategory(categories []datatypes.Product_Item_Category, categoryCode string) bool {
	for _, category := range categories {
		if *category.CategoryCode == categoryCode {
			return true
		}
	}
	return false
}

//Find the price in the given package that has the specified category
func FindPerformancePrice(productPackage datatypes.Product_Package, priceCategory string) (datatypes.Product_Item_Price, error) {
	for _, item := range productPackage.Items {
		for _, price := range item.Prices {
			// Only collect prices from valid location groups.
			if price.LocationGroupId != nil {
				continue
			}
			if !hasCategory(price.Categories, priceCategory) {
				continue
			}
			return price, nil
		}
	}
	return datatypes.Product_Item_Price{}, bosherr.Error("Unable to find price for performance storage")
}

//Find the price in the given package with the specified size
func FindPerformanceSpacePrice(productPackage datatypes.Product_Package, size int) (datatypes.Product_Item_Price, error) {
	for _, item := range productPackage.Items {
		if int(*item.Capacity) != size {
			continue
		}
		for _, price := range item.Prices {
			// Only collect prices from valid location groups.
			if price.LocationGroupId != nil {
				continue
			}
			if !hasCategory(price.Categories, "performance_storage_space") {
				continue
			}
			return price, nil
		}
	}
	return datatypes.Product_Item_Price{}, bosherr.Error("Unable to find disk space price for the given volume")
}

//Find the price in the given package with the specified size and iops
func FindPerformanceIOPSPrice(productPackage datatypes.Product_Package, size int, iops int) (datatypes.Product_Item_Price, error) {
	for _, item := range productPackage.Items {
		if int(*item.Capacity) != int(iops) {
			continue
		}
		for _, price := range item.Prices {
			// Only collect prices from valid location groups.
			if price.LocationGroupId != nil {
				continue
			}
			if !hasCategory(price.Categories, "performance_storage_iops") {
				continue
			}
			min, err := strconv.Atoi(*price.CapacityRestrictionMinimum)
			if err != nil {
				return datatypes.Product_Item_Price{}, bosherr.Error("Unable to find price for iops for the given volume")
			}
			if size < int(min) {
				continue
			}
			max, err := strconv.Atoi(*price.CapacityRestrictionMaximum)
			if err != nil {
				return datatypes.Product_Item_Price{}, bosherr.Error("Unable to find price for iops for the given volume")
			}
			if size > int(max) {
				continue
			}
			return price, nil
		}
	}
	return datatypes.Product_Item_Price{}, bosherr.Error("Unable to find price for iops for the given volume")
}

func (c *ClientManager) CancelBlockVolume(volumeId int, reason string, immediate bool) (bool, error) {
	blockVolume, found, err := c.GetBlockVolumeDetails2(volumeId, "id,billingItem.id")
	if err != nil {
		return false, err
	}

	if !found {
		return true, nil
	}

	if blockVolume.BillingItem == nil || blockVolume.BillingItem.Id == nil {
		return false, bosherr.Error("No billing item is found to cancel")
	}

	return c.BillingService.Id(*blockVolume.BillingItem.Id).CancelItem(sl.Bool(immediate), sl.Bool(true), sl.String(reason), sl.String(""))
}

func (c *ClientManager) AuthorizeHostToVolume(instance *datatypes.Virtual_Guest, volumeId int, until time.Time) (bool, error) {
	for {
		allowable, err := c.StorageService.Id(volumeId).AllowAccessFromVirtualGuest(instance)
		if err != nil {
			apiErr := err.(sl.Error)
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return false, bosherr.WrapErrorf(err, "Unable to find object with id of '%d'", volumeId)
			}
			if apiErr.Exception == SOFTLAYER_BLOCKINGOPERATIONINPROGRESS_EXCEPTION {
				continue
			}
			if apiErr.Exception == SOFTLAYER_GROUP_ACCESSCONTROLERROR_EXCEPTION &&
				strings.Contains(apiErr.Message, "not yet ready for mount") {
				continue
			}

			return false, err
		}

		if allowable {
			return allowable, nil
		}

		now := time.Now()
		if now.After(until) {
			return false, bosherr.Errorf("Authorizing instance with id '%d' to volume with id '%d' time out after %v", *instance.Id, volumeId, until.String())
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *ClientManager) DeauthorizeHostToVolume(instance *datatypes.Virtual_Guest, volumeId int, until time.Time) (bool, error) {
	for {
		disAllowed, err := c.StorageService.Id(volumeId).RemoveAccessFromVirtualGuest(instance)
		if err != nil {
			apiErr := err.(sl.Error)
			if apiErr.Exception == SOFTLAYER_OBJECTNOTFOUND_EXCEPTION {
				return false, bosherr.Errorf("Unable to find object with id of '%d'", volumeId)
			}
			if apiErr.Exception == SOFTLAYER_BLOCKINGOPERATIONINPROGRESS_EXCEPTION {
				continue
			}
			return false, err
		}

		if disAllowed {
			return disAllowed, nil
		}

		now := time.Now()
		if now.After(until) {
			return false, bosherr.Errorf("De-Authorizing instance with id '%d' to volume with id '%d' time out after %v", *instance.Id, volumeId, until.String())
		}

		min := math.Min(float64(5.0), float64(until.Sub(now)))
		time.Sleep(time.Duration(min) * time.Second)
	}
}

func (c *ClientManager) AttachSecondDiskToInstance(id int, diskSize int) error {
	var err error
	until := time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasNoneActiveTransaction(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has none active transaction before os_reload")
	}

	_, err = c.UpgradeInstance(id, 0, 0, 0, false, diskSize)
	if err != nil {
		apiErr := err.(sl.Error)
		if strings.Contains(apiErr.Message, "A current price was provided for the upgrade order") {
			return nil
		}
		return bosherr.WrapErrorf(err, "Adding second disk with size '%d' to virutal guest of id '%d'", diskSize, id)
	}

	until = time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasActiveTransaction(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has active transaction after upgrading instance")
	}

	until = time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasNoneActiveTransaction(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has none active transaction after upgrading instance")
	}

	until = time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceUntilReady(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance is ready after os_reload")
	}

	return nil
}

func (c *ClientManager) UpgradeInstanceConfig(id int, cpu int, memory int, network int, privateCPU bool) error {
	var err error
	until := time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasNoneActiveTransaction(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has none active transaction before os_reload")
	}

	_, err = c.UpgradeInstance(id, cpu, memory, network, privateCPU, 0)
	if err != nil {
		apiErr := err.(sl.Error)
		if strings.Contains(apiErr.Message, "A current price was provided for the upgrade order") {
			return nil
		}
		return bosherr.WrapErrorf(err, "Upgrading configuration to virutal guest of  id '%d'", id)
	}

	until = time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasActiveTransaction(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has active transaction after upgrading instance")
	}

	until = time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceHasNoneActiveTransaction(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance has none active transaction after upgrading instance")
	}

	until = time.Now().Add(time.Duration(1) * time.Hour)
	if err = c.WaitInstanceUntilReady(*sl.Int(id), until); err != nil {
		return bosherr.WrapError(err, "Waiting until instance is ready after os_reload")
	}

	return nil
}

func (c *ClientManager) CreateSshKey(label *string, key *string, fingerPrint *string) (*datatypes.Security_Ssh_Key, error) {
	var err error

	templateObject := &datatypes.Security_Ssh_Key{
		Label:       label,
		Key:         key,
		Fingerprint: fingerPrint,
	}

	sshKey, err := c.SecuritySshKeyService.CreateObject(templateObject)
	if err != nil {
		if apiErr, ok := err.(sl.Error); ok {
			if apiErr.Exception == SOFTLAYER_PUBLIC_EXCEPTION && strings.Contains(apiErr.Message, "SSH key already exists") {
				sshKeys, err := c.AccountService.Mask("id, key").Filter(filter.Path("sshKeys.key").Eq(*key).Build()).GetSshKeys()
				if err != nil {
					return &datatypes.Security_Ssh_Key{}, err
				}
				for _, sshKey := range sshKeys {
					return &sshKey, nil
				}
			}
		}
	}

	return &sshKey, err
}

func (c *ClientManager) DeleteSshKey(id int) (bool, error) {
	return c.SecuritySshKeyService.Id(id).DeleteObject()
}

func (c *ClientManager) selectMaximunIopsItemPriceIdOnSize(size int) (datatypes.Product_Item_Price, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("itemPrices.attributes.value").Eq(size))
	filters = append(filters, filter.Path("categories.categoryCode").Eq("performance_storage_iops"))

	itemPrices, err := c.PackageService.Id(NETWORK_PERFORMANCE_STORAGE_PACKAGE_ID).Filter(filters.Build()).GetItemPrices()
	if err != nil {
		return datatypes.Product_Item_Price{}, err
	}

	if len(itemPrices) > 0 {
		candidates := itemsFilter(itemPrices, func(itemPrice datatypes.Product_Item_Price) bool {
			return itemPrice.LocationGroupId == nil
		})
		if len(candidates) > 0 {
			sort.Sort(Product_Item_Price_Sorted_Data(candidates))
			return candidates[len(candidates)-1], nil
		} else {
			return datatypes.Product_Item_Price{}, bosherr.Errorf("No proper performance storage (iSCSI volume) for size %d", size)
		}
	}

	return datatypes.Product_Item_Price{}, bosherr.Errorf("No proper performance storage (iSCSI volume)for size %d", size)
}

func (c *ClientManager) selectMediumIopsItemPriceIdOnSize(size int) (datatypes.Product_Item_Price, error) {
	filters := filter.New()
	filters = append(filters, filter.Path("itemPrices.attributes.value").Eq(size))
	filters = append(filters, filter.Path("categories.categoryCode").Eq("performance_storage_iops"))

	itemPrices, err := c.PackageService.Id(NETWORK_PERFORMANCE_STORAGE_PACKAGE_ID).Filter(filters.Build()).GetItemPrices()
	if err != nil {
		return datatypes.Product_Item_Price{}, err
	}

	if len(itemPrices) > 0 {
		candidates := itemsFilter(itemPrices, func(itemPrice datatypes.Product_Item_Price) bool {
			return itemPrice.LocationGroupId == nil
		})
		if len(candidates) > 0 {
			sort.Sort(Product_Item_Price_Sorted_Data(candidates))
			return candidates[len(candidates)/2], nil
		} else {
			return datatypes.Product_Item_Price{}, bosherr.Errorf("No proper performance storage (iSCSI volume) for size %d", size)
		}
	}

	return datatypes.Product_Item_Price{}, bosherr.Errorf("No proper performance storage (iSCSI volume)for size %d", size)
}

func itemsFilter(vs []datatypes.Product_Item_Price, f func(datatypes.Product_Item_Price) bool) []datatypes.Product_Item_Price {
	vsf := make([]datatypes.Product_Item_Price, 0)
	for _, v := range vs {
		if f(v) {
			vsf = append(vsf, v)
		}
	}

	return vsf
}

type Product_Item_Price_Sorted_Data []datatypes.Product_Item_Price

func (sorted_data Product_Item_Price_Sorted_Data) Len() int {
	return len(sorted_data)
}

func (sorted_data Product_Item_Price_Sorted_Data) Swap(i, j int) {
	sorted_data[i], sorted_data[j] = sorted_data[j], sorted_data[i]
}

func (sorted_data Product_Item_Price_Sorted_Data) Less(i, j int) bool {
	return *sorted_data[i].Item.Capacity < *sorted_data[j].Item.Capacity
}
