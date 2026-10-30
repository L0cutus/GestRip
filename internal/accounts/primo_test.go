package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il primo titolare su un database vuoto (ACC-16, «sostituita»): al posto dei due file privati
// dell'installer dello standalone (Bootstrap.admin_from_files/2), un link monouso e a scadenza
// stampato da «gestrip primo-titolare», e la pagina /configura/<codice> che chiede email e
// password. Vale solo finché utenti è vuota; riga administrator.bootstrapped con attore installer.

// primoProva: un database migrato e senza nessun account.
func primoProva(t *testing.T) (*PrimoTitolare, *db.DB, *clock.Finto) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := orologio()
	if _, err := d.Migra(ctx, o); err != nil {
		t.Fatal(err)
	}
	return NuovoPrimoTitolare(d, o), d, o
}

type rigaCodice struct {
	hash               []byte
	scade              string
	usato, revocato    any
	creato, aggiornato string
}

func righeCodice(t *testing.T, d *db.DB) []rigaCodice {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT hash_codice, scade_il, usato_il, revocato_il, creato_il, aggiornato_il FROM codici_primo_titolare ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	var out []rigaCodice
	for rr.Next() {
		var r rigaCodice
		if err := rr.Scan(&r.hash, &r.scade, &r.usato, &r.revocato, &r.creato, &r.aggiornato); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// stato riassume quello che una scrittura può aver cambiato: account, codici spesi o annullati,
// righe del registro.
func stato(t *testing.T, d *db.DB) string {
	t.Helper()
	return strings.Join([]string{
		strconv.Itoa(conta(t, d, `SELECT count(*) FROM utenti`)),
		strconv.Itoa(conta(t, d, `SELECT count(*) FROM codici_primo_titolare`)),
		strconv.Itoa(conta(t, d, `SELECT count(*) FROM codici_primo_titolare WHERE usato_il IS NOT NULL`)),
		strconv.Itoa(conta(t, d, `SELECT count(*) FROM codici_primo_titolare WHERE revocato_il IS NOT NULL`)),
		strconv.Itoa(conta(t, d, `SELECT count(*) FROM eventi_audit`)),
	}, "|")
}

const pwTitolare = "una password lunga del titolare"

// inventario: ACC-16
// Il comando locale crea il codice: 32 byte casuali in base64url (43 caratteri), nel database solo
// il loro SHA-256, valido 60 minuti; riga administrator.bootstrap_requested con attore «console»,
// senza il codice. Il codice stampato apre la pagina.
func TestPrimoTitolareLinkDallaConsole(t *testing.T) {
	p, d, o := primoProva(t)
	codice, scade, err := p.LinkDallaConsole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := base64.RawURLEncoding.DecodeString(codice); err != nil || len(b) != 32 || len(codice) != 43 {
		t.Fatalf("codice: %q %v", codice, err)
	}
	if DurataPrimoTitolare != time.Hour || !scade.Equal(o.Adesso().Add(time.Hour)) {
		t.Errorf("scadenza: %v (%v)", scade, DurataPrimoTitolare)
	}
	rr := righeCodice(t, d)
	h := sha256.Sum256([]byte(codice))
	if len(rr) != 1 || string(rr[0].hash) != string(h[:]) || rr[0].scade != "2026-10-30T10:15:07.000000Z" || rr[0].usato != nil ||
		rr[0].revocato != nil || rr[0].creato != istante || rr[0].aggiornato != istante {
		t.Errorf("riga del codice: %+v", rr)
	}
	if got := ultimoEvento(t, d); got != `console|-|administrator.bootstrap_requested|installation|administrator|ok|{"channel":"console"}` {
		t.Errorf("registro: %q", got)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE instr(dati, '`+codice+`') > 0 OR instr(id_risorsa, '`+codice+`') > 0`) != 0 {
		t.Error("il codice è nel registro")
	}
	if ok, err := p.CodiceValido(ctx, codice); !ok || err != nil {
		t.Errorf("il codice stampato non vale: %v %v", ok, err)
	}
	altro, _, err := p.LinkDallaConsole(ctx)
	if err != nil || altro == codice {
		t.Fatalf("secondo codice: %v", err)
	}
}

// inventario: ACC-16
// Con un account qualunque nel database (anche disattivato, o un invito in attesa della password)
// il comando rifiuta, come already_initialized dello standalone che conta tutte le righe di users,
// e non scrive niente.
func TestPrimoTitolareLinkSoloSuDatabaseVuoto(t *testing.T) {
	for nome, riga := range map[string]string{
		"titolare":    `INSERT INTO utenti (email, ruolo, hash_password, attivo, creato_il, aggiornato_il) VALUES ('t@esempio.it', 'titolare', 'x', 1, '` + istante + `', '` + istante + `')`,
		"disattivato": `INSERT INTO utenti (email, ruolo, hash_password, attivo, creato_il, aggiornato_il) VALUES ('d@esempio.it', 'dipendente', 'x', 0, '` + istante + `', '` + istante + `')`,
		"in attesa":   `INSERT INTO utenti (email, ruolo, creato_il, aggiornato_il) VALUES ('i@esempio.it', 'dipendente', '` + istante + `', '` + istante + `')`,
	} {
		t.Run(nome, func(t *testing.T) {
			p, d, _ := primoProva(t)
			if _, err := d.Scrittura.Exec(riga); err != nil {
				t.Fatal(err)
			}
			prima := stato(t, d)
			if codice, _, err := p.LinkDallaConsole(ctx); !errors.Is(err, ErrGiaConfigurato) || codice != "" {
				t.Errorf("link con un account: %q %v", codice, err)
			}
			if got := stato(t, d); got != prima {
				t.Errorf("il rifiuto ha scritto: %s → %s", prima, got)
			}
		})
	}
}

// inventario: ACC-16
// Un codice nuovo annulla quello aperto: il primo non apre più la pagina e non crea l'account.
func TestPrimoTitolareNuovoCodiceAnnullaIlPrecedente(t *testing.T) {
	p, d, _ := primoProva(t)
	primo, _, err := p.LinkDallaConsole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondo, _, err := p.LinkDallaConsole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rr := righeCodice(t, d)
	if len(rr) != 2 || rr[0].revocato != istante || rr[0].aggiornato != istante || rr[1].revocato != nil {
		t.Errorf("righe: %+v", rr)
	}
	if ok, _ := p.CodiceValido(ctx, primo); ok {
		t.Error("il codice annullato apre ancora la pagina")
	}
	if _, _, err := p.Crea(ctx, primo, "t@esempio.it", pwTitolare, pwTitolare, ""); !errors.Is(err, ErrCodiceNonValido) {
		t.Errorf("il codice annullato crea l'account: %v", err)
	}
	if ok, _ := p.CodiceValido(ctx, secondo); !ok {
		t.Error("il codice nuovo non vale")
	}
}

// inventario: ACC-16, ADM-17
// Riuscito: il titolare nasce attivo, con ruolo titolare, l'email normalizzata come gli altri
// account, nessun nome, la password scelta (si entra); il codice è speso; riga
// administrator.bootstrapped con attore installer, l'id dell'account e nessun dato, come lo
// standalone, più l'indirizzo di chi l'ha fatto. Dopo, il codice non vale più, e né la pagina né il
// comando creano un secondo titolare.
func TestPrimoTitolareCrea(t *testing.T) {
	p, d, o := primoProva(t)
	codice, _, err := p.LinkDallaConsole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o.Sposta(time.Minute)
	id, email, err := p.Crea(ctx, codice, " Titolare@Esempio.IT\t", pwTitolare, pwTitolare, "192.0.2.7")
	if err != nil {
		t.Fatal(err)
	}
	if email != "titolare@esempio.it" || id == 0 {
		t.Errorf("creato: %d %q", id, email)
	}
	var em, ruolo, hash string
	var attivo, versione int
	var nome, ultimo, disattivato any
	if err := d.Lettura.QueryRow(`SELECT email, ruolo, hash_password, attivo, versione_sessione, nome_visibile, ultimo_accesso_il, disattivato_il FROM utenti WHERE id = ?`, id).
		Scan(&em, &ruolo, &hash, &attivo, &versione, &nome, &ultimo, &disattivato); err != nil {
		t.Fatal(err)
	}
	if em != "titolare@esempio.it" || ruolo != Titolare || attivo != 1 || versione != 0 || nome != nil || ultimo != nil || disattivato != nil || !Verifica(pwTitolare, hash) {
		t.Errorf("account: %q %q %d %d %v %v %v", em, ruolo, attivo, versione, nome, ultimo, disattivato)
	}
	if conta(t, d, `SELECT count(*) FROM utenti`) != 1 {
		t.Error("più di un account")
	}
	rr := righeCodice(t, d)
	if len(rr) != 1 || rr[0].usato != "2026-10-30T09:16:07.000000Z" || rr[0].aggiornato != "2026-10-30T09:16:07.000000Z" || rr[0].revocato != nil {
		t.Errorf("codice dopo: %+v", rr)
	}
	if got := ultimoEvento(t, d); got != "installer|192.0.2.7|administrator.bootstrapped|user|"+itoa(id)+"|ok|{}" {
		t.Errorf("registro: %q", got)
	}
	u := NuoviUtenti(d, o)
	if ut, ok, err := u.Autentica(ctx, "titolare@esempio.it", pwTitolare); !ok || err != nil || ut.Ruolo != Titolare {
		t.Errorf("il titolare non entra: %v %v", ok, err)
	}
	if ok, _ := p.CodiceValido(ctx, codice); ok {
		t.Error("il codice speso vale ancora")
	}
	if _, _, err := p.Crea(ctx, codice, "altro@esempio.it", pwTitolare, pwTitolare, ""); !errors.Is(err, ErrCodiceNonValido) {
		t.Errorf("seconda creazione con lo stesso codice: %v", err)
	}
	if _, _, err := p.LinkDallaConsole(ctx); !errors.Is(err, ErrGiaConfigurato) {
		t.Errorf("un nuovo codice dopo il titolare: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'administrator.bootstrapped'`) != 1 {
		t.Error("righe administrator.bootstrapped")
	}
}

// inventario: ACC-16
// I rifiuti del modulo (email di forma sbagliata o troppo lunga, password diverse, password sotto i
// 12 caratteri contati come String.length) non scrivono niente e non spendono il codice: il modulo
// si ripete finché il codice vale. Una password di 12 caratteri passa.
func TestPrimoTitolareCreaRifiuti(t *testing.T) {
	p, d, _ := primoProva(t)
	codice, _, err := p.LinkDallaConsole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prima := stato(t, d)
	lunga := strings.Repeat("a", 255-len("@esempio.it")) + "@esempio.it"
	undici := strings.Repeat("é", 11) // 11 caratteri, 22 rune
	for _, c := range []struct {
		email, pw, conf string
		atteso          error
	}{
		{"", pwTitolare, pwTitolare, ErrEmailNonValida},
		{"non-un-indirizzo", pwTitolare, pwTitolare, ErrEmailNonValida},
		{"a@b@c.esempio", pwTitolare, pwTitolare, ErrEmailNonValida},
		{"root@localhost", pwTitolare, pwTitolare, ErrEmailNonValida},
		{"a b@esempio.it", pwTitolare, pwTitolare, ErrEmailNonValida},
		{lunga, pwTitolare, pwTitolare, ErrEmailNonValida},
		{"t@esempio.it", pwTitolare, pwTitolare + "x", ErrPasswordDiverse},
		{"t@esempio.it", undici, undici, ErrPasswordCorta},
		{"t@esempio.it", "", "", ErrPasswordCorta},
	} {
		if _, _, err := p.Crea(ctx, codice, c.email, c.pw, c.conf, ""); !errors.Is(err, c.atteso) {
			t.Errorf("%q %q %q: %v, atteso %v", c.email, c.pw, c.conf, err, c.atteso)
		}
		if got := stato(t, d); got != prima {
			t.Fatalf("%q: il rifiuto ha scritto: %s → %s", c.email, prima, got)
		}
		if ok, _ := p.CodiceValido(ctx, codice); !ok {
			t.Fatalf("%q: il rifiuto ha speso il codice", c.email)
		}
	}
	dodici := strings.Repeat("é", 12)
	if _, _, err := p.Crea(ctx, codice, strings.Repeat("a", 254-len("@esempio.it"))+"@esempio.it", dodici, dodici, ""); err != nil {
		t.Errorf("12 caratteri e 254 di email: %v", err)
	}
}

// inventario: ACC-16
// Un codice che non va non apre la pagina e non crea niente, qualunque il motivo: inventato,
// vuoto, troppo lungo, scaduto (a 60 minuti esatti; un istante prima vale), annullato, speso (anche
// se utenti tornasse vuota), o valido ma con un account già nel database (creato per altra via).
func TestPrimoTitolareCodiceMorto(t *testing.T) {
	prova := func(t *testing.T, p *PrimoTitolare, d *db.DB, codice string) {
		t.Helper()
		if ok, err := p.CodiceValido(ctx, codice); ok || err != nil {
			t.Errorf("il codice apre la pagina: %v %v", ok, err)
		}
		prima := stato(t, d)
		if _, _, err := p.Crea(ctx, codice, "nuovo@esempio.it", pwTitolare, pwTitolare, ""); !errors.Is(err, ErrCodiceNonValido) {
			t.Errorf("crea: %v", err)
		}
		if got := stato(t, d); got != prima {
			t.Errorf("il rifiuto ha scritto: %s → %s", prima, got)
		}
	}
	t.Run("inventati", func(t *testing.T) {
		p, d, _ := primoProva(t)
		if _, _, err := p.LinkDallaConsole(ctx); err != nil {
			t.Fatal(err)
		}
		for _, c := range []string{"", "codice-inventato", strings.Repeat("A", 43), strings.Repeat("A", 65)} {
			prova(t, p, d, c)
		}
	})
	t.Run("scaduto", func(t *testing.T) {
		p, d, o := primoProva(t)
		codice, _, _ := p.LinkDallaConsole(ctx)
		o.Sposta(time.Hour - time.Microsecond)
		if ok, _ := p.CodiceValido(ctx, codice); !ok {
			t.Error("un istante prima della scadenza non vale")
		}
		o.Sposta(time.Microsecond)
		prova(t, p, d, codice)
	})
	t.Run("speso", func(t *testing.T) {
		p, d, _ := primoProva(t)
		codice, _, _ := p.LinkDallaConsole(ctx)
		if _, _, err := p.Crea(ctx, codice, "t@esempio.it", pwTitolare, pwTitolare, ""); err != nil {
			t.Fatal(err)
		}
		prova(t, p, d, codice)
		// Speso resta speso anche se utenti tornasse vuota (una cancellazione a mano).
		if _, err := d.Scrittura.Exec(`DELETE FROM utenti`); err != nil {
			t.Fatal(err)
		}
		prova(t, p, d, codice)
	})
	t.Run("account già presente", func(t *testing.T) {
		p, d, _ := primoProva(t)
		codice, _, _ := p.LinkDallaConsole(ctx)
		if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, attivo, creato_il, aggiornato_il) VALUES ('x@esempio.it', 'dipendente', 0, ?, ?)`, istante, istante); err != nil {
			t.Fatal(err)
		}
		prova(t, p, d, codice)
	})
}

// inventario: ACC-16
// Le condizioni stanno dentro la scrittura, non solo nel controllo prima: un codice che scade, è
// annullato da un nuovo «gestrip primo-titolare», o un account che compare tra il controllo e la
// transazione, non creano il titolare e non spendono il codice.
func TestPrimoTitolareGuardieNellaScrittura(t *testing.T) {
	for nome, colpo := range map[string]func(d *db.DB, o *clock.Finto){
		"scaduto": func(_ *db.DB, o *clock.Finto) { o.Sposta(time.Hour) },
		"annullato": func(d *db.DB, _ *clock.Finto) {
			_, _ = d.Scrittura.Exec(`UPDATE codici_primo_titolare SET revocato_il = '2026-10-30T09:15:08.000000Z'`)
		},
		"account comparso": func(d *db.DB, _ *clock.Finto) {
			_, _ = d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, hash_password, creato_il, aggiornato_il) VALUES ('altro@esempio.it', 'titolare', 'x', ?, ?)`, istante, istante)
		},
	} {
		t.Run(nome, func(t *testing.T) {
			_, d, f := primoProva(t)
			codice, _, err := NuovoPrimoTitolare(d, f).LinkDallaConsole(ctx)
			if err != nil {
				t.Fatal(err)
			}
			o := &orologioConColpo{Finto: f, quale: 2}
			o.colpo = func() { colpo(d, f) }
			if _, _, err := NuovoPrimoTitolare(d, o).Crea(ctx, codice, "t@esempio.it", pwTitolare, pwTitolare, ""); !errors.Is(err, ErrCodiceNonValido) {
				t.Errorf("creato: %v", err)
			}
			if o.n < 2 {
				t.Fatalf("il colpo non è caduto (%d letture dell'ora)", o.n)
			}
			if conta(t, d, `SELECT count(*) FROM utenti WHERE email = 't@esempio.it'`) != 0 ||
				conta(t, d, `SELECT count(*) FROM codici_primo_titolare WHERE usato_il IS NOT NULL`) != 0 ||
				conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'administrator.bootstrapped'`) != 0 {
				t.Error("la scrittura ha creato il titolare o speso il codice")
			}
		})
	}
}

// inventario: ACC-16
// Due codici aperti insieme (il comando annulla i vecchi; qui si scrivono a mano, per provare la
// guardia su utenti vuota da sola): due creazioni simultanee danno un solo titolare e una sola riga
// administrator.bootstrapped, e il codice rimasto aperto si annulla. Lo stesso con un codice solo e
// molte richieste.
func TestPrimoTitolareSenzaCorse(t *testing.T) {
	for _, quanti := range []int{1, 2} {
		t.Run(strconv.Itoa(quanti), func(t *testing.T) {
			p, d, o := primoProva(t)
			var codici []string
			for range quanti {
				c, h := nuovoToken()
				if _, err := d.Scrittura.Exec(`INSERT INTO codici_primo_titolare (hash_codice, scade_il, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`,
					h, o.Adesso().Add(time.Hour).UTC().Format(db.FormatoIstante), istante, istante); err != nil {
					t.Fatal(err)
				}
				codici = append(codici, c)
			}
			var wg sync.WaitGroup
			var mu sync.Mutex
			riuscite := 0
			for n := range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, _, err := p.Crea(ctx, codici[n%quanti], "t"+strconv.Itoa(n)+"@esempio.it", pwTitolare, pwTitolare, ""); err == nil {
						mu.Lock()
						riuscite++
						mu.Unlock()
					}
				}()
			}
			wg.Wait()
			if riuscite != 1 || conta(t, d, `SELECT count(*) FROM utenti`) != 1 ||
				conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'administrator.bootstrapped'`) != 1 {
				t.Errorf("riuscite %d, account %d", riuscite, conta(t, d, `SELECT count(*) FROM utenti`))
			}
			// Dopo il titolare nessun codice resta aperto: l'altro è annullato.
			if n := conta(t, d, `SELECT count(*) FROM codici_primo_titolare WHERE usato_il IS NULL AND revocato_il IS NULL`); n != 0 {
				t.Errorf("codici ancora aperti: %d", n)
			}
		})
	}
}

// inventario: ACC-16
// Se la riga del registro non si scrive, non resta niente: né il codice nuovo (né l'annullamento
// del vecchio), né il titolare, né il codice speso (un trigger fa fallire la scrittura dell'audit).
func TestPrimoTitolareAuditNellaTransazione(t *testing.T) {
	p, d, _ := primoProva(t)
	codice, _, err := p.LinkDallaConsole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`CREATE TRIGGER blocca BEFORE INSERT ON eventi_audit BEGIN SELECT RAISE(ABORT, 'no'); END`); err != nil {
		t.Fatal(err)
	}
	prima := stato(t, d)
	if _, _, err := p.LinkDallaConsole(ctx); err == nil {
		t.Error("codice creato senza audit")
	}
	if _, _, err := p.Crea(ctx, codice, "t@esempio.it", pwTitolare, pwTitolare, ""); err == nil {
		t.Error("titolare creato senza audit")
	}
	if got := stato(t, d); got != prima {
		t.Errorf("è rimasto qualcosa senza la riga del registro: %s → %s", prima, got)
	}
	if ok, _ := p.CodiceValido(ctx, codice); !ok {
		t.Error("il codice non vale più")
	}
}
