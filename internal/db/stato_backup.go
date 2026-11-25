package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Le letture del registro dei backup per la pagina Impostazioni → Backup e per il banner del titolare (slice 126,
// T5.2, tema E, risposta H). Solo letture piccole, su indici o su tabelle di poche righe al giorno: si fanno a ogni
// pagina del titolare (con la cache breve della faccia), mai un ricalcolo degli archivi.

// RigaRegistroBackup è una riga del registro come la mostra la pagina Backup: niente SHA-256 né conteggi.
type RigaRegistroBackup struct {
	ID                        int64
	Riuscita                  bool
	Destinazione              string
	Archivio                  string
	Byte                      int64
	CatturatoIl, VerificatoIl time.Time
	Errore                    string // il motivo, solo se non riuscita
	MediaRovinati             int
}

// UltimeVerificheBackup sono le ultime limite righe del registro (ogni esito e destinazione), la più recente prima
// (per id: le righe si aggiungono soltanto).
func UltimeVerificheBackup(ctx context.Context, q Elencatore, limite int) ([]RigaRegistroBackup, error) {
	rr, err := q.QueryContext(ctx, `SELECT id, esito, destinazione, archivio, byte, catturato_il, verificato_il,
		coalesce(errore, ''), media_rovinati FROM verifiche_backup ORDER BY id DESC LIMIT ?`, limite)
	if err != nil {
		return nil, fmt.Errorf("registro dei backup: %w", err)
	}
	defer func() { _ = rr.Close() }()
	var v []RigaRegistroBackup
	for rr.Next() {
		var r RigaRegistroBackup
		var esito, catturato, verificato string
		if err := rr.Scan(&r.ID, &esito, &r.Destinazione, &r.Archivio, &r.Byte, &catturato, &verificato, &r.Errore,
			&r.MediaRovinati); err != nil {
			return nil, fmt.Errorf("registro dei backup: %w", err)
		}
		r.Riuscita = esito == "ok"
		if r.CatturatoIl, err = time.Parse(FormatoIstante, catturato); err != nil {
			return nil, fmt.Errorf("registro dei backup: ora di cattura: %w", err)
		}
		if r.VerificatoIl, err = time.Parse(FormatoIstante, verificato); err != nil {
			return nil, fmt.Errorf("registro dei backup: ora di verifica: %w", err)
		}
		v = append(v, r)
	}
	if err := rr.Err(); err != nil {
		return nil, fmt.Errorf("registro dei backup: %w", err)
	}
	return v, nil
}

// IstantiRegistroBackup sono due istanti del registro per l'allarme: l'ora di cattura del primo backup verificato
// sulla chiavetta (la prova settimanale è attesa da lì in poi; zero senza righe) e l'istante più avanti di tutto il
// registro, cattura o verifica di qualunque riga (lo stesso che BackupVerificatoDopo guarda prima di fidarsi
// dell'orologio, nota 8(a); zero senza righe).
func IstantiRegistroBackup(ctx context.Context, q Interrogatore) (primoChiavetta, piuAvanti time.Time, err error) {
	var primo, maxC, maxV sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT
		(SELECT min(catturato_il) FROM verifiche_backup WHERE esito = 'ok' AND destinazione = ?),
		max(catturato_il), max(verificato_il) FROM verifiche_backup`, DestinazioneChiavetta).Scan(&primo, &maxC, &maxV); err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("registro dei backup: %w", err)
	}
	leggi := func(s sql.NullString) (time.Time, error) {
		if !s.Valid {
			return time.Time{}, nil
		}
		t, err := time.Parse(FormatoIstante, s.String)
		if err != nil {
			return time.Time{}, fmt.Errorf("registro dei backup: istante illeggibile: %w", err)
		}
		return t, nil
	}
	if primoChiavetta, err = leggi(primo); err != nil {
		return time.Time{}, time.Time{}, err
	}
	c, err := leggi(maxC)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	v, err := leggi(maxV)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	piuAvanti = c
	if v.After(piuAvanti) {
		piuAvanti = v
	}
	return primoChiavetta, piuAvanti, nil
}
