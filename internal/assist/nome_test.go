package assist

import (
	"testing"
)

// Nome del cliente e oreficeria dal dettato (slice 105; RIP-H-43, che si chiude con la 108: qui
// niente segno di copertura). Le regole sono tutte dello standalone (Assist.read_answer/3, shape/3,
// person_name/3, opening_name/2, recognise/2, match_oreficeria/2; assign_customer_pair/3 e
// assist_summary/1 di WorkshopLive): il traduttore di produzione passa «n» e «o» com'erano. Le
// tabelle vengono da test/gestrip_standalone/assist_test.exs (go-baseline), con il telefono nel
// dettato (D-104) invece che nella risposta del modello. Solo nomi e numeri inventati.

// risposta legge una risposta di prova che deve essere valida.
func risposta(t *testing.T, json string) Risposta {
	t.Helper()
	r, ok := LeggiRisposta(json)
	if !ok {
		t.Fatalf("risposta non valida: %s", json)
	}
	return r
}

// proposta è la proposta per un dettato del 23/10/2026 con il negozio detto.
func proposta(t *testing.T, json, dettato string, n Negozio) Proposta {
	t.Helper()
	return PropostaDa(risposta(t, json), dettato, giorno(t, "2026-10-23"), n)
}

// oreficerie sono le oreficerie esterne di prova, con id dal 2 in su nell'ordine detto.
func oreficerie(nomi ...string) []Oreficeria {
	var o []Oreficeria
	for i, n := range nomi {
		o = append(o, Oreficeria{ID: int64(i + 2), Nome: n})
	}
	return o
}

// «the name is a person's, never a shop's nor a placeholder» (nome_solo_persona del Flask).
func TestNomePersona(t *testing.T) {
	note := []string{"Bottari", "Bianchi Gioielli", "Gioielleria Marcon"}
	for _, c := range []struct{ nome, oreficeria, atteso string }{
		// svuotati
		{"Bottari", "Bottari", ""},
		{"bottari ", "", ""},
		{"Gioielleria Marcon", "Gioielleria Marcon", ""},
		{"Gioielleria Rossi", "", ""},
		{"Oreficeria Verdi", "", ""},
		{"Cliente", "Bottari", ""},
		{"Cliente Non Identificato", "Bottari", ""},
		{"cliente sconosciuto", "", ""},
		{"N/D", "", ""},
		{"", "Bottari", ""},
		{"   ", "", ""},
		{"Sconosciuto", "", ""},
		{"non specificato", "", ""},
		{"Cliente non indicato", "", ""},
		{"  ORÉFICERIA  Verdi", "", "ORÉFICERIA  Verdi"}, // «oréficeria» non è la parola «oreficeria»
		{"oreficeria", "", ""},
		{"GIOIELLERIA", "", ""},
		{"Oreficeria-Verdi", "", ""}, // il confine di parola dopo «oreficeria»
		{"B o t t a r i", "", ""},    // uguale a parte gli spazi
		{"Gioielleria Marcon", "", ""},
		{"gioielleriamarcon", "", ""},
		{"\uff22ottari", "", ""}, // «Ｂottari» con la B a larghezza piena: NFKD
		// tenuti
		{"Mario Rossi", "", "Mario Rossi"},
		{"Anna De Marco", "Bottari", "Anna De Marco"},
		{"Lucia Bianchi", "Bianchi Gioielli", "Lucia Bianchi"},
		{"Signor Verdi", "Bianchi Gioielli", "Signor Verdi"},
		{"Busta Cliente 7", "", "Busta Cliente 7"},
		{"Clienti Marco", "", "Clienti Marco"},
		{"Cliente Rossi", "", "Cliente Rossi"},
		{"  Mario Rossi  ", "", "Mario Rossi"},
		{"Oreficeriaè Verdi", "", "Oreficeriaè Verdi"}, // «è» è una lettera: niente confine
		{"Oreficeria2", "", "Oreficeria2"},
		{"Oreficeria_Verdi", "", "Oreficeria_Verdi"},
		{"Bottari Gioielli", "", "Bottari Gioielli"}, // uguaglianza, mai contenimento
		{"Bianchi", "", "Bianchi"},                   // dentro «Bianchi Gioielli» del negozio: resta
		{"\u180eOreficeria Verdi", "", ""},           // U+180E: non lo toglie String.trim/1, è \s di PCRE
	} {
		if got := nomePersona(c.nome, c.oreficeria, note); got != c.atteso {
			t.Errorf("nomePersona(%q, %q) = %q, atteso %q", c.nome, c.oreficeria, got, c.atteso)
		}
	}
	// Senza l'elenco del negozio conta solo l'oreficeria estratta.
	if got := nomePersona("Bottari", "", nil); got != "Bottari" {
		t.Errorf("senza elenco: %q", got)
	}
	if got := nomePersona("Bottari", "Bottari", nil); got != "" {
		t.Errorf("senza elenco, oreficeria estratta: %q", got)
	}
}

// «the name in the opening of the dictation» (nome_da_apertura del Flask, slice 192).
func TestNomeDallApertura(t *testing.T) {
	note := []string{"Bottari", "Gioielleria Marcon"}
	for _, c := range []struct{ testo, atteso string }{
		{"Cliente dalla lana.Due collane in oro giallo da sistemare chiusure.", "dalla lana"},
		{"cliente del monte, anello da allargare", "del monte"},
		{"Signora rosa fiore. Orecchini da lucidare", "rosa fiore"},
		{"Signor Da Ros, cambio pila", "Da Ros"},
		{"  Cliente   Xia ,  orologio", "Xia"},
		{"Cliente luca d'angelo.", "luca d'angelo"},
		{"Sig.ra Bassi: collana", "Bassi"},
		{"Sig. Verdi; fede da incidere", "Verdi"},
		{"Cliente da ros 347 1234567 fede da allargare", "da ros"},
		{"Cliente Mario Rossi 347 1234567 anello", "Mario Rossi"},
		{"Cliente rossi +39 347 1234567, anello", "rossi"},
		{"Cliente rossi (347) 1234567, anello", "rossi"},
		{"Cliente rossi / 347 1234567", "rossi"},
		{"Cliente rossi - 347 1234567", "rossi"},
		{"CLIENTE MARIO ROSSI, anello", "MARIO ROSSI"},
		{"Signora\u00a0Bassi, collana", "Bassi"},                         // spazio non separabile dopo l'apertura (\s di PCRE)
		{"Cliente anna\u00a0maria verdi, fede", "anna\u00a0maria verdi"}, // dentro il nome non divide
		{"Cliente rossi\u0663 anello", "rossi"},                          // una cifra araba ferma il pezzo (\d Unicode)
		{"Cliente Rossi!", "Rossi"},
		{"Cliente rossi\nanello", "rossi"},
		{"Signoramaria rossi, anello", ""}, // serve uno spazio dopo l'apertura
		// niente
		{"Cliente non presente, anello da lucidare", ""},
		{"Cliente sconosciuto. Collana", ""},
		{"Cliente busta 7, cambio pila", ""},
		{"Cliente 333 1234567, anello", ""},
		{"Cliente vuole il preventivo per la collana", ""},
		{"Cliente ha portato due anelli", ""},
		{"Cliente anello in oro da allargare", ""},
		{"Cliente di Bottari, collana da saldare", ""},
		{"Cliente Gioielleria Marcon. Fede", ""},
		{"Cliente di bòttari, collana", ""}, // l'oreficeria a parte gli accenti
		{"Cliente mario rossi detto il biondo di treviso. Anello", ""},
		{"Cliente anna maria luisa de rossi, anello", ""}, // cinque parole
		{"Cliente anna maria de rossi, anello", "anna maria de rossi"},
		{"Due collane da sistemare, cliente dalla lana", ""},
		{"Clienti Marco, anello", ""},
		{"Signori Bassi, due fedi", ""},
		{"Cliente rossi tel 347 1234567", ""},
		{"Cliente rossi telefono 347 1234567", ""},
		{"Cliente rossi cell 347 1234567", ""},
		{"Cliente rossi cellulare 347 1234567", ""},
		{"Cliente rossi n. 347", ""},
		{"Cliente rossi preventivo 50 euro", ""},
		{"Cliente Rossi È qui", ""}, // «è» anche maiuscola
		{"Cliente - , anello", ""},  // nessuna parola con una lettera
		{"", ""},
	} {
		if got := nomeDallApertura(c.testo, note); got != c.atteso {
			t.Errorf("nomeDallApertura(%q) = %q, atteso %q", c.testo, got, c.atteso)
		}
	}
	// Un'oreficeria senza nome non butta via niente.
	if got := nomeDallApertura("Cliente dalla lana, collana", []string{"", "  "}); got != "dalla lana" {
		t.Errorf("oreficeria senza nome: %q", got)
	}
}

// Il nome dell'apertura contro quello del modello, dentro la proposta (read_answer/3).
func TestNomeConApertura(t *testing.T) {
	n := Negozio{Oreficerie: oreficerie("Bottari", "Gioielleria Marcon")}
	for _, c := range []struct{ modello, testo, atteso string }{
		// vince l'apertura
		{"Lana", "Cliente dalla lana.Due collane in oro giallo da sistemare chiusure.", "Dalla Lana"},
		{"il monte", "cliente del monte, anello da allargare", "Del Monte"},
		{"Ros 347", "Cliente da ros 347 1234567 fede da allargare", "Da Ros"},
		{"Rosa Fiore", "Signora rosa fiore, orecchini da lucidare", "Rosa Fiore"},
		{"", "Cliente dalla lana. Collana da saldare", "Dalla Lana"},
		{"Rossi", "Cliente mario rossi, anello", "Mario Rossi"},
		{"Mario Rossi 3477334567", "Cliente mario rossi 347 7334567 anello", "Mario Rossi"},
		{"Rossi", "Cliente rossi +39 347 1234567, anello", "Rossi"},
		{"Da Ros", "Cliente da ros 347 1234567 fede", "Da Ros"},
		{"dell'Orto", "Cliente dell'orto, anello", "Dell'Orto"},
		{"ROSSI", "Cliente mario rossi, anello", "Mario Rossi"},
		{"dalla", "Cliente dalla lana, collana", "Dalla Lana"},    // solo particelle: come vuoto
		{"rossi ’", "Cliente mario rossi, anello", "Mario Rossi"}, // una parola senza lettere né cifre non conta
		{"Mario Rossi ’", "Cliente rossi, anello", "Mario Rossi ’"},
		// resta il modello
		{"Giulia Neri", "Cliente dalla lana, collana", "Giulia Neri"},
		{"Mario Rossi", "Cliente rossi, anello", "Mario Rossi"},
		{"Anna Verdi", "Collana da saldare per la signora Verdi", "Anna Verdi"},
		{"", "Cliente di Bottari, collana da saldare", ""},
		{"Carlo Bianchi", "Cliente non presente, anello", "Carlo Bianchi"},
	} {
		json := `{"n": ` + quota(c.modello) + `, "d": "lavoro"}`
		if got := proposta(t, json, c.testo, n).NomeCliente; got != c.atteso {
			t.Errorf("modello %q, dettato %q: nome %q, atteso %q", c.modello, c.testo, got, c.atteso)
		}
	}
}

// quota scrive s come stringa JSON (solo per testi di prova senza caratteri speciali).
func quota(s string) string { return `"` + s + `"` }

// Le maiuscole come str.title() di Python, nella forma di title_case/1 dello standalone.
func TestMaiuscole(t *testing.T) {
	for _, c := range []struct{ testo, atteso string }{
		{"", ""},
		{"   ", ""},
		{"LUCA D'ANGELO", "Luca D'Angelo"},
		{"mario rossi", "Mario Rossi"},
		{"bianchi gioielli", "Bianchi Gioielli"},
		{"  anna  de marco ", "Anna  De Marco"},
		{"maria-luisa", "Maria-Luisa"},
		{"o'neil mc3donald", "O'Neil Mc3Donald"},
		{"ÉLODIE ÇELIK", "Élodie Çelik"},
		{"nicolò", "Nicolò"},
		{"e\u0301lodie", "e\u0301Lodie"}, // grafema composto: non è una lettera sola
		{"ßtraße", "SStraße"},            // String.upcase/1 completo: «ß» → «SS»
		{"ǆemal", "Ǆemal"},               // upcase, non titlecase
		{"İSTANBUL", "İstanbul"},         // «İ» → «I» maiuscola in testa; dopo, «i̇»
		{"a\u0130", "Ai\u0307"},
		{"ΣΟΦΙΑ", "Σοφια"}, // niente sigma finale: si lavora un grafema per volta
		{"busta 45/26", "Busta 45/26"},
	} {
		if got := titolo(c.testo); got != c.atteso {
			t.Errorf("titolo(%q) = %q, atteso %q", c.testo, got, c.atteso)
		}
	}
	p := proposta(t, `{"n": "mario rossi", "o": "bianchi gioielli"}`, "anello", Negozio{})
	if p.NomeCliente != "Mario Rossi" || p.Oreficeria != "Bianchi Gioielli" {
		t.Errorf("proposta: %q, %q", p.NomeCliente, p.Oreficeria)
	}
}

// «the oreficeria the shop already knows» e la scelta della prima in ordine alfabetico.
func TestOreficeriaNota(t *testing.T) {
	for _, c := range []struct {
		nome, detta string
		orefs       []Oreficeria
		atteso      int64
	}{
		{"per nome", "bianchi gioielli", oreficerie("Bianchi Gioielli"), 2},
		{"il detto contiene il nome", "Oreficeria Bottari Snc", oreficerie("Bottari"), 2},
		{"il nome contiene il detto", "bottari", oreficerie("Oreficeria Bottari Snc"), 2},
		{"nessuno", "Nessuno Qui", oreficerie("Bottari"), 0},
		{"niente detto", "", oreficerie("Bottari"), 0},
		{"solo spazi", "   ", oreficerie("Bottari"), 0},
		{"elenco vuoto", "Bottari", nil, 0},
		{"la prima per nome", "rossi",
			[]Oreficeria{{ID: 2, Nome: "Rossi Gioielli"}, {ID: 3, Nome: "Gioielleria Rossi"}}, 3},
		{"a parità di nome la prima per id", "rossi",
			[]Oreficeria{{ID: 9, Nome: "Rossi"}, {ID: 4, Nome: "Rossi"}}, 4},
		{"ordine dei byte: le maiuscole prima", "oro",
			[]Oreficeria{{ID: 2, Nome: "oro bello"}, {ID: 3, Nome: "Oro Fino"}}, 3},
		{"accenti contano", "bòttari", oreficerie("Bottari"), 0},
		{"maiuscole Unicode", "ÉLITE", oreficerie("Élite Gioielli"), 2},
	} {
		json := `{"o": ` + quota(c.detta) + `}`
		if got := proposta(t, json, "anello", Negozio{Oreficerie: c.orefs}).OreficeriaID; got != c.atteso {
			t.Errorf("%s: oreficeria %d, attesa %d", c.nome, got, c.atteso)
		}
	}
}

// «the customer the shop already has»: solo i casi certi, e i casi di sicurezza della slice.
func TestRiconoscimentoCliente(t *testing.T) {
	type attesa struct {
		nome, telefono, correttoDa string
		corretto                   bool
	}
	mario := Cliente{ID: 1, Nome: "Mario Rossi", Telefono: "3331234567"}
	for _, c := range []struct {
		caso, json, dettato string
		n                   Negozio
		a                   attesa
	}{
		{"un telefono, un nome: corretto",
			`{"n": "mario rosi"}`, "mario rosi 333 123 4567 anello",
			Negozio{Clienti: []Cliente{mario}}, attesa{"Mario Rossi", "3331234567", "Mario Rosi", true}},
		{"telefono troppo corto dopo 0039",
			`{"n": "Carlo Bianchi"}`, "Carlo Bianchi 0039123456 anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: "123456"}}},
			attesa{"Carlo Bianchi", "0039123456", "", false}},
		{"due persone sullo stesso telefono",
			`{"n": "Mario Rosi"}`, "333 1234567 anello",
			Negozio{Clienti: []Cliente{mario, {ID: 2, Nome: "Anna Rossi", Telefono: "3331234567"}}},
			attesa{"Mario Rosi", "3331234567", "", false}},
		{"stesso telefono, stesso nome due volte: una persona",
			`{"n": "Mario Rosi"}`, "333 1234567 anello",
			Negozio{Clienti: []Cliente{mario, {ID: 2, Nome: " Mario Rossi ", Telefono: "+39 333 1234567"}}},
			attesa{"Mario Rossi", "3331234567", "Mario Rosi", true}},
		{"stesso nome scritto diverso",
			`{"n": "luca dangelo"}`, "anello da lucidare",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Luca D'Angelo", Telefono: "3339998888"}}},
			attesa{"Luca D'Angelo", "3339998888", "Luca Dangelo", true}},
		{"nome uguale a parte accenti e spazi",
			`{"n": "Nicolo  DellOrto"}`, "anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Nicolò Dell'Orto", Telefono: ""}}},
			attesa{"Nicolò Dell'Orto", "", "Nicolo  Dellorto", true}},
		{"due clienti con lo stesso nome a parte gli accenti",
			`{"n": "Nicolo Rossi"}`, "anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Nicolò Rossi", Telefono: "3330000001"}, {ID: 2, Nome: "Nicolo' Rossi", Telefono: "3330000002"}}},
			attesa{"Nicolo Rossi", "", "", false}},
		{"due omonimi: nessuna correzione, telefono del primo per nome e id (coppia dello standalone)",
			`{"n": "mario rossi"}`, "anello",
			Negozio{Clienti: []Cliente{{ID: 7, Nome: "Mario Rossi", Telefono: "3330000007"}, {ID: 3, Nome: "Mario Rossi", Telefono: "3330000003"}}},
			attesa{"Mario Rossi", "3330000003", "", false}},
		{"telefono con 39 davanti",
			`{"n": "Mario"}`, "39 333 1234567 anello",
			Negozio{Clienti: []Cliente{mario}}, attesa{"Mario Rossi", "393331234567", "Mario", true}},
		{"telefono con 0039 davanti, salvato con +39",
			`{"n": "Mario"}`, "0039 333 1234567 anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: "+39 333 123 4567"}}},
			attesa{"Mario Rossi", "3331234567", "Mario", true}},
		{"39 davanti a un numero corto resta",
			`{"n": "Mario"}`, "3912345678 anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: "12345678"}}},
			attesa{"Mario", "3912345678", "", false}},
		{"cliente senza telefono non si trova per telefono",
			`{"n": "Anna"}`, "333 1234567 anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: ""}}},
			attesa{"Anna", "3331234567", "", false}},
		{"cliente senza nome non conta",
			`{"n": "Anna"}`, "333 1234567 anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "  ", Telefono: "3331234567"}}},
			attesa{"Anna", "3331234567", "", false}},
		{"il telefono vince sul nome",
			`{"n": "Luca D'Angelo"}`, "333 1234567 anello",
			Negozio{Clienti: []Cliente{mario, {ID: 2, Nome: "Luca D'Angelo", Telefono: "3339998888"}}},
			attesa{"Mario Rossi", "3331234567", "Luca D'Angelo", true}},
		{"telefono sconosciuto: si prova il nome",
			`{"n": "luca d'angelo"}`, "333 7654321 anello",
			Negozio{Clienti: []Cliente{mario, {ID: 2, Nome: "Luca D'Angelo", Telefono: "3339998888"}}},
			attesa{"Luca D'Angelo", "3337654321", "", false}},
		{"nome che è un segnaposto e nessun telefono",
			`{"n": "Cliente"}`, "anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Cliente", Telefono: "3331234567"}}},
			attesa{"", "", "", false}},
		{"nome non simile: nessuna somiglianza approssimata",
			`{"n": "Mario Rosi"}`, "anello",
			Negozio{Clienti: []Cliente{mario}}, attesa{"Mario Rosi", "", "", false}},
		{"stesso nome a parte le maiuscole: nessuna correzione da dire, telefono del cliente",
			`{"n": "MARIO ROSSI"}`, "anello",
			Negozio{Clienti: []Cliente{mario}}, attesa{"Mario Rossi", "3331234567", "", false}},
		{"cliente salvato con 0039",
			`{"n": "Mario"}`, "333 1234567 anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: "0039 333 1234567"}}},
			attesa{"Mario Rossi", "3331234567", "Mario", true}},
		{"telefono di sette cifre",
			`{"n": "Anna"}`, "123 4567 anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Anna Verdi", Telefono: "1234567"}}},
			attesa{"Anna Verdi", "1234567", "Anna", true}},
		{"cifre arabe nel telefono (\\d Unicode)",
			`{"n": "Anna"}`, "\u0663\u0663\u0663\u0661\u0662\u0663\u0664\u0665\u0666\u0667 anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Anna Verdi", Telefono: "\u0663\u0663\u0663 \u0661\u0662\u0663\u0664\u0665\u0666\u0667"}}},
			attesa{"Anna Verdi", "\u0663\u0663\u0663\u0661\u0662\u0663\u0664\u0665\u0666\u0667", "Anna", true}},
		{"un cliente senza nome sullo stesso telefono non toglie la certezza",
			`{"n": "Anna"}`, "333 1234567 anello",
			Negozio{Clienti: []Cliente{mario, {ID: 2, Nome: "\u00a0", Telefono: "3331234567"}}},
			attesa{"Mario Rossi", "3331234567", "Anna", true}},
		{"nome svuotato: un cliente con un nome senza lettere non si trova",
			`{"n": "Cliente"}`, "anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "—", Telefono: "3331234567"}}},
			attesa{"", "", "", false}},
		// D-106 (domanda 28, Stefano 20/11/2026): con omonimi il telefono non si propone; si fa nella 108,
		// fino ad allora il Go copia lo standalone e questo caso cambia lì. Nell'ordine dei byte «MARIO ROSSI» viene prima di
		// «Mario Rossi»: il nome è lo stesso a parte le maiuscole, nessuna correzione certa (due nomi
		// scritti diversi), e la coppia prende il telefono di «MARIO ROSSI».
		{"omonimi a parte le maiuscole: telefono del primo nell'ordine dei byte",
			`{"n": "mario rossi"}`, "anello",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: "3330000001"}, {ID: 2, Nome: "MARIO ROSSI", Telefono: "3330000002"}}},
			attesa{"Mario Rossi", "3330000002", "", false}},
		{"il nome dell'apertura trova il cliente",
			`{"n": "Lana"}`, "Cliente dalla lana. Due collane da sistemare",
			Negozio{Clienti: []Cliente{{ID: 1, Nome: "Dalla Lana", Telefono: "3401112222"}}},
			attesa{"Dalla Lana", "3401112222", "", false}},
	} {
		p := proposta(t, c.json, c.dettato, c.n)
		got := attesa{p.NomeCliente, p.TelefonoCliente, p.NomeCorrettoDa, p.NomeCorretto}
		if got != c.a {
			t.Errorf("%s: %+v, atteso %+v", c.caso, got, c.a)
		}
	}
}

// «an extraction for an oreficeria with nobody named» e «a name equal to an oreficeria».
func TestOreficeriaSenzaNome(t *testing.T) {
	bottari := oreficerie("Bottari")
	for _, nome := range []string{"Bottari", "Cliente Non Identificato"} {
		p := proposta(t, `{"n": "`+nome+`", "o": "Bottari"}`, "collana da saldare per Bottari", Negozio{Oreficerie: bottari})
		if p.NomeCliente != "" || p.OreficeriaID != 2 || p.NomeCorretto {
			t.Errorf("%s: %+v", nome, p)
		}
	}
	// Una scheda cliente con il nome dell'oreficeria non lo riporta indietro.
	p := proposta(t, `{"n": "Bottari", "o": "Bottari"}`, "collana da saldare per Bottari",
		Negozio{Oreficerie: bottari, Clienti: []Cliente{{ID: 1, Nome: "Bottari", Telefono: "3330001111"}}})
	if p.NomeCliente != "" || p.OreficeriaID != 2 || p.TelefonoCliente != "" {
		t.Errorf("scheda con il nome dell'oreficeria: %+v", p)
	}
	// Prima la regola della persona, poi il riconoscimento: il telefono trova la persona e il nome
	// corretto «da» è quello svuotato, che non si annuncia.
	p = proposta(t, `{"n": "Bottari", "o": "Bottari"}`, "collana da saldare per Bottari 333 000 1111",
		Negozio{Oreficerie: bottari, Clienti: []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: "3330001111"}}})
	if p.NomeCliente != "Mario Rossi" || !p.NomeCorretto || p.NomeCorrettoDa != "" || p.Messaggio() != messaggioEstratti {
		t.Errorf("riconoscimento dopo la regola: %+v, %q", p, p.Messaggio())
	}
	// Un cognome dentro il nome di un'oreficeria resta.
	p = proposta(t, `{"n": "lucia bianchi", "o": "Bianchi Gioielli"}`, "anello di Lucia Bianchi per Bianchi Gioielli",
		Negozio{Oreficerie: oreficerie("Bianchi Gioielli")})
	if p.NomeCliente != "Lucia Bianchi" {
		t.Errorf("cognome dentro un'oreficeria: %q", p.NomeCliente)
	}
	// Un nome uguale a un'oreficeria del negozio si svuota anche senza oreficeria estratta.
	p = proposta(t, `{"n": "bianchi gioielli", "o": ""}`, "collana da saldare per Bianchi Gioielli",
		Negozio{Oreficerie: oreficerie("Bianchi Gioielli")})
	if p.NomeCliente != "" {
		t.Errorf("uguale a un'oreficeria: %q", p.NomeCliente)
	}
	// Il nome dell'apertura che è un'oreficeria del negozio resta vuoto.
	p = proposta(t, `{"n": ""}`, "Cliente di Bottari, collana da saldare", Negozio{Oreficerie: bottari})
	if p.NomeCliente != "" {
		t.Errorf("apertura con l'oreficeria: %q", p.NomeCliente)
	}
	// Le maiuscole vengono prima della regola della persona: «ßtudio» diventa «SStudio», uguale
	// all'oreficeria «Sstudio» del negozio.
	p = proposta(t, `{"n": "ßtudio"}`, "anello", Negozio{Oreficerie: oreficerie("Sstudio")})
	if p.NomeCliente != "" {
		t.Errorf("maiuscole prima della persona: %q", p.NomeCliente)
	}
	// read_answer/3 senza negozio: «BOTTARI» con l'oreficeria «bottari» estratta.
	p = proposta(t, `{"n": "BOTTARI", "o": "bottari"}`, "Bottari porta oro vecchio", Negozio{})
	if p.NomeCliente != "" || p.Oreficeria != "Bottari" || p.OreficeriaID != 0 || !p.CompraOro {
		t.Errorf("senza negozio: %+v", p)
	}
	if p := proposta(t, `{"n": "luca d'angelo"}`, "testo qualunque", Negozio{}); p.NomeCliente != "Luca D'Angelo" {
		t.Errorf("maiuscole: %q", p.NomeCliente)
	}
	if p := proposta(t, `{"n": "bianchi gioielli"}`, "testo", Negozio{}); p.NomeCliente != "Bianchi Gioielli" {
		t.Errorf("senza elenco resta: %q", p.NomeCliente)
	}
}

// Il nome come lo dà il modello, anche quando non è testo (to_string/1 dello standalone).
func TestNomeNonTesto(t *testing.T) {
	for _, c := range []struct{ json, atteso string }{
		{`{"n": 12}`, "12"},
		{`{"n": true}`, "True"},
		{`{"n": null}`, ""},
		{`{"n": 1.5}`, "1.5"},
	} {
		if got := proposta(t, c.json, "anello", Negozio{}).NomeCliente; got != c.atteso {
			t.Errorf("%s: %q, atteso %q", c.json, got, c.atteso)
		}
	}
}

// Il messaggio sotto la casella (assist_summary/1 di WorkshopLive).
func TestMessaggioProposta(t *testing.T) {
	if got := (Proposta{}).Messaggio(); got != "✓ Dati estratti. Verifica e correggi prima di salvare." {
		t.Errorf("senza correzione: %q", got)
	}
	p := Proposta{NomeCliente: "Mario Rossi", NomeCorretto: true, NomeCorrettoDa: "Mario Rosi"}
	if got := p.Messaggio(); got != "✓ Nome cliente corretto da «Mario Rosi» a «Mario Rossi». Verifica i dati prima di salvare." {
		t.Errorf("con correzione: %q", got)
	}
	// Un nome con «%s» resta al suo posto.
	p = Proposta{NomeCliente: "Mario %s Rossi", NomeCorretto: true, NomeCorrettoDa: "Mario %s Rosi\u0301"}
	if got := p.Messaggio(); got != "✓ Nome cliente corretto da «Mario %s Rosi\u0301» a «Mario %s Rossi». Verifica i dati prima di salvare." {
		t.Errorf("con «%%s» nel nome: %q", got)
	}
}

// La coppia nome/telefono del modulo (assign_customer_pair/3): vince il telefono dettato.
func TestCoppiaNomeTelefono(t *testing.T) {
	clienti := []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: "3331234567"}}
	p := proposta(t, `{"n": "Mario Rossi"}`, "Mario Rossi 347 7654321 anello", Negozio{Clienti: clienti})
	if p.NomeCliente != "Mario Rossi" || p.TelefonoCliente != "3477654321" || p.NomeCorretto {
		t.Errorf("telefono dettato: %+v", p)
	}
	p = proposta(t, `{"n": "Anna Verdi"}`, "anello", Negozio{Clienti: clienti})
	if p.TelefonoCliente != "" {
		t.Errorf("nessun cliente con quel nome: %q", p.TelefonoCliente)
	}
	p = proposta(t, `{"n": ""}`, "anello", Negozio{Clienti: []Cliente{{ID: 1, Nome: "", Telefono: "3331234567"}}})
	if p.TelefonoCliente != "" {
		t.Errorf("nome vuoto: %q", p.TelefonoCliente)
	}
}

// Estrai passa il negozio alla proposta.
func TestEstraiConNegozio(t *testing.T) {
	chiedi := func() (string, error) { return `{"n": "mario rosi", "o": "bottari"}`, nil }
	n := Negozio{Oreficerie: oreficerie("Bottari"), Clienti: []Cliente{{ID: 1, Nome: "Mario Rossi", Telefono: "3331234567"}}}
	p, err := Estrai(chiedi, "anello 333 1234567", giorno(t, "2026-10-23"), n)
	if err != nil || p.OreficeriaID != 2 || p.NomeCliente != "Mario Rossi" || p.NomeCorrettoDa != "Mario Rosi" {
		t.Errorf("Estrai: %+v, %v", p, err)
	}
}
