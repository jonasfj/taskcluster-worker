package scriptengine

import (
	"fmt"

	"github.com/Sirupsen/logrus"
	schematypes "github.com/taskcluster/go-schematypes"
	"github.com/walac/taskcluster-worker/engines"
	"github.com/walac/taskcluster-worker/runtime"
)

type engineProvider struct {
	engines.EngineProviderBase
}

type engine struct {
	engines.EngineBase
	Log         *logrus.Entry
	config      configType
	schema      schematypes.Object
	environment *runtime.Environment
}

func init() {
	engines.Register("script", engineProvider{})
}

func (engineProvider) ConfigSchema() schematypes.Schema {
	return configSchema
}

func (engineProvider) NewEngine(options engines.EngineOptions) (engines.Engine, error) {
	var config configType
	if schematypes.MustMap(configSchema, options.Config, &config) != nil {
		return nil, engines.ErrContractViolation
	}
	// Construct payload schema as schematypes.Object using schema.properties
	properties := schematypes.Properties{}
	for k, s := range config.Schema.Properties {
		schema, err := schematypes.NewSchema(s)
		if err != nil {
			return nil, fmt.Errorf("Error loading schema: %s", err)
		}
		properties[k] = schema
	}

	return &engine{
		Log:    options.Log,
		config: config,
		schema: schematypes.Object{
			Properties: properties,
		},
		environment: options.Environment,
	}, nil
}

func (e *engine) PayloadSchema() schematypes.Object {
	return e.schema
}

func (e *engine) NewSandboxBuilder(options engines.SandboxOptions) (engines.SandboxBuilder, error) {
	if e.schema.Validate(options.Payload) != nil {
		return nil, engines.ErrContractViolation
	}
	return &sandboxBuilder{
		payload: options.Payload,
		engine:  e,
		context: options.TaskContext,
	}, nil
}
