package main

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/web"
)

// gestrip serve dà alla faccia interna la pagina dell'assistente (slice 107) sul suo database: salvata
// l'estrazione, il client del modello la legge da lì.
func TestServeAssistente(t *testing.T) {
	var avvisi bytes.Buffer
	var pagina web.Assistente
	var letta assist.Configurazione
	var errPagina error
	o := opzioniServe{SenzaVolumeCifrato: true, DB: filepath.Join(t.TempDir(), "g.db"), Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0",
		avvisi: &avvisi, SenzaLavoriAutomatici: true,
		conDipendenze: func(d web.Dipendenze) {
			pagina = d.Assistente
			if pagina == nil {
				return
			}
			ctx := context.Background()
			rif, err := pagina.Salva(ctx, assist.FamigliaEstrazione, assist.Modulo{"endpoint": "http://127.0.0.1:11434", "model": "m"},
				audit.Contesto{Attore: "titolare@esempio.test"})
			if err != nil || rif != nil {
				errPagina = err
				return
			}
			p, err := pagina.Pagina(ctx)
			errPagina = err
			letta = assist.Configurazione{Indirizzo: p.Estrazione.Indirizzo, Modello: p.Estrazione.Modello}
		},
		aspettaConservazione: func(ctx context.Context) bool { <-ctx.Done(); return false },
		aspettaPulizia:       func(ctx context.Context) bool { <-ctx.Done(); return false },
	}
	ctx, ferma := context.WithCancel(context.Background())
	finito := make(chan error, 1)
	pronto := make(chan struct{})
	go func() { finito <- serve(ctx, o, func(net.Addr, net.Addr) { close(pronto) }) }()
	select {
	case <-pronto:
	case err := <-finito:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve non parte")
	}
	ferma()
	select {
	case err := <-finito:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve non si ferma")
	}
	if _, ok := pagina.(*assist.Impostazioni); !ok {
		t.Fatalf("pagina dell'assistente %T", pagina)
	}
	if errPagina != nil || letta != (assist.Configurazione{Indirizzo: "http://127.0.0.1:11434", Modello: "m"}) {
		t.Errorf("letta %q %q, %v", letta.Indirizzo, letta.Modello, errPagina)
	}
}
