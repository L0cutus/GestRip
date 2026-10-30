package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// inventario: ADM-20
// Lo stop che arriva mentre serve si avvia (qui, fermo a metà: dopo le migrazioni, prima della
// chiave del server) non è un errore: serve torna con errFermatoInAvvio e basta, senza aprire le
// facce.
func TestServeFermatoDuranteLAvvio(t *testing.T) {
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	fermo := make(chan struct{})
	o := opzioniServe{SenzaVolumeCifrato: true, DB: filepath.Join(t.TempDir(), "g.db"),
		Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard,
		inAvvio: func(ctx context.Context) {
			close(fermo)
			<-ctx.Done()
		}}
	finito := make(chan error, 1)
	go func() {
		finito <- serve(ctx, o, func(net.Addr, net.Addr) { t.Error("facce aperte dopo lo stop") })
	}()
	select {
	case <-fermo:
	case err := <-finito:
		t.Fatalf("serve è uscito prima di arrivare a metà: %v", err)
	}
	ferma()
	select {
	case err := <-finito:
		if err != errFermatoInAvvio { // proprio lui, non insieme a un altro errore
			t.Errorf("stop durante l'avvio: %v, atteso errFermatoInAvvio", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve appeso dopo lo stop")
	}
}

// inventario: ADM-20
// Un errore vero resta un errore anche se lo stop è già arrivato: un file di configurazione
// rovinato ferma serve con il suo messaggio, non come uno stop.
func TestServeErroreVeroNonEUnoStop(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "apparecchio.conf")
	if err := os.WriteFile(conf, []byte("riga senza uguale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, ferma := context.WithCancel(context.Background())
	ferma()
	err := serve(ctx, opzioniServe{SenzaVolumeCifrato: true, DB: filepath.Join(t.TempDir(), "g.db"), Config: conf,
		Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard}, nil)
	if err == nil || errors.Is(err, errFermatoInAvvio) || !strings.Contains(err.Error(), "configurazione dell'apparecchio") {
		t.Errorf("configurazione rovinata con lo stop già arrivato: %v", err)
	}
}

// inventario: ADM-20
// Conta come stop solo l'errore che viene dal contesto di serve finito, e solo da lui: un altro
// errore con il contesto finito, un «context canceled» con il contesto ancora vivo, o lo stop
// insieme a un errore vero (la copia di backup da togliere a mano) restano errori.
func TestFermatoOErrore(t *testing.T) {
	vivo := context.Background()
	finito, ferma := context.WithCancel(context.Background())
	ferma()
	vero := errors.New("disco pieno")
	casi := []struct {
		nome    string
		ctx     context.Context
		err     error
		fermato bool
	}{
		{"annullato, errore del contesto", finito, context.Canceled, true},
		{"annullato, errore del contesto avvolto", finito, fmt.Errorf("salvataggio della chiave del server: %w", context.Canceled), true},
		{"annullato, errore del contesto avvolto due volte", finito,
			fmt.Errorf("migrazione 0002: %w", fmt.Errorf("backup non verificato: %w", context.Canceled)), true},
		{"annullato, solo errori del contesto insieme", finito, errors.Join(context.Canceled, fmt.Errorf("x: %w", context.Canceled)), true},
		{"annullato, errore vero", finito, vero, false},
		{"annullato, lo stop insieme a un errore vero", finito, errors.Join(
			fmt.Errorf("backup prima della migrazione 0002 non riuscito, non migro: %w", context.Canceled),
			fmt.Errorf("copia non riuscita gestrip.db.prima-di-0002 da togliere a mano: %w", os.ErrPermission)), false},
		{"annullato, due %w di cui uno vero", finito, fmt.Errorf("%w e %w", context.Canceled, vero), false},
		{"annullato, errore che avvolge nil", finito, avvolgeNiente{}, false},
		{"vivo, errore del contesto", vivo, context.Canceled, false},
		{"vivo, errore vero", vivo, vero, false},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			got := fermatoOErrore(c.ctx, c.err)
			if c.fermato && got != errFermatoInAvvio {
				t.Errorf("%v: atteso errFermatoInAvvio", got)
			}
			if !c.fermato && got != c.err {
				t.Errorf("%v: atteso %v invariato", got, c.err)
			}
		})
	}
}

// avvolgeNiente è un errore con Unwrap che dà nil: non viene dal contesto.
type avvolgeNiente struct{}

func (avvolgeNiente) Error() string { return "avvolge niente" }
func (avvolgeNiente) Unwrap() error { return nil }
