package importer

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

var ctx = context.Background()

const (
	istante      = "2026-10-30T09:15:07.000000Z"
	formaIstante = `'[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'`
	formaData    = `'[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'`
)

func orologio() clock.Orologio {
	return clock.NuovoFinto(time.Date(2026, 10, 30, 9, 15, 7, 0, time.UTC))
}

func migrato(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Chiudi(); err != nil {
			t.Error(err)
		}
	})
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	return d
}

// colonneDi traduce i nomi delle colonne di una tabella dello standalone in quelli del Go, per
// ogni destinazione della tabella.
func colonneDi(standalone string) map[string]map[string]string {
	m := map[string]map[string]string{}
	for _, tab := range Mappa {
		if tab.Standalone != standalone || tab.Go == "" {
			continue
		}
		m[tab.Go] = map[string]string{}
		for _, c := range tab.Colonne {
			if c.Go != "" {
				m[tab.Go][c.Standalone] = c.Go
			}
		}
	}
	return m
}

// riga inserisce una riga con i valori dati, nell'ordine dei nomi.
func riga(d *db.DB, tab string, v map[string]any) error {
	cols := chiavi(v)
	args := make([]any, len(cols))
	for i, c := range cols {
		args[i] = v[c]
	}
	_, err := d.Scrittura.Exec(fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, tab, strings.Join(cols, ", "),
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")), args...)
	return err
}

func con(base map[string]any, k string, v any) map[string]any {
	m := map[string]any{}
	for x, y := range base {
		m[x] = y
	}
	if v == nil {
		delete(m, k)
	} else {
		m[k] = v
	}
	return m
}

// I valori di una riga valida per tabella, per il campione dei CHECK.
func righeValide() []struct {
	tab string
	v   map[string]any
} {
	sha := make([]byte, 32)
	sha[0] = 0xab
	chiave := "ab" + strings.Repeat("00", 31)
	return []struct {
		tab string
		v   map[string]any
	}{
		{"clienti", map[string]any{"id": 1, "nome": "Cliente Prova", "telefono": "3330000000", "creato_il": istante, "aggiornato_il": istante}},
		{"oreficerie", map[string]any{"id": 1, "nome": "Negozio"}},
		{"buste", map[string]any{"id": 1, "cliente_id": 1, "nome_cliente": "Cliente Prova", "telefono_cliente": "3330000000",
			"numero": "1/26", "data_accettazione": "2026-10-30", "descrizione": "anello", "hash_token_pubblico": sha,
			"token_pubblico_scade_il": istante, "creato_il": istante, "aggiornato_il": istante}},
		{"foto_buste", map[string]any{"busta_id": 1, "posizione": 1, "chiave": chiave, "nome_file": "a.png", "byte": 10, "sha256": sha,
			"visibile_cliente": 1, "visibile_artigiano": 1, "visibile_oreficeria": 1, "creato_il": istante, "aggiornato_il": istante}},
		{"messaggi_in_uscita", map[string]any{"id": 1, "busta_id": 1, "canale": "email", "destinatario": "customer", "indirizzo": "a@esempio.it",
			"testo": "ciao", "prossimo_tentativo_il": istante, "accodato_il": istante}},
		{"iscrizioni_telegram", map[string]any{"busta_id": 1, "chat_id": 12345, "collegata_il": istante, "creato_il": istante, "aggiornato_il": istante}},
		{"vendite", map[string]any{"id": 1, "data_vendita": "2026-10-30", "canale": "eBay", "descrizione": "orologio", "acquirente_nome": "Acquirente Prova",
			"creato_il": istante, "aggiornato_il": istante}},
		{"esecuzioni_lavori", map[string]any{"lavoro": "overdue", "avvio": "scheduled", "esito": "ok", "eseguito_il": istante, "giorno": "2026-10-30"}},
		{"segreti_provider", map[string]any{"nome": "outgoing.smtp.password", "valore": "prova-non-vera", "aggiornato_il": istante}},
		{"profilo_negozio", map[string]any{"id": 1, "nome": "Negozio Prova", "condizioni_garanzia": "24 mesi", "creato_il": istante, "aggiornato_il": istante}},
		{"immagini_negozio", map[string]any{"id": 1, "tipo": "logo", "chiave": chiave, "nome_file": "logo.png", "tipo_mime": "image/png", "byte": 10,
			"sha256": sha, "importata_il": istante}},
		{"lotti_avviso", map[string]any{"id": 1, "oreficeria_id": 1, "token": "token-lotto-prova", "creato_il": istante, "aggiornato_il": istante}},
		{"eventi_avviso_pronta", map[string]any{"id": 1, "busta_id": 1, "evento": "notified", "messaggio_in_uscita_id": 1, "avvenuto_il": istante,
			"creato_il": istante, "aggiornato_il": istante}},
		{"eventi_preventivo", map[string]any{"id": 1, "busta_id": 1, "evento": "rejected", "parte": "customer", "motivo": "troppo caro",
			"avvenuto_il": istante, "creato_il": istante, "aggiornato_il": istante}},
		{"eventi_firma_consegna", map[string]any{"id": 1, "busta_id": 1, "evento": "captured", "chiave": chiave, "avvenuto_il": istante,
			"creato_il": istante, "aggiornato_il": istante}},
		{"sequenze_documenti_cortesia", map[string]any{"anno": 2026, "ultimo_numero": 1}},
		{"revisioni_vendite", map[string]any{"id": 1, "vendita_id": 1, "numero_revisione": 1, "anno_documento": 2026, "numero_documento": 1,
			"data_vendita": "2026-10-30", "canale": "Sito", "descrizione": "orologio", "mesi_garanzia": 24, "garanzia_dal": "2026-10-30",
			"garanzia_al": "2028-10-30", "acquirente_nome": "Acquirente Prova", "venditore_nome": "Negozio Prova", "condizioni_garanzia": "24 mesi",
			"hash_token_pubblico": sha, "token_pubblico_scade_il": istante, "emessa_il": istante, "creato_il": istante}},
		{"foto_revisioni_vendite", map[string]any{"id": 1, "revisione_vendita_id": 1, "posizione": 1, "chiave": chiave, "nome_file": "a.jpg",
			"tipo_mime": "image/jpeg", "byte": 10, "sha256": sha, "creato_il": istante}},
		{"documenti_storici", map[string]any{"id": 1, "nome_file_flask": "1_26.pdf", "chiave": chiave, "byte": 10, "sha256": sha, "creato_il": istante}},
		{"cattura_import_flask", map[string]any{"id": 1, "byte_sorgente": 10, "sha256_sorgente": sha, "file_accanto_json": "[]", "sha256_cattura": sha, "creato_il": istante}},
		{"corrispondenze_import_flask", map[string]any{"id": 1, "tabella_flask": "riparazioni", "id_flask": "7", "tabella_standalone": "repairs",
			"id_standalone": 1, "creato_il": istante}},
		{"eventi_emergenza", map[string]any{"id": 1, "iniziato_il": istante, "motivo": "registratore guasto", "creato_il": istante, "aggiornato_il": istante}},
		{"righe_emergenza", map[string]any{"evento_emergenza_id": 1, "anno_documento": 2026, "numero": 1, "descrizione": "riparazione",
			"lordo_centesimi": 1220, "aliquota_iva": 22, "imponibile_centesimi": 1000, "iva_centesimi": 220, "pagamento": "contanti", "creato_il": istante}},
	}
}

// I CHECK rifiutano le forme sbagliate su un campione per tipo di tabella: istanti fuori da
// db.FormatoIstante, date non AAAA-MM-GG, booleani diversi da 0 e 1, valori fuori dagli elenchi,
// JSON non valido, regole tra colonne dello standalone (famiglie di stato, ciclo di vita delle
// vendite, consegna a mano WhatsApp, indirizzo di contenuto delle foto, somma dell'IVA) e i tipi
// di STRICT. Le righe valide passano.
func TestICheckRifiutanoLeFormeSbagliate(t *testing.T) {
	d := migrato(t)
	valide := map[string]map[string]any{}
	for _, r := range righeValide() {
		valide[r.tab] = r.v
		if err := riga(d, r.tab, r.v); err != nil {
			t.Fatalf("riga valida di %s rifiutata: %v", r.tab, err)
		}
	}
	// Una riga sbagliata in una sola colonna: senza id e con un suo hash pubblico, così l'unico
	// motivo del rifiuto è quella colonna (il test controlla che non sia un UNIQUE).
	n := byte(0)
	fuori := func(tab, k string, v any) map[string]any {
		m := con(valide[tab], k, v)
		delete(m, "id")
		if tab == "buste" {
			n++
			h := make([]byte, 32)
			h[31] = n
			m["hash_token_pubblico"] = h
		}
		return m
	}
	chiave := "ab" + strings.Repeat("00", 31) // quella delle righe valide
	casi := []struct {
		nome, tab string
		v         map[string]any
	}{
		{"istante con lo spazio", "clienti", fuori("clienti", "creato_il", "2026-10-30 09:15:07")},
		{"istante in millisecondi", "clienti", fuori("clienti", "aggiornato_il", "2026-10-30T09:15:07.000Z")},
		{"istante senza Z", "segreti_provider", con(con(valide["segreti_provider"], "nome", "assist.ai.api_key"), "aggiornato_il", "2026-10-30T09:15:07.000000")},
		{"data italiana", "buste", con(fuori("buste", "data_accettazione", "30/10/2026"), "numero", "2/26")},
		{"data con l'ora", "esecuzioni_lavori", fuori("esecuzioni_lavori", "giorno", istante)},
		{"booleano 2", "iscrizioni_telegram", con(fuori("iscrizioni_telegram", "consenso", 2), "chat_id", 999)},
		{"booleano di attesa 2", "buste", con(fuori("buste", "attesa_cliente", 2), "numero", "3/26")},
		{"stato della famiglia sbagliata", "buste", con(con(fuori("buste", "compro_oro", 1), "stato", "pronta"), "numero", "4/26")},
		{"stato sconosciuto", "buste", con(fuori("buste", "stato", "persa"), "numero", "5/26")},
		{"preventivo negativo", "buste", con(fuori("buste", "preventivo_centesimi", -1), "numero", "6/26")},
		{"codice Flask di 31 byte", "buste", con(fuori("buste", "hash_codice_flask", make([]byte, 31)), "numero", "7/26")},
		{"foto con la chiave sbagliata", "foto_buste", con(fuori("foto_buste", "chiave", strings.Repeat("00", 32)), "posizione", 2)},
		{"foto visibile 2", "foto_buste", con(fuori("foto_buste", "visibile_cliente", 2), "posizione", 3)},
		{"canale sconosciuto", "messaggi_in_uscita", fuori("messaggi_in_uscita", "canale", "sms")},
		{"WhatsApp in coda", "messaggi_in_uscita", fuori("messaggi_in_uscita", "canale", "whatsapp")},
		{"tastiera non JSON", "messaggi_in_uscita", fuori("messaggi_in_uscita", "tastiera", "{")},
		{"canale di vendita sconosciuto", "vendite", fuori("vendite", "canale", "Mercatino")},
		{"vendita emessa senza documento", "vendite", fuori("vendite", "stato", "issued")},
		{"data di vendita corta", "vendite", fuori("vendite", "data_vendita", "2026-1-1")},
		{"lavoro sconosciuto", "esecuzioni_lavori", fuori("esecuzioni_lavori", "lavoro", "pulizia")},
		{"IVA che non torna", "righe_emergenza", con(fuori("righe_emergenza", "iva_centesimi", 221), "numero", 2)},
		{"descrizione di spazi", "righe_emergenza", con(fuori("righe_emergenza", "descrizione", "   "), "numero", 3)},
		{"testo in una colonna intera (STRICT)", "righe_emergenza", con(fuori("righe_emergenza", "numero", "quattro"), "anno_documento", 2027)},
		{"istante NULL dove serve", "clienti", fuori("clienti", "creato_il", nil)},
		{"nome di un segreto vuoto", "segreti_provider", con(valide["segreti_provider"], "nome", "")},
		{"ambito del profilo diverso da default", "profilo_negozio", fuori("profilo_negozio", "ambito", "altro")},
		{"logo con la chiave di un altro contenuto", "immagini_negozio", con(fuori("immagini_negozio", "chiave", strings.Repeat("ab", 32)), "tipo", "icon_192")},
		{"logo di 0 byte", "immagini_negozio", con(fuori("immagini_negozio", "byte", 0), "tipo", "icon_512")},
		{"lotto chiuso senza invio", "lotti_avviso", con(fuori("lotti_avviso", "chiuso_il", istante), "token", "altro-token")},
		{"prezzo negativo", "buste", con(fuori("buste", "prezzo_centesimi", -1), "numero", "8/26")},
		{"foto di 0 byte", "foto_buste", con(fuori("foto_buste", "byte", 0), "posizione", 4)},
		{"tentativi negativi", "messaggi_in_uscita", fuori("messaggi_in_uscita", "tentativi", -1)},
		{"azzeramento con un messaggio", "eventi_avviso_pronta", fuori("eventi_avviso_pronta", "evento", "reset")},
		{"accettazione con un motivo", "eventi_preventivo", fuori("eventi_preventivo", "evento", "approved")},
		{"chiave della firma di 63", "eventi_firma_consegna", fuori("eventi_firma_consegna", "chiave", chiave[:63])},
		{"anno di documento 0", "sequenze_documenti_cortesia", con(valide["sequenze_documenti_cortesia"], "anno", 0)},
		{"data di vendita oltre il 9987", "vendite", fuori("vendite", "data_vendita", "9999-01-01")},
		{"garanzia di 121 mesi", "vendite", fuori("vendite", "mesi_garanzia", 121)},
		{"emessa senza anno e numero", "vendite", con(fuori("vendite", "stato", "issued"), "emessa_il", istante)},
		{"bozza con l'emissione", "vendite", fuori("vendite", "emessa_il", istante)},
		{"bozza con la chiusura", "vendite", fuori("vendite", "data_chiusura", "2026-10-30")},
		{"emessa con l'anno 0", "vendite", con(con(con(fuori("vendite", "stato", "issued"), "emessa_il", istante), "anno_documento", 0), "numero_documento", 5)},
		{"garanzia che finisce prima di cominciare", "revisioni_vendite",
			con(con(con(fuori("revisioni_vendite", "garanzia_al", "2026-10-29"), "numero_revisione", 2), "hash_token_pubblico", make([]byte, 32)), "numero_documento", 2)},
		{"foto di revisione con la chiave di un altro contenuto", "foto_revisioni_vendite",
			con(fuori("foto_revisioni_vendite", "chiave", strings.Repeat("ab", 32)), "posizione", 2)},
		{"IVA di un centesimo in meno", "righe_emergenza", con(fuori("righe_emergenza", "iva_centesimi", 219), "numero", 4)},
		{"avvisi negativi", "esecuzioni_lavori", fuori("esecuzioni_lavori", "avvisi", -1)},
		{"documento storico con la chiave di un altro contenuto", "documenti_storici",
			con(fuori("documenti_storici", "chiave", strings.Repeat("ab", 32)), "nome_file_flask", "2_26.pdf")},
		{"seconda cattura", "cattura_import_flask", con(valide["cattura_import_flask"], "id", 2)},
		{"id dello standalone 0", "corrispondenze_import_flask", con(fuori("corrispondenze_import_flask", "id_standalone", 0), "id_flask", "8")},
		{"sha256 della cattura di 31 byte", "cattura_import_flask", con(valide["cattura_import_flask"], "sha256_cattura", make([]byte, 31))},
	}
	for _, c := range casi {
		err := riga(d, c.tab, c.v)
		switch {
		case err == nil:
			t.Errorf("%s: %s accettata", c.tab, c.nome)
		case strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "FOREIGN KEY"):
			t.Errorf("%s: %s rifiutata per un altro motivo: %v", c.tab, c.nome, err)
		}
	}
}

// Le migrazioni si applicano su un database vuoto, un riavvio non ne applica altre, e con le
// righe del campione chiavi esterne e integrità sono pulite.
func TestMigrazioniSuDatabaseVuotoEDopoIlRiavvio(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	app, err := d.Migra(ctx, orologio())
	if err != nil || len(app) != 12 {
		t.Fatalf("applicate %v, %v", app, err)
	}
	for _, r := range righeValide() {
		if err := riga(d, r.tab, r.v); err != nil {
			t.Fatalf("%s: %v", r.tab, err)
		}
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	d, err = db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Chiudi(); err != nil {
			t.Error(err)
		}
	}()
	if app, err := d.Migra(ctx, orologio()); err != nil || len(app) != 0 {
		t.Fatalf("al riavvio applicate %v, %v; attese nessuna", app, err)
	}
	if n := conta(t, d.Lettura, `SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("foreign_key_check: %d righe", n)
	}
	var esito string
	if err := d.Lettura.QueryRow(`PRAGMA integrity_check`).Scan(&esito); err != nil || esito != "ok" {
		t.Errorf("integrity_check = %q, %v", esito, err)
	}
	// Tutte le tabelle del prodotto sono STRICT.
	rows, err := d.Lettura.Query(`SELECT name FROM pragma_table_list WHERE schema = 'main' AND type = 'table' AND strict = 0 AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != "schema_migrations" {
			t.Errorf("tabella %s non STRICT", n)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func conta(t *testing.T, q *sql.DB, s string) int {
	t.Helper()
	var n int
	if err := q.QueryRow(s).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// I segreti dei provider (D-03) non finiscono tra le impostazioni: vanno in segreti_provider, che
// è in db.TabelleSegrete. Le chiavi sono quelle che lo standalone stesso tiene fuori dai log
// (testdata/chiavi-segrete-standalone.txt, ricavato dal suo sorgente da tools/schema-standalone).
func TestSegretiDeiProviderAParte(t *testing.T) {
	migrato(t)
	if !slices.Contains(db.TabelleSegrete, "segreti_provider") {
		t.Error("segreti_provider non è in db.TabelleSegrete")
	}
	var attese []string
	for _, r := range strings.Split(chiaviSegreteStandalone, "\n") {
		if r = strings.TrimSpace(r); r != "" && !strings.HasPrefix(r, "#") {
			attese = append(attese, r)
		}
	}
	if len(attese) == 0 {
		t.Fatal("testdata/chiavi-segrete-standalone.txt vuoto")
	}
	got := slices.Clone(ChiaviSegreteProvider)
	sort.Strings(got)
	if !slices.Equal(got, attese) {
		t.Errorf("ChiaviSegreteProvider = %s, attese %s", elenco(got), elenco(attese))
	}
	var verso []string
	for _, tab := range Mappa {
		if tab.Standalone == "settings" {
			verso = append(verso, tab.Go)
		}
	}
	sort.Strings(verso)
	if !slices.Equal(verso, []string{"impostazioni", "segreti_provider"}) {
		t.Errorf("settings va in %s, attese impostazioni e segreti_provider", elenco(verso))
	}
}

// I trigger portati in questa slice fanno quello dello standalone: revisioni delle vendite, loro
// foto, sigilli e revoche non si modificano e non si cancellano; una revisione sigillata non prende
// foto; i PDF storici e la cattura del Flask non cambiano. (Il permesso di eliminazione definitiva
// arriva con VEN-18: per ora nessuna eccezione.)
func TestTriggerDiImmutabilita(t *testing.T) {
	d := migrato(t)
	sha := make([]byte, 32)
	sha[0] = 0xcd
	chiave := "cd" + strings.Repeat("00", 31)
	righe := []struct {
		tab string
		v   map[string]any
	}{
		{"vendite", map[string]any{"id": 1, "data_vendita": "2026-10-30", "canale": "Sito", "descrizione": "orologio", "acquirente_nome": "Acquirente Prova",
			"stato": "issued", "anno_documento": 2026, "numero_documento": 1, "emessa_il": istante, "creato_il": istante, "aggiornato_il": istante}},
		{"revisioni_vendite", map[string]any{"id": 1, "vendita_id": 1, "numero_revisione": 1, "anno_documento": 2026, "numero_documento": 1,
			"data_vendita": "2026-10-30", "canale": "Sito", "descrizione": "orologio", "mesi_garanzia": 24, "garanzia_dal": "2026-10-30",
			"garanzia_al": "2028-10-30", "acquirente_nome": "Acquirente Prova", "venditore_nome": "Negozio Prova", "condizioni_garanzia": "24 mesi",
			"hash_token_pubblico": sha, "token_pubblico_scade_il": istante, "emessa_il": istante, "creato_il": istante}},
		{"foto_revisioni_vendite", map[string]any{"id": 1, "revisione_vendita_id": 1, "posizione": 1, "chiave": chiave, "nome_file": "a.jpg",
			"tipo_mime": "image/jpeg", "byte": 10, "sha256": sha, "creato_il": istante}},
		{"sigilli_revisioni_vendite", map[string]any{"id": 1, "revisione_vendita_id": 1, "sigillata_il": istante}},
		{"revoche_revisioni_vendite", map[string]any{"id": 1, "revisione_vendita_id": 1, "motivo": "reso", "revocata_il": istante}},
		{"documenti_storici", map[string]any{"id": 1, "nome_file_flask": "1_26.pdf", "chiave": chiave, "byte": 10, "sha256": sha, "creato_il": istante}},
		{"cattura_import_flask", map[string]any{"id": 1, "byte_sorgente": 10, "sha256_sorgente": sha, "file_accanto_json": "[]", "sha256_cattura": sha, "creato_il": istante}},
	}
	for _, r := range righe {
		if err := riga(d, r.tab, r.v); err != nil {
			t.Fatalf("%s: %v", r.tab, err)
		}
	}
	if err := riga(d, "foto_revisioni_vendite", map[string]any{"revisione_vendita_id": 1, "posizione": 2, "chiave": chiave, "nome_file": "b.jpg",
		"tipo_mime": "image/jpeg", "byte": 10, "sha256": sha, "creato_il": istante}); err == nil || !strings.Contains(err.Error(), "sigillata") {
		t.Errorf("foto su una revisione sigillata: %v", err)
	}
	for _, r := range righe[1:] {
		col := map[string]string{"revisioni_vendite": "descrizione", "foto_revisioni_vendite": "nome_file", "sigilli_revisioni_vendite": "sigillata_il",
			"revoche_revisioni_vendite": "motivo", "documenti_storici": "nome_file_flask", "cattura_import_flask": "creato_il"}[r.tab]
		if _, err := d.Scrittura.Exec(`UPDATE ` + r.tab + ` SET ` + col + ` = ` + col + ` WHERE id = 1`); err == nil || !strings.Contains(err.Error(), "non si modific") {
			t.Errorf("%s modificata: %v", r.tab, err)
		}
		if _, err := d.Scrittura.Exec(`DELETE FROM ` + r.tab + ` WHERE id = 1`); err == nil || !strings.Contains(err.Error(), "non si cancell") {
			t.Errorf("%s cancellata: %v", r.tab, err)
		}
	}
}

// Le regole che valgono tra più righe: una sola foto della busta per busta (le altre no), una
// sola emergenza aperta (dopo la chiusura se ne apre un'altra), un addebito vivo per busta e giro
// (lo storno non conta), la foto di una revisione rifiutata se è sigillata e non se è solo revocata.
func TestVincoliTraPiuRighe(t *testing.T) {
	d := migrato(t)
	for _, r := range righeValide() {
		if err := riga(d, r.tab, r.v); err != nil {
			t.Fatalf("%s: %v", r.tab, err)
		}
	}
	valide := map[string]map[string]any{}
	for _, r := range righeValide() {
		valide[r.tab] = con(r.v, "id", nil)
	}
	passa := func(nome, tab string, v map[string]any) {
		t.Helper()
		if err := riga(d, tab, v); err != nil {
			t.Errorf("%s: rifiutata: %v", nome, err)
		}
	}
	rifiuta := func(nome, tab string, v map[string]any) {
		t.Helper()
		if err := riga(d, tab, v); err == nil {
			t.Errorf("%s: accettata", nome)
		}
	}
	foto := valide["foto_buste"]
	passa("prima foto qualsiasi", "foto_buste", con(foto, "posizione", 10))
	passa("seconda foto qualsiasi", "foto_buste", con(foto, "posizione", 11))
	passa("foto della busta", "foto_buste", con(con(foto, "posizione", 12), "foto_busta", 1))
	rifiuta("seconda foto della busta", "foto_buste", con(con(foto, "posizione", 13), "foto_busta", 1))

	rifiuta("seconda emergenza aperta", "eventi_emergenza", valide["eventi_emergenza"])
	if _, err := d.Scrittura.Exec(`UPDATE eventi_emergenza SET chiuso_il = ? WHERE id = 1`, istante); err != nil {
		t.Fatal(err)
	}
	passa("emergenza dopo la chiusura", "eventi_emergenza", valide["eventi_emergenza"])

	addebito := con(con(valide["righe_emergenza"], "busta_id", 1), "id", 10)
	passa("addebito della busta", "righe_emergenza", con(addebito, "numero", 10))
	passa("storno dell'addebito, stesso giro", "righe_emergenza", con(con(con(addebito, "id", 11), "numero", 11), "storno_di_id", 10))
	rifiuta("secondo addebito vivo nello stesso giro", "righe_emergenza", con(con(addebito, "id", 12), "numero", 12))
	passa("addebito nel giro dopo", "righe_emergenza", con(con(con(addebito, "id", 13), "numero", 13), "giro_addebito", 1))

	rev := valide["revisioni_vendite"]
	h2, h3 := make([]byte, 32), make([]byte, 32)
	h2[1], h3[1] = 2, 3
	passa("revisione 2", "revisioni_vendite", con(con(con(rev, "id", 2), "numero_revisione", 2), "hash_token_pubblico", h2))
	passa("revisione 3", "revisioni_vendite", con(con(con(rev, "id", 3), "numero_revisione", 3), "hash_token_pubblico", h3))
	passa("sigillo della 2", "sigilli_revisioni_vendite", map[string]any{"revisione_vendita_id": 2, "sigillata_il": istante})
	passa("revoca della 3", "revoche_revisioni_vendite", map[string]any{"revisione_vendita_id": 3, "motivo": "reso", "revocata_il": istante})
	fr := valide["foto_revisioni_vendite"]
	rifiuta("foto sulla revisione sigillata", "foto_revisioni_vendite", con(fr, "revisione_vendita_id", 2))
	passa("foto sulla revisione solo revocata", "foto_revisioni_vendite", con(fr, "revisione_vendita_id", 3))
}
