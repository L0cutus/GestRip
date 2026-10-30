package db

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/L0cutus/gestrip/internal/clock"
)

// LunghezzaChiaveServer è la lunghezza della chiave del server, in byte.
const LunghezzaChiaveServer = 32

// TabelleSegrete sono le tabelle che un export, una copia per prove o una pagina d'elenco non
// devono mai portare con sé (la chiave del server firma sessioni e CSRF).
var TabelleSegrete = []string{"segreti"}

// ChiaveServer dà la chiave del server. Si crea alla prima partenza, in una transazione di
// scrittura (più avvii insieme ottengono la stessa), e resta nella tabella segreti: è la stessa
// tra i riavvii e viaggia con il backup cifrato, che quindi è una credenziale. Una chiave
// rovinata è un errore e resta com'è: rigenerarla in silenzio invaliderebbe sessioni e moduli
// aperti. Il programma passa alle facce solo sottochiavi (SottoChiave), mai questa.
func (d *DB) ChiaveServer(ctx context.Context, o clock.Orologio) ([]byte, error) {
	return d.chiaveServer(ctx, o, false)
}

// RinnovaChiaveServer sostituisce la chiave del server: per un ripristino isolato, una macchina
// di prova o una chiave compromessa. Sessioni e moduli firmati prima non valgono più.
func (d *DB) RinnovaChiaveServer(ctx context.Context, o clock.Orologio) ([]byte, error) {
	return d.chiaveServer(ctx, o, true)
}

func (d *DB) chiaveServer(ctx context.Context, o clock.Orologio, rinnova bool) ([]byte, error) {
	tx, err := d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if !rinnova {
		var k []byte
		err = tx.QueryRowContext(ctx, `SELECT valore FROM segreti WHERE nome = 'chiave_server'`).Scan(&k)
		if err == nil {
			if len(k) != LunghezzaChiaveServer {
				return nil, errors.New("chiave del server rovinata nella tabella segreti: non la rigenero (sessioni e moduli aperti non varrebbero più); ripristinare il backup o rinnovarla di proposito")
			}
			return k, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	k := make([]byte, LunghezzaChiaveServer)
	_, _ = rand.Read(k) // crypto/rand.Read non fallisce (in caso estremo il processo si ferma)
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO segreti (nome, valore, creato_il) VALUES ('chiave_server', ?, ?)`,
		k, o.Adesso().UTC().Format(FormatoIstante)); err != nil {
		return nil, fmt.Errorf("salvataggio della chiave del server: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return k, nil
}

// SottoChiave dà una chiave per uno scopo («csrf», «sessione»…) dalla chiave del server: un
// segreto per ogni uso, così una firma di un tipo non vale mai per un altro.
func SottoChiave(chiave []byte, scopo string) []byte {
	m := hmac.New(sha256.New, chiave)
	m.Write([]byte("gestrip sottochiave " + scopo))
	return m.Sum(nil)
}
