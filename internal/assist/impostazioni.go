package assist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina «Assistente e dettatura» (slice 107; ADM-11, RIP-H-46 parte impostazioni), parte configurazione:
// Config.put/3, Config.change/2 e il «test» di AssistLive dello standalone a go-baseline. La raccolta dei
// dettati (ADM-12) arriva con la 114.
//
// Le tre regole della 012 dello standalone, più una: la chiave non torna mai sulla pagina, una casella della
// chiave vuota vuol dire «tieni quella salvata», l'audit nomina i campi cambiati e mai un valore; e una chiave
// non viaggia mai in chiaro verso un'altra macchina (http va bene solo verso questa).

// FraseDiProva è @self_test di Assist: la frase che la prova manda al modello, senza nessun cliente.
const FraseDiProva = "Busta 1, anello in oro da stringere di una misura, preventivo 40 euro."

// I messaggi del changeset, come li mostra lo standalone (quelli di Ecto tradotti come nella 069b).
const (
	msgVuoto          = "non può essere vuoto"
	msgSchema         = "deve cominciare con http:// oppure https://"
	msgSenzaHost      = "manca l'indirizzo del server"
	msgChiaveInChiaro = "una chiave non viaggia in chiaro verso un host remoto"
	msgNonValido      = "Valore non valido." // is invalid, il cast di :boolean di clear_secret
)

func msgTroppoLungo(n int) string { return fmt.Sprintf("Troppo lungo (massimo %d caratteri).", n) }

// msgGlossarioLungo è il messaggio di normalize_glossary/2: «N carattere/caratteri di troppo sul limite di 2000».
func msgGlossarioLungo(n int) string {
	parola := "caratteri"
	if n == 1 {
		parola = "carattere"
	}
	return fmt.Sprintf("%d %s di troppo sul limite di %d", n, parola, MassimoGlossario)
}

// ErrFamiglia: una famiglia che non è «ai» né «stt» (nello standalone non c'è un evento per lei).
var ErrFamiglia = errors.New("assist: famiglia sconosciuta")

// Modulo sono i campi di un modulo come li manda il browser (endpoint, model, api_key, glossary, clear_secret):
// un campo che manca non c'era e resta quello salvato (cast/3).
type Modulo map[string]string

// Rifiuto è un salvataggio rifiutato: gli errori per campo (il più recente prima, come changeset.errors) e i
// valori da rimettere nel modulo (quelli mandati, altrimenti i salvati); mai la chiave (blank_secret/1).
type Rifiuto struct {
	Errori map[string][]string
	Valori map[string]string
}

// Famiglia è quello che la pagina mostra di una famiglia: i valori salvati (mai la chiave: solo se c'è) e se è
// configurata (usable?/1).
type Famiglia struct {
	Indirizzo, Modello, Glossario string
	ChiaveSalvata, Configurata    bool
}

// PaginaAssistente sono le due famiglie della pagina (load/1 di AssistLive, con Config.redact/1).
type PaginaAssistente struct {
	Estrazione, Trascrizione Famiglia
}

// Impostazioni è la pagina dell'assistente sul database: lettura, salvataggio, prova, e dalla 109 la
// trascrizione. Attesa accorcia l'attesa del modello nei test (0 = AttesaModello), AttesaTrascrizione quella
// del trascrittore (0 = AttesaTrascrizione); Trascrittore è il motore della trascrizione (nil = ServerEsterno).
type Impostazioni struct {
	d                  *db.DB
	o                  clock.Orologio
	reg                *audit.Registro
	Attesa             time.Duration
	AttesaTrascrizione time.Duration
	Trascrittore       Transcriber
	Registro           *slog.Logger // per il panic del motore (solo famiglia e tipo); nil = slog.Default()
}

// NuoveImpostazioni dà la pagina dell'assistente sul database d.
func NuoveImpostazioni(d *db.DB, o clock.Orologio) *Impostazioni {
	return &Impostazioni{d: d, o: o, reg: audit.Nuovo(d, o)}
}

// Pagina legge quello che la pagina mostra.
func (i *Impostazioni) Pagina(ctx context.Context) (PaginaAssistente, error) {
	ai, err := LeggiEstrazione(ctx, i.d.Lettura)
	if err != nil {
		return PaginaAssistente{}, err
	}
	stt, err := LeggiTrascrizione(ctx, i.d.Lettura)
	if err != nil {
		return PaginaAssistente{}, err
	}
	return PaginaAssistente{
		Estrazione:   Famiglia{Indirizzo: ai.Indirizzo, Modello: ai.Modello, ChiaveSalvata: ai.Chiave != "", Configurata: ai.Usabile()},
		Trascrizione: Famiglia{Indirizzo: stt.Indirizzo, Glossario: stt.Glossario, ChiaveSalvata: stt.Chiave != "", Configurata: stt.Usabile()},
	}, nil
}

func ptr(s string) *string { return &s }

func uguali(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// presente è present?/1: un testo con qualcosa oltre gli spazi.
func presente(p *string) bool { return p != nil && trimElixir(*p) != "" }

// vero è truthy?/1 di keep_secret/2.
func vero(s string) bool { return s == "true" || s == "1" || s == "on" }

// usabile è usable?/1 sui valori finali di una famiglia.
func usabile(famiglia string, v map[string]*string) bool {
	if famiglia == FamigliaEstrazione {
		return presente(v[campoIndirizzo]) && presente(v[campoModello])
	}
	return presente(v[campoIndirizzo])
}

// Salva è Config.put(famiglia, attrs, context). keep_secret/2: con «Cancella la chiave salvata» la chiave è
// tolta (vince su una chiave scritta), una chiave scritta (non solo spazi) è la nuova, altrimenti resta quella
// salvata. Poi il changeset: il cast (un campo mandato vuoto o di soli spazi è nil; cambia solo se è diverso),
// validate_required su indirizzo (e modello per l'estrazione) sui valori finali, l'indirizzo con schema
// http/https e host, la chiave mai in chiaro verso un'altra macchina (sui valori finali, anche la chiave tenuta),
// il glossario cambiato portato alla forma salvata o rifiutato se la forma a una riga supera MassimoGlossario, e
// le lunghezze massime dei campi cambiati. Valido: in una transazione i campi della famiglia (nil = "") in
// impostazioni, la chiave in segreti_provider (nil la toglie), e assist.provider_configured con la famiglia, i
// nomi dei campi cambiati e se è configurata; mai un valore. Rifiutato: niente scritto e il *Rifiuto.
func (i *Impostazioni) Salva(ctx context.Context, famiglia string, m Modulo, a audit.Contesto) (*Rifiuto, error) {
	campi := CampiDi(famiglia)
	if campi == nil {
		return nil, ErrFamiglia
	}
	tx, err := i.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	prima, err := leggiFamiglia(ctx, tx, famiglia)
	if err != nil {
		return nil, err
	}
	// keep_secret/2.
	param := map[string]*string{}
	for k, v := range m {
		param[k] = ptr(v)
	}
	switch c := param[campoChiave]; {
	case vero(m[campoCancella]):
		param[campoChiave] = nil
	case presente(c):
	default:
		param[campoChiave] = prima[campoChiave]
	}
	// cast/3: dopo è il valore finale (get_field), cambiati i campi in changes.
	dopo := map[string]*string{}
	for _, campo := range campi {
		dopo[campo] = prima[campo]
	}
	cambiati := map[string]bool{}
	for _, campo := range campi {
		v, mandato := param[campo]
		if !mandato {
			continue
		}
		if v != nil && trimElixir(*v) == "" {
			v = nil
		}
		if !uguali(v, prima[campo]) {
			dopo[campo], cambiati[campo] = v, true
		}
	}
	errori := map[string][]string{}
	sbaglia := func(campo, msg string) { errori[campo] = append(errori[campo], msg) }
	if v, mandato := m[campoCancella]; mandato && trimElixir(v) != "" && v != "true" && v != "1" && v != "false" && v != "0" {
		sbaglia(campoCancella, msgNonValido)
	}
	// validate_required/3 (salta un campo che ha già un errore: qui nessuno dei richiesti può averne).
	richiesti := []string{campoIndirizzo}
	if famiglia == FamigliaEstrazione {
		richiesti = append(richiesti, campoModello)
	}
	for _, campo := range richiesti {
		if !presente(dopo[campo]) {
			sbaglia(campo, msgVuoto)
		}
	}
	// validate_endpoint/1.
	if v := dopo[campoIndirizzo]; v != nil {
		switch schema, host := partiIndirizzo(trimElixir(*v)); {
		case schema != "http" && schema != "https":
			sbaglia(campoIndirizzo, msgSchema)
		case host == "":
			sbaglia(campoIndirizzo, msgSenzaHost)
		}
	}
	// validate_key_travel/1.
	if presente(dopo[campoChiave]) && dopo[campoIndirizzo] != nil && inChiaroAltrove(*dopo[campoIndirizzo]) {
		sbaglia(campoChiave, msgChiaveInChiaro)
	}
	// normalize_glossary/2: solo un glossario cambiato e non nil (get_change). La forma salvata uguale a quella di
	// prima toglie il cambiamento (put_change/3), anche dalle lunghezze.
	if v := dopo[campoGlossario]; famiglia == FamigliaTrascrizione && cambiati[campoGlossario] && v != nil {
		if n := EccessoGlossario(*v); n > 0 {
			sbaglia(campoGlossario, msgGlossarioLungo(n))
		} else {
			normale := ptr(GlossarioSalvato(*v))
			dopo[campoGlossario] = normale
			cambiati[campoGlossario] = !uguali(normale, prima[campoGlossario])
		}
	}
	// validate_lengths/2: solo i campi cambiati e non nil (validate_change).
	for _, campo := range campi {
		if v := dopo[campo]; cambiati[campo] && v != nil && uniseg.GraphemeClusterCount(*v) > lunghezzeMassime[campo] {
			sbaglia(campo, msgTroppoLungo(lunghezzeMassime[campo]))
		}
	}
	if len(errori) > 0 {
		for _, e := range errori {
			slices.Reverse(e) // il più recente prima, come changeset.errors
		}
		rif := &Rifiuto{Errori: errori, Valori: map[string]string{}}
		for _, campo := range campi {
			if campo == campoChiave {
				continue // blank_secret/1: né i parametri, né i cambiamenti, né i dati
			}
			if v, mandato := m[campo]; mandato {
				rif.Valori[campo] = v // i parametri, così come sono arrivati
			} else {
				rif.Valori[campo] = testoDi(prima[campo])
			}
		}
		return rif, nil
	}
	return nil, i.scrivi(ctx, tx, famiglia, prima, dopo, a)
}

// scrivi è write/4: i campi della famiglia, la chiave e la riga di audit, nella transazione tx.
func (i *Impostazioni) scrivi(ctx context.Context, tx *sql.Tx, famiglia string, prima, dopo map[string]*string, a audit.Contesto) error {
	ora := i.o.Adesso().UTC().Format(db.FormatoIstante)
	cambiate := []string{}
	for _, campo := range CampiDi(famiglia) {
		if !uguali(prima[campo], dopo[campo]) {
			cambiate = append(cambiate, campo)
		}
		chiave := chiaveDi(famiglia, campo)
		var err error
		switch {
		case campo != campoChiave:
			_, err = tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
				ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`, chiave, testoDi(dopo[campo]), ora)
		case dopo[campo] != nil:
			_, err = tx.ExecContext(ctx, `INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES (?, ?, ?)
				ON CONFLICT (nome) DO UPDATE SET valore = excluded.valore, aggiornato_il = excluded.aggiornato_il`, chiave, *dopo[campo], ora)
		default:
			_, err = tx.ExecContext(ctx, `DELETE FROM segreti_provider WHERE nome = ?`, chiave)
		}
		if err != nil {
			return err
		}
	}
	if err := i.reg.Scrivi(ctx, tx, a, "assist.provider_configured", "assist_provider", famiglia, "ok",
		map[string]any{"family": famiglia, "changed": cambiate, "configured": usabile(famiglia, dopo)}); err != nil {
		return err
	}
	return tx.Commit()
}

// Prova è il «test» di AssistLive: la FraseDiProva al modello configurato, con il client della 106
// (Collegamento.Estrai: rifiuti, nuovo tentativo, attesa e audit assist.extract con soli conteggi). Il negozio è
// vuoto: la frase non nomina clienti né oreficerie e la pagina mostra solo la descrizione, quindi la prova non
// legge i clienti. Una lettura della configurazione che fallisce è l'errore del database, non un rifiuto.
func (i *Impostazioni) Prova(ctx context.Context, a audit.Contesto) (Estrazione, error) {
	cfg, err := LeggiEstrazione(ctx, i.d.Lettura)
	if err != nil {
		return Estrazione{}, err
	}
	c := Collegamento{Config: cfg, Attesa: i.Attesa}
	return c.Estrai(ctx, FraseDiProva, clock.Locale(i.o.Adesso()), Negozio{}, i.reg, a)
}
