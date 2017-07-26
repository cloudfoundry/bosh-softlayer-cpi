package action

import (
	"bosh-softlayer-cpi/api"
	instance "bosh-softlayer-cpi/softlayer/virtual_guest_service"

	"bosh-softlayer-cpi/registry"
)

type ConfigureNetworks struct {
	vmService      instance.Service
	registryClient registry.Client
}

func NewConfigureNetworks(
	vmService instance.Service,
	registryClient registry.Client,
) (action ConfigureNetworks) {
	return ConfigureNetworks{
		vmService:      vmService,
		registryClient: registryClient,
	}
}

func (cn ConfigureNetworks) Run(vmCID VMCID, networks Networks) (interface{}, error) {
	return nil, api.NotSupportedError{}
}
