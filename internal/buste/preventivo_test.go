package buste

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La risposta al preventivo (CLI-05, CLI-06, CLI-07, CLI-09, RIP-S-22), come quote_decision_test.exs,
// quote_decision_event_test.exs e public_repair_controller_test.exs dello standalone (go-baseline).

var pubblicoProva = audit.Contesto{Attore: "public", IP: "192.0.2.10"}

// inPreventivo crea una busta in «preventivo» con il preventivo dato ("" senza importo).
func inPreventivo(t *testing.T, b *Buste, preventivo string, f func(*Accettazione)) Creata {
	t.Helper()
	c := crea(t, b, con(modulo(), func(a *Accettazione) {
		a.Preventivo = preventivo
		if f != nil {
			f(a)
		}
	}))
	if _, err := b.CambiaStato(ctx, c.ID, "preventivo", audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	return c
}

func statoBusta(t *testing.T, b *Buste, id int64) string {
	t.Helper()
	return valore(t, b.d, `SELECT stato FROM buste WHERE id = ?`, id).(string)
}

// inventario: CLI-05
func TestPreventivoStatoPubblico(t *testing.T) {
	b, _, o := prova(t)
	senza := inPreventivo(t, b, "", nil)
	if r := pagina(t, b, senza.Token).Risposta; r.Fase != RispostaSenzaImporto {
		t.Errorf("senza importo: %+v", r)
	}
	con := inPreventivo(t, b, "40", nil)
	if r := pagina(t, b, con.Token).Risposta; r.Fase != RispostaInAttesa {
		t.Errorf("con importo: %+v", r)
	}
	// In centesimi soltanto (busta importata) conta come importo.
	esegui(t, b.d, `UPDATE buste SET preventivo_testo = NULL, preventivo_centesimi = 1250 WHERE id = ?`, senza.ID)
	if p := pagina(t, b, senza.Token); p.Risposta.Fase != RispostaInAttesa || p.Preventivo != "€ 12,50" {
		t.Errorf("centesimi: %+v %q", p.Risposta, p.Preventivo)
	}
	// Fuori da «preventivo» la pagina non dice niente, nemmeno di una risposta data.
	aperta := crea(t, b, con2("40"))
	if r := pagina(t, b, aperta.Token).Risposta; r.Fase != RispostaNonApplicabile {
		t.Errorf("in lavorazione: %+v", r)
	}
	o.Sposta(time.Minute)
	if err := b.ApprovaPreventivo(ctx, con.Token, pubblicoProva); err != nil {
		t.Fatal(err)
	}
	if r := pagina(t, b, con.Token).Risposta; r.Fase != RispostaNonApplicabile {
		t.Errorf("approvata e passata in lavorazione: %+v", r)
	}
	// Riportata a «preventivo» senza revoca, la risposta si vede (la risposta vince sullo stato).
	esegui(t, b.d, `UPDATE buste SET stato = 'preventivo' WHERE id = ?`, con.ID)
	if r := pagina(t, b, con.Token).Risposta; r.Fase != RispostaApprovata || !r.Il.Equal(o.Adesso()) {
		t.Errorf("approvata in preventivo: %+v", r)
	}
}

func con2(preventivo string) Accettazione {
	return con(modulo(), func(a *Accettazione) { a.Preventivo = preventivo })
}

// inventario: CLI-06
func TestPreventivoApprova(t *testing.T) {
	b, d, o := prova(t)
	c := inPreventivo(t, b, "40", nil)
	esegui(t, d, `UPDATE buste SET preventivo_accettato_letto_il = '2026-10-01T00:00:00.000000Z' WHERE id = ?`, c.ID)
	versione0, _ := b.Versione(ctx, c.ID)
	cambi0 := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`)
	o.Sposta(time.Hour)
	if err := b.ApprovaPreventivo(ctx, c.Token, pubblicoProva); err != nil {
		t.Fatal(err)
	}
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if s := statoBusta(t, b, c.ID); s != "in_lavorazione" {
		t.Errorf("stato %q", s)
	}
	if v := valore(t, d, `SELECT preventivo_accettato_il FROM buste WHERE id = ?`, c.ID); v != ora {
		t.Errorf("accettato il %v", v)
	}
	if v := valore(t, d, `SELECT preventivo_accettato_letto_il FROM buste WHERE id = ?`, c.ID); v != nil {
		t.Errorf("segno letto non azzerato: %v", v)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_preventivo WHERE busta_id = ? AND evento = 'approved' AND parte = 'customer'
		AND motivo IS NULL AND attore = 'public' AND avvenuto_il = ?`, c.ID, ora); n != 1 {
		t.Errorf("storia: %d righe", n)
	}
	if a := ultimoAudit(t, d, "quote.approved"); a != `quote.approved|repair|`+itoa(c.ID)+`|ok|{"party":"customer","reason_given":false,"status":"in_lavorazione"}` {
		t.Errorf("audit %q", a)
	}
	// Nessuna riga repair.status_changed (come lo standalone), ma la versione dello stato conta.
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`); n != cambi0 {
		t.Errorf("status_changed: %d, prima %d", n, cambi0)
	}
	if v, _ := b.Versione(ctx, c.ID); v != versione0+1 {
		t.Errorf("versione %d, prima %d", v, versione0)
	}
}

// inventario: CLI-06, CLI-07
// La risposta è una: la seconda, uguale o opposta, non scrive niente e dice perché; una busta
// fuori da «preventivo» mai risposta è «non in preventivo».
func TestPreventivoDoppiaRisposta(t *testing.T) {
	b, d, _ := prova(t)
	a := inPreventivo(t, b, "40", nil)
	if err := b.ApprovaPreventivo(ctx, a.Token, pubblicoProva); err != nil {
		t.Fatal(err)
	}
	r := inPreventivo(t, b, "40", nil)
	if err := b.RifiutaPreventivo(ctx, r.Token, "", pubblicoProva); err != nil {
		t.Fatal(err)
	}
	aperta := crea(t, b, con2("40"))
	prima := scatta(t, d)
	for _, c := range []struct {
		nome   string
		err    error
		atteso error
	}{
		{"approva due volte", b.ApprovaPreventivo(ctx, a.Token, pubblicoProva), ErrGiaApprovato},
		{"rifiuta dopo approvato", b.RifiutaPreventivo(ctx, a.Token, "no", pubblicoProva), ErrGiaApprovato},
		{"rifiuta due volte", b.RifiutaPreventivo(ctx, r.Token, "no", pubblicoProva), ErrGiaRifiutato},
		{"approva dopo rifiutato", b.ApprovaPreventivo(ctx, r.Token, pubblicoProva), ErrGiaRifiutato},
		{"non in preventivo", b.ApprovaPreventivo(ctx, aperta.Token, pubblicoProva), ErrNonInPreventivo},
		{"rifiuta non in preventivo", b.RifiutaPreventivo(ctx, aperta.Token, "", pubblicoProva), ErrNonInPreventivo},
	} {
		if !errors.Is(c.err, c.atteso) {
			t.Errorf("%s: %v, atteso %v", c.nome, c.err, c.atteso)
		}
	}
	uguali(t, "dopo le risposte rifiutate", prima, scatta(t, d))
	// Anche con la busta approvata riportata a «preventivo» senza revoca: la risposta vince.
	esegui(t, d, `UPDATE buste SET stato = 'preventivo' WHERE id = ?`, a.ID)
	if err := b.ApprovaPreventivo(ctx, a.Token, pubblicoProva); !errors.Is(err, ErrGiaApprovato) {
		t.Errorf("approvata in preventivo: %v", err)
	}
	// Una risposta già data vince anche sullo stato: rifiutata e poi annullata dal negozio.
	esegui(t, d, `UPDATE buste SET stato = 'annullata' WHERE id = ?`, r.ID)
	if err := b.ApprovaPreventivo(ctx, r.Token, pubblicoProva); !errors.Is(err, ErrGiaRifiutato) {
		t.Errorf("rifiutata e annullata: %v", err)
	}
}

// inventario: CLI-06
// Due telefoni con lo stesso link, insieme: una risposta e un rifiuto, mai due risposte.
func TestPreventivoRisposteInsieme(t *testing.T) {
	b, d, _ := prova(t)
	c := inPreventivo(t, b, "40", nil)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				errs[i] = b.ApprovaPreventivo(ctx, c.Token, pubblicoProva)
			} else {
				errs[i] = b.RifiutaPreventivo(ctx, c.Token, "", pubblicoProva)
			}
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrGiaApprovato), errors.Is(err, ErrGiaRifiutato):
		default:
			t.Errorf("errore %v", err)
		}
	}
	if ok != 1 || conta(t, d, `SELECT count(*) FROM eventi_preventivo WHERE busta_id = ?`, c.ID) != 1 ||
		conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('quote.approved', 'quote.rejected')`) != 1 {
		t.Errorf("riuscite %d", ok)
	}
}

// inventario: CLI-07
func TestPreventivoRifiuta(t *testing.T) {
	b, d, _ := prova(t)
	lungo := strings.Repeat("é", 299) + "👍🏽" + "oltre"
	for _, c := range []struct{ nome, motivo, salvato string }{
		{"vuoto", "", ""}, {"spazi", " \t\n ", ""}, {"con spazi ai bordi", "  troppo caro \n", "troppo caro"},
		{"lungo", lungo, strings.Repeat("é", 299) + "👍🏽"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			busta := inPreventivo(t, b, "40", func(a *Accettazione) { a.OreficeriaID = "2" })
			esegui(t, d, `UPDATE buste SET preventivo_rifiutato_letto_il = '2026-10-01T00:00:00.000000Z' WHERE id = ?`, busta.ID)
			tok := busta.Token
			if err := b.RifiutaPreventivo(ctx, tok, c.motivo, pubblicoProva); err != nil {
				t.Fatal(err)
			}
			if s := statoBusta(t, b, busta.ID); s != "preventivo" {
				t.Errorf("stato %q: il rifiuto non cambia lo stato", s)
			}
			if st, err := b.StoriaPreventivo(ctx, busta.ID); err != nil || len(st) != 1 || st[0].NomeParte() != "oreficeria" ||
				st[0].Etichetta() != "Preventivo rifiutato" {
				t.Errorf("storia %+v %v", st, err)
			}
			var atteso any
			if c.salvato != "" {
				atteso = c.salvato
			}
			if v := valore(t, d, `SELECT preventivo_rifiuto_motivo FROM buste WHERE id = ?`, busta.ID); v != atteso {
				t.Errorf("motivo %q", v)
			}
			if v := valore(t, d, `SELECT motivo FROM eventi_preventivo WHERE busta_id = ? AND evento = 'rejected' AND parte = 'oreficeria'`, busta.ID); v != atteso {
				t.Errorf("motivo nella storia %q", v)
			}
			if v := valore(t, d, `SELECT preventivo_rifiutato_letto_il FROM buste WHERE id = ?`, busta.ID); v != nil {
				t.Errorf("segno letto non azzerato: %v", v)
			}
			// L'audit dice se c'è un motivo, mai il testo.
			dato := `"reason_given":` + map[bool]string{true: "true", false: "false"}[c.salvato != ""]
			if a := ultimoAudit(t, d, "quote.rejected"); !strings.Contains(a, `"party":"oreficeria"`) || !strings.Contains(a, dato) ||
				!strings.Contains(a, `"status":"preventivo"`) || (c.salvato != "" && strings.Contains(a, c.salvato)) {
				t.Errorf("audit %q", a)
			}
			p := pagina(t, b, tok)
			if p.Risposta.Fase != RispostaRifiutata || (c.salvato != "" && *p.Risposta.Motivo != c.salvato) || (c.salvato == "" && p.Risposta.Motivo != nil) {
				t.Errorf("pagina %+v", p.Risposta)
			}
		})
	}
}

// inventario: CLI-02, CLI-06
// Un token che non apre niente non risponde: inesistente, revocato, scaduto, cestino.
func TestPreventivoTokenNonValido(t *testing.T) {
	b, d, o := prova(t)
	rev := inPreventivo(t, b, "40", nil)
	if _, err := b.RevocaLink(ctx, rev.ID, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	cest := inPreventivo(t, b, "40", nil)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, "2026-10-31T09:00:00.000000Z", cest.ID)
	scad := inPreventivo(t, b, "40", nil)
	esegui(t, d, `UPDATE buste SET token_pubblico_scade_il = ? WHERE id = ?`, o.Adesso().Format("2006-01-02T15:04:05.000000Z"), scad.ID)
	prima := scatta(t, d)
	for nome, tok := range map[string]string{"inesistente": "NON-ESISTE-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "vuoto": "",
		"revocato": rev.Token, "cestino": cest.Token, "scaduto": scad.Token} {
		if err := b.ApprovaPreventivo(ctx, tok, pubblicoProva); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("approva %s: %v", nome, err)
		}
		if err := b.RifiutaPreventivo(ctx, tok, "x", pubblicoProva); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("rifiuta %s: %v", nome, err)
		}
	}
	uguali(t, "token non validi", prima, scatta(t, d))
}

// inventario: CLI-09, RIP-S-22
func TestPreventivoRevocaApprovazione(t *testing.T) {
	b, d, o := prova(t)
	c := inPreventivo(t, b, "40", nil)
	if err := b.ApprovaPreventivo(ctx, c.Token, pubblicoProva); err != nil {
		t.Fatal(err)
	}
	risposta := valore(t, d, `SELECT preventivo_accettato_il FROM buste WHERE id = ?`, c.ID)
	o.Sposta(time.Hour)
	shop := audit.Contesto{Attore: "dipendente@esempio.it"}
	// La pagina mostrava un'approvazione: revocare un rifiuto non c'è.
	if err := b.RevocaRisposta(ctx, c.ID, "rejected", shop); !errors.Is(err, ErrNonRifiutato) {
		t.Errorf("revoca del rifiuto: %v", err)
	}
	if err := b.RevocaRisposta(ctx, c.ID, "approved", shop); err != nil {
		t.Fatal(err)
	}
	// Lo stato resta: in lavorazione, e il cliente non rivede i pulsanti.
	if s := statoBusta(t, b, c.ID); s != "in_lavorazione" {
		t.Errorf("stato %q", s)
	}
	if v := valore(t, d, `SELECT preventivo_accettato_il FROM buste WHERE id = ?`, c.ID); v != nil {
		t.Errorf("segno %v", v)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_preventivo WHERE busta_id = ?`, c.ID); n != 2 {
		t.Errorf("storia: %d righe (la revoca aggiunge, non cancella)", n)
	}
	if a := ultimoAudit(t, d, "quote.approval_revoked"); a != `quote.approval_revoked|repair|`+itoa(c.ID)+`|ok|{"answered_at":"`+risposta.(string)+`","status":"in_lavorazione"}` {
		t.Errorf("audit %q", a)
	}
	s, err := b.leggiScheda(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.StoriaPreventivo) != 2 || s.StoriaPreventivo[0].Evento != "approved" || s.StoriaPreventivo[1].Evento != "approval_revoked" ||
		s.StoriaPreventivo[1].Parte != "shop" || s.StoriaPreventivo[1].Etichetta() != "Approvazione revocata" || s.StoriaPreventivo[1].NomeParte() != "negozio" {
		t.Errorf("storia %+v", s.StoriaPreventivo)
	}
	// Doppia revoca: una riga sola.
	prima := scatta(t, d)
	if err := b.RevocaRisposta(ctx, c.ID, "approved", shop); !errors.Is(err, ErrNonApprovato) {
		t.Errorf("seconda revoca: %v", err)
	}
	uguali(t, "seconda revoca", prima, scatta(t, d))
	// Una risposta su un preventivo revocato: la busta non è più in preventivo.
	if err := b.ApprovaPreventivo(ctx, c.Token, pubblicoProva); !errors.Is(err, ErrNonInPreventivo) {
		t.Errorf("risposta dopo la revoca: %v", err)
	}
	// Rimessa in preventivo dal negozio, il cliente risponde di nuovo.
	if _, err := b.CambiaStato(ctx, c.ID, "preventivo", shop); err != nil {
		t.Fatal(err)
	}
	if err := b.RifiutaPreventivo(ctx, c.Token, "", pubblicoProva); err != nil {
		t.Errorf("risposta dopo revoca e preventivo: %v", err)
	}
	for _, x := range []string{"", "APPROVED", "approvato"} {
		if err := b.RevocaRisposta(ctx, c.ID, x, shop); !errors.Is(err, ErrRispostaNonValid) {
			t.Errorf("risposta %q: %v", x, err)
		}
	}
	if err := b.RevocaRisposta(ctx, 999, "approved", shop); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta che non c'è: %v", err)
	}
}

// inventario: CLI-09, RIP-S-22
// La revoca del rifiuto riporta a «preventivo» solo una busta annullata (e la conservazione
// riparte); altrimenti lo stato resta.
func TestPreventivoRevocaRifiuto(t *testing.T) {
	b, d, _ := prova(t)
	shop := audit.Contesto{Attore: "titolare@esempio.it"}
	annullata := inPreventivo(t, b, "40", nil)
	resta := inPreventivo(t, b, "40", nil)
	for _, c := range []Creata{annullata, resta} {
		if err := b.RifiutaPreventivo(ctx, c.Token, "caro", pubblicoProva); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.CambiaStato(ctx, annullata.ID, "annullata", shop); err != nil {
		t.Fatal(err)
	}
	if valore(t, d, `SELECT conservazione_iniziata_il FROM buste WHERE id = ?`, annullata.ID) == nil {
		t.Fatal("conservazione non avviata")
	}
	if err := b.RevocaRisposta(ctx, annullata.ID, "approved", shop); !errors.Is(err, ErrNonApprovato) {
		t.Errorf("revoca dell'approvazione: %v", err)
	}
	if err := b.RevocaRisposta(ctx, annullata.ID, "rejected", shop); err != nil {
		t.Fatal(err)
	}
	if s := statoBusta(t, b, annullata.ID); s != "preventivo" {
		t.Errorf("annullata: stato %q", s)
	}
	for _, k := range []string{"conservazione_iniziata_il", "preventivo_rifiutato_il", "preventivo_rifiuto_motivo", "preventivo_rifiutato_letto_il"} {
		if v := valore(t, d, `SELECT `+k+` FROM buste WHERE id = ?`, annullata.ID); v != nil {
			t.Errorf("%s: %v", k, v)
		}
	}
	if a := ultimoAudit(t, d, "quote.rejection_revoked"); !strings.Contains(a, `"status":"preventivo"`) || !strings.Contains(a, `"answered_at":"2026-10-31T09:00:00.000000Z"`) {
		t.Errorf("audit %q", a)
	}
	// Il cliente rivede i pulsanti e risponde di nuovo.
	if r := pagina(t, b, annullata.Token).Risposta; r.Fase != RispostaInAttesa {
		t.Errorf("dopo la revoca: %+v", r)
	}
	if err := b.ApprovaPreventivo(ctx, annullata.Token, pubblicoProva); err != nil {
		t.Errorf("nuova risposta: %v", err)
	}
	if err := b.RevocaRisposta(ctx, resta.ID, "rejected", shop); err != nil {
		t.Fatal(err)
	}
	if s := statoBusta(t, b, resta.ID); s != "preventivo" {
		t.Errorf("non annullata: stato %q", s)
	}
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, resta.ID)
	if err := b.RifiutaPreventivo(ctx, resta.Token, "", pubblicoProva); !errors.Is(err, ErrNonInPreventivo) {
		t.Errorf("rifiuto fuori da preventivo dopo la revoca: %v", err)
	}
}
