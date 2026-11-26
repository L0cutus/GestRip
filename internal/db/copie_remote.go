package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// Le copie remote dei backup (slice 128, T5.2, OPS-16 destinazione remota): la tabella copie_remote (migrazione
// 0024), una riga per ogni copia arrivata in fondo, e le letture del registro dei backup verificati per la
// destinazione «sftp». La copia la fa internal/backup; qui ci sono la scrittura e le letture per lo stato dei backup
// (126) e per la rotazione remota. Come il registro dei backup verificati, le righe «sftp» non sbloccano mai niente
// (risposta F del piano): BackupVerificatoDopo, BloccatoDopoImport e UltimoVerificatoSullaChiavetta guardano solo
// la chiavetta.

// Esiti di una copia remota, le parole della colonna esito.
const (
	EsitoCopiaOK      = "ok"
	EsitoCopiaAvviso  = "avviso"
	EsitoCopiaFallita = "fallita"
)

// CodiciCopiaRemota sono i codici ammessi nella colonna codice (lo stesso elenco del CHECK della 0024).
var CodiciCopiaRemota = []string{"configurazione", "chiave", "rete", "impronta", "accesso", "cartella", "spazio",
	"esistente", "caricamento", "controllo", "registro", "rotazione", "altro"}

// maxErroreCopia è quanti byte del motivo entrano nella riga.
const maxErroreCopia = 1000

// ErrCopiaRemota è una riga che non si può scrivere in copie_remote.
var ErrCopiaRemota = errors.New("copie remote: riga non valida")

// CopiaRemota è una riga di copie_remote.
type CopiaRemota struct {
	ID                   int64
	Avvio                string // AvvioProvaAutomatico o AvvioProvaAMano: le stesse parole
	IniziatoIl, FinitoIl time.Time
	Esito                string
	Codice               string // "" per ok
	Errore               string // "" per ok
	Archivio             string // il percorso sul server, "" se il caricamento non è arrivato alla rinomina
}

// RegistraCopiaRemota scrive una riga di copie_remote e ne dà l'id. Una riga senza i campi giusti è ErrCopiaRemota
// e non si scrive; il motivo si accorcia a maxErroreCopia byte.
func RegistraCopiaRemota(ctx context.Context, d *DB, c CopiaRemota) (int64, error) {
	var codice, errore, archivio sql.NullString
	switch c.Esito {
	case EsitoCopiaOK:
		if c.Codice != "" || c.Errore != "" {
			return 0, fmt.Errorf("%w: una copia riuscita non ha codice né motivo", ErrCopiaRemota)
		}
	case EsitoCopiaAvviso, EsitoCopiaFallita:
		ok := false
		for _, k := range CodiciCopiaRemota {
			ok = ok || k == c.Codice
		}
		e := strings.TrimSpace(c.Errore)
		if !ok || e == "" {
			return 0, fmt.Errorf("%w: codice %q o motivo mancante", ErrCopiaRemota, c.Codice)
		}
		if len(e) > maxErroreCopia {
			e = strings.ToValidUTF8(e[:maxErroreCopia], "")
		}
		codice, errore = sql.NullString{String: c.Codice, Valid: true}, sql.NullString{String: e, Valid: true}
	default:
		return 0, fmt.Errorf("%w: esito %q", ErrCopiaRemota, c.Esito)
	}
	if c.Avvio != AvvioProvaAutomatico && c.Avvio != AvvioProvaAMano {
		return 0, fmt.Errorf("%w: avvio %q", ErrCopiaRemota, c.Avvio)
	}
	if c.IniziatoIl.IsZero() || c.FinitoIl.IsZero() {
		return 0, fmt.Errorf("%w: istanti mancanti", ErrCopiaRemota)
	}
	if c.Archivio != "" {
		if !path.IsAbs(c.Archivio) || path.Clean(c.Archivio) != c.Archivio {
			return 0, fmt.Errorf("%w: percorso remoto non assoluto", ErrCopiaRemota)
		}
		archivio = sql.NullString{String: c.Archivio, Valid: true}
	}
	r, err := d.Scrittura.ExecContext(ctx, `INSERT INTO copie_remote
		(avvio, iniziato_il, finito_il, esito, codice, errore, archivio) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Avvio, c.IniziatoIl.UTC().Format(FormatoIstante), c.FinitoIl.UTC().Format(FormatoIstante), c.Esito, codice,
		errore, archivio)
	if err != nil {
		return 0, fmt.Errorf("copie remote: %w", err)
	}
	return r.LastInsertId()
}

// UltimaCopiaRemota è l'ultima riga di copie_remote (per id: le righe si aggiungono soltanto). Senza righe: false.
func UltimaCopiaRemota(ctx context.Context, q Interrogatore) (CopiaRemota, bool, error) {
	return copiaRemota(ctx, q, `SELECT id, avvio, iniziato_il, finito_il, esito, coalesce(codice, ''),
		coalesce(errore, ''), coalesce(archivio, '') FROM copie_remote ORDER BY id DESC LIMIT 1`)
}

// UltimaCopiaRemotaRiuscita è l'ultima copia con l'archivio sul server e nel registro ('ok' o 'avviso': la
// rotazione non riuscita non toglie la copia). Senza: false.
func UltimaCopiaRemotaRiuscita(ctx context.Context, q Interrogatore) (CopiaRemota, bool, error) {
	return copiaRemota(ctx, q, `SELECT id, avvio, iniziato_il, finito_il, esito, coalesce(codice, ''),
		coalesce(errore, ''), coalesce(archivio, '') FROM copie_remote WHERE esito IN ('ok', 'avviso')
		ORDER BY id DESC LIMIT 1`)
}

func copiaRemota(ctx context.Context, q Interrogatore, query string) (CopiaRemota, bool, error) {
	var c CopiaRemota
	var inizio, fine string
	err := q.QueryRowContext(ctx, query).Scan(&c.ID, &c.Avvio, &inizio, &fine, &c.Esito, &c.Codice, &c.Errore, &c.Archivio)
	if errors.Is(err, sql.ErrNoRows) {
		return CopiaRemota{}, false, nil
	}
	if err != nil {
		return CopiaRemota{}, false, fmt.Errorf("copie remote: %w", err)
	}
	if c.IniziatoIl, err = time.Parse(FormatoIstante, inizio); err != nil {
		return CopiaRemota{}, false, fmt.Errorf("copie remote: inizio: %w", err)
	}
	if c.FinitoIl, err = time.Parse(FormatoIstante, fine); err != nil {
		return CopiaRemota{}, false, fmt.Errorf("copie remote: fine: %w", err)
	}
	return c, true, nil
}

// ArchiviRemotiNellaCartella dà le righe «sftp» del registro dei backup verificati (di ogni esito) il cui archivio
// sta direttamente nella cartella remota (percorso assoluto e pulito, con le barre del server): la rotazione remota
// (slice 128) cancella solo archivi che il registro conosce come copie remote. Le righe della chiavetta e della
// cartella non contano qui, e le righe «sftp» non contano nella rotazione della chiavetta (ArchiviNellaCartella).
func ArchiviRemotiNellaCartella(ctx context.Context, q Elencatore, cartella string) ([]ArchivioRegistrato, error) {
	if !path.IsAbs(cartella) || path.Clean(cartella) != cartella {
		return nil, fmt.Errorf("%w: cartella remota non assoluta", ErrRegistroBackup)
	}
	rr, err := q.QueryContext(ctx, `SELECT id, esito, destinazione, archivio, sha256, byte FROM verifiche_backup
		WHERE destinazione = ? ORDER BY id`, DestinazioneSFTP)
	if err != nil {
		return nil, fmt.Errorf("registro dei backup: %w", err)
	}
	defer func() { _ = rr.Close() }()
	var v []ArchivioRegistrato
	for rr.Next() {
		var a ArchivioRegistrato
		var esito string
		if err := rr.Scan(&a.ID, &esito, &a.Destinazione, &a.Archivio, &a.SHA256, &a.Byte); err != nil {
			return nil, fmt.Errorf("registro dei backup: %w", err)
		}
		a.Riuscita = esito == "ok"
		if path.Dir(a.Archivio) == cartella {
			v = append(v, a)
		}
	}
	if err := rr.Err(); err != nil {
		return nil, fmt.Errorf("registro dei backup: %w", err)
	}
	return v, nil
}

// UltimoVerificatoRemoto è l'ultima riga 'ok' «sftp» del registro per ora di cattura (come
// UltimoVerificatoSullaChiavetta per la chiavetta): la rotazione remota parte solo se è proprio la copia appena
// fatta. Non sblocca niente. Senza righe: false.
func UltimoVerificatoRemoto(ctx context.Context, q Interrogatore) (ArchivioRegistrato, bool, error) {
	a := ArchivioRegistrato{Riuscita: true, Destinazione: DestinazioneSFTP}
	var catturato string
	err := q.QueryRowContext(ctx, `SELECT id, archivio, sha256, byte, catturato_il FROM verifiche_backup
		WHERE esito = 'ok' AND destinazione = ? ORDER BY catturato_il DESC, id DESC LIMIT 1`,
		DestinazioneSFTP).Scan(&a.ID, &a.Archivio, &a.SHA256, &a.Byte, &catturato)
	if errors.Is(err, sql.ErrNoRows) {
		return ArchivioRegistrato{}, false, nil
	}
	if err != nil {
		return ArchivioRegistrato{}, false, fmt.Errorf("registro dei backup: %w", err)
	}
	if a.CatturatoIl, err = time.Parse(FormatoIstante, catturato); err != nil {
		return ArchivioRegistrato{}, false, fmt.Errorf("registro dei backup: ora di cattura: %w", err)
	}
	return a, true, nil
}
