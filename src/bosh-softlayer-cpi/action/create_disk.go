package action

import (
	bosherr "github.com/bluebosh/bosh-utils/errors"

	"bosh-softlayer-cpi/api"
	"bosh-softlayer-cpi/softlayer/disk_service"
	instance "bosh-softlayer-cpi/softlayer/virtual_guest_service"
)

type CreateDisk struct {
	diskService disk.Service
	vmService   instance.Service
}

func NewCreateDisk(
	diskService disk.Service,
	vmService instance.Service,
) CreateDisk {
	return CreateDisk{
		diskService: diskService,
		vmService:   vmService,
	}
}

func (cd CreateDisk) Run(size int, cloudProps DiskCloudProperties, vmCID VMCID) (string, error) {
	// Find the VM (if provided) so we can create the disk in the same datacenter
	var location string
	if vmCID != 0 {
		vm, err := cd.vmService.Find(vmCID.Int())
		if err != nil {
			if _, ok := err.(api.CloudError); ok {
				return "", err
			}
			return "", bosherr.WrapErrorf(err, "Creating disk with size '%d'", size)
		}
		location = *vm.Datacenter.Name
	} else {
		if len(cloudProps.DataCenter) > 0 {
			location = cloudProps.DataCenter
		} else {
			return "", bosherr.Errorf("Creating disk with size '%d': Invalid datacenter name specified.", size)
		}
	}

	// Create the Disk
	disk, err := cd.diskService.Create(size, cloudProps.Iops, location, cloudProps.SnapshotSpace)
	if err != nil {
		return "", bosherr.WrapErrorf(err, "Creating disk with size '%d'", size)
	}

	return DiskCID(disk).String(), nil
}
