package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

func TestWritePublicKeyDerivesTheKeyAndRemovesItWhenInvalid(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.txt")
	// RFC 7748 section 6.1, Alice.
	priv, err := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString("8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")
	if err != nil {
		t.Fatal(err)
	}

	writePublicKey(cfg, base64.StdEncoding.EncodeToString(priv))
	got, err := os.ReadFile(filepath.Join(dir, "public_key"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != base64.StdEncoding.EncodeToString(want) {
		t.Fatalf("public key %q", got)
	}

	writePublicKey(cfg, "not-a-key")
	if _, err := os.Stat(filepath.Join(dir, "public_key")); !os.IsNotExist(err) {
		t.Fatalf("stale public key left for an invalid private key: %v", err)
	}
}
