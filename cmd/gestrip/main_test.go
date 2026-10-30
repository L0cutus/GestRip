package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

// inventario: ADM-20
// Il binario serve /healthz su tutte e due le facce, solo su loopback di default.
func TestServeRispondeSuDueFacce(t *testing.T) {
	ctx, ferma := context.WithCancel(context.Background())
	pronto := make(chan [2]string, 1)
	finito := make(chan error, 1)
	go func() {
		finito <- serve(ctx, opzioniServe{
			DB:       filepath.Join(t.TempDir(), "gestrip.db"),
			Interna:  "127.0.0.1:0",
			Pubblica: "127.0.0.1:0",
		}, func(interna, pubblica net.Addr) { pronto <- [2]string{interna.String(), pubblica.String()} })
	}()
	var indirizzi [2]string
	select {
	case indirizzi = <-pronto:
	case err := <-finito:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve non parte")
	}
	for _, a := range indirizzi {
		r, err := http.Get("http://" + a + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 || string(b) != "ok" {
			t.Errorf("%s: %d %q", a, r.StatusCode, b)
		}
	}
	ferma()
	if err := <-finito; err != nil {
		t.Errorf("chiusura: %v", err)
	}
}

func TestServeRifiutaLeFacceSullaStessaPorta(t *testing.T) {
	err := serve(context.Background(), opzioniServe{
		DB: filepath.Join(t.TempDir(), "g.db"), Interna: "127.0.0.1:47999", Pubblica: "127.0.0.1:47999",
	}, nil)
	if err == nil {
		t.Error("interna e pubblica sullo stesso indirizzo accettate")
	}
}
