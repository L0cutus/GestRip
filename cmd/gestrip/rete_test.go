package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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

// inventario: OPS-12, ACC-18
// Le facce ascoltano solo su loopback: 127.0.0.0/8, ::1 o il nome localhost, scritti così
// (nessun nome si risolve). Tutte le interfacce (anche con la sola porta), un indirizzo della LAN
// o della tailnet, un altro nome o un indirizzo senza porta sono rifiutati.
func TestIndirizzoDAscoltoLocale(t *testing.T) {
	casi := map[string]bool{
		"127.0.0.1:47101":              true,
		"127.0.0.1:0":                  true,
		"127.1.2.3:47101":              true,
		"127.255.255.254:47101":        true,
		"[::1]:47101":                  true,
		"localhost:47101":              true,
		"[::ffff:127.0.0.1]:47101":     true,
		":47101":                       false,
		"0.0.0.0:47101":                false,
		"[::]:47101":                   false,
		"[::ffff:0.0.0.0]:47101":       false,
		"192.168.1.10:47101":           false,
		"10.0.0.1:47101":               false,
		"100.64.0.1:47101":             false,
		"128.0.0.1:47101":              false,
		"[fe80::1%lo]:47101":           false,
		"[::1%lo]:47101":               false,
		"[::2]:47101":                  false,
		"LOCALHOST:47101":              false,
		"localhost.:47101":             false,
		"negozio.esempio.ts.net:47101": false,
		"127.0.0.1.esempio.it:47101":   false,
		"127.0.0.1":                    false,
		"localhost":                    false,
		"":                             false,
	}
	for ind, locale := range casi {
		err := controllaAscoltoLocale(ind)
		if locale && err != nil {
			t.Errorf("%q rifiutato: %v", ind, err)
		}
		if !locale && err == nil {
			t.Errorf("%q accettato", ind)
		}
	}
}

// inventario: OPS-12, ACC-18
// serve rifiuta un indirizzo non locale per ognuna delle due facce prima di creare il database,
// e il messaggio non dice come spegnere il controllo.
func TestServeRifiutaLAscoltoNonLocale(t *testing.T) {
	for _, c := range []struct{ faccia, interna, pubblica string }{
		{"faccia interna", "0.0.0.0:0", "127.0.0.1:0"},
		{"faccia interna", ":0", "127.0.0.1:0"},
		{"faccia pubblica", "127.0.0.1:0", "[::]:0"},
		{"faccia pubblica", "127.0.0.1:0", "192.0.2.10:0"},
	} {
		db := filepath.Join(t.TempDir(), "g.db")
		// Se le facce si aprissero, il test le chiude subito: un rifiuto mancato non resta appeso.
		ctx, ferma := context.WithCancel(context.Background())
		err := serve(ctx, opzioniServe{SenzaVolumeCifrato: true, DB: db, Interna: c.interna, Pubblica: c.pubblica,
			avvisi: io.Discard}, func(net.Addr, net.Addr) {
			t.Errorf("%s %s: facce aperte", c.interna, c.pubblica)
			ferma()
		})
		ferma()
		if err == nil || !strings.Contains(err.Error(), c.faccia) || !strings.Contains(err.Error(), "non è locale") ||
			strings.Contains(err.Error(), "ascolto-non-locale") {
			t.Errorf("%s %s: %v", c.interna, c.pubblica, err)
		}
		if _, e := os.Stat(db); e == nil {
			t.Errorf("%s %s: database creato", c.interna, c.pubblica)
		}
	}
}

// inventario: OPS-12, ACC-18
// -ascolto-non-locale (sviluppo e banco) lascia le facce su ogni indirizzo, ma lo dice forte
// all'avvio. Di default è spento. Qui l'ascolto vero resta su loopback: il test non apre porte
// sulla rete.
func TestServeAscoltoNonLocale(t *testing.T) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	o := opzioniDiServe(fs)
	if err := fs.Parse(nil); err != nil || o.AscoltoNonLocale {
		t.Errorf("di default l'ascolto non locale è permesso: %v", err)
	}
	fs = flag.NewFlagSet("serve", flag.ContinueOnError)
	o = opzioniDiServe(fs)
	if err := fs.Parse([]string{"-ascolto-non-locale"}); err != nil || !o.AscoltoNonLocale {
		t.Errorf("-ascolto-non-locale: %v %v", o.AscoltoNonLocale, err)
	}
	var avvisi bytes.Buffer
	var chiesti []string
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	o2 := opzioniServe{SenzaVolumeCifrato: true, AscoltoNonLocale: true, DB: filepath.Join(t.TempDir(), "g.db"),
		Interna: "0.0.0.0:47101", Pubblica: ":47102", avvisi: &avvisi,
		ascolta: func(rete, ind string) (net.Listener, error) {
			chiesti = append(chiesti, ind)
			return net.Listen(rete, "127.0.0.1:0")
		}}
	finito := make(chan error, 1)
	pronto := make(chan struct{})
	go func() { finito <- serve(ctx, o2, func(net.Addr, net.Addr) { close(pronto) }) }()
	select {
	case <-pronto:
		ferma()
		if err := <-finito; err != nil {
			t.Errorf("chiusura: %v", err)
		}
	case err := <-finito:
		t.Fatalf("con -ascolto-non-locale: %v", err)
	}
	if strings.Join(chiesti, " ") != "0.0.0.0:47101 :47102" {
		t.Errorf("indirizzi chiesti: %q", chiesti)
	}
	// La riga di questa opzione, da sola: c'è anche quella del volume cifrato, che dice lo stesso.
	var riga string
	for r := range strings.SplitSeq(avvisi.String(), "\n") {
		if strings.Contains(r, "-ascolto-non-locale") {
			riga = r
		}
	}
	if !strings.Contains(riga, "ATTENZIONE") || !strings.Contains(riga, "mai sull'apparecchio") {
		t.Errorf("avviso: %q", avvisi.String())
	}
}

// ascoltoAltrove è una faccia che dice di ascoltare su un indirizzo non locale.
type ascoltoAltrove struct{ net.Listener }

func (ascoltoAltrove) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 47101}
}

// inventario: OPS-12, ACC-18
// Anche dopo l'apertura serve guarda dove ascolta davvero ognuna delle due facce: localhost che
// finisse su un indirizzo della rete (un /etc/hosts sbagliato) ferma l'avvio e le facce aperte si
// chiudono.
func TestServeControllaLAscoltoVero(t *testing.T) {
	for _, c := range []struct{ faccia, interna, pubblica string }{
		{"faccia interna", "localhost:0", "127.0.0.1:0"},
		{"faccia pubblica", "127.0.0.1:0", "localhost:0"},
	} {
		t.Run(c.faccia, func(t *testing.T) {
			var aperte []net.Listener
			o := opzioniServe{SenzaVolumeCifrato: true, DB: filepath.Join(t.TempDir(), "g.db"),
				Interna: c.interna, Pubblica: c.pubblica, avvisi: io.Discard,
				ascolta: func(rete, ind string) (net.Listener, error) {
					l, err := net.Listen(rete, "127.0.0.1:0")
					if err != nil {
						return nil, err
					}
					aperte = append(aperte, l)
					if ind == "localhost:0" {
						return ascoltoAltrove{l}, nil
					}
					return l, nil
				}}
			ctx, ferma := context.WithCancel(context.Background())
			err := serve(ctx, o, func(net.Addr, net.Addr) {
				t.Error("facce aperte")
				ferma() // un controllo mancato non lascia serve appeso
			})
			ferma()
			if err == nil || !strings.Contains(err.Error(), c.faccia) || !strings.Contains(err.Error(), "192.0.2.10") {
				t.Errorf("ascolto vero non locale: %v", err)
			}
			for _, l := range aperte {
				if conn, err := net.Dial("tcp", l.Addr().String()); err == nil {
					_ = conn.Close() // la connessione serve solo a vedere se la faccia è ancora aperta
					t.Errorf("faccia %s ancora aperta", l.Addr())
				}
			}
		})
	}
}
