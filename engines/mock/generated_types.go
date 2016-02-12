// This source code file is AUTO-GENERATED by github.com/taskcluster/jsonschema2go

package mockengine

type (
	Payload struct {
		Argument string `json:"argument"`

		Delay int `json:"delay"`

		// Possible values:
		//   * "true"
		//   * "false"
		//   * "set-volume"
		//   * "get-volume"
		//   * "ping-proxy"
		//   * "write-log"
		//   * "write-error-log"
		Function string `json:"function"`
	}
)