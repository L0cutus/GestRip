package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/backup"
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
	if err != nil || o.Porta != backup.PortaRemotaPredefinita {
		t.Errorf("%+v %v", o, err)
	}
}
