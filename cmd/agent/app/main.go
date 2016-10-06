package ddagentmain

import (
	"path/filepath"

	"github.com/DataDog/datadog-agent/pkg/collector/check"
	"github.com/DataDog/datadog-agent/pkg/collector/check/core"
	"github.com/DataDog/datadog-agent/pkg/collector/check/py"
	"github.com/DataDog/datadog-agent/pkg/collector/loader"
	"github.com/DataDog/datadog-agent/pkg/collector/scheduler"
	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/kardianos/osext"
	"github.com/op/go-logging"
	"github.com/sbinet/go-python"

	// register core checks
	_ "github.com/DataDog/datadog-agent/pkg/collector/check/core/system"
)

const agentVersion = "6.0.0"

var here, _ = osext.ExecutableFolder()
var distPath = filepath.Join(here, "dist")
var log = logging.MustGetLogger("datadog-agent")

// for testing purposes only: collect and log check results
type metric struct {
	Name  string
	Value float64
	Tags  []string
}

type metrics map[string][]metric

// build a list of providers for checks' configurations, the sequence defines
// the precedence.
func getConfigProviders() (providers []loader.ConfigProvider) {
	confdPath := filepath.Join(distPath, "conf.d")
	configPaths := []string{confdPath}

	// File Provider
	providers = append(providers, loader.NewFileConfigProvider(configPaths))

	return providers
}

// build a list of check loaders, the sequence defines the precedence.
func getCheckLoaders() []loader.CheckLoader {
	return []loader.CheckLoader{
		py.NewPythonCheckLoader(),
		core.NewGoCheckLoader(),
	}
}

// build a list of providers for Agent configuration, the sequence
// define the precedence.
func getAgentConfigProviders() (providers []config.Provider) {
	return []config.Provider{
		config.NewFileProvider(configPath),
	}
}

// Start the main check loop
func Start() {

	log.Infof("Starting Datadog Agent v%v", agentVersion)

	// Global Agent configuration
	cfg := config.NewConfig()
	for _, provider := range getAgentConfigProviders() {
		if err := provider.Configure(cfg); err != nil {
			log.Warningf("Unable to load configuration from provider %v: %v", provider, err)
		}
	}

	// Create a channel to enqueue the checks
	pending := make(chan check.Check, 10)

	// Initialize the CPython interpreter
	state := py.Initialize(distPath, filepath.Join(distPath, "checks"))

	// Get a single Runner instance, i.e. we process checks sequentially
	go check.Runner(pending)

	// Get a list of config checks from the configured providers
	var configs []check.Config
	for _, provider := range getConfigProviders() {
		c, _ := provider.Collect()
		configs = append(configs, c...)
	}

	// Instance the scheduler
	scheduler := scheduler.NewScheduler(pending)

	// given a list of configurations, try to load corresponding checks using different loaders
	// TODO add check type to the conf file so that we avoid the inner for
	loaders := getCheckLoaders()
	for _, conf := range configs {
		for _, loader := range loaders {
			res, err := loader.Load(conf)
			if err == nil {
				scheduler.Enter(res)
			}
		}
	}

	// Run the scheduler
	scheduler.Run()

	// indefinitely block here for now, later we'll migrate to a more sophisticated
	// system to handle interrupts (reloads, restarts, service discovery events, etc...)
	var c chan bool
	<-c

	// this is not called for now, sorry CPython for leaving a mess on exit!
	python.PyEval_RestoreThread(state)
}
