package vm_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/maximilien/bosh-softlayer-cpi/softlayer/vm"

	boshlog "github.com/cloudfoundry/bosh-agent/logger"
	common "github.com/maximilien/bosh-softlayer-cpi/common"
	disk "github.com/maximilien/bosh-softlayer-cpi/softlayer/disk"

	fakedisk "github.com/maximilien/bosh-softlayer-cpi/softlayer/disk/fakes"
	fakevm "github.com/maximilien/bosh-softlayer-cpi/softlayer/vm/fakes"
	fakeslclient "github.com/maximilien/softlayer-go/client/fakes"
)

var _ = Describe("SoftLayerVM", func() {
	var (
		softLayerClient *fakeslclient.FakeSoftLayerClient
		agentEnvService *fakevm.FakeAgentEnvService
		logger          boshlog.Logger
		vm              SoftLayerVM
	)

	BeforeEach(func() {
		softLayerClient = fakeslclient.NewFakeSoftLayerClient("fake-username", "fake-api-key")

		agentEnvService = &fakevm.FakeAgentEnvService{}
		logger = boshlog.NewLogger(boshlog.LevelNone)

		vm = NewSoftLayerVM(1234, softLayerClient, agentEnvService, logger)
	})

	Describe("Delete", func() {
		Context("valid VM ID is used", func() {
			BeforeEach(func() {
				softLayerClient.DoRawHttpRequestResponse = []byte("true")
				vm = NewSoftLayerVM(1234567, softLayerClient, agentEnvService, logger)
			})

			It("deletes the VM successfully", func() {
				err := vm.Delete()
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("invalid VM ID is used", func() {
			BeforeEach(func() {
				softLayerClient.DoRawHttpRequestResponse = []byte("false")
				vm = NewSoftLayerVM(00000, softLayerClient, agentEnvService, logger)
			})

			It("fails deleting the VM", func() {
				err := vm.Delete()
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("Reboot", func() {
		Context("valid VM ID is used", func() {
			BeforeEach(func() {
				softLayerClient.DoRawHttpRequestResponse = []byte("true")
				vm = NewSoftLayerVM(1234567, softLayerClient, agentEnvService, logger)
			})

			It("reboots the VM successfully", func() {
				err := vm.Reboot()
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("invalid VM ID is used", func() {
			BeforeEach(func() {
				softLayerClient.DoRawHttpRequestResponse = []byte("false")
				vm = NewSoftLayerVM(00000, softLayerClient, agentEnvService, logger)
			})

			It("fails rebooting the VM", func() {
				err := vm.Reboot()
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("SetMetadata", func() {
		var (
			metadata VMMetadata
		)

		Context("valid VM ID is used", func() {
			BeforeEach(func() {
				fileNames := []string{
					"SoftLayer_Virtual_Guest_Service_getPowerState.json",
					"SoftLayer_Virtual_Guest_Service_getActiveTransactions.json",

					"SoftLayer_Virtual_Guest_Service_setMetadata.json",
					"SoftLayer_Virtual_Guest_Service_configureMetadataDisk.json",

					"SoftLayer_Virtual_Guest_Service_getPowerState.json",
				}
				common.SetTestFixturesForFakeSoftLayerClient(softLayerClient, fileNames)

				metadata = VMMetadata{}
				vm = NewSoftLayerVM(1234567, softLayerClient, agentEnvService, logger)
			})

			It("sets the vm metadata successfully", func() {
				err := vm.SetMetadata(metadata)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("invalid VM ID is used", func() {
			BeforeEach(func() {
				fileNames := []string{
					"SoftLayer_Virtual_Guest_Service_getPowerState.json",
					"SoftLayer_Virtual_Guest_Service_getActiveTransactions.json",

					"SoftLayer_Virtual_Guest_Service_setMetadata_false.json",
					"SoftLayer_Virtual_Guest_Service_configureMetadataDisk.json",

					"SoftLayer_Virtual_Guest_Service_getPowerState.json",
				}
				common.SetTestFixturesForFakeSoftLayerClient(softLayerClient, fileNames)

				metadata = VMMetadata{}
				vm = NewSoftLayerVM(00000, softLayerClient, agentEnvService, logger)
			})

			It("fails setting the vm metadata", func() {
				err := vm.SetMetadata(metadata)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("ConfigureNetworks", func() {
		var (
			networks Networks
		)

		BeforeEach(func() {
			networks = Networks{}
			vm = NewSoftLayerVM(1234567, softLayerClient, agentEnvService, logger)
		})

		It("returns NotSupportedError", func() {
			err := vm.ConfigureNetworks(networks)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("Not supported"))
			Expect(err.(NotSupportedError).Type()).To(Equal("Bosh::Clouds::NotSupported"))
		})
	})

	Describe("#AttachDisk", func() {
		var (
			disk disk.Disk
		)

		BeforeEach(func() {
			disk = fakedisk.NewFakeDisk(1234)
			vm = NewSoftLayerVM(1234567, softLayerClient, agentEnvService, logger)
			fileNames := []string{
				"SoftLayer_Virtual_Guest_Service_getObject.json",
				"SoftLayer_Network_Storage_Service_getIscsiVolume.json",
			}
			common.SetTestFixturesForFakeSoftLayerClient(softLayerClient, fileNames)
		})

		It("attaches the iSCSI volume successfully", func() {
			softLayerClient.ExecShellCommandResult = "fake-user\nfake-devicename"

			err := vm.AttachDisk(disk)
			Expect(err).ToNot(HaveOccurred())
		})

		It("reports error when failed to attach the iSCSI volume", func() {
			softLayerClient.ExecShellCommandError = errors.New("fake-error")

			err := vm.AttachDisk(disk)
			Expect(err).To(HaveOccurred())
		})
	})

	XDescribe("#DetachDisk", func() {

	})
})
