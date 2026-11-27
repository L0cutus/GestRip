package web

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// L'ora del backup notturno nella pagina Backup (backup.go, slice 133b, D-115): il modulo, chi lo può mandare e cosa
// scrive. Il backup è quello vero (backup.Notturno) su un database di prova, senza chiavetta e con il lavoro notturno
// spento come nella copia isolata: niente parte. La regola del passaggio all'ora nuova la provano i test di
// internal/backup (ora_linux_test.go). Dati inventati.

const rottaOra = "/settings/backup/ora"

func facciaOraVera(t *testing.T) (*browser, http.Handler, *db.DB, *backup.Notturno) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(martediPromemoria)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	n := backup.NuovoNotturno(d, nil, "", o).ConLavoroSpento()
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Backup = o, ratelimit.Nuovo(o), n
	h := mustInterna(t, dd)
	b := titolareDentro(t, h)
	b.vai("GET", "/", nil, nil)
	return b, h, d, n
}

func oraSalvata(t *testing.T, d *db.DB) string {
	t.Helper()
	var v string
	if err := d.Lettura.QueryRow(`SELECT coalesce((SELECT valore FROM impostazioni WHERE chiave = ?), '')`, backup.ChiaveOra).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func righeAuditOra(t *testing.T, d *db.DB, attore string) int {
	t.Helper()
	return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'backup.schedule_updated' AND attore = '`+attore+`'`)
}

// Voce OPS-28 (il segno sta su backup.TestCambioOraBordi).
// Il titolare vede il campo con le 02:30 e il token, salva le 23:00: ritorno alla pagina con la conferma, il campo e
// la riga «Ora del backup notturno» con le 23:00, l'ora nel database, una riga d'audit sua. La stessa ora di nuovo
// dice «niente da cambiare» e non scrive. Nessun backup parte (niente nello storico).
func TestOraBackupTitolare(t *testing.T) {
	b, _, d, _ := facciaOraVera(t)
	r := b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "pagina", r.corpo, `<form id="backup-hour-form" action="/settings/backup/ora" method="post">`,
		`name="_csrf_token" type="hidden" value="`+b.token+`"`, `name="backup[ora]" value="02:30"`, "Alle 02:30 se non la cambi.")
	r = b.vai("POST", rottaOra, url.Values{"_csrf_token": {b.token}, "backup[ora]": {"23:00"}}, nil)
	if !redirectA(r, rottaPaginaBackup) {
		t.Fatalf("salva: %d %q", r.codice, r.location)
	}
	if oraSalvata(t, d) != "23:00" || righeAuditOra(t, d, "titolare@esempio.it") != 1 {
		t.Fatalf("ora %q, audit %d", oraSalvata(t, d), righeAuditOra(t, d, "titolare@esempio.it"))
	}
	r = b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "dopo", r.corpo, html.EscapeString("Ora del backup notturno salvata: 23:00, tutti i giorni."),
		`name="backup[ora]" value="23:00"`, `<dd id="backup-hour">23:00 (ora italiana, tutti i giorni)</dd>`)
	// Senza chiavetta e con il lavoro spento (come la copia isolata) la conferma non promette backup.
	nonContiene(t, "dopo", r.corpo, "parte entro pochi minuti, poi", "Il primo a quest")
	b.vai("POST", rottaOra, url.Values{"_csrf_token": {b.token}, "backup[ora]": {"23:00"}}, nil)
	r = b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "uguale", r.corpo, html.EscapeString(msgOraUguale))
	if righeAuditOra(t, d, "titolare@esempio.it") != 1 {
		t.Error("la stessa ora ha scritto l'audit")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM esecuzioni_backup`); n != 0 {
		t.Errorf("esecuzioni: %d", n)
	}
}

// Voce OPS-28 (vedi TestOraBackupTitolare).
// Il dipendente torna a / e niente cambia; senza token o con un token sbagliato (anche il titolare) 403; senza sessione
// niente. Un'ora non valida torna alla pagina con «Ora non valida» e niente scritto; spazi intorno all'ora vanno bene.
func TestOraBackupRifiuti(t *testing.T) {
	b, h, d, n := facciaOraVera(t)
	dip := dipendenteDentro(t, h)
	dip.vai("GET", "/", nil, nil)
	if r := dip.vai("POST", rottaOra, url.Values{"_csrf_token": {dip.token}, "backup[ora]": {"23:00"}}, nil); r.codice != 302 || r.location != "/" {
		t.Errorf("dipendente: %d %q", r.codice, r.location)
	}
	for nome, v := range map[string]url.Values{"senza token": {"backup[ora]": {"23:00"}},
		"token sbagliato": {"_csrf_token": {"inventato"}, "backup[ora]": {"23:00"}}} {
		if r := b.vai("POST", rottaOra, v, nil); r.codice != 403 {
			t.Errorf("titolare %s: %d", nome, r.codice)
		}
	}
	fuori := nuovoBrowser(t, h)
	if r := fuori.vai("POST", rottaOra, url.Values{"backup[ora]": {"23:00"}}, nil); r.codice == 302 && r.location == rottaPaginaBackup {
		t.Errorf("senza sessione: %d %q", r.codice, r.location)
	}
	if oraSalvata(t, d) != "" {
		t.Fatalf("ora scritta da chi non poteva: %q", oraSalvata(t, d))
	}
	for _, v := range []string{"", "2:30", "24:00", "12:60", "02:30:00", "ab:cd", "+2:30", "0230"} {
		r := b.vai("POST", rottaOra, url.Values{"_csrf_token": {b.token}, "backup[ora]": {v}}, nil)
		if !redirectA(r, rottaPaginaBackup) {
			t.Fatalf("%q: %d %q", v, r.codice, r.location)
		}
		r = b.vai("GET", rottaPaginaBackup, nil, nil)
		contiene(t, v, r.corpo, html.EscapeString(msgOraNonValida))
	}
	// Senza il campo è come vuoto.
	b.vai("POST", rottaOra, url.Values{"_csrf_token": {b.token}}, nil)
	if r := b.vai("GET", rottaPaginaBackup, nil, nil); !strings.Contains(r.corpo, html.EscapeString(msgOraNonValida)) {
		t.Error("senza il campo: niente errore")
	}
	if oraSalvata(t, d) != "" || contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'backup.schedule_updated'`) != 0 {
		t.Fatalf("un'ora non valida ha scritto: %q", oraSalvata(t, d))
	}
	b.vai("POST", rottaOra, url.Values{"_csrf_token": {b.token}, "backup[ora]": {" 04:15 "}}, nil)
	if oraSalvata(t, d) != "04:15" {
		t.Errorf("con gli spazi: %q", oraSalvata(t, d))
	}
	if o, err := n.Ora(context.Background()); err != nil || backup.TestoOra(o) != "04:15" {
		t.Errorf("ora del backup: %v %v", o, err)
	}
}

// La conferma dice da quando vale l'ora nuova, solo se il lavoro notturno gira.
func TestMsgOraSalvata(t *testing.T) {
	prossimo := time.Date(2026, 11, 25, 22, 0, 0, 0, time.UTC) // mercoledì alle 23:00 di Roma
	for _, c := range []struct {
		es     backup.EsitoOra
		atteso string
	}{
		{backup.EsitoOra{Ora: 23 * time.Hour, Cambiata: true, Prossimo: prossimo, Acceso: true},
			"Ora del backup notturno salvata: 23:00, tutti i giorni. Il primo a quest'ora: mercoledì 25/11/2026 23:00."},
		{backup.EsitoOra{Ora: 23 * time.Hour, Cambiata: true, Subito: true, Acceso: true},
			"Ora del backup notturno salvata: 23:00, tutti i giorni. Aspettando quest'ora l'ultimo backup buono avrebbe più di 25 ore: il prossimo backup parte entro pochi minuti, poi ogni giorno a quest'ora."},
		{backup.EsitoOra{Ora: 23 * time.Hour, Cambiata: true, Prossimo: prossimo}, "Ora del backup notturno salvata: 23:00, tutti i giorni."},
		{backup.EsitoOra{Ora: 23 * time.Hour, Cambiata: true, Subito: true}, "Ora del backup notturno salvata: 23:00, tutti i giorni."},
	} {
		if g := msgOraSalvata(c.es); g != c.atteso {
			t.Errorf("%+v:\n%s\natteso\n%s", c.es, g, c.atteso)
		}
	}
}
