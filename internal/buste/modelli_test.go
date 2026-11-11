package buste

import (
	"strings"
	"testing"
)

// I testi WhatsApp (CLI-31, CLI-32, CLI-33), come Outgoing.Templates dello standalone (go-baseline):
// i casi di templates_test.exs sulle nove famiglie WhatsApp. L'editor (CLI-38) è della slice 067.

// inventario: CLI-31
// Le nove famiglie, nell'ordine dello standalone, con l'etichetta della sorgente, la chiave delle
// impostazioni e i campi dichiarati; i predefiniti nominano solo campi dichiarati e i testi del
// cliente portano {link} e mai {password}.
func TestFamiglieWhatsApp(t *testing.T) {
	ordine := []string{"wa_preventivo_con_importo", "wa_preventivo_senza_importo", "wa_pronta_compro_oro", "wa_pronta",
		"wa_link_stato", "wa_cliente_aggiornamento", "wa_oreficeria", "wa_oreficeria_lotto", "wa_artigiano"}
	if len(FamiglieWhatsApp) != len(ordine) {
		t.Fatalf("famiglie %d", len(FamiglieWhatsApp))
	}
	etichette := map[string]string{
		"wa_preventivo_con_importo": "Preventivo (con importo)", "wa_preventivo_senza_importo": "Preventivo (senza importo)",
		"wa_pronta_compro_oro": "Pronta — Compro oro", "wa_pronta": "Pronta — con link", "wa_link_stato": "Invia link stato cliente",
		"wa_cliente_aggiornamento": "Aggiornamento link al cliente", "wa_oreficeria": "Aggiornamento oreficeria",
		"wa_oreficeria_lotto": "Avviso lotto pronte oreficeria", "wa_artigiano": "Sollecito artigiano",
	}
	for i, f := range FamiglieWhatsApp {
		if string(f) != ordine[i] {
			t.Errorf("famiglia %d: %s, attesa %s", i, f, ordine[i])
		}
		if f.Etichetta() != etichette[string(f)] {
			t.Errorf("%s: etichetta %q", f, f.Etichetta())
		}
		if f.Chiave() != "outgoing.template."+string(f) {
			t.Errorf("%s: chiave %q", f, f.Chiave())
		}
		if e := ErroreModello(f, f.Predefinito()); e != "" {
			t.Errorf("%s: il predefinito non vale: %s", f, e)
		}
	}
	cliente := "nome numero link negozio citta orari social telegram_optin"
	campi := map[Famiglia]string{
		FamPreventivoConImporto: cliente + " preventivo descrizione", FamPreventivoSenzaImporto: cliente,
		FamLinkStato: cliente + " stima", FamClienteAggiornamento: cliente + " stima", FamPronta: cliente,
		FamProntaCompraOro: cliente, FamOreficeria: "oref_nome nome_cliente numero lavorazione stato importo consegna note_riga link negozio",
		FamOreficeriaLotto: "oref_nome n_buste link negozio", FamArtigiano: "artigiano_nome numero lavorazione stato consegna note_riga link negozio",
	}
	for f, c := range campi {
		if strings.Join(f.Campi(), " ") != c {
			t.Errorf("%s: campi %q", f, f.Campi())
		}
	}
	for _, f := range FamiglieCliente {
		if !strings.Contains(f.Predefinito(), "{link}") || strings.Contains(f.Predefinito(), "password") {
			t.Errorf("%s: predefinito senza link o con il codice", f)
		}
		if e := ErroreModello(f, "Codice {password}"); e != "Segnaposto non supportato: {password}" {
			t.Errorf("%s: %q", f, e)
		}
		if e := ErroreModello(f, "Ecco {link} {orari} {social} {telegram_optin}"); e != "" {
			t.Errorf("%s: %q", f, e)
		}
	}
	if e := ErroreModello(FamOreficeria, "Ciao {telegram_optin}"); e != "Segnaposto non supportato: {telegram_optin}" {
		t.Errorf("oreficeria con telegram_optin: %q", e)
	}
	// Parola per parola (app.py della sorgente meno la riga del codice d'accesso, {stima} della 158).
	if FamLinkStato.Predefinito() != "Buongiorno {nome}, può seguire in tempo reale lo stato della Sua riparazione n° {numero} da questo link:\n{link}\n\n{stima}{social}👋 {negozio}{citta}\n{orari}" {
		t.Errorf("wa_link_stato: %q", FamLinkStato.Predefinito())
	}
	if FamOreficeria.Predefinito() != "Ciao {oref_nome}, aggiornamento sulla lavorazione:\nCliente: {nome_cliente} — busta n. {numero}\n{lavorazione}Stato: {stato}\n{importo}{consegna}{note_riga}\nDettagli e foto: {link}\n{negozio}" {
		t.Errorf("wa_oreficeria: %q", FamOreficeria.Predefinito())
	}
	if FamPreventivoConImporto.Predefinito() != FamPreventivoSenzaImporto.Predefinito() {
		t.Error("i due preventivi devono avere lo stesso testo")
	}
}

// inventario: CLI-31
// format_error/2: niente da dire per un testo con i soli campi dichiarati; un campo non dichiarato
// è «Segnaposto non supportato: {x}»; un campo posizionale, vuoto, con attributo, indice, formato,
// conversione o una graffa dentro è «Sintassi non valida: …»; una } da sola, una { non chiusa e un
// byte non UTF-8 anche. Un nome con spazi o oltre 32 caratteri si mostra «{…}».
func TestErroreModello(t *testing.T) {
	if e := ErroreModello(FamOreficeria, "Ciao {oref_nome}"); e != "" {
		t.Errorf("valido: %q", e)
	}
	if e := ErroreModello(FamOreficeria, "Ciao {nome}"); e != "Segnaposto non supportato: {nome}" {
		t.Errorf("sconosciuto: %q", e)
	}
	sintassi := map[string]string{
		"Ciao {0}":             "un segnaposto posizionale {0}",
		"Ciao {}":              "un segnaposto senza nome",
		"Ciao {rep.nome}":      "un attributo in {rep.nome}",
		"Ciao {rep[0]}":        "un indice in {rep[0]}",
		"Ciao {oref_nome:>10}": "un formato in {oref_nome:>10}",
		"Ciao {oref_nome!r}":   "una conversione in {oref_nome!r}",
		"Ciao {a{b}":           "una { dentro un segnaposto",
		"Ciao } fine":          "una } da sola",
		"Ciao {oref_nome":      "una { non chiusa",
		"Ciao \xff":            "un byte non valido",
	}
	for testo, dettaglio := range sintassi {
		if e := ErroreModello(FamOreficeria, testo); e != "Sintassi non valida: "+dettaglio {
			t.Errorf("%q: %q", testo, e)
		}
	}
	if e := ErroreModello(FamOreficeria, "{Gentile cliente, la sua riparazione}"); e != "Segnaposto non supportato: {…}" {
		t.Errorf("nome con spazi: %q", e)
	}
	if e := ErroreModello(FamOreficeria, "{"+strings.Repeat("a", 33)+"}"); e != "Segnaposto non supportato: {…}" {
		t.Errorf("nome lungo: %q", e)
	}
	if e := ErroreModello(FamOreficeria, "{"+strings.Repeat("a", 32)+"}"); e != "Segnaposto non supportato: {"+strings.Repeat("a", 32)+"}" {
		t.Errorf("nome di 32: %q", e)
	}
	// Prima si legge tutto il testo (la sintassi vince ovunque sia), poi i campi in ordine.
	if e := ErroreModello(FamLinkStato, "Ciao {nome}, {password} {0}"); e != "Sintassi non valida: un segnaposto posizionale {0}" {
		t.Errorf("sintassi prima: %q", e)
	}
	if e := ErroreModello(FamLinkStato, "Ciao {nome}, {password} {altro}"); e != "Segnaposto non supportato: {password}" {
		t.Errorf("ordine: %q", e)
	}
}

// inventario: CLI-33
// Il testo intorno ai segnaposto resta com'è, «{{» e «}}» sono una graffa; un valore non si rilegge
// come modello.
func TestComponiModello(t *testing.T) {
	got, err := componiTesto("{{letterale}} {nome}! }}", map[string]string{"nome": "{numero}"})
	if err != nil || got != "{letterale} {numero}! }" {
		t.Errorf("%q %v", got, err)
	}
}

// inventario: CLI-31
// Il testo in vigore: quello salvato nelle impostazioni (outgoing.template.<famiglia>, importato
// dallo standalone), altrimenti il predefinito; un testo salvato vuoto o di soli spazi vale il
// predefinito. Un testo salvato che non si compone dà l'errore con la famiglia e l'avviso, mai il
// testo e mai il predefinito al suo posto.
func TestTestoInVigore(t *testing.T) {
	b, d, _ := prova(t)
	valori := map[string]string{"nome": "Ada", "numero": "1/26", "link": "{link}", "telegram_optin": "{telegram_optin}",
		"negozio": "N", "citta": "", "orari": "", "social": "", "stima": ""}
	got, errM, err := b.componiFamiglia(ctx, FamClienteAggiornamento, valori)
	if err != nil || errM != nil || !strings.HasPrefix(got, "Buongiorno Ada, c'è stato un aggiornamento sulla Sua riparazione n° 1/26.") {
		t.Fatalf("predefinito: %q %v %v", got, errM, err)
	}
	for _, vuoto := range []string{"", "   \n"} {
		esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_cliente_aggiornamento', ?, '2026-10-31T09:00:00.000000Z')
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, vuoto)
		if got, _, _ := b.componiFamiglia(ctx, FamClienteAggiornamento, valori); !strings.HasPrefix(got, "Buongiorno Ada") {
			t.Errorf("salvato %q: %q", vuoto, got)
		}
	}
	esegui(t, d, `UPDATE impostazioni SET valore = 'Ciao {nome} {{x}}' WHERE chiave = 'outgoing.template.wa_cliente_aggiornamento'`)
	if got, _, _ := b.componiFamiglia(ctx, FamClienteAggiornamento, valori); got != "Ciao Ada {x}" {
		t.Errorf("salvato: %q", got)
	}
	esegui(t, d, `UPDATE impostazioni SET valore = 'Frase privata {password}' WHERE chiave = 'outgoing.template.wa_cliente_aggiornamento'`)
	got, errM, err = b.componiFamiglia(ctx, FamClienteAggiornamento, valori)
	if err != nil || got != "" || errM == nil || errM.Famiglia != FamClienteAggiornamento || errM.Avviso != "Segnaposto non supportato: {password}" {
		t.Errorf("non valido: %q %+v %v", got, errM, err)
	}
	if strings.Contains(errM.Avviso, "Frase privata") {
		t.Error("l'avviso porta il testo")
	}
}
