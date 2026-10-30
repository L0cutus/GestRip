package main

import (
	"flag"
	"net"
	"testing"
)

// inventario: ACC-18
// Le due facce ascoltano di default solo su 127.0.0.1: in chiaro le raggiunge solo tailscaled
// sullo stesso apparecchio, che fa il TLS (tailscale serve e funnel). Nessuna porta in chiaro
// nella LAN; HSTS e cookie __Host- (solo Secure) fanno il resto nel browser.
func TestFacceSoloSuLoopbackDiDefault(t *testing.T) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	o := opzioniDiServe(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	for nome, ind := range map[string]string{"interna": o.Interna, "pubblica": o.Pubblica} {
		host, _, err := net.SplitHostPort(ind)
		if err != nil || host != "127.0.0.1" {
			t.Errorf("faccia %s di default su %q, attesa solo su 127.0.0.1", nome, ind)
		}
	}
}
