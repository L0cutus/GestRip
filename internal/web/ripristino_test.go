package web

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// La conferma del ripristino (ripristino.go, slice 131, OPS-19, D-12, D-118): banner per tutto il personale nella
// copia isolata, pagina del titolare con l'avviso di spegnere la macchina vecchia, i messaggi in coda con «manda» o
// «scarta» (scarta già scelto) e la parola. La conferma vera (backup.Conferma) su un database isolato inventato.

const rottaRipristinoProva = "/settings/backup/ripristino"

// facciaIsolata è la faccia interna di un serve partito su una copia isolata (copiaIsolata) con il database dato e
// le buste vere; dà anche una busta pronta (per la pagina della firma).
func facciaIsolata(t *testing.T, copiaIsolata bool) (http.Handler, *db.DB, int64) {
	t.Helper()
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(martediPromemoria)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	ora := "2026-11-10T09:00:00.000000Z"
	eseguiSQL(t, d, `INSERT INTO impostazioni VALUES (?, '1', ?), (?, '1', ?)`, uscita.ChiaveEmailAcceso, ora, uscita.ChiaveTelegramAcceso, ora)
	eseguiSQL(t, d, `INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, oggetto, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
		VALUES (21, 'telegram', 'customer', '4242', NULL, 'La riparazione INVENTATA è pronta', 'queued', 0, ?1, ?1),
		(22, 'email', 'customer', 'cliente@example.invalid', 'Avviso inventato', 'Testo email INVENTATO', 'queued', 0, ?1, ?1)`, ora)
	if copiaIsolata {
		if _, err := db.IsolaRipristino(context.Background(), d, o, db.Isolamento{Archivio: "gestrip-backup-20261110T020000Z.tar.age",
			SHA256: strings.Repeat("cd", 32), CatturatoIl: ora}, backup.ImpostazioniSpenteDalRipristino, backup.ImpostazioniTolteDalRipristino); err != nil {
			t.Fatal(err)
		}
	}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.CopiaIsolata = copiaIsolata
	dd.Ripristino = backup.NuovaConferma(d, o)
	b := buste.Nuove(d, o).ConFoto(media.Nuovo(media.CartellaDi(percorso)), normalizzaQui{})
	dd.Buste = b
	id := bustaScheda(t, d, o, nil)
	if _, err := b.CambiaStato(context.Background(), id, "pronta", audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	return mustInterna(t, dd), d, id
}

func isolataAncora(t *testing.T, d *db.DB) bool {
	t.Helper()
	_, ok, err := db.LeggiIsolamento(context.Background(), d.Lettura)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func inCodaAncora(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM messaggi_in_uscita WHERE stato = 'queued'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Voce OPS-19 (D-118 (b), (d)): il banner c'è su ogni pagina interna, per il titolare con il link alla conferma, per
// il dipendente con «chiama il titolare»; la pagina dice di spegnere la macchina vecchia, l'archivio, cosa si
// riaccende e i messaggi in coda con «scarta» già scelto. La parola sbagliata e un messaggio arrivato dopo la pagina
// sono rifiuti che non cambiano niente; la conferma giusta scarta e tiene come scelto, toglie l'isolamento e il
// banner dice di riavviare.
func TestConfermaRipristinoDallaPagina(t *testing.T) {
	h, d, firma := facciaIsolata(t, true)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/", nil, nil)
	contiene(t, "banner del titolare", r.corpo, `id="isolated-copy"`, "Questa è una copia isolata", `href="`+rottaRipristinoProva+`"`)
	dip := dipendenteDentro(t, h)
	r = dip.vai("GET", "/", nil, nil)
	contiene(t, "banner del dipendente", r.corpo, `id="isolated-copy"`, "Questa è una copia isolata", "chiama il titolare")
	nonContiene(t, "banner del dipendente", r.corpo, `href="`+rottaRipristinoProva+`"`)
	// Il dipendente non apre la pagina e non conferma.
	if r := dip.vai("GET", rottaRipristinoProva, nil, nil); !redirectA(r, "/") {
		t.Errorf("dipendente sulla pagina: %d %q", r.codice, r.location)
	}
	dip.vai("GET", "/", nil, nil)
	dip.vai("POST", rottaRipristinoProva, url.Values{"_csrf_token": {dip.token}, "parola": {"CONFERMO"}, "riga": {"21", "22"}}, nil)
	if !isolataAncora(t, d) {
		t.Fatal("confermata dal dipendente")
	}

	r = b.vai("GET", rottaRipristinoProva, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina: %d", r.codice)
	}
	contiene(t, "pagina", r.corpo, "Spegni la macchina vecchia", "gestrip-backup-20261110T020000Z.tar.age", "email, Telegram",
		html.EscapeString("La riparazione INVENTATA è pronta"), "Avviso inventato", `name="riga" value="21"`, `name="riga" value="22"`,
		`name="scelta-21" value="scarta" checked`, `name="scelta-21" value="manda"`, `name="parola"`, `action="`+rottaRipristinoProva+`"`,
		`name="_csrf_token" type="hidden" value="`+b.token+`"`)
	nonContiene(t, "pagina", r.corpo, `value="manda" checked`, `id="isolated-copy"`)
	contiene(t, "pagina", r.corpo, "resta in coda finché non lo riaccendi")
	// Né la firma (il tablet passa al cliente) né la pagina della conferma hanno il banner; la scheda sì.
	if rf := b.vai("GET", "/repairs/"+strconv.FormatInt(firma, 10)+"/firma", nil, nil); rf.codice != 200 {
		t.Fatalf("firma: %d", rf.codice)
	} else {
		contiene(t, "firma", rf.corpo, `id="signature-form"`)
		nonContiene(t, "firma", rf.corpo, `id="isolated-copy"`)
	}
	contiene(t, "scheda", b.vai("GET", "/repairs/"+strconv.FormatInt(firma, 10), nil, nil).corpo, `id="isolated-copy"`)
	b.vai("GET", rottaRipristinoProva, nil, nil) // il token della pagina, di nuovo
	// Senza CSRF: 403 e niente.
	if r := b.vai("POST", rottaRipristinoProva, url.Values{"parola": {"CONFERMO"}, "riga": {"21", "22"}}, nil); r.codice != http.StatusForbidden {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	// La parola sbagliata.
	r = b.vai("POST", rottaRipristinoProva, url.Values{"_csrf_token": {b.token}, "parola": {"sì"}, "riga": {"21", "22"}}, nil)
	contiene(t, "parola sbagliata", r.corpo, html.EscapeString(msgParolaSbagliata))
	// Un id che non si legge o che non è in coda: rifiuto, niente cambia.
	for _, righe := range [][]string{{"21", "22", "abc"}, {"21", "22", "999"}, {"21", "22", "-1"}} {
		r = b.vai("POST", rottaRipristinoProva, url.Values{"_csrf_token": {b.token}, "parola": {"CONFERMO"}, "riga": righe}, nil)
		contiene(t, "riga "+righe[2], r.corpo, html.EscapeString(msgCodaCambiata))
	}
	if !isolataAncora(t, d) || inCodaAncora(t, d) != 2 {
		t.Fatal("una riga non valida ha cambiato qualcosa")
	}
	// Un messaggio accodato dopo che la pagina è stata disegnata.
	eseguiSQL(t, d, `INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
		VALUES (23, 'telegram', 'shop', '-1001', 'arrivato dopo', 'queued', 0, '2026-11-17T09:00:00.000000Z', '2026-11-17T09:00:00.000000Z')`)
	r = b.vai("POST", rottaRipristinoProva, url.Values{"_csrf_token": {b.token}, "parola": {"CONFERMO"}, "riga": {"21", "22"}}, nil)
	contiene(t, "coda cambiata", r.corpo, html.EscapeString(msgCodaCambiata), "arrivato dopo")
	if !isolataAncora(t, d) || inCodaAncora(t, d) != 3 {
		t.Fatal("un rifiuto ha cambiato qualcosa")
	}
	// La conferma: 21 «manda», 22 e 23 «scarta» (23 senza scelta: la proposta).
	r = b.vai("POST", rottaRipristinoProva, url.Values{"_csrf_token": {b.token}, "parola": {"confermo"}, "riga": {"21", "22", "23"},
		"scelta-21": {"manda"}, "scelta-22": {"scarta"}}, nil)
	if !redirectA(r, rottaRipristinoProva) {
		t.Fatalf("conferma: %d %q", r.codice, r.corpo)
	}
	if isolataAncora(t, d) {
		t.Fatal("ancora isolata dopo la conferma")
	}
	var id int64
	if err := d.Lettura.QueryRow(`SELECT id FROM messaggi_in_uscita WHERE stato = 'queued'`).Scan(&id); err != nil || id != 21 || inCodaAncora(t, d) != 1 {
		t.Errorf("in coda dopo la conferma: %d %v", id, err)
	}
	var attore string
	if err := d.Lettura.QueryRow(`SELECT attore FROM eventi_audit WHERE azione = 'backup.restore_confirmed'`).Scan(&attore); err != nil || attore != "titolare@esempio.it" {
		t.Errorf("audit: %q %v", attore, err)
	}
	r = b.vai("GET", rottaRipristinoProva, nil, nil)
	// D-119: martedì alle 10:00 in negozio ritardi e solleciti erano già dovuti; il briefing è spento.
	contiene(t, "dopo la conferma", r.corpo, "Riavvia GestRip", html.EscapeString(msgRipristinoConfermato+
		" Lavori di oggi segnati come già fatti: Buste in ritardo, Solleciti artigiano. Se la macchina vecchia non li aveva fatti, usa «Esegui adesso» in Promemoria e job dopo il riavvio."))
	nonContiene(t, "dopo la conferma", r.corpo, `name="parola"`)
	// Il serve che gira resta isolato fino al riavvio: il banner lo dice a tutti.
	contiene(t, "banner dopo la conferma", dip.vai("GET", "/", nil, nil).corpo, `id="isolated-copy"`, "riavviare GestRip")
}

// Una conferma già fatta (qui dalla console, cioè direttamente sul database) mentre la pagina era aperta: il POST
// dice «già confermato: riavvia», non «niente da confermare».
func TestConfermaRipristinoGiaFatta(t *testing.T) {
	h, d, _ := facciaIsolata(t, true)
	b := titolareDentro(t, h)
	b.vai("GET", rottaRipristinoProva, nil, nil)
	if _, err := backup.ConfermaRipristino(context.Background(), d, clock.Sistema{}, backup.SceltaConferma{Parola: backup.ParolaDiConferma,
		Scarta: []int64{21, 22}}, audit.Contesto{Attore: "console"}); err != nil {
		t.Fatal(err)
	}
	r := b.vai("POST", rottaRipristinoProva, url.Values{"_csrf_token": {b.token}, "parola": {"CONFERMO"}, "riga": {"21", "22"}}, nil)
	contiene(t, "già confermato", r.corpo, html.EscapeString(msgRipristinoGiaConfermato))
	nonContiene(t, "già confermato", r.corpo, html.EscapeString(msgRipristinoNienteDaConfermare))
}

// Su una macchina normale niente banner e la pagina dice che non c'è niente da confermare; il POST non fa niente.
func TestConfermaRipristinoMacchinaNormale(t *testing.T) {
	h, d, _ := facciaIsolata(t, false)
	b := titolareDentro(t, h)
	nonContiene(t, "home", b.vai("GET", "/", nil, nil).corpo, `id="isolated-copy"`)
	r := b.vai("GET", rottaRipristinoProva, nil, nil)
	contiene(t, "pagina", r.corpo, `id="restore-nothing"`)
	nonContiene(t, "pagina", r.corpo, `name="parola"`)
	b.vai("POST", rottaRipristinoProva, url.Values{"_csrf_token": {b.token}, "parola": {"CONFERMO"}, "riga": {"21", "22"}}, nil)
	if inCodaAncora(t, d) != 2 {
		t.Error("coda toccata su una macchina normale")
	}
}

// Voce OPS-19 (D-118 (b)): nel serve della copia isolata «Esegui adesso» dei promemoria rifiuta con una frase
// semplice e non accoda niente.
func TestPromemoriaNellaCopiaIsolata(t *testing.T) {
	pp, b := facciaConPromemoria(t)
	pp.accendiTelegram(t)
	pp.bustaInRitardo(t, "7/26", 10)
	pp.p.ConCopiaIsolata()
	r := b.vai("POST", rottaPromemoria+"/esegui", modPromemoria(b, "job", "overdue"), nil)
	if !redirectA(r, rottaPromemoria) {
		t.Fatalf("esegui: %d", r.codice)
	}
	contiene(t, "copia isolata", b.vai("GET", rottaPromemoria, nil, nil).corpo, html.EscapeString(msgEseguiCopiaIsolata))
	var n int
	if err := pp.d.Lettura.QueryRow(`SELECT count(*) FROM messaggi_in_uscita`).Scan(&n); err != nil || n != 0 {
		t.Errorf("righe in coda: %d %v", n, err)
	}
}
