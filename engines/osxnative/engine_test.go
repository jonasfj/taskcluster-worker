// +build darwin

package osxnative

import (
	t "testing"

	"github.com/walac/taskcluster-worker/engines/enginetest"
)

var provider = enginetest.EngineProvider{
	Engine: "macosx",
	Config: `{
		"createUser": false
	}`,
}

var envVarTestCase = enginetest.EnvVarTestCase{
	EngineProvider:       &provider,
	VariableName:         "HELLO_WORLD",
	InvalidVariableNames: []string{"bad d", "also bad", "can't have space"},
	Payload: `{
		"command": ["ls"]
	}`,
}

func TestPrintVariable(t *t.T)        { envVarTestCase.TestPrintVariable() }
func TestVariableNameConflict(t *t.T) { envVarTestCase.TestVariableNameConflict() }
func TestInvalidVariableNames(t *t.T) { envVarTestCase.TestInvalidVariableNames() }
func TestEnvVarTestCase(t *t.T)       { envVarTestCase.Test() }

var loggingTestCase = enginetest.LoggingTestCase{
	EngineProvider: &provider,
	Target:         "HOME",
	TargetPayload: `{
		"command": ["/bin/bash", "-c", "env"]
	}`,
	FailingPayload: `{
		"command": ["/bin/bash", "-c", "env;exit 1"]
	}`,
	SilentPayload: `{
		"command": ["/bin/echo", "test"]
	}`,
}

func TestLogTarget(t *t.T)            { loggingTestCase.TestLogTarget() }
func TestLogTargetWhenFailing(t *t.T) { loggingTestCase.TestLogTargetWhenFailing() }
func TestSilentTask(t *t.T)           { loggingTestCase.TestSilentTask() }
func TestLoggingTestCase(t *t.T)      { loggingTestCase.Test() }
