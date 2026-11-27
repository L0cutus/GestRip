package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// L'ora del backup scelta dal titolare (ora.go, slice 133b, D-115): SalvaOra, la regola del passaggio all'ora nuova
// nel calendario (statoTurno) e l'allarme delle 26 ore, che il cambio non tocca. Storico e registro scritti a mano, senza
// backup veri; orologio finto; dati inventati.

var titolareOra = audit.Contesto{Attore: "titolare@esempio.it"}

// buonoSullaChiavetta scrive nel registro un backup verificato sulla chiavetta catturato a at.
func buonoSullaChiavetta(t *testing.T, d *db.DB, at time.Time) {
	t.Helper()
	if err := db.RegistraVerificaBackup(context.Background(), d, db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneChiavetta,
		Archivio: "/chiavetta/inventato-" + at.Format("20060102T150405Z") + ".tar.age", SHA256: strings.Repeat("c", 64), Byte: 1,
		CatturatoIl: at, VerificatoIl: at.Add(time.Minute), Conteggi: map[string]int64{"buste": 1}}); err != nil {
		t.Fatal(err)
	}
}

// oraFissa scrive l'ora del backup come se ci fosse da prima (senza cambio).
func oraFissa(t *testing.T, d *db.DB, v string) {
	t.Helper()
	if _, err := d.Scrittura.ExecContext(context.Background(), `INSERT INTO impostazioni (chiave, valore, aggiornata_il)
		VALUES (?, ?, '2026-11-01T08:00:00.000000Z')`, ChiaveOra, v); err != nil {
		t.Fatal(err)
	}
}

func salvaAlle(t *testing.T, pn *provaNotturno, at time.Time, v string) EsitoOra {
	t.Helper()
	pn.o.Imposta(at)
	es, err := pn.n.SalvaOra(context.Background(), v, titolareOra)
	if err != nil {
		t.Fatalf("salva %s: %v", v, err)
	}
	return es
}

func valore(t *testing.T, d *db.DB, chiave string) string {
	t.Helper()
	var v string
	if err := d.Lettura.QueryRow(`SELECT coalesce((SELECT valore FROM impostazioni WHERE chiave = ?), '')`, chiave).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func contaAudit(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit WHERE azione = 'backup.schedule_updated'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func problemiAlle(t *testing.T, pn *provaNotturno, at time.Time) []string {
	t.Helper()
	pn.o.Imposta(at)
	s, err := pn.n.Stato(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return codici(s.Problemi())
}

// Solo «HH:MM» fra 00:00 e 23:59: ogni altra forma è ErrOraNonValida e non scrive niente (né l'ora, né il cambio, né
// l'audit). Un'ora valida scrive l'ora, il cambio e una riga backup.schedule_updated con chi, da e a; la stessa ora
// di nuovo non scrive niente.
func TestSalvaOra(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
	d := pn.p.s.DB
	for _, v := range []string{"", "2:30", "24:00", "02:60", "02:30:00", "ab:cd", "+2:30", "-1:30", "0230", "02-30", "02:3", "１２:00", " 02:30", "02:+5", "+2:05"} {
		if _, err := pn.n.SalvaOra(context.Background(), v, titolareOra); !errors.Is(err, ErrOraNonValida) {
			t.Errorf("%q: %v", v, err)
		}
	}
	if valore(t, d, ChiaveOra) != "" || valore(t, d, ChiaveCambioOra) != "" || contaAudit(t, d) != 0 {
		t.Fatal("un'ora non valida ha scritto qualcosa")
	}
	if es := salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), "02:30"); es.Cambiata || es.Ora != OraPredefinita {
		t.Errorf("le 02:30 di sempre: %+v", es)
	}
	if valore(t, d, ChiaveOra) != "" || contaAudit(t, d) != 0 {
		t.Fatal("la stessa ora ha scritto qualcosa")
	}
	for _, v := range []string{"00:00", "23:59", "23:00"} {
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), v); !es.Cambiata || TestoOra(es.Ora) != v {
			t.Errorf("%s: %+v", v, es)
		}
		if valore(t, d, ChiaveOra) != v {
			t.Errorf("%s: scritta %q", v, valore(t, d, ChiaveOra))
		}
	}
	if o, err := pn.n.Ora(context.Background()); err != nil || o != 23*time.Hour {
		t.Errorf("ora letta: %v %v", o, err)
	}
	cambio := valore(t, d, ChiaveCambioOra)
	if es := salvaAlle(t, pn, alle(2026, 11, 25, 11, 0), "23:00"); es.Cambiata {
		t.Errorf("23:00 di nuovo: %+v", es)
	}
	if valore(t, d, ChiaveCambioOra) != cambio || contaAudit(t, d) != 3 {
		t.Errorf("23:00 di nuovo ha riscritto: %q, audit %d", valore(t, d, ChiaveCambioOra), contaAudit(t, d))
	}
	var attore, dati string
	if err := d.Lettura.QueryRow(`SELECT attore, dati FROM eventi_audit WHERE azione = 'backup.schedule_updated' ORDER BY id DESC LIMIT 1`).
		Scan(&attore, &dati); err != nil {
		t.Fatal(err)
	}
	if attore != titolareOra.Attore || !strings.Contains(dati, `"from":"23:59"`) || !strings.Contains(dati, `"to":"23:00"`) ||
		!strings.Contains(dati, `"due_now":true`) {
		t.Errorf("audit: %q %s", attore, dati)
	}
}

// inventario: OPS-28
// Voce OPS-28 (calendario del backup, D-115: l'ora modificabile dal titolare). Il passaggio all'ora nuova, sui bordi
// della ricevuta 133b: il primo backup all'ora nuova al più 25 ore dopo l'ultimo verificato, altrimenti subito; mai
// saltata la notte; l'allarme delle 26 ore uguale a prima.
func TestCambioOraBordi(t *testing.T) {
	t.Run("02:30 → 23:00 alle 10:00, dopo il backup della notte", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		riga(t, d, AvvioAutomatico, "2026-11-24", EsitoOK, alle(2026, 11, 24, 2, 35))
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 2, 35))
		buonoSullaChiavetta(t, d, alle(2026, 11, 25, 2, 31))
		// Un tentativo riuscito di stanotte senza riga (contato in memoria): è di prima del cambio, non conta.
		n.ricordaNonScritta(chiaveBackup(clock.DataDi(alle(2026, 11, 25, 12, 0))), true, alle(2026, 11, 25, 2, 40))
		es := salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), "23:00")
		if es.Subito || !es.Prossimo.Equal(alle(2026, 11, 25, 23, 0)) {
			t.Fatalf("esito: %+v", es)
		}
		for _, at := range []time.Time{alle(2026, 11, 25, 10, 0), alle(2026, 11, 25, 22, 59)} {
			if dovutoAlle(t, n, o, at) {
				t.Errorf("%v: dovuto", at)
			}
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 25, 23, 0)) {
			t.Error("23:00 di stasera: non dovuto (il backup delle 02:30 di stanotte non vale per l'ora nuova)")
		}
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 23, 5))
		if dovutoAlle(t, n, o, alle(2026, 11, 26, 2, 30)) || dovutoAlle(t, n, o, alle(2026, 11, 26, 22, 59)) {
			t.Error("dopo il backup delle 23:00: dovuto prima delle 23:00 di domani")
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 26, 23, 0)) {
			t.Error("23:00 di domani: non dovuto")
		}
	})
	t.Run("23:00 → 01:00 alle 10:00", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		oraFissa(t, d, "23:00")
		riga(t, d, AvvioAutomatico, "2026-11-24", EsitoOK, alle(2026, 11, 24, 23, 5))
		buonoSullaChiavetta(t, d, alle(2026, 11, 24, 23, 1))
		// Le 01:00 di stanotte sarebbero 26 ore dopo l'ultimo backup: il backup non farebbe in tempo a verificarsi prima
		// dell'allarme. Si fa adesso, poi alle 01:00.
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), "01:00"); !es.Subito || !es.Prossimo.IsZero() {
			t.Fatalf("esito: %+v", es)
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 25, 10, 0)) {
			t.Fatal("10:00: non dovuto")
		}
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 10, 6))
		if dovutoAlle(t, n, o, alle(2026, 11, 25, 23, 0)) || dovutoAlle(t, n, o, alle(2026, 11, 26, 0, 59)) ||
			!dovutoAlle(t, n, o, alle(2026, 11, 26, 1, 0)) {
			t.Error("dopo il backup subito, il prossimo non è alle 01:00 di stanotte")
		}
	})
	t.Run("23:00 → 01:00 alle 23:30, dopo il backup della sera", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 23, 30))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		oraFissa(t, d, "23:00")
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 23, 5))
		buonoSullaChiavetta(t, d, alle(2026, 11, 25, 23, 1))
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 23, 30), "01:00"); es.Subito || !es.Prossimo.Equal(alle(2026, 11, 26, 1, 0)) {
			t.Fatalf("esito: %+v", es)
		}
		if dovutoAlle(t, n, o, alle(2026, 11, 25, 23, 30)) || !dovutoAlle(t, n, o, alle(2026, 11, 26, 1, 0)) {
			t.Error("la prima 01:00 dopo il cambio non è la prima da fare")
		}
	})
	t.Run("02:30 → 01:00 alle 01:30, prima del backup della notte", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 1, 30))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		riga(t, d, AvvioAutomatico, "2026-11-24", EsitoOK, alle(2026, 11, 24, 2, 35))
		buonoSullaChiavetta(t, d, alle(2026, 11, 24, 2, 31))
		// Le 01:00 nuove di stanotte sono passate e l'ultimo backup è di 23 ore prima: aspettare le 01:00 di domani
		// salterebbe la notte. Si fa adesso.
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 1, 30), "01:00"); !es.Subito || !es.Prossimo.IsZero() {
			t.Fatalf("esito: %+v", es)
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 25, 1, 30)) {
			t.Fatal("01:30: non dovuto")
		}
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 1, 36))
		if dovutoAlle(t, n, o, alle(2026, 11, 25, 2, 30)) || dovutoAlle(t, n, o, alle(2026, 11, 26, 0, 59)) {
			t.Error("dopo il backup di recupero: dovuto prima delle 01:00 di domani")
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 26, 1, 0)) {
			t.Error("01:00 di domani: non dovuto")
		}
	})
	t.Run("02:30 → 06:00 alle 10:00", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 2, 35))
		buonoSullaChiavetta(t, d, alle(2026, 11, 25, 2, 31))
		// Le 06:00 di domani sono 27 ore e mezza dopo l'ultimo backup: subito.
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), "06:00"); !es.Subito {
			t.Fatalf("esito: %+v", es)
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 25, 10, 0)) {
			t.Error("10:00: non dovuto")
		}
	})
	t.Run("lunedì 23:00, martedì alle 11:01 si passa alle 11:00", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 11, 1))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		oraFissa(t, d, "23:00")
		riga(t, d, AvvioAutomatico, "2026-11-23", EsitoOK, alle(2026, 11, 23, 23, 5))
		buonoSullaChiavetta(t, d, alle(2026, 11, 23, 23, 1))
		// Le 11:00 di mercoledì sarebbero 36 ore dopo: subito (revisione della 133b).
		if es := salvaAlle(t, pn, alle(2026, 11, 24, 11, 1), "11:00"); !es.Subito {
			t.Fatalf("esito: %+v", es)
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 24, 11, 1)) {
			t.Error("11:01: non dovuto")
		}
	})
	t.Run("al limite delle 25 ore e un minuto oltre", func(t *testing.T) {
		for _, c := range []struct {
			ora    string
			subito bool
		}{{"03:30", false}, {"03:31", true}} {
			pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
			d, n, o := pn.p.s.DB, pn.n, pn.o
			riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 2, 35))
			buonoSullaChiavetta(t, d, alle(2026, 11, 25, 2, 30))
			es := salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), c.ora)
			if es.Subito != c.subito || dovutoAlle(t, n, o, alle(2026, 11, 25, 10, 0)) != c.subito {
				t.Errorf("%s: %+v", c.ora, es)
			}
			if !c.subito && !es.Prossimo.Equal(alle(2026, 11, 26, 3, 30)) {
				t.Errorf("%s: prossimo %v", c.ora, es.Prossimo)
			}
		}
	})
	t.Run("cambio fatto esattamente all'ora nuova", func(t *testing.T) {
		// 02:30 → 03:00 alle 03:00 in punto, dopo il backup delle 02:30: la prima ora nuova è domani alle 03:00 (24 ore
		// e mezza dopo), non adesso.
		pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 3, 0))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 2, 35))
		buonoSullaChiavetta(t, d, alle(2026, 11, 25, 2, 31))
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 3, 0), "03:00"); es.Subito || !es.Prossimo.Equal(alle(2026, 11, 26, 3, 0)) {
			t.Fatalf("esito: %+v", es)
		}
		if dovutoAlle(t, n, o, alle(2026, 11, 25, 3, 0)) || !dovutoAlle(t, n, o, alle(2026, 11, 26, 3, 0)) {
			t.Error("03:00 in punto: non il backup di domani alle 03:00")
		}
		// 02:30 → 23:00 alle 23:00 in punto: le 23:00 di domani sarebbero 44 ore dopo, quindi adesso.
		pn = nuovaProvaNotturno(t, alle(2026, 11, 25, 23, 0))
		d, n, o = pn.p.s.DB, pn.n, pn.o
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 2, 35))
		buonoSullaChiavetta(t, d, alle(2026, 11, 25, 2, 31))
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 23, 0), "23:00"); !es.Subito || !dovutoAlle(t, n, o, alle(2026, 11, 25, 23, 0)) {
			t.Errorf("23:00 in punto: %+v", es)
		}
	})
	t.Run("domenica alle 10:00 dalle 23:00 alle 02:30 (voce 18(c))", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 29, 10, 0))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		oraFissa(t, d, "23:00")
		riga(t, d, AvvioAutomatico, "2026-11-28", EsitoOK, alle(2026, 11, 28, 23, 5))
		buonoSullaChiavetta(t, d, alle(2026, 11, 28, 23, 1))
		rigaProva(t, d, db.AvvioProvaAutomatico, "2026-11-22", true, alle(2026, 11, 22, 23, 20))
		if es := salvaAlle(t, pn, alle(2026, 11, 29, 10, 0), "02:30"); !es.Subito {
			t.Fatalf("esito: %+v", es)
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 29, 10, 0)) || provaDovutaAlle(t, n, o, alle(2026, 11, 29, 10, 0)) {
			t.Fatal("prima il backup, poi la prova")
		}
		riga(t, d, AvvioAutomatico, "2026-11-29", EsitoOK, alle(2026, 11, 29, 10, 6))
		if !provaDovutaAlle(t, n, o, alle(2026, 11, 29, 10, 7)) {
			t.Error("la prova della domenica non parte dopo il backup subito")
		}
	})
	t.Run("l'allarme suona oltre le 26 ore anche dopo un cambio", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
		d := pn.p.s.DB
		riga(t, d, AvvioAutomatico, "2026-11-25", EsitoOK, alle(2026, 11, 25, 2, 35))
		buonoSullaChiavetta(t, d, alle(2026, 11, 25, 2, 31))
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), "23:00"); es.Subito {
			t.Fatalf("esito: %+v", es)
		}
		// Il backup delle 23:00 non arriva.
		if slices.Contains(problemiAlle(t, pn, alle(2026, 11, 26, 4, 31)), ProblemaBackupVecchio) {
			t.Error("26 ore esatte: allarme")
		}
		if !slices.Contains(problemiAlle(t, pn, alle(2026, 11, 26, 4, 32)), ProblemaBackupVecchio) {
			t.Error("26 ore e un minuto: niente allarme")
		}
	})
	t.Run("senza un backup recente", func(t *testing.T) {
		pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
		d, n, o := pn.p.s.DB, pn.n, pn.o
		riga(t, d, AvvioAutomatico, "2026-11-23", EsitoOK, alle(2026, 11, 23, 2, 35))
		buonoSullaChiavetta(t, d, alle(2026, 11, 23, 2, 31))
		// Tre tentativi falliti stanotte: il turno è chiuso, ma il backup è vecchio. Cambiare l'ora lo riapre.
		for _, fine := range []time.Time{alle(2026, 11, 25, 2, 35), alle(2026, 11, 25, 3, 5), alle(2026, 11, 25, 3, 35)} {
			riga(t, d, AvvioAutomatico, "2026-11-25", EsitoFallito, fine)
		}
		if es := salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), "23:00"); !es.Subito {
			t.Fatalf("esito: %+v", es)
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 25, 10, 0)) {
			t.Error("backup vecchio: non dovuto subito")
		}
		if !slices.Contains(problemiAlle(t, pn, alle(2026, 11, 25, 10, 0)), ProblemaBackupVecchio) {
			t.Error("backup vecchio: l'allarme tace dopo il cambio")
		}
	})
}

// Voce OPS-28 (vedi TestCambioOraBordi).
// L'ora legale: sabato 28/03/2026 le 02:30 diventano 02:15; domenica 29/03 le 02:15 non esistono (alle 02:00 si va
// alle 03:00) e il backup parte alle 03:00 CEST, non alle 01:59 CET. La prova settimanale resta agganciata al
// backup della domenica, all'ora nuova: dopo di lui, non prima.
func TestCambioOraLegale(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 3, 28, 10, 0))
	d, n, o := pn.p.s.DB, pn.n, pn.o
	riga(t, d, AvvioAutomatico, "2026-03-28", EsitoOK, alle(2026, 3, 28, 2, 35))
	buonoSullaChiavetta(t, d, alle(2026, 3, 28, 2, 31))
	rigaProva(t, d, db.AvvioProvaAutomatico, "2026-03-22", true, alle(2026, 3, 22, 2, 50))
	if es := salvaAlle(t, pn, alle(2026, 3, 28, 10, 0), "02:15"); es.Subito || clock.DataDi(es.Prossimo).String() != "2026-03-29" {
		t.Fatalf("esito: %+v", es)
	}
	prima, dopo := time.Date(2026, 3, 29, 0, 59, 0, 0, time.UTC), time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)
	if dovutoAlle(t, n, o, alle(2026, 3, 28, 10, 0)) || dovutoAlle(t, n, o, prima) {
		t.Error("dovuto prima delle 03:00 CEST")
	}
	if !dovutoAlle(t, n, o, dopo) {
		t.Fatal("03:00 CEST: non dovuto")
	}
	if provaDovutaAlle(t, n, o, dopo) {
		t.Error("prova prima del backup della domenica")
	}
	riga(t, d, AvvioAutomatico, "2026-03-29", EsitoOK, dopo.Add(5*time.Minute))
	if !provaDovutaAlle(t, n, o, dopo.Add(6*time.Minute)) {
		t.Error("prova non dovuta dopo il backup della domenica all'ora nuova")
	}
}

// Voce OPS-28 (vedi TestCambioOraBordi).
// Sulla copia isolata (nessuna chiavetta, lavoro notturno spento, come serve la fa partire) l'ora si salva ma non fa
// partire niente: nessuna esecuzione, nessun lucchetto dei backup, nessun allarme, niente dovuto.
func TestSalvaOraCopiaIsolata(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
	d := pn.p.s.DB
	iso := NuovoNotturno(d, pn.p.s.Archivio, "", pn.o).ConLavoroSpento()
	lucchetto := filepath.Join(filepath.Dir(d.Percorso()), ".lucchetto-backup")
	_ = os.Remove(lucchetto)
	es, err := iso.SalvaOra(context.Background(), "23:00", titolareOra)
	if err != nil || !es.Cambiata || !es.Subito {
		t.Fatalf("salva: %+v %v", es, err)
	}
	for _, at := range []time.Time{alle(2026, 11, 25, 10, 0), alle(2026, 11, 25, 23, 0)} {
		if dovutoAlle(t, iso, pn.o, at) {
			t.Errorf("%v: dovuto sulla copia isolata", at)
		}
		if _, fatto, err := iso.Giro(context.Background()); fatto || err != nil {
			t.Errorf("%v: giro %v %v", at, fatto, err)
		}
	}
	if s, err := iso.Stato(context.Background()); err != nil || len(s.Allarme()) != 0 || s.Ora != 23*time.Hour {
		t.Errorf("stato: %+v %v", s, err)
	}
	if n := len(pn.righeStorico(t)); n != 0 {
		t.Errorf("esecuzioni: %d", n)
	}
	if _, err := os.Stat(lucchetto); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("lucchetto dei backup preso: %v", err)
	}
}

// Una riga del cambio che non si legge non ferma i backup: il calendario resta quello della 124.
func TestCambioOraRovinato(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
	d, n, o := pn.p.s.DB, pn.n, pn.o
	riga(t, d, AvvioAutomatico, "2026-11-24", EsitoOK, alle(2026, 11, 24, 2, 35))
	for _, v := range []string{"rovinato", `{"il":"ieri","dal":"2026-11-30"}`, `{"il":"2026-11-25T09:00:00.000000Z","dal":"30/11"}`,
		`{"il":"2026-11-25T09:00:00.000000Z","dal":"2026-11-30","previsto":"presto"}`} {
		if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, '2026-11-25T09:00:00.000000Z')
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, ChiaveCambioOra, v); err != nil {
			t.Fatal(err)
		}
		if !dovutoAlle(t, n, o, alle(2026, 11, 25, 10, 0)) {
			t.Errorf("%s: il turno di oggi non è da fare", v)
		}
	}
}

// SalvaOra dimentica i tentativi di backup contati solo in memoria (sono dell'ora vecchia), non quelli delle prove.
func TestSalvaOraDimenticaTentativi(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 25, 10, 0))
	turno := clock.DataDi(alle(2026, 11, 25, 12, 0))
	pn.n.ricordaNonScritta(chiaveBackup(turno), false, alle(2026, 11, 25, 2, 40))
	pn.n.ricordaNonScritta(chiaveProva(SettimanaDi(turno)), false, alle(2026, 11, 25, 2, 50))
	salvaAlle(t, pn, alle(2026, 11, 25, 10, 0), "23:00")
	if _, c := pn.n.nonScritte[chiaveBackup(turno)]; c {
		t.Error("tentativo di backup di prima del cambio ancora in memoria")
	}
	if _, c := pn.n.nonScritte[chiaveProva(SettimanaDi(turno))]; !c {
		t.Error("tentativo della prova dimenticato")
	}
}
