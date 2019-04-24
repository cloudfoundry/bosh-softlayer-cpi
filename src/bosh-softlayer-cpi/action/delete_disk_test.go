package action_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/softlayer/softlayer-go/datatypes"

	. "bosh-softlayer-cpi/action"
	diskfakes "bosh-softlayer-cpi/softlayer/disk_service/fakes"
)

var _ = Describe("DeleteDisk", func() {
	var (
		err     error
		diskCID DiskCID

		diskService *diskfakes.FakeService

		deleteDisk DeleteDisk
	)

	BeforeEach(func() {
		diskCID = DiskCID(22345678)
		diskService = &diskfakes.FakeService{}
		deleteDisk = NewDeleteDisk(diskService)
	})

	Describe("Run", func() {
		It("deletes the disk", func() {
			_, err = deleteDisk.Run(diskCID)
			Expect(err).NotTo(HaveOccurred())
			Expect(diskService.DeleteCallCount()).To(Equal(1))
		})

		It("returns an error if diskService delete call returns an error", func() {
			diskService.DeleteReturns(errors.New("fake-disk-service-error"))

			_, err = deleteDisk.Run(diskCID)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("fake-disk-service-error"))
			Expect(diskService.DeleteCallCount()).To(Equal(1))
		})
		
		It("return nil if diskService delete call returns an api error", func() {
			diskService.FindReturns(
				&datatypes.Network_Storage{},
				errors.New("fake-disk-service-error"), 
			)

			_, err = deleteDisk.Run(diskCID)
			Expect(err).To(HaveOccurred())
			Expect(diskService.FindCallCount()).To(Equal(1))
		})

	})
})
