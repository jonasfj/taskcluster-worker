package env

import (
	"testing"

	"github.com/walac/taskcluster-worker/plugins/plugintest"
)

func TestEnvNone(*testing.T) {
	plugintest.Case{
		Payload: `{
			"delay": 0,
			"function": "true",
			"argument": "whatever"
		}`,
		PluginConfig:  `{}`,
		Plugin:        "env",
		PluginSuccess: true,
		EngineSuccess: true,
	}.Test()
}

func TestEnvDefinition(*testing.T) {
	plugintest.Case{
		Payload: `{
			"delay": 0,
			"function": "print-env-var",
			"argument": "ENV1",
			"env": {
				"ENV1": "env1"
			}
		}`,
		PluginConfig:  `{}`,
		Plugin:        "env",
		PluginSuccess: true,
		EngineSuccess: true,
		MatchLog:      "env1",
	}.Test()
}

func TestEnvUnDefinition(*testing.T) {
	plugintest.Case{
		Payload: `{
			"delay": 0,
			"function": "print-env-var",
			"argument": "ENV1",
			"env": {
				"ENV2": "env2"
			}
		}`,
		PluginConfig:  `{}`,
		Plugin:        "env",
		PluginSuccess: true,
		EngineSuccess: false,
		NotMatchLog:   "env1",
	}.Test()
}

func TestEnvConfig(*testing.T) {
	plugintest.Case{
		Payload: `{
			"delay": 0,
			"function": "print-env-var",
			"argument": "ENV1",
			"env": {
				"ENV2" : "env2"
			}
		}`,
		PluginConfig: `{
			"extra": {
				"ENV1": "env1"
			}
		}`,
		Plugin:        "env",
		PluginSuccess: true,
		EngineSuccess: true,
		MatchLog:      "env1",
	}.Test()
}
