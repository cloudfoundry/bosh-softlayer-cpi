package action

import (
	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	bslcvm "github.com/maximilien/bosh-softlayer-cpi/softlayer/vm"
)

type FindVM struct {
	vmFinder bslcvm.Finder
}

func NewFindVM(vmFinder bslcvm.Finder) FindVM {
	return FindVM{vmFinder: vmFinder}
}

func (a FindVM) Run(vmCID VMCID) (string, string, error) {
	vm, found, err := a.vmFinder.Find(int(vmCID))

	if err != nil || !found {
		return "","", bosherr.WrapErrorf(err, "Finding vm '%s'", vmCID)
	}

	if found {
		virtualGuest, err := vm.FetchVMDetails()
		if err != nil {
			return "", "", bosherr.WrapErrorf(err, "Fetching backend ip of vm '%s'", vmCID)
		}
		return virtualGuest.FullyQualifiedDomainName, virtualGuest.PrimaryBackendIpAddress, nil
	}

	return
}
