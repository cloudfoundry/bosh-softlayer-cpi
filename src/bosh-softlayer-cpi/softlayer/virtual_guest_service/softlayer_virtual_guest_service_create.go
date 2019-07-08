package instance

import (
	"fmt"
	"strings"

	bosherr "github.com/bluebosh/bosh-utils/errors"
	"github.com/softlayer/softlayer-go/datatypes"

	"bosh-softlayer-cpi/api"
	"bosh-softlayer-cpi/registry"
)

func (vg SoftlayerVirtualGuestService) Create(virtualGuest *datatypes.Virtual_Guest, enableVps bool, stemcellID int, sshKeys []int, userData *registry.SoftlayerUserData) (int, error) {
	var err error

	if enableVps {
		virtualGuest, err = vg.softlayerClient.CreateInstanceFromVPS(virtualGuest, stemcellID, sshKeys, userData)
	} else {
		virtualGuest, err = vg.softlayerClient.CreateInstance(virtualGuest, userData)
	}
	if err != nil {
		if strings.Contains(err.Error(), "Time Out") {
			return 0, api.NewVMCreationFailedError(err.Error(), true)
		}
		return 0, api.NewVMCreationFailedError(err.Error(), false)
	}

	return *virtualGuest.Id, nil
}

func (vg SoftlayerVirtualGuestService) CleanUp(id int) error {
	if err := vg.Delete(id, false); err != nil {
		vg.logger.Debug(softlayerVirtualGuestServiceLogTag, "Failed cleaning up Softlayer VirtualGuest '%s': %d", id, err)
		return bosherr.WrapError(err, fmt.Sprintf("Failed cleaning up Softlayer VirtualGuest '%d'", id))
	} else {
		return nil
	}
}
