package buste

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// La riapertura di una busta chiusa (RIP-S-14), come Workshop.reopen_repair/3,
// Repair.reopen_statuses/1 e Repair.reopen_changeset/2 dello standalone (go-baseline). I casi
// da workshop_test.exs: «a closed bag reopens only through its own door, with a reason, and keeps
// its dates» e «a cancelled compro oro card reopens into its own family only».

// inventario: RIP-S-14
// Una busta consegnata si riapre solo dalla sua porta: con un motivo (tolto dagli spazi, al
// massimo 255) e verso uno stato di lavoro della sua famiglia. Le date restano, anche dopo una
// seconda consegna; la conservazione si azzera e riparte alla nuova chiusura; l'audit
// repair.reopened ha gli stati da/a e il motivo. Ogni rifiuto non scrive niente.
func TestRiaperturaDiUnaBustaChiusa(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	if _, err := b.CambiaStato(ctx, busta.ID, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	primaChiusura := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if _, err := b.CambiaStato(ctx, busta.ID, "consegnata", chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT conservazione_iniziata_il FROM buste WHERE id = ?`, busta.ID); v != primaChiusura {
		t.Fatalf("conservazione %v", v)
	}
	// La porta ordinaria resta chiusa.
	if _, err := b.CambiaStato(ctx, busta.ID, "in_lavorazione", chi); !errors.Is(err, ErrTransizione) {
		t.Fatalf("consegnata → in_lavorazione: %v", err)
	}
	if got := StatiDiRiapertura("consegnata", false); !reflect.DeepEqual(got, []string{"in_lavorazione", "preventivo", "pronta", "in_attesa_materiali"}) {
		t.Errorf("stati di riapertura %v", got)
	}

	o.Sposta(time.Hour)
	prima := scatta(t, d)
	riga := valore(t, d, `SELECT stato || aggiornato_il || conservazione_iniziata_il FROM buste WHERE id = ?`, busta.ID)
	for _, m := range []string{"", "   ", "\t\n", strings.Repeat("a", 256), " " + strings.Repeat("è", 256) + " "} {
		_, err := b.Riapri(ctx, busta.ID, "in_lavorazione", m, chi)
		campi := erroriCampi(t, err)
		atteso := MsgMotivoObbligatorio
		if strings.TrimSpace(m) != "" {
			atteso = MsgMotivoLungo
		}
		if !reflect.DeepEqual(campi, Campi{"motivo": {atteso}}) {
			t.Errorf("motivo %q: %v", m, campi)
		}
	}
	// Uno stato chiuso non è una riapertura; uno stato vuoto o sconosciuto nemmeno.
	for _, s := range []string{"consegnata", "annullata", "chiusa", "", "valutata", "boh"} {
		campi := erroriCampi(t, func() error { _, err := b.Riapri(ctx, busta.ID, s, "Ripensamento", chi); return err }())
		if !reflect.DeepEqual(campi, Campi{"stato": {MsgNonRiapre}}) {
			t.Errorf("stato %q: %v", s, campi)
		}
	}
	// Tutti gli errori insieme, come il changeset.
	if campi := erroriCampi(t, func() error { _, err := b.Riapri(ctx, busta.ID, "chiusa", " ", chi); return err }()); !reflect.DeepEqual(campi,
		Campi{"stato": {MsgNonRiapre}, "motivo": {MsgMotivoObbligatorio}}) {
		t.Errorf("due errori: %v", campi)
	}
	uguali(t, "riaperture rifiutate", prima, scatta(t, d))
	if valore(t, d, `SELECT stato || aggiornato_il || conservazione_iniziata_il FROM buste WHERE id = ?`, busta.ID) != riga {
		t.Error("la riga è cambiata")
	}

	c, err := b.Riapri(ctx, busta.ID, "in_lavorazione", "  Pietra da rifissare  ", chi)
	if err != nil || c.Prima != "consegnata" || c.Dopo != "in_lavorazione" || c.Versione != 3 {
		t.Fatalf("riapertura: %+v %v", c, err)
	}
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if v := valore(t, d, `SELECT stato || '|' || data_pronta || '|' || data_consegna || '|' || coalesce(conservazione_iniziata_il, '-') || '|' || aggiornato_il
		FROM buste WHERE id = ?`, busta.ID); v != "in_lavorazione|2026-10-31|2026-10-31|-|"+ora {
		t.Errorf("dopo la riapertura: %v", v)
	}
	if s := ultimoAudit(t, d, "repair.reopened"); s != "repair.reopened|repair|"+itoa(busta.ID)+
		`|ok|{"reason":"Pietra da rifissare","status_from":"consegnata","status_to":"in_lavorazione"}` {
		t.Errorf("audit %q", s)
	}
	if valore(t, d, `SELECT attore || '|' || ip FROM eventi_audit WHERE azione = 'repair.reopened'`) != "titolare@esempio.test|203.0.113.7" {
		t.Error("attore dell'audit")
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`); n != 2 {
		t.Errorf("la riapertura ha scritto repair.status_changed: %d", n)
	}

	// Riaperta è una busta come le altre: una seconda consegna non riscrive le date, ma riavvia la
	// conservazione.
	o.Sposta(14 * 24 * time.Hour)
	for _, s := range []string{"pronta", "consegnata"} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); err != nil {
			t.Fatal(err)
		}
	}
	ora = o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if v := valore(t, d, `SELECT data_pronta || '|' || data_consegna || '|' || conservazione_iniziata_il FROM buste WHERE id = ?`, busta.ID); v !=
		"2026-10-31|2026-10-31|"+ora {
		t.Errorf("dopo la seconda consegna: %v", v)
	}

	// Una busta aperta non ha porta di riapertura.
	aperta := crea(t, b, modulo())
	if got := StatiDiRiapertura("in_lavorazione", false); len(got) != 0 {
		t.Errorf("stati di riapertura di una busta aperta: %v", got)
	}
	if campi := erroriCampi(t, func() error {
		_, err := b.Riapri(ctx, aperta.ID, "pronta", "Nessuna chiusura da annullare", chi)
		return err
	}()); !reflect.DeepEqual(campi, Campi{"stato": {MsgNonChiusa}}) {
		t.Errorf("busta aperta: %v", campi)
	}
	if valore(t, d, `SELECT stato FROM buste WHERE id = ?`, aperta.ID) != "in_lavorazione" {
		t.Error("la busta aperta è cambiata")
	}
}

// inventario: RIP-S-14
// Una scheda compro oro annullata si riapre solo nella sua famiglia, senza date.
func TestRiaperturaCompraOro(t *testing.T) {
	b, d, _ := prova(t)
	oro := crea(t, b, con(modulo(), func(a *Accettazione) { a.CompraOro = "1" }))
	if _, err := b.CambiaStato(ctx, oro.ID, "annullata", chi); err != nil {
		t.Fatal(err)
	}
	if got := StatiDiRiapertura("annullata", true); !reflect.DeepEqual(got, []string{"in_valutazione", "valutata", "perfezionata"}) {
		t.Errorf("stati di riapertura %v", got)
	}
	if got := StatiDiRiapertura("annullata", false); !reflect.DeepEqual(got, []string{"in_lavorazione", "preventivo", "pronta", "in_attesa_materiali"}) {
		t.Errorf("stati di riapertura di una riparazione annullata %v", got)
	}
	if campi := erroriCampi(t, func() error {
		_, err := b.Riapri(ctx, oro.ID, "in_lavorazione", "Torna in lavorazione", chi)
		return err
	}()); !reflect.DeepEqual(campi, Campi{"stato": {MsgNonRiapre}}) {
		t.Errorf("stato dell'altra famiglia: %v", campi)
	}
	if _, err := b.Riapri(ctx, oro.ID, "valutata", "Cliente tornato", chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT stato || '|' || compro_oro || '|' || coalesce(data_pronta, '-') || '|' || coalesce(data_consegna, '-') || '|' ||
		coalesce(conservazione_iniziata_il, '-') FROM buste WHERE id = ?`, oro.ID); v != "valutata|1|-|-|-" {
		t.Errorf("riga %v", v)
	}
	if got := Consentiti("valutata", true); !reflect.DeepEqual(got, []string{"in_valutazione", "perfezionata", "annullata"}) {
		t.Errorf("consentiti dopo la riapertura %v", got)
	}
}

// inventario: RIP-S-14
// Il limite del motivo è in grafemi (validate_length di Ecto): 255 «è» scritte come «e» più
// l'accento combinante passano; una busta che non c'è o sta nel cestino non si riapre; un audit
// che non si scrive lascia la busta chiusa; la riapertura non spegne né riaccende Telegram.
func TestRiaperturaLimitiECestino(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	for _, s := range []string{"annullata"} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); err != nil {
			t.Fatal(err)
		}
	}
	esegui(t, d, `CREATE TEMP TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.reopened' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	prima := scatta(t, d)
	if _, err := b.Riapri(ctx, busta.ID, "pronta", "Motivo", chi); err == nil || errors.Is(err, ErrBustaNonTrovata) {
		t.Fatalf("audit rifiutato: %v", err)
	}
	uguali(t, "audit rifiutato", prima, scatta(t, d))
	if valore(t, d, `SELECT stato || '|' || coalesce(conservazione_iniziata_il, '-') FROM buste WHERE id = ?`, busta.ID) != "annullata|"+istanteProva {
		t.Error("la busta è cambiata senza audit")
	}
	esegui(t, d, `DROP TRIGGER blocca`)

	lungo := strings.Repeat("é", 255)
	if _, err := b.Riapri(ctx, busta.ID, "pronta", lungo, chi); err != nil {
		t.Fatalf("255 grafemi: %v", err)
	}
	if s := ultimoAudit(t, d, "repair.reopened"); !strings.Contains(s, `"reason":"`+lungo+`"`) {
		t.Errorf("motivo nell'audit %q", s)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_switched_off'`) != 0 {
		t.Error("audit Telegram")
	}

	for _, id := range []int64{busta.ID + 100, 0, -1} {
		if _, err := b.Riapri(ctx, id, "pronta", "Motivo", chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("busta %d: %v", id, err)
		}
	}
	if _, err := b.CambiaStato(ctx, busta.ID, "annullata", chi); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, busta.ID)
	if _, err := b.Riapri(ctx, busta.ID, "pronta", "Motivo", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta nel cestino: %v", err)
	}
	if valore(t, d, `SELECT stato FROM buste WHERE id = ?`, busta.ID) != "annullata" {
		t.Error("la busta nel cestino è cambiata")
	}
}
