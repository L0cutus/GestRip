package buste

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Gli stati della busta (RIP-S-13, RIP-H-38), come @transitions, @gold_transitions,
// transact_transition/5 e milestone_changes/4 di workshop.ex e Repair.default_status/1.

// inventario: RIP-S-13
// I due grafi, esatti, e l'ordine degli stati di ogni famiglia.
func TestGrafiDegliStati(t *testing.T) {
	riparazioni := map[string][]string{
		"in_lavorazione":      {"preventivo", "pronta", "in_attesa_materiali", "annullata"},
		"preventivo":          {"in_lavorazione", "annullata"},
		"in_attesa_materiali": {"in_lavorazione", "preventivo", "pronta", "annullata"},
		"pronta":              {"in_lavorazione", "consegnata", "annullata"},
		"consegnata":          {},
		"annullata":           {},
	}
	oro := map[string][]string{
		"in_valutazione": {"valutata", "annullata"},
		"valutata":       {"in_valutazione", "perfezionata", "annullata"},
		"perfezionata":   {"chiusa", "annullata"},
		"chiusa":         {},
		"annullata":      {},
	}
	for famiglia, g := range map[bool]map[string][]string{false: riparazioni, true: oro} {
		for stato, attesi := range g {
			if got := Consentiti(stato, famiglia); !reflect.DeepEqual(got, attesi) {
				t.Errorf("compro oro %v, %s: %v, attesi %v", famiglia, stato, got, attesi)
			}
		}
	}
	if got := Consentiti("valutata", false); len(got) != 0 {
		t.Errorf("stato dell'altra famiglia: %v", got)
	}
	if got := strings.Join(Stati(false), " "); got != "in_lavorazione preventivo pronta consegnata in_attesa_materiali annullata" {
		t.Errorf("stati delle riparazioni: %s", got)
	}
	if got := strings.Join(Stati(true), " "); got != "in_valutazione valutata perfezionata chiusa annullata" {
		t.Errorf("stati del compro oro: %s", got)
	}
	// Chi chiama non può cambiare i grafi.
	Consentiti("in_lavorazione", false)[0] = "consegnata"
	Stati(false)[0] = "x"
	if Consentiti("in_lavorazione", false)[0] != "preventivo" || Stati(false)[0] != "in_lavorazione" {
		t.Error("i grafi si cambiano da fuori")
	}
}

// inventario: RIP-H-38, RIP-H-30
// «Sì» al compro oro solo per "1", "on" e "true" (come compro_oro?/1 con i valori di un modulo);
// tutto il resto è una riparazione. Lo stato iniziale viene dal tipo e non si imposta da fuori.
func TestStatoIniziale(t *testing.T) {
	b, d, _ := prova(t)
	for _, c := range []struct {
		valore string
		oro    bool
	}{{"1", true}, {"on", true}, {"true", true}, {"", false}, {"0", false}, {"no", false}, {"false", false},
		{"TRUE", false}, {"On", false}, {" 1", false}, {"yes", false}} {
		if CompraOroDalModulo(c.valore) != c.oro {
			t.Errorf("CompraOroDalModulo(%q) != %v", c.valore, c.oro)
		}
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.CompraOro = c.valore }))
		atteso := "in_lavorazione|0"
		if c.oro {
			atteso = "in_valutazione|1"
		}
		if v := valore(t, d, `SELECT stato || '|' || compro_oro FROM buste WHERE id = ?`, got.ID); v != atteso {
			t.Errorf("%q: %v, atteso %s", c.valore, v, atteso)
		}
	}
	if StatoIniziale(false) != "in_lavorazione" || StatoIniziale(true) != "in_valutazione" {
		t.Error("stato iniziale")
	}
}

// inventario: RIP-S-13
// Una riparazione: pronta scrive la data di pronta (oggi in negozio), consegnata la data di
// consegna e l'inizio della conservazione; un passo fuori dal grafo è «Transizione non
// consentita.». Audit repair.status_changed con il nuovo stato; contatore delle versioni.
func TestCambioStatoDiUnaRiparazione(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	o.Sposta(time.Hour)
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")

	c, err := b.CambiaStato(ctx, busta.ID, "pronta", chi)
	if err != nil || c.Prima != "in_lavorazione" || c.Dopo != "pronta" || c.Versione != 1 {
		t.Fatalf("pronta: %+v %v", c, err)
	}
	riga := func() string {
		return valore(t, d, `SELECT stato || '|' || coalesce(data_pronta, '-') || '|' || coalesce(data_consegna, '-') || '|' ||
			coalesce(conservazione_iniziata_il, '-') || '|' || aggiornato_il FROM buste WHERE id = ?`, busta.ID).(string)
	}
	if got := riga(); got != "pronta|2026-10-31|-|-|"+ora {
		t.Errorf("dopo pronta: %s", got)
	}
	if s := ultimoAudit(t, d, "repair.status_changed"); s != "repair.status_changed|repair|"+itoa(busta.ID)+`|ok|{"status":"pronta"}` {
		t.Errorf("audit %q", s)
	}
	if valore(t, d, `SELECT attore || '|' || ip FROM eventi_audit WHERE azione = 'repair.status_changed'`) != "titolare@esempio.test|203.0.113.7" {
		t.Error("attore dell'audit")
	}

	if _, err := b.CambiaStato(ctx, busta.ID, "preventivo", chi); !errors.Is(err, ErrTransizione) || err.Error() != "Transizione non consentita." {
		t.Errorf("pronta → preventivo: %v", err)
	}
	o.Sposta(24 * time.Hour)
	if c, err := b.CambiaStato(ctx, busta.ID, "consegnata", chi); err != nil || c.Versione != 2 {
		t.Fatalf("consegnata: %+v %v", c, err)
	}
	ora = o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if got := riga(); got != "consegnata|2026-10-31|2026-11-01|"+ora+"|"+ora {
		t.Errorf("dopo consegnata: %s", got)
	}
	for _, s := range []string{"in_lavorazione", "pronta", "annullata", "consegnata"} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); !errors.Is(err, ErrTransizione) {
			t.Errorf("consegnata → %s: %v", s, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`); n != 2 {
		t.Errorf("righe d'audit %d", n)
	}
}

// inventario: RIP-S-13
// Le date storiche non si sovrascrivono: una data di pronta o di consegna già scritta resta.
func TestCambioStatoTieneLeDateStoriche(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET data_pronta = '2026-01-02', data_consegna = '2026-01-03' WHERE id = ?`, busta.ID)
	for _, s := range []string{"pronta", "in_lavorazione", "pronta", "consegnata"} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if v := valore(t, d, `SELECT data_pronta || '|' || data_consegna FROM buste WHERE id = ?`, busta.ID); v != "2026-01-02|2026-01-03" {
		t.Errorf("date %v", v)
	}
	if v := valore(t, d, `SELECT versione FROM versioni_stato_buste WHERE busta_id = ?`, busta.ID); v != int64(4) {
		t.Errorf("versione %v", v)
	}
}

// inventario: RIP-S-13, RIP-H-38
// Una scheda compro oro segue il suo grafo, non prende stati delle riparazioni e non scrive
// date di pronta o di consegna; «chiusa» avvia la conservazione.
func TestCambioStatoCompraOro(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, con(modulo(), func(a *Accettazione) { a.CompraOro = "1" }))
	for _, s := range []string{"pronta", "consegnata", "in_lavorazione", "perfezionata", "chiusa", "sconosciuto", ""} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); !errors.Is(err, ErrTransizione) {
			t.Errorf("in_valutazione → %q: %v", s, err)
		}
	}
	for _, s := range []string{"valutata", "in_valutazione", "valutata", "perfezionata", "chiusa"} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if v := valore(t, d, `SELECT stato || '|' || coalesce(data_pronta, '-') || '|' || coalesce(data_consegna, '-') || '|' || conservazione_iniziata_il FROM buste WHERE id = ?`, busta.ID); v != "chiusa|-|-|"+o.Adesso().Format("2006-01-02T15:04:05.000000Z") {
		t.Errorf("riga %v", v)
	}
	if len(Consentiti("chiusa", true)) != 0 {
		t.Error("chiusa ha uscite")
	}
}

// inventario: RIP-S-13
// Anche «annullata» è uno stato chiuso: avvia la conservazione, in tutte e due le famiglie; uno
// stato aperto non la tocca.
func TestAnnullataAvviaLaConservazione(t *testing.T) {
	b, d, _ := prova(t)
	for _, oro := range []string{"", "1"} {
		busta := crea(t, b, con(modulo(), func(a *Accettazione) { a.CompraOro = oro }))
		primo := "in_attesa_materiali"
		if oro != "" {
			primo = "valutata"
		}
		if _, err := b.CambiaStato(ctx, busta.ID, primo, chi); err != nil {
			t.Fatal(err)
		}
		if valore(t, d, `SELECT conservazione_iniziata_il FROM buste WHERE id = ?`, busta.ID) != nil {
			t.Error("uno stato aperto ha avviato la conservazione")
		}
		if _, err := b.CambiaStato(ctx, busta.ID, "annullata", chi); err != nil {
			t.Fatal(err)
		}
		if valore(t, d, `SELECT conservazione_iniziata_il FROM buste WHERE id = ?`, busta.ID) != istanteProva {
			t.Errorf("compro oro %q: conservazione non avviata", oro)
		}
	}
}

// inventario: RIP-S-13
// Un cambio rifiutato non scrive niente: né stato, né aggiornato_il, né audit, né versione. Una
// busta che non c'è o che sta nel cestino non cambia stato.
func TestCambioStatoRifiutatoNonScriveNiente(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	o.Sposta(time.Hour)
	prima := scatta(t, d)
	riga := valore(t, d, `SELECT stato || aggiornato_il FROM buste WHERE id = ?`, busta.ID)
	for _, s := range []string{"consegnata", "in_lavorazione", "valutata"} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); !errors.Is(err, ErrTransizione) {
			t.Errorf("%s: %v", s, err)
		}
	}
	if _, err := b.CambiaStato(ctx, busta.ID+100, "pronta", chi); !errors.Is(err, ErrTransizione) {
		t.Errorf("busta che non c'è: %v", err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, busta.ID)
	if _, err := b.CambiaStato(ctx, busta.ID, "pronta", chi); !errors.Is(err, ErrTransizione) {
		t.Errorf("busta nel cestino: %v", err)
	}
	uguali(t, "cambio rifiutato", prima, scatta(t, d))
	if valore(t, d, `SELECT stato || aggiornato_il FROM buste WHERE id = ?`, busta.ID) != riga {
		t.Error("la riga è cambiata")
	}
}

// inventario: RIP-S-13
// Il contatore delle versioni lo tiene il database, per chiunque scriva lo stato (come il
// trigger repair_status_versions_bump): un'altra colonna o lo stesso stato non contano.
func TestVersioniContateDalDatabase(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	if v, err := b.Versione(ctx, busta.ID); err != nil || v != 0 {
		t.Fatalf("versione iniziale %d %v", v, err)
	}
	esegui(t, d, `UPDATE buste SET note_interne = 'altro' WHERE id = ?`, busta.ID)
	esegui(t, d, `UPDATE buste SET stato = stato WHERE id = ?`, busta.ID)
	if conta(t, d, `SELECT count(*) FROM versioni_stato_buste`) != 0 {
		t.Error("contata una scrittura che non cambia lo stato")
	}
	esegui(t, d, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, busta.ID)
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, busta.ID)
	if v, _ := b.Versione(ctx, busta.ID); v != 2 {
		t.Errorf("versione %d, attesa 2", v)
	}
	if c, err := b.CambiaStato(ctx, busta.ID, "pronta", chi); err != nil || c.Versione != 3 {
		t.Errorf("cambio %+v %v", c, err)
	}
}

// inventario: RIP-S-13
// Le date di pronta e di consegna sono il giorno del negozio (Europe/Rome): alle 23:30 UTC del
// 31/10 in negozio è già l'1/11.
func TestDateDelCambioSulGiornoDelNegozio(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	o.Imposta(time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC))
	for _, s := range []string{"pronta", "consegnata"} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); err != nil {
			t.Fatal(err)
		}
	}
	if v := valore(t, d, `SELECT data_pronta || '|' || data_consegna FROM buste WHERE id = ?`, busta.ID); v != "2026-11-01|2026-11-01" {
		t.Errorf("date %v", v)
	}
}
