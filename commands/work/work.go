package work

import (
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/taskcluster/slugid-go/slugid"
	cmd_ep "github.com/taskcluster/taskcluster-worker/commands/extpoints"
	"github.com/taskcluster/taskcluster-worker/config"
	"github.com/taskcluster/taskcluster-worker/engines/extpoints"
	"github.com/taskcluster/taskcluster-worker/runtime"
	"github.com/taskcluster/taskcluster-worker/runtime/webhookserver"
	"github.com/taskcluster/taskcluster-worker/worker"
)

func init() {
	cmd_ep.CommandProviders.Register(cmd{}, "work")
}

type cmd struct{}

func (cmd) Summary() string {
	return "Start the worker."
}

func (cmd) Usage() string {
	return `Usage:
  taskcluster-worker work <engine> [--logging-level <level>]

Options:
  -l --logging-level <level>  	Set logging at <level>.
`
}

func (cmd) Execute(args map[string]interface{}) bool {
	// Setup logger
	var level string
	if l := args["--logging-level"]; l != nil {
		level = l.(string)
	}
	logger, err := runtime.CreateLogger(level)
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(1)
	}

	// TODO (garndt): Need to load up a real config in the future
	config := &config.Config{
		Credentials: struct {
			AccessToken string `json:"accessToken"`
			Certificate string `json:"certificate"`
			ClientID    string `json:"clientId"`
		}{
			AccessToken: os.Getenv("TASKCLUSTER_ACCESS_TOKEN"),
			Certificate: os.Getenv("TASKCLUSTER_CERTIFICATE"),
			ClientID:    os.Getenv("TASKCLUSTER_CLIENT_ID"),
		},
		Taskcluster: struct {
			Queue struct {
				URL string `json:"url,omitempty"`
			} `json:"queue,omitempty"`
		}{
			Queue: struct {
				URL string `json:"url,omitempty"`
			}{
				URL: "https://queue.taskcluster.net/v1/",
			},
		},
		Capacity:      5,
		ProvisionerID: "dummy-test-provisioner",
		WorkerType:    "garndt-test-type",
		WorkerGroup:   "dummy-test-group",
		WorkerID:      "garndt-test-worker",
		QueueService: struct {
			ExpirationOffset int `json:"expirationOffset"`
		}{
			ExpirationOffset: 300,
		},
		PollingInterval: 10,
		StatelessHostname: struct {
			Domain  string `json:"domain"`
			Enabled bool   `json:"enabled"`
			Secret  string `json:"secret"`
		}{
			Domain:  "taskcluster-worker.net",
			Enabled: true,
			Secret:  os.Getenv("STATELESS_SECRET"),
		},
	}

	l := logger.WithFields(logrus.Fields{
		"workerID":      config.WorkerID,
		"workerType":    config.WorkerType,
		"workerGroup":   config.WorkerGroup,
		"provisionerID": config.ProvisionerID,
	})

	// Find engine provider
	engineName := args["<engine>"].(string)
	engineProvider := extpoints.EngineProviders.Lookup(engineName)
	if engineProvider == nil {
		engineNames := extpoints.EngineProviders.Names()
		logger.Fatalf("Must supply a valid engine.  Supported Engines %v", engineNames)
	}

	// Create a temporary folder
	tempPath := filepath.Join(os.TempDir(), slugid.Nice())
	tempStorage, err := runtime.NewTemporaryStorage(tempPath)
	if err != nil {
		panic(err)
	}

	localServer, err := webhookserver.NewLocalServer(
		net.TCPAddr{
			IP:   []byte{127, 0, 0, 1},
			Port: 60000,
		},
		"taskcluster-worker.net",
		config.StatelessHostname.Secret,
		"",
		"",
		10*time.Minute,
	)
	if err != nil {
		panic(err)
	}

	go func() {
		panic(localServer.ListenAndServe())
	}()

	runtimeEnvironment := &runtime.Environment{
		Log:              logger,
		TemporaryStorage: tempStorage,
		WebHookServer:    localServer,
	}

	// Initialize the engine
	engine, err := engineProvider.NewEngine(extpoints.EngineOptions{
		Environment: runtimeEnvironment,
		Log:         logger.WithField("engine", engineName),
	})
	if err != nil {
		logger.Fatal(err.Error())
	}

	w, err := worker.New(config, engine, runtimeEnvironment, l)
	if err != nil {
		logger.Fatalf("Could not create worker. %s", err)
	}

	w.Start()
	return true
}
