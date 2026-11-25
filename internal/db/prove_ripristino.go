package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Il registro delle prove di ripristino settimanali (slice 125, T5.2, D-15, risposta G del piano): la tabella
// prove_ripristino (migrazione 0023). La prova la fa internal/backup; qui ci sono la scrittura e le letture
// che servono al calendario della prova e all'allarme (126: «prova settimanale fallita»). È qui e non in
// internal/backup come il registro dei backup verificati: la pagina e gli avvisi della 126 la leggono senza
// dipendere dal pacchetto che fa i backup.

// Avvii di una prova, le parole della colonna avvio.
const (
	AvvioProvaAutomatico = "automatico"
	AvvioProvaAMano      = "a_mano"
)

// maxErroreProva è quanti byte del motivo di una prova fallita entrano nel registro.
const maxErroreProva = 1000

// ErrProvaRipristino è una riga che non si può scrivere nel registro delle prove.
var ErrProvaRipristino = errors.New("registro delle prove di ripristino: riga non valida")

// ProvaRipristino è una riga di prove_ripristino.
type ProvaRipristino struct {
	ID        int64
	Avvio     string
	Settimana clock.Data // la domenica del turno del backup a cui la prova appartiene
	// IniziatoIl, FinitoIl: inizio e fine della prova.
	IniziatoIl, FinitoIl time.Time
	Riuscita             bool
	Errore               string // il motivo, solo se non riuscita
	// Archivio, SHA256, CatturatoIl: l'archivio provato, se il registro dei backup ne aveva uno (in una prova
	// riuscita sempre).
	Archivio    string
	SHA256      string
	CatturatoIl time.Time
	// MediaRovinati è quanti media dell'archivio non hanno lo SHA-256 del loro nome (D-116): non fanno fallire.
	MediaRovinati int
}

// RegistraProvaRipristino scrive una riga nel registro delle prove e ne dà l'id. Una riga senza i campi giusti è
// ErrProvaRipristino e non si scrive. Il motivo si accorcia a maxErroreProva byte.
func RegistraProvaRipristino(ctx context.Context, d *DB, p ProvaRipristino) (int64, error) {
	esito, errore := "ok", sql.NullString{}
	if !p.Riuscita {
		esito = "fallita"
		e := strings.TrimSpace(p.Errore)
		if e == "" {
			return 0, fmt.Errorf("%w: prova fallita senza motivo", ErrProvaRipristino)
		}
		if len(e) > maxErroreProva {
			e = strings.ToValidUTF8(e[:maxErroreProva], "")
		}
		errore = sql.NullString{String: e, Valid: true}
	}
	var archivio, sha, catturato sql.NullString
	switch {
	case p.Avvio != AvvioProvaAutomatico && p.Avvio != AvvioProvaAMano:
		return 0, fmt.Errorf("%w: avvio %q", ErrProvaRipristino, p.Avvio)
	case p.Settimana == (clock.Data{}):
		return 0, fmt.Errorf("%w: settimana mancante", ErrProvaRipristino)
	case p.IniziatoIl.IsZero() || p.FinitoIl.IsZero():
		return 0, fmt.Errorf("%w: istanti mancanti", ErrProvaRipristino)
	case p.MediaRovinati < 0:
		return 0, fmt.Errorf("%w: media rovinati %d", ErrProvaRipristino, p.MediaRovinati)
	}
	if p.Archivio != "" {
		if !filepath.IsAbs(p.Archivio) || filepath.Clean(p.Archivio) != p.Archivio {
			return 0, fmt.Errorf("%w: percorso dell'archivio non assoluto", ErrProvaRipristino)
		}
		if !impronta(p.SHA256) || p.CatturatoIl.IsZero() {
			return 0, fmt.Errorf("%w: archivio senza SHA-256 o ora di cattura", ErrProvaRipristino)
		}
		archivio = sql.NullString{String: p.Archivio, Valid: true}
		sha = sql.NullString{String: p.SHA256, Valid: true}
		catturato = sql.NullString{String: p.CatturatoIl.UTC().Format(FormatoIstante), Valid: true}
	} else if p.Riuscita {
		return 0, fmt.Errorf("%w: prova riuscita senza archivio", ErrProvaRipristino)
	}
	r, err := d.Scrittura.ExecContext(ctx, `INSERT INTO prove_ripristino
		(avvio, settimana, iniziato_il, finito_il, esito, errore, archivio, sha256, catturato_il, media_rovinati)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Avvio, p.Settimana.String(), p.IniziatoIl.UTC().Format(FormatoIstante), p.FinitoIl.UTC().Format(FormatoIstante),
		esito, errore, archivio, sha, catturato, p.MediaRovinati)
	if err != nil {
		return 0, fmt.Errorf("registro delle prove di ripristino: %w", err)
	}
	return r.LastInsertId()
}

// TentativiProva sono le prove di una settimana con un avvio: quante, quante riuscite, e la fine dell'ultima (zero
// senza righe). Servono al calendario della prova (internal/backup).
func TentativiProva(ctx context.Context, q Interrogatore, avvio string, settimana clock.Data) (tutte, riuscite int, ultima time.Time, err error) {
	var fine sql.NullString
	err = q.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE esito = 'ok'), max(finito_il)
		FROM prove_ripristino WHERE avvio = ? AND settimana = ?`, avvio, settimana.String()).Scan(&tutte, &riuscite, &fine)
	if err != nil {
		return 0, 0, time.Time{}, fmt.Errorf("registro delle prove di ripristino: %w", err)
	}
	if fine.Valid {
		if ultima, err = time.Parse(FormatoIstante, fine.String); err != nil {
			return 0, 0, time.Time{}, fmt.Errorf("registro delle prove di ripristino: fine dell'ultima prova: %w", err)
		}
	}
	return tutte, riuscite, ultima, nil
}

// UltimaProvaRipristino è l'ultima prova scritta (per id: le righe si aggiungono soltanto), automatica o a mano.
// Senza righe: false.
func UltimaProvaRipristino(ctx context.Context, q Interrogatore) (ProvaRipristino, bool, error) {
	var p ProvaRipristino
	var settimana, inizio, fine, esito string
	var errore, archivio, sha, catturato sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, avvio, settimana, iniziato_il, finito_il, esito, errore, archivio, sha256,
		catturato_il, media_rovinati FROM prove_ripristino ORDER BY id DESC LIMIT 1`).
		Scan(&p.ID, &p.Avvio, &settimana, &inizio, &fine, &esito, &errore, &archivio, &sha, &catturato, &p.MediaRovinati)
	if errors.Is(err, sql.ErrNoRows) {
		return ProvaRipristino{}, false, nil
	}
	if err != nil {
		return ProvaRipristino{}, false, fmt.Errorf("registro delle prove di ripristino: %w", err)
	}
	g, err := time.Parse(time.DateOnly, settimana)
	if err != nil {
		return ProvaRipristino{}, false, fmt.Errorf("registro delle prove di ripristino: settimana: %w", err)
	}
	p.Settimana = clock.Data{Anno: g.Year(), Mese: g.Month(), Giorno: g.Day()}
	if p.IniziatoIl, err = time.Parse(FormatoIstante, inizio); err != nil {
		return ProvaRipristino{}, false, fmt.Errorf("registro delle prove di ripristino: inizio: %w", err)
	}
	if p.FinitoIl, err = time.Parse(FormatoIstante, fine); err != nil {
		return ProvaRipristino{}, false, fmt.Errorf("registro delle prove di ripristino: fine: %w", err)
	}
	if catturato.Valid {
		if p.CatturatoIl, err = time.Parse(FormatoIstante, catturato.String); err != nil {
			return ProvaRipristino{}, false, fmt.Errorf("registro delle prove di ripristino: ora di cattura: %w", err)
		}
	}
	p.Riuscita, p.Errore, p.Archivio, p.SHA256 = esito == "ok", errore.String, archivio.String, sha.String
	return p, true, nil
}

// ProvaSettimanaleFallita è lo stato dell'allarme «prova settimanale fallita» (risposta H, banner e avvisi della
// 126): l'ultima prova scritta, automatica o a mano, non è riuscita. Dà quella prova (il motivo è in Errore).
// Una prova riuscita dopo, anche a mano (gestrip prova-backup), spegne l'allarme. Senza prove: false (che la prova
// non sia mai stata fatta lo dice la 126 con UltimaProvaRipristino).
func ProvaSettimanaleFallita(ctx context.Context, q Interrogatore) (ProvaRipristino, bool, error) {
	p, ok, err := UltimaProvaRipristino(ctx, q)
	if err != nil || !ok || p.Riuscita {
		return ProvaRipristino{}, false, err
	}
	return p, true, nil
}
