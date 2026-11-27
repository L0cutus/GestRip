package backup

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/jobs"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// La conferma «questa è ora la macchina vera» (slice 131, OPS-19, D-12, D-118). Tutto inventato.

// copiaIsolataProva è un database migrato con email, Telegram e copia remota accesi, tre messaggi in coda (due
// Telegram, un'email) e uno già accettato, poi isolato come fa il ripristino.
func copiaIsolataProva(t *testing.T) *db.DB {
	t.Helper()
	d := nuovaProva(t).s.DB
	ora := "2026-11-20T09:00:00.000000Z"
	for _, q := range []string{
		`INSERT INTO impostazioni VALUES ('` + uscita.ChiaveEmailAcceso + `', '1', '` + ora + `'), ('` + uscita.ChiaveTelegramAcceso +
			`', '1', '` + ora + `'), ('` + ChiaveRemoto + `', '{"host":"sftp.example.invalid"}', '` + ora + `')`,
		`INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
			VALUES (11, 'telegram', 'shop', '-1001', 'avviso al negozio', 'queued', 0, '` + ora + `', '` + ora + `'),
			(12, 'telegram', 'customer', '4242', 'la tua riparazione è pronta', 'queued', 0, '` + ora + `', '` + ora + `'),
			(13, 'email', 'customer', 'cliente@example.invalid', 'testo email', 'queued', 0, '` + ora + `', '` + ora + `'),
			(14, 'telegram', 'customer', '4242', 'già partito', 'accepted', 1, '` + ora + `', '` + ora + `')`,
	} {
		if _, err := d.Scrittura.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.ChiaveServer(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IsolaRipristino(context.Background(), d, clock.NuovoFinto(alle(2026, 11, 27, 8, 0)),
		db.Isolamento{Archivio: "gestrip-backup-20261120T020000Z.tar.age", SHA256: strings.Repeat("ab", 32), CatturatoIl: ora},
		ImpostazioniSpenteDalRipristino, ImpostazioniTolteDalRipristino); err != nil {
		t.Fatal(err)
	}
	return d
}

// righeLavori sono le righe di esecuzioni_lavori (D-119).
func righeLavori(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM esecuzioni_lavori`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func valoreImpostazione(t *testing.T, d *db.DB, k string) (string, bool) {
	t.Helper()
	var v string
	if err := d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, k).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func idInCoda(t *testing.T, d *db.DB) []int64 {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT id FROM messaggi_in_uscita WHERE stato = 'queued' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rr.Close() }()
	var ids []int64
	for rr.Next() {
		var id int64
		if err := rr.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func righeAudit(t *testing.T, d *db.DB, azione string) []map[string]any {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT attore, id_risorsa, dati FROM eventi_audit WHERE azione = ? ORDER BY id`, azione)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rr.Close() }()
	var out []map[string]any
	for rr.Next() {
		var attore, id, dati string
		if err := rr.Scan(&attore, &id, &dati); err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(dati), &m); err != nil {
			t.Fatal(err)
		}
		m["_attore"], m["_id"] = attore, id
		out = append(out, m)
	}
	return out
}

// Voce OPS-19 (D-12, D-118): lo stato della copia dice archivio, cosa si riaccenderà e i messaggi in coda; la
// conferma con la parola giusta e una scelta per ogni messaggio, in una transazione: scarta (via dalla coda, con
// outgoing.queued_discarded) o tiene in coda (manda), rimette email, Telegram e copia remota com'erano nel backup,
// toglie lo stato isolato, segna come già fatti oggi i lavori già dovuti (D-119: venerdì alle 09:00 i ritardi; i
// solleciti alle 10:00 e il briefing spento no) e scrive backup.restore_confirmed con archivio, ora del ripristino e
// il conteggio dei lavori segnati.
func TestConfermaRipristino(t *testing.T) {
	d := copiaIsolataProva(t)
	ctx := context.Background()
	st, err := LeggiStatoRipristino(ctx, d.Lettura)
	if err != nil || !st.Isolata {
		t.Fatalf("stato: %+v %v", st, err)
	}
	if st.Isolamento.Archivio != "gestrip-backup-20261120T020000Z.tar.age" || !slices.Equal(st.DaRiaccendere, []string{"email", "Telegram", "copia remota"}) {
		t.Errorf("stato: %+v", st)
	}
	var ids []int64
	for _, m := range st.Coda {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []int64{11, 12, 13}) || st.Coda[1].Canale != "telegram" || st.Coda[1].Destinatario != "customer" ||
		st.Coda[1].Testo != "la tua riparazione è pronta" {
		t.Errorf("coda: %+v", st.Coda)
	}
	o := clock.NuovoFinto(alle(2026, 11, 27, 9, 0))
	chi := audit.Contesto{Attore: "titolare@example.invalid"}
	es, err := ConfermaRipristino(ctx, d, o, SceltaConferma{Parola: " confermo ", Manda: []int64{11}, Scarta: []int64{12, 13}}, chi)
	if err != nil {
		t.Fatal(err)
	}
	if es.Mandati != 1 || es.Scartati != 2 || !slices.Equal(es.Riaccesi, []string{"email", "Telegram", "copia remota"}) ||
		!slices.Equal(es.LavoriSegnati, []string{jobs.Ritardi}) {
		t.Errorf("esito %+v", es)
	}
	var segnati int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM esecuzioni_lavori WHERE lavoro = ? AND avvio = ? AND esito = ? AND giorno = '2026-11-27'
		AND eseguito_il = '2026-11-27T08:00:00.000000Z' AND dettaglio = ?`, jobs.Ritardi, jobs.AvvioAutomatico, jobs.EsitoOK,
		jobs.DettaglioDopoRipristino).Scan(&segnati); err != nil || segnati != 1 {
		t.Errorf("lavori segnati: %d %v", segnati, err)
	}
	if _, isolata, err := db.LeggiIsolamento(ctx, d.Lettura); isolata || err != nil {
		t.Errorf("ancora isolata: %v %v", isolata, err)
	}
	for k, atteso := range map[string]string{uscita.ChiaveEmailAcceso: "1", uscita.ChiaveTelegramAcceso: "1", ChiaveRemoto: `{"host":"sftp.example.invalid"}`} {
		if v, ok := valoreImpostazione(t, d, k); !ok || v != atteso {
			t.Errorf("%s = %q %v, atteso %q", k, v, ok, atteso)
		}
	}
	if got := idInCoda(t, d); !slices.Equal(got, []int64{11}) {
		t.Errorf("in coda dopo la conferma: %v", got)
	}
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM messaggi_in_uscita WHERE id = 14 AND stato = 'accepted'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("riga già accettata toccata: %d %v", n, err)
	}
	scarti := righeAudit(t, d, "outgoing.queued_discarded")
	if len(scarti) != 2 || scarti[0]["_id"] != "12" || scarti[1]["_id"] != "13" || scarti[0]["channel"] != "telegram" ||
		scarti[1]["party"] != "customer" || scarti[0]["_attore"] != chi.Attore {
		t.Errorf("audit degli scarti: %v", scarti)
	}
	for _, s := range scarti {
		if _, c := s["text"]; c {
			t.Errorf("testo nell'audit: %v", s)
		}
	}
	conf := righeAudit(t, d, "backup.restore_confirmed")
	if len(conf) != 1 || conf[0]["archive"] != "gestrip-backup-20261120T020000Z.tar.age" || conf[0]["restored_at"] != "2026-11-27T07:00:00.000000Z" ||
		conf[0]["queued_kept"] != float64(1) || conf[0]["queued_discarded"] != float64(2) || conf[0]["_attore"] != chi.Attore ||
		conf[0]["jobs_marked_done"] != float64(1) {
		t.Errorf("audit della conferma: %v", conf)
	}
	// Una seconda conferma: niente da confermare.
	if _, err := ConfermaRipristino(ctx, d, o, SceltaConferma{Parola: ParolaDiConferma, Manda: []int64{11}}, chi); !errors.Is(err, ErrNonIsolata) {
		t.Errorf("seconda conferma: %v", err)
	}
	if st, err := LeggiStatoRipristino(ctx, d.Lettura); err != nil || st.Isolata {
		t.Errorf("stato dopo: %+v %v", st, err)
	}
}

// Prima di toccare qualcosa: la parola sbagliata, un messaggio senza scelta (arrivato dopo che la pagina li ha
// mostrati), un id in più, un id in tutte e due le scelte sono rifiuti e la copia resta isolata com'era, coda
// compresa.
func TestConfermaRipristinoRifiuti(t *testing.T) {
	d := copiaIsolataProva(t)
	ctx := context.Background()
	o := clock.NuovoFinto(alle(2026, 11, 27, 9, 0))
	for nome, c := range map[string]struct {
		s   SceltaConferma
		err error
	}{
		"parola sbagliata":   {SceltaConferma{Parola: "SI", Scarta: []int64{11, 12, 13}}, ErrParolaSbagliata},
		"parola vuota":       {SceltaConferma{Scarta: []int64{11, 12, 13}}, ErrParolaSbagliata},
		"uno senza scelta":   {SceltaConferma{Parola: ParolaDiConferma, Scarta: []int64{11, 12}}, ErrCodaCambiata},
		"uno in più":         {SceltaConferma{Parola: ParolaDiConferma, Scarta: []int64{11, 12, 13, 99}}, ErrCodaCambiata},
		"già accettato":      {SceltaConferma{Parola: ParolaDiConferma, Scarta: []int64{11, 12, 13}, Manda: []int64{14}}, ErrCodaCambiata},
		"in tutte e due":     {SceltaConferma{Parola: ParolaDiConferma, Scarta: []int64{11, 12, 13}, Manda: []int64{13}}, ErrCodaCambiata},
		"uno ripetuto":       {SceltaConferma{Parola: ParolaDiConferma, Scarta: []int64{11, 12, 13, 13}}, ErrCodaCambiata},
		"nessuna scelta mai": {SceltaConferma{Parola: ParolaDiConferma}, ErrCodaCambiata},
	} {
		if _, err := ConfermaRipristino(ctx, d, o, c.s, audit.Contesto{Attore: "console"}); !errors.Is(err, c.err) {
			t.Errorf("%s: %v, atteso %v", nome, err, c.err)
		}
	}
	if _, isolata, err := db.LeggiIsolamento(ctx, d.Lettura); !isolata || err != nil {
		t.Errorf("non più isolata: %v %v", isolata, err)
	}
	if got := idInCoda(t, d); !slices.Equal(got, []int64{11, 12, 13}) {
		t.Errorf("coda toccata: %v", got)
	}
	for _, k := range ImpostazioniSpenteDalRipristino {
		if _, ok := valoreImpostazione(t, d, k); ok {
			t.Errorf("%s riacceso da un rifiuto", k)
		}
	}
	if len(righeAudit(t, d, "backup.restore_confirmed"))+len(righeAudit(t, d, "outgoing.queued_discarded")) != 0 {
		t.Error("audit scritto da un rifiuto")
	}
	if n := righeLavori(t, d); n != 0 {
		t.Errorf("%d lavori segnati da un rifiuto", n)
	}
}

// Una conferma a metà non lascia niente a metà: con la scrittura dell'audit che fallisce (tabella degli eventi
// tolta) la transazione si annulla, e scarti, interruttori e stato isolato restano com'erano.
func TestConfermaRipristinoAMeta(t *testing.T) {
	d := copiaIsolataProva(t)
	ctx := context.Background()
	if _, err := d.Scrittura.Exec(`CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit
		WHEN NEW.azione = 'backup.restore_confirmed' BEGIN SELECT RAISE(ABORT, 'audit finto rotto'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := ConfermaRipristino(ctx, d, clock.NuovoFinto(alle(2026, 11, 27, 9, 0)),
		SceltaConferma{Parola: ParolaDiConferma, Scarta: []int64{11, 12, 13}}, audit.Contesto{Attore: "console"})
	if err == nil {
		t.Fatal("conferma riuscita con l'audit rotto")
	}
	if _, isolata, err := db.LeggiIsolamento(ctx, d.Lettura); !isolata || err != nil {
		t.Errorf("non più isolata: %v %v", isolata, err)
	}
	if got := idInCoda(t, d); !slices.Equal(got, []int64{11, 12, 13}) {
		t.Errorf("coda toccata: %v", got)
	}
	for _, k := range ImpostazioniSpenteDalRipristino {
		if _, ok := valoreImpostazione(t, d, k); ok {
			t.Errorf("%s riacceso da una conferma a metà", k)
		}
	}
	if len(righeAudit(t, d, "outgoing.queued_discarded")) != 0 {
		t.Error("scarti scritti da una conferma a metà")
	}
	if n := righeLavori(t, d); n != 0 {
		t.Errorf("%d lavori segnati da una conferma a metà", n)
	}
}

// D-119: un errore nel segnare i lavori (scrittura di esecuzioni_lavori rifiutata da un trigger) fa fallire tutta la
// conferma: scarti, interruttori, stato isolato e audit restano com'erano, e nessuna riga dei lavori.
func TestConfermaRipristinoLavoriNonSegnati(t *testing.T) {
	d := copiaIsolataProva(t)
	ctx := context.Background()
	if _, err := d.Scrittura.Exec(`CREATE TRIGGER niente_lavori BEFORE INSERT ON esecuzioni_lavori
		BEGIN SELECT RAISE(ABORT, 'lavori finti rotti'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := ConfermaRipristino(ctx, d, clock.NuovoFinto(alle(2026, 11, 27, 9, 0)),
		SceltaConferma{Parola: ParolaDiConferma, Scarta: []int64{11, 12, 13}}, audit.Contesto{Attore: "console"})
	if err == nil || !strings.Contains(err.Error(), "lavoro overdue segnato come fatto") {
		t.Fatalf("conferma con i lavori rotti: %v", err)
	}
	if _, isolata, err := db.LeggiIsolamento(ctx, d.Lettura); !isolata || err != nil {
		t.Errorf("non più isolata: %v %v", isolata, err)
	}
	if got := idInCoda(t, d); !slices.Equal(got, []int64{11, 12, 13}) {
		t.Errorf("coda toccata: %v", got)
	}
	for _, k := range ImpostazioniSpenteDalRipristino {
		if _, ok := valoreImpostazione(t, d, k); ok {
			t.Errorf("%s riacceso da una conferma fallita", k)
		}
	}
	if len(righeAudit(t, d, "outgoing.queued_discarded"))+len(righeAudit(t, d, "backup.restore_confirmed")) != 0 || righeLavori(t, d) != 0 {
		t.Error("scritto qualcosa da una conferma fallita")
	}
}

// Un interruttore che il titolare ha spento a mano durante l'isolamento resta spento (scelta prudente): la conferma
// rimette solo quello che manca, e l'esito dice solo quello. Senza messaggi in coda basta la parola.
func TestConfermaRipristinoNonSovrascrive(t *testing.T) {
	d := copiaIsolataProva(t)
	ctx := context.Background()
	if _, err := d.Scrittura.Exec(`DELETE FROM messaggi_in_uscita WHERE stato = 'queued'`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni VALUES (?, '0', '2026-11-27T08:30:00.000000Z')`, uscita.ChiaveTelegramAcceso); err != nil {
		t.Fatal(err)
	}
	es, err := ConfermaRipristino(ctx, d, clock.NuovoFinto(alle(2026, 11, 27, 9, 0)), SceltaConferma{Parola: ParolaDiConferma}, audit.Contesto{Attore: "console"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(es.Riaccesi, []string{"email", "copia remota"}) {
		t.Errorf("riaccesi %v", es.Riaccesi)
	}
	if v, _ := valoreImpostazione(t, d, uscita.ChiaveTelegramAcceso); v != "0" {
		t.Errorf("Telegram %q", v)
	}
}

// Su un database mai ripristinato: niente da confermare, niente cambia.
func TestConfermaSenzaRipristino(t *testing.T) {
	d := nuovaProva(t).s.DB
	ctx := context.Background()
	if st, err := LeggiStatoRipristino(ctx, d.Lettura); err != nil || st.Isolata {
		t.Errorf("stato %+v %v", st, err)
	}
	if _, err := ConfermaRipristino(ctx, d, clock.Sistema{}, SceltaConferma{Parola: ParolaDiConferma}, audit.Contesto{Attore: "console"}); !errors.Is(err, ErrNonIsolata) {
		t.Errorf("conferma: %v", err)
	}
}
