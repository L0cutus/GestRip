package buste

import (
	"errors"
	"testing"
)

// Lo spegnimento delle iscrizioni Telegram è nel database, come i trigger
// telegram_subscriptions_off_when_closed, _trashed e _b2b dello standalone (20261012150000):
// qualunque scrittura chiuda la busta, la metta nel cestino o la passi a un'oreficeria spegne le
// iscrizioni ancora attive, toglie i token del link t.me e scrive una riga d'audit per
// iscrizione (attore «system», mai la chat).

func iscrizione(t *testing.T, b *Buste, busta int64, chat int64, fermata any) {
	t.Helper()
	esegui(t, b.d, `INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, consenso_il, fermata_il, creato_il, aggiornato_il)
		VALUES (?2, ?3, 1, ?1, ?1, ?4, ?1, ?1)`, istanteProva, busta, chat, fermata)
}

func tokenTelegram(t *testing.T, b *Buste, busta int64, h string) {
	t.Helper()
	esegui(t, b.d, `INSERT INTO token_telegram (busta_id, hash_token, scade_il, creato_il) VALUES (?, ?, ?, ?)`, busta, []byte(h), istanteProva, istanteProva)
}

func attive(t *testing.T, b *Buste, busta int64) int {
	t.Helper()
	return conta(t, b.d, `SELECT count(*) FROM iscrizioni_telegram WHERE busta_id = ? AND fermata_il IS NULL AND consenso = 1`, busta)
}

// inventario: RIP-S-13
// Una busta consegnata con CambiaStato spegne le sue iscrizioni attive (consenso 0, fermata, motivo
// lo stato), toglie i token e scrive repair.telegram_switched_off per ognuna; un'iscrizione già
// fermata e le iscrizioni di un'altra busta restano come sono; uno stato aperto non spegne niente.
func TestChiusuraSpegneLeIscrizioniTelegram(t *testing.T) {
	b, d, _ := prova(t)
	busta, altra := crea(t, b, modulo()), crea(t, b, modulo())
	iscrizione(t, b, busta.ID, 101, nil)
	iscrizione(t, b, busta.ID, 102, nil)
	iscrizione(t, b, busta.ID, 103, istanteProva)
	iscrizione(t, b, altra.ID, 104, nil)
	tokenTelegram(t, b, busta.ID, "token-uno")
	tokenTelegram(t, b, altra.ID, "token-due")
	if _, err := b.CambiaStato(ctx, busta.ID, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	if attive(t, b, busta.ID) != 2 || conta(t, d, `SELECT count(*) FROM token_telegram`) != 2 {
		t.Fatal("uno stato aperto ha spento le iscrizioni")
	}
	if _, err := b.CambiaStato(ctx, busta.ID, "consegnata", chi); err != nil {
		t.Fatal(err)
	}
	if attive(t, b, busta.ID) != 0 {
		t.Error("iscrizioni ancora attive")
	}
	if v := valore(t, d, `SELECT group_concat(chat_id || ':' || consenso || ':' || coalesce(motivo_fermata, '-') || ':' ||
		coalesce(fermata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z', 'n') || ':' ||
		coalesce(aggiornato_il = fermata_il, 'n'), ',') FROM (SELECT * FROM iscrizioni_telegram ORDER BY id)`); v != "101:0:consegnata:1:1,102:0:consegnata:1:1,103:1:-:1:1,104:1:-:n:n" {
		t.Errorf("iscrizioni %v", v)
	}
	if v := valore(t, d, `SELECT group_concat(busta_id) FROM token_telegram`); v != itoa(altra.ID) {
		t.Errorf("token rimasti: %v", v)
	}
	if v := valore(t, d, `SELECT group_concat(attore || '|' || risorsa || '|' || id_risorsa || '|' || esito || '|' || dati || '|' || coalesce(ip, '-'), ',')
		FROM (SELECT * FROM eventi_audit WHERE azione = 'repair.telegram_switched_off' ORDER BY id)`); v !=
		`system|repair|`+itoa(busta.ID)+`|ok|{"bag_number":"1/26","subscription_id":1,"reason":"consegnata"}|-,`+
			`system|repair|`+itoa(busta.ID)+`|ok|{"bag_number":"1/26","subscription_id":2,"reason":"consegnata"}|-` {
		t.Errorf("audit %v", v)
	}
}

// inventario: RIP-S-13
// Lo spegnimento sta nella transazione del cambio di stato: se il cambio non si salva (qui
// l'audit repair.status_changed rifiutato da un trigger di prova), le iscrizioni restano attive
// e nessuna riga d'audit resta.
func TestSpegnimentoNellaTransazioneDelCambio(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	iscrizione(t, b, busta.ID, 101, nil)
	tokenTelegram(t, b, busta.ID, "token-uno")
	esegui(t, d, `CREATE TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.status_changed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.CambiaStato(ctx, busta.ID, "annullata", chi); err == nil || errors.Is(err, ErrTransizione) {
		t.Fatalf("cambio con l'audit rifiutato: %v", err)
	}
	if attive(t, b, busta.ID) != 1 || conta(t, d, `SELECT count(*) FROM token_telegram`) != 1 ||
		conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_switched_off'`) != 0 ||
		valore(t, d, `SELECT stato FROM buste WHERE id = ?`, busta.ID) != "in_lavorazione" {
		t.Error("il cambio rifiutato ha lasciato qualcosa")
	}
}

// inventario: RIP-S-13
// Anche il cestino (motivo «deleted»), il passaggio a un'oreficeria esterna (motivo
// «oreficeria») e la chiusura di una scheda compro oro spengono le iscrizioni. La diretta (NULL)
// e il negozio (1) no.
func TestAltriSpegnimentiTelegram(t *testing.T) {
	b, d, _ := prova(t)
	cestino, oref, oro := crea(t, b, modulo()), crea(t, b, modulo()), crea(t, b, con(modulo(), func(a *Accettazione) { a.CompraOro = "1" }))
	for i, id := range []int64{cestino.ID, oref.ID, oro.ID} {
		iscrizione(t, b, id, int64(200+i), nil)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 1 WHERE id = ?`, oref.ID)
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`, oref.ID)
	if attive(t, b, oref.ID) != 1 {
		t.Fatal("spenta passando alla diretta o al negozio")
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, oref.ID)
	for _, s := range []string{"valutata", "perfezionata", "chiusa"} {
		if _, err := b.CambiaStato(ctx, oro.ID, s, chi); err != nil {
			t.Fatal(err)
		}
	}
	if v := valore(t, d, `SELECT group_concat(motivo_fermata, ',') FROM (SELECT * FROM iscrizioni_telegram ORDER BY id)`); v != "deleted,oreficeria,chiusa" {
		t.Errorf("motivi %v", v)
	}
}

// inventario: RIP-S-13
// Una busta scritta con INSERT (come fa l'import) non fa scattare niente, anche se è già chiusa,
// nel cestino o di un'oreficeria.
func TestInsertNonSpegneTelegram(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	iscrizione(t, b, busta.ID, 300, nil)
	esegui(t, d, `INSERT INTO buste (cliente_id, nome_cliente, telefono_cliente, numero, data_accettazione, descrizione, stato,
		oreficeria_id, hash_token_pubblico, token_pubblico_scade_il, cancellata_il, creato_il, aggiornato_il)
		SELECT cliente_id, nome_cliente, telefono_cliente, '99/26', data_accettazione, descrizione, 'consegnata', 2, x'00', ?1, ?1, ?1, ?1
		FROM buste WHERE id = ?2`, istanteProva, busta.ID)
	if attive(t, b, busta.ID) != 1 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_switched_off'`) != 0 {
		t.Error("un INSERT ha spento le iscrizioni")
	}
}
