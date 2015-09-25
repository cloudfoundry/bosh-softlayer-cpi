package delete_stemcell_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	testhelperscpi "github.com/maximilien/bosh-softlayer-cpi/test_helpers"
	slclient "github.com/maximilien/softlayer-go/client"
	softlayer "github.com/maximilien/softlayer-go/softlayer"
	testhelpers "github.com/maximilien/softlayer-go/test_helpers"
	"log"
)

const configPath = "test_fixtures/cpi_methods/config.json"

var _ = Describe("BOSH Director Level Integration for delete_stemcell", func() {
	var (
		err error

		client softlayer.Client

		username, apiKey string

		accountService softlayer.SoftLayer_Account_Service

		rootTemplatePath, tmpConfigPath string
		replacementMap                  map[string]string

		output map[string]interface{}

		vmId float64
	)

	BeforeEach(func() {
		username = os.Getenv("SL_USERNAME")
		Expect(username).ToNot(Equal(""), "username cannot be empty, set SL_USERNAME")

		apiKey = os.Getenv("SL_API_KEY")
		Expect(apiKey).ToNot(Equal(""), "apiKey cannot be empty, set SL_API_KEY")

		client = slclient.NewSoftLayerClient(username, apiKey)
		Expect(client).ToNot(BeNil())

		accountService, err = testhelpers.CreateAccountService()
		Expect(err).ToNot(HaveOccurred())

		testhelpers.TIMEOUT = 35 * time.Minute
		testhelpers.POLLING_INTERVAL = 10 * time.Second

		pwd, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		rootTemplatePath = filepath.Join(pwd, "..", "..")

		tmpConfigPath, err = testhelperscpi.CreateTmpConfigPath(rootTemplatePath, configPath, username, apiKey)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		err = os.RemoveAll(tmpConfigPath)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("delete_stemcell in SoftLayer", func() {
		It("returns true because valid parameters", func() {
			jsonPayload, err := testhelperscpi.GenerateCpiJsonPayload("delete_stemcell", rootTemplatePath, replacementMap)
			Expect(err).ToNot(HaveOccurred())

			outputBytes, err := testhelperscpi.RunCpi(rootTemplatePath, tmpConfigPath, jsonPayload)
			log.Println("outputBytes=" + string(outputBytes))
			Expect(err).ToNot(HaveOccurred())

			err = json.Unmarshal(outputBytes, &output)
			Expect(err).ToNot(HaveOccurred())
			Expect(output["result"]).ToNot(BeNil())
			Expect(output["error"]).To(BeNil())

			vmId = output["result"].(float64)
			Expect(vmId).ToNot(BeZero())
		})
	})

	FContext("delete_stemcell in SoftLayer", func() {
		It("returns false because empty parameters", func() {
			jsonPayload := `{"method": "delete_stemcell", "arguments": [],"context": {}}`

			outputBytes, err := testhelperscpi.RunCpi(rootTemplatePath, tmpConfigPath, jsonPayload)
			log.Println("outputBytes=" + string(outputBytes))
			Expect(err).ToNot(HaveOccurred())

			err = json.Unmarshal(outputBytes, &output)
			Expect(err).ToNot(HaveOccurred())
			Expect(output["result"]).To(BeNil())
			Expect(output["error"]).ToNot(BeNil())
		})
	})
})