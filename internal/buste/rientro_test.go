package buste

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Il rientro di una busta dall'artigiano dalla scheda (slice 080, ART-13, RIP-S-16), come
// Workshop.return_from_artisan/4 e applicable_outcomes/1 dello standalone (302439d): i casi di
// handovers_test.exs («returning a bag from the artisan»), più l'email di pronta nella stessa transazione
// (solo Go, sezione 070b di SVILUPPO). L'orologio dei test è fermo al 31/10/2026, le 10:00 in negozio.

// fuori crea una busta uscita nella consegna data (0: nessuna) con le colonne in più; dà l'id.
func fuori(t *testing.T, b *Buste, consegna int64, colonne string, arg ...any) int64 {
	t.Helper()
	q := `consegnata_artigiano_il = '2026-10-20T08:00:00.000000Z', artigiano_id = 1`
	if consegna != 0 {
		q += `, consegna_artigiano_id = ` + itoa(consegna)
	}
	if colonne != "" {
		q += ", " + colonne
	}
	return bustaQR(t, b, q, arg...)
}

// rientroDi è «stato|pronta|consegna|uscita|rientro|note|aggiornato» della busta (NULL come «-»).
func rientroDi(t *testing.T, b *Buste, id int64) string {
	t.Helper()
	return valore(t, b.d, `SELECT stato || '|' || coalesce(data_pronta, '-') || '|' || coalesce(consegna_artigiano_id, '-') || '|' ||
		coalesce(consegnata_artigiano_il, '-') || '|' || coalesce(rientrata_artigiano_il, '-') || '|' || coalesce(note_artigiano, '-') || '|' ||
		aggiornato_il FROM buste WHERE id = ?`, id).(string)
}

func chiusaIl(t *testing.T, b *Buste, consegna int64) string {
	t.Helper()
	v := valore(t, b.d, `SELECT coalesce(chiusa_il, '-') FROM consegne_artigiani WHERE id = ?`, consegna)
	return v.(string)
}

const uscitaProva = "2026-10-20T08:00:00.000000Z"

// inventario: ART-13, RIP-S-16
// Il rientro timbra l'istante (quello dell'orologio, anche in aggiornato_il), lascia uscita e consegna
// com'erano e scrive una riga repair.returned_from_artisan con la consegna, la chiusura, l'esito, lo stato e
// se c'è una nota, mai la nota; nessuna repair.status_changed. La consegna resta aperta finché una sua busta
// è fuori e si chiude con l'ultima, con lo stesso istante; dopo il rientro la busta non è più fuori bottega.
func TestRientroDallArtigiano(t *testing.T) {
	b, d, o := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	prima := fuori(t, b, consegna, "")
	seconda := fuori(t, b, consegna, "")
	o.Sposta(time.Hour)
	ora := o.Adesso().UTC().Format("2006-01-02T15:04:05.000000Z")
	r, err := b.RientraDallArtigiano(ctx, prima, "", "", chi)
	if err != nil {
		t.Fatal(err)
	}
	if r.Prima != "in_lavorazione" || r.Dopo != "in_lavorazione" || r.Consegna == nil || *r.Consegna != consegna || r.ConsegnaChiusa {
		t.Errorf("rientro: %+v", r)
	}
	if got := rientroDi(t, b, prima); got != "in_lavorazione|-|"+itoa(consegna)+"|"+uscitaProva+"|"+ora+"|-|"+ora {
		t.Errorf("busta: %s", got)
	}
	if got := chiusaIl(t, b, consegna); got != "-" {
		t.Errorf("consegna chiusa con una busta ancora fuori: %s", got)
	}
	if got := ultimoAudit(t, d, "repair.returned_from_artisan"); got != "repair.returned_from_artisan|repair|"+itoa(prima)+
		`|ok|{"artisan_notes":false,"handover_closed":false,"handover_id":`+itoa(consegna)+`,"outcome":"","status":"in_lavorazione"}` {
		t.Errorf("audit: %s", got)
	}
	if v := valore(t, d, `SELECT attore || '|' || ip FROM eventi_audit WHERE azione = 'repair.returned_from_artisan'`); v != chi.Attore+"|"+chi.IP {
		t.Errorf("chi: %v", v)
	}
	if s, err := b.Scheda(ctx, prima); err != nil || s.FuoriBottega {
		t.Errorf("ancora fuori bottega: %+v %v", s.FuoriBottega, err)
	}
	if r, err = b.RientraDallArtigiano(ctx, seconda, "", "", chi); err != nil || !r.ConsegnaChiusa {
		t.Fatalf("seconda: %+v %v", r, err)
	}
	if got := chiusaIl(t, b, consegna); got != ora {
		t.Errorf("consegna non chiusa con l'ultima: %s", got)
	}
	if conta(t, d, `SELECT count(*) FROM consegne_artigiani WHERE id = ? AND aggiornato_il = ?`, consegna, ora) != 1 {
		t.Error("aggiornato_il della consegna")
	}
	if !strings.Contains(ultimoAudit(t, d, "repair.returned_from_artisan"), `"handover_closed":true`) {
		t.Errorf("audit della chiusura: %s", ultimoAudit(t, d, "repair.returned_from_artisan"))
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.status_changed', 'repair.returned_from_artisan_auto')`); n != 0 {
		t.Errorf("righe in più: %d", n)
	}
}

// inventario: ART-13
// Gli esiti: «fatto» porta a «pronta» e scrive la data di pronta (oggi in negozio) solo se vuota; su una busta
// già pronta non è un passaggio e scrive solo la data vuota; «da_rifare» riporta in lavorazione senza togliere
// la data di pronta, e non cambia niente su una busta già in lavorazione; l'esito vuoto lascia stato e data.
// Il contatore dei cambi di stato conta solo i cambi veri.
func TestRientroEsiti(t *testing.T) {
	b, d, _ := prova(t)
	casi := []struct {
		colonne, esito, atteso string
		versione               int
	}{
		{``, "fatto", "pronta|2026-10-31", 1},
		{`data_pronta = '2026-01-01'`, "fatto", "pronta|2026-01-01", 1},
		{`stato = 'pronta'`, "fatto", "pronta|2026-10-31", 0},
		{`stato = 'in_attesa_materiali'`, "fatto", "pronta|2026-10-31", 1},
		{`stato = 'pronta', data_pronta = '2026-01-01'`, "da_rifare", "in_lavorazione|2026-01-01", 1},
		{``, "da_rifare", "in_lavorazione|-", 0},
		{`stato = 'in_attesa_materiali'`, "da_rifare", "in_lavorazione|-", 1},
		{`stato = 'preventivo'`, "", "preventivo|-", 0},
		{`stato = 'preventivo'`, "  ", "preventivo|-", 0},
		{`stato = 'preventivo'`, " da_rifare ", "in_lavorazione|-", 1},
	}
	for _, c := range casi {
		id := fuori(t, b, 0, c.colonne)
		v0 := conta(t, d, `SELECT coalesce((SELECT versione FROM versioni_stato_buste WHERE busta_id = ?), 0)`, id)
		r, err := b.RientraDallArtigiano(ctx, id, c.esito, "", chi)
		if err != nil {
			t.Fatalf("%s %q: %v", c.colonne, c.esito, err)
		}
		got := valore(t, d, `SELECT stato || '|' || coalesce(data_pronta, '-') FROM buste WHERE id = ?`, id)
		if got != c.atteso || r.Dopo != strings.Split(c.atteso, "|")[0] {
			t.Errorf("%s %q: %v %+v", c.colonne, c.esito, got, r)
		}
		if v := conta(t, d, `SELECT coalesce((SELECT versione FROM versioni_stato_buste WHERE busta_id = ?), 0)`, id); v-v0 != c.versione {
			t.Errorf("%s %q: versioni %d", c.colonne, c.esito, v-v0)
		}
		esito := strings.TrimSpace(c.esito)
		if a := ultimoAudit(t, d, "repair.returned_from_artisan"); !strings.Contains(a, `"outcome":"`+esito+`","status":"`+r.Dopo+`"`) {
			t.Errorf("%s %q: audit %s", c.colonne, c.esito, a)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`); n != 0 {
		t.Errorf("repair.status_changed: %d", n)
	}
}

// inventario: ART-13
// I rifiuti non scrivono niente: un esito che il campo non può dare («Esito lavoro non valido.», prima di
// ogni altra domanda), una busta mai uscita o già rientrata («La riparazione non risulta fuori bottega.»),
// una busta che non c'è o nel cestino (non trovata), un esito che il grafo della famiglia non permette («Da
// «preventivo» non si passa a «pronta»: scegli un altro esito lavoro.»: tutto il rientro, nota compresa).
func TestRientroRifiuti(t *testing.T) {
	b, d, _ := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	casa := bustaQR(t, b, `consegna_artigiano_id = ?`, consegna) // aggiunta, mai confermata
	tornata := fuori(t, b, consegna, `rientrata_artigiano_il = ?`, istanteProva)
	cestino := fuori(t, b, consegna, `cancellata_il = ?`, istanteProva)
	preventivo := fuori(t, b, consegna, `stato = 'preventivo', note_artigiano = 'vecchia'`)
	consegnata := fuori(t, b, consegna, `stato = 'consegnata'`)
	oro := fuori(t, b, consegna, `compro_oro = 1, stato = 'in_valutazione'`)
	foto := scatta(t, d)
	righe := func() string {
		return valore(t, d, `SELECT group_concat(x, ',') FROM (SELECT id || stato || coalesce(rientrata_artigiano_il, '-') || coalesce(note_artigiano, '-') ||
			aggiornato_il AS x FROM buste ORDER BY id)`).(string) + chiusaIl(t, b, consegna)
	}
	prima := righe()
	var vietato *EsitoNonConsentito
	casi := []struct {
		id         int64
		esito      string
		errore     error
		messaggio  string
		nonTrovata bool
	}{
		{preventivo, "consegnata", ErrEsitoNonValido, "Esito lavoro non valido.", false},
		{999, "boh", ErrEsitoNonValido, "Esito lavoro non valido.", false},
		{casa, "fatto", ErrNonFuoriBottega, "La riparazione non risulta fuori bottega.", false},
		{tornata, "", ErrNonFuoriBottega, "La riparazione non risulta fuori bottega.", false},
		{999, "fatto", ErrBustaNonTrovata, "", true},
		{cestino, "fatto", ErrBustaNonTrovata, "", true},
		{preventivo, "fatto", nil, "Da «preventivo» non si passa a «pronta»: scegli un altro esito lavoro.", false},
		{consegnata, "da_rifare", nil, "Da «consegnata» non si passa a «in lavorazione»: scegli un altro esito lavoro.", false},
		{oro, "fatto", nil, "Da «in valutazione» non si passa a «pronta»: scegli un altro esito lavoro.", false},
	}
	for _, c := range casi {
		_, err := b.RientraDallArtigiano(ctx, c.id, c.esito, "nota nuova", chi)
		switch {
		case c.errore != nil && !errors.Is(err, c.errore):
			t.Errorf("%d %q: %v", c.id, c.esito, err)
		case c.errore == nil && !errors.As(err, &vietato):
			t.Errorf("%d %q: %v", c.id, c.esito, err)
		case !c.nonTrovata && (err == nil || err.Error() != c.messaggio):
			t.Errorf("%d %q: messaggio %v", c.id, c.esito, err)
		}
	}
	uguali(t, "rifiuti", foto, scatta(t, d))
	if got := righe(); got != prima {
		t.Errorf("buste cambiate:\n%s\n%s", prima, got)
	}
	// La compro oro rientra con l'esito vuoto; la busta del preventivo con «da_rifare».
	if r, err := b.RientraDallArtigiano(ctx, oro, "", "", chi); err != nil || r.Dopo != "in_valutazione" {
		t.Errorf("compro oro: %+v %v", r, err)
	}
	if r, err := b.RientraDallArtigiano(ctx, preventivo, "da_rifare", "", chi); err != nil || r.Dopo != "in_lavorazione" {
		t.Errorf("preventivo: %+v %v", r, err)
	}
}

// inventario: ART-13
// La nota dell'artigiano senza spazi ai bordi; una vuota (o di soli spazi) cancella quella del giro. Il
// registro dice solo se c'era.
func TestRientroNota(t *testing.T) {
	b, d, _ := prova(t)
	con := fuori(t, b, 0, `note_artigiano = 'vecchia'`)
	if _, err := b.RientraDallArtigiano(ctx, con, "", "  Sostituita la molla \n ", chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT note_artigiano FROM buste WHERE id = ?`, con); v != "Sostituita la molla" {
		t.Errorf("nota: %q", v)
	}
	if a := ultimoAudit(t, d, "repair.returned_from_artisan"); !strings.Contains(a, `"artisan_notes":true`) || strings.Contains(a, "molla") {
		t.Errorf("audit: %s", a)
	}
	vuota := fuori(t, b, 0, `note_artigiano = 'vecchia'`)
	if _, err := b.RientraDallArtigiano(ctx, vuota, "", " \t ", chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT note_artigiano FROM buste WHERE id = ?`, vuota); v != nil {
		t.Errorf("nota vuota: %v", v)
	}
	if a := ultimoAudit(t, d, "repair.returned_from_artisan"); !strings.Contains(a, `"artisan_notes":false`) {
		t.Errorf("audit: %s", a)
	}
}

// inventario: ART-13
// La chiusura della consegna: una busta importata uscita senza consegna rientra senza chiudere niente
// (handover_id nullo); una consegna già chiusa tiene la sua prima chiusura; una busta aggiunta ma mai
// confermata, una già rientrata e una nel cestino non la tengono aperta (il cestino come Repo.prepare_query/3
// dello standalone, anche per il rientro automatico dal QR).
func TestRientroChiusuraConsegna(t *testing.T) {
	b, d, _ := prova(t)
	senza := fuori(t, b, 0, "")
	if r, err := b.RientraDallArtigiano(ctx, senza, "fatto", "", chi); err != nil || r.Consegna != nil || r.ConsegnaChiusa {
		t.Fatalf("senza consegna: %+v %v", r, err)
	}
	if a := ultimoAudit(t, d, "repair.returned_from_artisan"); !strings.HasSuffix(a, `{"artisan_notes":false,"handover_closed":false,"handover_id":null,"outcome":"fatto","status":"pronta"}`) {
		t.Errorf("audit senza consegna: %s", a)
	}
	chiusa := consegnaIl(t, d, 1, istanteProva, "2026-10-25T08:00:00.000000Z")
	vecchia := fuori(t, b, chiusa, "")
	if r, err := b.RientraDallArtigiano(ctx, vecchia, "", "", chi); err != nil || r.ConsegnaChiusa {
		t.Fatalf("consegna chiusa: %+v %v", r, err)
	}
	if got := chiusaIl(t, b, chiusa); got != "2026-10-25T08:00:00.000000Z" {
		t.Errorf("richiusa: %s", got)
	}
	for _, via := range []string{"scheda", "qr"} {
		consegna := consegnaIl(t, d, 1, istanteProva, "")
		ultima := fuori(t, b, consegna, "")
		bustaQR(t, b, `consegna_artigiano_id = ?`, consegna)              // mai confermata
		fuori(t, b, consegna, `rientrata_artigiano_il = ?`, istanteProva) // già rientrata
		fuori(t, b, consegna, `cancellata_il = ?`, istanteProva)          // nel cestino
		if via == "scheda" {
			if r, err := b.RientraDallArtigiano(ctx, ultima, "", "", chi); err != nil || !r.ConsegnaChiusa {
				t.Errorf("%s: %+v %v", via, r, err)
			}
		} else if c, err := b.AvanzaDaQR(ctx, ultima, "in_lavorazione", chi); err != nil || !c.Rientrata {
			t.Errorf("%s: %+v %v", via, c, err)
		}
		if chiusaIl(t, b, consegna) == "-" {
			t.Errorf("%s: la consegna resta aperta", via)
		}
	}
}

// inventario: ART-13, ART-14
// Due rientri insieme sulla stessa busta: uno timbra, l'altro è «non fuori bottega»; una nota sola, l'esito
// una volta, una riga d'audit, la consegna chiusa. Dopo il rientro manuale il QR non rientra di nuovo, e dopo
// quello del QR il manuale è rifiutato.
func TestRientroDoppio(t *testing.T) {
	b, d, _ := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	id := fuori(t, b, consegna, "")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, nota := range []string{"prima", "seconda"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = b.RientraDallArtigiano(ctx, id, "fatto", nota, chi)
		}()
	}
	wg.Wait()
	ok, rifiutati := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrNonFuoriBottega):
			rifiutati++
		default:
			t.Errorf("errore: %v", err)
		}
	}
	if ok != 1 || rifiutati != 1 {
		t.Errorf("riusciti %d, rifiutati %d", ok, rifiutati)
	}
	if v := valore(t, d, `SELECT stato || '|' || note_artigiano FROM buste WHERE id = ?`, id); v != "pronta|prima" && v != "pronta|seconda" {
		t.Errorf("busta: %v", v)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.returned_from_artisan'`) != 1 || chiusaIl(t, b, consegna) == "-" {
		t.Error("audit o chiusura")
	}
	if c, err := b.AvanzaDaQR(ctx, id, "pronta", chi); err != nil || c.Rientrata {
		t.Errorf("QR dopo il rientro: %+v %v", c, err)
	}
	auto := fuori(t, b, 0, "")
	if c, err := b.AvanzaDaQR(ctx, auto, "in_lavorazione", chi); err != nil || !c.Rientrata {
		t.Fatalf("QR: %+v %v", c, err)
	}
	if _, err := b.RientraDallArtigiano(ctx, auto, "", "", chi); !errors.Is(err, ErrNonFuoriBottega) {
		t.Errorf("manuale dopo il QR: %v", err)
	}
}

// inventario: ART-13, RIP-S-16
// Gli esiti che una busta può prendere (applicable_outcomes/1), sempre con quello vuoto: tutti e tre da in
// lavorazione, in attesa materiali e pronta (lo stesso stato non è un passaggio); da preventivo solo «da
// rifare»; da consegnata, annullata e dai compro oro solo quello vuoto.
func TestEsitiApplicabili(t *testing.T) {
	casi := []struct {
		stato  string
		oro    bool
		attesi []string
	}{
		{"in_lavorazione", false, []string{"fatto", "da_rifare", ""}},
		{"in_attesa_materiali", false, []string{"fatto", "da_rifare", ""}},
		{"pronta", false, []string{"fatto", "da_rifare", ""}},
		{"preventivo", false, []string{"da_rifare", ""}},
		{"consegnata", false, []string{""}},
		{"annullata", false, []string{""}},
		{"in_valutazione", true, []string{""}},
		{"valutata", true, []string{""}},
	}
	for _, c := range casi {
		if got := EsitiApplicabili(c.stato, c.oro); !slices.Equal(got, c.attesi) {
			t.Errorf("%s: %q", c.stato, got)
		}
	}
}

// inventario: ART-13, CLI-42
// Il rientro che porta a «pronta» accoda l'email di pronta nella sua transazione (avvisaPronta, sezione
// 070b): l'email, il segno e repair.email_ready_queued; niente email se la busta era già «pronta» prima del
// rientro, né con «da_rifare» o con l'esito vuoto.
func TestRientroEmailPronta(t *testing.T) {
	b, d, _ := provaPronta(t)
	c := crea(t, b, modulo())
	confermata(t, b, d, c, indirizzoPronta)
	esegui(t, d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, uscitaProva, c.ID)
	if r, err := b.RientraDallArtigiano(ctx, c.ID, "fatto", "", chi); err != nil || r.Dopo != "pronta" {
		t.Fatalf("rientro: %+v %v", r, err)
	}
	p := emailPronte(t, d)
	if len(p) != 1 || p[0].busta != c.ID || p[0].indirizzo != indirizzoPronta || p[0].stato != "queued" {
		t.Fatalf("email: %+v", p)
	}
	if conta(t, d, `SELECT count(*) FROM buste WHERE id = ? AND email_pronta_inviata_il = rientrata_artigiano_il`, c.ID) != 1 {
		t.Error("il segno non ha l'istante del rientro")
	}
	// La riga del rientro prima di quella dell'email, come lo standalone (che accoda dopo il commit).
	if conta(t, d, `SELECT count(*) FROM eventi_audit r, eventi_audit e WHERE r.azione = 'repair.returned_from_artisan'
		AND e.azione = 'repair.email_ready_queued' AND r.id < e.id`) != 1 {
		t.Error("ordine delle righe")
	}
	for _, x := range []struct{ colonne, esito string }{
		{`stato = 'pronta'`, "fatto"},
		{`stato = 'in_attesa_materiali'`, "da_rifare"},
		{``, ""},
	} {
		altra := crea(t, b, modulo())
		confermata(t, b, d, altra, indirizzoPronta)
		q := `consegnata_artigiano_il = '` + uscitaProva + `'`
		if x.colonne != "" {
			q += ", " + x.colonne
		}
		esegui(t, d, `UPDATE buste SET `+q+` WHERE id = ?`, altra.ID)
		if _, err := b.RientraDallArtigiano(ctx, altra.ID, x.esito, "", chi); err != nil {
			t.Fatalf("%s: %v", x.colonne, err)
		}
		if p := emailPronte(t, d); len(p) != 1 {
			t.Errorf("%s %q: email %+v", x.colonne, x.esito, p)
		}
	}
}

// inventario: ART-13, CLI-42
// Il rientro, il nuovo stato, la chiusura della consegna e l'email si scrivono nella stessa transazione: prima
// del commit c'è tutto; se il commit non arriva non resta niente. Un'email che non parte (la coda che rifiuta)
// non toglie il rientro: repair.email_ready_not_queued e il rientro con «pronta».
func TestRientroEmailStessaTransazione(t *testing.T) {
	b, d, _ := provaPronta(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	c := crea(t, b, modulo())
	confermata(t, b, d, c, indirizzoPronta)
	esegui(t, d, `UPDATE buste SET consegnata_artigiano_il = ?, consegna_artigiano_id = ? WHERE id = ?`, uscitaProva, consegna, c.ID)
	foto := scatta(t, d)
	tracce := traccePronta(t, d)
	guasto := errors.New("commit mancato di prova")
	var visto string
	b.primaDelCommit = func(tx *sql.Tx) error {
		var s string
		if err := tx.QueryRow(`SELECT stato || '|' || (rientrata_artigiano_il IS NOT NULL) || '|' || (email_pronta_inviata_il IS NOT NULL) || '|' ||
			(SELECT chiusa_il IS NOT NULL FROM consegne_artigiani WHERE id = ?) || '|' ||
			(SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.email_ready_queued', 'repair.returned_from_artisan')) FROM buste WHERE id = ?`,
			consegna, c.ID).Scan(&s); err != nil {
			return err
		}
		visto = s
		return guasto
	}
	if _, err := b.RientraDallArtigiano(ctx, c.ID, "fatto", "nota", chi); !errors.Is(err, guasto) {
		t.Fatalf("errore: %v", err)
	}
	b.primaDelCommit = nil
	if visto != "pronta|1|1|1|2" {
		t.Errorf("nella transazione: %s", visto)
	}
	uguali(t, "commit mancato", foto, scatta(t, d))
	if traccePronta(t, d) != tracce || chiusaIl(t, b, consegna) != "-" ||
		valore(t, d, `SELECT stato || coalesce(rientrata_artigiano_il, '-') || coalesce(note_artigiano, '-') FROM buste WHERE id = ?`, c.ID) != "in_lavorazione--" {
		t.Error("è rimasto qualcosa")
	}
	vera := b.posta
	b.posta = postaCheRifiutaIn{vera}
	r, err := b.RientraDallArtigiano(ctx, c.ID, "fatto", "", chi)
	b.posta = vera
	if err != nil || r.Dopo != "pronta" || !r.ConsegnaChiusa {
		t.Fatalf("coda che rifiuta: %+v %v", r, err)
	}
	if !strings.HasSuffix(ultimoAudit(t, d, "repair.email_ready_not_queued"), `"reason":"not_configured"}`) || len(emailPronte(t, d)) != 0 {
		t.Errorf("mancato invio: %s", ultimoAudit(t, d, "repair.email_ready_not_queued"))
	}
	if valore(t, d, `SELECT rientrata_artigiano_il IS NOT NULL FROM buste WHERE id = ?`, c.ID) != int64(1) {
		t.Error("rientro tolto")
	}
}
