package main

import (
	"bytes"
	"context"
	"flag"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/apparecchio"
)

// sistemaSintetico prepara un /proc e un /sys finti in cui il montaggio vero della cartella dir
// è del dispositivo 253:0, cifrato con LUKS2 o in chiaro.
func sistemaSintetico(t *testing.T, dir string, cifrato bool) string {
	t.Helper()
	radice := t.TempDir()
	reale, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	scrivi := func(rel, testo string) {
		p := filepath.Join(radice, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(testo), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Il montaggio della cartella è quello vero (fdinfo): il mountinfo finto lo dà a 253:0.
	f, err := os.Open(reale)
	if err != nil {
		t.Fatal(err)
	}
	id, err := apparecchio.IdMontaggioDaFdinfo(f)
	_ = f.Close() // in sola lettura
	if err != nil {
		t.Fatal(err)
	}
	scrivi("proc/self/mountinfo", "0 0 8:2 / / rw shared:1 - ext4 /dev/sda2 rw\n"+
		id+" 1 253:0 / "+reale+" rw shared:5 - ext4 /dev/mapper/dati rw\n")
	uuid := "LVM-sintetico"
	if cifrato {
		uuid = "CRYPT-LUKS2-0123456789abcdef0123456789abcdef-dati"
	}
	scrivi("sys/dev/block/253:0/dm/uuid", uuid+"\n")
	scrivi("sys/dev/block/253:0/dm/name", "dati\n")
	if err := os.MkdirAll(filepath.Join(radice, "sys/dev/block/253:0/slaves"), 0o755); err != nil {
		t.Fatal(err)
	}
	return radice
}

// avviaServe fa partire serve con o, e lo ferma appena è pronto.
func avviaServe(t *testing.T, o opzioniServe) error {
	t.Helper()
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	o.Interna, o.Pubblica = "127.0.0.1:0", "127.0.0.1:0"
	finito := make(chan error, 1)
	pronto := make(chan struct{})
	go func() { finito <- serve(ctx, o, func(net.Addr, net.Addr) { close(pronto) }) }()
	select {
	case <-pronto:
		ferma()
		return <-finito
	case err := <-finito:
		return err
	}
}

// inventario: OPS-12
// serve non parte se la cartella dei dati non sta su un volume cifrato, e non crea il database
// in chiaro; il rifiuto non nomina l'opzione che spegne il controllo. Sul volume cifrato parte.
func TestServeSoloSulVolumeCifrato(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "gestrip.db")
	var avvisi bytes.Buffer
	err := avviaServe(t, opzioniServe{DB: db, radiceSistema: sistemaSintetico(t, dir, false), avvisi: &avvisi})
	if err == nil || !strings.Contains(err.Error(), "volume cifrato") || strings.Contains(err.Error(), "senza-volume-cifrato") {
		t.Errorf("volume in chiaro: %v", err)
	}
	if _, e := os.Stat(db); e == nil {
		t.Error("sul volume in chiaro ha creato il database")
	}
	if err := avviaServe(t, opzioniServe{DB: db, radiceSistema: sistemaSintetico(t, dir, true), avvisi: &avvisi}); err != nil {
		t.Errorf("volume cifrato: %v", err)
	}
	if avvisi.Len() != 0 {
		t.Errorf("avvisi con il controllo acceso: %q", avvisi.String())
	}
	// Il percorso relativo si controlla dalla cartella vera.
	t.Chdir(dir)
	if err := avviaServe(t, opzioniServe{DB: "gestrip.db", radiceSistema: sistemaSintetico(t, dir, false)}); err == nil {
		t.Error("percorso relativo sul volume in chiaro accettato")
	}
}

// inventario: OPS-12
// -senza-volume-cifrato (sviluppo e banco) salta il controllo, ma lo dice forte all'avvio. Di
// default il controllo è acceso.
func TestServeSenzaVolumeCifrato(t *testing.T) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	o := opzioniDiServe(fs)
	if err := fs.Parse(nil); err != nil || o.SenzaVolumeCifrato {
		t.Errorf("di default il controllo è spento: %v", err)
	}
	fs = flag.NewFlagSet("serve", flag.ContinueOnError)
	o = opzioniDiServe(fs)
	if err := fs.Parse([]string{"-senza-volume-cifrato"}); err != nil || !o.SenzaVolumeCifrato {
		t.Errorf("-senza-volume-cifrato: %v %v", o.SenzaVolumeCifrato, err)
	}
	dir := t.TempDir()
	var avvisi bytes.Buffer
	// Il /proc finto è vuoto: con il controllo spento non si legge.
	err := avviaServe(t, opzioniServe{DB: filepath.Join(dir, "gestrip.db"), SenzaVolumeCifrato: true,
		radiceSistema: t.TempDir(), avvisi: &avvisi})
	if err != nil {
		t.Fatal(err)
	}
	if a := avvisi.String(); !strings.Contains(a, "ATTENZIONE") || !strings.Contains(a, "-senza-volume-cifrato") ||
		!strings.Contains(a, "mai sull'apparecchio") {
		t.Errorf("avviso: %q", a)
	}
}

// inventario: OPS-13
// SECRET_KEY_BASE o LEGACY_WARRANTY_SECRET nell'ambiente fermano serve prima di tutto il resto:
// niente database, niente facce.
func TestServeRifiutaISegretiNellAmbiente(t *testing.T) {
	for _, nome := range []string{"SECRET_KEY_BASE", "LEGACY_WARRANTY_SECRET"} {
		t.Run(nome, func(t *testing.T) {
			t.Setenv(nome, strings.Repeat("x", 64))
			db := filepath.Join(t.TempDir(), "gestrip.db")
			err := avviaServe(t, opzioniServe{DB: db, SenzaVolumeCifrato: true, avvisi: &bytes.Buffer{}})
			if err == nil || !strings.Contains(err.Error(), nome+" non è accettata") {
				t.Errorf("%s: %v", nome, err)
			}
			if _, e := os.Stat(db); e == nil {
				t.Error("con il segreto nell'ambiente ha creato il database")
			}
		})
	}
}

// inventario: OPS-13
// Come runtime.exs, che vale per ogni avvio del rilascio: anche reset-link, invito e primo-titolare
// si fermano con un segreto come valore nell'ambiente, prima di toccare il database e senza
// stampare un link.
func TestComandiRifiutanoISegretiNellAmbiente(t *testing.T) {
	t.Setenv("SECRET_KEY_BASE", strings.Repeat("x", 64))
	p := filepath.Join(t.TempDir(), "manca.db")
	var out bytes.Buffer
	if err := resetLink(context.Background(), opzioniReset{DB: p, URL: "https://negozio.esempio.ts.net"}, &out); err == nil ||
		!strings.Contains(err.Error(), "SECRET_KEY_BASE non è accettata") || out.Len() > 0 {
		t.Errorf("reset-link: %v %q", err, out.String())
	}
	if err := invito(context.Background(), opzioniInvito{DB: p, URL: "https://negozio.esempio.ts.net", Email: "nuovo@esempio.it"}, &out); err == nil ||
		!strings.Contains(err.Error(), "SECRET_KEY_BASE non è accettata") || out.Len() > 0 {
		t.Errorf("invito: %v %q", err, out.String())
	}
	if err := primoTitolare(context.Background(), opzioniPrimo{DB: p, URL: "https://negozio.esempio.ts.net"}, &out); err == nil ||
		!strings.Contains(err.Error(), "SECRET_KEY_BASE non è accettata") || out.Len() > 0 {
		t.Errorf("primo-titolare: %v %q", err, out.String())
	}
}

// inventario: OPS-12
// Con la cartella dei dati sul volume cifrato, un gestrip.db che è un link verso una cartella in
// chiaro ferma serve, e il bersaglio non nasce (prima il controllo guardava solo la cartella).
func TestServeRifiutaIlDatabaseLink(t *testing.T) {
	dir := t.TempDir()
	bersaglio := filepath.Join(t.TempDir(), "in-chiaro.db")
	db := filepath.Join(dir, "gestrip.db")
	if err := os.Symlink(bersaglio, db); err != nil {
		t.Fatal(err)
	}
	err := avviaServe(t, opzioniServe{DB: db, radiceSistema: sistemaSintetico(t, dir, true), avvisi: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "è un link") {
		t.Errorf("database link: %v", err)
	}
	if _, e := os.Lstat(bersaglio); e == nil {
		t.Error("il bersaglio del link è nato in chiaro")
	}
}

// inventario: OPS-12
// Di default serve controlla il sistema vero («/»): sulla macchina dei test il controllo legge
// mountinfo (parte se il disco è cifrato, altrimenti rifiuta per il volume, mai perché mountinfo
// manca). Il database di default è /var/lib/gestrip/gestrip.db.
func TestServeControllaIlSistemaVero(t *testing.T) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	o := opzioniDiServe(fs)
	if err := fs.Parse(nil); err != nil || o.DB != "/var/lib/gestrip/gestrip.db" {
		t.Errorf("database di default: %q %v", o.DB, err)
	}
	err := avviaServe(t, opzioniServe{DB: filepath.Join(t.TempDir(), "gestrip.db"), avvisi: &bytes.Buffer{}})
	if err != nil && (strings.Contains(err.Error(), "mountinfo non si legge") || !strings.Contains(err.Error(), "volume cifrato")) {
		t.Errorf("sistema vero: %v", err)
	}
}
