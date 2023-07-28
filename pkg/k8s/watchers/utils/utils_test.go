// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package utils

import (
	"net"
	"testing"

	"github.com/cilium/cilium/pkg/loadbalancer"
)

func TestDeepEqualBackends(t *testing.T) {
	type args struct {
		backends1, backends2 []*loadbalancer.Backend
	}
	testCases := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "backends not equal",
			args: args{
				backends1: []*loadbalancer.Backend{
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.2"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.3"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
				},
				backends2: []*loadbalancer.Backend{
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.2"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.8"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "backend slice lengths not equal",
			args: args{
				backends1: []*loadbalancer.Backend{
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.2"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.3"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
				},
				backends2: []*loadbalancer.Backend{
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.2"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "backends equal",
			args: args{
				backends1: []*loadbalancer.Backend{
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.2"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.3"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
				},
				backends2: []*loadbalancer.Backend{
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.2"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
					{
						L3n4Addr: loadbalancer.L3n4Addr{
							IP: net.ParseIP("10.0.0.3"),
							L4Addr: loadbalancer.L4Addr{
								Protocol: loadbalancer.TCP,
								Port:     8081,
							},
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeepEqualBackends(tc.args.backends1, tc.args.backends2); got != tc.want {
				t.Errorf("DeepEqualBackends() = %v, want %v", got, tc.want)
			}
		})
	}
}
