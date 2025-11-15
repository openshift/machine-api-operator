package main

import (
	"os"
	"regexp"
	"strings"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	"github.com/openshift-eng/openshift-tests-extension/pkg/extension/extensiontests"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"

	// If using ginkgo, import your tests here
	_ "github.com/openshift/machine-api-operator/test/e2e/vsphere"
)

func main() {
	logs.InitLogs()
	defer logs.FlushLogs()
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)

	// Create our registry of openshift-tests extensions
	extensionRegistry := e.NewRegistry()
	kubeTestsExtension := e.NewExtension("openshift", "payload", "machine-api-operator")
	extensionRegistry.Register(kubeTestsExtension)

	// Carve up the kube tests into our openshift suites...
	kubeTestsExtension.AddSuite(e.Suite{
		Name: "mao/conformance/parallel",
		Parents: []string{
			"openshift/conformance/parallel",
		},
		Qualifiers: []string{`!labels.exists(l, l == "Serial") && labels.exists(l, l == "Conformance")`},
	})

	kubeTestsExtension.AddSuite(e.Suite{
		Name: "mao/conformance/serial",
		Parents: []string{
			"openshift/conformance/serial",
		},
		Qualifiers: []string{`labels.exists(l, l == "Serial") && labels.exists(l, l == "Conformance")`},
	})

	// Build our specs from ginkgo only when not running a single test
	// When running in parallel, child processes are spawned with "run-test" command
	// and should not rebuild the Ginkgo suite tree to avoid "cannot clone suite after tree has been built" error
	isRunningTest := len(os.Args) > 1 && os.Args[1] == "run-test"
	if !isRunningTest {
		// Build our specs from ginkgo
		specs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
		if err != nil {
			panic(err)
		}

		// Initialization for kube ginkgo test framework needs to run before all tests execute
		specs.AddBeforeAll(func() {
			if err := initializeTestFramework(os.Getenv("TEST_PROVIDER")); err != nil {
				panic(err)
			}
		})

		// Let's scan for tests with a platform label and create the rule for them such as [platform:vsphere]
		foundPlatforms := make(map[string]string)
		for _, test := range specs.Select(extensiontests.NameContains("[platform:")).Names() {
			re := regexp.MustCompile(`\[platform:[a-z]*]`)
			match := re.FindStringSubmatch(test)
			for _, platformDef := range match {
				if _, ok := foundPlatforms[platformDef]; !ok {
					platform := platformDef[strings.Index(platformDef, ":")+1 : len(platformDef)-1]
					foundPlatforms[platformDef] = platform
					specs.Select(extensiontests.NameContains(platformDef)).
						Include(extensiontests.PlatformEquals(platform))
				}
			}

		}

		kubeTestsExtension.AddSpecs(specs)
	}

	// Cobra stuff
	root := &cobra.Command{
		Long: "Machine API Operator tests extension for OpenShift",
	}

	root.AddCommand(cmd.DefaultExtensionCommands(extensionRegistry)...)

	if err := func() error {
		return root.Execute()
	}(); err != nil {
		os.Exit(1)
	}
}
