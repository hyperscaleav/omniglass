package lab

import (
	"slices"
	"testing"
)

func TestAServiceJoinsTheLabNetworkByItsName(t *testing.T) {
	o, err := createOptions(Service{Name: "outpost", Container: "p-outpost-1", Image: "img",
		User: "0:0", CapAdd: []string{"NET_ADMIN"}, Restart: "on-failure"}, "p_default")
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != "p-outpost-1" || o.Config.User != "0:0" || !slices.Equal(o.HostConfig.CapAdd, []string{"NET_ADMIN"}) {
		t.Fatalf("the container lost its spec: %+v %+v", o.Config, o.HostConfig)
	}
	if o.HostConfig.NetworkMode != "" {
		t.Fatalf("network mode = %q, want the default", o.HostConfig.NetworkMode)
	}
	ep := o.NetworkingConfig.EndpointsConfig["p_default"]
	if ep == nil || !slices.Equal(ep.Aliases, []string{"outpost"}) {
		t.Fatalf("want the lab network with the alias outpost, got %+v", o.NetworkingConfig)
	}
}

// A sidecar shares another container's network namespace: its loopback is
// that container's, which is how a simulator serves components an outpost
// reaches on its own loopback. Docker refuses aliases or a network of its own
// on such a container, so it is given none.
func TestAServiceCanShareAnotherContainersNetwork(t *testing.T) {
	o, err := createOptions(Service{Name: "sim", Container: "p-sim-1", Image: "img",
		NetworkOf: "abc123"}, "p_default")
	if err != nil {
		t.Fatal(err)
	}
	if o.HostConfig.NetworkMode != "container:abc123" {
		t.Fatalf("network mode = %q, want container:abc123", o.HostConfig.NetworkMode)
	}
	if o.NetworkingConfig != nil {
		t.Fatalf("a shared namespace takes no network of its own, got %+v", o.NetworkingConfig)
	}
}

func TestASharedNetworkPublishesNoPort(t *testing.T) {
	_, err := createOptions(Service{Name: "sim", Container: "p-sim-1", Image: "img", NetworkOf: "abc123",
		Publish: &Publish{HostIP: "127.0.0.1", ContainerPort: 8080}}, "p_default")
	if err == nil {
		t.Fatal("a service sharing a namespace cannot publish a port of its own, want an error")
	}
}
