package buste

import (
	"errors"
	"testing"
	"time"
)

// Il pannello delle azioni dopo la scansione (qr.go, slice 054): @qr_actions, qr_select/2,
// qr_confirm_text/2 di WorkshopLive e qr_advance_repair/3, qr_set_estimate/3, qr_next_status/1 e
// qr_auto_return/4 di Workshop, a go-baseline.

// bustaQR crea una busta e le mette le colonne date (stato, compro oro, artigiano…) con SQL.
func bustaQR(t *testing.T, b *Buste, colonne string, arg ...any) int64 {
	t.Helper()
	id := crea(t, b, modulo()).ID
	if colonne != "" {
		esegui(t, b.d, `UPDATE buste SET `+colonne+` WHERE id = ?`, append(arg, id)...)
	}
	return id
}

// inventario: RIP-H-23, RIP-H-25, RIP-H-26
// La tabella del pannello per ogni stato delle due famiglie: il pulsante, la conferma, cosa segue,
// «💰 Preventivo» e l'avviso delle buste che non si avanzano (con lo stato nelle parole della
// sorgente, gli spazi al posto di «_»).
func TestAzioniDelPannello(t *testing.T) {
	casi := []struct {
		oro                   bool
		stato                 string
		etichetta, conferma   string
		dopo                  DopoQR
		preventivo            bool
		avviso                string
		prossimo, errProssimo string
	}{
		{false, "in_lavorazione", "✅ Pronta", "", DopoPrezzo, true, "", "pronta", ""},
		{false, "in_attesa_materiali", "✅ Pronta", "", DopoPrezzo, true, "", "pronta", ""},
		{false, "preventivo", "▶ In lavorazione", "", DopoLista, false, "", "in_lavorazione", ""},
		{false, "pronta", "✅ Consegnata", "Marchio come consegnata? Verrà impostata data oggi.", DopoLista, false, "", "consegnata", ""},
		{false, "consegnata", "", "", "", false, "Stato consegnata — non avanzabile.", "", `Stato "consegnata" non avanzabile`},
		{false, "annullata", "", "", "", false, "Stato annullata — non avanzabile.", "", `Stato "annullata" non avanzabile`},
		{true, "in_valutazione", "✅ Valutata", `Conferma valutazione completata? Verrà aperto WA "scheda pronta firma".`, DopoAvviso, false, "", "valutata", ""},
		{true, "valutata", "✅ Perfezionata", "Confermi firma scheda? Movimento di cassa registrato.", DopoLista, false, "", "perfezionata", ""},
		{true, "perfezionata", "✅ Chiusa", "", DopoLista, false, "", "chiusa", ""},
		{true, "chiusa", "", "", "", false, "Stato chiusa — non avanzabile.", "", `Stato "chiusa" non avanzabile`},
		{true, "annullata", "", "", "", false, "Stato annullata — non avanzabile.", "", `Stato "annullata" non avanzabile`},
		// Uno stato che la tabella non conosce: «Stato non avanzabile.».
		{true, "in_lavorazione", "", "", "", false, "Stato non avanzabile.", "", `Stato CO "in lavorazione" non gestito`},
	}
	for _, c := range casi {
		r := RigaQR{Stato: c.stato, CompraOro: c.oro}
		a, ok := r.Azione()
		if ok != (c.etichetta != "") || a.Etichetta != c.etichetta || a.Conferma != c.conferma || a.Dopo != c.dopo {
			t.Errorf("%v %s: %+v %v", c.oro, c.stato, a, ok)
		}
		if r.ConPreventivo() != c.preventivo {
			t.Errorf("%v %s: preventivo %v", c.oro, c.stato, !c.preventivo)
		}
		if got := r.AvvisoDelPannello(); got != c.avviso {
			t.Errorf("%v %s: avviso %q", c.oro, c.stato, got)
		}
		p, err := prossimoDaQR(c.stato, c.oro)
		if p != c.prossimo || (err == nil) != (c.errProssimo == "") || (err != nil && err.Error() != c.errProssimo) {
			t.Errorf("%v %s: prossimo %q %v", c.oro, c.stato, p, err)
		}
		// Fuori bottega: la frase del rientro solo verso pronta o consegnata, dopo quella della riga.
		r.FuoriBottega = true
		a, _ = r.Azione()
		rientro := c.prossimo == "pronta" || c.prossimo == "consegnata"
		if r.RientroAutomatico() != rientro {
			t.Errorf("%v %s: rientro %v", c.oro, c.stato, !rientro)
		}
		attesa := c.conferma
		if rientro {
			attesa = c.conferma + " " + confermaRientro
			if c.conferma == "" {
				attesa = confermaRientro
			}
		}
		if a.Conferma != attesa {
			t.Errorf("%v %s fuori bottega: conferma %q", c.oro, c.stato, a.Conferma)
		}
	}
	if confermaRientro != "La busta risulta fuori bottega: viene segnata anche rientrata dall'artigiano." {
		t.Error("frase del rientro")
	}
	if (RigaQR{Stato: "pronta", FuoriBottega: true}).Azione(); azioniQR[chiaveAzioneQR{false, "pronta"}].Conferma != "Marchio come consegnata? Verrà impostata data oggi." {
		t.Error("la frase del rientro cambia la tabella")
	}
}

// inventario: RIP-H-23
// L'avanzamento dal QR: il prossimo stato dalla riga riletta, date e conservazione come la scheda,
// audit repair.status_changed con «via»: «qr»; la catena di una riparazione e di un compro oro fino
// alla chiusura, poi il rifiuto; una busta nel cestino o che non c'è.
func TestAvanzaDaQR(t *testing.T) {
	b, d, o := prova(t)
	id := bustaQR(t, b, "")
	riga := func(id int64) string {
		return valore(t, d, `SELECT stato || '|' || coalesce(data_pronta, '-') || '|' || coalesce(data_consegna, '-') || '|' ||
			coalesce(conservazione_iniziata_il, '-') FROM buste WHERE id = ?`, id).(string)
	}
	c, err := b.AvanzaDaQR(ctx, id, "in_lavorazione", chi)
	if err != nil || c.Prima != "in_lavorazione" || c.Dopo != "pronta" || c.Versione != 1 || c.Rientrata {
		t.Fatalf("pronta: %+v %v", c, err)
	}
	if got := riga(id); got != "pronta|2026-10-31|-|-" {
		t.Errorf("dopo pronta: %s", got)
	}
	if s := ultimoAudit(t, d, "repair.status_changed"); s != "repair.status_changed|repair|"+itoa(id)+`|ok|{"status":"pronta","via":"qr"}` {
		t.Errorf("audit %q", s)
	}
	o.Sposta(time.Hour)
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if c, err = b.AvanzaDaQR(ctx, id, "pronta", chi); err != nil || c.Dopo != "consegnata" {
		t.Fatalf("consegnata: %+v %v", c, err)
	}
	if got := riga(id); got != "consegnata|2026-10-31|2026-10-31|"+ora {
		t.Errorf("dopo consegnata: %s", got)
	}
	var chiusa ErrNonAvanzabile
	if _, err = b.AvanzaDaQR(ctx, id, "consegnata", chi); !errors.As(err, &chiusa) || err.Error() != `Stato "consegnata" non avanzabile` {
		t.Errorf("consegnata: %v", err)
	}
	// preventivo → in lavorazione.
	prev := bustaQR(t, b, `stato = 'preventivo'`)
	if c, err = b.AvanzaDaQR(ctx, prev, "preventivo", chi); err != nil || c.Dopo != "in_lavorazione" {
		t.Errorf("preventivo: %+v %v", c, err)
	}
	// Il compro oro fino a chiusa.
	oro := bustaQR(t, b, `compro_oro = 1, stato = 'in_valutazione'`)
	for _, passo := range [][2]string{{"in_valutazione", "valutata"}, {"valutata", "perfezionata"}, {"perfezionata", "chiusa"}} {
		if c, err = b.AvanzaDaQR(ctx, oro, passo[0], chi); err != nil || c.Prima != passo[0] || c.Dopo != passo[1] {
			t.Errorf("compro oro %s: %+v %v", passo[0], c, err)
		}
	}
	if got := riga(oro); got != "chiusa|-|-|"+ora {
		t.Errorf("compro oro chiuso: %s", got)
	}
	if _, err = b.AvanzaDaQR(ctx, oro, "chiusa", chi); !errors.As(err, &chiusa) {
		t.Errorf("chiusa: %v", err)
	}
	// Nel cestino o mai esistita.
	cestino := bustaQR(t, b, `cancellata_il = ?`, istanteProva)
	for _, x := range []int64{cestino, 999} {
		prima := scatta(t, d)
		if _, err := b.AvanzaDaQR(ctx, x, "in_lavorazione", chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("busta %d: %v", x, err)
		}
		uguali(t, "busta assente", prima, scatta(t, d))
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed' AND dati LIKE '%"via":"qr"%'`); n != 6 {
		t.Errorf("righe d'audit col via: %d", n)
	}
}

// inventario: RIP-H-23
// La busta deve avere ancora lo stato che il pannello mostrava (:expected_status): cambiata nel
// frattempo dà ErrStatoCambiato con lo stato di adesso e non scrive niente; chiusa nel frattempo dà
// prima il rifiuto della busta chiusa. Uno stato del compro oro senza seguito è ErrOroNonGestito.
func TestAvanzaDaQRBustaCambiata(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaQR(t, b, `stato = 'pronta'`)
	prima := scatta(t, d)
	var cambiata ErrStatoCambiato
	// «in_lavorazione» e «in_attesa_materiali» porterebbero proprio a pronta: è il secondo invio
	// (TestAvanzaDaQRSecondoInvio).
	for _, visto := range []string{"preventivo", "", "consegnata", "valutata", "x"} {
		if _, err := b.AvanzaDaQR(ctx, id, visto, chi); !errors.As(err, &cambiata) || cambiata.Attuale != "pronta" ||
			err.Error() != "Stato cambiato nel frattempo: ricontrolla." {
			t.Errorf("visto %q: %v", visto, err)
		}
	}
	uguali(t, "busta cambiata", prima, scatta(t, d))
	esegui(t, d, `UPDATE buste SET stato = 'annullata' WHERE id = ?`, id)
	var chiusa ErrNonAvanzabile
	if _, err := b.AvanzaDaQR(ctx, id, "pronta", chi); !errors.As(err, &chiusa) || chiusa.Stato != "annullata" {
		t.Errorf("chiusa nel frattempo: %v", err)
	}
	// Un compro oro che il CHECK lascia in uno stato senza seguito non c'è; la regola sta in
	// prossimoDaQR (TestAzioniDelPannello). Il grafo vale comunque: valutata → perfezionata sì,
	// un passo fuori grafo no.
	var oro ErrOroNonGestito
	if _, err := prossimoDaQR("x", true); !errors.As(err, &oro) || err.Error() != `Stato CO "x" non gestito` {
		t.Errorf("compro oro: %v", err)
	}
}

// inventario: RIP-H-25
// «💰 Preventivo»: la busta va in preventivo con l'audit e «via»: «qr»; già in preventivo resta,
// senza audit; un compro oro è rifiutato; da pronta il grafo dice «Transizione non consentita.».
func TestPreventivoDaQR(t *testing.T) {
	b, d, _ := prova(t)
	for _, stato := range []string{"in_lavorazione", "in_attesa_materiali"} {
		id := bustaQR(t, b, `stato = ?`, stato)
		c, err := b.PreventivoDaQR(ctx, id, chi)
		if err != nil || c.Prima != stato || c.Dopo != "preventivo" {
			t.Errorf("%s: %+v %v", stato, c, err)
		}
		if s := ultimoAudit(t, d, "repair.status_changed"); s != "repair.status_changed|repair|"+itoa(id)+`|ok|{"status":"preventivo","via":"qr"}` {
			t.Errorf("audit %q", s)
		}
	}
	gia := bustaQR(t, b, `stato = 'preventivo'`)
	prima := scatta(t, d)
	if c, err := b.PreventivoDaQR(ctx, gia, chi); err != nil || c.Dopo != "preventivo" {
		t.Errorf("già in preventivo: %+v %v", c, err)
	}
	uguali(t, "già in preventivo", prima, scatta(t, d))
	oro := bustaQR(t, b, `compro_oro = 1, stato = 'in_valutazione'`)
	if _, err := b.PreventivoDaQR(ctx, oro, chi); !errors.Is(err, ErrPreventivoOro) || err.Error() != "QR preventivo non supportato per buste compro oro" {
		t.Errorf("compro oro: %v", err)
	}
	for _, stato := range []string{"pronta", "consegnata"} {
		id := bustaQR(t, b, `stato = ?`, stato)
		prima := scatta(t, d)
		if _, err := b.PreventivoDaQR(ctx, id, chi); !errors.Is(err, ErrTransizione) {
			t.Errorf("%s: %v", stato, err)
		}
		uguali(t, stato, prima, scatta(t, d))
	}
	if _, err := b.PreventivoDaQR(ctx, 999, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("assente: %v", err)
	}
}

// inventario: RIP-H-24
// Il rientro automatico (sui dati importati: le consegne all'artigiano vengono dallo standalone):
// una busta ancora dall'artigiano che dal QR diventa pronta (o consegnata) rientra nella stessa
// transazione, con il solo istante (né esito né note) e la riga repair.returned_from_artisan_auto;
// la consegna si chiude quando era l'ultima fuori, una volta sola; una busta già rientrata, una in
// bottega, un passo che non porta a pronta o consegnata e il preventivo non toccano niente.
func TestRientroAutomaticoDaQR(t *testing.T) {
	b, d, o := prova(t)
	esegui(t, d, `INSERT INTO consegne_artigiani (id, artigiano_id, note, creato_il, aggiornato_il) VALUES (7, 1, 'Consegna di prova', ?1, ?1)`, istanteProva)
	prima := bustaQR(t, b, `artigiano_id = 1, consegna_artigiano_id = 7, consegnata_artigiano_il = ?, note_artigiano = 'Nota del ciclo'`, istanteProva)
	seconda := bustaQR(t, b, `artigiano_id = 1, consegna_artigiano_id = 7, consegnata_artigiano_il = ?, stato = 'in_attesa_materiali'`, istanteProva)
	o.Sposta(time.Hour)
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	riga := func(id int64) string {
		return valore(t, d, `SELECT stato || '|' || coalesce(rientrata_artigiano_il, '-') || '|' || coalesce(note_artigiano, '-') || '|' ||
			aggiornato_il FROM buste WHERE id = ?`, id).(string)
	}
	consegna := func() string {
		return valore(t, d, `SELECT coalesce(chiusa_il, '-') || '|' || note FROM consegne_artigiani WHERE id = 7`).(string)
	}
	if r, err := b.RigaPerQR(ctx, prima); err != nil || !r.FuoriBottega || !r.RientroAutomatico() {
		t.Fatalf("riga del pannello: %+v %v", r, err)
	}
	c, err := b.AvanzaDaQR(ctx, prima, "in_lavorazione", chi)
	if err != nil || c.Dopo != "pronta" || !c.Rientrata {
		t.Fatalf("prima: %+v %v", c, err)
	}
	if got := riga(prima); got != "pronta|"+ora+"|Nota del ciclo|"+ora {
		t.Errorf("prima rientrata: %s", got)
	}
	if got := consegna(); got != "-|Consegna di prova" {
		t.Errorf("la consegna resta aperta con la seconda fuori: %s", got)
	}
	if s := ultimoAudit(t, d, "repair.returned_from_artisan_auto"); s != "repair.returned_from_artisan_auto|repair|"+itoa(prima)+
		`|ok|{"bag_number":"`+valore(t, d, `SELECT numero FROM buste WHERE id = ?`, prima).(string)+`","handover_closed":false,"handover_id":7,"via":"qr"}` {
		t.Errorf("audit %q", s)
	}
	// La riga del rientro viene dopo quella dello stato.
	if v := valore(t, d, `SELECT group_concat(azione, ',') FROM (SELECT azione FROM eventi_audit WHERE risorsa = 'repair' AND id_risorsa = ? ORDER BY id)`, itoa(prima)); v != "repair.created,repair.status_changed,repair.returned_from_artisan_auto" {
		t.Errorf("ordine delle righe: %v", v)
	}
	// La seconda, ultima fuori: la consegna si chiude.
	if c, err = b.AvanzaDaQR(ctx, seconda, "in_attesa_materiali", chi); err != nil || !c.Rientrata {
		t.Fatalf("seconda: %+v %v", c, err)
	}
	if got := consegna(); got != ora+"|Consegna di prova" {
		t.Errorf("consegna chiusa: %s", got)
	}
	if s := ultimoAudit(t, d, "repair.returned_from_artisan_auto"); s != "repair.returned_from_artisan_auto|repair|"+itoa(seconda)+
		`|ok|{"bag_number":"`+valore(t, d, `SELECT numero FROM buste WHERE id = ?`, seconda).(string)+`","handover_closed":true,"handover_id":7,"via":"qr"}` {
		t.Errorf("audit %q", s)
	}
	// Pronta → consegnata: già rientrata, nessun secondo rientro.
	if c, err = b.AvanzaDaQR(ctx, prima, "pronta", chi); err != nil || c.Rientrata || riga(prima) != "consegnata|"+ora+"|Nota del ciclo|"+ora {
		t.Errorf("consegnata dopo il rientro: %+v %v %s", c, err, riga(prima))
	}
	// Fuori bottega e pronta → consegnata (anche senza consegna): rientra, handover_id nullo.
	pronta := bustaQR(t, b, `stato = 'pronta', consegnata_artigiano_il = ?`, istanteProva)
	if c, err = b.AvanzaDaQR(ctx, pronta, "pronta", chi); err != nil || !c.Rientrata {
		t.Errorf("pronta fuori bottega: %+v %v", c, err)
	}
	if s := ultimoAudit(t, d, "repair.returned_from_artisan_auto"); s != "repair.returned_from_artisan_auto|repair|"+itoa(pronta)+
		`|ok|{"bag_number":"`+valore(t, d, `SELECT numero FROM buste WHERE id = ?`, pronta).(string)+`","handover_closed":false,"handover_id":null,"via":"qr"}` {
		t.Errorf("audit senza consegna %q", s)
	}
	// Preventivo → in lavorazione e il preventivo dal QR: niente rientro.
	prev := bustaQR(t, b, `stato = 'preventivo', consegnata_artigiano_il = ?`, istanteProva)
	if c, err = b.AvanzaDaQR(ctx, prev, "preventivo", chi); err != nil || c.Rientrata || riga(prev) != "in_lavorazione|-|-|"+ora {
		t.Errorf("preventivo fuori bottega: %+v %v %s", c, err, riga(prev))
	}
	if c, err = b.PreventivoDaQR(ctx, prev, chi); err != nil || c.Rientrata || riga(prev) != "preventivo|-|-|"+ora {
		t.Errorf("💰 preventivo fuori bottega: %+v %v %s", c, err, riga(prev))
	}
	// Una busta in bottega: nessuna riga di rientro. La scheda non rientra mai (CambiaStato).
	dentro := bustaQR(t, b, "")
	scheda := bustaQR(t, b, `consegnata_artigiano_il = ?`, istanteProva)
	if c, err = b.AvanzaDaQR(ctx, dentro, "in_lavorazione", chi); err != nil || c.Rientrata {
		t.Errorf("in bottega: %+v %v", c, err)
	}
	if c, err = b.CambiaStato(ctx, scheda, "pronta", chi); err != nil || c.Rientrata || riga(scheda) != "pronta|-|-|"+ora {
		t.Errorf("dalla scheda: %+v %v %s", c, err, riga(scheda))
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.returned_from_artisan_auto'`); n != 3 {
		t.Errorf("righe di rientro: %d", n)
	}
	// Una consegna già chiusa non si richiude.
	esegui(t, d, `INSERT INTO consegne_artigiani (id, artigiano_id, chiusa_il, creato_il, aggiornato_il) VALUES (8, 1, '2026-01-01T00:00:00.000000Z', ?1, ?1)`, istanteProva)
	vecchia := bustaQR(t, b, `consegna_artigiano_id = 8, consegnata_artigiano_il = ?`, istanteProva)
	if c, err = b.AvanzaDaQR(ctx, vecchia, "in_lavorazione", chi); err != nil || !c.Rientrata {
		t.Errorf("consegna già chiusa: %+v %v", c, err)
	}
	if v := valore(t, d, `SELECT chiusa_il FROM consegne_artigiani WHERE id = 8`); v != "2026-01-01T00:00:00.000000Z" {
		t.Errorf("consegna richiusa: %v", v)
	}
	if s := ultimoAudit(t, d, "repair.returned_from_artisan_auto"); s == "" || s[len(s)-len(`"handover_closed":false,"handover_id":8,"via":"qr"}`):] != `"handover_closed":false,"handover_id":8,"via":"qr"}` {
		t.Errorf("audit consegna già chiusa %q", s)
	}
}

// inventario: RIP-H-22, RIP-H-24
// La riga del pannello porta «fuori bottega» (consegnata e non rientrata) anche dalla scansione.
func TestRigaQRFuoriBottega(t *testing.T) {
	b, _, _ := prova(t)
	fuori := bustaQR(t, b, `codice_qr = 'QR-FUORI', consegnata_artigiano_il = ?`, istanteProva)
	bustaQR(t, b, `codice_qr = 'QR-FUORI', consegnata_artigiano_il = ?1, rientrata_artigiano_il = ?1`, istanteProva)
	rr, err := b.CercaPerQR(ctx, "QR-FUORI", true, chi)
	if err != nil || len(rr) != 2 || rr[0].FuoriBottega || !rr[1].FuoriBottega || rr[1].ID != fuori {
		t.Fatalf("%+v %v", rr, err)
	}
	cestino := bustaQR(t, b, `cancellata_il = ?`, istanteProva)
	for _, x := range []int64{999, cestino} {
		if _, err := b.RigaPerQR(ctx, x); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("busta %d: %v", x, err)
		}
	}
	if r, err := b.RigaPerQR(ctx, fuori); err != nil || r.ID != fuori || !r.FuoriBottega || r.Stato != "in_lavorazione" {
		t.Errorf("riga: %+v %v", r, err)
	}
}

// inventario: RIP-H-23, RIP-H-24
// Il secondo invio dello stesso modulo (doppio tocco, review della 054, M1): la busta è già nello
// stato che l'azione dello stato mostrato avrebbe prodotto, quindi è riuscita senza scrivere niente
// (né stato, né audit, né un secondo rientro). Uno stato mostrato che non è della famiglia no.
func TestAvanzaDaQRSecondoInvio(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO consegne_artigiani (id, artigiano_id, creato_il, aggiornato_il) VALUES (7, 1, ?1, ?1)`, istanteProva)
	casi := []struct {
		colonne, visto, dopo string
		oro                  bool
	}{
		{`consegna_artigiano_id = 7, consegnata_artigiano_il = ?`, "in_lavorazione", "pronta", false},
		{``, "pronta", "consegnata", false},
		{``, "preventivo", "in_lavorazione", false},
		{`compro_oro = 1, stato = 'in_valutazione'`, "in_valutazione", "valutata", true},
		{`compro_oro = 1, stato = 'perfezionata'`, "perfezionata", "chiusa", true},
	}
	for _, c := range casi {
		var arg []any
		if c.colonne != "" && c.colonne[len(c.colonne)-1] == '?' {
			arg = append(arg, istanteProva)
		}
		id := bustaQR(t, b, c.colonne, arg...)
		esegui(t, d, `UPDATE buste SET stato = ? WHERE id = ?`, c.visto, id)
		if _, err := b.AvanzaDaQR(ctx, id, c.visto, chi); err != nil {
			t.Fatalf("%s primo: %v", c.visto, err)
		}
		prima := scatta(t, d)
		riga := valore(t, d, `SELECT stato || coalesce(rientrata_artigiano_il, '-') || aggiornato_il FROM buste WHERE id = ?`, id)
		ch, err := b.AvanzaDaQR(ctx, id, c.visto, chi)
		if err != nil || ch.Prima != c.dopo || ch.Dopo != c.dopo || ch.Rientrata {
			t.Errorf("%s secondo: %+v %v", c.visto, ch, err)
		}
		uguali(t, c.visto+" secondo", prima, scatta(t, d))
		if v := valore(t, d, `SELECT stato || coalesce(rientrata_artigiano_il, '-') || aggiornato_il FROM buste WHERE id = ?`, id); v != riga {
			t.Errorf("%s: la busta è cambiata: %v", c.visto, v)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.returned_from_artisan_auto'`); n != 1 {
		t.Errorf("rientri: %d", n)
	}
	// Uno stato mostrato dell'altra famiglia non vale come secondo invio: per un compro oro valutato
	// «in_lavorazione» resta una busta cambiata.
	oro := bustaQR(t, b, `compro_oro = 1, stato = 'valutata'`)
	var cambiata ErrStatoCambiato
	if _, err := b.AvanzaDaQR(ctx, oro, "in_lavorazione", chi); !errors.As(err, &cambiata) {
		t.Errorf("stato dell'altra famiglia: %v", err)
	}
	if a, ok := AzioneDiStato("in_lavorazione", false); !ok || a.Dopo != DopoPrezzo || a.Conferma != "" {
		t.Errorf("azione dello stato: %+v %v", a, ok)
	}
}
