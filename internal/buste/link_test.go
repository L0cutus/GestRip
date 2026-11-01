package buste

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Il link pubblico della busta sulla scheda (RIP-S-24, CLI-23, CLI-24, CLI-25), come lo standalone
// (go-baseline). I casi da workshop/public_link_test.exs («regenerate_public_link/2», «revoke_public_access/2»,
// «from a stale page», «the token never leaves the database»), workshop/legacy_public_revocation_test.exs,
// telegram_subscriptions_test.exs («a customer link revoked or replaced») ed
// email_ready_notice_test.exs («a customer link revoked or replaced (slice 154)»).

var reToken = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func impronta(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func tokenSalvato(t *testing.T, b *Buste, id int64) any {
	t.Helper()
	return valore(t, b.d, `SELECT token_pubblico FROM buste WHERE id = ?`, id)
}

func colonnaBusta(t *testing.T, b *Buste, id int64, c string) any {
	t.Helper()
	return valore(t, b.d, `SELECT `+c+` FROM buste WHERE id = ?`, id)
}

func scheda(t *testing.T, b *Buste, id int64) Scheda {
	t.Helper()
	s, err := b.Scheda(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// conCodice dà alla busta il codice del vecchio link /stato (with_code/1 dei test dello standalone).
func conCodice(t *testing.T, b *Buste, id int64) {
	t.Helper()
	esegui(t, b.d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, bytes.Repeat([]byte{7}, 32), id)
}

// importata segna la busta come venuta dall'import Flask (imported/1).
func importata(t *testing.T, b *Buste, id int64) {
	t.Helper()
	esegui(t, b.d, `INSERT INTO corrispondenze_import_flask (tabella_flask, id_flask, tabella_standalone, id_standalone, creato_il)
		VALUES ('riparazioni', '900', 'repairs', ?, ?)`, id, istanteProva)
}

func quanteAzioni(t *testing.T, b *Buste, azione string) int {
	t.Helper()
	return conta(t, b.d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azione)
}

// inventario: RIP-S-24, CLI-23
// Lo stato del link come public_link_status/1 e legacy_link_status/1: attivo con la scadenza
// (90 giorni dalla busta), solo impronta con la scadenza, scaduto alla scadenza esatta e dopo (senza
// scadenza), revocato prima di tutto il resto; il vecchio link con codice c'è con il codice o con la
// riga dell'import Flask, ed è revocato solo se c'è; «inviato il» dalla colonna. La scheda non porta
// mai il testo del token.
func TestStatoDelLink(t *testing.T) {
	b, _, o := prova(t)
	busta := crea(t, b, modulo())
	scade := time.Date(2027, 1, 29, 9, 0, 0, 0, time.UTC)

	l := scheda(t, b, busta.ID).Link
	if l.Stato != LinkAttivo || l.Scade == nil || !l.Scade.Equal(scade) || l.Storico != StoricoNessuno || l.InviatoIl != nil || !l.Vivo() {
		t.Errorf("busta nuova: %+v", l)
	}
	if s := fmt.Sprintf("%+v %#v", scheda(t, b, busta.ID), scheda(t, b, busta.ID)); strings.Contains(s, busta.Token) {
		t.Error("la scheda porta il token")
	}

	esegui(t, b.d, `UPDATE buste SET token_pubblico = NULL WHERE id = ?`, busta.ID)
	if l := scheda(t, b, busta.ID).Link; l.Stato != LinkSoloImpronta || l.Scade == nil || !l.Scade.Equal(scade) || !l.Vivo() {
		t.Errorf("solo impronta: %+v", l)
	}
	// Scaduto alla scadenza esatta (DateTime.compare != :gt).
	o.Imposta(scade)
	if l := scheda(t, b, busta.ID).Link; l.Stato != LinkScaduto || l.Scade != nil || l.Vivo() {
		t.Errorf("alla scadenza: %+v", l)
	}
	o.Imposta(scade.Add(-time.Microsecond))
	if l := scheda(t, b, busta.ID).Link; l.Stato != LinkSoloImpronta {
		t.Errorf("un attimo prima: %+v", l)
	}
	o.Imposta(scade.Add(time.Hour))
	esegui(t, b.d, `UPDATE buste SET token_pubblico = 'gettone-finto' WHERE id = ?`, busta.ID)
	if l := scheda(t, b, busta.ID).Link; l.Stato != LinkScaduto {
		t.Errorf("scaduto con il token: %+v", l)
	}
	o.Imposta(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))
	esegui(t, b.d, `UPDATE buste SET token_pubblico_revocato_il = ?, token_pubblico_scade_il = ? WHERE id = ?`,
		istanteProva, "2026-10-01T00:00:00.000000Z", busta.ID)
	if l := scheda(t, b, busta.ID).Link; l.Stato != LinkRevocato || l.Scade != nil || l.Vivo() {
		t.Errorf("revocato e scaduto: %+v", l)
	}

	// Il vecchio link con codice.
	esegui(t, b.d, `UPDATE buste SET codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, busta.ID)
	if l := scheda(t, b, busta.ID).Link; l.Storico != StoricoNessuno {
		t.Errorf("revocato senza codice: %v", l.Storico)
	}
	altra := crea(t, b, modulo())
	conCodice(t, b, altra.ID)
	if l := scheda(t, b, altra.ID).Link; l.Storico != StoricoVivo {
		t.Errorf("con il codice: %v", l.Storico)
	}
	esegui(t, b.d, `UPDATE buste SET codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, altra.ID)
	if l := scheda(t, b, altra.ID).Link; l.Storico != StoricoRevocato {
		t.Errorf("codice revocato: %v", l.Storico)
	}
	terza := crea(t, b, modulo())
	importata(t, b, terza.ID)
	if l := scheda(t, b, terza.ID).Link; l.Storico != StoricoVivo {
		t.Errorf("importata: %v", l.Storico)
	}
	// Una riga dell'import di un'altra tabella non conta.
	quarta := crea(t, b, modulo())
	esegui(t, b.d, `INSERT INTO corrispondenze_import_flask (tabella_flask, id_flask, tabella_standalone, id_standalone, creato_il)
		VALUES ('clienti', '901', 'customers', ?, ?)`, quarta.ID, istanteProva)
	if l := scheda(t, b, quarta.ID).Link; l.Storico != StoricoNessuno {
		t.Errorf("riga di un'altra tabella: %v", l.Storico)
	}

	esegui(t, b.d, `UPDATE buste SET link_inviato_il = ? WHERE id = ?`, "2026-10-30T23:30:00.000000Z", quarta.ID)
	if l := scheda(t, b, quarta.ID).Link; l.InviatoIl == nil || !l.InviatoIl.Equal(time.Date(2026, 10, 30, 23, 30, 0, 0, time.UTC)) {
		t.Errorf("inviato il: %+v", l.InviatoIl)
	}
}

// inventario: RIP-S-24, CLI-24
// «Revoca link» (revoke_public_access/2): segna la revoca, toglie il testo del token (l'impronta
// resta, così il vecchio link dà «non trovato»), non ne emette uno nuovo; revoca per sempre il link
// con codice (il primo istante resta); repair.public_link_revoked senza dati, o con legacy: true se il
// link con codice era vivo (codice o riga dell'import). La conversazione pubblica non cambia.
// Dice se la busta è di un'oreficeria. Una revoca ripetuta (POST forgiato) riscrive l'istante.
func TestRevocaLink(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	o.Sposta(time.Hour)
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")

	b2b, err := b.RevocaLink(ctx, busta.ID, chi)
	if err != nil || b2b {
		t.Fatalf("revoca: %v %v", b2b, err)
	}
	if tokenSalvato(t, b, busta.ID) != nil {
		t.Error("il token è rimasto")
	}
	if !bytes.Equal(colonnaBusta(t, b, busta.ID, "hash_token_pubblico").([]byte), impronta(busta.Token)) {
		t.Error("l'impronta è cambiata")
	}
	for _, c := range []string{"token_pubblico_revocato_il", "codice_flask_revocato_il", "aggiornato_il"} {
		if v := colonnaBusta(t, b, busta.ID, c); v != ora {
			t.Errorf("%s = %v", c, v)
		}
	}
	if v := colonnaBusta(t, b, busta.ID, "messaggi_visibili_dal"); v != nil {
		t.Errorf("la conversazione è cambiata: %v", v)
	}
	if s := ultimoAudit(t, d, "repair.public_link_revoked"); s != "repair.public_link_revoked|repair|"+itoa(busta.ID)+"|ok|{}" {
		t.Errorf("audit %q", s)
	}
	if valore(t, d, `SELECT attore || '|' || ip FROM eventi_audit WHERE azione = 'repair.public_link_revoked'`) != "titolare@esempio.test|203.0.113.7" {
		t.Error("attore dell'audit")
	}
	if quanteAzioni(t, b, "repair.public_link_issued") != 0 || quanteAzioni(t, b, "repair.public_access_revoked") != 0 {
		t.Error("la revoca ha emesso un link")
	}
	if l := scheda(t, b, busta.ID).Link; l.Stato != LinkRevocato || l.Storico != StoricoNessuno {
		t.Errorf("dopo la revoca: %+v", l)
	}

	// Ripetuta: l'istante della revoca si riscrive, quello del codice resta.
	o.Sposta(time.Hour)
	if _, err := b.RevocaLink(ctx, busta.ID, chi); err != nil {
		t.Fatal(err)
	}
	if v := colonnaBusta(t, b, busta.ID, "token_pubblico_revocato_il"); v != o.Adesso().Format("2006-01-02T15:04:05.000000Z") {
		t.Errorf("seconda revoca: %v", v)
	}
	if v := colonnaBusta(t, b, busta.ID, "codice_flask_revocato_il"); v != ora {
		t.Errorf("il primo istante del codice è cambiato: %v", v)
	}

	// Con il codice: legacy: true, il codice resta; poi non è più vivo.
	conCodiceB := crea(t, b, modulo())
	conCodice(t, b, conCodiceB.ID)
	if _, err := b.RevocaLink(ctx, conCodiceB.ID, chi); err != nil {
		t.Fatal(err)
	}
	if s := ultimoAudit(t, d, "repair.public_link_revoked"); s != "repair.public_link_revoked|repair|"+itoa(conCodiceB.ID)+"|ok|{\"legacy\":true}" {
		t.Errorf("audit con il codice %q", s)
	}
	if !bytes.Equal(colonnaBusta(t, b, conCodiceB.ID, "hash_codice_flask").([]byte), bytes.Repeat([]byte{7}, 32)) {
		t.Error("il codice è cambiato")
	}
	if l := scheda(t, b, conCodiceB.ID).Link; l.Storico != StoricoRevocato {
		t.Errorf("storico dopo la revoca: %v", l.Storico)
	}
	if _, err := b.RevocaLink(ctx, conCodiceB.ID, chi); err != nil {
		t.Fatal(err)
	}
	if s := ultimoAudit(t, d, "repair.public_link_revoked"); !strings.HasSuffix(s, "|ok|{}") {
		t.Errorf("una revoca dopo quella del codice è legacy: %q", s)
	}

	// Importata senza codice: anche lei ha il vecchio link.
	imp := crea(t, b, modulo())
	importata(t, b, imp.ID)
	if _, err := b.RevocaLink(ctx, imp.ID, chi); err != nil {
		t.Fatal(err)
	}
	if s := ultimoAudit(t, d, "repair.public_link_revoked"); !strings.HasSuffix(s, "|ok|{\"legacy\":true}") {
		t.Errorf("importata: %q", s)
	}

	// Di un'oreficeria esterna; la riga 1 è il negozio.
	for _, c := range []struct {
		oreficeria string
		b2b        bool
	}{{"2", true}, {"", false}} {
		x := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = c.oreficeria }))
		if got, err := b.RevocaLink(ctx, x.ID, chi); err != nil || got != c.b2b {
			t.Errorf("oreficeria %q: %v %v", c.oreficeria, got, err)
		}
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 1 WHERE id = ?`, busta.ID)
	if got, err := b.RevocaLink(ctx, busta.ID, chi); err != nil || got {
		t.Errorf("riga 1: %v %v", got, err)
	}
}

// inventario: RIP-S-24, CLI-24
// La revoca non trova una busta che non c'è o sta nel cestino, e non scrive niente; se l'audit non
// si scrive, il link non cambia. Legge la riga salvata, non quella di una pagina: un token emesso
// dopo si toglie lo stesso.
func TestRevocaLinkRifiutiETransazione(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	nel := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, nel.ID)
	prima := scatta(t, d)
	for _, id := range []int64{busta.ID + 100, 0, -1, nel.ID} {
		if _, err := b.RevocaLink(ctx, id, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("busta %d: %v", id, err)
		}
		if _, err := b.NuovoLink(ctx, id, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("nuovo link busta %d: %v", id, err)
		}
	}
	uguali(t, "non trovata", prima, scatta(t, d))
	if tokenSalvato(t, b, nel.ID) != nel.Token {
		t.Error("la busta nel cestino è cambiata")
	}

	esegui(t, d, `CREATE TEMP TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.public_link_revoked' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.RevocaLink(ctx, busta.ID, chi); err == nil || errors.Is(err, ErrBustaNonTrovata) {
		t.Fatalf("audit rifiutato: %v", err)
	}
	if tokenSalvato(t, b, busta.ID) != busta.Token || colonnaBusta(t, b, busta.ID, "token_pubblico_revocato_il") != nil ||
		colonnaBusta(t, b, busta.ID, "codice_flask_revocato_il") != nil {
		t.Error("revoca senza audit")
	}
	esegui(t, d, `DROP TRIGGER blocca`)

	// Una pagina vecchia: il token di adesso si toglie.
	esegui(t, d, `UPDATE buste SET token_pubblico = 'gettone-emesso-dopo' WHERE id = ?`, busta.ID)
	if _, err := b.RevocaLink(ctx, busta.ID, chi); err != nil {
		t.Fatal(err)
	}
	if tokenSalvato(t, b, busta.ID) != nil {
		t.Error("il token emesso dopo è rimasto")
	}
}

// inventario: RIP-S-24, CLI-25
// «Genera nuovo link» (regenerate_public_link/2): sempre un token nuovo (32 byte in base64url),
// salvato con l'impronta del testo, scadenza a 90 giorni da adesso, revoca tolta; il vecchio link
// smette di funzionare (la sua impronta non c'è più); repair.public_link_issued con il motivo
// manual (e legacy: true se il link con codice era vivo, che si revoca per sempre: il primo istante
// resta). Dice se la busta è di un'oreficeria.
func TestNuovoLink(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	if _, err := b.RevocaLink(ctx, busta.ID, chi); err != nil {
		t.Fatal(err)
	}
	revocaCodice := colonnaBusta(t, b, busta.ID, "codice_flask_revocato_il")
	o.Sposta(48 * time.Hour)
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")

	n, err := b.NuovoLink(ctx, busta.ID, chi)
	if err != nil || n.Oreficeria {
		t.Fatalf("nuovo link: %v %v", n, err)
	}
	if !reToken.MatchString(n.Token) || n.Token == busta.Token {
		t.Errorf("token %d caratteri", len(n.Token))
	}
	if tokenSalvato(t, b, busta.ID) != n.Token {
		t.Error("il token nuovo non è salvato")
	}
	if !bytes.Equal(colonnaBusta(t, b, busta.ID, "hash_token_pubblico").([]byte), impronta(n.Token)) {
		t.Error("impronta del token nuovo")
	}
	if conta(t, d, `SELECT count(*) FROM buste WHERE hash_token_pubblico = ?`, impronta(busta.Token)) != 0 {
		t.Error("il vecchio link risponde ancora")
	}
	if v := colonnaBusta(t, b, busta.ID, "token_pubblico_scade_il"); v != o.Adesso().Add(90*24*time.Hour).Format("2006-01-02T15:04:05.000000Z") {
		t.Errorf("scadenza %v", v)
	}
	if v := colonnaBusta(t, b, busta.ID, "token_pubblico_revocato_il"); v != nil {
		t.Errorf("revoca rimasta: %v", v)
	}
	if v := colonnaBusta(t, b, busta.ID, "codice_flask_revocato_il"); v != revocaCodice {
		t.Errorf("il primo istante del codice è cambiato: %v", v)
	}
	if v := colonnaBusta(t, b, busta.ID, "aggiornato_il"); v != ora {
		t.Errorf("aggiornato_il %v", v)
	}
	if s := ultimoAudit(t, d, "repair.public_link_issued"); s != "repair.public_link_issued|repair|"+itoa(busta.ID)+"|ok|{\"reason\":\"manual\"}" {
		t.Errorf("audit %q", s)
	}
	if l := scheda(t, b, busta.ID).Link; l.Stato != LinkAttivo || l.Scade == nil || !l.Scade.Equal(o.Adesso().Add(90*24*time.Hour)) {
		t.Errorf("dopo: %+v", l)
	}
	if again, err := b.NuovoLink(ctx, busta.ID, chi); err != nil || again.Token == n.Token {
		t.Errorf("secondo nuovo link: %v", err)
	}

	// Con il codice vivo: legacy: true e il codice revocato adesso.
	cc := crea(t, b, modulo())
	conCodice(t, b, cc.ID)
	if _, err := b.NuovoLink(ctx, cc.ID, chi); err != nil {
		t.Fatal(err)
	}
	if s := ultimoAudit(t, d, "repair.public_link_issued"); s != "repair.public_link_issued|repair|"+itoa(cc.ID)+"|ok|{\"legacy\":true,\"reason\":\"manual\"}" {
		t.Errorf("audit con il codice %q", s)
	}
	if v := colonnaBusta(t, b, cc.ID, "codice_flask_revocato_il"); v != ora {
		t.Errorf("codice non revocato: %v", v)
	}
	if l := scheda(t, b, cc.ID).Link; l.Storico != StoricoRevocato || l.Stato != LinkAttivo {
		t.Errorf("con il codice dopo: %+v", l)
	}
	if _, err := b.NuovoLink(ctx, cc.ID, chi); err != nil {
		t.Fatal(err)
	}
	if s := ultimoAudit(t, d, "repair.public_link_issued"); !strings.HasSuffix(s, "|ok|{\"reason\":\"manual\"}") {
		t.Errorf("secondo con il codice revocato: %q", s)
	}

	x := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	if n, err := b.NuovoLink(ctx, x.ID, chi); err != nil || !n.Oreficeria {
		t.Errorf("oreficeria: %v %v", n, err)
	}

	// Se l'audit non si scrive, il link resta quello di prima.
	prima := tokenSalvato(t, b, x.ID)
	esegui(t, d, `CREATE TEMP TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.public_link_issued' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.NuovoLink(ctx, x.ID, chi); err == nil || errors.Is(err, ErrBustaNonTrovata) {
		t.Fatalf("audit rifiutato: %v", err)
	}
	if tokenSalvato(t, b, x.ID) != prima {
		t.Error("nuovo link senza audit")
	}
}

// iscrizioneCon è un'iscrizione Telegram con consenso, fermata e motivo dati.
func iscrizioneCon(t *testing.T, b *Buste, busta, chat int64, fermata, motivo any) int64 {
	t.Helper()
	esegui(t, b.d, `INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, consenso_il, fermata_il, motivo_fermata, creato_il, aggiornato_il)
		VALUES (?2, ?3, ?4, ?1, ?1, ?5, ?6, ?1, ?1)`, istanteProva, busta, chat, fermata == nil, fermata, motivo)
	return valore(t, b.d, `SELECT max(id) FROM iscrizioni_telegram`).(int64)
}

func inCoda(t *testing.T, b *Buste, busta any, canale, destinatario, indirizzo string) int64 {
	t.Helper()
	esegui(t, b.d, `INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, testo, stato, prossimo_tentativo_il, accodato_il)
		VALUES (?, ?, ?, ?, 'Testo inventato', 'queued', ?, ?)`, busta, canale, destinatario, indirizzo, istanteProva, istanteProva)
	return valore(t, b.d, `SELECT max(id) FROM messaggi_in_uscita`).(int64)
}

func messaggio(t *testing.T, b *Buste, id int64) string {
	t.Helper()
	return valore(t, b.d, `SELECT stato || '|' || coalesce(fallito_il, '∅') || '|' || coalesce(ultimo_errore, '∅') FROM messaggi_in_uscita WHERE id = ?`, id).(string)
}

// inventario: RIP-S-24, CLI-24, CLI-25
// I due comandi spengono le iscrizioni Telegram della busta (TelegramSubscriptions.cut_off/2):
// consenso 0, fermata adesso, motivo «link», una riga repair.telegram_stop per iscrizione attiva
// (busta, iscrizione, via «link», mai la chat); un'iscrizione già fermata prende il motivo «link» e
// tiene il suo istante, senza riga (con il motivo NULL resta NULL, come stop_reason != "link" in SQL); via i token t.me della busta; i messaggi Telegram al cliente
// ancora in coda falliscono con il testo dello standalone. Le altre buste restano come sono.
func TestLinkTagliaTelegram(t *testing.T) {
	for _, nome := range []string{"revoca", "nuovo"} {
		t.Run(nome, func(t *testing.T) {
			b, d, o := prova(t)
			busta, altra := crea(t, b, modulo()), crea(t, b, modulo())
			attiva := iscrizioneCon(t, b, busta.ID, 4242001, nil, nil)
			fermata := iscrizioneCon(t, b, busta.ID, 4242002, "2026-10-01T00:00:00.000000Z", "bot")
			giaLink := iscrizioneCon(t, b, busta.ID, 4242003, "2026-10-02T00:00:00.000000Z", "link")
			// stop_reason != "link" in SQL: un motivo NULL resta NULL.
			senzaMotivo := iscrizioneCon(t, b, busta.ID, 4242004, "2026-10-03T00:00:00.000000Z", nil)
			dAltra := iscrizioneCon(t, b, altra.ID, 4242001, nil, nil)
			tokenTelegram(t, b, busta.ID, "token-telegram-finto-1")
			tokenTelegram(t, b, altra.ID, "token-telegram-finto-2")
			tg := inCoda(t, b, busta.ID, "telegram", "customer", "4242001")
			tgNegozio := inCoda(t, b, busta.ID, "telegram", "shop", "4242009")
			tgAltra := inCoda(t, b, altra.ID, "telegram", "customer", "4242001")
			o.Sposta(time.Hour)
			ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
			var err error
			if nome == "revoca" {
				_, err = b.RevocaLink(ctx, busta.ID, chi)
			} else {
				_, err = b.NuovoLink(ctx, busta.ID, chi)
			}
			if err != nil {
				t.Fatal(err)
			}
			riga := func(id int64) string {
				return valore(t, d, `SELECT consenso || '|' || coalesce(fermata_il, '∅') || '|' || coalesce(motivo_fermata, '∅') || '|' || aggiornato_il
					FROM iscrizioni_telegram WHERE id = ?`, id).(string)
			}
			for id, atteso := range map[int64]string{
				attiva:      "0|" + ora + "|link|" + ora,
				fermata:     "0|2026-10-01T00:00:00.000000Z|link|" + ora,
				giaLink:     "0|2026-10-02T00:00:00.000000Z|link|" + istanteProva,
				dAltra:      "1|∅|∅|" + istanteProva,
				senzaMotivo: "0|2026-10-03T00:00:00.000000Z|∅|" + istanteProva,
			} {
				if got := riga(id); got != atteso {
					t.Errorf("iscrizione %d: %s, attesa %s", id, got, atteso)
				}
			}
			if n := conta(t, d, `SELECT count(*) FROM token_telegram WHERE busta_id = ?`, busta.ID); n != 0 {
				t.Errorf("token della busta: %d", n)
			}
			if n := conta(t, d, `SELECT count(*) FROM token_telegram WHERE busta_id = ?`, altra.ID); n != 1 {
				t.Errorf("token dell'altra busta: %d", n)
			}
			if got := messaggio(t, b, tg); got != "failed|"+ora+"|Iscrizione Telegram chiusa con il link della busta" {
				t.Errorf("messaggio in coda: %s", got)
			}
			for _, id := range []int64{tgNegozio, tgAltra} {
				if got := messaggio(t, b, id); got != "queued|∅|∅" {
					t.Errorf("messaggio %d: %s", id, got)
				}
			}
			if n := quanteAzioni(t, b, "repair.telegram_stop"); n != 1 {
				t.Errorf("repair.telegram_stop %d", n)
			}
			atteso := fmt.Sprintf(`repair.telegram_stop|repair|%d|ok|{"bag_number":%q,"subscription_id":%d,"via":"link"}`, busta.ID, busta.Numero, attiva)
			if s := ultimoAudit(t, d, "repair.telegram_stop"); s != atteso {
				t.Errorf("audit %q, atteso %q", s, atteso)
			}
			if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%4242001%'`) != 0 {
				t.Error("la chat è nell'audit")
			}
		})
	}
}

// inventario: RIP-S-24, CLI-24, CLI-25
// I due comandi tolgono l'indirizzo email della busta (EmailNotice.cut_off/2): indirizzo, conferma
// e invio della pronta vuoti; le email ancora in coda per quell'indirizzo falliscono con il testo
// dello standalone (le altre restano); repair.email_optout con busta e via «link», mai l'indirizzo.
// Senza indirizzo, nessuna riga.
func TestLinkTagliaEmail(t *testing.T) {
	for _, nome := range []string{"revoca", "nuovo"} {
		t.Run(nome, func(t *testing.T) {
			b, d, o := prova(t)
			busta, senza := crea(t, b, modulo()), crea(t, b, modulo())
			esegui(t, d, `UPDATE buste SET email_cliente = 'cliente@esempio.test', email_confermata_il = ?1, email_pronta_inviata_il = ?1 WHERE id = ?2`, istanteProva, busta.ID)
			pronta := inCoda(t, b, busta.ID, "email", "customer", "cliente@esempio.test")
			altroIndirizzo := inCoda(t, b, busta.ID, "email", "customer", "vecchio@esempio.test")
			o.Sposta(time.Hour)
			ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
			taglia := func(id int64) {
				var err error
				if nome == "revoca" {
					_, err = b.RevocaLink(ctx, id, chi)
				} else {
					_, err = b.NuovoLink(ctx, id, chi)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			taglia(busta.ID)
			if got := valore(t, d, `SELECT coalesce(email_cliente, '∅') || coalesce(email_confermata_il, '∅') || coalesce(email_pronta_inviata_il, '∅') FROM buste WHERE id = ?`, busta.ID); got != "∅∅∅" {
				t.Errorf("indirizzo: %v", got)
			}
			if got := messaggio(t, b, pronta); got != "failed|"+ora+"|Avvisi email chiusi con il link della busta" {
				t.Errorf("email in coda: %s", got)
			}
			if got := messaggio(t, b, altroIndirizzo); got != "queued|∅|∅" {
				t.Errorf("email a un altro indirizzo: %s", got)
			}
			atteso := fmt.Sprintf(`repair.email_optout|repair|%d|ok|{"bag_number":%q,"via":"link"}`, busta.ID, busta.Numero)
			if s := ultimoAudit(t, d, "repair.email_optout"); s != atteso {
				t.Errorf("audit %q", s)
			}
			if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%esempio.test%'`) != 0 {
				t.Error("l'indirizzo è nell'audit")
			}
			taglia(senza.ID)
			if n := quanteAzioni(t, b, "repair.email_optout"); n != 1 {
				t.Errorf("repair.email_optout %d", n)
			}
		})
	}
}

// inventario: RIP-S-24, CLI-25
// Il token non esce dal database: le righe d'audit dei due comandi hanno il motivo e mai il
// token; LinkNuovo lo nasconde in %v, %+v, %#v e slog; gli errori non lo portano.
func TestLinkTokenMaiFuori(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	n, err := b.NuovoLink(ctx, busta.ID, chi)
	if err != nil {
		t.Fatal(err)
	}
	m, err := b.NuovoLink(ctx, busta.ID, chi)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.RevocaLink(ctx, busta.ID, chi); err != nil {
		t.Fatal(err)
	}
	var righe string
	if err := d.Lettura.QueryRow(`SELECT group_concat(attore || azione || risorsa || id_risorsa || coalesce(dati, ''), ' ') FROM eventi_audit`).Scan(&righe); err != nil {
		t.Fatal(err)
	}
	for _, segreto := range []string{busta.Token, n.Token, m.Token} {
		if strings.Contains(righe, segreto) {
			t.Error("un token è nell'audit")
		}
	}
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("prova", "link", n)
	for _, s := range []string{fmt.Sprint(n), fmt.Sprintf("%+v", n), fmt.Sprintf("%#v", n), log.String()} {
		if strings.Contains(s, n.Token) || !strings.Contains(s, "redacted") {
			t.Errorf("stampa: %s", s)
		}
	}
}

// gettoneFinto è un token evidentemente finto, di 43 caratteri come quelli veri.
const gettoneFinto = "GETTONE-FINTO-DI-PROVA-0000000000000000000x"

// inventario: RIP-S-24, CLI-25
// Un «Genera nuovo link» che fallisce (aggiornamento, audit, tagli dei canali, commit) dà un errore
// che non porta il token appena generato, e non cambia il link.
func TestNuovoLinkErroriSenzaToken(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	b.nuovoGettone = func() (string, []byte) { return gettoneFinto, impronta(gettoneFinto) }
	if len(gettoneFinto) != 43 {
		t.Fatal("token finto della lunghezza sbagliata")
	}
	iscrizioneCon(t, b, busta.ID, 4242001, nil, nil)
	for _, c := range []struct{ nome, guasto string }{
		{"aggiornamento", `CREATE TEMP TRIGGER blocca BEFORE UPDATE OF token_pubblico ON buste BEGIN SELECT RAISE(ABORT, 'no'); END`},
		{"audit", `CREATE TEMP TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.public_link_issued' BEGIN SELECT RAISE(ABORT, 'no'); END`},
		{"telegram", `CREATE TEMP TRIGGER blocca BEFORE UPDATE ON iscrizioni_telegram BEGIN SELECT RAISE(ABORT, 'no'); END`},
		// Una chiave esterna differita violata: l'errore arriva solo al Commit.
		{"commit", `CREATE TEMP TRIGGER blocca AFTER UPDATE OF token_pubblico ON buste BEGIN INSERT INTO figli_finti (x) VALUES (999); END`},
	} {
		nome := c.nome
		esegui(t, d, `CREATE TEMP TABLE IF NOT EXISTS padri_finti (id INTEGER PRIMARY KEY)`)
		esegui(t, d, `CREATE TEMP TABLE IF NOT EXISTS figli_finti (x INTEGER REFERENCES padri_finti (id) DEFERRABLE INITIALLY DEFERRED)`)
		esegui(t, d, c.guasto)
		_, err := b.NuovoLink(ctx, busta.ID, chi)
		esegui(t, d, `DROP TRIGGER blocca`)
		if err == nil {
			t.Errorf("%s: nessun errore", nome)
			continue
		}
		if strings.Contains(err.Error(), gettoneFinto) || strings.Contains(fmt.Sprintf("%+v %#v", err, err), gettoneFinto) {
			t.Errorf("%s: l'errore porta il token: %v", nome, err)
		}
		if tokenSalvato(t, b, busta.ID) != busta.Token {
			t.Errorf("%s: il link è cambiato", nome)
		}
	}
	// Dopo i guasti la scrittura riparte: niente transazione rimasta aperta.
	if n, err := b.NuovoLink(ctx, busta.ID, chi); err != nil || n.Token != gettoneFinto || tokenSalvato(t, b, busta.ID) != gettoneFinto {
		t.Errorf("dopo i guasti: %v", err)
	}
}

// inventario: RIP-S-24
// Il cambio di oreficeria revoca il link senza toccare il vecchio link con codice (what never
// stamps: «the change of counterparty»).
func TestCambioDiControparteNonRevocaIlCodice(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	conCodice(t, b, busta.ID)
	if _, err := b.Modifica(ctx, busta.ID, Modifica{OreficeriaID: p("2")}, nil, chi); err != nil {
		t.Fatal(err)
	}
	if s := ultimoAudit(t, d, "repair.public_link_revoked"); !strings.HasSuffix(s, `|ok|{"reason":"counterparty_changed"}`) {
		t.Errorf("audit %q", s)
	}
	if v := colonnaBusta(t, b, busta.ID, "codice_flask_revocato_il"); v != nil {
		t.Errorf("codice revocato dal cambio: %v", v)
	}
	if l := scheda(t, b, busta.ID).Link; l.Stato != LinkRevocato || l.Storico != StoricoVivo {
		t.Errorf("dopo il cambio: %+v", l)
	}
}
