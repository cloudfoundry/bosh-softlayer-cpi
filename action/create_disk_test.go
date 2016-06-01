package action_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/cloudfoundry/bosh-softlayer-cpi/action"

	fakedisk "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/disk/fakes"
	fakevm "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/vm/fakes"

	bslcdisk "github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/disk"
)

var _ = Describe("CreateDisk", func() {
	var (
		vmFinder    *fakevm.FakeFinder
		diskCreator *fakedisk.FakeCreator
		action      CreateDisk
	)

	BeforeEach(func() {
		vmFinder = &fakevm.FakeFinder{}
		diskCreator = &fakedisk.FakeCreator{}
		action = NewCreateDisk(vmFinder, diskCreator)
	})

	Describe("Run", func() {
		var (
			diskCloudProp bslcdisk.DiskCloudProperties
		)

		BeforeEach(func() {
			diskCloudProp = bslcdisk.DiskCloudProperties{}
		})

		It("returns id for created disk for specific size", func() {
			vmFinder.FindFound = true
			vmFinder.FindVM = fakevm.NewFakeVM(1234)

			diskCreator.CreateDisk = fakedisk.NewFakeDisk(1234)

			id, err := action.Run(20, diskCloudProp, VMCID(1234))
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal(DiskCID(1234).String()))

			Expect(diskCreator.CreateSize).To(Equal(20))
		})

		It("returns error if creating disk fails", func() {
			vmFinder.FindFound = true
			vmFinder.FindVM = fakevm.NewFakeVM(1234)

			diskCreator.CreateErr = errors.New("fake-create-err")

			id, err := action.Run(20, diskCloudProp, VMCID(1234))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("fake-create-err"))
			Expect(id).To(Equal(DiskCID(0).String()))
		})
	})
})
