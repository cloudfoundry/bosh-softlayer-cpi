package instance_test

import (
	"errors"
	"fmt"
	"regexp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	boshlog "github.com/cloudfoundry/bosh-utils/logger"
	fakeuuid "github.com/cloudfoundry/bosh-utils/uuid/fakes"

	cpiLog "bosh-softlayer-cpi/logger"
	fakeslclient "bosh-softlayer-cpi/softlayer/client/fakes"
	. "bosh-softlayer-cpi/softlayer/virtual_guest_service"
)

var _ = Describe("Virtual Guest Service", func() {
	var (
		cli                 *fakeslclient.FakeClient
		uuidGen             *fakeuuid.FakeGenerator
		logger              cpiLog.Logger
		virtualGuestService SoftlayerVirtualGuestService
	)

	BeforeEach(func() {
		cli = &fakeslclient.FakeClient{}
		uuidGen = &fakeuuid.FakeGenerator{}
		logger = cpiLog.NewLogger(boshlog.LevelDebug, "")
		virtualGuestService = NewSoftLayerVirtualGuestService(cli, uuidGen, logger)
	})

	Describe("Call SetMetadata", func() {
		var (
			vmID     int
			metaData Metadata
		)

		BeforeEach(func() {
			vmID = 12345678
			metaData = Metadata{
				"deployment": "fake=deployment",
				"director":   "fake-director-uuid",
				"compiling":  "fake-compiling",
			}
		})

		It("Set tags successfully", func() {
			cli.SetTagsReturns(
				true,
				nil,
			)

			err := virtualGuestService.SetMetadata(vmID, metaData)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.SetTagsCallCount()).To(Equal(1))
		})

		It("Set tags successfully with 'job,index' tag and without 'compiling' tag", func() {
			metaData = Metadata{
				"deployment": "fake=deployment",
				"director":   "fake-director-uuid",
				"job":        "fake-job",
				"index":      "fake-index",
			}

			cli.SetTagsReturns(
				true,
				nil,
			)

			err := virtualGuestService.SetMetadata(vmID, metaData)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.SetTagsCallCount()).To(Equal(1))
		})

		It("Set clean tags successfully if metaData contains invalid characters", func() {
			metaData = Metadata{
				"invalid_string": "invalid_string_+@!",
				"valid_string":   "a-zA-Z0-9 _-.:",
			}

			cli.SetTagsReturns(
				true,
				nil,
			)

			err := virtualGuestService.SetMetadata(vmID, metaData)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.SetTagsCallCount()).To(Equal(1))
			_, tags := cli.SetTagsArgsForCall(0)
			Expect(tags).To(ContainSubstring("a-zA-Z0-9 _-."))
			Expect(tags).NotTo(ContainSubstring("+"))
			Expect(tags).NotTo(ContainSubstring("@"))
			Expect(tags).NotTo(ContainSubstring("!"))
		})

		It("Set converted tags successfully if metaData contains multiple colons", func() {
			metaData = Metadata{
				"created_at": "2020-02-12T02:54:06Z",
			}

			cli.SetTagsReturns(
				true,
				nil,
			)

			err := virtualGuestService.SetMetadata(vmID, metaData)
			Expect(err).NotTo(HaveOccurred())
			Expect(cli.SetTagsCallCount()).To(Equal(1))
			_, tags := cli.SetTagsArgsForCall(0)
			Expect(tags).To(ContainSubstring("2020-02-12T02-54-06Z"))

			rep := regexp.MustCompile(":")
			matches := rep.FindAllStringIndex(tags, -1)
			Expect(len(matches)).To(Equal(1))
		})

		It("Return error if softLayerClient SetTags call returns an error", func() {
			cli.SetTagsReturns(
				false,
				errors.New("fake-client-error"),
			)

			err := virtualGuestService.SetMetadata(vmID, metaData)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("fake-client-error"))
			Expect(cli.SetTagsCallCount()).To(BeNumerically(">", 1))
		})

		It("Return error if softLayerClient SetTags call returns an error", func() {
			cli.SetTagsReturns(
				false,
				nil,
			)

			err := virtualGuestService.SetMetadata(vmID, metaData)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("VM '%d' not found", vmID)))
			Expect(cli.SetTagsCallCount()).To(Equal(1))
		})
	})
})
