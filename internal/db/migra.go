package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/clock"
)

// FormatoIstante è la forma degli istanti salvati come testo: sempre UTC, microsecondi.
const FormatoIstante = "2006-01-02T15:04:05.000000Z"

//go:embed migrazioni/*.sql
var migrazioniProdotto embed.FS

var reMigrazione = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

type migrazione struct {
	versione int
	nome     string
	sql      string
	impronta string // sha256 del file, in esadecimale
}

// istruzioniVietate sono le istruzioni che, all'inizio di uno statement, romperebbero la
// transazione della migrazione (COMMIT, BEGIN…), cambierebbero il collegamento (PRAGMA:
// foreign_keys dentro una transazione non ha effetto e farebbe credere il contrario) o
// toccherebbero altri file.
var istruzioniVietate = map[string]bool{
	"BEGIN": true, "COMMIT": true, "ROLLBACK": true, "SAVEPOINT": true, "RELEASE": true,
	"PRAGMA": true, "VACUUM": true, "ATTACH": true, "DETACH": true,
}

// controllaIstruzioni guarda la prima parola di ogni statement, saltando commenti, stringhe e
// identificatori tra virgolette. Dentro un CREATE TRIGGER il BEGIN…END del corpo è permesso;
// un END fuori da un trigger (END TRANSACTION) no.
func controllaIstruzioni(sql string) error {
	var pulito strings.Builder
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '-' && strings.HasPrefix(sql[i:], "--"):
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			pulito.WriteByte(' ')
		case c == '/' && strings.HasPrefix(sql[i:], "/*"):
			j := strings.Index(sql[i+2:], "*/")
			if j < 0 {
				return errors.New("commento /* senza chiusura")
			}
			i += j + 3
			pulito.WriteByte(' ')
		case c == '\'' || c == '"' || c == '`' || c == '[':
			fine := c
			if c == '[' {
				fine = ']'
			}
			j := i + 1
			for ; j < len(sql); j++ {
				// Un '' dentro una stringa si legge come la fine di una stringa e l'inizio di
				// un'altra: per trovare dove finisce il testo è lo stesso.
				if sql[j] == fine {
					break
				}
			}
			if j >= len(sql) {
				return errors.New("stringa o identificatore senza chiusura")
			}
			i = j
			pulito.WriteString(" x ")
		default:
			pulito.WriteByte(c)
		}
	}
	nelTrigger := false
	for _, st := range strings.Split(pulito.String(), ";") {
		parole := strings.Fields(strings.ToUpper(st))
		if len(parole) == 0 {
			continue
		}
		switch {
		case parole[0] == "END" && nelTrigger:
			nelTrigger = false
		case parole[0] == "END" || istruzioniVietate[parole[0]]:
			return fmt.Errorf("istruzione %s vietata in una migrazione (romperebbe la transazione o il collegamento)", parole[0])
		case parole[0] == "CREATE" && (slices.Contains(parole[1:min(3, len(parole))], "TRIGGER")):
			nelTrigger = true
		}
	}
	return nil
}

// leggiMigrazioni legge i file NNNN_nome.sql: numerati da 0001 senza buchi, nessun altro file,
// nessun file vuoto.
func leggiMigrazioni(fsys fs.FS) ([]migrazione, error) {
	voci, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var ms []migrazione
	for _, v := range voci { // ReadDir le dà in ordine di nome
		m := reMigrazione.FindStringSubmatch(v.Name())
		if m == nil {
			return nil, fmt.Errorf("migrazioni: file inatteso %q (atteso NNNN_nome.sql)", v.Name())
		}
		n, _ := strconv.Atoi(m[1])
		if n != len(ms)+1 {
			return nil, fmt.Errorf("migrazioni: %s fuori sequenza, attesa la %04d", v.Name(), len(ms)+1)
		}
		b, err := fs.ReadFile(fsys, v.Name())
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(b)) == "" {
			return nil, fmt.Errorf("migrazioni: %s è vuota", v.Name())
		}
		if err := controllaIstruzioni(string(b)); err != nil {
			return nil, fmt.Errorf("migrazioni: %s: %w", v.Name(), err)
		}
		h := sha256.Sum256(b)
		ms = append(ms, migrazione{n, m[2], string(b), hex.EncodeToString(h[:])})
	}
	if len(ms) == 0 {
		return nil, errors.New("migrazioni: nessun file")
	}
	return ms, nil
}

// Migra porta il database all'ultima versione delle migrazioni incorporate e dice quali ha
// applicato. Si va solo avanti (PIANO §1.4).
func (d *DB) Migra(ctx context.Context, o clock.Orologio) ([]int, error) {
	sub, err := fs.Sub(migrazioniProdotto, "migrazioni")
	if err != nil {
		return nil, err
	}
	return d.migraDa(ctx, sub, o)
}

// migraDa applica le migrazioni mancanti, ognuna in una sua transazione insieme alla riga di
// schema_migrations: una migrazione che fallisce non lascia niente e ferma le successive.
// Prima di toccare un database che ha già migrazioni fa un solo backup accanto (dello stato
// prima della prima migrazione in sospeso) e, se non riesce, non migra. Rifiuta, senza
// modificarli, un database più nuovo del programma, con una storia diversa da questi file
// (nome, buco o contenuto cambiato), con tabelle ma senza schema_migrations, o dello standalone.
func (d *DB) migraDa(ctx context.Context, fsys fs.FS, o clock.Orologio) ([]int, error) {
	ms, err := leggiMigrazioni(fsys)
	if err != nil {
		return nil, err
	}
	var tabelle, nostra, ecto int
	if err := d.Scrittura.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations'),
		(SELECT count(*) FROM pragma_table_info('schema_migrations') WHERE name = 'versione'),
		(SELECT count(*) FROM pragma_table_info('schema_migrations') WHERE name = 'version')`,
	).Scan(&tabelle, &nostra, &ecto); err != nil {
		return nil, err
	}
	switch {
	case ecto > 0:
		return nil, errors.New("è un database dello standalone (Ecto): si importa, non si migra; non lo tocco")
	}
	var applicate []registrata
	if nostra > 0 {
		if applicate, err = d.applicate(ctx); err != nil {
			return nil, err
		}
	}
	if len(applicate) == 0 && tabelle > 0 {
		return nil, errors.New("il database ha delle tabelle ma nessuna migrazione registrata: non è un database di GestRip, non lo tocco")
	}
	for i, r := range applicate {
		if i >= len(ms) {
			return nil, fmt.Errorf("il database è più nuovo del programma (migrazione %04d, il programma arriva alla %04d): serve la versione che l'ha migrato, o il ripristino del backup", len(applicate), len(ms))
		}
		if r.nome != ms[i].nome {
			return nil, fmt.Errorf("migrazione %04d: nel database si chiama %q, nel programma %q", i+1, r.nome, ms[i].nome)
		}
		if r.impronta != ms[i].impronta {
			return nil, fmt.Errorf("migrazione %04d_%s: il file è cambiato dopo essere stata applicata (sha256 diverso)", i+1, r.nome)
		}
	}
	if _, err := d.Scrittura.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		versione     INTEGER PRIMARY KEY,
		nome         TEXT NOT NULL,
		sha256       TEXT NOT NULL,
		applicata_il TEXT NOT NULL
	) STRICT`); err != nil {
		return nil, err
	}
	da := ms[len(applicate):]
	if len(da) > 0 && len(applicate) > 0 {
		if err := d.backupPrima(ctx, da[0].versione, len(applicate), o); err != nil {
			return nil, err
		}
	}
	var fatte []int
	for _, m := range da {
		if err := d.applica(ctx, m, o); err != nil {
			return fatte, fmt.Errorf("migrazione %04d_%s: %w", m.versione, m.nome, err)
		}
		fatte = append(fatte, m.versione)
	}
	return fatte, nil
}

type registrata struct{ nome, impronta string }

// applicate dà le migrazioni registrate, in ordine; i numeri devono essere 1…n.
func (d *DB) applicate(ctx context.Context) ([]registrata, error) {
	rows, err := d.Scrittura.QueryContext(ctx, `SELECT versione, nome, sha256 FROM schema_migrations ORDER BY versione`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nomi []registrata
	for rows.Next() {
		var v int
		var r registrata
		if err := rows.Scan(&v, &r.nome, &r.impronta); err != nil {
			return nil, err
		}
		if v != len(nomi)+1 {
			return nil, fmt.Errorf("schema_migrations: versione %d fuori sequenza", v)
		}
		nomi = append(nomi, r)
	}
	return nomi, rows.Err()
}

func (d *DB) applica(ctx context.Context, m migrazione, o clock.Orologio) error {
	tx, err := d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (versione, nome, sha256, applicata_il) VALUES (?, ?, ?, ?)`,
		m.versione, m.nome, m.impronta, o.Adesso().UTC().Format(FormatoIstante)); err != nil {
		return err
	}
	return tx.Commit()
}

// backupPrima copia il database a caldo accanto al file, prima della migrazione versione, e
// controlla la copia (integrità e numero di migrazioni registrate). Se qualcosa non va toglie la
// copia, che ha creato lei, e non migra. È il segnaposto del backup prima dell'aggiornamento
// (T5.2, OPS-20).
func (d *DB) backupPrima(ctx context.Context, versione, registrate int, o clock.Orologio) (err error) {
	dest := fmt.Sprintf("%s.prima-di-%04d-%s", d.percorso, versione, o.Adesso().UTC().Format("20060102T150405.000Z"))
	// VACUUM INTO può scrivere sopra un file che esiste già: il nome si riserva prima, creando
	// il file vuoto in modo esclusivo (VACUUM INTO accetta un file vuoto).
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("backup prima della migrazione %04d non riuscito, non migro: %w", versione, err)
	}
	defer func() {
		if err == nil {
			return
		}
		if errRm := os.Remove(dest); errRm != nil {
			err = errors.Join(err, fmt.Errorf("copia non riuscita %s da togliere a mano: %w", dest, errRm))
		}
	}()
	if err := f.Close(); err != nil {
		return fmt.Errorf("backup prima della migrazione %04d non riuscito, non migro: %w", versione, err)
	}
	if _, err := d.Scrittura.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("backup prima della migrazione %04d non riuscito, non migro: %w", versione, err)
	}
	if err := verificaBackup(ctx, dest, registrate); err != nil {
		return fmt.Errorf("backup %s non verificato, non migro: %w", path.Base(dest), err)
	}
	return nil
}

// verificaBackup controlla che la copia sia integra e contenga le migrazioni attese (un file
// vuoto passa quick_check, quindi l'integrità da sola non basta). È una variabile per i test.
// L'errore di una lettura si avvolge con %w: uno stop arrivato qui resta context.Canceled, e
// serve lo riconosce come uno stop (ADM-20).
var verificaBackup = func(ctx context.Context, dest string, attese int) error {
	c, err := sql.Open("sqlite", "file:"+dest+"?mode=ro")
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }() // sola lettura: niente da perdere
	var esito string
	if err := c.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&esito); err != nil {
		return fmt.Errorf("integrità: %w", err)
	}
	if esito != "ok" {
		return fmt.Errorf("integrità %q", esito)
	}
	var n int
	if err := c.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		return fmt.Errorf("migrazioni registrate nella copia: %w", err)
	}
	if n != attese {
		return fmt.Errorf("migrazioni registrate nella copia %d, attese %d", n, attese)
	}
	return nil
}
