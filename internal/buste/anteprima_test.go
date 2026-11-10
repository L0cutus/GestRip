package buste

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// L'anteprima del cliente (CLI-27, RIP-S-27), come preview/2 e repair_customer_preview_test.exs dello
// standalone (go-baseline): la pagina del link senza token, una sola riga d'audit, niente altro.

// fotografiaTabelle è il numero di righe di ogni tabella e la riga della busta com'è salvata
// (snapshot/1 del test dello standalone).
func fotografiaTabelle(t *testing.T, b *Buste, id int64) (map[string]int, string) {
	t.Helper()
	righe, err := b.d.Lettura.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var nomi []string
	for righe.Next() {
		var n string
		if err := righe.Scan(&n); err != nil {
			t.Fatal(err)
		}
		nomi = append(nomi, n)
	}
	_ = righe.Close()
	conti := map[string]int{}
	for _, n := range nomi {
		conti[n] = conta(t, b.d, `SELECT count(*) FROM "`+n+`"`)
	}
	var riga strings.Builder
	r, err := b.d.Lettura.Query(`SELECT * FROM buste WHERE id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	colonne, _ := r.Columns()
	for r.Next() {
		v := make([]any, len(colonne))
		p := make([]any, len(colonne))
		for i := range v {
			p[i] = &v[i]
		}
		if err := r.Scan(p...); err != nil {
			t.Fatal(err)
		}
		for i, c := range colonne {
			riga.WriteString(c + "=")
			switch x := v[i].(type) {
			case []byte:
				riga.WriteString(string(x))
			case nil:
				riga.WriteString("NULL")
			default:
				riga.WriteString(fmt.Sprint(x))
			}
			riga.WriteString(";")
		}
	}
	return conti, riga.String()
}

// inventario: CLI-27, RIP-S-27
// L'anteprima è la stessa pagina che il link della busta disegna (stessi campi, stesse foto, stesso
// preventivo, stessa conversazione), e resta uguale con il link revocato, scaduto o mai emesso.
// Scrive una sola riga, repair.customer_preview_viewed con il numero interno e chi guarda; nessun
// segno «letto», nessun token, nessun'altra riga in nessuna tabella.
func TestAnteprimaClienteComeLaPagina(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.NotePubbliche = "Pietra da ripulire"; a.Preventivo = "80,00" }))
	if _, err := b.CambiaStato(ctx, c.ID, "preventivo", chi); err != nil {
		t.Fatal(err)
	}
	if err := b.ScriviMessaggioCliente(ctx, c.Token, "Quando è pronto?", chi); err != nil {
		t.Fatal(err)
	}
	link, err := b.PaginaCliente(ctx, c.Token)
	if err != nil {
		t.Fatal(err)
	}
	conti, riga := fotografiaTabelle(t, b, c.ID)
	a, err := b.AnteprimaCliente(ctx, itoa(c.ID), chi)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, link) {
		t.Errorf("anteprima diversa dalla pagina del link:\n%+v\n%+v", a, link)
	}
	if !a.ConConversazione || len(a.Conversazione) != 1 || a.Risposta.Fase != RispostaInAttesa {
		t.Errorf("conversazione %v %d, preventivo %v", a.ConConversazione, len(a.Conversazione), a.Risposta.Fase)
	}
	conti2, riga2 := fotografiaTabelle(t, b, c.ID)
	if riga2 != riga {
		t.Errorf("la busta è cambiata:\n%s\n%s", riga, riga2)
	}
	conti["eventi_audit"]++
	if !reflect.DeepEqual(conti, conti2) {
		t.Errorf("righe cambiate: %v → %v", conti, conti2)
	}
	if u := ultimoAudit(t, d, "repair.customer_preview_viewed"); u != "repair.customer_preview_viewed|repair|"+itoa(c.ID)+`|ok|{"bag_number":"1/26"}` {
		t.Errorf("audit %q", u)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.customer_preview_viewed' AND attore = ? AND ip = ?`, chi.Attore, chi.IP); n != 1 {
		t.Errorf("righe dell'anteprima di chi guarda: %d", n)
	}
	// Link revocato, scaduto, senza testo (nel Go ogni busta ha l'impronta del link: «mai emesso» è il
	// testo che manca, come un link importato con la sola impronta): la stessa pagina.
	for _, q := range []string{
		`UPDATE buste SET token_pubblico_revocato_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`,
		`UPDATE buste SET token_pubblico_revocato_il = NULL, token_pubblico_scade_il = '2026-10-01T00:00:00.000000Z' WHERE id = ?`,
		`UPDATE buste SET token_pubblico = NULL WHERE id = ?`,
	} {
		esegui(t, d, q, c.ID)
		x, err := b.AnteprimaCliente(ctx, itoa(c.ID), chi)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if !reflect.DeepEqual(x, link) {
			t.Errorf("%s: pagina diversa", q)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.customer_preview_viewed'`); n != 4 {
		t.Errorf("righe dell'anteprima: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM buste WHERE id = ? AND token_pubblico IS NULL AND token_pubblico_scade_il = '2026-10-01T00:00:00.000000Z'`, c.ID); n != 1 {
		t.Error("l'anteprima ha emesso un token")
	}
}

// inventario: CLI-27, RIP-S-27
// Su una busta B2B l'anteprima è la pagina dell'oreficeria (le foto «orefice», non quelle «cliente»).
func TestAnteprimaClienteOreficeria(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	link, err := b.PaginaCliente(ctx, c.Token)
	if err != nil {
		t.Fatal(err)
	}
	a, err := b.AnteprimaCliente(ctx, itoa(c.ID), chi)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Oreficeria || !reflect.DeepEqual(a, link) {
		t.Errorf("anteprima B2B: %+v", a)
	}
	_ = d
}

// inventario: CLI-27, RIP-S-27
// Busta che non c'è, id malformato o busta nel cestino: ErrBustaNonTrovata e nessuna riga; una riga
// che non si scrive: ErrAnteprimaNonRegistrata, niente pagina.
func TestAnteprimaClienteRifiuti(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	cestino := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, cestino.ID)
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, id := range []string{"999999", "abc", "0", "-1", "", "1.0", " 1", "99999999999999999999", itoa(cestino.ID)} {
		if _, err := b.AnteprimaCliente(ctx, id, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%q: %v", id, err)
		}
	}
	if dopo := conta(t, d, `SELECT count(*) FROM eventi_audit`); dopo != prima {
		t.Errorf("righe d'audit: %d", dopo-prima)
	}
	esegui(t, d, `CREATE TEMP TRIGGER no_anteprima BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.customer_preview_viewed'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.AnteprimaCliente(ctx, itoa(c.ID), chi); !errors.Is(err, ErrAnteprimaNonRegistrata) {
		t.Errorf("riga non scritta: %v", err)
	}
}

// inventario: CLI-53, CLI-54
// Il profilo del negozio come lo leggono /m/ e /privacy: assente senza riga, i campi NULL nil, quelli
// scritti com'è (anche vuoti).
func TestLeggiProfiloNegozio(t *testing.T) {
	b, d, _ := prova(t)
	p, err := b.LeggiProfiloNegozio(ctx)
	if err != nil || p.Presente {
		t.Fatalf("senza profilo: %+v %v", p, err)
	}
	esegui(t, d, `INSERT INTO profilo_negozio (nome, ragione_sociale, partita_iva, indirizzo, citta, telefono, email, condizioni_garanzia,
		creato_il, aggiornato_il) VALUES ('Bottega Inventata', 'Bottega Inventata S.r.l.', '00000000000', 'Via Finta 1', 'Paese Finto',
		'', NULL, '', ?, ?)`, istanteProva, istanteProva)
	p, err = b.LeggiProfiloNegozio(ctx)
	if err != nil {
		t.Fatal(err)
	}
	vuoto := ""
	ragione, piva, via, paese := "Bottega Inventata S.r.l.", "00000000000", "Via Finta 1", "Paese Finto"
	atteso := ProfiloNegozio{Presente: true, Nome: "Bottega Inventata", RagioneSociale: &ragione, PartitaIVA: &piva, Indirizzo: &via,
		Citta: &paese, Telefono: &vuoto}
	if !reflect.DeepEqual(p, atteso) {
		t.Errorf("profilo %+v", p)
	}
}
