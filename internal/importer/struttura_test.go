package importer

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// traduzioneDi dà la traduzione di ogni colonna Go mappata, come «tabella.colonna».
func traduzioneDi() map[string]Traduzione {
	m := map[string]Traduzione{}
	for _, tab := range Mappa {
		for _, c := range tab.Colonne {
			if tab.Go != "" && c.Go != "" {
				m[tab.Go+"."+c.Go] = c.Traduzione
			}
		}
	}
	return m
}

// Ogni tabella dello standalone a go-baseline ha una destinazione nel Go o è scartata con il
// motivo, e così ogni sua colonna: una tabella o una colonna che la mappa non conosce non può
// sparire in silenzio. La mappa non nomina tabelle o colonne che lo standalone non ha.
func TestOgniTabellaDelloStandaloneHaUnaDestinazione(t *testing.T) {
	sa := schemaStandalone(t)
	viste := map[string]int{}
	for _, tab := range Mappa {
		if _, ok := sa.colonne[tab.Standalone]; !ok {
			t.Errorf("la mappa nomina %s, che lo standalone non ha", tab.Standalone)
			continue
		}
		viste[tab.Standalone]++
		if tab.Go == "" {
			if len(tab.Nota) < 20 || len(tab.Colonne) > 0 {
				t.Errorf("%s scartata senza un motivo (o con colonne): %q", tab.Standalone, tab.Nota)
			}
			continue
		}
		mappate := map[string]bool{}
		for _, c := range tab.Colonne {
			if mappate[c.Standalone] {
				t.Errorf("%s.%s mappata due volte", tab.Standalone, c.Standalone)
			}
			mappate[c.Standalone] = true
			if _, ok := sa.colonna(tab.Standalone, c.Standalone); !ok {
				t.Errorf("la mappa nomina %s.%s, che lo standalone non ha", tab.Standalone, c.Standalone)
			}
			if c.Go == "" && len(c.Nota) < 20 {
				t.Errorf("%s.%s scartata senza un motivo", tab.Standalone, c.Standalone)
			}
			if c.Traduzione == "" {
				t.Errorf("%s.%s senza traduzione", tab.Standalone, c.Standalone)
			}
		}
		for _, x := range sa.colonne[tab.Standalone] {
			if !mappate[x.nome] {
				t.Errorf("%s.%s né mappata né scartata (verso %s)", tab.Standalone, x.nome, tab.Go)
			}
		}
	}
	for _, nome := range chiavi(sa.colonne) {
		if viste[nome] == 0 {
			t.Errorf("tabella %s dello standalone né mappata né scartata", nome)
		}
	}
	for _, tab := range Mappa {
		if viste[tab.Standalone] > 1 && tab.Righe == "" {
			t.Errorf("%s va in %d tabelle: la destinazione %s non dice quali righe", tab.Standalone, viste[tab.Standalone], tab.Go)
		}
	}
}

// Ogni colonna mappata esiste nella sua tabella Go con lo stesso tipo di SQLite, lo stesso NOT
// NULL (o chiave primaria), la stessa chiave primaria e lo stesso DEFAULT (tradotto); ogni tabella
// e ogni colonna del Go viene da una colonna dello standalone o dice perché è solo del Go.
func TestLeColonneMappateEsistonoNelGo(t *testing.T) {
	sa := schemaStandalone(t)
	g := schemaGo(t, migrato(t))
	destinazioni := map[string]map[string]bool{}
	for _, tab := range Mappa {
		if tab.Go == "" {
			continue
		}
		if _, ok := g.colonne[tab.Go]; !ok {
			t.Errorf("%s → %s: la tabella Go non esiste", tab.Standalone, tab.Go)
			continue
		}
		if destinazioni[tab.Go] == nil {
			destinazioni[tab.Go] = map[string]bool{}
		}
		for _, c := range tab.Colonne {
			s, ok := sa.colonna(tab.Standalone, c.Standalone)
			if c.Go == "" || !ok {
				continue
			}
			destinazioni[tab.Go][c.Go] = true
			x, ok := g.colonna(tab.Go, c.Go)
			da := tab.Standalone + "." + c.Standalone + " → " + tab.Go + "." + c.Go
			switch {
			case !ok:
				t.Errorf("%s: la colonna Go non esiste", da)
				continue
			case x.tipo != s.tipo:
				t.Errorf("%s: tipo %s nello standalone, %s nel Go", da, s.tipo, x.tipo)
			case x.nonNulla != s.nonNulla:
				t.Errorf("%s: NOT NULL %v nello standalone, %v nel Go", da, s.nonNulla, x.nonNulla)
			case x.chiave != s.chiave:
				t.Errorf("%s: chiave primaria %v nello standalone, %v nel Go", da, s.chiave, x.chiave)
			}
			if want := norma(traduci(tab.Standalone, tab.Go, s.predefinito)); norma(x.predefinito) != want {
				t.Errorf("%s: DEFAULT %q nello standalone, %q nel Go", da, s.predefinito, x.predefinito)
			}
		}
	}
	for _, tab := range chiavi(g.colonne) {
		if destinazioni[tab] == nil {
			if motivo := TabelleSoloGo[tab]; len(motivo) < 20 {
				t.Errorf("tabella Go %s senza una sorgente nello standalone né un motivo", tab)
			}
			continue
		}
		for _, c := range g.colonne[tab] {
			if !destinazioni[tab][c.nome] && len(ColonneSoloGo[tab][c.nome]) < 20 {
				t.Errorf("colonna Go %s.%s senza una sorgente nello standalone né un motivo", tab, c.nome)
			}
		}
	}
	for tab := range TabelleSoloGo {
		if _, ok := g.colonne[tab]; !ok || destinazioni[tab] != nil {
			t.Errorf("TabelleSoloGo nomina %s, che non esiste o ha una sorgente", tab)
		}
	}
	for tab, cols := range ColonneSoloGo {
		for col := range cols {
			if _, ok := g.colonna(tab, col); !ok || destinazioni[tab][col] {
				t.Errorf("ColonneSoloGo nomina %s.%s, che non esiste o ha una sorgente", tab, col)
			}
		}
	}
}

// Le chiavi esterne dello standalone esistono nel Go, tra le tabelle e le colonne tradotte, con
// la stessa azione di ON DELETE; il Go non ne ha altre.
func TestChiaviEsterneComeLoStandalone(t *testing.T) {
	sa := schemaStandalone(t)
	g := schemaGo(t, migrato(t))
	attese := map[string]bool{}
	for _, e := range sa.esterne {
		for gtab, cols := range colonneDi(e.tab) {
			for padre, pcols := range colonneDi(e.padre) {
				attese[gtab+"."+cols[e.col]+" -> "+padre+"("+pcols[e.a]+") "+e.azione] = true
			}
		}
	}
	trovate := map[string]bool{}
	for _, e := range g.esterne {
		k := e.tab + "." + e.col + " -> " + e.padre + "(" + e.a + ") " + e.azione
		trovate[k] = true
		if !attese[k] {
			t.Errorf("chiave esterna nel Go senza l'uguale nello standalone: %s", k)
		}
	}
	for _, k := range chiavi(attese) {
		if !trovate[k] {
			t.Errorf("chiave esterna dello standalone mancante nel Go: %s", k)
		}
	}
}

func chiaveIndice(tab string, cols []string, ix indiceSchema) string {
	k := tab + "(" + strings.Join(cols, ",") + ")"
	if ix.unico {
		k += " unico"
	}
	if ix.parziale {
		k += " parziale"
	}
	return k
}

// Gli indici dello standalone, unici e no, esistono nel Go sulle colonne tradotte, unici e parziali
// dove lo sono, con la stessa condizione (tradotta) se sono parziali; il Go non ne ha altri senza
// un motivo.
func TestIndiciComeLoStandalone(t *testing.T) {
	sa := schemaStandalone(t)
	g := schemaGo(t, migrato(t))
	nelGo := map[string]indiceSchema{}
	for _, ix := range g.indici {
		nelGo[chiaveIndice(ix.tab, ix.cols, ix)] = ix
	}
	attesi := map[string]bool{}
	for _, ix := range sa.indici {
		for gtab, cols := range colonneDi(ix.tab) {
			var gc []string
			for _, c := range ix.cols {
				gc = append(gc, cols[c])
			}
			k := chiaveIndice(gtab, gc, ix)
			attesi[k] = true
			x, ok := nelGo[k]
			switch {
			case !ok:
				t.Errorf("indice dello standalone %s mancante nel Go: %s", chiaveIndice(ix.tab, ix.cols, ix), k)
			case ix.parziale && norma(traduci(ix.tab, gtab, ix.where)) != norma(x.where):
				t.Errorf("%s: condizione %q nello standalone, %q nel Go", k, ix.where, x.where)
			}
		}
	}
	for k := range nelGo {
		if !attesi[k] && len(IndiciSoloGo[k]) < 20 {
			t.Errorf("indice nel Go senza l'uguale nello standalone né un motivo: %s", k)
		}
	}
}

// Ogni trigger dello standalone ha una destinazione detta: lo stesso trigger nel Go (che esiste),
// un CHECK della tabella Go o il rinvio alla slice della sua area. Il Go non ha trigger senza
// sorgente.
func TestTriggerDelloStandalone(t *testing.T) {
	sa := schemaStandalone(t)
	g := schemaGo(t, migrato(t))
	usati := map[string]bool{}
	for _, nome := range chiavi(sa.trigger) {
		dest, ok := Trigger[nome]
		switch {
		case !ok:
			t.Errorf("trigger %s dello standalone senza destinazione", nome)
		case len(dest.Nota) < 20:
			t.Errorf("trigger %s: manca il perché", nome)
		case dest.Go != "" && g.trigger[dest.Go] == "":
			t.Errorf("trigger %s → %s: nel Go non esiste", nome, dest.Go)
		}
		usati[dest.Go] = true
	}
	for nome := range Trigger {
		if sa.trigger[nome] == "" {
			t.Errorf("Trigger nomina %s, che lo standalone non ha", nome)
		}
	}
	for n := range g.trigger {
		if !usati[n] {
			t.Errorf("trigger %s nel Go senza sorgente nello standalone", n)
		}
	}
}

// Ogni CHECK dello standalone, con i nomi del Go, sta in un CHECK della tabella Go: elenchi,
// intervalli (BETWEEN), regole tra colonne. Il confronto è sul testo senza spazi né virgolette.
func TestCheckDelloStandaloneNelGo(t *testing.T) {
	sa := schemaStandalone(t)
	g := schemaGo(t, migrato(t))
	for _, tab := range chiavi(sa.sqlTab) {
		for gtab := range colonneDi(tab) {
			var nelGo []string
			for _, c := range controlli(g.sqlTab[gtab]) {
				nelGo = append(nelGo, norma(c))
			}
			for _, c := range controlli(sa.sqlTab[tab]) {
				want := norma(traduci(tab, gtab, c))
				if !slices.ContainsFunc(nelGo, func(x string) bool { return strings.Contains(x, want) }) {
					t.Errorf("%s → %s: CHECK dello standalone mancante nel Go: %s", tab, gtab, traduci(tab, gtab, c))
				}
			}
		}
	}
}

// Gli elenchi chiusi di valori nei CHECK del Go sono quelli dello standalone (ricavati dal suo
// DDL, con i valori tradotti), né più né meno; un elenco che lo standalone non ha nel database
// deve avere il suo perché.
func TestGliElenchiDiValoriSonoQuelliDelloStandalone(t *testing.T) {
	sa := schemaStandalone(t)
	g := schemaGo(t, migrato(t))
	attesi := map[string][]string{}
	for _, tab := range chiavi(sa.sqlTab) {
		for gtab, cols := range colonneDi(tab) {
			var tradotti []string
			for _, c := range controlli(sa.sqlTab[tab]) {
				tradotti = append(tradotti, traduci(tab, gtab, c))
			}
			for col, v := range elenchi(tradotti) {
				if slices.Contains(chiavi(cols), col) || slices.ContainsFunc(chiavi(cols), func(s string) bool { return cols[s] == col }) {
					attesi[gtab+"."+col] = chiavi(v)
				}
			}
		}
	}
	trovati := map[string]bool{}
	for _, gtab := range chiavi(g.sqlTab) {
		for col, v := range elenchi(controlli(g.sqlTab[gtab])) {
			k := gtab + "." + col
			trovati[k] = true
			want, ok := attesi[k]
			switch {
			case !ok && (ColonneSoloGo[gtab][col] != "" || TabelleSoloGo[gtab] != "" || len(ElenchiSoloGo[k]) >= 20):
			case !ok:
				t.Errorf("%s ha nel Go l'elenco %s, che lo standalone non ha", k, elenco(chiavi(v)))
			case !slices.Equal(chiavi(v), want):
				t.Errorf("%s: nel Go %s, nello standalone %s", k, elenco(chiavi(v)), elenco(want))
			}
		}
	}
	for k := range attesi {
		if !trovati[k] {
			t.Errorf("%s: lo standalone ha un elenco di valori che il Go non controlla", k)
		}
	}
	for k := range ElenchiSoloGo {
		if !trovati[k] {
			t.Errorf("ElenchiSoloGo nomina %s, che nel Go non ha un elenco", k)
		}
	}
}

// senzaCheckDellaForma sono le colonne di una tabella Go più vecchia di questa slice che non hanno
// il CHECK della loro forma, con il perché. Aggiungerlo vuol dire ricostruire la tabella: lo farà
// una slice futura (deciso da Stefano, 30/10/2026).
var senzaCheckDellaForma = map[string]string{
	"impostazioni.aggiornata_il": "0001 nata senza il CHECK dell'istante; l'unico che la scrive (audit.Conservazione) usa db.FormatoIstante",
}

// Ogni colonna tradotta come istante, data, booleano o JSON ha nel Go il CHECK della sua forma
// (istante in db.FormatoIstante, data AAAA-MM-GG, booleano 0 o 1, JSON valido; il ruolo con i nomi
// del Go), e al contrario ogni colonna Go con il CHECK di una di queste forme ha nella mappa la
// traduzione che le corrisponde.
func TestOgniColonnaHaIlCheckDellaSuaForma(t *testing.T) {
	g := schemaGo(t, migrato(t))
	forme := []struct {
		tr Traduzione
		re func(col string) string
	}{
		{Istante, func(c string) string { return `(?:^|[^a-z0-9_])` + c + ` GLOB ` + regexp.QuoteMeta(formaIstante) }},
		{Data, func(c string) string { return `(?:^|[^a-z0-9_])` + c + ` GLOB ` + regexp.QuoteMeta(formaData) }},
		{Booleano, func(c string) string { return `(?:^|[^a-z0-9_])` + c + ` IN \(0, 1\)` }},
		{JSON, func(c string) string { return `json_valid\(` + c + `\)` }},
		{Ruolo, func(c string) string { return `(?:^|[^a-z0-9_])` + c + ` IN \('titolare', 'dipendente'\)` }},
	}
	tr := traduzioneDi()
	for _, tab := range chiavi(g.sqlTab) {
		for _, c := range g.colonne[tab] {
			k := tab + "." + c.nome
			for _, f := range forme {
				ha := regexp.MustCompile(f.re(regexp.QuoteMeta(c.nome))).MatchString(g.sqlTab[tab])
				vuole := tr[k] == f.tr
				switch {
				case vuole && !ha && senzaCheckDellaForma[k] == "":
					t.Errorf("%s (%s): manca il CHECK della forma", k, f.tr)
				case ha && !vuole && TabelleSoloGo[tab] == "" && ColonneSoloGo[tab][c.nome] == "":
					t.Errorf("%s ha il CHECK %s, ma nella mappa la traduzione è %q", k, f.tr, tr[k])
				}
			}
		}
	}
}

// Il nome Go di ogni colonna è fatto delle parole che traducono il nome dello standalone
// (paroleNomi) e ognuna di quelle parole, se non è facoltativa, vi compare: così uno scambio tra
// due colonne simili (note pubbliche e interne, città e provincia, imponibile e IVA, l'istante e
// la sua lettura) si vede.
func TestINomiGoTraduconoQuelliDelloStandalone(t *testing.T) {
	for _, tab := range Mappa {
		for _, c := range tab.Colonne {
			if c.Go == "" {
				continue
			}
			permesse := map[string]bool{}
			for _, w := range strings.Split(c.Standalone, "_") {
				tr, ok := paroleNomi[w]
				if !ok {
					t.Errorf("%s.%s: la parola %q non è in paroleNomi", tab.Standalone, c.Standalone, w)
					continue
				}
				for _, x := range tr {
					permesse[x] = true
				}
				if len(tr) > 0 && !paroleFacoltative[w] && !slices.ContainsFunc(strings.Split(c.Go, "_"), func(g string) bool { return slices.Contains(tr, g) }) {
					t.Errorf("%s.%s → %s: nessuna traduzione di %q (%s)", tab.Standalone, c.Standalone, c.Go, w, elenco(tr))
				}
			}
			for _, w := range strings.Split(c.Go, "_") {
				if !permesse[w] {
					t.Errorf("%s.%s → %s: la parola %q non traduce il nome dello standalone", tab.Standalone, c.Standalone, c.Go, w)
				}
			}
		}
	}
}
