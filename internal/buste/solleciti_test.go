package buste

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// I solleciti all'artigiano (solleciti.go, slice 083): ArtisanReminders e record_artisan_reminder/4 dello
// standalone (302439d), i casi di artisan_reminders_test.exs. L'orologio è fermo al 31/10/2026 alle 09:00 UTC.

// fuoriDa mette la busta fuori con la consegna dall'istante dato.
func fuoriDa(t *testing.T, d *db.DB, busta, consegna int64, uscita string) {
	t.Helper()
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = ? WHERE id = ?`, consegna, uscita, busta)
}

func scriviSolleciti(s Solleciti) string {
	var b strings.Builder
	fmt.Fprintf(&b, "giorni=%d totale=%d", s.Giorni, s.Totale)
	for _, g := range s.Gruppi {
		tel := "-"
		if g.Telefono != nil {
			tel = *g.Telefono
		}
		ultimo := "-"
		if g.UltimoSollecito != nil {
			ultimo = g.UltimoSollecito.UTC().Format(db.FormatoIstante)
		}
		fmt.Fprintf(&b, "\n%d %s %s ultimo=%s:", g.ArtigianoID, g.Artigiano, tel, ultimo)
		for _, x := range g.Buste {
			u := "-"
			if x.UltimoSollecito != nil {
				u = x.UltimoSollecito.UTC().Format(db.FormatoIstante)
			}
			fmt.Fprintf(&b, " [%d %dgg c%d %s]", x.ID, x.GiorniFuori, x.Consegna, u)
		}
	}
	return b.String()
}

// inventario: ART-20
// La soglia come threshold_days/1: il parametro se non è vuoto, poi l'impostazione, poi 14; un parametro che non
// è un numero dà 14 anche con l'impostazione; zero e negativi diventano 1; Integer.parse/1 per intero.
func TestSogliaSolleciti(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
		ok   bool
	}{
		{"14", 14, true}, {" 7\n", 7, true}, {"+5", 5, true}, {"0", 1, true}, {"-3", 1, true}, {"-0", 1, true},
		{"120", 120, true}, {"365", 365, true}, {"99999999999999999999", 1<<63 - 1, true},
		{"", 0, false}, {"+", 0, false}, {"5.0", 0, false}, {"12abc", 0, false}, {"1e3", 0, false}, {"١٢", 0, false}, {"- 3", 0, false},
	} {
		if got, ok := giorniDaTesto(c.in); got != c.want || ok != c.ok {
			t.Errorf("giorniDaTesto(%q) = %d %v, voluto %d %v", c.in, got, ok, c.want, c.ok)
		}
	}
	b, d, _ := prova(t)
	soglia := func(p string) int64 {
		t.Helper()
		n, err := b.SogliaSolleciti(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if soglia("") != 14 || soglia("abc") != 14 || soglia("3") != 3 {
		t.Fatal("senza impostazione")
	}
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('solleciti_artigiano_giorni', ' 30 ', ?)`, istanteProva)
	if soglia("") != 30 || soglia("  ") != 30 || soglia("abc") != 14 || soglia("3") != 3 || soglia("0") != 1 {
		t.Fatal("con impostazione")
	}
	esegui(t, d, `UPDATE impostazioni SET valore = 'trenta' WHERE chiave = 'solleciti_artigiano_giorni'`)
	if soglia("") != 14 {
		t.Fatal("impostazione illeggibile")
	}
	esegui(t, d, `UPDATE impostazioni SET valore = '-2' WHERE chiave = 'solleciti_artigiano_giorni'`)
	if soglia("") != 1 {
		t.Fatal("impostazione negativa")
	}
}

// inventario: ART-20, ART-07
// Le buste in ritardo: il limite compreso (14 giorni esatti sì, un microsecondo in meno no), i giorni fuori
// troncati, i gruppi per l'artigiano della consegna (non della busta) per nome e poi per uscita; fuori le buste
// rientrate, quelle senza consegna e quelle mai uscite; le buste nel cestino non si escludono (come late_query/3). Il contatore usa
// la soglia dell'impostazione e ignora il parametro.
func TestSollecitiPendenti(t *testing.T) {
	b, d, o := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome, telefono) VALUES (2, 'Bottega Zeta', '+39 333 1234567')`)
	zeta := consegnaIl(t, d, 2, "2026-09-01T08:00:00.000000Z", "")
	esempio := consegnaIl(t, d, 1, "2026-09-01T08:00:00.000000Z", "")
	limite, _ := bustaCon(t, b, d, "")
	sotto, _ := bustaCon(t, b, d, "")
	venti, _ := bustaCon(t, b, d, "")
	rientrata, _ := bustaCon(t, b, d, "")
	senzaConsegna, _ := bustaCon(t, b, d, "")
	cestino, _ := bustaCon(t, b, d, "")
	altroArtigiano, _ := bustaCon(t, b, d, "")
	bustaCon(t, b, d, "")                                                 // mai uscita
	fuoriDa(t, d, limite, zeta, "2026-10-17T09:00:00.000000Z")            // 14 giorni esatti
	fuoriDa(t, d, sotto, zeta, "2026-10-17T09:00:00.000001Z")             // un microsecondo in meno
	fuoriDa(t, d, venti, zeta, "2026-10-10T18:59:59.000000Z")             // 20 giorni e 14 ore: 20
	fuoriDa(t, d, rientrata, zeta, "2026-09-01T09:00:00.000000Z")         //
	fuoriDa(t, d, cestino, esempio, "2026-10-01T09:00:00.000000Z")        // 30 giorni, nel cestino
	fuoriDa(t, d, altroArtigiano, esempio, "2026-10-16T09:00:00.000000Z") // la busta dice Zeta, la consegna Esempio
	esegui(t, d, `UPDATE buste SET rientrata_artigiano_il = '2026-09-02T09:00:00.000000Z' WHERE id = ?`, rientrata)
	esegui(t, d, `UPDATE buste SET consegnata_artigiano_il = '2026-09-01T09:00:00.000000Z' WHERE id = ?`, senzaConsegna)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	esegui(t, d, `UPDATE buste SET artigiano_id = 2 WHERE id = ?`, altroArtigiano)
	s, err := b.SollecitiPendenti(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("giorni=14 totale=4\n1 Artigiano Esempio - ultimo=-: [%d 30gg c%d -] [%d 15gg c%d -]\n2 Bottega Zeta +39 333 1234567 ultimo=-: [%d 20gg c%d -] [%d 14gg c%d -]",
		cestino, esempio, altroArtigiano, esempio, venti, zeta, limite, zeta)
	if got := scriviSolleciti(s); got != want {
		t.Fatalf("elenco:\n%s\nvoluto\n%s", got, want)
	}
	if n, err := b.ContaSollecitiPendenti(ctx); err != nil || n != 4 {
		t.Fatalf("contatore %d %v", n, err)
	}
	// La soglia della pagina: 20 giorni lascia due buste; un testo che non è un numero è 14.
	if s, _ := b.SollecitiPendenti(ctx, "20"); s.Giorni != 20 || s.Totale != 2 {
		t.Fatalf("20: %s", scriviSolleciti(s))
	}
	if s, _ := b.SollecitiPendenti(ctx, "tanti"); s.Giorni != 14 || s.Totale != 4 {
		t.Fatalf("tanti: %s", scriviSolleciti(s))
	}
	if s, _ := b.SollecitiPendenti(ctx, "99999999"); s.Giorni != 99999999 || s.Totale != 0 || s.Gruppi != nil {
		t.Fatalf("oltre: %s", scriviSolleciti(s))
	}
	// Il contatore segue l'impostazione, mai il parametro; un microsecondo dopo «sotto» entra.
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('solleciti_artigiano_giorni', '21', ?)`, istanteProva)
	if n, _ := b.ContaSollecitiPendenti(ctx); n != 1 {
		t.Fatalf("contatore a 21: %d", n)
	}
	esegui(t, d, `DELETE FROM impostazioni WHERE chiave = 'solleciti_artigiano_giorni'`)
	o.Sposta(time.Microsecond)
	if n, _ := b.ContaSollecitiPendenti(ctx); n != 5 {
		t.Fatalf("contatore dopo un microsecondo: %d", n)
	}
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('solleciti_artigiano_giorni', '999999', ?)`, istanteProva)
	if n, _ := b.ContaSollecitiPendenti(ctx); n != 0 {
		t.Fatalf("contatore oltre: %d", n)
	}
}

// inventario: ART-21, ART-22
// Il sollecito rilegge le buste dell'artigiano all'istante della scrittura: una riga solleciti_artigiani e una
// repair.artisan_reminded per busta (artigiano, consegna, giorni fuori, attore); nessuna busta (o un artigiano
// che non c'è) è il rifiuto e niente si scrive. Due tocchi scrivono due solleciti. L'ultimo sollecito torna
// sull'elenco; i solleciti della busta restano dopo il rientro, dal più recente.
func TestRegistraSollecito(t *testing.T) {
	b, d, o := prova(t)
	c := consegnaIl(t, d, 1, "2026-09-01T08:00:00.000000Z", "")
	x, _ := bustaCon(t, b, d, "")
	y, _ := bustaCon(t, b, d, "")
	fuoriDa(t, d, x, c, "2026-10-01T09:00:00.000000Z")
	fuoriDa(t, d, y, c, "2026-10-20T09:00:00.000000Z") // 11 giorni: non con la soglia 14
	conta := func() string {
		var r, a int
		_ = d.Lettura.QueryRow(`SELECT count(*) FROM solleciti_artigiani`).Scan(&r)
		_ = d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit WHERE azione = 'repair.artisan_reminded'`).Scan(&a)
		return fmt.Sprintf("%d/%d", r, a)
	}
	for _, art := range []int64{0, -1, 2, 99} {
		if _, err := b.RegistraSollecito(ctx, art, "", chi); !errors.Is(err, ErrNessunaBustaDaSollecitare) {
			t.Fatalf("artigiano %d: %v", art, err)
		}
	}
	if conta() != "0/0" {
		t.Fatal("rifiuti scritti", conta())
	}
	g, err := b.RegistraSollecito(ctx, 1, "", chi)
	if err != nil || len(g.Buste) != 1 || g.Buste[0].ID != x || g.Artigiano != "Artigiano Esempio" {
		t.Fatalf("%+v %v", g, err)
	}
	var riga string
	_ = d.Lettura.QueryRow(`SELECT busta_id || '|' || artigiano_id || '|' || consegna_artigiano_id || '|' || attore || '|' || giorni_fuori || '|' || creato_il
		FROM solleciti_artigiani`).Scan(&riga)
	if want := fmt.Sprintf("%d|1|%d|titolare@esempio.test|30|2026-10-31T09:00:00.000000Z", x, c); riga != want {
		t.Fatalf("riga %s, voluta %s", riga, want)
	}
	if got, want := ultimoAudit(t, d, "repair.artisan_reminded"), fmt.Sprintf(`repair.artisan_reminded|repair|%d|ok|{"artisan_id":1,"days_out":30,"handover_id":%d}`, x, c); got != want {
		t.Fatalf("audit %s, voluto %s", got, want)
	}
	// Con la soglia della pagina (5 giorni) e un'ora dopo: tutte e due, e due tocchi sono due solleciti.
	o.Sposta(time.Hour)
	for range 2 {
		if g, err := b.RegistraSollecito(ctx, 1, " 5", chi); err != nil || len(g.Buste) != 2 {
			t.Fatalf("5 giorni: %+v %v", g, err)
		}
	}
	if conta() != "5/5" {
		t.Fatal("dopo tre tocchi", conta())
	}
	s, _ := b.SollecitiPendenti(ctx, "5")
	if want := fmt.Sprintf("giorni=5 totale=2\n1 Artigiano Esempio - ultimo=2026-10-31T10:00:00.000000Z: [%d 30gg c%d 2026-10-31T10:00:00.000000Z] [%d 11gg c%d 2026-10-31T10:00:00.000000Z]", x, c, y, c); scriviSolleciti(s) != want {
		t.Fatalf("elenco:\n%s", scriviSolleciti(s))
	}
	// L'ultimo sollecito del gruppo è il più recente delle sue buste, in qualunque ordine siano.
	esegui(t, d, `UPDATE solleciti_artigiani SET creato_il = '2026-10-31T11:00:00.000000Z' WHERE id = (SELECT max(id) FROM solleciti_artigiani WHERE busta_id = ?)`, y)
	if s, _ := b.SollecitiPendenti(ctx, "5"); s.Gruppi[0].UltimoSollecito == nil || !s.Gruppi[0].UltimoSollecito.Equal(time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("ultimo del gruppo: %s", scriviSolleciti(s))
	}
	// Rientrata, la busta non si sollecita più, ma i suoi solleciti restano.
	esegui(t, d, `UPDATE buste SET rientrata_artigiano_il = '2026-10-31T10:30:00.000000Z' WHERE id IN (?, ?)`, x, y)
	if _, err := b.RegistraSollecito(ctx, 1, "1", chi); !errors.Is(err, ErrNessunaBustaDaSollecitare) || conta() != "5/5" {
		t.Fatalf("rientrate: %v %s", err, conta())
	}
	ss, err := b.SollecitiDellaBusta(ctx, x)
	if err != nil || len(ss) != 3 || ss[0].GiorniFuori != 30 || !ss[0].Il.Equal(ss[1].Il) || ss[2].Il.After(ss[1].Il) || ss[2].Attore == nil || *ss[2].Attore != "titolare@esempio.test" {
		t.Fatalf("solleciti della busta: %+v %v", ss, err)
	}
	// Dal più recente, a pari istante dall'ultimo scritto (id); senza attore, nessun attore.
	var ultimo, primo int64
	_ = d.Lettura.QueryRow(`SELECT max(id), min(id) FROM solleciti_artigiani WHERE busta_id = ?`, x).Scan(&ultimo, &primo)
	esegui(t, d, `UPDATE solleciti_artigiani SET attore = NULL, giorni_fuori = 77 WHERE id = ?`, ultimo)
	if ss, _ := b.SollecitiDellaBusta(ctx, x); ss[0].GiorniFuori != 77 || ss[0].Attore != nil || ss[2].Il.After(ss[0].Il) {
		t.Fatalf("ordine: %+v", ss)
	}
	if ss, _ := b.SollecitiDellaBusta(ctx, 9999); ss != nil {
		t.Fatal("busta senza solleciti", ss)
	}
}

// inventario: ART-20
// La lettura della pagina scrive la soglia e quante buste, mai un numero di busta.
func TestRegistraVistaSolleciti(t *testing.T) {
	b, d, _ := prova(t)
	if err := b.RegistraVistaSolleciti(ctx, 14, 3, chi); err != nil {
		t.Fatal(err)
	}
	if got := ultimoAudit(t, d, "handover.reminders_viewed"); got != `handover.reminders_viewed|artisan_reminders|index|ok|{"count":3,"days":14}` {
		t.Fatal(got)
	}
}
