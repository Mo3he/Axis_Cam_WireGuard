package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseLocalForwards(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		want     []localForward
		rejected []string
	}{
		{"empty", "", nil, nil},
		{
			"udp and tcp",
			"udp:5514:10.0.0.1:514, tcp:6514:syslog.example.com:6514",
			[]localForward{
				{proto: "udp", port: 5514, dst: "10.0.0.1:514"},
				{proto: "tcp", port: 6514, dst: "syslog.example.com:6514"},
			},
			nil,
		},
		{
			"proto is case-insensitive",
			"UDP:5514:10.0.0.1:514",
			[]localForward{{proto: "udp", port: 5514, dst: "10.0.0.1:514"}},
			nil,
		},
		{
			"ipv6 destination",
			"udp:5514:[fd00::1]:514",
			[]localForward{{proto: "udp", port: 5514, dst: "[fd00::1]:514"}},
			nil,
		},
		{
			"same port allowed once per protocol",
			"udp:5514:10.0.0.1:514,tcp:5514:10.0.0.1:601,udp:5514:10.0.0.2:514",
			[]localForward{
				{proto: "udp", port: 5514, dst: "10.0.0.1:514"},
				{proto: "tcp", port: 5514, dst: "10.0.0.1:601"},
			},
			[]string{"udp:5514:10.0.0.2:514"},
		},
		{
			"invalid entries rejected",
			"sctp:5514:10.0.0.1:514,udp:514:10.0.0.1:514,udp:5514:10.0.0.1,udp:5514::514,udp:5514:10.0.0.1:0,udp:x:10.0.0.1:514,junk",
			nil,
			[]string{"sctp:5514:10.0.0.1:514", "udp:514:10.0.0.1:514", "udp:5514:10.0.0.1", "udp:5514::514", "udp:5514:10.0.0.1:0", "udp:x:10.0.0.1:514", "junk"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, rejected := parseLocalForwards(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("forwards = %+v, want %+v", got, tc.want)
			}
			if !reflect.DeepEqual(rejected, tc.rejected) {
				t.Errorf("rejected = %q, want %q", rejected, tc.rejected)
			}
		})
	}
}

func TestParseLocalForwardsCap(t *testing.T) {
	entries := make([]string, maxLocalForwards+2)
	for i := range entries {
		entries[i] = fmt.Sprintf("udp:%d:10.0.0.1:514", 5000+i)
	}
	got, rejected := parseLocalForwards(strings.Join(entries, ","))
	if len(got) != maxLocalForwards || len(rejected) != 2 {
		t.Fatalf("got %d forwards and %d rejected, want %d and 2", len(got), len(rejected), maxLocalForwards)
	}
}
