package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/apparecchio"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// fotoDiProva crea nel database dbp (migrato) una busta con una foto normalizzata dal figlio e ne
// dà busta, foto e chiave.
func fotoDiProva(t *testing.T, d *db.DB, b *buste.Buste, dir string) (int64, int64, string) {
	t.Helper()
	c, err := b.Crea(context.Background(), buste.Accettazione{NomeCliente: "Ada Esempio", TelefonoCliente: "333 111",
		DataAccettazione: "2026-10-31", Descrizione: "Anello"}, audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "in.png")
	if err := os.WriteFile(in, sintetiche.PNG(800, 600, 3), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := b.AllegaFoto(context.Background(), c.ID, []buste.Caricamento{{Percorso: in, NomeOriginale: "in.png"}}, nil,
		audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID, f[0].ID, f[0].Chiave
}

// inventario: RIP-S-33, RIP-S-34
// serve dà alle buste l'archivio accanto al database, il lavoratore (il processo figlio, mai la
// conversione nel processo delle pagine) e la cache delle miniature in «miniature» accanto al
// database: la foto si normalizza nel figlio, la miniatura si fa nel figlio e resta nella cache.
// La cache scrive nel registro di serve (una miniatura non fatta per le risorse, slice 037c).
func TestFotoEMiniatureDelServe(t *testing.T) {
	t.Setenv("GESTRIP_TEST_CPU_FIGLIO", "30") // serve non la legge
	dir := t.TempDir()
	dbp := filepath.Join(dir, "gestrip.db")
	d, err := db.Apri(dbp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	registro := slog.New(slog.NewTextHandler(io.Discard, nil)) // un gestore suo: si confronta per identità
	m, err := mediaDelServe(dbp, registro)
	if err != nil {
		t.Fatal(err)
	}
	if m.miniature.Registro == nil || m.miniature.Registro.Handler() != registro.Handler() {
		t.Error("la cache delle miniature non scrive nel registro di serve")
	}
	if m.archivio.Cartella() != filepath.Join(dir, "media") || m.miniature.Cartella() != filepath.Join(dir, "miniature") {
		t.Errorf("cartelle %q %q", m.archivio.Cartella(), m.miniature.Cartella())
	}
	if e, err := os.Executable(); err != nil || m.lavoratore.Eseguibile != e {
		t.Errorf("lavoratore %q", m.lavoratore.Eseguibile)
	}
	// I limiti di produzione del figlio (slice 037e): CPU e tempo 0 sono LimitiDelFiglio (8 s di
	// CPU) e TempoMassimo (10 s), anche con la variabile dei test nell'ambiente.
	if m.lavoratore.CPU != 0 || m.lavoratore.Tempo != 0 || imaging.LimitiDelFiglio.CPU != 8 || imaging.TempoMassimo != 10*time.Second {
		t.Errorf("lavoratore di serve: CPU %d (produzione %d), tempo %v", m.lavoratore.CPU, imaging.LimitiDelFiglio.CPU, m.lavoratore.Tempo)
	}
	dip := dipendenzeWeb(d, make([]byte, 32), apparecchio.Configurazione{}, opzioniServe{DB: dbp}, m)
	b, ok := dip.Buste.(*buste.Buste)
	if !ok || len(dip.ChiaveGalleria) < 32 {
		t.Fatalf("buste %T, chiave della galleria di %d byte", dip.Buste, len(dip.ChiaveGalleria))
	}
	busta, foto, chiave := fotoDiProva(t, d, b, dir)
	mf, err := b.LeggiFotoPersonale(context.Background(), itoa(busta), itoa(foto), buste.VarianteMiniatura, audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil || mf == nil || mf.Variante != buste.VarianteMiniatura {
		t.Fatalf("miniatura %+v %v", mf, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "miniature", chiave+".webp")); err != nil {
		t.Errorf("voce della cache: %v", err)
	}
}

// inventario: RIP-S-34
// All'avvio serve fa partire il riscaldatore (ThumbnailWarmer, acceso in produzione): pulisce la
// cache e prepara le miniature delle foto che non le hanno.
func TestServePreparaLeMiniature(t *testing.T) {
	dir := t.TempDir()
	dbp := filepath.Join(dir, "gestrip.db")
	d, err := db.Apri(dbp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	m, err := mediaDelServe(dbp, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := buste.Nuove(d, clock.Sistema{}).ConFoto(m.archivio, m.lavoratore)
	_, _, chiave := fotoDiProva(t, d, b, dir)
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	orfana := filepath.Join(dir, "miniature", "ab"+chiave[2:]+".webp")
	if err := os.MkdirAll(filepath.Dir(orfana), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orfana, []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	finito := make(chan error, 1)
	pronto := make(chan struct{})
	o := opzioniServe{DB: dbp, SenzaVolumeCifrato: true, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: &bytesScarto{}}
	go func() { finito <- serve(ctx, o, func(net.Addr, net.Addr) { close(pronto) }) }()
	select {
	case <-pronto:
	case err := <-finito:
		t.Fatal(err)
	}
	voce := filepath.Join(dir, "miniature", chiave+".webp")
	scadenza := time.Now().Add(20 * time.Second)
	for time.Now().Before(scadenza) {
		if _, err := os.Lstat(voce); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Lstat(voce); err != nil {
		t.Errorf("miniatura non preparata all'avvio: %v", err)
	}
	if _, err := os.Lstat(orfana); err == nil {
		t.Error("voce orfana non tolta all'avvio")
	}
	ferma()
	if err := <-finito; err != nil {
		t.Fatal(err)
	}
}

type bytesScarto struct{}

func (bytesScarto) Write(p []byte) (int, error) { return len(p), nil }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
