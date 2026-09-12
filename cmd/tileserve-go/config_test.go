package main

import (
	"net"
	"testing"
)

func TestParseSubnets(t *testing.T) {
	t.Parallel()

	t.Run("empty string yields no subnets", func(t *testing.T) {
		t.Parallel()

		got, err := parseSubnets("")
		if err != nil {
			t.Fatalf("parseSubnets() error = %v", err)
		}

		if len(got) != 0 {
			t.Errorf("parseSubnets() = %v, want empty", got)
		}
	})

	t.Run("blank entries are skipped", func(t *testing.T) {
		t.Parallel()

		got, err := parseSubnets(" , 10.0.0.0/8 , ")
		if err != nil {
			t.Fatalf("parseSubnets() error = %v", err)
		}

		if len(got) != 1 || got[0].String() != "10.0.0.0/8" {
			t.Errorf("parseSubnets() = %v, want [10.0.0.0/8]", got)
		}
	})

	t.Run("multiple valid CIDRs", func(t *testing.T) {
		t.Parallel()

		got, err := parseSubnets("10.0.0.0/8,192.168.1.0/24")
		if err != nil {
			t.Fatalf("parseSubnets() error = %v", err)
		}

		if len(got) != 2 {
			t.Fatalf("parseSubnets() returned %d subnets, want 2", len(got))
		}

		if !got[0].Contains(net.ParseIP("10.1.2.3")) {
			t.Errorf("first subnet %v should contain 10.1.2.3", got[0])
		}

		if !got[1].Contains(net.ParseIP("192.168.1.42")) {
			t.Errorf("second subnet %v should contain 192.168.1.42", got[1])
		}
	})

	t.Run("invalid CIDR returns an error", func(t *testing.T) {
		t.Parallel()

		if _, err := parseSubnets("not-a-cidr"); err == nil {
			t.Fatal("parseSubnets() error = nil, want error for invalid CIDR")
		}
	})
}
