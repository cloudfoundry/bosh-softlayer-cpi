package action_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "bosh-softlayer-cpi/action"

	diskfakes "bosh-softlayer-cpi/softlayer/disk_service/fakes"
	instancefakes "bosh-softlayer-cpi/softlayer/virtual_guest_service/fakes"
	"github.com/softlayer/softlayer-go/datatypes"
	"github.com/softlayer/softlayer-go/sl"
	"bosh-softlayer-cpi/api"
)

var _ = Describe("CreateDisk", func() {
	var (
		err     error
		diskCID string

		diskService *diskfakes.FakeService
		vmService   *instancefakes.FakeService
		createDisk  CreateDisk
	)
	BeforeEach(func() {
		diskService = &diskfakes.FakeService{}
		vmService = &instancefakes.FakeService{}
		createDisk = NewCreateDisk(diskService, vmService)
	})

	Describe("Run", func() {
		var (
			size       int
			cloudProps DiskCloudProperties
			vmCID      VMCID
		)
		BeforeEach(func() {
			size = 32768
			cloudProps = DiskCloudProperties{}
			vmCID = VMCID(0)
		})

		It("creates the disk", func() {
			diskService.CreateReturns(
				22345678,
				nil,
			)

			diskCID, err = createDisk.Run(size, cloudProps, vmCID)
			Expect(err).NotTo(HaveOccurred())
			Expect(vmService.FindCallCount()).To(Equal(0))
			Expect(diskService.CreateCallCount()).To(Equal(1))
			actualSize, _, _ := diskService.CreateArgsForCall(0)
			Expect(actualSize).To(Equal(32768))
			Expect(diskCID).To(Equal("22345678"))
		})

		It("returns an error if diskService create call returns an error", func() {
			diskService.CreateReturns(
				0,
				errors.New("fake-disk-service-error"),
			)

			_, err = createDisk.Run(32768, cloudProps, vmCID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("fake-disk-service-error"))
			Expect(vmService.FindCallCount()).To(Equal(0))
			Expect(diskService.CreateCallCount()).To(Equal(1))
		})

		Context("when vmCID is set", func() {
			BeforeEach(func() {
				vmCID = VMCID(12345678)
				cloudProps = DiskCloudProperties{
					DataCenter: "fake-datacenter-name",
				}
			})

			It("creates the disk at the vm zone", func() {
				diskService.CreateReturns(
					22345678,
					nil,
				)
				vmService.FindReturns(
					datatypes.Virtual_Guest{
						Id: sl.Int(1234567),
						Datacenter: &datatypes.Location{
							Name: sl.String("fake-datacenter-name"),
						},
					},
					true,
					nil,
				)

				diskCID, err = createDisk.Run(32768, cloudProps, vmCID)
				Expect(err).NotTo(HaveOccurred())
				Expect(vmService.FindCallCount()).To(Equal(1))
				actualCid := vmService.FindArgsForCall(0)
				Expect(actualCid).To(Equal(12345678))
				Expect(diskService.CreateCallCount()).To(Equal(1))
				actualSize, _, actualLocation := diskService.CreateArgsForCall(0)
				Expect(actualSize).To(Equal(32768))
				Expect(actualLocation).To(Equal("fake-datacenter-name"))
				Expect(diskCID).To(Equal("22345678"))
			})

			It("returns an error if vmService find call returns an error", func() {
				vmService.FindReturns(
					datatypes.Virtual_Guest{
					},
					false,
					errors.New("fake-instance-service-error"),
				)

				_, err = createDisk.Run(32768, cloudProps, vmCID)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("fake-instance-service-error"))
				Expect(vmService.FindCallCount()).To(Equal(1))
				Expect(diskService.CreateCallCount()).To(Equal(0))
			})

			It("returns an error if instance is not found", func() {
				vmService.FindReturns(
					datatypes.Virtual_Guest{
					},
					false,
					nil,
				)

				_, err = createDisk.Run(32768, cloudProps, vmCID)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(api.NewVMNotFoundError(vmCID.String()).Error()))
				Expect(vmService.FindCallCount()).To(Equal(1))
				Expect(diskService.CreateCallCount()).To(Equal(0))
			})
		})
	})
})
