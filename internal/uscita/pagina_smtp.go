package uscita

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina «Provider di invio», parte email (slice 069b, ADM-09, CLI-52 parte email), come ProvidersLive,
// Outgoing.Config.put/3, Smtp.changeset/3 ed enqueue_test_message/2 dello standalone a go-baseline (la
// prova però parte subito, senza coda: MandaProva, 069c), più due cose solo del Go:
//   - l'interruttore dell'email (D-03): dopo l'import email e Telegram sono spenti finché il titolare non
//     li accende (ChiaveEmailAcceso);
//   - la revisione delle email rimaste in coda (D-27): l'email non si accende finché c'è un'email in coda;
//     il titolare le guarda e le scarta (ScartaInSospeso) o le manda una per una (MandaInSospeso), mai
//     tutte insieme. Con l'email accesa le manda la coda, e queste due azioni non fanno niente.
// Il segreto non esce mai: né nella pagina (la casella della password è sempre vuota), né nell'audit
// (solo i nomi delle chiavi cambiate), né nel registro del server.

// I campi del modulo (smtp[…]) nell'ordine di keys(:email), e la spunta che cancella le credenziali.
const (
	campoServer       = "host"
	campoPorta        = "port"
	campoSicurezza    = "security"
	campoUtente       = "username"
	campoPassword     = "password"
	campoMittente     = "from_address"
	campoNomeMittente = "from_name"
	campoCancella     = "clear_secret"
)

// CampiSMTP sono i campi del modulo nell'ordine dello standalone (Config.keys(:email)).
var CampiSMTP = []string{campoServer, campoPorta, campoSicurezza, campoUtente, campoPassword, campoMittente, campoNomeMittente}

// chiaviCampi sono le impostazioni dei campi (la password sta in segreti_provider).
var chiaviCampi = map[string]string{campoServer: chiaveServer, campoPorta: chiavePorta, campoSicurezza: chiaveSicurezza,
	campoUtente: chiaveUtente, campoMittente: chiaveMittente, campoNomeMittente: chiaveNomeMittente}

// I testi degli errori dei campi: quelli del catalogo it dello standalone (priv/gettext/it/errors.po) e i
// due scritti a mano in validate_credentials/1.
const (
	msgNonValido       = "Valore non valido."                                        // is invalid
	msgFormato         = "Formato non valido."                                       // has invalid format
	msgMaggioreDiZero  = "Deve essere maggiore di 0."                                // must be greater than %{number}
	msgAlMassimoPorta  = "Al massimo 65535."                                         // must be less than or equal to %{number}
	msgPasswordUtente  = "serve una password per questo utente"                      // validate_credentials/1
	msgPasswordChiaro  = "le credenziali non viaggiano su una connessione in chiaro" // validate_credentials/1
	portaMassima       = 65535
	oggettoProvaEmail  = "GestRip — prova del provider di posta"
	testoProvaEmail    = "Messaggio di prova inviato dalla tua installazione GestRip.\n\nSe lo stai leggendo, l'account SMTP configurato funziona.\nNessun cliente e nessuna oreficeria ha ricevuto questo messaggio."
	righeRecentiPagina = 20 // list_recent(20) di ProvidersLive
)

// msgTroppoLungo è «should be at most %{count} character(s)» del catalogo, al plurale.
func msgTroppoLungo(n int) string { return fmt.Sprintf("Troppo lungo (massimo %d caratteri).", n) }

// lunghezzeMassime sono le validate_length(…, max:) del changeset, in grafemi come String.length/1.
var lunghezzeMassime = []struct {
	campo string
	max   int
}{{campoServer, 255}, {campoUtente, 255}, {campoPassword, 255}, {campoMittente, 254}, {campoNomeMittente, 200}}

// I rifiuti delle azioni della pagina: niente è scritto.
var (
	// ErrEmailInSospeso: ci sono email in coda da guardare (D-27): l'email non si accende.
	ErrEmailInSospeso = errors.New("uscita: email in coda da guardare prima di accendere")
	// ErrEmailAccesa: con l'email accesa le email in coda le manda la coda, non il titolare.
	ErrEmailAccesa = errors.New("uscita: invio delle email acceso")
	// ErrNonInSospeso: la riga non c'è, non è un'email o non è più in coda.
	ErrNonInSospeso = errors.New("uscita: email non in coda")
)

// ModuloSMTP sono i campi del modulo come li manda il browser, per nome (host, port, …, clear_secret): un
// campo che manca non c'era nel modulo e resta quello salvato (cast/3).
type ModuloSMTP map[string]string

// RifiutoSMTP è un modulo rifiutato: gli errori per campo (il più recente prima, come changeset.errors) e
// i valori da rimettere nel modulo (quelli mandati, o i salvati per un campo che non c'era); la password è
// sempre vuota.
type RifiutoSMTP struct {
	Errori map[string][]string
	Valori map[string]string
}

// RigaCoda è una riga della coda come la mostra la pagina.
type RigaCoda struct {
	ID                     int64
	BustaID                *int64
	Canale, Destinatario   string
	Indirizzo              string
	Oggetto                *string
	Testo, Stato           string
	Tentativi              int
	AccodatoIl             time.Time
	AccettatoIl, FallitoIl *time.Time
	Riferimento            string
	UltimoErrore           string
	// NumeroBusta e CodiceQRBusta sono numero e codice QR della busta della riga (nil senza busta), per
	// il numero visibile della D-64 nelle email da guardare (069c).
	NumeroBusta, CodiceQRBusta *string
}

// PaginaSMTP è quello che la pagina mostra: i valori salvati del modulo (senza password), se c'è una
// password salvata, se l'email è configurata (Usabile) e accesa (D-03), i conteggi e le ultime righe
// della coda (email e Telegram: WhatsApp non è una coda) e, con l'email spenta, le email rimaste in coda
// da guardare (D-27), dalla più vecchia.
type PaginaSMTP struct {
	Valori                               map[string]string
	PasswordSalvata, Configurata, Accesa bool
	InCoda, Accettati, Falliti           int
	Recenti, InSospeso                   []RigaCoda
}

// EsitoAMano è com'è andato un messaggio mandato a mano: il suo canale (email, o Telegram dalla 071), lo
// stato della riga dopo il tentativo (accepted, queued o failed), l'errore salvato (senza dati, come quello
// della coda) e i tentativi fatti.
type EsitoAMano struct {
	Canale        string
	Stato, Errore string
	Tentativi     int
}

// valoriSMTP sono i valori dei campi come la struttura Smtp: nil è nil (un campo vuoto), la porta un
// intero.
type valoriSMTP struct {
	testi map[string]*string
	porta *int64
}

func ptr(s string) *string { return &s }

// attuali è get(:email): un valore assente o di soli spazi è nil, porta 587 e starttls se mancano.
func attuali(c ConfigSMTP) valoriSMTP {
	v := valoriSMTP{testi: map[string]*string{}}
	for campo, s := range map[string]string{campoServer: c.Server, campoSicurezza: c.Sicurezza, campoUtente: c.Utente,
		campoPassword: c.Password.valore(), campoMittente: c.Mittente, campoNomeMittente: c.NomeMittente} {
		if s != "" {
			v.testi[campo] = ptr(s)
		}
	}
	p := int64(c.Porta)
	v.porta = &p
	return v
}

func uguali(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func ugualiPorta(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// vuoto è empty_trimmed_string?/1: il cast di Ecto fa nil un testo di soli spazi.
func vuoto(s string) bool { return strings.TrimSpace(s) == "" }

// interoElixir è Integer.parse/1 che deve consumare tutto (il cast di :integer): un segno facoltativo e
// solo cifre ASCII. Fuori dall'int64 resta un numero molto grande o molto piccolo, che la validazione
// rifiuta come lo standalone.
func interoElixir(s string) (int64, bool) {
	cifre := strings.TrimLeft(s, "+-")
	if len(s)-len(cifre) > 1 || cifre == "" {
		return 0, false
	}
	for i := 0; i < len(cifre); i++ {
		if cifre[i] < '0' || cifre[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		if s[0] == '-' {
			return math.MinInt64, true
		}
		return math.MaxInt64, true
	}
	return n, true
}

// spazioPCRE è \s di una regex di Elixir senza «u»: spazio, \t, \n, \v, \f, \r.
func spazioPCRE(b byte) bool { return b == ' ' || b >= '\t' && b <= '\r' }

// senzaACapoFinale toglie un solo «\n» in fondo: «$» di PCRE, senza D, combacia anche prima dell'ultimo
// a capo (come lo standalone, che accetta «x@y.it\n»).
func senzaACapoFinale(s string) string { return strings.TrimSuffix(s, "\n") }

// mittenteValido è validate_format(:from_address, ~r/^[^@\s]+@[^@\s]+\.[^@\s]{2,}$/), sui byte come una
// regex senza «u»: niente spazi, una sola «@» con qualcosa prima, e dopo un punto con almeno un byte prima
// e almeno due dopo.
func mittenteValido(s string) bool {
	s = senzaACapoFinale(s)
	for i := 0; i < len(s); i++ {
		if spazioPCRE(s[i]) {
			return false
		}
	}
	locale, dominio, ok := strings.Cut(s, "@")
	if !ok || locale == "" || strings.Contains(dominio, "@") {
		return false
	}
	for i := 1; i <= len(dominio)-3; i++ {
		if dominio[i] == '.' {
			return true
		}
	}
	return false
}

// nomeValido è validate_format(:from_name, ~r/^[^\r\n]*$/): nessun a capo, tranne l'ultimo «\n» che
// «$» lascia passare.
func nomeValido(s string) bool { return !strings.ContainsAny(senzaACapoFinale(s), "\r\n") }

// vero è truthy?/1 di keep_secret/3.
func vero(s string) bool { return s == "true" || s == "1" || s == "on" }

// SalvaSMTP è Config.put(:email, attrs, context): con la casella della password vuota tiene quella salvata
// (keep_secret/3), con «Cancella le credenziali salvate» toglie password e utente; poi Smtp.changeset/3
// (le validazioni guardano solo i campi cambiati, la regola delle credenziali i valori finali). Valido: in
// una transazione le sei impostazioni (nil = ""), la password in segreti_provider (nil la toglie) e
// outgoing.provider_configured con il canale, i nomi delle chiavi cambiate e se l'email è configurata;
// mai un valore. Rifiutato: niente scritto e il *RifiutoSMTP.
func (c *Coda) SalvaSMTP(ctx context.Context, m ModuloSMTP, a audit.Contesto) (*RifiutoSMTP, error) {
	cfg, err := LeggiSMTP(ctx, c.d.Lettura)
	if err != nil {
		return nil, err
	}
	prima := attuali(cfg)
	// keep_secret/3: i parametri come li vede cast/3 (un campo che c'è con nil è un nil esplicito).
	param := map[string]*string{}
	for k, v := range m {
		param[k] = ptr(v)
	}
	switch p, mandata := m[campoPassword]; {
	case vero(m[campoCancella]):
		param[campoPassword], param[campoUtente] = nil, nil
	case mandata && !vuoto(p):
	default:
		param[campoPassword] = prima.testi[campoPassword]
	}
	errori := map[string][]string{}
	sbaglia := func(campo, msg string) { errori[campo] = append(errori[campo], msg) }
	// cast/3: i cambiati sono quelli diversi dal valore di adesso.
	dopo := valoriSMTP{testi: map[string]*string{}, porta: prima.porta}
	for k, v := range prima.testi {
		dopo.testi[k] = v
	}
	cambiati := map[string]bool{}
	for _, campo := range CampiSMTP {
		v, c := param[campo]
		if !c {
			continue
		}
		if v != nil && vuoto(*v) {
			v = nil
		}
		if campo == campoPorta {
			var n *int64
			if v != nil {
				x, ok := interoElixir(*v)
				if !ok {
					sbaglia(campo, msgNonValido)
					continue
				}
				n = &x
			}
			if !ugualiPorta(n, prima.porta) {
				dopo.porta, cambiati[campo] = n, true
			}
			continue
		}
		if !uguali(v, prima.testi[campo]) {
			dopo.testi[campo], cambiati[campo] = v, true
		}
	}
	if v, c := param[campoCancella]; c && !vuoto(*v) && *v != "true" && *v != "1" && *v != "false" && *v != "0" {
		sbaglia(campoCancella, msgNonValido) // il cast di :boolean («on» no, anche se keep_secret/3 lo prende)
	}
	// Le validate_*/3, sui soli cambiati non nil (validate_change/3).
	if cambiati[campoPorta] && dopo.porta != nil {
		switch {
		case *dopo.porta <= 0:
			sbaglia(campoPorta, msgMaggioreDiZero)
		case *dopo.porta > portaMassima:
			sbaglia(campoPorta, msgAlMassimoPorta)
		}
	}
	nuovo := func(campo string) (string, bool) {
		v := dopo.testi[campo]
		if !cambiati[campo] || v == nil {
			return "", false
		}
		return *v, true
	}
	if s, ok := nuovo(campoSicurezza); ok && s != SicurezzaStartTLS && s != SicurezzaTLS && s != SicurezzaNessuna {
		sbaglia(campoSicurezza, msgNonValido)
	}
	if s, ok := nuovo(campoMittente); ok && !mittenteValido(s) {
		sbaglia(campoMittente, msgFormato)
	}
	if s, ok := nuovo(campoNomeMittente); ok && !nomeValido(s) {
		sbaglia(campoNomeMittente, msgFormato)
	}
	for _, l := range lunghezzeMassime {
		if s, ok := nuovo(l.campo); ok && uniseg.GraphemeClusterCount(s) > l.max {
			sbaglia(l.campo, msgTroppoLungo(l.max))
		}
	}
	// validate_credentials/1, sui valori finali (get_field/2).
	presente := func(campo string) bool { v := dopo.testi[campo]; return v != nil && !vuoto(*v) }
	if presente(campoUtente) && !presente(campoPassword) {
		sbaglia(campoPassword, msgPasswordUtente)
	}
	if presente(campoPassword) && uguali(dopo.testi[campoSicurezza], ptr(SicurezzaNessuna)) {
		sbaglia(campoPassword, msgPasswordChiaro)
	}
	if len(errori) > 0 {
		return rifiutoSMTP(errori, param, prima), nil
	}
	return nil, c.scriviSMTP(ctx, prima, dopo, a)
}

// rifiutoSMTP prepara il modulo rifiutato: gli errori di ogni campo dal più recente, come changeset.errors,
// e i valori mandati (input_value/2 legge i parametri), o quelli salvati per un campo che non c'era; mai
// la password.
func rifiutoSMTP(errori map[string][]string, param map[string]*string, prima valoriSMTP) *RifiutoSMTP {
	for k, e := range errori {
		for i, j := 0, len(e)-1; i < j; i, j = i+1, j-1 {
			e[i], e[j] = e[j], e[i]
		}
		errori[k] = e
	}
	r := &RifiutoSMTP{Errori: errori, Valori: valoriDelModulo(prima)}
	for _, campo := range CampiSMTP {
		if v, c := param[campo]; c && campo != campoPassword {
			r.Valori[campo] = ""
			if v != nil {
				r.Valori[campo] = *v
			}
		}
	}
	return r
}

// valoriDelModulo sono i valori salvati come li mostra il modulo (Config.change(:email)): la porta come
// numero, la password mai.
func valoriDelModulo(v valoriSMTP) map[string]string {
	out := map[string]string{}
	for _, campo := range CampiSMTP {
		switch {
		case campo == campoPassword:
			out[campo] = ""
		case campo == campoPorta:
			out[campo] = ""
			if v.porta != nil {
				out[campo] = strconv.FormatInt(*v.porta, 10)
			}
		case v.testi[campo] != nil:
			out[campo] = *v.testi[campo]
		default:
			out[campo] = ""
		}
	}
	return out
}

// scriviSMTP è persist/4: tutte le impostazioni, il segreto e l'audit in una transazione.
func (c *Coda) scriviSMTP(ctx context.Context, prima, dopo valoriSMTP, a audit.Contesto) error {
	cambiate := []string{}
	for _, campo := range CampiSMTP {
		if campo == campoPorta && !ugualiPorta(prima.porta, dopo.porta) || campo != campoPorta && !uguali(prima.testi[campo], dopo.testi[campo]) {
			cambiate = append(cambiate, campo)
		}
	}
	ora := c.o.Adesso().UTC().Format(db.FormatoIstante)
	tx, err := c.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	for _, campo := range CampiSMTP {
		if campo == campoPassword {
			continue
		}
		valore := ""
		switch {
		case campo == campoPorta && dopo.porta != nil:
			valore = strconv.FormatInt(*dopo.porta, 10)
		case campo != campoPorta && dopo.testi[campo] != nil:
			valore = *dopo.testi[campo]
		}
		if err := scriviImpostazione(ctx, tx, chiaviCampi[campo], valore, ora); err != nil {
			return err
		}
	}
	if p := dopo.testi[campoPassword]; p != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES (?, ?, ?)
			ON CONFLICT (nome) DO UPDATE SET valore = excluded.valore, aggiornato_il = excluded.aggiornato_il`, chiavePassword, *p, ora)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM segreti_provider WHERE nome = ?`, chiavePassword)
	}
	if err != nil {
		return err
	}
	if err := c.reg.Scrivi(ctx, tx, a, "outgoing.provider_configured", "outgoing_provider", CanaleEmail, "ok",
		map[string]any{"channel": CanaleEmail, "changed": cambiate, "configured": configDa(dopo).Usabile()}); err != nil {
		return err
	}
	return tx.Commit()
}

// configDa è la ConfigSMTP dei valori salvati (per «configured» dell'audit).
func configDa(v valoriSMTP) ConfigSMTP {
	s := func(campo string) string {
		if p := v.testi[campo]; p != nil {
			return *p
		}
		return ""
	}
	c := ConfigSMTP{Server: s(campoServer), Porta: portaPredefinita, Sicurezza: s(campoSicurezza), Utente: s(campoUtente),
		Password: NuovaPassword(s(campoPassword)), Mittente: s(campoMittente), NomeMittente: s(campoNomeMittente)}
	if c.Sicurezza == "" {
		c.Sicurezza = SicurezzaStartTLS
	}
	return c
}

func scriviImpostazione(ctx context.Context, tx *sql.Tx, chiave, valore, ora string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`, chiave, valore, ora)
	return err
}

// PaginaSMTP legge quello che la pagina mostra (load/1 di ProvidersLive, più l'interruttore e la revisione).
func (c *Coda) PaginaSMTP(ctx context.Context) (PaginaSMTP, error) {
	cfg, err := LeggiSMTP(ctx, c.d.Lettura)
	if err != nil {
		return PaginaSMTP{}, err
	}
	accesa, err := emailAccesa(ctx, c.d.Lettura)
	if err != nil {
		return PaginaSMTP{}, err
	}
	p := PaginaSMTP{Valori: valoriDelModulo(attuali(cfg)), PasswordSalvata: cfg.Password.Presente(), Configurata: cfg.Usabile(), Accesa: accesa}
	// counts_by_status/0: solo le righe della coda (email e Telegram).
	rr, err := c.d.Lettura.QueryContext(ctx, `SELECT stato, count(*) FROM messaggi_in_uscita WHERE canale IN ('email', 'telegram') GROUP BY stato`)
	if err != nil {
		return PaginaSMTP{}, err
	}
	defer rr.Close()
	for rr.Next() {
		var stato string
		var n int
		if err := rr.Scan(&stato, &n); err != nil {
			return PaginaSMTP{}, err
		}
		switch stato {
		case "queued":
			p.InCoda = n
		case "accepted":
			p.Accettati = n
		case "failed":
			p.Falliti = n
		}
	}
	if err := rr.Err(); err != nil {
		return PaginaSMTP{}, err
	}
	if p.Recenti, err = c.righe(ctx, `canale IN ('email', 'telegram') ORDER BY id DESC LIMIT ?`, righeRecentiPagina); err != nil {
		return PaginaSMTP{}, err
	}
	if !accesa {
		if p.InSospeso, err = c.righe(ctx, `canale = 'email' AND stato = 'queued' ORDER BY id`); err != nil {
			return PaginaSMTP{}, err
		}
	}
	return p, nil
}

// righe legge le righe della coda con la condizione dopo WHERE.
func (c *Coda) righe(ctx context.Context, dove string, arg ...any) ([]RigaCoda, error) {
	rr, err := c.d.Lettura.QueryContext(ctx, `SELECT id, busta_id, canale, destinatario, indirizzo, oggetto, testo, stato, tentativi, accodato_il,
		accettato_il, fallito_il, coalesce(riferimento_provider, ''), coalesce(ultimo_errore, ''),
		(SELECT b.numero FROM buste b WHERE b.id = messaggi_in_uscita.busta_id), (SELECT b.codice_qr FROM buste b WHERE b.id = messaggi_in_uscita.busta_id)
		FROM messaggi_in_uscita WHERE `+dove, arg...)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []RigaCoda
	for rr.Next() {
		var r RigaCoda
		var accodato string
		var accettato, fallito sql.NullString
		if err := rr.Scan(&r.ID, &r.BustaID, &r.Canale, &r.Destinatario, &r.Indirizzo, &r.Oggetto, &r.Testo, &r.Stato, &r.Tentativi, &accodato,
			&accettato, &fallito, &r.Riferimento, &r.UltimoErrore, &r.NumeroBusta, &r.CodiceQRBusta); err != nil {
			return nil, err
		}
		if r.AccodatoIl, err = time.Parse(db.FormatoIstante, accodato); err != nil {
			return nil, err
		}
		for _, x := range []struct {
			s   sql.NullString
			dst **time.Time
		}{{accettato, &r.AccettatoIl}, {fallito, &r.FallitoIl}} {
			if x.s.Valid {
				t, err := time.Parse(db.FormatoIstante, x.s.String)
				if err != nil {
					return nil, err
				}
				*x.dst = &t
			}
		}
		out = append(out, r)
	}
	return out, rr.Err()
}

// MandaProva è il messaggio di prova «subito» (decisione di Stefano dell'11/11, slice 069c): lo stesso
// messaggio di enqueue_test_message(:email, context) dello standalone (al mittente stesso, parte «shop»,
// nessuna busta, oggetto e testo fissi), ma mandato dentro la richiesta, senza riga nella coda, e anche con
// l'invio delle email spento (D-03): serve a controllare i dati del server prima di accendere. Differenza
// voluta: lo standalone lo accoda (outgoing.queued) e lo manda il giro.
// Senza configurazione usabile (o senza un provider che sa mandare a mano) ErrNonConfigurato, senza
// aprire niente. Altrimenti un tentativo col provider SMTP (i suoi tempi massimi, il contesto della
// richiesta: non c'è una riga, quindi niente da rimandare due volte) e l'esito: "" se il server l'ha
// accettato, se no il genere dell'errore come lo salva la coda («smtp:535», «smtp:transport:…»), mai il
// testo del server, l'indirizzo o la password. Audit outgoing.test_sent (risorsa outgoing_provider, email)
// con canale, parte e stato (accepted o failed), esito ok o error: mai indirizzo, testo o errore, come
// outgoing.queued_sent_by_hand. Un audit non scritto dopo l'invio va nel registro del server e l'esito
// resta quello vero.
func (c *Coda) MandaProva(ctx context.Context, a audit.Contesto) (string, error) {
	cfg, err := LeggiSMTP(ctx, c.d.Lettura)
	if err != nil {
		return "", err
	}
	p, ok := c.provider[CanaleEmail].(conInvioAMano)
	if !cfg.Usabile() || !ok {
		return "", ErrNonConfigurato
	}
	oggetto := oggettoProvaEmail
	m := Messaggio{Canale: CanaleEmail, Destinatario: "shop", Indirizzo: cfg.Mittente, Oggetto: &oggetto, Testo: testoProvaEmail}
	_, errore := c.provaCon(ctx, m, c.provider[CanaleEmail], p.ConsegnaAMano)
	risultato, stato := "ok", "accepted"
	if errore != "" {
		risultato, stato = "error", "failed"
	}
	if err := c.reg.Registra(context.WithoutCancel(ctx), a, "outgoing.test_sent", "outgoing_provider", CanaleEmail, risultato,
		map[string]any{"channel": CanaleEmail, "party": "shop", "status": stato}); err != nil {
		c.log.Error("coda in uscita: audit della prova non scritto", "tipo", fmt.Sprintf("%T", err))
	}
	return errore, nil
}

// ImpostaEmail accende o spegne l'invio delle email (D-03, solo Go). Il modulo porta lo stato che chiede:
// lo stesso di adesso non scrive niente (un doppio clic non torna indietro). Per accendere servono una
// configurazione usabile (ErrNonConfigurato) e nessuna email in coda (ErrEmailInSospeso, D-27: prima il
// titolare le guarda una per una). Scrive canale_email_acceso («1» o «0») e outgoing.channel_enabled o
// outgoing.channel_disabled con il canale, in una transazione.
func (c *Coda) ImpostaEmail(ctx context.Context, acceso bool, a audit.Contesto) error {
	return c.impostaCanale(ctx, CanaleEmail, acceso, a)
}

// interruttori sono, per canale, la chiave dell'interruttore della D-03, se la configurazione salvata è
// usabile e il rifiuto di un'accensione con delle righe in coda (D-27).
var interruttori = map[string]struct {
	chiave    string
	usabile   func(context.Context, db.Interrogatore) (bool, error)
	inSospeso error
	errAcceso error
}{
	CanaleEmail: {ChiaveEmailAcceso, func(ctx context.Context, q db.Interrogatore) (bool, error) {
		c, err := LeggiSMTP(ctx, q)
		return c.Usabile(), err
	}, ErrEmailInSospeso, ErrEmailAccesa},
	CanaleTelegram: {ChiaveTelegramAcceso, func(ctx context.Context, q db.Interrogatore) (bool, error) {
		c, err := LeggiTelegram(ctx, q)
		return c.Usabile(), err
	}, ErrTelegramInSospeso, ErrTelegramAcceso},
}

// canaleAcceso legge l'interruttore della D-03 del canale.
func canaleAcceso(ctx context.Context, q db.Interrogatore, canale string) (bool, error) {
	if canale == CanaleTelegram {
		return telegramAcceso(ctx, q)
	}
	return emailAccesa(ctx, q)
}

// impostaCanale è ImpostaEmail (e ImpostaTelegram, slice 071) per il canale dato.
func (c *Coda) impostaCanale(ctx context.Context, canale string, acceso bool, a audit.Contesto) error {
	k := interruttori[canale]
	c.aMano.Lock()
	defer c.aMano.Unlock()
	tx, err := c.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	adesso, err := canaleAcceso(ctx, tx, canale)
	if err != nil || adesso == acceso {
		return err
	}
	azione, valore := "outgoing.channel_disabled", "0"
	if acceso {
		azione, valore = "outgoing.channel_enabled", "1"
		usabile, err := k.usabile(ctx, tx)
		if err != nil {
			return err
		}
		if !usabile {
			return ErrNonConfigurato
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM messaggi_in_uscita WHERE canale = ? AND stato = 'queued'`, canale).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return k.inSospeso
		}
	}
	if err := scriviImpostazione(ctx, tx, k.chiave, valore, c.o.Adesso().UTC().Format(db.FormatoIstante)); err != nil {
		return err
	}
	if err := c.reg.Scrivi(ctx, tx, a, azione, "outgoing_provider", canale, "ok", map[string]any{"channel": canale}); err != nil {
		return err
	}
	return tx.Commit()
}

// inSospeso legge la riga in coda id: ErrNonInSospeso se non c'è, non è più in coda o non è di un canale
// della coda con un provider in questa coda (email, e Telegram dalla 071).
func (c *Coda) inSospeso(ctx context.Context, q db.Interrogatore, id int64) (Messaggio, error) {
	m, err := leggiMessaggio(q.QueryRowContext(ctx, `SELECT `+colonneMessaggio+` FROM messaggi_in_uscita
		WHERE id = ? AND canale IN ('email', 'telegram') AND stato = 'queued'`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Messaggio{}, ErrNonInSospeso
	}
	if err != nil {
		return Messaggio{}, err
	}
	if _, ok := c.provider[m.Canale]; !ok {
		return Messaggio{}, ErrNonInSospeso
	}
	return m, nil
}

// datiRiga sono i dati d'audit di una riga: canale, parte, busta; mai testo né indirizzo.
func datiRiga(m Messaggio) map[string]any {
	var busta any
	if m.BustaID != nil {
		busta = *m.BustaID
	}
	return map[string]any{"channel": m.Canale, "party": m.Destinatario, "repair_id": busta}
}

// ScartaInSospeso toglie dalla coda un messaggio che non deve partire (D-27, solo Go), con l'invio del suo
// canale spento: la riga va via e outgoing.queued_discarded (canale, parte, busta; mai testo né
// indirizzo), in una transazione. Dà il canale della riga. Con il canale acceso ErrEmailAccesa o
// ErrTelegramAcceso.
func (c *Coda) ScartaInSospeso(ctx context.Context, id int64, a audit.Contesto) (string, error) {
	c.aMano.Lock()
	defer c.aMano.Unlock()
	tx, err := c.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	m, err := c.inSospeso(ctx, tx, id)
	if err != nil {
		return "", err
	}
	switch acceso, err := canaleAcceso(ctx, tx, m.Canale); {
	case err != nil:
		return m.Canale, err
	case acceso:
		return m.Canale, interruttori[m.Canale].errAcceso
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM messaggi_in_uscita WHERE id = ? AND stato = 'queued'`, id); err != nil {
		return m.Canale, err
	}
	if err := c.reg.Scrivi(ctx, tx, a, "outgoing.queued_discarded", "outgoing_message", id, "ok", datiRiga(m)); err != nil {
		return m.Canale, err
	}
	return m.Canale, tx.Commit()
}

// MandaInSospeso manda adesso un solo messaggio rimasto in coda (D-27, solo Go), con l'invio del suo canale
// spento: un tentativo vero col provider senza l'interruttore (ConsegnaAMano), il cui esito si scrive sulla
// riga come in un giro della coda (accettata, o un tentativo con l'errore senza dati e l'attesa, al quinto
// fallita; per Telegram anche le regole del 400 e del 403), e outgoing.queued_sent_by_hand con canale,
// parte, busta e lo stato dopo (mai testo, indirizzo o errore). Con il canale acceso ErrEmailAccesa o
// ErrTelegramAcceso; senza configurazione ErrNonConfigurato, senza tentativo. Un solo messaggio per volta
// in tutto il processo (aMano, lo stesso del giro): due clic non lo mandano due volte. Un audit non
// scritto dopo l'invio va nel registro del server e l'esito resta quello vero.
func (c *Coda) MandaInSospeso(ctx context.Context, id int64, a audit.Contesto) (EsitoAMano, error) {
	// Una richiesta chiusa dal browser a metà non ferma l'invio né la scrittura del suo esito (review della
	// 069b): restano i tempi massimi del provider.
	ctx = context.WithoutCancel(ctx)
	c.aMano.Lock()
	defer c.aMano.Unlock()
	m, err := c.inSospeso(ctx, c.d.Lettura, id)
	if err != nil {
		return EsitoAMano{}, err
	}
	switch acceso, err := canaleAcceso(ctx, c.d.Lettura, m.Canale); {
	case err != nil:
		return EsitoAMano{Canale: m.Canale}, err
	case acceso:
		return EsitoAMano{Canale: m.Canale}, interruttori[m.Canale].errAcceso
	}
	usabile, err := interruttori[m.Canale].usabile(ctx, c.d.Lettura)
	if err != nil {
		return EsitoAMano{Canale: m.Canale}, err
	}
	p, ok := c.provider[m.Canale].(conInvioAMano)
	if !usabile || !ok {
		return EsitoAMano{Canale: m.Canale}, ErrNonConfigurato
	}
	rif, errore := c.provaCon(ctx, m, c.provider[m.Canale], p.ConsegnaAMano)
	var e Esito
	if _, err := c.salva(ctx, m, rif, errore, &e); err != nil {
		return EsitoAMano{Canale: m.Canale}, err
	}
	esito := EsitoAMano{Canale: m.Canale, Stato: "queued", Errore: errore, Tentativi: m.Tentativi + 1}
	switch {
	case e.Accettati > 0:
		esito.Stato = "accepted"
	case e.Falliti > 0:
		esito.Stato = "failed"
	}
	risultato := "ok"
	if errore != "" {
		risultato = "error"
	}
	dati := datiRiga(m)
	dati["status"] = esito.Stato
	// L'invio è già fatto e il suo esito scritto sulla riga: un audit che non si scrive va nel registro del
	// server (senza dati), e la pagina dice l'esito vero (review della 069b).
	if err := c.reg.Registra(ctx, a, "outgoing.queued_sent_by_hand", "outgoing_message", id, risultato, dati); err != nil {
		c.log.Error("coda in uscita: audit dell'invio a mano non scritto", "messaggio", id, "tipo", fmt.Sprintf("%T", err))
	}
	return esito, nil
}
