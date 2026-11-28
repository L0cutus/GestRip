package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/sftpfinto"
)

// I comandi della copia remota (slice 128) contro il server SFTP finto: chiave-sftp crea e rilegge la chiave
// dell'apparecchio; backup-remoto mostra l'impronta senza scrivere niente, rifiuta un'impronta diversa, scrive la
// configurazione solo con l'impronta giusta, la chiave accettata e la cartella che c'è, e la toglie con -togli.
func TestComandiCopiaRemota(t *testing.T) {
	p := databaseVuoto(t)
	srv := sftpfinto.Nuovo(t)
	if err := os.Mkdir(srv.Locale("/backup"), 0o700); err != nil {
		t.Fatal(err)
	}
	vecchio := componiRemoto
	componiRemoto = func(r *backup.Remoto) { r.Componi = srv.Componi }
	t.Cleanup(func() { componiRemoto = vecchio })
	ctx := context.Background()

	var out bytes.Buffer
	if err := chiaveSFTP(opzioniChiaveSFTP{DB: p}, &out); err != nil || !strings.Contains(out.String(), "ssh-ed25519 ") ||
		!strings.Contains(out.String(), "creata") {
		t.Fatalf("chiave-sftp: %v %q", err, out.String())
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(out.String()[strings.Index(out.String(), "ssh-ed25519"):]))
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := chiaveSFTP(opzioniChiaveSFTP{DB: p}, &out); err != nil || !strings.Contains(out.String(), "già presente") {
		t.Fatalf("chiave-sftp di nuovo: %v %q", err, out.String())
	}

	o := opzioniBackupRemoto{DB: p, Host: "u1.storage.invalid", Porta: 23, Utente: "u1-sub1", Cartella: "backup"}
	configurata := func() bool {
		d, err := db.Apri(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Chiudi() }()
		_, _, ok, _ := backup.LeggiConfigRemota(ctx, d.Lettura)
		return ok
	}
	out.Reset()
	if err := backupRemoto(ctx, o, &out, oraChiave); !errors.Is(err, errImprontaDaVedere) ||
		!strings.Contains(out.String(), srv.Impronta()) || configurata() {
		t.Fatalf("senza impronta: %v %q", err, out.String())
	}
	o.Impronta = "SHA256:" + strings.Repeat("B", 43)
	out.Reset()
	if err := backupRemoto(ctx, o, &out, oraChiave); !errors.Is(err, backup.ErrImprontaServer) || configurata() ||
		!strings.Contains(out.String(), "IMPRONTA DIVERSA") {
		t.Fatalf("impronta diversa: %v %q", err, out.String())
	}
	o.Impronta = srv.Impronta()
	if err := backupRemoto(ctx, o, &out, oraChiave); backup.CodiceRemoto(err) != backup.CodiceRemotoAccesso || configurata() {
		t.Fatalf("chiave non ancora nel pannello: %v", err)
	}
	srv.Autorizza(pub)
	o.Cartella = "manca"
	if err := backupRemoto(ctx, o, &out, oraChiave); backup.CodiceRemoto(err) != backup.CodiceRemotoCartella || configurata() {
		t.Fatalf("cartella che non c'è: %v", err)
	}
	o.Cartella = "backup"
	out.Reset()
	if err := backupRemoto(ctx, o, &out, oraChiave); err != nil || !configurata() {
		t.Fatalf("configurazione: %v %q", err, out.String())
	}
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	c, _, _, err := backup.LeggiConfigRemota(ctx, d.Lettura)
	_ = d.Chiudi()
	if err != nil || c.Cartella != "/backup" || c.Impronta != srv.Impronta() || c.TipoChiaveServer != ssh.KeyAlgoED25519 || c.Porta != 23 {
		t.Errorf("configurazione scritta %+v %v", c, err)
	}
	if err := backupRemoto(ctx, opzioniBackupRemoto{DB: p, Togli: true}, &out, oraChiave); err != nil || configurata() {
		t.Errorf("togli: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p), "secrets", backup.NomeChiaveSFTP)); err != nil {
		t.Errorf("chiave: %v", err)
	}
}

func TestOpzioniBackupRemoto(t *testing.T) {
	for _, a := range [][]string{
		{}, {"-host", "h"}, {"-host", "h", "-utente", "u"}, {"-togli", "-host", "h"}, {"x"},
	} {
		if _, err := opzioniDiBackupRemoto(a); !errors.Is(err, errUsoBackupRemoto) {
			t.Errorf("%v: %v", a, err)
		}
	}
	o, err := opzioniDiBackupRemoto([]string{"-host", "h", "-utente", "u", "-cartella", "/c"})
	if err != nil || o.Porta != 23 { // la 23 della Storage Box (133c)
		t.Errorf("%+v %v", o, err)
	}
	// L'aiuto di -porta propone la 23 (revisione della 133c).
	var aiuto strings.Builder
	aiutoBackupRemoto = &aiuto
	defer func() { aiutoBackupRemoto = io.Discard }()
	if _, err := opzioniDiBackupRemoto([]string{"-h"}); !errors.Is(err, errUsoBackupRemoto) {
		t.Errorf("-h: %v", err)
	}
	if v := aiuto.String(); !strings.Contains(v, "-porta int") ||
		!strings.Contains(v, "porta SSH del server (per la Storage Box: 23) (default 23)") {
		t.Errorf("aiuto di -porta: %q", v)
	}
}

// La prova della copia remota a mano (slice 129c): gestrip prova-backup -remota scarica dal server finto l'ultima copia
// remota del registro e la verifica per intera (riga «a mano» riuscita); con la copia rovinata sul server fallisce
// (uscita con errore, riga fallita «controllo», niente cartelle temporanee); senza copia remota configurata non scrive
// niente. -remota e -chiavetta insieme, o nessuna delle due, sono un errore d'uso.
func TestProvaBackupRemotaComando(t *testing.T) {
	for _, a := range [][]string{{"-remota", "-chiavetta", "/x"}, {"-remota", "altro"}, {"-remota=false"}} {
		if _, err := opzioniDiProvaBackup(a); !errors.Is(err, errUsoProvaBackup) {
			t.Errorf("%q: %v", a, err)
		}
	}
	if o, err := opzioniDiProvaBackup([]string{"-remota", "-db", "/d/g.db"}); err != nil || o != (opzioniProvaBackup{DB: "/d/g.db", Remota: true}) {
		t.Errorf("%+v %v", o, err)
	}
	p := databaseConChiave(t)
	dati := filepath.Dir(p)
	ctx := context.Background()
	var out bytes.Buffer
	if err := provaBackup(ctx, opzioniProvaBackup{DB: p, Remota: true}, &out); !errors.Is(err, backup.ErrRemotoNonConfigurato) {
		t.Errorf("non configurata: %v", err)
	}
	leggi := func() (db.ProvaRemota, bool) {
		t.Helper()
		d, err := db.Apri(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Chiudi() }()
		u, ok, err := db.UltimaProvaRemota(ctx, d.Lettura)
		if err != nil {
			t.Fatal(err)
		}
		return u, ok
	}
	if _, ok := leggi(); ok {
		t.Fatal("riga scritta senza configurazione")
	}

	// Un backup vero in una cartella, messo a mano sul server finto e registrato come copia remota.
	dest := t.TempDir()
	if err := faiBackup(ctx, opzioniBackup{DB: p, Cartella: dest}, &out); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(filepath.Join(dest, "gestrip-backup-*.tar.age"))
	if len(m) != 1 {
		t.Fatalf("archivi %v", m)
	}
	v, err := backup.VerificaArchivio(ctx, dati, m[0])
	if err != nil {
		t.Fatal(err)
	}
	catturato, err := time.Parse(db.FormatoIstante, v.CatturatoIl)
	if err != nil {
		t.Fatal(err)
	}
	srv := sftpfinto.Nuovo(t)
	if err := os.Mkdir(srv.Locale("/backup"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(m[0])
	if err != nil {
		t.Fatal(err)
	}
	remoto := "/backup/" + filepath.Base(m[0])
	if err := os.WriteFile(srv.Locale(remoto), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := chiaveSFTP(opzioniChiaveSFTP{DB: p}, &out); err != nil {
		t.Fatal(err)
	}
	firma, err := backup.LeggiChiaveSFTP(dati)
	if err != nil {
		t.Fatal(err)
	}
	srv.Autorizza(firma.PublicKey())
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg := backup.ConfigRemota{Host: "u1.storage.invalid", Porta: 23, Utente: "u1-sub1", Cartella: "/backup",
		TipoChiaveServer: ssh.KeyAlgoED25519, Impronta: srv.Impronta()}
	if err := backup.ScriviConfigRemota(ctx, d, cfg, catturato); err != nil {
		t.Fatal(err)
	}
	if err := db.RegistraVerificaBackup(ctx, d, db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneSFTP,
		Archivio: remoto, SHA256: v.SHA256, Byte: v.ByteCifrati, CatturatoIl: catturato, VerificatoIl: catturato,
		Conteggi: v.Conteggi}); err != nil {
		t.Fatal(err)
	}
	_ = d.Chiudi()
	vecchio, vecchiaComp := orologioProva, componiRemoto
	t.Cleanup(func() { orologioProva, componiRemoto = vecchio, vecchiaComp })
	orologioProva = clock.NuovoFinto(catturato.Add(time.Hour))
	componiRemoto = func(r *backup.Remoto) { r.Componi = srv.Componi }

	out.Reset()
	if err := provaBackup(ctx, opzioniProvaBackup{DB: p, Remota: true}, &out); err != nil ||
		!strings.Contains(out.String(), "scaricata: ") || !strings.Contains(out.String(), "prova della copia remota riuscita") ||
		strings.Contains(out.String(), "storage.invalid") {
		t.Fatalf("riuscita: %v %q", err, out.String())
	}
	if u, ok := leggi(); !ok || !u.Riuscita || u.Avvio != db.AvvioProvaAMano || u.Archivio != remoto || u.SHA256 != v.SHA256 {
		t.Errorf("riga %+v %v", u, ok)
	}
	b[len(b)/2] ^= 0xff
	if err := os.WriteFile(srv.Locale(remoto), b, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = provaBackup(ctx, opzioniProvaBackup{DB: p, Remota: true}, &out)
	if err == nil || backup.CodiceRemoto(err) != backup.CodiceRemotoControllo ||
		!strings.Contains(out.String(), "PROVA DELLA COPIA REMOTA NON RIUSCITA") {
		t.Errorf("rovinata: %v %q", err, out.String())
	}
	if u, ok := leggi(); !ok || u.Riuscita || u.Codice != backup.CodiceRemotoControllo {
		t.Errorf("riga %+v %v", u, ok)
	}
	if r, _ := filepath.Glob(filepath.Join(dati, ".*backup-*")); len(r) != 0 {
		t.Errorf("cartelle temporanee rimaste: %v", r)
	}
}
