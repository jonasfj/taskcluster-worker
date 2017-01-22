package runtime

import (
	"github.com/Sirupsen/logrus"
	"github.com/getsentry/raven-go"
	"github.com/walac/taskcluster-worker/runtime/gc"
	"github.com/walac/taskcluster-worker/runtime/webhookserver"
)

// Environment is a collection of objects that makes up a runtime environment.
type Environment struct {
	GarbageCollector gc.ResourceTracker
	//TODO: Add some sort of interface to the system logger
	//TODO: Add some interface to submit statistics for influxdb/signalfx
	//TODO: Add some interface to attach a http.Handler to public facing server
	TemporaryStorage TemporaryStorage
	Log              *logrus.Logger
	Sentry           *raven.Client
	WebHookServer    webhookserver.WebHookServer
}
