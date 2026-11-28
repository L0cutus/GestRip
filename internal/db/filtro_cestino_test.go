package db

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Il filtro del cestino (slice 136, incongruenza 1 del piano T4.11; RIP-S-50). Il PIANO §1.4 voleva un filtro «vivo»
// unico in internal/db; nel codice il cestino si esclude scrivendo a mano `cancellata_il IS NULL` in ogni query sulle
// buste. Questo test statico tiene onesta quella scelta.
//
// Come legge: il sorgente Go (non i test) di internal/ e cmd/. Un «pezzo SQL» è una stringa o una concatenazione con +
// che contiene almeno una stringa. Nel pezzo si contano i riferimenti alla tabella `buste` (FROM, JOIN, UPDATE, DELETE
// FROM; non INSERT INTO: una busta nuova non è nel cestino) e i filtri `cancellata_il IS NULL` (con o senza alias). Gli
// operandi che non sono stringhe si risolvono così:
//   - una variabile locale: il testo della sua assegnazione con meno filtri (un `cestino := "…IS NULL"` che un ramo
//     cambia in "1" vale "1");
//   - un parametro: il testo dell'argomento con meno filtri fra tutte le chiamate del pacchetto;
//   - una costante o variabile del pacchetto: tutte le stringhe del suo valore (anche di una funzione che lo calcola);
//   - una chiamata a una funzione del pacchetto (`where, arg, err := b.dove(…)`): il suo risultato, cioè i filtri del
//     return con meno (senza i return d'errore, "" o nil), risolto nella sua funzione; strings.Join(x, …) è x, append e
//     []string{…} sono tutto quello che contengono, e una lista che cresce con append conta tutti i suoi pezzi;
//   - il resto (Sprintf, Repeat…) non si conta.
//
// Le funzioni che ricevono il WHERE da chi chiama (funzioniConWhere: leggiBustaEmail, paginaDove…) hanno la loro query
// fra le eccezioni; per loro si controlla ogni chiamata, e una chiamata senza filtro va nell'elenco con il suo chiamante.
//
// Un pezzo con meno filtri che riferimenti è senza filtro e deve stare in eccezioniFiltroCestino, con il numero esatto
// di pezzi della funzione e il motivo. Una query nuova che dimentica il filtro fa fallire il test; anche un'eccezione
// che non serve più (il numero non torna), così l'elenco resta vero. Limiti, voluti: si legge solo il testo SQL scritto
// nel sorgente; un filtro scritto dove il test non lo risolve conta come mancante (e serve un'eccezione); un filtro
// scritto in un ramo che non si esegue o in una lista che poi non si usa conta come presente.

// eccezioneCestino è una funzione (o una dichiarazione del pacchetto) con pezzi SQL sulle buste senza il filtro, voluti.
type eccezioneCestino struct {
	pezzi  int
	motivo string
}

// I motivi delle eccezioni ripetuti.
const (
	// La busta è già stata letta (o presa con una scrittura condizionata) fuori dal cestino nella stessa transazione, o
	// appena prima nella stessa richiesta; la query dopo la riprende per id. Il cestinamento (buste.SpostaNelCestino)
	// scrive nella sua transazione e SQLite ne fa una sola alla volta.
	motivoGiaLetta = "busta già letta fuori dal cestino nella stessa transazione o appena prima; la query la riprende per id"
	// Un frammento di SQL (colonne e FROM) che chi lo usa completa con il WHERE: le query complete si controllano lì.
	motivoFrammento = "frammento (colonne e FROM): il filtro lo aggiunge ogni query che lo usa, controllata lì"
	// RIP-S-50: i controlli che decidono se si può cancellare un genitore contano anche le buste nel cestino, per non
	// rompere un ripristino futuro (with_discarded dello standalone).
	motivoGenitore = "RIP-S-50: un genitore (artigiano, consegna, lotto) con una busta nel cestino non si cancella, per il ripristino"
	// L'import porta anche le buste nel cestino (OPS-03, decisione del 31/10/2026) e le riscrive tutte.
	motivoImport = "import: porta e riscrive anche le buste nel cestino (OPS-03, D-77, D-120)"
	// Un numero usato da una busta nel cestino resta occupato (indice unico su buste.numero).
	motivoNumeri = "numerazione: un numero usato da una busta nel cestino resta occupato (indice unico su numero)"
)

// eccezioniFiltroCestino: chiave «percorso/dal/repo.go:Funzione» (o «:Tipo.Metodo», «:const nome», «:var nome»).
var eccezioniFiltroCestino = map[string]eccezioneCestino{
	"internal/backup/conferma.go:messaggiInCoda":                    {1, "il numero della busta accanto a un messaggio già in coda, nella conferma del ripristino (solo titolare): il messaggio esiste anche con la busta nel cestino"},
	"internal/buste/artigiani.go:Buste.EliminaArtigiano":            {1, motivoGenitore},
	"internal/buste/attesa.go:Buste.Attesa":                         {1, motivoGiaLetta},
	"internal/buste/avvisi.go:Buste.AzzeraAvviso":                   {1, motivoGiaLetta},
	"internal/buste/avvisi.go:Buste.garantisciLinkIn":               {1, motivoGiaLetta},
	"internal/buste/cestino_pagina.go:Buste.busteNelCestino":        {1, "la pagina del cestino (slice 137, RIP-S-51): legge proprio le buste nel cestino (cancellata_il IS NOT NULL, discarded/1)"},
	"internal/buste/cestino_pagina.go:const sqlRipristinoBusta":     {1, "il ripristino (slice 137, RIP-S-52): la guardia è cancellata_il IS NOT NULL, la busta deve essere nel cestino"},
	"internal/buste/consegne.go:aggiungibile":                       {1, "controllo dello stato su un id già trovato fuori dal cestino (bustaPerNumero, AggiungiAllaConsegna, AggiungiAlLotto)"},
	"internal/buste/consegne.go:Buste.AggiungiAllaConsegna":         {1, motivoGiaLetta},
	"internal/buste/consegne.go:Buste.ApriConsegna":                 {1, "condInRaccolta: una consegna con una busta uscita, anche nel cestino, non è più in raccolta (confirmed_handover_ids/0)"},
	"internal/buste/consegne.go:Buste.ConsegneInRaccolta":           {1, "condInRaccolta (come ApriConsegna); il conteggio delle buste della consegna ha il filtro"},
	"internal/buste/consegne.go:const condInRaccolta":               {1, "una consegna con una busta uscita, anche nel cestino, non è più in raccolta (confirmed_handover_ids/0)"},
	"internal/buste/consegne.go:Buste.bustaPerNumeroScelta":         {1, "conCestino: l'esclusione dalla raccolta dei dettati cerca anche nel cestino (D-109, with_discarded); gli altri chiamanti passano il filtro"},
	"internal/buste/consegne.go:Buste.PulisciConsegneVuote":         {1, motivoGenitore},
	"internal/buste/contatore.go:massimoDellAnno":                   {1, motivoNumeri},
	"internal/buste/conversazione.go:Buste.aggiungiMessaggio":       {1, motivoGiaLetta},
	"internal/buste/email_cliente.go:Buste.chiediEmail":             {1, motivoGiaLetta},
	"internal/buste/email_cliente.go:Buste.ConfermaEmail":           {1, motivoGiaLetta},
	"internal/buste/email_cliente.go:Buste.linkStabile":             {1, motivoGiaLetta},
	"internal/buste/email_cliente.go:Buste.MandaLinkDisattivazione": {1, "D-02: il link di disattivazione arriva anche per una busta nel cestino (un ripristino non riporta un consenso ritirato)"},
	"internal/buste/email_cliente.go:Buste.togliEmail":              {1, motivoGiaLetta},
	"internal/buste/email_cliente.go:const colonneBustaEmail":       {1, motivoFrammento},
	"internal/buste/email_cliente.go:leggiBustaEmail":               {1, "il WHERE lo scrive chi chiama: con il filtro le pagine e le richieste; senza solo emailDellaScheda (scheda già letta fuori dal cestino) e le disattivazioni (anche nel cestino, D-02)"},
	"internal/buste/email_pronta.go:Buste.mancataPronta":            {1, motivoGiaLetta},
	"internal/buste/email_pronta.go:Buste.notificaPronta":           {1, motivoGiaLetta},
	"internal/buste/email_pronta.go:giaDetto":                       {1, motivoGiaLetta},
	"internal/buste/email_pronta.go:statoPronta":                    {1, motivoGiaLetta},
	"internal/buste/esporta_dettati.go:Buste.dettatiDaEsportare":    {1, "esportazione dei dettati (RIP-H-52): legge anche le buste nel cestino per contarle e scartarle"},
	"internal/buste/firma.go:Buste.scriviFirma":                     {1, motivoGiaLetta},
	"internal/buste/fissa.go:Buste.CambiaFissata":                   {1, motivoGiaLetta},
	"internal/buste/giri.go:Buste.GiriDellArtigiano":                {1, "i giri della scheda già aperta (segni.go): la busta è letta fuori dal cestino prima"},
	"internal/buste/giri.go:const sqlGiriAperti":                    {1, "ricostruzione dei giri alla migrazione: anche le buste nel cestino hanno la loro storia (per il ripristino)"},
	"internal/buste/giri.go:const sqlRicostruzioneRientri":          {1, "ricostruzione dei giri alla migrazione (sqlRientriNeiGiri)"},
	"internal/buste/giri.go:const sqlRientriNeiGiri":                {1, "giri chiusi dalle righe di rientro: anche le buste nel cestino hanno la loro storia (per il ripristino)"},
	"internal/buste/giri.go:rientroNeiGiri":                         {1, "il giro del rientro appena scritto, nella transazione del rientro (busta già letta fuori dal cestino)"},
	"internal/buste/link.go:Buste.NuovoLink":                        {1, motivoGiaLetta},
	"internal/buste/link.go:Buste.RevocaLink":                       {1, motivoGiaLetta},
	"internal/buste/link.go:Buste.tagliaEmail":                      {2, motivoGiaLetta},
	"internal/buste/lotti.go:Buste.PulisciLottiVuoti":               {1, motivoGenitore},
	"internal/buste/lotto_buste.go:Buste.AggiungiAlLotto":           {1, motivoGiaLetta},
	"internal/buste/lotto_buste.go:Buste.TogliDalLotto":             {1, motivoGiaLetta},
	"internal/buste/lotto_invio.go:Buste.InviaLotto":                {1, "RIP-S-50: una busta nel cestino di un'altra oreficeria blocca comunque l'invio del lotto (il ripristino la riporterebbe)"},
	"internal/buste/lotto_invio.go:rifiutoInvio":                    {1, "il motivo del rifiuto di InviaLotto, cestino compreso (send_refusal/2)"},
	"internal/buste/lotto_invio.go:rifiutoMembro":                   {1, "D-99: il numero visibile della busta del rifiuto si legge anche dal cestino"},
	"internal/buste/lotto_pubblico.go:Buste.chiudiAllaLettura":      {1, "RIP-S-50: una busta nel cestino non ritirata tiene aperto il lotto (close_if_complete/2)"},
	"internal/buste/modifica.go:aggiorna":                           {1, motivoGiaLetta},
	"internal/buste/modifica.go:Buste.revocaPerControparte":         {2, motivoGiaLetta},
	"internal/buste/preventivo.go:Buste.RevocaRisposta":             {1, motivoGiaLetta},
	"internal/buste/preventivo.go:Buste.rispondi":                   {2, motivoGiaLetta},
	"internal/buste/qr.go:Buste.rientroAutomatico":                  {2, motivoGiaLetta},
	"internal/buste/raccolta_pagina.go:Buste.bustaDellaRaccolta":    {1, "D-109: l'esclusione dalla raccolta cerca anche nel cestino (with_discarded); la riga è quella appena trovata"},
	"internal/buste/raccolta_pagina.go:Buste.escludiCliente":        {1, "l'opposizione cancella i dettati di tutte le buste del cliente, cestino compreso"},
	"internal/buste/raccolta_scheda.go:Buste.ScadenzaDettati":       {1, "cestino: il giro giornaliero cancella i dettati delle buste nel cestino (expire/1, slice 136)"},
	"internal/buste/riapertura.go:Buste.Riapri":                     {1, motivoGiaLetta},
	"internal/buste/ricerca.go:righeDellEtichettaCon":               {1, "il filtro lo passa chi chiama; «1» solo per l'esclusione dalla raccolta (D-109, with_discarded)"},
	"internal/buste/rientro.go:Buste.RientraDallArtigiano":          {2, motivoGiaLetta},
	"internal/buste/scheda.go:Buste.leggiScheda":                    {3, motivoGiaLetta},
	"internal/buste/solleciti_giro.go:TimbraSollecitiDelGiro":       {1, "il timbro sulle buste appena lette con il filtro (busteInRitardoDove) nella stessa transazione"},
	"internal/buste/sollecito_artigiano.go:leggiSollecito":          {1, "l'artigiano della busta appena letta fuori dal cestino (leggiAvviso)"},
	"internal/buste/stati.go:Buste.passaStato":                      {1, motivoGiaLetta},
	"internal/buste/stati.go:chiudiLottoCompleto":                   {1, "RIP-S-50: una busta nel cestino non ritirata tiene aperto il lotto (close_after_delivery/2)"},
	"internal/buste/storica.go:Buste.bustaStoricaDove":              {1, "link storici: legge cancellata_il e una busta nel cestino ferma la ricerca con il 404 (slice 064, 135)"},
	"internal/buste/storico.go:Buste.EliminaDalloStorico":           {1, "lo sgancio dalla consegna eliminata vale anche per le buste nel cestino (with_discarded, workshop.ex); il conto mostrato ha il filtro"},
	"internal/buste/telegram_avvisi.go:Buste.avvisaMessaggio":       {1, motivoGiaLetta},
	"internal/buste/telegram_bot.go:Buste.ConsensoTelegram":         {1, "iscrizione non fermata: il trigger della 0014 ferma le iscrizioni della busta che va nel cestino"},
	"internal/buste/telegram_bot.go:Buste.FermaChatTelegram":        {1, "lo stop della chat spegne le iscrizioni rimaste; il numero è solo per l'audit"},
	"internal/importer/riscritture.go:copiatore.codiciNormalizzati": {2, motivoImport},
	"internal/importer/riscritture.go:copiatore.numeriSenzaZeri":    {2, motivoImport},
	"internal/importer/riscritture.go:copiatore.riscriviBuste":      {1, motivoImport},
	"internal/uscita/coda.go:Coda.bloccaChat":                       {1, "il blocco della chat spegne le iscrizioni rimaste; il numero è solo per l'audit"},
	"internal/uscita/pagina_smtp.go:Coda.righe":                     {1, "il numero della busta accanto ai messaggi già in coda, nella pagina dei provider (solo titolare)"},
	// Le chiamate senza filtro alle funzioni di funzioniConWhere, una per chiamante.
	"internal/buste/consegne.go:Buste.bustaPerNumeroScelta→righeDellEtichettaCon":     {1, "conCestino: solo l'esclusione dalla raccolta cerca anche nel cestino (D-109, with_discarded); altrimenti il filtro"},
	"internal/buste/email_cliente.go:Buste.DisattivaConLink→leggiBustaEmail":          {1, "D-02: il «Disattiva» del link vale anche con la busta nel cestino (un ripristino non riporta un consenso ritirato)"},
	"internal/buste/email_cliente.go:Buste.mandaRipiego→leggiBustaEmail":              {1, "D-02: il link di disattivazione nuovo arriva anche per una busta nel cestino"},
	"internal/buste/email_pronta.go:Buste.emailDellaScheda→leggiBustaEmail":           {1, "il riquadro email della scheda, già letta fuori dal cestino (leggiScheda)"},
	"internal/buste/raccolta_pagina.go:Buste.bustaDellaRaccolta→bustaPerNumeroScelta": {1, "D-109: l'esclusione dalla raccolta cerca anche nel cestino (with_discarded)"},
	"internal/buste/storica.go:Buste.cercaPerSlug→bustaStoricaDove":                   {3, "link storici: bustaStoricaDove legge cancellata_il e una busta nel cestino ferma la ricerca con il 404"},
	"internal/buste/storica.go:Buste.cercaStorica→bustaStoricaDove":                   {2, "link storici: bustaStoricaDove legge cancellata_il e una busta nel cestino ferma la ricerca con il 404"},
}

var (
	reRiferimentoBuste = regexp.MustCompile(`(?i)\b(?:from|join|update)\s+buste\b`)
	reFiltroCestino    = regexp.MustCompile(`(?i)\b(?:[a-z_][a-z0-9_]*\.)?cancellata_il\s+is\s+null\b`)
)

// inventario: RIP-S-50
// Ogni query sulle buste di internal/ e cmd/ ha il filtro del cestino, tranne le eccezioni elencate con il motivo.
func TestFiltroDelCestinoInOgniQuery(t *testing.T) {
	trovate, viste := pezziSenzaFiltro(t, filepath.Join("..", ".."))
	var errori []string
	for f := range funzioniConWhere {
		if viste[f] == 0 {
			errori = append(errori, f+": nessuna chiamata trovata: la funzione non c'è più o ha cambiato nome (aggiorna funzioniConWhere)")
		}
	}
	for chiave, righe := range trovate {
		e, ok := eccezioniFiltroCestino[chiave]
		switch {
		case !ok:
			errori = append(errori, fmt.Sprintf("%s (righe %v): %d query sulle buste senza «cancellata_il IS NULL»: aggiungi il filtro, o un'eccezione con il motivo", chiave, righe, len(righe)))
		case e.pezzi != len(righe):
			errori = append(errori, fmt.Sprintf("%s (righe %v): %d query senza filtro, l'eccezione ne dice %d", chiave, righe, len(righe), e.pezzi))
		}
	}
	for chiave, e := range eccezioniFiltroCestino {
		if _, ok := trovate[chiave]; !ok {
			errori = append(errori, fmt.Sprintf("%s: eccezione per %d query che non ci sono più: togli la riga", chiave, e.pezzi))
		}
		if strings.TrimSpace(e.motivo) == "" {
			errori = append(errori, chiave+": eccezione senza motivo")
		}
	}
	sort.Strings(errori)
	for _, e := range errori {
		t.Error(e)
	}
}

// Il test statico vede davvero una query senza filtro, risolve i pezzi nelle variabili, nei parametri, nelle costanti
// e nelle funzioni del pacchetto, e non conta INSERT INTO buste né le altre tabelle.
func TestFiltroDelCestinoRiconosce(t *testing.T) {
	sorgente := "package p\n\n" +
		"const colonne = \"id, numero\"\n" +
		"const vivo = \"cancellata_il IS NULL\"\n\n" +
		"func dove() string { return \"WHERE b.cancellata_il IS NULL\" }\n\n" +
		"func senza(q Q) { q.Query(\"SELECT \" + colonne + \" FROM buste WHERE id = ?\") }\n" +
		"func con(q Q) { q.Query(\"SELECT id FROM buste b WHERE b.id = ? AND b.cancellata_il IS NULL\") }\n" +
		"func costante(q Q) { q.Query(\"SELECT id FROM buste WHERE \" + vivo) }\n" +
		"func locale(q Q, tutte bool) {\n\tc := \"cancellata_il IS NULL\"\n\tif tutte {\n\t\tc = \"1\"\n\t}\n" +
		"\tq.Query(\"SELECT id FROM buste WHERE \" + c)\n}\n" +
		"func parametro(q Q, f string) { q.Query(\"UPDATE buste SET x = 1 WHERE \" + f) }\n" +
		"func chiamaCon(q Q) { parametro(q, \"cancellata_il IS NULL\") }\n" +
		"func chiamaSenza(q Q) { parametro(q, \"1\") }\n" +
		"func parametroBuono(q Q, f string) { q.Query(\"UPDATE buste SET x = 1 WHERE \" + f) }\n" +
		"func chiamaBuono(q Q) { parametroBuono(q, \"b.cancellata_il IS NULL\") }\n" +
		"func funzione(q Q) {\n\tw := dove()\n\tq.Query(\"SELECT count(*) FROM buste b \" + w)\n}\n" +
		"func join(q Q) { q.Query(\"SELECT 1 FROM foto_buste f JOIN buste b ON b.id = f.busta_id WHERE b.cancellata_il IS NULL AND EXISTS (SELECT 1 FROM buste x WHERE x.id = 2)\") }\n" +
		"func nuova(q Q) { q.Query(\"INSERT INTO buste (numero) VALUES (?)\") }\n" +
		"func cancella(q Q) { q.Query(\"DELETE FROM buste WHERE id = ?\") }\n" +
		"func minuscolo(q Q) { q.Query(\"select id from buste where id = ? and cancellata_il is null\") }\n" +
		"func altraTabella(q Q) { q.Query(\"SELECT 1 FROM foto_buste WHERE busta_id = ?\") }\n" +
		// Un costruttore del WHERE come condizioni e Buste.dove: una lista che cresce, strings.Join, più risultati, un
		// return d'errore; e lo stesso senza il filtro.
		"func cond(f bool) (string, []any) {\n\tw := []string{\"b.cancellata_il IS NULL\"}\n\tif f {\n\t\tw = append(w, \"b.x = 1\")\n\t}\n" +
		"\treturn \"WHERE \" + strings.Join(w, \" AND \"), nil\n}\n" +
		"func dov(f bool) (string, []any, error) {\n\tif f {\n\t\treturn \"\", nil, errNo\n\t}\n\twhere, a := cond(f)\n\treturn where, a, nil\n}\n" +
		"func lista(q Q) {\n\twhere, _, _ := dov(true)\n\tq.Query(\"SELECT count(*) FROM buste b \" + where)\n}\n" +
		"func condSenza(f bool) (string, []any) {\n\tw := []string{\"1\"}\n\tw = append(w, \"b.x = 1\")\n\treturn \"WHERE \" + strings.Join(w, \" AND \"), nil\n}\n" +
		"func listaSenza(q Q) {\n\twhere, _ := condSenza(true)\n\tq.Query(\"SELECT count(*) FROM buste b \" + where)\n}\n"
	percorso := filepath.Join(t.TempDir(), "p.go")
	if err := os.WriteFile(percorso, []byte(sorgente), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := leggiPacchetto([]string{percorso}, []string{"p.go"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for k, v := range pkg.senzaFiltro() {
		got[k] = len(v)
	}
	want := map[string]int{"p.go:senza": 1, "p.go:locale": 1, "p.go:parametro": 1, "p.go:join": 1, "p.go:cancella": 1,
		"p.go:listaSenza": 1}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("senza filtro = %v, attese %v", got, want)
	}
}

// pezziSenzaFiltro dà, per funzione, le righe dei pezzi SQL sulle buste senza il filtro, nel sorgente sotto radice.
func pezziSenzaFiltro(t *testing.T, radice string) (map[string][]int, map[string]int) {
	t.Helper()
	perDir := map[string][][2]string{}
	var file int
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(radice, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(radice, p)
			if err != nil {
				return err
			}
			file++
			perDir[filepath.Dir(p)] = append(perDir[filepath.Dir(p)], [2]string{p, filepath.ToSlash(rel)})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if file < 100 {
		t.Fatalf("letti solo %d file Go: radice sbagliata?", file)
	}
	trovate, viste := map[string][]int{}, map[string]int{}
	for _, ff := range perDir {
		var percorsi, rel []string
		for _, f := range ff {
			percorsi, rel = append(percorsi, f[0]), append(rel, f[1])
		}
		pkg, err := leggiPacchetto(percorsi, rel)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range pkg.senzaFiltro() {
			trovate[k] = append(trovate[k], v...)
		}
		for k, n := range pkg.viste {
			viste[k] += n
		}
	}
	return trovate, viste
}

// pacchettoSQL è un pacchetto Go letto, con quello che serve a risolvere i pezzi.
type pacchettoSQL struct {
	fset     *token.FileSet
	file     []*ast.File
	rel      []string
	funzioni map[string][]*ast.FuncDecl // per nome (metodi compresi, senza il tipo)
	valori   map[string]ast.Expr        // costanti e variabili del pacchetto
	chiamate map[string][]chiamataSQL   // per nome della funzione chiamata
	viste    map[string]int             // chiamate trovate alle funzioni di funzioniConWhere
}

type chiamataSQL struct {
	call *ast.CallExpr
	in   *ast.FuncDecl
}

func leggiPacchetto(percorsi, rel []string) (*pacchettoSQL, error) {
	p := &pacchettoSQL{fset: token.NewFileSet(), rel: rel, funzioni: map[string][]*ast.FuncDecl{},
		valori: map[string]ast.Expr{}, chiamate: map[string][]chiamataSQL{}, viste: map[string]int{}}
	for _, percorso := range percorsi {
		f, err := parser.ParseFile(p.fset, percorso, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		p.file = append(p.file, f)
		for _, d := range f.Decls {
			switch x := d.(type) {
			case *ast.FuncDecl:
				p.funzioni[x.Name.Name] = append(p.funzioni[x.Name.Name], x)
				ast.Inspect(x, func(n ast.Node) bool {
					if c, ok := n.(*ast.CallExpr); ok {
						if nome := nomeChiamata(c); nome != "" {
							p.chiamate[nome] = append(p.chiamate[nome], chiamataSQL{c, x})
						}
					}
					return true
				})
			case *ast.GenDecl:
				for _, s := range x.Specs {
					if v, ok := s.(*ast.ValueSpec); ok && len(v.Values) > 0 {
						for i, n := range v.Names {
							p.valori[n.Name] = v.Values[min(i, len(v.Values)-1)]
						}
					}
				}
			}
		}
	}
	return p, nil
}

// funzioniConWhere sono le funzioni che ricevono il WHERE (o la scelta di guardare anche il cestino) da chi chiama:
// «cartella:nome» → l'indice dell'argomento. La loro query è nelle eccezioni come pezzo senza filtro, quindi il filtro va
// controllato a ogni chiamata: un argomento stringa senza «cancellata_il IS NULL», o true per una scelta del cestino, è una
// chiamata senza filtro, e il chiamante deve stare in eccezioniFiltroCestino con la chiave «file:Chiamante→funzione».
var funzioniConWhere = map[string]int{
	"internal/buste:leggiBustaEmail":       2, // dove
	"internal/buste:righeDellEtichettaCon": 3, // cestino
	"internal/buste:bustaPerNumeroScelta":  4, // conCestino
	"internal/buste:bustaStoricaDove":      1, // dove
	"internal/buste:paginaDove":            1, // dove
}

// senzaFiltro dà, per dichiarazione, le righe dei pezzi senza filtro e delle chiamate senza filtro alle funzioni di
// funzioniConWhere; viste conta le chiamate trovate per ognuna di quelle funzioni.
func (p *pacchettoSQL) senzaFiltro() map[string][]int {
	trovate := map[string][]int{}
	cartella := ""
	if len(p.rel) > 0 {
		cartella = path.Dir(p.rel[0])
	}
	for i, f := range p.file {
		for _, decl := range f.Decls {
			fn, _ := decl.(*ast.FuncDecl)
			chiave := p.rel[i] + ":" + nomeDichiarazione(decl)
			if fn != nil {
				ast.Inspect(fn, func(n ast.Node) bool {
					c, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					nome := nomeChiamata(c)
					indice, ok := funzioniConWhere[cartella+":"+nome]
					if !ok || indice >= len(c.Args) {
						return true
					}
					p.viste[cartella+":"+nome]++
					if p.argomentoSenzaFiltro(c.Args[indice], fn) {
						k := chiave + "→" + nome
						trovate[k] = append(trovate[k], p.fset.Position(c.Pos()).Line)
					}
					return true
				})
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				e, ok := n.(ast.Expr)
				if !ok || !haStringa(e) {
					return true
				}
				if p.conta(e, fn, 0).senzaFiltro() {
					trovate[chiave] = append(trovate[chiave], p.fset.Position(e.Pos()).Line)
				}
				return false // i pezzi dentro una concatenazione sono contati con lei
			})
		}
	}
	return trovate
}

// argomentoSenzaFiltro: true (la scelta di guardare anche il cestino), o un testo che non ha il filtro.
func (p *pacchettoSQL) argomentoSenzaFiltro(e ast.Expr, fn *ast.FuncDecl) bool {
	if id, ok := e.(*ast.Ident); ok && (id.Name == "true" || id.Name == "false") {
		return id.Name == "true"
	}
	return p.conta(e, fn, 0).fil == 0
}

// haStringa: una stringa, o una concatenazione con + che ne contiene una.
func haStringa(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING
	case *ast.BinaryExpr:
		return x.Op == token.ADD && (haStringa(x.X) || haStringa(x.Y))
	case *ast.ParenExpr:
		return haStringa(x.X)
	}
	return false
}

// profonditaMassima ferma la risoluzione dei nomi (una variabile che rimanda a un parametro che rimanda…).
const profonditaMassima = 4

// conto sono i riferimenti alla tabella buste e i filtri del cestino di un pezzo.
type conto struct{ rif, fil int }

func (c conto) piu(d conto) conto { return conto{c.rif + d.rif, c.fil + d.fil} }

func contoDi(sql string) conto {
	return conto{len(reRiferimentoBuste.FindAllStringIndex(sql, -1)), len(reFiltroCestino.FindAllStringIndex(sql, -1))}
}

// conta conta l'espressione e, scritta nella funzione fn (nil: a livello del pacchetto). I riferimenti si contano
// nelle stringhe del pezzo e nei valori del pacchetto che usa (i frammenti condivisi, come le colonne con il FROM), non
// nelle variabili locali, nei parametri e nei risultati delle funzioni: un frammento locale («q := "UPDATE buste SET
// …"», poi «q + ", x = ?"») è già contato dove nasce. I filtri si contano dappertutto.
func (p *pacchettoSQL) conta(e ast.Expr, fn *ast.FuncDecl, prof int) conto {
	if prof > profonditaMassima {
		return conto{}
	}
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return conto{}
		}
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			return conto{}
		}
		return contoDi(s)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return conto{}
		}
		return p.conta(x.X, fn, prof).piu(p.conta(x.Y, fn, prof))
	case *ast.ParenExpr:
		return p.conta(x.X, fn, prof)
	case *ast.Ident:
		return p.identificatore(x.Name, fn, prof+1)
	case *ast.CompositeLit: // []string{"b.cancellata_il IS NULL", …}
		var n conto
		for _, el := range x.Elts {
			n = n.piu(p.conta(el, fn, prof))
		}
		return n
	case *ast.CallExpr:
		return p.chiamata(x, 0, fn, prof+1)
	}
	return conto{}
}

// chiamata conta il risultato i di una chiamata: strings.Join(x, …) è x; append(a, b…) è tutto quello che unisce; una
// funzione del pacchetto è il suo risultato i (i suoi filtri, il return con meno); il resto non si conta.
func (p *pacchettoSQL) chiamata(c *ast.CallExpr, i int, fn *ast.FuncDecl, prof int) conto {
	if prof > profonditaMassima {
		return conto{}
	}
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "strings" && sel.Sel.Name == "Join" && len(c.Args) > 0 {
			return p.conta(c.Args[0], fn, prof)
		}
	}
	if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "append" {
		var n conto
		for _, a := range c.Args {
			n = n.piu(p.conta(a, fn, prof))
		}
		return n
	}
	var alt []conto
	for _, f := range p.funzioni[nomeChiamata(c)] {
		ast.Inspect(f.Body, func(n ast.Node) bool {
			switch r := n.(type) {
			case *ast.FuncLit:
				return false // i return di una funzione dentro non sono quelli di f
			case *ast.ReturnStmt:
				if i < len(r.Results) && !vuoto(r.Results[i]) {
					alt = append(alt, conto{fil: p.conta(r.Results[i], f, prof).fil})
				}
			}
			return true
		})
	}
	if len(alt) == 0 {
		return conto{}
	}
	return conto{fil: menoFiltri(alt)}
}

// vuoto: un risultato che non porta SQL (la stringa vuota o nil di un return d'errore).
func vuoto(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING && (x.Value == `""` || x.Value == "``")
	case *ast.Ident:
		return x.Name == "nil"
	}
	return false
}

// identificatore risolve un nome: variabile locale (il ramo con meno filtri; una lista che cresce con append, tutto
// quello che contiene), parametro (la chiamata con meno filtri), valore del pacchetto (riferimenti e filtri).
func (p *pacchettoSQL) identificatore(nome string, fn *ast.FuncDecl, prof int) conto {
	if fn != nil {
		if alt := assegnazioni(fn, nome); len(alt) > 0 {
			var c []conto
			var lista conto
			cresce := false
			for _, a := range alt {
				if ap, ok := a.e.(*ast.CallExpr); ok && aggiuntaA(ap, nome) {
					cresce = true
					for _, x := range ap.Args[1:] {
						lista = lista.piu(p.conta(x, fn, prof))
					}
					continue
				}
				var n conto
				if ch, ok := a.e.(*ast.CallExpr); ok {
					n = p.chiamata(ch, a.i, fn, prof)
				} else {
					n = p.conta(a.e, fn, prof)
				}
				c = append(c, n)
				lista = lista.piu(n)
			}
			if cresce {
				return conto{fil: lista.fil}
			}
			return conto{fil: menoFiltri(c)}
		}
		if i := indiceParametro(fn, nome); i >= 0 {
			var c []conto
			for _, ch := range p.chiamate[fn.Name.Name] {
				if i < len(ch.call.Args) {
					c = append(c, p.conta(ch.call.Args[i], ch.in, prof))
				}
			}
			if len(c) == 0 {
				return conto{}
			}
			return conto{fil: menoFiltri(c)}
		}
	}
	if v, ok := p.valori[nome]; ok {
		return contoDi(p.stringheIn(v))
	}
	return conto{}
}

// aggiuntaA: append(nome, …), la lista che cresce.
func aggiuntaA(c *ast.CallExpr, nome string) bool {
	id, ok := c.Fun.(*ast.Ident)
	if !ok || id.Name != "append" || len(c.Args) == 0 {
		return false
	}
	a, ok := c.Args[0].(*ast.Ident)
	return ok && a.Name == nome
}

// stringheIn sono tutte le stringhe dentro n, unite da spazi.
func (p *pacchettoSQL) stringheIn(n ast.Node) string {
	var parti []string
	ast.Inspect(n, func(n ast.Node) bool {
		if l, ok := n.(*ast.BasicLit); ok && l.Kind == token.STRING {
			if s, err := strconv.Unquote(l.Value); err == nil {
				parti = append(parti, s)
			}
		}
		return true
	})
	return strings.Join(parti, " ")
}

// assegnazione è un'espressione assegnata a un nome; i è il risultato che prende da una chiamata con più risultati.
type assegnazione struct {
	e ast.Expr
	i int
}

// assegnazioni sono le espressioni assegnate al nome nella funzione (:=, =, var), fuori dalle funzioni dentro.
func assegnazioni(fn *ast.FuncDecl, nome string) []assegnazione {
	var alt []assegnazione
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE && x.Tok != token.ASSIGN {
				return true
			}
			for i, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name == nome {
					if len(x.Rhs) == 1 && len(x.Lhs) > 1 {
						alt = append(alt, assegnazione{x.Rhs[0], i})
					} else {
						alt = append(alt, assegnazione{x.Rhs[min(i, len(x.Rhs)-1)], 0})
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if id.Name == nome && len(x.Values) > 0 {
					if len(x.Values) == 1 && len(x.Names) > 1 {
						alt = append(alt, assegnazione{x.Values[0], i})
					} else {
						alt = append(alt, assegnazione{x.Values[min(i, len(x.Values)-1)], 0})
					}
				}
			}
		}
		return true
	})
	return alt
}

func indiceParametro(fn *ast.FuncDecl, nome string) int {
	i := 0
	for _, campo := range fn.Type.Params.List {
		if len(campo.Names) == 0 {
			i++
			continue
		}
		for _, n := range campo.Names {
			if n.Name == nome {
				return i
			}
			i++
		}
	}
	return -1
}

func nomeChiamata(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// menoFiltri sono i filtri dell'alternativa che ne ha meno.
func menoFiltri(c []conto) int {
	return slices.MinFunc(c, func(a, b conto) int { return a.fil - b.fil }).fil
}

// senzaFiltro: più riferimenti alla tabella buste che filtri del cestino.
func (c conto) senzaFiltro() bool { return c.rif > c.fil }

func nomeDichiarazione(d ast.Decl) string {
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Recv != nil && len(x.Recv.List) == 1 {
			return nomeTipo(x.Recv.List[0].Type) + "." + x.Name.Name
		}
		return x.Name.Name
	case *ast.GenDecl:
		var nomi []string
		for _, s := range x.Specs {
			switch v := s.(type) {
			case *ast.ValueSpec:
				for _, n := range v.Names {
					nomi = append(nomi, n.Name)
				}
			case *ast.TypeSpec:
				nomi = append(nomi, v.Name.Name)
			}
		}
		slices.Sort(nomi)
		return x.Tok.String() + " " + strings.Join(nomi, ",")
	}
	return "?"
}

func nomeTipo(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return nomeTipo(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return nomeTipo(x.X)
	}
	return "?"
}
