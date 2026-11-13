package buste

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// «📨 Invii registrati» della scheda e «Togli dalla lista» (CLI-37, RIP-S-21), come
// Outgoing.history_for_repair/1, discardable_ids/1 e discard_handoff/3 dello standalone
// (go-baseline); poi la D-82 ({numero} senza «N.» nei testi), la D-81 (la revoca del rifiuto scrive
// repair.status_changed) e {telegram_optin} a canale spento. Dati inventati.

// rigaInviata scrive una riga di messaggi_in_uscita come le scrive lo standalone (anche email e
// Telegram, che arrivano dall'import) e ne dà l'id.
func rigaInviata(t *testing.T, b *Buste, busta any, canale, parte, indirizzo, testo, stato, accodato string, accettato any) int64 {
	t.Helper()
	esegui(t, b.d, `INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, testo, stato, tentativi,
		prossimo_tentativo_il, accodato_il, accettato_il) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		busta, canale, parte, indirizzo, testo, stato, accodato, accodato, accettato)
	return valore(t, b.d, `SELECT max(id) FROM messaggi_in_uscita`).(int64)
}

// inventario: CLI-37, RIP-S-21
// La lista della scheda: ogni riga della busta, dalla più recente (per id), con canale, parte, stato,
// indirizzo, testo e l'istante (accettato, o messo in coda); solo le righe WhatsApp che non portano un
// avviso di ritiro si possono togliere. Le righe di un'altra busta non ci sono.
func TestInviiDellaScheda(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, false, "stato = 'pronta'")
	altra := bustaAvviso(t, b, false, "")
	if s, err := b.Scheda(ctx, id); err != nil || len(s.Invii) != 0 {
		t.Fatalf("senza invii: %+v %v", s.Invii, err)
	}
	z := apriAvviso(t, b, id, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Avviso di ritiro inventato", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	z = apriAvviso(t, b, id, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Secondo messaggio", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	mail := rigaInviata(t, b, id, "email", "customer", "ada@esempio.test", "Corpo email", "queued", "2026-10-30T08:00:00.000000Z", nil)
	tg := rigaInviata(t, b, id, "telegram", "customer", "123456789", "<b>Pronta</b>", "accepted", "2026-10-30T08:00:00.000000Z", "2026-10-30T08:05:00.000000Z")
	rigaInviata(t, b, altra, "whatsapp", "customer", "+390000000001", "Di un'altra busta", "accepted", "2026-10-30T08:00:00.000000Z", "2026-10-30T08:00:00.000000Z")
	s, err := b.Scheda(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Invii) != 4 {
		t.Fatalf("invii %+v", s.Invii)
	}
	if s.Invii[0].ID != tg || s.Invii[1].ID != mail || s.Invii[2].Testo != "Secondo messaggio" || s.Invii[3].Testo != "Avviso di ritiro inventato" {
		t.Errorf("ordine %+v", s.Invii)
	}
	ora, _ := time.Parse(time.RFC3339, "2026-10-30T08:05:00Z")
	if i := s.Invii[0]; i.Canale != "telegram" || i.Parte != "customer" || i.Stato != "accepted" || i.Indirizzo != "123456789" || !i.Il.Equal(ora) || i.Togliibile {
		t.Errorf("telegram %+v", i)
	}
	ora, _ = time.Parse(time.RFC3339, "2026-10-30T08:00:00Z")
	if i := s.Invii[1]; i.Stato != "queued" || !i.Il.Equal(ora) || i.Togliibile {
		t.Errorf("email (in coda: l'istante della coda) %+v", i)
	}
	if i := s.Invii[2]; !i.Togliibile || i.Canale != "whatsapp" || i.Indirizzo != "+393331112222" {
		t.Errorf("aggiornamento %+v", i)
	}
	if i := s.Invii[3]; i.Togliibile {
		t.Errorf("l'avviso di ritiro si può togliere: %+v", i)
	}
}

// inventario: CLI-37, RIP-S-21
// «Togli dalla lista» toglie una riga WhatsApp della busta, con outgoing.handoff_discarded (canale,
// parte, busta; mai il testo), e non tocca il timbro «link inviato». Rifiuti senza scrivere niente:
// l'avviso di ritiro (ErrInvioAvvisoPronta), una riga di un'altra busta, una riga email o Telegram, una
// riga che non c'è più (ErrInvioNonInLista); una busta che non c'è o è nel cestino è ErrBustaNonTrovata.
func TestTogliInvio(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaAvviso(t, b, false, "")
	altra := bustaAvviso(t, b, false, "")
	z := apriAvviso(t, b, id, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Messaggio privato da togliere", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	mid := valore(t, d, `SELECT id FROM messaggi_in_uscita`).(int64)
	timbro := colonnaBusta(t, b, id, "link_inviato_il")
	if timbro == nil {
		t.Fatal("timbro mancante")
	}
	mail := rigaInviata(t, b, id, "email", "customer", "ada@esempio.test", "Corpo", "queued", "2026-10-30T08:00:00.000000Z", nil)
	dellAltra := rigaInviata(t, b, altra, "whatsapp", "customer", "+390000000001", "Altra", "accepted", "2026-10-30T08:00:00.000000Z", "2026-10-30T08:00:00.000000Z")
	prima := statoAvvisi(t, b)
	for _, c := range []struct {
		busta, invio int64
		err          error
	}{{id, dellAltra, ErrInvioNonInLista}, {id, mail, ErrInvioNonInLista}, {id, 9999, ErrInvioNonInLista}, {altra, mid, ErrInvioNonInLista},
		{9999, mid, ErrBustaNonTrovata}} {
		if err := b.TogliInvio(ctx, c.busta, c.invio, chi); !errors.Is(err, c.err) {
			t.Errorf("%d/%d: %v", c.busta, c.invio, err)
		}
	}
	if statoAvvisi(t, b) != prima {
		t.Fatal("un rifiuto ha scritto")
	}
	if err := b.TogliInvio(ctx, id, mid, chi); err != nil {
		t.Fatal(err)
	}
	if n := conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE id = ?`, mid); n != 0 {
		t.Error("riga rimasta")
	}
	a := ultimoAudit(t, d, "outgoing.handoff_discarded")
	if a != "outgoing.handoff_discarded|outgoing_message|"+itoa(mid)+`|ok|{"channel":"whatsapp","party":"customer","repair_id":`+itoa(id)+`}` {
		t.Errorf("audit %q", a)
	}
	if strings.Contains(a, "privato") {
		t.Error("l'audit porta il testo")
	}
	if v := colonnaBusta(t, b, id, "link_inviato_il"); v != timbro {
		t.Errorf("timbro %v", v)
	}
	// Già tolta: non c'è più.
	if err := b.TogliInvio(ctx, id, mid, chi); !errors.Is(err, ErrInvioNonInLista) {
		t.Errorf("di nuovo: %v", err)
	}
	// Nel cestino.
	esegui(t, d, `UPDATE buste SET cancellata_il = '2026-10-31T09:00:00.000000Z' WHERE id = ?`, altra)
	if err := b.TogliInvio(ctx, altra, dellAltra, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("cestino: %v", err)
	}
}

// inventario: CLI-37, RIP-S-21, CLI-36
// La riga dell'avviso di ritiro non si toglie da qui, nemmeno dopo l'azzeramento (la storia la tiene),
// e niente si scrive.
func TestTogliInvioAvvisoDiRitiro(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaAvviso(t, b, false, "stato = 'pronta'")
	z := apriAvviso(t, b, id, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Ritiro", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	mid := valore(t, d, `SELECT id FROM messaggi_in_uscita`).(int64)
	if err := b.AzzeraAvviso(ctx, id, chi); err != nil {
		t.Fatal(err)
	}
	prima := statoAvvisi(t, b)
	if err := b.TogliInvio(ctx, id, mid, chi); !errors.Is(err, ErrInvioAvvisoPronta) {
		t.Errorf("%v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("il rifiuto ha scritto")
	}
}

// inventario: CLI-33
// D-82: nei testi {numero} è il numero visibile senza «N.»: «1/26» per una busta senza etichetta,
// «0045/26» con l'etichetta; il popup (Numero, «Busta …») resta quello della D-64.
func TestNumeroNeiTestiD82(t *testing.T) {
	b, _, _ := prova(t)
	senza := bustaAvviso(t, b, false, "")
	z := apriAvviso(t, b, senza, ScopoControparte)
	if z.Numero != "N. 1/26" || !strings.Contains(z.Testo, "riparazione n° 1/26 da questo link") || strings.Contains(z.Testo, "N. 1/26") {
		t.Errorf("senza etichetta: %q %q", z.Numero, z.Testo)
	}
	con := bustaAvviso(t, b, false, "codice_qr = 'RIP-2026-0045'")
	z = apriAvviso(t, b, con, ScopoControparte)
	if z.Numero != "0045/26" || !strings.Contains(z.Testo, "riparazione n° 0045/26 da questo link") {
		t.Errorf("con etichetta: %q %q", z.Numero, z.Testo)
	}
	ore := bustaAvviso(t, b, true, "")
	z = apriAvviso(t, b, ore, ScopoControparte)
	if !strings.Contains(z.Testo, "busta n. 3/26\n") || z.Numero != "N. 3/26" {
		t.Errorf("oreficeria: %q %q", z.Numero, z.Testo)
	}
	if NumeroNeiTesti("RIP-2026-0045", "1/26") != "0045/26" || NumeroNeiTesti("", "45/26") != "45/26" || NumeroNeiTesti("SINT-QR-1", "45/26") != "45/26" {
		t.Error("NumeroNeiTesti")
	}
}

// inventario: CLI-32
// {telegram_optin} a canale spento: all'invio diventa <base>/r/<token>/telegram con il token vivo
// della busta, e quel link apre il rifiuto del canale spento (ErrTelegramSpento), mai «non trovato»;
// il popup del cliente diretto lo segnala (TelegramSpento).
func TestTelegramOptinACanaleSpento(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaAvviso(t, b, false, "")
	z := apriAvviso(t, b, id, ScopoControparte)
	if !z.TelegramSpento {
		t.Error("il popup non segnala il canale spento")
	}
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Notifiche: {telegram_optin}", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	testo := valore(t, d, `SELECT testo FROM messaggi_in_uscita`).(string)
	token := tokenSalvato(t, b, id).(string)
	if testo != "Notifiche: "+baseProva+"/r/"+token+"/telegram" {
		t.Fatalf("testo %q", testo)
	}
	if _, err := b.LinkTelegram(ctx, token, baseProva, chi); !errors.Is(err, ErrTelegramSpento) {
		t.Errorf("link valido: %v", err)
	}
}

// inventario: CLI-09, RIP-S-22
// D-81: la revoca del rifiuto che riporta una busta annullata a «preventivo» scrive anche
// repair.status_changed («status»: «preventivo», come gli altri cambi), nella stessa transazione e
// dopo quote.rejection_revoked; senza motivo. Una revoca che non cambia lo stato (busta già in
// preventivo) e la revoca dell'approvazione non la scrivono.
func TestRevocaRifiutoScriveIlCambioD81(t *testing.T) {
	b, d, _ := prova(t)
	annullata := inPreventivo(t, b, "40", nil)
	resta := inPreventivo(t, b, "40", nil)
	for _, c := range []Creata{annullata, resta} {
		if err := b.RifiutaPreventivo(ctx, c.Token, "caro", pubblicoProva); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.CambiaStato(ctx, annullata.ID, "annullata", chi); err != nil {
		t.Fatal(err)
	}
	cambi := func() int {
		return conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`)
	}
	prima := cambi()
	if err := b.RevocaRisposta(ctx, annullata.ID, "rejected", chi); err != nil {
		t.Fatal(err)
	}
	if cambi() != prima+1 {
		t.Fatalf("cambi %d → %d", prima, cambi())
	}
	if a := ultimoAudit(t, d, "repair.status_changed"); a != "repair.status_changed|repair|"+itoa(annullata.ID)+`|ok|{"status":"preventivo"}` {
		t.Errorf("audit %q", a)
	}
	if v := valore(t, d, `SELECT group_concat(azione, ',') FROM (SELECT azione FROM eventi_audit WHERE id_risorsa = ? AND azione IN
		('quote.rejection_revoked', 'repair.status_changed') ORDER BY id DESC LIMIT 2)`, itoa(annullata.ID)); v != "repair.status_changed,quote.rejection_revoked" {
		t.Errorf("ordine %v", v)
	}
	if err := b.RevocaRisposta(ctx, resta.ID, "rejected", chi); err != nil {
		t.Fatal(err)
	}
	if cambi() != prima+1 {
		t.Error("una revoca senza cambio di stato ha scritto status_changed")
	}
	if err := b.ApprovaPreventivo(ctx, annullata.Token, pubblicoProva); err != nil {
		t.Fatal(err)
	}
	if err := b.RevocaRisposta(ctx, annullata.ID, "approved", chi); err != nil {
		t.Fatal(err)
	}
	if cambi() != prima+1 {
		t.Error("la revoca dell'approvazione ha scritto status_changed")
	}
}
