package importer

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"modernc.org/sqlite"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Le modalità dell'import (OPS-01): dati inventati, o una copia di dati veri con il riferimento
// di chi ha autorizzato e quando.
const (
	Sintetico   = "sintetico"
	Autorizzato = "autorizzato"
)

// Opzioni sono gli ingressi di Importa.
type Opzioni struct {
	// Sorgente è il SQLite in chiaro dello standalone (decifrato dall'operatore: il binario non ha
	// SQLCipher). Si apre solo in lettura, per copiarlo.
	Sorgente string
	// Destinazione è il database Go nuovo: non deve esistere. Nella sua cartella (sull'apparecchio
	// il volume cifrato) stanno la copia della sorgente e il database in costruzione.
	Destinazione string
	// Report è il report.json: non deve esistere.
	Report string
	// Sha256 è l'impronta dichiarata della sorgente; Dimensione la sua dimensione in byte (0: non
	// dichiarata).
	Sha256     string
	Dimensione int64
	// Una sola delle due modalità; Riferimento solo e sempre con Autorizzato.
	Sintetico, Autorizzato bool
	Riferimento            string
	Orologio               clock.Orologio
	// DopoTabella, se c'è, si chiama dopo la copia di ogni tabella Go, con il contesto
	// dell'import. Serve ai test (interruzione a metà).
	DopoTabella func(ctx context.Context, tabellaGo string)
	// PrimaDiAttivare, se c'è, si chiama subito prima dell'attivazione. Serve ai test.
	PrimaDiAttivare func(ctx context.Context)
}

// Agganci dei test: dopoLaCopia gira dopo la copia della sorgente, primaDellaVerifica sul database
// finito prima della verifica d'integrità. nil fuori dai test.
var (
	dopoLaCopia        func()
	primaDellaVerifica func(percorso string)
)

// Esito è com'è finito un import arrivato al report.
type Esito struct {
	Verdetto string
	Modalita string
	Report   string
	Sha256   string
	Lette    int64
	Scritte  int64
}

// ErrInterrotto: l'import è stato fermato (SIGINT, SIGTERM). Niente è attivato, i temporanei sono
// tolti, il report non si scrive.
var ErrInterrotto = errors.New("import interrotto: niente attivato")

func controllaModalita(o Opzioni) (string, error) {
	switch {
	case o.Sintetico && o.Autorizzato:
		return "", errors.New("una sola modalità: -sintetico oppure -autorizzato")
	case o.Sintetico && o.Riferimento != "":
		return "", errors.New("-riferimento vale solo con -autorizzato")
	case o.Sintetico:
		return Sintetico, nil
	case o.Autorizzato && strings.TrimSpace(o.Riferimento) == "":
		return "", errors.New("-autorizzato chiede -riferimento \"<chi ha autorizzato e quando>\"")
	case o.Autorizzato:
		return Autorizzato, nil
	}
	return "", errors.New("serve la modalità: -sintetico (dati inventati) oppure -autorizzato -riferimento \"<chi e quando>\"")
}

// Importa porta il database dello standalone in un database Go nuovo (PIANO T2, OPS-01…05,
// OPS-07). Prima di aprire la sorgente controlla modalità, percorsi e file accanto; poi copia la
// sorgente in una cartella privata accanto alla destinazione e confronta impronta e dimensione con
// quelle dichiarate. Fin qui un rifiuto non lascia nessun file. Da lì in poi scrive sempre il
// report (tranne un'interruzione), e attiva la destinazione solo con il verdetto Riconciliato:
// il database si costruisce in un file temporaneo e prende il nome della destinazione solo alla
// fine, con un link che non sovrascrive. I temporanei si tolgono in ogni caso.
func Importa(ctx context.Context, o Opzioni) (es Esito, err error) {
	modalita, err := controllaModalita(o)
	if err != nil {
		return Esito{}, err
	}
	attesa, err := leggiImpronta(o.Sha256)
	if err != nil {
		return Esito{}, err
	}
	if o.Dimensione < 0 {
		return Esito{}, errors.New("-dimensione non può essere negativa")
	}
	if o.Sorgente == "" || o.Destinazione == "" || o.Report == "" {
		return Esito{}, errors.New("servono -sorgente, -destinazione e -report")
	}
	if o.Orologio == nil {
		return Esito{}, errors.New("import: manca l'orologio")
	}
	if err := nonEsiste("la destinazione", o.Destinazione); err != nil {
		return Esito{}, err
	}
	if err := nonEsiste("il report", o.Report); err != nil {
		return Esito{}, err
	}
	dirDest, err := cartellaDi("database", o.Destinazione)
	if err != nil {
		return Esito{}, err
	}
	if _, err := cartellaDi("report", o.Report); err != nil {
		return Esito{}, err
	}
	if err := percorsiDistinti(o); err != nil {
		return Esito{}, err
	}
	if err := controllaAccanto(o.Sorgente); err != nil {
		return Esito{}, err
	}
	if err := ctx.Err(); err != nil {
		return Esito{}, fmt.Errorf("%w: %w", ErrInterrotto, err)
	}
	// I resti di un import finito senza pulizia (SIGKILL, corrente) possono avere una copia della
	// sorgente: si tolgono a mano, l'import non li cancella da solo.
	resti, err := filepath.Glob(filepath.Join(dirDest, prefissoLavoro+"*"))
	if err != nil {
		return Esito{}, err
	}
	if len(resti) > 0 {
		return Esito{}, fmt.Errorf("resti di un import interrotto in %s: toglili a mano prima di riprovare", resti[0])
	}
	// La cartella privata (0700, nome casuale) sta accanto alla destinazione, quindi sul volume
	// cifrato: lì vanno la copia della sorgente e il database in costruzione, e si toglie sempre.
	lavoro, err := os.MkdirTemp(dirDest, prefissoLavoro)
	if err != nil {
		return Esito{}, fmt.Errorf("cartella di lavoro: %w", err)
	}
	defer func() {
		if e := os.RemoveAll(lavoro); e != nil {
			err = errors.Join(err, fmt.Errorf("cartella di lavoro %s da togliere a mano: %w", lavoro, e))
		} else if e := sincronizzaCartella(dirDest); e != nil {
			err = errors.Join(err, fmt.Errorf("cartella %s: %w", dirDest, e))
		}
	}()
	if err := os.Chmod(lavoro, 0o700); err != nil {
		return Esito{}, fmt.Errorf("cartella di lavoro: %w", err)
	}
	copia := filepath.Join(lavoro, "sorgente.db")
	n, impronta, err := copiaSorgente(o.Sorgente, copia)
	if err != nil {
		return Esito{}, err
	}
	if impronta != attesa {
		return Esito{}, fmt.Errorf("lo SHA-256 della sorgente è %s, non quello dichiarato: rifiuto", hex.EncodeToString(impronta[:]))
	}
	if o.Dimensione > 0 && n != o.Dimensione {
		return Esito{}, fmt.Errorf("la sorgente è di %d byte, non %d come dichiarato: rifiuto", n, o.Dimensione)
	}
	// Un -wal o un -journal comparso mentre si copiava: la copia potrebbe non essere a riposo.
	if dopoLaCopia != nil {
		dopoLaCopia()
	}
	if err := controllaAccanto(o.Sorgente); err != nil {
		return Esito{}, err
	}

	// Un solo istante dell'import, per il report, il segno e le migrazioni.
	adesso := o.Orologio.Adesso().UTC()
	o.Orologio = clock.NuovoFinto(adesso)
	rep := Report{Formato: FormatoReport, Modalita: modalita, Sorgente: SorgenteReport{Sha256: hex.EncodeToString(impronta[:]), Byte: n},
		Tabelle: []TabellaReport{}, Scartate: []TabellaReport{}, Totali: []TotaleReport{}, Avvisi: []Voce{}, Differenze: []Voce{},
		Istante: adesso.Format(db.FormatoIstante)}
	if modalita == Autorizzato {
		rep.Riferimento = strings.TrimSpace(o.Riferimento)
	}
	nuovo := filepath.Join(lavoro, "nuovo.db")
	errCostruzione := costruisci(ctx, copia, nuovo, impronta, n, modalita, o, &rep)
	if ctx.Err() != nil {
		return Esito{}, fmt.Errorf("%w: %w", ErrInterrotto, ctx.Err())
	}
	if errCostruzione != nil {
		// Un errore che non è dei dati (disco, lettura): il report lo dice senza il messaggio, che
		// torna a chi ha lanciato il comando.
		rep.blocca(Voce{Codice: "import/errore"})
	}
	rep.Verdetto = Riconciliato
	if rep.bloccato() {
		rep.Verdetto = Bloccato
	}
	es = Esito{Verdetto: rep.Verdetto, Modalita: modalita, Report: o.Report, Sha256: rep.Sorgente.Sha256}
	for _, t := range rep.Tabelle {
		es.Lette += t.Lette
		for _, s := range t.Scritte {
			es.Scritte += s
		}
	}
	// Prima il report in un temporaneo sincronizzato; poi, solo con il verdetto riconciliato, il
	// link della destinazione; poi il link del report e la cartella sincronizzata. Nessun link
	// sovrascrive. Se il report non prende il suo nome, la destinazione si toglie.
	tmpRep, err := preparaReport(o.Report, rep)
	if err != nil {
		return Esito{}, fmt.Errorf("%w; niente attivato", err)
	}
	defer func() {
		if e := os.Remove(tmpRep); e != nil && !errors.Is(e, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("report: temporaneo %s da togliere a mano: %w", tmpRep, e))
		}
	}()
	dirRep := filepath.Dir(o.Report)
	attivato := false
	if rep.Verdetto == Riconciliato {
		if o.PrimaDiAttivare != nil {
			o.PrimaDiAttivare(ctx)
		}
		if ctx.Err() != nil {
			return Esito{}, fmt.Errorf("%w: %w", ErrInterrotto, ctx.Err())
		}
		if err := os.Link(nuovo, o.Destinazione); err != nil {
			return Esito{}, fmt.Errorf("attivazione di %s non riuscita, niente attivato: %w", o.Destinazione, err)
		}
		attivato = true
	}
	togli := func(causa error) error {
		if !attivato {
			return causa
		}
		return errors.Join(causa, os.Remove(o.Destinazione), sincronizzaCartella(dirDest))
	}
	if err := os.Link(tmpRep, o.Report); err != nil {
		if errors.Is(err, fs.ErrExist) {
			err = fmt.Errorf("il report %s esiste già: non lo sovrascrivo", o.Report)
		}
		return Esito{}, fmt.Errorf("%w; niente attivato", togli(err))
	}
	if err := errors.Join(sincronizzaCartella(dirDest), sincronizzaCartella(dirRep)); err != nil {
		return Esito{}, fmt.Errorf("%w; niente attivato", togli(errors.Join(err, os.Remove(o.Report))))
	}
	if errCostruzione != nil {
		return es, fmt.Errorf("import non riuscito, niente attivato: %w", errCostruzione)
	}
	return es, nil
}

// apriSorgente apre la copia della sorgente in sola lettura e immutabile: SQLite non scrive niente
// accanto (né -wal né -shm), nemmeno se l'intestazione dice WAL.
func apriSorgente(copia string) (*sql.DB, error) {
	r := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")
	s, err := sql.Open("sqlite", "file:"+r.Replace(copia)+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	s.SetMaxOpenConns(1)
	return s, nil
}

// costruisci fa il lavoro sui dati: controlla la sorgente, costruisce il database nuovo e riempie
// rep. Un problema dei dati va nel report e non è un errore; l'errore è per tutto il resto.
func costruisci(ctx context.Context, copia, nuovo string, impronta [32]byte, n int64, modalita string, o Opzioni, rep *Report) (err error) {
	src, err := apriSorgente(copia)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }() // sola lettura: niente da perdere
	var esito string
	if err := src.QueryRowContext(ctx, `PRAGMA integrity_check(1)`).Scan(&esito); err != nil || esito != "ok" {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rep.blocca(Voce{Codice: "sorgente/illeggibile"})
		return nil
	}
	ok, err := controllaSchema(ctx, src, rep)
	if err != nil || !ok {
		return err
	}
	d, err := db.Apri(nuovo)
	if err != nil {
		return err
	}
	chiuso := false
	defer func() {
		if !chiuso {
			err = errors.Join(err, d.Chiudi())
		}
	}()
	if _, err := d.Migra(ctx, o.Orologio); err != nil {
		return err
	}
	g, err := leggiGo(ctx, d.Scrittura)
	if err != nil {
		return err
	}
	tx, err := d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il commit non fa niente
	// Le chiavi esterne si controllano tutte alla fine, con foreign_key_check: così ogni riga
	// orfana diventa una differenza con il suo id invece di fermare la scrittura.
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	c := &copiatore{src: src, tx: tx, g: g, rep: rep, scartate: map[string]map[int64]bool{}}
	if err := c.controlliPrima(ctx); err != nil {
		return err
	}
	for _, t := range Mappa {
		if t.Go == "" {
			continue
		}
		if err := c.copiaTabella(ctx, t); err != nil {
			return err
		}
		if o.DopoTabella != nil {
			o.DopoTabella(ctx, t.Go)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if err := c.chiaviEsterne(ctx); err != nil {
		return err
	}
	if err := c.sequenze(ctx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO import_standalone (id, modalita, sha256_sorgente, byte_sorgente, importato_il)
		VALUES (1, ?, ?, ?, ?)`, modalita, impronta[:], n, o.Orologio.Adesso().UTC().Format(db.FormatoIstante)); err != nil {
		return err
	}
	if err := c.riconcilia(ctx); err != nil {
		return err
	}
	if rep.bloccato() {
		return nil // il rollback del defer: niente resta, il file si toglie con la cartella di lavoro
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	chiuso = true
	if err := d.Chiudi(); err != nil {
		return err
	}
	return verificaNuovo(ctx, nuovo, rep)
}

// verificaNuovo porta il database finito in journal DELETE e poi, con un collegamento nuovo,
// controlla integrità e chiavi esterne. Il passaggio a DELETE serve: il nome si dà a un file solo,
// e db.Chiudi, che di solito porta il WAL nel file, lo lascia accanto se il checkpoint trova il
// file occupato (non un errore, per db). serve lo rimette in WAL.
func verificaNuovo(ctx context.Context, nuovo string, rep *Report) error {
	dsn := "file:" + strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(nuovo)
	apri := func() (*sql.DB, error) {
		c, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		c.SetMaxOpenConns(1)
		return c, nil
	}
	c, err := apri()
	if err != nil {
		return err
	}
	var modo string
	err = c.QueryRowContext(ctx, `PRAGMA journal_mode = DELETE`).Scan(&modo)
	if err = errors.Join(err, c.Close()); err != nil {
		return err
	}
	if modo != "delete" {
		return fmt.Errorf("il database nuovo resta in journal %s", modo)
	}
	for _, s := range fileAccanto {
		if _, err := os.Lstat(nuovo + s); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("accanto al database nuovo resta %s (%v)", s, err)
		}
	}
	if primaDellaVerifica != nil {
		primaDellaVerifica(nuovo)
	}
	if c, err = apri(); err != nil {
		return err
	}
	return errors.Join(controllaNuovo(ctx, c, rep), c.Close())
}

func controllaNuovo(ctx context.Context, c *sql.DB, rep *Report) error {
	var esito string
	if err := c.QueryRowContext(ctx, `PRAGMA integrity_check(1)`).Scan(&esito); err != nil {
		// Un database rovinato può non rispondere nemmeno: è un dato della verifica, non un guasto.
		rep.blocca(Voce{Codice: "destinazione/integrita"})
		return nil
	}
	if esito != "ok" {
		rep.blocca(Voce{Codice: "destinazione/integrita"})
		return nil
	}
	var orfane int64
	if err := c.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&orfane); err != nil {
		return err
	}
	if orfane > 0 {
		rep.blocca(Voce{Codice: "destinazione/chiavi_esterne", Righe: ptr(orfane)})
	}
	return nil
}

// colonneScartate sono le colonne delle tabelle scartate: anche lì una colonna non prevista ferma
// l'import.
var colonneScartate = map[string][]string{
	"permanent_deletion_permits": {"family", "resource_id"},
	"schema_migrations":          {"version", "inserted_at"},
}

func virgolette(nome string) string { return `"` + strings.ReplaceAll(nome, `"`, `""`) + `"` }

// controllaSchema confronta tabelle e colonne della sorgente con la mappa e conta le righe di ogni
// tabella. Una tabella o una colonna in più o in meno è bloccante (OPS-01): di una tabella
// sconosciuta il report dice nome e righe, mai il contenuto. ok è false se l'import non può
// andare avanti.
func controllaSchema(ctx context.Context, src *sql.DB, rep *Report) (ok bool, err error) {
	var nomi []string
	rows, err := src.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var nome string
		if err := rows.Scan(&nome); err != nil {
			_ = rows.Close()
			return false, err
		}
		nomi = append(nomi, nome)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return false, err
	}
	attese := map[string][]string{}
	var ordine []string
	for _, t := range Mappa {
		if t.Standalone == "sqlite_sequence" {
			continue
		}
		if _, gia := attese[t.Standalone]; !gia {
			ordine = append(ordine, t.Standalone)
			attese[t.Standalone] = nil
		}
		for _, c := range t.Colonne {
			if !slices.Contains(attese[t.Standalone], c.Standalone) {
				attese[t.Standalone] = append(attese[t.Standalone], c.Standalone)
			}
		}
	}
	for tab, cols := range colonneScartate {
		attese[tab] = cols
	}
	presenti := map[string]int64{}
	for _, nome := range nomi {
		var righe int64
		if err := src.QueryRowContext(ctx, `SELECT count(*) FROM `+virgolette(nome)).Scan(&righe); err != nil {
			return false, err
		}
		presenti[nome] = righe
		if _, nota := attese[nome]; !nota {
			rep.blocca(Voce{Codice: "schema/tabella_sconosciuta", Tabella: nome, Righe: ptr(righe)})
		}
	}
	ok = !rep.bloccato()
	for _, tab := range ordine {
		righe, c := presenti[tab]
		if !c {
			rep.blocca(Voce{Codice: "schema/tabella_mancante", Tabella: tab})
			ok = false
			continue
		}
		if mappata(tab) {
			rep.Tabelle = append(rep.Tabelle, TabellaReport{Standalone: tab, Lette: righe, Scritte: map[string]int64{}})
		} else {
			rep.Scartate = append(rep.Scartate, TabellaReport{Standalone: tab, Lette: righe})
		}
		var cols []string
		r, err := src.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, tab)
		if err != nil {
			return false, err
		}
		for r.Next() {
			var c string
			if err := r.Scan(&c); err != nil {
				_ = r.Close()
				return false, err
			}
			cols = append(cols, c)
		}
		if err := errors.Join(r.Err(), r.Close()); err != nil {
			return false, err
		}
		for _, c := range cols {
			if !slices.Contains(attese[tab], c) {
				rep.blocca(Voce{Codice: "schema/colonna_sconosciuta", Tabella: tab, Colonna: c})
				ok = false
			}
		}
		for _, c := range attese[tab] {
			if !slices.Contains(cols, c) {
				rep.blocca(Voce{Codice: "schema/colonna_mancante", Tabella: tab, Colonna: c})
				ok = false
			}
		}
		// I permessi di eliminazione definitiva vivono solo dentro la loro transazione: in una copia
		// a riposo la tabella è vuota (mappa).
		if tab == "permanent_deletion_permits" && righe > 0 {
			rep.blocca(Voce{Codice: "dati/permessi_eliminazione_presenti", Tabella: tab, Righe: ptr(righe)})
			ok = false
		}
	}
	return ok, nil
}

func mappata(standalone string) bool {
	for _, t := range Mappa {
		if t.Standalone == standalone && t.Go != "" {
			return true
		}
	}
	return false
}

// colonnaGo è quello che l'import deve sapere di una colonna del Go.
type colonnaGo struct {
	tipo string
}

// schemaGoNuovo sono le colonne e l'AUTOINCREMENT delle tabelle del Go migrato.
type schemaGoNuovo struct {
	colonne       map[string]map[string]colonnaGo
	ordine        map[string][]string
	autoincrement map[string]bool
}

func leggiGo(ctx context.Context, q *sql.DB) (schemaGoNuovo, error) {
	g := schemaGoNuovo{colonne: map[string]map[string]colonnaGo{}, ordine: map[string][]string{}, autoincrement: map[string]bool{}}
	rows, err := q.QueryContext(ctx, `SELECT m.name, m.sql, p.name, p.type FROM sqlite_schema m, pragma_table_info(m.name) p
		WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY m.name, p.cid`)
	if err != nil {
		return g, err
	}
	defer rows.Close()
	for rows.Next() {
		var tab, ddl, col, tipo string
		if err := rows.Scan(&tab, &ddl, &col, &tipo); err != nil {
			return g, err
		}
		if g.colonne[tab] == nil {
			g.colonne[tab] = map[string]colonnaGo{}
		}
		g.colonne[tab][col] = colonnaGo{tipo: strings.ToUpper(tipo)}
		g.ordine[tab] = append(g.ordine[tab], col)
		g.autoincrement[tab] = strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT")
	}
	return g, rows.Err()
}

// copiatore porta le righe dalla sorgente alla transazione del database nuovo.
type copiatore struct {
	src *sql.DB
	tx  *sql.Tx
	g   schemaGoNuovo
	rep *Report
	// scartate sono le righe (tabella dello standalone, rowid) che hanno già una differenza
	// bloccante dai controlli di prima: non si scrivono, così il report non le conta due volte.
	scartate map[string]map[int64]bool
}

func (c *copiatore) scarta(tab string, id int64, v Voce) {
	if c.scartate[tab] == nil {
		c.scartate[tab] = map[int64]bool{}
	}
	if !c.scartate[tab][id] {
		c.rep.blocca(v)
	}
	c.scartate[tab][id] = true
}

// controlliPrima guarda la sorgente prima di scrivere:
//   - typeof() di ogni colonna che nel Go è BLOB: le tabelle STRICT rifiutano un hash salvato come
//     testo, e lo standalone, che non è STRICT, può averlo (mappa);
//   - le email degli account attivi: se due diventano uguali dopo la normalizzazione serve una
//     scelta (PIANO T2). Un attivo e un disattivato con la stessa email vanno bene: nel Go l'email
//     è unica solo tra gli attivi (utenti_email_attivi), come nello standalone.
func (c *copiatore) controlliPrima(ctx context.Context) error {
	for _, t := range Mappa {
		if t.Go == "" {
			continue
		}
		for _, col := range t.Colonne {
			if c.g.colonne[t.Go][col.Go].tipo != "BLOB" {
				continue
			}
			rows, err := c.src.QueryContext(ctx, fmt.Sprintf(`SELECT rowid FROM %s WHERE typeof(%s) NOT IN ('blob', 'null') ORDER BY rowid`,
				virgolette(t.Standalone), virgolette(col.Standalone)))
			if err != nil {
				return err
			}
			var ids []int64
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					_ = rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			if err := errors.Join(rows.Err(), rows.Close()); err != nil {
				return err
			}
			for _, id := range ids {
				c.scarta(t.Standalone, id, Voce{Codice: "tipo/blob_atteso", Tabella: t.Standalone, Id: ptr(id), Colonna: col.Standalone})
			}
		}
	}
	// Le chiavi che scrive solo il Go non possono venire dallo standalone (mappa: ChiaviSoloGo).
	for _, k := range chiaviOrdinate(ChiaviSoloGo) {
		var id int64
		err := c.src.QueryRowContext(ctx, `SELECT rowid FROM settings WHERE key = ?`, k).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		c.scarta("settings", id, Voce{Codice: "settings/chiave_solo_go", Tabella: "settings", Id: ptr(id), Colonna: "key", Chiave: k})
	}
	rows, err := c.src.QueryContext(ctx, `SELECT id, email FROM users WHERE active = 1 AND typeof(email) = 'text' ORDER BY id`)
	if err != nil {
		return err
	}
	perEmail := map[string][]int64{}
	var ordine []string
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			_ = rows.Close()
			return err
		}
		k := accounts.NormalizzaEmail(email)
		if perEmail[k] == nil {
			ordine = append(ordine, k)
		}
		perEmail[k] = append(perEmail[k], id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, k := range ordine {
		if ids := perEmail[k]; len(ids) > 1 {
			for _, id := range ids {
				c.scarta("users", id, Voce{Codice: "utenti/email_uguale_tra_attivi", Tabella: "users", Id: ptr(id), Colonna: "email"})
			}
		}
	}
	return nil
}

// errTraduzione è un valore che non si traduce: codice, mai il valore.
type errTraduzione string

func (e errTraduzione) Error() string { return string(e) }

var reIstante = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,6})?Z$`)

// traduciValore passa un valore dallo standalone al Go secondo la mappa. avviso dice che il valore è
// cambiato in un modo da segnalare (l'email normalizzata).
func traduciValore(tr Traduzione, v any) (out any, avviso bool, err error) {
	if v == nil {
		return nil, false, nil
	}
	switch tr {
	case Uguale:
		return v, false, nil
	case Istante:
		s, ok := v.(string)
		if !ok || !reIstante.MatchString(s) {
			return nil, false, errTraduzione("traduzione/istante")
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, false, errTraduzione("traduzione/istante")
		}
		return t.UTC().Format(db.FormatoIstante), false, nil
	case Data:
		// time.Parse con la forma AAAA-MM-GG vuole proprio quelle cifre e una data che esiste.
		s, ok := v.(string)
		if !ok {
			return nil, false, errTraduzione("traduzione/data")
		}
		if _, err := time.Parse(time.DateOnly, s); err != nil {
			return nil, false, errTraduzione("traduzione/data")
		}
		return s, false, nil
	case Booleano:
		if i, ok := v.(int64); ok && (i == 0 || i == 1) {
			return i, false, nil
		}
		return nil, false, errTraduzione("traduzione/booleano")
	case JSON:
		s, ok := v.(string)
		if !ok || !json.Valid([]byte(s)) {
			return nil, false, errTraduzione("traduzione/json")
		}
		return s, false, nil
	case Ruolo:
		switch v {
		case "owner":
			return accounts.Titolare, false, nil
		case "staff":
			return accounts.Dipendente, false, nil
		}
		return nil, false, errTraduzione("traduzione/ruolo")
	case Email:
		s, ok := v.(string)
		if !ok {
			return nil, false, errTraduzione("traduzione/email")
		}
		n := accounts.NormalizzaEmail(s)
		return n, n != s, nil
	}
	return nil, false, fmt.Errorf("traduzione %q sconosciuta", tr)
}

// righeDi dice se una riga della sorgente va in questa tabella Go: settings si divide tra
// impostazioni e segreti_provider (D-03).
func righeDi(t Tabella, valori map[string]any) bool {
	if t.Standalone != "settings" {
		return true
	}
	k, _ := valori["key"].(string)
	return slices.Contains(ChiaviSegreteProvider, k) == (t.Go == "segreti_provider")
}

// copiaTabella copia una tabella della mappa, una riga alla volta in ordine di rowid. Una riga
// che non si traduce o che il Go rifiuta (CHECK, unicità, tipo, trigger) è una differenza
// bloccante con tabella, id, colonna e codice; le chiavi esterne si guardano alla fine.
func (c *copiatore) copiaTabella(ctx context.Context, t Tabella) error {
	var cs, gs []string
	for _, col := range t.Colonne {
		cs = append(cs, virgolette(col.Standalone))
		gs = append(gs, virgolette(col.Go))
	}
	var extra []any
	for _, k := range chiaviOrdinate(ValoriSoloGo[t.Go]) {
		gs = append(gs, virgolette(k))
		extra = append(extra, ValoriSoloGo[t.Go][k])
	}
	ins, err := c.tx.PrepareContext(ctx, fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, virgolette(t.Go), strings.Join(gs, ", "),
		strings.TrimSuffix(strings.Repeat("?, ", len(gs)), ", ")))
	if err != nil {
		return err
	}
	defer func() { _ = ins.Close() }() // la transazione decide
	rows, err := c.src.QueryContext(ctx, fmt.Sprintf(`SELECT rowid, %s FROM %s ORDER BY rowid`, strings.Join(cs, ", "), virgolette(t.Standalone)))
	if err != nil {
		return err
	}
	defer rows.Close()
	letti := 0
	for rows.Next() {
		buf := make([]any, len(t.Colonne)+1)
		ptrs := make([]any, len(buf))
		for i := range buf {
			ptrs[i] = &buf[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if letti++; letti%500 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		id, ok := buf[0].(int64)
		if !ok {
			return fmt.Errorf("%s: rowid non intero", t.Standalone)
		}
		valori := map[string]any{}
		for i, col := range t.Colonne {
			valori[col.Standalone] = buf[i+1]
		}
		if !righeDi(t, valori) || c.scartate[t.Standalone][id] {
			continue
		}
		args := make([]any, 0, len(gs))
		rotta := false
		for _, col := range t.Colonne {
			v, avviso, err := traduciValore(col.Traduzione, valori[col.Standalone])
			var et errTraduzione
			if errors.As(err, &et) {
				c.scarta(t.Standalone, id, Voce{Codice: string(et), Tabella: t.Standalone, Id: ptr(id), Colonna: col.Standalone})
				rotta = true
				break
			}
			if err != nil {
				return err
			}
			if avviso {
				c.rep.avvisa(Voce{Codice: "utenti/email_normalizzata", Tabella: t.Standalone, Id: ptr(id), Colonna: col.Standalone})
			}
			args = append(args, v)
		}
		if rotta {
			continue
		}
		args = append(args, extra...)
		if _, err := ins.ExecContext(ctx, args...); err != nil {
			codice, colGo, vincolo := codiceVincolo(err, t.Go, c.g.ordine[t.Go])
			if !vincolo {
				return err
			}
			c.scarta(t.Standalone, id, Voce{Codice: codice, Tabella: t.Standalone, Id: ptr(id), Colonna: standaloneDi(t, colGo)})
		}
	}
	return rows.Err()
}

func chiaviOrdinate[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// standaloneDi dà il nome dello standalone di una colonna Go della tabella (vuoto se non c'è).
func standaloneDi(t Tabella, colGo string) string {
	for _, c := range t.Colonne {
		if c.Go == colGo && colGo != "" {
			return c.Standalone
		}
	}
	return ""
}

// I codici estesi di SQLite per i vincoli (sqlite3.h).
const (
	sqliteConstraint           = 19
	sqliteConstraintCheck      = 19 | 1<<8
	sqliteConstraintForeignKey = 19 | 3<<8
	sqliteConstraintNotNull    = 19 | 5<<8
	sqliteConstraintPrimaryKey = 19 | 6<<8
	sqliteConstraintTrigger    = 19 | 7<<8
	sqliteConstraintUnique     = 19 | 8<<8
	sqliteConstraintDatatype   = 19 | 12<<8
)

var reParola = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// codiceVincolo traduce il rifiuto di SQLite in un codice e, dove si capisce, la colonna Go. Dal
// messaggio si prendono solo nomi che sono colonne della tabella (cols), così nessun valore
// passa. vincolo è false se l'errore non è un vincolo (disco, transazione): allora è un errore
// dell'import, non dei dati.
func codiceVincolo(err error, tab string, cols []string) (codice, colonna string, vincolo bool) {
	var se *sqlite.Error
	if !errors.As(err, &se) || se.Code()&0xff != sqliteConstraint {
		return "", "", false
	}
	switch se.Code() {
	case sqliteConstraintCheck:
		codice = "vincolo/check"
	case sqliteConstraintForeignKey:
		codice = "vincolo/chiave_esterna"
	case sqliteConstraintNotNull:
		codice = "vincolo/non_nullo"
	case sqliteConstraintPrimaryKey:
		codice = "vincolo/chiave_primaria"
	case sqliteConstraintTrigger:
		codice = "vincolo/trigger"
	case sqliteConstraintUnique:
		codice = "vincolo/unico"
	case sqliteConstraintDatatype:
		codice = "vincolo/tipo"
	default:
		codice = "vincolo/altro"
	}
	msg := se.Error()
	// «UNIQUE constraint failed: tab.col», «cannot store TEXT value in INTEGER column tab.col»:
	// la colonna dopo il nome della tabella; per un CHECK la prima colonna nominata nell'espressione.
	if i := strings.Index(msg, tab+"."); i >= 0 {
		if p := reParola.FindString(msg[i+len(tab)+1:]); slices.Contains(cols, p) {
			return codice, p, true
		}
	}
	if codice == "vincolo/check" {
		if _, espr, ok := strings.Cut(msg, "CHECK constraint failed:"); ok {
			for _, p := range reParola.FindAllString(espr, -1) {
				if slices.Contains(cols, p) {
					return codice, p, true
				}
			}
		}
	}
	return codice, "", true
}

// chiaviEsterne guarda tutte le chiavi esterne alla fine (sono rimandate): ogni riga orfana è una
// differenza con la tabella, l'id e la colonna dello standalone.
func (c *copiatore) chiaviEsterne(ctx context.Context) error {
	rows, err := c.tx.QueryContext(ctx, `SELECT "table", rowid, fkid FROM pragma_foreign_key_check ORDER BY "table", rowid, fkid`)
	if err != nil {
		return err
	}
	type orfana struct {
		tab  string
		id   int64
		fkid int64
	}
	var orfane []orfana
	for rows.Next() {
		var o orfana
		var id sql.NullInt64
		if err := rows.Scan(&o.tab, &id, &o.fkid); err != nil {
			_ = rows.Close()
			return err
		}
		o.id = id.Int64
		orfane = append(orfane, o)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, o := range orfane {
		var colGo string
		if err := c.tx.QueryRowContext(ctx, `SELECT "from" FROM pragma_foreign_key_list(?) WHERE id = ? LIMIT 1`, o.tab, o.fkid).Scan(&colGo); err != nil {
			return err
		}
		t, ok := tabellaGo(o.tab)
		if !ok {
			return fmt.Errorf("chiave esterna orfana in %s, che non viene dallo standalone", o.tab)
		}
		// Nelle tabelle con id il rowid del Go è l'id dello standalone; l'altra tabella con chiavi
		// esterne (versioni_stato_buste) ha come chiave primaria intera la colonna stessa, che è il
		// rowid in tutti e due.
		c.rep.blocca(Voce{Codice: "vincolo/chiave_esterna", Tabella: t.Standalone, Id: ptr(o.id), Colonna: standaloneDi(t, colGo)})
	}
	return nil
}

func tabellaGo(nome string) (Tabella, bool) {
	for _, t := range Mappa {
		if t.Go == nome {
			return t, true
		}
	}
	return Tabella{}, false
}

// sequenze porta sqlite_sequence di ogni tabella Go con AUTOINCREMENT a max(seq dello standalone,
// max id): un id non si riusa mai (PIANO T2).
func (c *copiatore) sequenze(ctx context.Context) error {
	seq := map[string]int64{}
	var ha int
	if err := c.src.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'sqlite_sequence'`).Scan(&ha); err != nil {
		return err
	}
	if ha > 0 {
		rows, err := c.src.QueryContext(ctx, `SELECT name, seq FROM sqlite_sequence`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var nome string
			var n sql.NullInt64
			if err := rows.Scan(&nome, &n); err != nil {
				_ = rows.Close()
				return err
			}
			seq[nome] = max(seq[nome], n.Int64)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
	}
	for _, t := range Mappa {
		if t.Go == "" || !c.g.autoincrement[t.Go] {
			continue
		}
		var maxId sql.NullInt64
		if err := c.tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT max(rowid) FROM %s`, virgolette(t.Go))).Scan(&maxId); err != nil {
			return err
		}
		n := max(seq[t.Standalone], maxId.Int64)
		if n <= 0 {
			continue
		}
		res, err := c.tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq = ? WHERE name = ?`, n, t.Go)
		if err != nil {
			return err
		}
		if k, err := res.RowsAffected(); err != nil {
			return err
		} else if k == 0 {
			if _, err := c.tx.ExecContext(ctx, `INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)`, t.Go, n); err != nil {
				return err
			}
		}
	}
	return nil
}

// riconcilia conta le righe scritte per tabella e somma le colonne di soldi, e confronta con la
// sorgente: ogni differenza è bloccante (OPS-05). Niente acconti: lo schema dello standalone non
// li ha (PIANO T2).
func (c *copiatore) riconcilia(ctx context.Context) error {
	for i := range c.rep.Tabelle {
		tr := &c.rep.Tabelle[i]
		var tot int64
		for _, t := range Mappa {
			if t.Standalone != tr.Standalone || t.Go == "" {
				continue
			}
			var n int64
			if err := c.tx.QueryRowContext(ctx, `SELECT count(*) FROM `+virgolette(t.Go)).Scan(&n); err != nil {
				return err
			}
			tr.Scritte[t.Go] = n
			tot += n
			for _, col := range t.Colonne {
				if !strings.HasSuffix(col.Standalone, "_cents") {
					continue
				}
				var s, g any
				if err := c.src.QueryRowContext(ctx, fmt.Sprintf(`SELECT coalesce(sum(%s), 0) FROM %s`, virgolette(col.Standalone), virgolette(t.Standalone))).Scan(&s); err != nil {
					return err
				}
				if err := c.tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT coalesce(sum(%s), 0) FROM %s`, virgolette(col.Go), virgolette(t.Go))).Scan(&g); err != nil {
					return err
				}
				si, ok1 := s.(int64)
				gi, ok2 := g.(int64)
				c.rep.Totali = append(c.rep.Totali, TotaleReport{Tabella: t.Standalone, Colonna: col.Standalone, Sorgente: si, Destinazione: gi})
				if !ok1 || !ok2 || si != gi {
					c.rep.blocca(Voce{Codice: "riconciliazione/totale", Tabella: t.Standalone, Colonna: col.Standalone})
				}
			}
		}
		if tot != tr.Lette {
			c.rep.blocca(Voce{Codice: "riconciliazione/conteggio", Tabella: tr.Standalone, Righe: ptr(tr.Lette - tot)})
		}
	}
	return nil
}

// prefissoLavoro è l'inizio del nome della cartella di lavoro dell'import.
const prefissoLavoro = ".gestrip-import-"

// percorsiDistinti: destinazione e report sono due file diversi, e nessuno dei due è la sorgente o
// uno dei suoi file accanto (un -wal scritto dall'import cambierebbe la sorgente). Si confrontano
// i percorsi assoluti puliti.
func percorsiDistinti(o Opzioni) error {
	ass := func(p string) (string, error) { return filepath.Abs(p) }
	src, err := ass(o.Sorgente)
	if err != nil {
		return err
	}
	dest, err := ass(o.Destinazione)
	if err != nil {
		return err
	}
	rep, err := ass(o.Report)
	if err != nil {
		return err
	}
	if dest == rep {
		return errors.New("destinazione e report devono essere due file diversi")
	}
	vietati := []string{src}
	for _, s := range fileAccanto {
		vietati = append(vietati, src+s)
	}
	for _, p := range []struct{ cosa, p string }{{"la destinazione", dest}, {"il report", rep}} {
		if slices.Contains(vietati, p.p) {
			return fmt.Errorf("%s non può essere la sorgente o un suo file accanto", p.cosa)
		}
	}
	return nil
}
