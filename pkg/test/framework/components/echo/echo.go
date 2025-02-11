// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package echo

import (
	"context"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	dto "github.com/prometheus/client_model/go"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

// Builder for a group of collaborating Echo Instances. Once built, all Instances in the
// group:
//
//     1. Are ready to receive traffic, and
//     2. Can call every other Instance in the group (i.e. have received Envoy config
//        from Pilot).
//
// If a test needs to verify that one Instance is NOT reachable from another, there are
// a couple of options:
//
//     1. Build a group while all Instances ARE reachable. Then apply a policy
//        disallowing the communication.
//     2. Build the source and destination Instances in separate groups and then
//        call `source.WaitUntilCallable(destination)`.
type Builder interface {
	// With adds a new Echo configuration to the Builder. Once built, the instance
	// pointer will be updated to point at the new Instance.
	With(i *Instance, cfg Config) Builder

	// WithConfig mimics the behavior of With, but does not allow passing a reference
	// and returns an echoboot builder rather than a generic echo builder.
	// TODO rename this to With, and the old method to WithInstance
	WithConfig(cfg Config) Builder

	// WithClusters will cause subsequent With or WithConfig calls to be applied to the given clusters.
	WithClusters(...cluster.Cluster) Builder

	// Build and initialize all Echo Instances. Upon returning, the Instance pointers
	// are assigned and all Instances are ready to communicate with each other.
	Build() (Instances, error)
	BuildOrFail(t test.Failer) Instances
}

type Caller interface {
	// Call makes a call from this Instance to a target Instance.
	Call(options CallOptions) (client.ParsedResponses, error)
	CallOrFail(t test.Failer, options CallOptions) client.ParsedResponses

	// CallWithRetry is the same as call, except that it will attempt to retry based on the provided
	// options. If no options are provided, uses defaults.
	CallWithRetry(options CallOptions, retryOptions ...retry.Option) (client.ParsedResponses, error)
	CallWithRetryOrFail(t test.Failer, options CallOptions, retryOptions ...retry.Option) client.ParsedResponses
}

type Callers []Caller

// Instances returns an Instances if all callers are Instance, otherwise returns nil.
func (c Callers) Instances() Instances {
	var out Instances
	for _, caller := range c {
		c, ok := caller.(Instance)
		if !ok {
			return nil
		}
		out = append(out, c)
	}
	return out
}

// Instance is a component that provides access to a deployed echo service.
type Instance interface {
	Caller
	resource.Resource

	// Config returns the configuration of the Echo instance.
	Config() Config

	// Address of the service (e.g. Kubernetes cluster IP). May be "" if headless.
	Address() string

	// Workloads retrieves the list of all deployed workloads for this Echo service.
	// Guarantees at least one workload, if error == nil.
	Workloads() ([]Workload, error)
	WorkloadsOrFail(t test.Failer) []Workload

	// Restart restarts the workloads associated with this echo instance
	Restart() error
}

// Workload port exposed by an Echo instance
type WorkloadPort struct {
	// Port number
	Port int

	// Protocol to be used for this port.
	Protocol protocol.Instance

	// TLS determines whether the connection will be plain text or TLS. By default this is false (plain text).
	TLS bool

	// ServerFirst determines whether the port will use server first communication, meaning the client will not send the first byte.
	ServerFirst bool
}

// Port exposed by an Echo Instance
type Port struct {
	// Name of this port
	Name string

	// Protocol to be used for the port.
	Protocol protocol.Instance

	// ServicePort number where the service can be reached. Does not necessarily
	// map to the corresponding port numbers for the instances behind the
	// service.
	ServicePort int

	// InstancePort number where this instance is listening for connections.
	// This need not be the same as the ServicePort where the service is accessed.
	InstancePort int

	// TLS determines whether the connection will be plain text or TLS. By default this is false (plain text).
	TLS bool

	// ServerFirst determines whether the port will use server first communication, meaning the client will not send the first byte.
	ServerFirst bool

	// InstanceIP determines if echo will listen on the instance IP; otherwise, it will listen on wildcard
	InstanceIP bool

	// LocalhostIP determines if echo will listen on the localhost IP; otherwise, it will listen on wildcard
	LocalhostIP bool
}

// Workload provides an interface for a single deployed echo server.
type Workload interface {
	// PodName gets the original pod name for the workload.
	PodName() string
	// Address returns the network address of the endpoint.
	Address() string

	// Sidecar if one was specified.
	Sidecar() Sidecar

	// ForwardEcho executes specific call from this workload.
	ForwardEcho(context.Context, *proto.ForwardEchoRequest) (client.ParsedResponses, error)

	// Logs returns the logs for the app container
	Logs() (string, error)
	// LogsOrFail returns the logs for the app container, or aborts if an error is found
	LogsOrFail(t test.Failer) string
}

// Sidecar provides an interface to execute queries against a single Envoy sidecar.
type Sidecar interface {
	// NodeID returns the node ID used for uniquely identifying this sidecar to Pilot.
	NodeID() string

	// Info about the Envoy instance.
	Info() (*envoyAdmin.ServerInfo, error)
	InfoOrFail(t test.Failer) *envoyAdmin.ServerInfo

	// Config of the Envoy instance.
	Config() (*envoyAdmin.ConfigDump, error)
	ConfigOrFail(t test.Failer) *envoyAdmin.ConfigDump

	// WaitForConfig queries the Envoy configuration an executes the given accept handler. If the
	// response is not accepted, the request will be retried until either a timeout or a response
	// has been accepted.
	WaitForConfig(accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) error
	WaitForConfigOrFail(t test.Failer, accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option)

	// Clusters for the Envoy instance
	Clusters() (*envoyAdmin.Clusters, error)
	ClustersOrFail(t test.Failer) *envoyAdmin.Clusters

	// Listeners for the Envoy instance
	Listeners() (*envoyAdmin.Listeners, error)
	ListenersOrFail(t test.Failer) *envoyAdmin.Listeners

	// Logs returns the logs for the sidecar container
	Logs() (string, error)
	// LogsOrFail returns the logs for the sidecar container, or aborts if an error is found
	LogsOrFail(t test.Failer) string
	Stats() (map[string]*dto.MetricFamily, error)
	StatsOrFail(t test.Failer) map[string]*dto.MetricFamily
}
