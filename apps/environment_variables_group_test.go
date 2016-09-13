package apps

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path"
	"strconv"
	"time"

	. "github.com/cloudfoundry/cf-acceptance-tests/cats_suite_helpers"

	"github.com/cloudfoundry-incubator/cf-test-helpers/cf"
	"github.com/cloudfoundry-incubator/cf-test-helpers/helpers"
	"github.com/cloudfoundry-incubator/cf-test-helpers/workflowhelpers"
	"github.com/cloudfoundry/cf-acceptance-tests/helpers/app_helpers"
	"github.com/cloudfoundry/cf-acceptance-tests/helpers/assets"
	"github.com/cloudfoundry/cf-acceptance-tests/helpers/random_name"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"

	archive_helpers "code.cloudfoundry.org/archiver/extractor/test_helper"
)

var _ = AppsDescribe("Environment Variables Groups", func() {
	var createBuildpack = func(envVarName string) string {
		tmpPath, err := ioutil.TempDir("", "env-group-staging")
		Expect(err).ToNot(HaveOccurred())

		buildpackArchivePath := path.Join(tmpPath, "buildpack.zip")

		archive_helpers.CreateZipArchive(buildpackArchivePath, []archive_helpers.ArchiveFile{
			{
				Name: "bin/compile",
				Body: fmt.Sprintf(`#!/usr/bin/env bash
sleep 5
echo $%s
exit 1
`, envVarName),
			},
			{
				Name: "bin/detect",
				Body: `#!/bin/bash
exit 1
`,
			},
			{
				Name: "bin/release",
				Body: `#!/usr/bin/env bash
exit 1
`,
			},
		})

		return buildpackArchivePath
	}

	var fetchEnvironmentVariables = func(groupType string) map[string]string {
		var session *Session
		workflowhelpers.AsUser(context.AdminUserContext(), DEFAULT_TIMEOUT, func() {
			session = cf.Cf("curl", fmt.Sprintf("/v2/config/environment_variable_groups/%s", groupType)).Wait(DEFAULT_TIMEOUT)
			Expect(session).To(Exit(0))
		})

		var envMap map[string]string
		err := json.Unmarshal(session.Out.Contents(), &envMap)
		Expect(err).NotTo(HaveOccurred())

		return envMap
	}

	var marshalUpdatedEnv = func(envMap map[string]string) []byte {
		jsonObj, err := json.Marshal(envMap)
		Expect(err).NotTo(HaveOccurred())
		return jsonObj
	}

	var extendEnv = func(groupType, envVarName, envVarValue string) {
		envMap := fetchEnvironmentVariables(groupType)
		envMap[envVarName] = envVarValue
		jsonObj := marshalUpdatedEnv(envMap)

		command := fmt.Sprintf("set-%s-environment-variable-group", groupType)
		Expect(cf.Cf(command, string(jsonObj)).Wait(DEFAULT_TIMEOUT)).To(Exit(0))
	}

	var revertExtendedEnv = func(groupType, envVarName string) {
		envMap := fetchEnvironmentVariables(groupType)
		delete(envMap, envVarName)
		jsonObj := marshalUpdatedEnv(envMap)

		apiUrl := fmt.Sprintf("/v2/config/environment_variable_groups/%s", groupType)
		Expect(cf.Cf("curl", apiUrl, "-X", "PUT", "-d", string(jsonObj)).Wait(DEFAULT_TIMEOUT)).To(Exit(0))
	}

	Context("Staging environment variable groups", func() {
		var appName string
		var buildpackName string
		var envVarName string

		BeforeEach(func() {
			appName = random_name.CATSRandomName("APP")
			envVarName = fmt.Sprintf("CATS_STAGING_TEST_VAR_%s", strconv.Itoa(int(time.Now().UnixNano())))
		})

		AfterEach(func() {
			app_helpers.AppReport(appName, DEFAULT_TIMEOUT)

			workflowhelpers.AsUser(context.AdminUserContext(), DEFAULT_TIMEOUT, func() {
				revertExtendedEnv("staging", envVarName)
				if buildpackName != "" {
					Expect(cf.Cf("delete-buildpack", buildpackName, "-f").Wait(DEFAULT_TIMEOUT)).To(Exit(0))
				}
			})

			Expect(cf.Cf("delete", appName, "-f", "-r").Wait(CF_PUSH_TIMEOUT)).To(Exit(0))
		})

		It("Applies environment variables while staging apps", func() {
			buildpackName = random_name.CATSRandomName("BPK")
			buildpackZip := createBuildpack(envVarName)
			envVarValue := fmt.Sprintf("staging_env_value_%s", strconv.Itoa(int(time.Now().UnixNano())))

			workflowhelpers.AsUser(context.AdminUserContext(), DEFAULT_TIMEOUT, func() {
				extendEnv("staging", envVarName, envVarValue)
				Expect(cf.Cf("create-buildpack", buildpackName, buildpackZip, "999").Wait(DEFAULT_TIMEOUT)).To(Exit(0))
			})

			Expect(cf.Cf("push", appName, "--no-start", "-m", DEFAULT_MEMORY_LIMIT, "-b", buildpackName, "-p", assets.NewAssets().HelloWorld, "-d", config.AppsDomain).Wait(DEFAULT_TIMEOUT)).To(Exit(0))
			app_helpers.SetBackend(appName)
			Expect(cf.Cf("start", appName).Wait(CF_PUSH_TIMEOUT)).To(Exit(1))

			Eventually(func() *Session {
				appLogsSession := cf.Cf("logs", "--recent", appName)
				Expect(appLogsSession.Wait(DEFAULT_TIMEOUT)).To(Exit(0))
				return appLogsSession
			}, DEFAULT_TIMEOUT).Should(Say(envVarValue))
		})
	})

	Context("Running environment variable groups", func() {
		var appName string
		var envVarName string

		BeforeEach(func() {
			appName = random_name.CATSRandomName("APP")
			envVarName = fmt.Sprintf("CATS_RUNNING_TEST_VAR_%s", strconv.Itoa(int(time.Now().UnixNano())))
		})

		AfterEach(func() {
			app_helpers.AppReport(appName, DEFAULT_TIMEOUT)

			workflowhelpers.AsUser(context.AdminUserContext(), DEFAULT_TIMEOUT, func() {
				revertExtendedEnv("running", envVarName)
			})

			Expect(cf.Cf("delete", appName, "-f", "-r").Wait(CF_PUSH_TIMEOUT)).To(Exit(0))
		})

		It("Applies correct environment variables while running apps", func() {
			envVarValue := fmt.Sprintf("running_env_value_%s", strconv.Itoa(int(time.Now().UnixNano())))
			workflowhelpers.AsUser(context.AdminUserContext(), DEFAULT_TIMEOUT, func() {
				extendEnv("running", envVarName, envVarValue)
			})

			Expect(cf.Cf("push", appName, "--no-start", "-b", config.RubyBuildpackName, "-m", DEFAULT_MEMORY_LIMIT, "-p", assets.NewAssets().Dora, "-d", config.AppsDomain).Wait(DEFAULT_TIMEOUT)).To(Exit(0))
			app_helpers.SetBackend(appName)
			Expect(cf.Cf("start", appName).Wait(CF_PUSH_TIMEOUT)).To(Exit(0))

			env := helpers.CurlApp(appName, "/env")

			Expect(env).To(ContainSubstring(envVarValue))
		})
	})
})
