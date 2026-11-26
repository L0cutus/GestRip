package db

// Lo stato «isolato» di un database ripristinato (slice 130, OPS-19, D-12, risposta I del piano della T5.2).
// Lo standalone (restore.sh, packaging.py write_isolation_overlay a go-baseline) isola la copia con una rete
// Docker interna senza uscita e lo scrive in state.json; il Go non ha una rete da togliere, quindi lo stato sta
// nei dati ripristinati: una riga di impostazioni che serve legge a ogni avvio. Finché c'è, serve tiene spenti
// email, Telegram (invio e lettore), lavori automatici, backup automatici e copia remota, e la faccia pubblica
// risponde «in manutenzione». La riga la toglie solo la conferma «questa è ora la macchina vera» (slice 131),
// che riaccende quello che la riga dice acceso nel backup.

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// ChiaveIsolamento è la chiave di impostazioni, solo del Go, dello stato isolato (JSON di Isolamento).
const ChiaveIsolamento = "ripristino.isolato"

// NomeChiaveServerDelBackup è il nome, nella tabella segreti, della chiave del server che il backup aveva: il
// ripristino la rinnova (sessioni, CSRF e moduli della macchina vecchia non valgono sulla copia) ma tiene la
// vecchia da parte, mai usata da serve, perché da lei vengono anche le firme dei link già mandati ai clienti
// (conferma e disiscrizione email) e agli artigiani: se tenerla o buttarla lo decide la 131 (dubbio della 130).
const NomeChiaveServerDelBackup = "chiave_server_del_backup"

// FormatoIsolamento è il formato di Isolamento che questo programma sa leggere.
const FormatoIsolamento = 1

// TabelleSvuotateDalRipristino sono i registri dei backup che il ripristino svuota (risposta I, nota 13(a) e (c)):
// parlano della macchina vecchia, dei suoi archivi e della sua Storage Box. Svuotati, la rotazione della copia non
// cancella mai un archivio fatto dalla macchina vecchia e niente si sblocca (D-11) finché la copia non fa un
// backup suo.
var TabelleSvuotateDalRipristino = []string{"verifiche_backup", "esecuzioni_backup", "prove_ripristino", "copie_remote", "prove_remote"}

// Isolamento è lo stato isolato: da quale archivio viene la copia e cosa il ripristino ha spento.
type Isolamento struct {
	Formato        int    `json:"formato"`
	RipristinatoIl string `json:"ripristinato_il"`
	// Archivio è il solo nome del file dell'archivio (mai il percorso), SHA256 la sua impronta, CatturatoIl l'ora
	// della copia dei dati (snapshot.json).
	Archivio    string `json:"archivio"`
	SHA256      string `json:"sha256"`
	CatturatoIl string `json:"catturato_il"`
	// Spento sono le impostazioni che il ripristino ha tolto, con il valore che avevano nel backup (chiave →
	// valore): gli interruttori di email e Telegram e la configurazione della copia remota. La 131 le rimette.
	Spento map[string]string `json:"spento"`
}

// ErrIsolamento: la riga dello stato isolato c'è ma non si legge. serve non parte (fallisce chiuso: una copia che
// non sa se è isolata non deve parlare con nessuno).
var ErrIsolamento = errors.New("stato del ripristino isolato rovinato")

// LeggiIsolamento dice se il database è una copia ripristinata ancora isolata. Senza riga (false, nil). Una riga
// che non è un Isolamento del formato conosciuto, scritto per intero, è ErrIsolamento.
func LeggiIsolamento(ctx context.Context, q Interrogatore) (Isolamento, bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveIsolamento).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return Isolamento{}, false, nil
	}
	if err != nil {
		return Isolamento{}, false, err
	}
	var i Isolamento
	d := json.NewDecoder(bytes.NewReader([]byte(v)))
	d.DisallowUnknownFields()
	if err := d.Decode(&i); err != nil || d.More() {
		return Isolamento{}, true, fmt.Errorf("%w: non si legge", ErrIsolamento)
	}
	if i.Formato != FormatoIsolamento {
		return Isolamento{}, true, fmt.Errorf("%w: formato %d, conosco solo il %d", ErrIsolamento, i.Formato, FormatoIsolamento)
	}
	if _, err := time.Parse(FormatoIstante, i.RipristinatoIl); err != nil {
		return Isolamento{}, true, fmt.Errorf("%w: ora del ripristino non valida", ErrIsolamento)
	}
	return i, true, nil
}

// IsolaRipristino mette il database appena ripristinato nello stato isolato, in una transazione sola:
//   - le impostazioni daSpegnere si tolgono e il loro valore va in Isolamento.Spento (se il backup era già di una
//     copia isolata, i valori che quella aveva messo da parte restano: sono quelli della macchina vera);
//   - le impostazioni daTogliere si tolgono e basta (lo stato degli avvisi dei backup);
//   - i registri dei backup (TabelleSvuotateDalRipristino) si svuotano;
//   - la chiave del server si rinnova; la vecchia resta in segreti come NomeChiaveServerDelBackup (se c'è già, da
//     un ripristino di prima, resta quella: è la più vecchia);
//   - si scrive la riga ChiaveIsolamento.
//
// iso porta archivio, impronta e ora di cattura; formato, ora del ripristino e Spento li mette questa funzione.
func IsolaRipristino(ctx context.Context, d *DB, o clock.Orologio, iso Isolamento, daSpegnere, daTogliere []string) (Isolamento, error) {
	adesso := o.Adesso().UTC().Format(FormatoIstante)
	tx, err := d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Isolamento{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo Commit non fa niente
	spento := map[string]string{}
	var prima string
	switch err := tx.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveIsolamento).Scan(&prima); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return Isolamento{}, err
	default:
		// Un backup fatto da una copia ancora isolata: i valori della macchina vera stanno nella sua riga.
		var vecchio Isolamento
		if json.Unmarshal([]byte(prima), &vecchio) != nil {
			return Isolamento{}, fmt.Errorf("%w: nel backup", ErrIsolamento)
		}
		maps.Copy(spento, vecchio.Spento)
	}
	for _, k := range slices.Concat(daSpegnere, daTogliere) {
		var v string
		err := tx.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, k).Scan(&v)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return Isolamento{}, err
		}
		if _, gia := spento[k]; !gia && slices.Contains(daSpegnere, k) {
			spento[k] = v
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM impostazioni WHERE chiave = ?`, k); err != nil {
			return Isolamento{}, err
		}
	}
	for _, t := range TabelleSvuotateDalRipristino {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t); err != nil {
			return Isolamento{}, fmt.Errorf("svuotamento di %s: %w", t, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO segreti (nome, valore, creato_il)
		SELECT ?, valore, creato_il FROM segreti WHERE nome = 'chiave_server'`, NomeChiaveServerDelBackup); err != nil {
		return Isolamento{}, fmt.Errorf("chiave del server del backup: %w", err)
	}
	k := make([]byte, LunghezzaChiaveServer)
	_, _ = rand.Read(k) // crypto/rand.Read non fallisce
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO segreti (nome, valore, creato_il) VALUES ('chiave_server', ?, ?)`,
		k, adesso); err != nil {
		return Isolamento{}, fmt.Errorf("rinnovo della chiave del server: %w", err)
	}
	iso.Formato, iso.RipristinatoIl, iso.Spento = FormatoIsolamento, adesso, spento
	j, err := json.Marshal(iso)
	if err != nil {
		return Isolamento{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)`,
		ChiaveIsolamento, string(j), adesso); err != nil {
		return Isolamento{}, err
	}
	if err := tx.Commit(); err != nil {
		return Isolamento{}, err
	}
	return iso, nil
}
