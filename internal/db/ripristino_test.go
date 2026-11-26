package db

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Lo stato isolato di un database ripristinato (slice 130). Tutto inventato.

// conta dà le righe di una tabella.
func conta(t *testing.T, d *DB, tabella string) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM ` + tabella).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func impostazione(t *testing.T, d *DB, k string) (string, bool) {
	t.Helper()
	var v string
	err := d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, k).Scan(&v)
	if err != nil {
		return "", false
	}
	return v, true
}

func segreto(t *testing.T, d *DB, nome string) []byte {
	t.Helper()
	var v []byte
	if err := d.Lettura.QueryRow(`SELECT valore FROM segreti WHERE nome = ?`, nome).Scan(&v); err != nil {
		return nil
	}
	return v
}

// Voce OPS-19, in parte (la conferma della 131 manca): niente segno «inventario:».
// IsolaRipristino spegne e mette da parte gli interruttori e la copia remota, toglie lo stato degli avvisi, svuota
// i cinque registri dei backup, rinnova la chiave del server tenendo da parte la vecchia e scrive la riga dello
// stato isolato, che LeggiIsolamento rilegge; il resto delle impostazioni non si tocca. Un secondo ripristino di un
// backup fatto dalla copia isolata tiene i valori e la chiave della macchina vera.
func TestIsolaRipristino(t *testing.T) {
	d := registroProva(t)
	o := clock.NuovoFinto(time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC))
	vecchia, err := d.ChiaveServer(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	ora := "2026-11-20T02:30:00.000000Z"
	for _, q := range []string{
		`INSERT INTO impostazioni VALUES ('canale_email_acceso', '1', '` + ora + `'), ('canale_telegram_acceso', '1', '` + ora + `'),
			('backup.remoto', '{"host":"sftp.example.invalid"}', '` + ora + `'), ('backup.avvisi', '{}', '` + ora + `'),
			('jobs.giorni_apertura', '1,2,3', '` + ora + `')`,
	} {
		if _, err := d.Scrittura.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	a, sha, n := archivioProva(t, "archivio inventato")
	if err := RegistraVerificaBackup(ctx, d, VerificaBackup{Riuscita: true, Destinazione: DestinazioneChiavetta, Archivio: a, SHA256: sha,
		Byte: n, CatturatoIl: time.Date(2026, 11, 20, 2, 30, 0, 0, time.UTC), VerificatoIl: time.Date(2026, 11, 20, 2, 31, 0, 0, time.UTC),
		Conteggi: map[string]int64{"buste": 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := RegistraProvaRemota(ctx, d, provaRemotaRiuscita(AvvioProvaAutomatico, time.Date(2026, 12, 6, 1, 50, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LeggiIsolamento(ctx, d.Lettura); ok || err != nil {
		t.Fatalf("isolato prima del ripristino: %v %v", ok, err)
	}
	spegni := []string{"canale_email_acceso", "canale_telegram_acceso", "backup.remoto", "non.cera"}
	iso, err := IsolaRipristino(ctx, d, o, Isolamento{Archivio: "gestrip-backup-20261120T023000Z.tar.age", SHA256: sha,
		CatturatoIl: ora}, spegni, []string{"backup.avvisi"})
	if err != nil {
		t.Fatal(err)
	}
	letto, ok, err := LeggiIsolamento(ctx, d.Lettura)
	if !ok || err != nil || letto.RipristinatoIl != "2026-11-26T10:00:00.000000Z" || letto.Archivio != iso.Archivio || letto.Formato != 1 ||
		len(letto.Spento) != 3 || letto.Spento["canale_email_acceso"] != "1" || letto.Spento["backup.remoto"] != `{"host":"sftp.example.invalid"}` {
		t.Fatalf("stato isolato %+v %v %v", letto, ok, err)
	}
	for _, k := range []string{"canale_email_acceso", "canale_telegram_acceso", "backup.remoto", "backup.avvisi"} {
		if _, c := impostazione(t, d, k); c {
			t.Errorf("%s ancora nelle impostazioni", k)
		}
	}
	if v, _ := impostazione(t, d, "jobs.giorni_apertura"); v != "1,2,3" {
		t.Errorf("giorni di apertura toccati: %q", v)
	}
	for _, tab := range TabelleSvuotateDalRipristino {
		if n := conta(t, d, tab); n != 0 {
			t.Errorf("%s: %d righe", tab, n)
		}
	}
	nuova, err := d.ChiaveServer(ctx, o)
	if err != nil || bytes.Equal(nuova, vecchia) || len(nuova) != LunghezzaChiaveServer {
		t.Fatalf("chiave del server non rinnovata: %v", err)
	}
	if !bytes.Equal(segreto(t, d, NomeChiaveServerDelBackup), vecchia) {
		t.Error("la chiave del server del backup non è stata tenuta da parte")
	}

	// Un backup fatto dalla copia ancora isolata, ripristinato di nuovo: valori e chiave della macchina vera restano.
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni VALUES ('canale_email_acceso', '0', '` + ora + `')`); err != nil {
		t.Fatal(err)
	}
	if _, err := IsolaRipristino(ctx, d, o, Isolamento{Archivio: "altro.tar.age"}, spegni, nil); err != nil {
		t.Fatal(err)
	}
	letto, _, _ = LeggiIsolamento(ctx, d.Lettura)
	if letto.Archivio != "altro.tar.age" || letto.Spento["canale_email_acceso"] != "1" || letto.Spento["backup.remoto"] == "" {
		t.Errorf("secondo ripristino: %+v", letto)
	}
	if !bytes.Equal(segreto(t, d, NomeChiaveServerDelBackup), vecchia) {
		t.Error("il secondo ripristino ha perso la chiave della macchina vera")
	}
}

// Una riga dello stato isolato rovinata, di un formato sconosciuto o con campi in più è ErrIsolamento (serve non
// parte): mai «non isolato».
func TestLeggiIsolamentoRovinato(t *testing.T) {
	d := registroProva(t)
	for _, v := range []string{
		`non json`,
		`{"formato":2,"ripristinato_il":"2026-11-26T10:00:00.000000Z","archivio":"","sha256":"","catturato_il":"","spento":{}}`,
		`{"formato":1,"ripristinato_il":"ieri","archivio":"","sha256":"","catturato_il":"","spento":{}}`,
		`{"formato":1,"ripristinato_il":"2026-11-26T10:00:00.000000Z","archivio":"","sha256":"","catturato_il":"","spento":{},"isolato":false}`,
		`{"formato":1,"ripristinato_il":"2026-11-26T10:00:00.000000Z","archivio":"","sha256":"","catturato_il":"","spento":{}} {}`,
	} {
		if _, err := d.Scrittura.Exec(`INSERT OR REPLACE INTO impostazioni VALUES (?, ?, '2026-11-26T10:00:00.000000Z')`, ChiaveIsolamento, v); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := LeggiIsolamento(ctx, d.Lettura); !ok || !errors.Is(err, ErrIsolamento) {
			t.Errorf("%s: %v %v", strings.SplitN(v, ",", 2)[0], ok, err)
		}
	}
}
