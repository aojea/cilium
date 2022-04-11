//go:build !privileged_tests && integration_tests
// +build !privileged_tests,integration_tests

package cmd

import (
	"context"

	apiEndpoint "github.com/cilium/cilium/api/v1/server/restapi/endpoint"
	"github.com/cilium/cilium/pkg/checker"
	"github.com/cilium/cilium/pkg/option"
	networkv1alpha1 "gke-internal.googlesource.com/anthos-networking/apis/v2/network/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	. "gopkg.in/check.v1"
)

func (ds *DaemonSuite) TestCreateMultiNICEndpointsNoK8sEnabled(c *C) {
	option.Config.EnableGoogleMultiNIC = true
	defer func() {
		option.Config.EnableGoogleMultiNIC = false
	}()
	epTemplate := getEPTemplate(c, ds.d)
	epTemplate.K8sPodName = "foo-pod"
	epTemplate.K8sNamespace = "foo-ns"
	// Create the primary endpoint
	ep, _, err := ds.d.createEndpoint(context.TODO(), ds, epTemplate)
	c.Assert(err, IsNil)
	eps := ds.d.endpointManager.LookupEndpointsByContainerID(epTemplate.ContainerID)
	c.Assert(eps, HasLen, 1)

	_, code, err := ds.d.createMultiNICEndpoints(context.TODO(), ds, epTemplate, ep)
	c.Assert(code, Equals, apiEndpoint.PutEndpointIDInvalidCode)
	// Make sure the primary endpoint is also deleted
	c.Assert(err, ErrorMatches, "k8s needs to be enabled for multinic endpoint creation")
	eps = ds.d.endpointManager.LookupEndpointsByContainerID(epTemplate.ContainerID)
	c.Assert(eps, HasLen, 0)
}

func (ds *DaemonSuite) TestCreateMultiNICEndpointsNoK8sPodName(c *C) {
	option.Config.EnableGoogleMultiNIC = true
	defer func() {
		option.Config.EnableGoogleMultiNIC = false
	}()
	epTemplate := getEPTemplate(c, ds.d)
	// Create the primary endpoint
	ep, _, err := ds.d.createEndpoint(context.TODO(), ds, epTemplate)
	c.Assert(err, IsNil)
	eps := ds.d.endpointManager.LookupEndpointsByContainerID(epTemplate.ContainerID)
	c.Assert(eps, HasLen, 1)

	_, code, err := ds.d.createMultiNICEndpoints(context.TODO(), ds, epTemplate, ep)
	c.Assert(code, Equals, apiEndpoint.PutEndpointIDInvalidCode)
	// Make sure the primary endpoint is also deleted
	c.Assert(err, ErrorMatches, "k8s namespace and pod name are required to create multinic endpoints")
	eps = ds.d.endpointManager.LookupEndpointsByContainerID(epTemplate.ContainerID)
	c.Assert(eps, HasLen, 0)
}

func (ds *DaemonSuite) TestConvertNetworkSpec(c *C) {
	intf := convertNetworkSpecToInterface(nil)
	c.Assert(intf, IsNil)

	network := &networkv1alpha1.Network{
		ObjectMeta: metav1.ObjectMeta{
			Name: "network-1",
		},
		Spec: networkv1alpha1.NetworkSpec{
			Routes: []networkv1alpha1.Route{
				{To: "1.1.1.1/20"},
				{To: "2.2.2.2/20"},
			},
			Gateway4: pointer.StringPtr("3.3.3.3"),
		},
	}

	expectedIntf := &networkv1alpha1.NetworkInterface{
		Spec: networkv1alpha1.NetworkInterfaceSpec{
			NetworkName: "network-1",
		},
		Status: networkv1alpha1.NetworkInterfaceStatus{
			Routes: []networkv1alpha1.Route{
				{To: "1.1.1.1/20"},
				{To: "2.2.2.2/20"},
			},
			Gateway4: pointer.StringPtr("3.3.3.3"),
		},
	}

	intf = convertNetworkSpecToInterface(network)
	c.Assert(intf, checker.DeepEquals, expectedIntf)
}
