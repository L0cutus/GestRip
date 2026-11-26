package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Il registro delle prove mensili della copia remota (slice 129c, T5.2, OPS-28, risposta E del piano): la tabella
// prove_remote (migrazione 0025). La prova la fa internal/backup (scarica l'ultima copia remota e la verifica per
// intera); qui ci sono la scrittura e le letture per il calendario e per lo stato dei backup (banner e avvisi). Come
// prove_ripristino, sta qui perché la pagina e gli avvisi la leggono senza dipendere dal pacchetto dei backup.

// CodiciProvaRemota sono i codici ammessi nella colonna codice (lo stesso elenco del CHECK della 0025): quelli della
// copia remota che valgono anche per il download, più quelli solo della prova.
var CodiciProvaRemota = []string{"configurazione", "chiave", "rete", "impronta", "accesso", "cartella", "spazio",
	"controllo", "altro", "nessuna", "vecchia", "orologio", "assente", "scaricamento", "verifica"}

// maxErroreProvaRemota è quanti byte del motivo entrano nella riga.
const maxErroreProvaRemota = 1000

// reMese è la forma della colonna mese.
var reMese = regexp.MustCompile(`\A[0-9]{4}-(0[1-9]|1[0-2])\z`)

// ErrProvaRemota è una riga che non si può scrivere in prove_remote.
var ErrProvaRemota = errors.New("registro delle prove della copia remota: riga non valida")

// ProvaRemota è una riga di prove_remote.
type ProvaRemota struct {
	ID    int64
	Avvio string // AvvioProvaAutomatico o AvvioProvaAMano
	// Mese è il mese della prova, «aaaa-mm» (backup.MeseDi).
	Mese string
	// Turno è il giorno del negozio del turno del backup (la notte della prova): i tentativi si contano per turno.
	Turno                clock.Data
	IniziatoIl, FinitoIl time.Time
	Riuscita             bool
	Codice               string // "" se riuscita
	Errore               string // "" se riuscita
	// Archivio (percorso sul server), SHA256, Byte, CatturatoIl: la copia remota provata, se il registro dei backup
	// ne aveva una (in una prova riuscita sempre).
	Archivio    string
	SHA256      string
	Byte        int64
	CatturatoIl time.Time
	// MediaRovinati: i media rovinati (D-116) trovati dalla verifica; non fanno fallire.
	MediaRovinati int
}

// RegistraProvaRemota scrive una riga nel registro delle prove della copia remota e ne dà l'id. Una riga senza i
// campi giusti è ErrProvaRemota e non si scrive; il motivo si accorcia a maxErroreProvaRemota byte.
func RegistraProvaRemota(ctx context.Context, d *DB, p ProvaRemota) (int64, error) {
	esito := "ok"
	var codice, errore sql.NullString
	if p.Riuscita {
		if p.Codice != "" || p.Errore != "" {
			return 0, fmt.Errorf("%w: una prova riuscita non ha codice né motivo", ErrProvaRemota)
		}
	} else {
		esito = "fallita"
		ok := false
		for _, k := range CodiciProvaRemota {
			ok = ok || k == p.Codice
		}
		e := strings.TrimSpace(p.Errore)
		if !ok || e == "" {
			return 0, fmt.Errorf("%w: codice %q o motivo mancante", ErrProvaRemota, p.Codice)
		}
		if len(e) > maxErroreProvaRemota {
			e = strings.ToValidUTF8(e[:maxErroreProvaRemota], "")
		}
		codice, errore = sql.NullString{String: p.Codice, Valid: true}, sql.NullString{String: e, Valid: true}
	}
	switch {
	case p.Avvio != AvvioProvaAutomatico && p.Avvio != AvvioProvaAMano:
		return 0, fmt.Errorf("%w: avvio %q", ErrProvaRemota, p.Avvio)
	case !reMese.MatchString(p.Mese):
		return 0, fmt.Errorf("%w: mese %q", ErrProvaRemota, p.Mese)
	case p.Turno == (clock.Data{}):
		return 0, fmt.Errorf("%w: turno mancante", ErrProvaRemota)
	case p.IniziatoIl.IsZero() || p.FinitoIl.IsZero():
		return 0, fmt.Errorf("%w: istanti mancanti", ErrProvaRemota)
	case p.MediaRovinati < 0:
		return 0, fmt.Errorf("%w: media rovinati %d", ErrProvaRemota, p.MediaRovinati)
	}
	var archivio, sha, catturato sql.NullString
	var byte sql.NullInt64
	if p.Archivio != "" {
		if !path.IsAbs(p.Archivio) || path.Clean(p.Archivio) != p.Archivio {
			return 0, fmt.Errorf("%w: percorso remoto non assoluto", ErrProvaRemota)
		}
		if !impronta(p.SHA256) || p.Byte <= 0 || p.CatturatoIl.IsZero() {
			return 0, fmt.Errorf("%w: copia senza SHA-256, byte o ora di cattura", ErrProvaRemota)
		}
		archivio = sql.NullString{String: p.Archivio, Valid: true}
		sha = sql.NullString{String: p.SHA256, Valid: true}
		byte = sql.NullInt64{Int64: p.Byte, Valid: true}
		catturato = sql.NullString{String: p.CatturatoIl.UTC().Format(FormatoIstante), Valid: true}
	} else if p.Riuscita {
		return 0, fmt.Errorf("%w: prova riuscita senza copia", ErrProvaRemota)
	}
	r, err := d.Scrittura.ExecContext(ctx, `INSERT INTO prove_remote
		(avvio, mese, turno, iniziato_il, finito_il, esito, codice, errore, archivio, sha256, byte, catturato_il, media_rovinati)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Avvio, p.Mese, p.Turno.String(), p.IniziatoIl.UTC().Format(FormatoIstante), p.FinitoIl.UTC().Format(FormatoIstante), esito,
		codice, errore, archivio, sha, byte, catturato, p.MediaRovinati)
	if err != nil {
		return 0, fmt.Errorf("registro delle prove della copia remota: %w", err)
	}
	return r.LastInsertId()
}

// TentativiProvaRemota servono al calendario (internal/backup): per un avvio, le prove del turno (quante e la fine
// dell'ultima, zero senza righe) e quante prove riuscite ha il mese (revisione della 129c: un mese non riuscito si
// riprova ogni notte, al più tre volte per turno).
func TentativiProvaRemota(ctx context.Context, q Interrogatore, avvio, mese string, turno clock.Data) (tutteTurno, riusciteMese int, ultima time.Time, err error) {
	var fine sql.NullString
	err = q.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE turno = ?), count(*) FILTER (WHERE esito = 'ok'),
		max(finito_il) FILTER (WHERE turno = ?) FROM prove_remote WHERE avvio = ? AND mese = ?`, turno.String(), turno.String(),
		avvio, mese).Scan(&tutteTurno, &riusciteMese, &fine)
	if err != nil {
		return 0, 0, time.Time{}, fmt.Errorf("registro delle prove della copia remota: %w", err)
	}
	if fine.Valid {
		if ultima, err = time.Parse(FormatoIstante, fine.String); err != nil {
			return 0, 0, time.Time{}, fmt.Errorf("registro delle prove della copia remota: fine dell'ultima prova: %w", err)
		}
	}
	return tutteTurno, riusciteMese, ultima, nil
}

// UltimaProvaRemota è l'ultima prova scritta (per id: le righe si aggiungono soltanto), automatica o a mano. Senza
// righe: false. Lo stato dei backup ne fa i problemi «prova remota fallita» (non riuscita) e «prova remota mancante»
// (troppo vecchia); una prova a mano riuscita dopo spegne il primo.
func UltimaProvaRemota(ctx context.Context, q Interrogatore) (ProvaRemota, bool, error) {
	var p ProvaRemota
	var inizio, fine, esito string
	var codice, errore, archivio, sha, catturato sql.NullString
	var byte sql.NullInt64
	var turno string
	err := q.QueryRowContext(ctx, `SELECT id, avvio, mese, turno, iniziato_il, finito_il, esito, codice, errore, archivio,
		sha256, byte, catturato_il, media_rovinati FROM prove_remote ORDER BY id DESC LIMIT 1`).
		Scan(&p.ID, &p.Avvio, &p.Mese, &turno, &inizio, &fine, &esito, &codice, &errore, &archivio, &sha, &byte, &catturato,
			&p.MediaRovinati)
	if errors.Is(err, sql.ErrNoRows) {
		return ProvaRemota{}, false, nil
	}
	if err != nil {
		return ProvaRemota{}, false, fmt.Errorf("registro delle prove della copia remota: %w", err)
	}
	g, err := time.Parse(time.DateOnly, turno)
	if err != nil {
		return ProvaRemota{}, false, fmt.Errorf("registro delle prove della copia remota: turno: %w", err)
	}
	p.Turno = clock.Data{Anno: g.Year(), Mese: g.Month(), Giorno: g.Day()}
	if p.IniziatoIl, err = time.Parse(FormatoIstante, inizio); err != nil {
		return ProvaRemota{}, false, fmt.Errorf("registro delle prove della copia remota: inizio: %w", err)
	}
	if p.FinitoIl, err = time.Parse(FormatoIstante, fine); err != nil {
		return ProvaRemota{}, false, fmt.Errorf("registro delle prove della copia remota: fine: %w", err)
	}
	if catturato.Valid {
		if p.CatturatoIl, err = time.Parse(FormatoIstante, catturato.String); err != nil {
			return ProvaRemota{}, false, fmt.Errorf("registro delle prove della copia remota: ora di cattura: %w", err)
		}
	}
	p.Riuscita, p.Codice, p.Errore, p.Archivio, p.SHA256, p.Byte = esito == "ok", codice.String, errore.String,
		archivio.String, sha.String, byte.Int64
	return p, true, nil
}
