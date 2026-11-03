package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// inventario: RIP-S-57
// serve fa girare la pulizia dei media (MediaCleanup) in sottofondo: il giro dell'avvio toglie il
// file in coda che nessuna riga usa, con la sua miniatura, e i temporanei «.upload-» abbandonati;
// allo stop il lavoro esce e serve torna senza errori. La pulizia usa lo stesso archivio dei
// caricamenti (con il suo lucchetto preso il giro aspetta) e la cache delle miniature di serve.
func TestServePuliziaDeiMedia(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	a := media.Nuovo(media.CartellaDi(percorso))
	k, err := a.Salva([]byte("foto inventata da togliere"))
	if err != nil {
		t.Fatal(err)
	}
	mini := filepath.Join(media.MiniatureDi(percorso), k+".webp")
	if err := os.MkdirAll(filepath.Dir(mini), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mini, []byte("RIFF\x04\x00\x00\x00WEBP"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(media.CartellaDi(percorso), ".upload-abbandonato")
	if err := os.WriteFile(tmp, []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := media.Accoda(context.Background(), tx, []string{k}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	ctx, ferma := context.WithCancel(context.Background())
	giro := make(chan struct{}, 1)
	uscito := make(chan struct{})
	preso, lascia := make(chan struct{}), make(chan struct{})
	o := opzioniServe{SenzaVolumeCifrato: true, DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard,
		aspettaConservazione: func(ctx context.Context) bool { <-ctx.Done(); return false },
		aspettaPulizia: func(ctx context.Context) bool {
			giro <- struct{}{}
			<-ctx.Done()
			close(uscito)
			return false
		},
		conMedia: func(ms mediaServe) {
			// Il lucchetto dell'archivio dei caricamenti, preso prima che la pulizia parta.
			go func() {
				_ = ms.archivio.ConLucchetto(func() error {
					close(preso)
					<-lascia
					return nil
				})
			}()
			<-preso
		}}
	finito := make(chan error, 1)
	go func() { finito <- serve(ctx, o, nil) }()
	select {
	case <-giro:
		t.Fatal("giro della pulizia con il lucchetto dei caricamenti preso: non è lo stesso archivio")
	case err := <-finito:
		t.Fatal(err)
	case <-time.After(500 * time.Millisecond):
	}
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatalf("temporaneo tolto con il lucchetto preso: %v", err)
	}
	close(lascia)
	select {
	case <-giro:
	case err := <-finito:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("la pulizia non ha fatto il giro dell'avvio")
	}
	for _, p := range []string{filepath.Join(media.CartellaDi(percorso), k), tmp, mini} {
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("rimasto dopo il giro dell'avvio: %v", err)
		}
	}
	ferma()
	select {
	case err := <-finito:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve non si ferma")
	}
	select {
	case <-uscito:
	default:
		t.Error("serve è tornato prima che la pulizia uscisse")
	}
}
