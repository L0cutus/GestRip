package buste

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Il profilo del negozio (slice 087, ADM-02), come OnlineSales.put_shop_profile/2 e ShopProfile.changeset/2
// dello standalone (go-baseline): i casi di shop_live_test.exs, shop_tax_code_test.exs e online_sales_test.exs,
// con nomi e numeri inventati. La prova() non semina il profilo: ce n'è uno solo (ambito «default»).

// modulo del profilo dai campi chiave, valore (i campi che mancano non arrivano, come in un modulo forgiato).
func moduloProfilo(kv ...string) ModuloProfilo {
	m := ModuloProfilo{}
	for i := 0; i+1 < len(kv); i += 2 {
		v := kv[i+1]
		m[kv[i]] = &v
	}
	return m
}

// moduloBase è il minimo valido: nome e garanzia.
func moduloBase(extra ...string) ModuloProfilo {
	return moduloProfilo(append([]string{"shop_name", "Bottega Sintetica", "warranty_terms", "Dodici mesi"}, extra...)...)
}

func salvaProfilo(t *testing.T, b *Buste, m ModuloProfilo) {
	t.Helper()
	if err := b.SalvaProfilo(ctx, m, chi); err != nil {
		t.Fatalf("salva: %v", err)
	}
}

// rifiutato dà gli errori per campo di un salvataggio che deve essere rifiutato.
func rifiutato(t *testing.T, b *Buste, m ModuloProfilo) *ProfiloRifiutato {
	t.Helper()
	var r *ProfiloRifiutato
	if err := b.SalvaProfilo(ctx, m, chi); !errors.As(err, &r) {
		t.Fatalf("atteso un rifiuto, avuto %v", err)
	}
	return r
}

// riga del profilo come «nome|garanzia|…» per i campi dati, NULL come «∅».
func rigaProfilo(t *testing.T, b *Buste, colonne ...string) string {
	t.Helper()
	var out []string
	for _, c := range colonne {
		var v *string
		if err := b.d.Lettura.QueryRow(`SELECT ` + c + ` FROM profilo_negozio WHERE ambito = 'default'`).Scan(&v); err != nil {
			return "(assente)"
		}
		if v == nil {
			out = append(out, "∅")
		} else {
			out = append(out, *v)
		}
	}
	return strings.Join(out, "|")
}

// inventario: ADM-02
// La prima volta il profilo si crea: nome e garanzia ripuliti dagli spazi ai bordi, tutti gli altri campi NULL, una riga
// sola con «shop_profile.updated» (anche alla creazione) e l'attore, nella stessa transazione.
func TestProfiloNegozioCreazione(t *testing.T) {
	b, d, _ := prova(t)
	if n := conta(t, d, `SELECT count(*) FROM profilo_negozio`); n != 0 {
		t.Fatalf("la prova ha già un profilo: %d", n)
	}
	salvaProfilo(t, b, moduloProfilo("shop_name", " Otto Gioielli ", "warranty_terms", " Garanzia iniziale "))
	if got := rigaProfilo(t, b, "nome", "condizioni_garanzia", "ragione_sociale", "indirizzo", "cap", "citta", "provincia", "email",
		"email_vendite", "telefono", "partita_iva", "codice_fiscale", "sottotitolo", "orari", "instagram", "facebook", "sito"); got !=
		"Otto Gioielli|Garanzia iniziale|∅|∅|∅|∅|∅|∅|∅|∅|∅|∅|∅|∅|∅|∅|∅" {
		t.Fatalf("riga %q", got)
	}
	if got := rigaProfilo(t, b, "ambito", "creato_il", "aggiornato_il"); got != "default|"+istanteProva+"|"+istanteProva {
		t.Fatalf("ambito e istanti %q", got)
	}
	id := valore(t, d, `SELECT id FROM profilo_negozio`)
	if got := ultimoAudit(t, d, "shop_profile.updated"); got != "shop_profile.updated|shop_profile|"+itoa(id.(int64))+"|ok|{}" {
		t.Fatalf("audit %q", got)
	}
	if got := valore(t, d, `SELECT attore FROM eventi_audit WHERE azione = 'shop_profile.updated'`); got != chi.Attore {
		t.Fatalf("attore %v", got)
	}
}

// inventario: ADM-02
// Un secondo salvataggio riscrive la stessa riga (il profilo è uno solo, anche con un campo svuotato), lascia
// «creato_il» e porta «aggiornato_il» all'ora del salvataggio; ogni salvataggio scrive una riga d'audit.
func TestProfiloNegozioSecondoSalvataggio(t *testing.T) {
	b, d, o := prova(t)
	salvaProfilo(t, b, moduloBase("city", "Paese Finto", "subtitle", "Oreficeria dal 1952"))
	id := valore(t, d, `SELECT id FROM profilo_negozio`)
	o.Sposta(2 * time.Hour)
	salvaProfilo(t, b, moduloProfilo("shop_name", "Bottega Sintetica", "warranty_terms", "Nuove condizioni", "city", "   ", "subtitle", "Oreficeria dal 1952"))
	if n := conta(t, d, `SELECT count(*) FROM profilo_negozio`); n != 1 {
		t.Fatalf("righe %d", n)
	}
	if got := valore(t, d, `SELECT id FROM profilo_negozio`); got != id {
		t.Fatalf("id %v, atteso %v", got, id)
	}
	if got := rigaProfilo(t, b, "condizioni_garanzia", "citta", "sottotitolo"); got != "Nuove condizioni|∅|Oreficeria dal 1952" {
		t.Fatalf("riga %q", got)
	}
	if got := rigaProfilo(t, b, "creato_il", "aggiornato_il"); got != istanteProva+"|2026-10-31T11:00:00.000000Z" {
		t.Fatalf("istanti %q", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated' AND id_risorsa = ?`, itoa(id.(int64))); n != 2 {
		t.Fatalf("righe d'audit %d", n)
	}
}

// inventario: ADM-02
// Nome e testo della garanzia sono obbligatori: vuoti, di soli spazi (anche Unicode) o assenti danno «Campo
// obbligatorio.» sul loro campo, tutti e due insieme, e non si scrive niente (nemmeno l'audit); sul profilo già
// salvato resta com'era.
func TestProfiloNegozioObbligatori(t *testing.T) {
	b, d, _ := prova(t)
	for nome, m := range map[string]ModuloProfilo{
		"vuoti":   moduloProfilo("shop_name", "", "warranty_terms", ""),
		"spazi":   moduloProfilo("shop_name", "  \t ", "warranty_terms", " 　"),
		"assenti": moduloProfilo("city", "Paese Finto"),
	} {
		r := rifiutato(t, b, m)
		if len(r.Errori) != 2 || len(r.Errori["shop_name"]) != 1 || r.Errori["shop_name"][0] != "Campo obbligatorio." ||
			len(r.Errori["warranty_terms"]) != 1 || r.Errori["warranty_terms"][0] != "Campo obbligatorio." {
			t.Errorf("%s: errori %v", nome, r.Errori)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM profilo_negozio`) + conta(t, d, `SELECT count(*) FROM eventi_audit`); n != 0 {
		t.Fatalf("un rifiuto ha scritto: %d", n)
	}
	salvaProfilo(t, b, moduloBase("city", "Paese Finto"))
	prima := rigaProfilo(t, b, "nome", "condizioni_garanzia", "citta", "aggiornato_il")
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	r := rifiutato(t, b, moduloProfilo("shop_name", "", "warranty_terms", "Altro testo", "city", "Altro paese"))
	if len(r.Errori) != 1 || r.Errori["shop_name"][0] != "Campo obbligatorio." {
		t.Fatalf("errori %v", r.Errori)
	}
	if rigaProfilo(t, b, "nome", "condizioni_garanzia", "citta", "aggiornato_il") != prima || conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
		t.Fatal("un rifiuto ha scritto sul profilo già salvato")
	}
	// Un campo obbligatorio che non arriva lascia quello salvato (come cast/3): non è un errore.
	salvaProfilo(t, b, moduloProfilo("city", "Altro paese"))
	if got := rigaProfilo(t, b, "nome", "condizioni_garanzia", "citta"); got != "Bottega Sintetica|Dodici mesi|Altro paese" {
		t.Fatalf("riga %q", got)
	}
}

// inventario: ADM-02
// Le lunghezze massime per campo, in grafemi dopo gli spazi ai bordi tolti: il massimo passa, uno in più no, con
// «Troppo lungo (massimo N caratteri).» sul campo; il testo della garanzia non ha limite.
func TestProfiloNegozioLunghezze(t *testing.T) {
	b, d, _ := prova(t)
	massimi := []struct {
		campo string
		n     int
	}{{"shop_name", 255}, {"legal_name", 255}, {"address", 255}, {"postal_code", 20}, {"city", 120}, {"province", 20},
		{"email", 255}, {"sales_email", 255}, {"phone", 80}, {"fiscal_id", 40}, {"subtitle", 255}, {"opening_hours", 500},
		{"instagram", 255}, {"facebook", 255}, {"website", 255}}
	for _, c := range massimi {
		// Il campo dato in coda vince su quello di moduloBase (shop_name).
		salvaProfilo(t, b, moduloBase(c.campo, "  "+strings.Repeat("a", c.n)+"  "))
		r := rifiutato(t, b, moduloBase(c.campo, strings.Repeat("b", c.n+1)))
		want := "Troppo lungo (massimo " + itoa(int64(c.n)) + " caratteri)."
		if len(r.Errori) != 1 || len(r.Errori[c.campo]) != 1 || r.Errori[c.campo][0] != want {
			t.Errorf("%s: errori %v, atteso %q", c.campo, r.Errori, want)
		}
	}
	// I grafemi, non i byte né i punti di codice: «e» e accento combinato sono un carattere.
	salvaProfilo(t, b, moduloBase("postal_code", strings.Repeat("é", 20)))
	if r := rifiutato(t, b, moduloBase("postal_code", strings.Repeat("é", 21))); r.Errori["postal_code"][0] != "Troppo lungo (massimo 20 caratteri)." {
		t.Errorf("errori %v", r.Errori)
	}
	salvaProfilo(t, b, moduloBase("warranty_terms", strings.Repeat("g", 20000)))
	if n := valore(t, d, `SELECT length(condizioni_garanzia) FROM profilo_negozio`); n != int64(20000) {
		t.Fatalf("garanzia lunga %v", n)
	}
	// Più errori insieme, uno per campo; il rifiuto non scrive.
	prima := rigaProfilo(t, b, "citta", "orari", "aggiornato_il")
	r := rifiutato(t, b, moduloBase("city", strings.Repeat("c", 121), "opening_hours", strings.Repeat("o", 501), "province", "ok"))
	if len(r.Errori) != 2 || r.Errori["city"][0] != "Troppo lungo (massimo 120 caratteri)." || r.Errori["opening_hours"][0] != "Troppo lungo (massimo 500 caratteri)." {
		t.Errorf("errori %v", r.Errori)
	}
	if rigaProfilo(t, b, "citta", "orari", "aggiornato_il") != prima {
		t.Error("il rifiuto ha scritto")
	}
}

// inventario: ADM-02
// I campi facoltativi: ripuliti dagli spazi ai bordi (anche Unicode), vuoti o di soli spazi diventano NULL, e
// riscrivere un campo con niente lo toglie. Un campo che non arriva nel modulo resta com'è.
func TestProfiloNegozioFacoltativi(t *testing.T) {
	b, _, _ := prova(t)
	salvaProfilo(t, b, moduloBase("opening_hours", "  Lun-Sab 9-19  ", "instagram", " @otto ", "facebook", "otto.gioielli", "website", "otto.example",
		"legal_name", " Otto S.r.l.　", "address", "", "email", "   "))
	if got := rigaProfilo(t, b, "orari", "instagram", "facebook", "sito", "ragione_sociale", "indirizzo", "email"); got !=
		"Lun-Sab 9-19|@otto|otto.gioielli|otto.example|Otto S.r.l.|∅|∅" {
		t.Fatalf("riga %q", got)
	}
	salvaProfilo(t, b, moduloBase("opening_hours", "", "instagram", " \t "))
	if got := rigaProfilo(t, b, "orari", "instagram", "facebook"); got != "∅|∅|otto.gioielli" {
		t.Fatalf("dopo lo svuotamento %q", got)
	}
	// Un campo non arrivato resta, uno arrivato vuoto si toglie: «facebook» non c'è nel modulo.
	salvaProfilo(t, b, moduloBase("sales_email", "vendite@bottega.example"))
	if got := rigaProfilo(t, b, "facebook", "email_vendite", "sito"); got != "otto.gioielli|vendite@bottega.example|otto.example" {
		t.Fatalf("campi non arrivati %q", got)
	}
}

// inventario: ADM-02
// Come Ecto, i cambi sono quelli del cast: un valore scritto uguale a quello salvato (anche con spazi ai bordi, come
// lo lascia un import) non si ripulisce né si controlla, uno diverso sì.
func TestProfiloNegozioSoloICambiPassanoDalControllo(t *testing.T) {
	b, d, _ := prova(t)
	salvaProfilo(t, b, moduloBase())
	esegui(t, d, `UPDATE profilo_negozio SET indirizzo = 'Via Finta 1  ', citta = ' Paese Finto', cap = 'ABCDE'||'0123456789'||'0123456789'||'0123456789'`)
	salvaProfilo(t, b, moduloBase("address", "Via Finta 1  ", "city", " Paese Finto", "postal_code", "ABCDE"+strings.Repeat("0123456789", 3)))
	if got := rigaProfilo(t, b, "indirizzo", "citta", "length(cap)"); got != "Via Finta 1  | Paese Finto|35" {
		t.Fatalf("valori uguali ai salvati %q", got)
	}
	salvaProfilo(t, b, moduloBase("address", " Via Finta 2  ", "city", "Paese Finto "))
	if got := rigaProfilo(t, b, "indirizzo", "citta"); got != "Via Finta 2|Paese Finto" {
		t.Fatalf("valori diversi %q", got)
	}
}

// inventario: ADM-02
// Il codice fiscale: senza spazi (anche Unicode) e in maiuscole, 16 lettere e cifre o 11 cifre; ogni altra cosa è
// rifiutata con il messaggio dello standalone e il profilo resta com'era; svuotato torna NULL (la P.IVA).
func TestProfiloNegozioCodiceFiscale(t *testing.T) {
	b, _, _ := prova(t)
	salvaProfilo(t, b, moduloBase("fiscal_id", "01234567890", "tax_code", " zmp sfn70a01l407x "))
	if got := rigaProfilo(t, b, "codice_fiscale", "partita_iva"); got != "ZMPSFN70A01L407X|01234567890" {
		t.Fatalf("riga %q", got)
	}
	salvaProfilo(t, b, moduloBase("tax_code", "012 345 678 90"))
	if got := rigaProfilo(t, b, "codice_fiscale"); got != "01234567890" {
		t.Fatalf("società %q", got)
	}
	for _, cf := range []string{"ABC123", "ZMPSFN70A01L407", "ZMPSFN70A01L407XY", "0123456789A", "ZMPSFN70A01L40-X", "0123456789", "ZMPSFN70A01L407è"} {
		prima := rigaProfilo(t, b, "codice_fiscale", "aggiornato_il")
		r := rifiutato(t, b, moduloBase("tax_code", cf))
		if len(r.Errori) != 1 || len(r.Errori["tax_code"]) != 1 || r.Errori["tax_code"][0] != "codice fiscale non valido: 16 caratteri, o 11 cifre per una società" {
			t.Errorf("%q: errori %v", cf, r.Errori)
		}
		if rigaProfilo(t, b, "codice_fiscale", "aggiornato_il") != prima {
			t.Errorf("%q: il rifiuto ha scritto", cf)
		}
	}
	// Le maiuscole di «ß» e delle legature si sciolgono come String.upcase/1 (ß → SS): 14 caratteri più «ß» fanno 16.
	salvaProfilo(t, b, moduloBase("tax_code", "ZMPSFN70A01L40ß"))
	if got := rigaProfilo(t, b, "codice_fiscale"); got != "ZMPSFN70A01L40SS" {
		t.Fatalf("ß %q", got)
	}
	salvaProfilo(t, b, moduloBase("tax_code", "  "))
	if got := rigaProfilo(t, b, "codice_fiscale"); got != "∅" {
		t.Fatalf("svuotato %q", got)
	}
}

// inventario: ADM-02
// Un campo che arriva in una forma che non è un testo (una lista o una mappa nel modulo) è «Valore non valido.»
// sul campo, senza altri errori per quel campo, e non si scrive niente.
func TestProfiloNegozioValoreNonTesto(t *testing.T) {
	b, d, _ := prova(t)
	m := moduloBase()
	m["city"] = nil
	m["shop_name"] = nil
	r := rifiutato(t, b, m)
	if len(r.Errori) != 2 || r.Errori["city"][0] != "Valore non valido." || len(r.Errori["shop_name"]) != 1 || r.Errori["shop_name"][0] != "Valore non valido." {
		t.Fatalf("errori %v", r.Errori)
	}
	if conta(t, d, `SELECT count(*) FROM profilo_negozio`) != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
}

// inventario: ADM-02
// Dopo un rifiuto i valori da rimettere nel modulo sono quelli del changeset: il cambio ripulito (vuoto → «»), il
// testo scritto per un campo che non cambia (senza profilo, uno di soli spazi è «niente» come il salvato, quindi
// resta com'è scritto), il salvato per un campo che non è arrivato.
func TestProfiloNegozioValoriDopoUnRifiuto(t *testing.T) {
	b, _, _ := prova(t)
	if r := rifiutato(t, b, moduloProfilo("shop_name", "  ", "warranty_terms", "", "city", " Paese ")); r.Valori["shop_name"] != "  " ||
		r.Valori["warranty_terms"] != "" || r.Valori["city"] != "Paese" {
		t.Errorf("senza profilo: %v", r.Valori)
	}
	salvaProfilo(t, b, moduloBase("city", "Paese Finto", "address", "Via Finta 1", "instagram", "@otto"))
	r := rifiutato(t, b, moduloProfilo("shop_name", "  ", "warranty_terms", "  Nuova garanzia  ", "city", "Paese Finto", "address", " Via Nuova ",
		"instagram", "   ", "website", "  sito.example  "))
	atteso := map[string]string{"shop_name": "", "warranty_terms": "Nuova garanzia", "city": "Paese Finto", "address": "Via Nuova",
		"instagram": "", "website": "sito.example", "facebook": "", "legal_name": ""}
	for k, v := range atteso {
		if r.Valori[k] != v {
			t.Errorf("valore di %s = %q, atteso %q", k, r.Valori[k], v)
		}
	}
	if len(r.Valori) != len(CampiProfilo) {
		t.Errorf("valori %d, campi %d", len(r.Valori), len(CampiProfilo))
	}
}

// inventario: ADM-02
// Il profilo salvato si rilegge per il modulo con tutti i campi (NULL come testo vuoto), e senza profilo è tutto
// vuoto.
func TestProfiloPerIlModulo(t *testing.T) {
	b, _, _ := prova(t)
	v, err := b.ProfiloPerModulo(ctx)
	if err != nil || len(v) != len(CampiProfilo) {
		t.Fatalf("senza profilo: %v %v", v, err)
	}
	for k, x := range v {
		if x != "" {
			t.Errorf("senza profilo %s = %q", k, x)
		}
	}
	salvaProfilo(t, b, moduloBase("legal_name", "Bottega S.r.l.", "tax_code", "01234567890", "province", "XX"))
	v, err = b.ProfiloPerModulo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v["shop_name"] != "Bottega Sintetica" || v["warranty_terms"] != "Dodici mesi" || v["legal_name"] != "Bottega S.r.l." ||
		v["tax_code"] != "01234567890" || v["province"] != "XX" || v["city"] != "" {
		t.Errorf("valori %v", v)
	}
}

// inventario: ADM-02
// La riga d'audit è nella transazione: se non si scrive, il profilo non si crea e quello salvato non cambia.
func TestProfiloNegozioAuditNellaTransazione(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `CREATE TEMP TRIGGER no_audit_profilo BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'shop_profile.updated'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if err := b.SalvaProfilo(ctx, moduloBase(), chi); err == nil {
		t.Fatal("salvato senza la riga d'audit")
	} else if errors.As(err, new(*ProfiloRifiutato)) {
		t.Fatalf("un guasto non è un rifiuto: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM profilo_negozio`) != 0 {
		t.Fatal("la creazione è rimasta senza la riga d'audit")
	}
	esegui(t, d, `DROP TRIGGER no_audit_profilo`)
	salvaProfilo(t, b, moduloBase("city", "Paese Finto"))
	esegui(t, d, `CREATE TEMP TRIGGER no_audit_profilo BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'shop_profile.updated'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if err := b.SalvaProfilo(ctx, moduloBase("city", "Altro paese"), chi); err == nil {
		t.Fatal("modificato senza la riga d'audit")
	}
	if got := rigaProfilo(t, b, "citta"); got != "Paese Finto" {
		t.Fatalf("la modifica è rimasta senza la riga d'audit: %q", got)
	}
}

// inventario: ADM-02
// Chi legge già il profilo vede i valori salvati dalla pagina: l'informativa e il portale ritirato (nome, ragione
// sociale, P.IVA, sede, contatti), il nome della lista, la ricevuta (nome e sottotitolo), la scheda PDF, il report e
// i testi dei messaggi (città, orari, social).
func TestProfiloSalvatoLoLeggonoGliAltri(t *testing.T) {
	b, _, _ := prova(t)
	salvaProfilo(t, b, moduloBase("shop_name", "Bottega Letta", "subtitle", "Oreficeria dal 1952", "legal_name", "Bottega Letta S.r.l.",
		"fiscal_id", "01234567890", "address", "Via Finta 1", "city", "Paese Finto", "phone", "0423 000111", "email", "info@bottega.example",
		"opening_hours", "Lun-Sab 9-19", "instagram", "@lettura", "facebook", "lettura.fb", "website", "lettura.example"))
	p, err := b.LeggiProfiloNegozio(ctx)
	if err != nil || !p.Presente || p.Nome != "Bottega Letta" || valoreTesto(p.RagioneSociale) != "Bottega Letta S.r.l." ||
		valoreTesto(p.PartitaIVA) != "01234567890" || valoreTesto(p.Indirizzo) != "Via Finta 1" || valoreTesto(p.Citta) != "Paese Finto" ||
		valoreTesto(p.Telefono) != "0423 000111" || valoreTesto(p.Email) != "info@bottega.example" {
		t.Fatalf("informativa: %+v %v", p, err)
	}
	if n, err := b.nomeNegozio(ctx); err != nil || n != "Bottega Letta" {
		t.Fatalf("nome della lista: %q %v", n, err)
	}
	pa, err := b.leggiProfiloAvviso(ctx)
	if err != nil || pa.negozio() != "Bottega Letta" || valoreTesto(pa.citta) != "Paese Finto" || valoreTesto(pa.orari) != "Lun-Sab 9-19" ||
		valoreTesto(pa.instagram) != "@lettura" || valoreTesto(pa.facebook) != "lettura.fb" || valoreTesto(pa.sito) != "lettura.example" {
		t.Fatalf("testi dei messaggi: %+v %v", pa, err)
	}
	c := crea(t, b, modulo())
	id := itoa(c.ID)
	if _, dr, err := b.Ricevuta(ctx, id, chi, func(DatiRicevuta) ([]byte, error) { return []byte("%PDF"), nil }); err != nil || dr.NomeNegozio != "Bottega Letta" || valoreTesto(dr.Sottotitolo) != "Oreficeria dal 1952" {
		t.Fatalf("ricevuta: %+v %v", dr, err)
	}
	if _, ds, err := b.SchedaPDF(ctx, id, chi, func(DatiSchedaPDF) ([]byte, error) { return []byte("%PDF"), nil }); err != nil || ds.NomeNegozio != "Bottega Letta" {
		t.Fatalf("scheda PDF: %+v %v", ds, err)
	}
	if _, dr, err := b.Report(ctx, []int64{c.ID}, chi, func(DatiReport) ([]byte, error) { return []byte("%PDF"), nil }); err != nil || dr.NomeNegozio != "Bottega Letta" {
		t.Fatalf("report: %+v %v", dr, err)
	}
	// Il nome riscritto da un secondo salvataggio si legge subito, da tutti.
	salvaProfilo(t, b, moduloBase("shop_name", "Bottega Nuova"))
	if n, _ := b.nomeNegozio(ctx); n != "Bottega Nuova" {
		t.Fatalf("nome dopo la modifica: %q", n)
	}
}

// inventario: ADM-02
// Un salvataggio che non cambia niente non tocca la riga: un valore che dopo gli spazi tolti è quello salvato («Paese»
// su «Paese»), un codice fiscale che normalizzato è quello salvato, un secondo salvataggio identico lasciano
// `aggiornato_il` com'è (nessun UPDATE, come repo.update/1 senza cambi) e scrivono comunque la riga d'audit.
func TestProfiloNegozioSenzaCambiNienteUpdate(t *testing.T) {
	b, d, o := prova(t)
	salvaProfilo(t, b, moduloBase("city", "Paese", "tax_code", "ZMPSFN70A01L407X"))
	prima := rigaProfilo(t, b, "aggiornato_il", "citta", "codice_fiscale")
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated'`)
	for nome, m := range map[string]ModuloProfilo{
		"valore uguale dopo gli spazi tolti":      moduloBase("city", " Paese ", "tax_code", "ZMPSFN70A01L407X"),
		"codice fiscale uguale dopo normalizzare": moduloBase("city", "Paese", "tax_code", "zmp sfn70a01l407x"),
		"salvataggio identico":                    moduloBase("city", "Paese", "tax_code", "ZMPSFN70A01L407X"),
	} {
		o.Sposta(time.Hour)
		salvaProfilo(t, b, m)
		audit++
		if got := rigaProfilo(t, b, "aggiornato_il", "citta", "codice_fiscale"); got != prima {
			t.Errorf("%s: riga %q, attesa %q (nessun UPDATE)", nome, got, prima)
		}
		if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated'`); n != audit {
			t.Errorf("%s: righe d'audit %d, attese %d", nome, n, audit)
		}
	}
}
