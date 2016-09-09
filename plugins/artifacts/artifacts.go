//go:generate go-composite-schema --unexported artifacts payload-schema.yml generated_payloadschema.go

// Package artifacts is responsible for uploading artifacts after builds
package artifacts

import (
	"mime"
	"path/filepath"
	"time"

	"github.com/taskcluster/taskcluster-client-go/tcclient"
	"github.com/taskcluster/taskcluster-worker/engines"
	"github.com/taskcluster/taskcluster-worker/plugins"
	"github.com/taskcluster/taskcluster-worker/plugins/extpoints"
	"github.com/taskcluster/taskcluster-worker/runtime"
	"github.com/taskcluster/taskcluster-worker/runtime/ioext"
)

type pluginProvider struct {
	extpoints.PluginProviderBase
}

func (pluginProvider) NewPlugin(extpoints.PluginOptions) (plugins.Plugin, error) {
	return plugin{}, nil
}

type plugin struct {
	plugins.PluginBase
}

func (plugin) PayloadSchema() (runtime.CompositeSchema, error) {
	return payloadSchema, nil
}

func (plugin) NewTaskPlugin(options plugins.TaskPluginOptions) (plugins.TaskPlugin, error) {
	if options.Payload == nil {
		return plugins.TaskPluginBase{}, nil
	}
	return &taskPlugin{
		TaskPluginBase: plugins.TaskPluginBase{},
		payload:        *(options.Payload.(*payload)),
	}, nil
}

type taskPlugin struct {
	plugins.TaskPluginBase
	context *runtime.TaskContext
	payload payload
}

func (tp *taskPlugin) Prepare(context *runtime.TaskContext) error {
	tp.context = context
	return nil
}

func (tp *taskPlugin) Stopped(result engines.ResultSet) (bool, error) {
	nonFatalErrs := []engines.MalformedPayloadError{}

	for _, artifact := range tp.payload {
		// If expires is set to this time it's either the default value or has been set to an invalid time anyway
		if time.Time(artifact.Expires).IsZero() {
			artifact.Expires = tp.context.TaskInfo.Expires
		}
		switch artifact.Type {
		case "directory":
			err := result.ExtractFolder(artifact.Path, tp.createUploadHandler(artifact.Name, artifact.Expires))
			if err != nil {
				if tp.errorHandled(artifact.Name, artifact.Expires, err) {
					nonFatalErrs = append(nonFatalErrs, engines.NewMalformedPayloadError(err.Error()))
					continue
				}
				return false, err
			}
		case "file":
			fileReader, err := result.ExtractFile(artifact.Path)
			if err != nil {
				if tp.errorHandled(artifact.Name, artifact.Expires, err) {
					nonFatalErrs = append(nonFatalErrs, engines.NewMalformedPayloadError(err.Error()))
					continue
				}
				return false, err
			}
			err = tp.attemptUpload(fileReader, artifact.Path, artifact.Name, artifact.Expires)
			if err != nil {
				return false, err
			}
		}
	}

	if len(nonFatalErrs) > 0 {
		return false, engines.MergeMalformedPayload(nonFatalErrs...)
	}
	return true, nil
}

func (tp taskPlugin) errorHandled(name string, expires tcclient.Time, err error) bool {
	var reason string
	if _, ok := err.(*engines.MalformedPayloadError); ok {
		reason = "invalid-resource-on-worker"
	} else if err == engines.ErrFeatureNotSupported || err == engines.ErrNonFatalInternalError || err == engines.ErrHandlerInterrupt {
		reason = "invalid-resource-on-worker"
	} else if err == engines.ErrResourceNotFound {
		reason = "file-missing-on-worker"
	}

	if reason != "" {
		tp.context.Log("Artifact upload error handled. Continuing...", name, err.Error())
		runtime.CreateErrorArtifact(runtime.ErrorArtifact{
			Name:    name,
			Message: err.Error(),
			Reason:  reason,
			Expires: expires,
		}, tp.context)
		return true
	}
	return false
}

func (tp taskPlugin) createUploadHandler(name string, expires tcclient.Time) func(string, ioext.ReadSeekCloser) error {
	return func(path string, stream ioext.ReadSeekCloser) error {
		return tp.attemptUpload(stream, path, filepath.Join(name, path), expires)
	}
}

func (tp taskPlugin) attemptUpload(fileReader ioext.ReadSeekCloser, path string, name string, expires tcclient.Time) error {
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		// application/octet-stream is the mime type for "unknown"
		mimeType = "application/octet-stream"
	}
	return runtime.UploadS3Artifact(runtime.S3Artifact{
		Name:     name,
		Mimetype: mimeType,
		Stream:   fileReader,
		Expires:  expires,
	}, tp.context)
}

func init() {
	extpoints.PluginProviders.Register(new(pluginProvider), "artifacts")
}
