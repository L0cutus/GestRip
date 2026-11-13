package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// La pagina «Provider di invio» (ADM-09, CLI-52 parte email, slice 069b; ADM-10 e CLI-52 parte Telegram,
// slice 071), come ProvidersLive dello standalone (go-baseline), senza LiveView, solo per il titolare:
//   - GET /settings/providers: il riquadro «Posta SMTP» («Configurato» o «Non configurato», il modulo con
//     la password mai rimostrata, «✉ Invia messaggio di prova»), il riquadro «Telegram» (slice 071, sotto)
//     e la «Coda uscente» (conteggi e ultime 20 righe di email e Telegram); «↻ Aggiorna» è un link alla
//     pagina;
//   - POST /settings/providers/smtp è save_smtp: salvato, la pagina con «Provider salvato.»; rifiutato, la
//     pagina con gli errori sotto i campi e i valori mandati (mai la password);
//   - POST /settings/providers/prova è test di «email», ma subito (069c, decisione dell'11/11): la prova
//     parte dentro la richiesta al mittente stesso, senza riga nella coda, anche con l'invio delle email
//     spento; la pagina dice se il server l'ha accettata o il genere dell'errore. Senza configurazione il
//     rifiuto dello standalone.
// Solo del Go, nello stesso riquadro: l'interruttore dell'invio delle email (D-03: dopo l'import è spento
// finché il titolare non lo accende) e le email rimaste in coda da guardare prima di accendere (D-27):
//   - POST /settings/providers/accensione, campo acceso («1» accendi, altro spegni): il modulo porta lo stato
//     che chiede, così un doppio clic non torna indietro; non si accende senza configurazione né con delle
//     email in coda;
//   - POST /settings/providers/sospese/{id}/scarta e …/manda: un messaggio per volta (email, o Telegram
//     dalla 071), solo con l'invio del suo canale spento.
// Il riquadro Telegram (slice 071), come quello dello standalone:
//   - POST /settings/providers/telegram è save_telegram: token (mai rimostrato) e chat del negozio;
//   - POST /settings/providers/prova-telegram è test di «telegram»: getMe e, se il bot risponde, il messaggio
//     di prova alla chat del negozio, subito e senza coda come la posta (069c);
//   - POST /settings/providers/notifiche-clienti è toggle_customer_notifications (campo enabled, «true»
//     accende): «Notifiche Telegram ai clienti», spente di partenza; sotto, l'ultimo errore del lettore
//     (un 409 soprattutto) come lo salva lo standalone (il lettore arriva con la 073);
//   - solo del Go, POST /settings/providers/accensione-telegram (campo acceso): l'invio Telegram (D-03), che
//     non si accende con dei messaggi Telegram in coda (D-27).
// Né la password, né il token, né i testi dei messaggi vanno nel registro del server o nell'audit;
// l'indirizzo del Bot API non compare mai.

// ProviderInvio è la coda in uscita vista dalla pagina (uscita.Coda).
type ProviderInvio interface {
	PaginaSMTP(ctx context.Context) (uscita.PaginaSMTP, error)
	SalvaSMTP(ctx context.Context, m uscita.ModuloSMTP, a audit.Contesto) (*uscita.RifiutoSMTP, error)
	MandaProva(ctx context.Context, a audit.Contesto) (string, error)
	ImpostaEmail(ctx context.Context, acceso bool, a audit.Contesto) error
	ScartaInSospeso(ctx context.Context, id int64, a audit.Contesto) (string, error)
	MandaInSospeso(ctx context.Context, id int64, a audit.Contesto) (uscita.EsitoAMano, error)
	PaginaTelegram(ctx context.Context) (uscita.PaginaTelegram, error)
	SalvaTelegram(ctx context.Context, m uscita.ModuloTelegram, a audit.Contesto) (*uscita.RifiutoTelegram, error)
	MandaProvaTelegram(ctx context.Context, a audit.Contesto) (uscita.EsitoProvaTelegram, error)
	ImpostaTelegram(ctx context.Context, acceso bool, a audit.Contesto) error
	ImpostaNotificheClienti(ctx context.Context, accese bool, a audit.Contesto) error
}

// I messaggi del riquadro Telegram: quelli dello standalone (ProvidersLive) e, solo del Go, quelli della
// prova subito, dell'interruttore e della revisione.
const (
	msgTelegramNonConfig    = "Telegram non è configurato: niente è stato accodato."
	msgProvaTelegramOK      = "Messaggio di prova accettato da Telegram: guarda se è arrivato nella chat del negozio."
	msgNotificheAccese      = "Notifiche Telegram ai clienti accese."
	msgNotificheSpente      = "Notifiche Telegram ai clienti spente."
	msgTelegramAcceso       = "Invio Telegram acceso: da adesso la coda manda i messaggi da sola."
	msgTelegramSpento       = "Invio Telegram spento: i messaggi restano in coda finché non lo riaccendi."
	msgPrimaGuardaTelegram  = "Prima di accendere guarda i messaggi Telegram rimasti in coda: mandali o scartali uno per uno."
	msgAccendiTelegramNo    = "Telegram non è configurato: l'invio Telegram non si accende."
	msgTelegramScartato     = "Messaggio Telegram scartato: non partirà."
	msgLaMandaLaCodaTg      = "L'invio Telegram è acceso: i messaggi in coda li manda la coda."
	msgTelegramAccettato    = "Messaggio Telegram accettato dal provider."
	msgMandaTelegramNo      = "Telegram non è configurato: niente è stato inviato."
	msgTelegramNonMandato   = "Non è stato possibile mandare il messaggio Telegram."
	msgTelegramNonScartato  = "Non è stato possibile scartare il messaggio Telegram."
	confermaScartaTelegram  = "Scartare questo messaggio? Non partirà più."
	segnapostoToken         = "salvato — lascia vuoto per non cambiarlo"
	aiutoChatTelegram       = "Usa un gruppo o una chat diversa da quella con cui provi il bot da cliente: i messaggi da questa chat sono ignorati."
	testoNotificheClienti   = "Accese, l'installazione legge i messaggi che arrivano al bot. Per ora risponde a chiunque scriva che le notifiche si attivano dal link della riparazione: l'iscrizione dei clienti non c'è ancora."
	msgBotNonConfigurato    = "Il bot non è configurato: niente viene letto finché non lo è."
	msgConflittoLettore     = "Telegram rifiuta la lettura perché sullo stesso token è attivo un webhook o un'altra installazione sta leggendo. L'installazione non rimuove il webhook da sola e riprova con un'attesa crescente: va tolto il webhook o fermato l'altro lettore."
	msgTelegramInterruttore = "Non è stato possibile salvare l'interruttore."
	msgTelegramAccesoNonCfg = "L'invio Telegram è acceso ma Telegram non è configurato: nessun messaggio Telegram parte finché non completi token e chat qui sopra."
)

// datiTelegram è il riquadro Telegram della pagina.
type datiTelegram struct {
	Configurata, Acceso, NotificheClienti bool
	Token, Chat                           campoProvider
	AiutoChat                             string
	NomeBot                               string
	TestoNotifiche, BotNonConfigurato     string // il secondo "" se non serve
	AccesoNonConfig                       string // la riga «acceso ma non configurato» (review della 071), "" se non serve
	ChatSconosciute                       int
	ErroreLettore, ErroreLettoreIl        string
	Conflitto                             string // "" senza un 409
	InSospeso                             []rigaProvider
	ConfermaScarta                        string
}

const paginaProviderPercorso = "/settings/providers"

// I messaggi dello standalone (ProvidersLive) e, solo del Go, quelli dell'interruttore e della revisione.
const (
	msgProviderSalvato  = "Provider salvato."
	msgProvaAccettata   = "Messaggio di prova accettato da Posta SMTP: guarda se è arrivato nella casella del mittente."
	msgPostaNonConfig   = "Posta SMTP non è configurato: niente è stato accodato."
	msgProvaNonMandata  = "Non è stato possibile mandare la prova."
	msgAccesaNonConfig  = "L'invio delle email è acceso ma Posta SMTP non è configurato: nessuna email parte finché non completi i dati del server qui sopra."
	msgSenzaIndirizzo   = "Senza indirizzo pubblico il riquadro «avvisami via email» della pagina del cliente non compare e l'email di pronta non parte: "
	msgEmailAccesa      = "Invio delle email acceso: da adesso la coda le manda da sola."
	msgEmailSpenta      = "Invio delle email spento: le email restano in coda finché non lo riaccendi."
	msgPrimaGuarda      = "Prima di accendere guarda le email rimaste in coda: mandale o scartale una per una."
	msgAccendiNonConfig = "Posta SMTP non è configurato: l'invio delle email non si accende."
	msgInterruttore     = "Non è stato possibile salvare l'interruttore."
	msgEmailScartata    = "Email scartata: non partirà."
	msgNonPiuInCoda     = "Questa email non è più in coda."
	msgLaMandaLaCoda    = "L'invio delle email è acceso: le email in coda le manda la coda."
	msgEmailAccettata   = "Email accettata dal provider."
	msgMandaNonConfig   = "Posta SMTP non è configurato: niente è stato inviato."
	msgEmailNonMandata  = "Non è stato possibile mandare l'email."
	msgEmailNonScartata = "Non è stato possibile scartare l'email."
	confermaScarta      = "Scartare questa email? Non partirà più."
	introProvider       = "Questa installazione manda i messaggi con account propri: nessun servizio centrale, nessun bot condiviso. La coda in fondo dice fin dove è arrivato ogni messaggio, e si ferma ad «accettato dal provider»: nessuno qui può sapere se è stato letto."
	segnapostoPassword  = "salvata — lascia vuoto per non cambiarla"
)

// campoProvider è un campo del modulo SMTP (l'input di core_components).
type campoProvider struct {
	ID, Nome, Etichetta, Tipo, Valore, Segnaposto string
	Password                                      bool
	Errori                                        []string
	Scelte                                        []opzione
}

// rigaProvider è una riga della coda nella pagina.
type rigaProvider struct {
	ID                                       int64
	Canale, Stato, ClasseStato, Destinatario string
	Accodato                                 string
	Tentativi, Massimo                       int
	Accettato, Riferimento, Fallito, Errore  string
	Oggetto, Testo                           string
	Busta                                    string // il numero visibile della D-64, "" senza busta
}

// datiProvider è la pagina.
type datiProvider struct {
	Intro                      string
	Configurata, Accesa        bool
	Campi                      []campoProvider
	InCoda, Accettati, Falliti int
	Recenti, InSospeso         []rigaProvider
	ConfermaScarta             string
	AccesaNonConfig            string // la riga «acceso ma non configurato» (069c), "" se non serve
	SenzaIndirizzo             string // la riga «acceso senza indirizzo pubblico» (070b), "" se non serve
	Telegram                   datiTelegram
}

// I campi del modulo come li disegna ProvidersLive: etichetta, tipo, segnaposto; le sicurezze con le loro
// etichette (security_label/1).
var (
	campiSMTP = []struct{ nome, etichetta, tipo, segnaposto string }{
		{"host", "Server", "text", "smtp.esempio.it"}, {"port", "Porta", "number", ""}, {"security", "Sicurezza", "select", ""},
		{"username", "Utente", "text", ""}, {"password", "Password", "password", ""}, {"from_address", "Indirizzo mittente", "text", ""},
		{"from_name", "Nome mittente", "text", ""},
	}
	sicurezzeSMTP = []opzione{{Valore: "starttls", Etichetta: "STARTTLS (consigliato)"}, {Valore: "tls", Etichetta: "TLS diretto"},
		{Valore: "none", Etichetta: "Nessuna (relay locale, senza credenziali)"}}
)

func (f *facciaWeb) registraProvider(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /settings/providers", soloTitolare, f.paginaProvider)
	f.rottaConGuscio(mux, "POST /settings/providers/smtp", soloTitolare, f.salvaProvider) // ridisegna dopo un rifiuto
	f.rotta(mux, "POST /settings/providers/prova", soloTitolare, f.provaProvider)
	f.rotta(mux, "POST /settings/providers/accensione", soloTitolare, f.accensioneEmail)
	f.rotta(mux, "POST /settings/providers/sospese/{id}/scarta", soloTitolare, f.scartaSospesa)
	f.rotta(mux, "POST /settings/providers/sospese/{id}/manda", soloTitolare, f.mandaSospesa)
	f.rottaConGuscio(mux, "POST /settings/providers/telegram", soloTitolare, f.salvaTelegram) // ridisegna dopo un rifiuto
	f.rotta(mux, "POST /settings/providers/prova-telegram", soloTitolare, f.provaTelegram)
	f.rotta(mux, "POST /settings/providers/notifiche-clienti", soloTitolare, f.notificheClienti)
	f.rotta(mux, "POST /settings/providers/accensione-telegram", soloTitolare, f.accensioneTelegram)
}

// etichettaCanale è channel_label/1.
func etichettaCanale(canale string) string {
	if n, ok := nomiCanale[canale]; ok {
		return n
	}
	return canale
}

// indirizzoCoda è address_label/1.
func indirizzoCoda(indirizzo string) string {
	if indirizzo == "contatto-scelto" {
		return "contatto scelto sul telefono"
	}
	return indirizzo
}

// classeStato è badge/1.
func classeStato(stato string) string {
	switch stato {
	case "accepted":
		return "badge-ok"
	case "failed":
		return "badge-errore"
	}
	return "badge-attesa"
}

func rigaDellaPagina(r uscita.RigaCoda) rigaProvider {
	x := rigaProvider{ID: r.ID, Canale: etichettaCanale(r.Canale), Stato: statoInvio(r.Canale, r.Stato), ClasseStato: classeStato(r.Stato),
		Destinatario: indirizzoCoda(r.Indirizzo), Accodato: clock.Timbro(r.AccodatoIl), Tentativi: r.Tentativi, Massimo: uscita.MaxTentativi,
		Riferimento: r.Riferimento, Errore: r.UltimoErrore, Testo: testoInvio(r.Canale, r.Testo)}
	if r.AccettatoIl != nil {
		x.Accettato = clock.Timbro(*r.AccettatoIl)
	}
	if r.FallitoIl != nil {
		x.Fallito = clock.Timbro(*r.FallitoIl)
	}
	if r.Oggetto != nil {
		x.Oggetto = *r.Oggetto
	}
	if r.NumeroBusta != nil {
		qr := ""
		if r.CodiceQRBusta != nil {
			qr = *r.CodiceQRBusta
		}
		x.Busta, _ = buste.NumeroVisibile(qr, *r.NumeroBusta) // D-64
	}
	return x
}

// mostraProvider disegna la pagina; rif e rifTg sono un salvataggio rifiutato (valori ed errori del
// modulo della posta o del Telegram).
func (f *facciaWeb) mostraProvider(w http.ResponseWriter, r *http.Request, rif *uscita.RifiutoSMTP, rifTg *uscita.RifiutoTelegram) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	p, err := f.uscita.PaginaSMTP(r.Context())
	var tg datiTelegram
	if err == nil {
		tg, err = f.riquadroTelegram(r.Context(), rifTg)
	}
	if err != nil {
		f.registro.Error("pagina dei provider non letta")
		Errore(w, r)
		return
	}
	d := datiProvider{Intro: introProvider, Configurata: p.Configurata, Accesa: p.Accesa, InCoda: p.InCoda, Accettati: p.Accettati,
		Falliti: p.Falliti, ConfermaScarta: confermaScarta, Telegram: tg}
	if p.Accesa && !p.Configurata {
		d.AccesaNonConfig = msgAccesaNonConfig
	}
	// Dubbio 1 della 070 (slice 070b): l'email accesa senza indirizzo_pubblico non ha il riquadro della
	// pagina del cliente né l'email di pronta (D-16, D-46), e la pagina lo dice con cosa configurare.
	if p.Accesa && f.pubblico == "" {
		d.SenzaIndirizzo = msgSenzaIndirizzo + f.comeConfigurare() + "."
	}
	valori, errori := p.Valori, map[string][]string{}
	if rif != nil {
		valori, errori = rif.Valori, rif.Errori
	}
	for _, c := range campiSMTP {
		x := campoProvider{ID: "smtp_" + c.nome, Nome: "smtp[" + c.nome + "]", Etichetta: c.etichetta, Tipo: c.tipo, Valore: valori[c.nome],
			Segnaposto: c.segnaposto, Errori: errori[c.nome]}
		switch c.nome {
		case "password":
			// La password non torna mai (value="" anche dopo un rifiuto); il segnaposto dice se ce n'è una.
			x.Valore, x.Password = "", true
			if p.PasswordSalvata {
				x.Segnaposto = segnapostoPassword
			}
		case "security":
			for _, s := range sicurezzeSMTP {
				s.Scelta = s.Valore == x.Valore
				x.Scelte = append(x.Scelte, s)
			}
		}
		d.Campi = append(d.Campi, x)
	}
	for _, x := range p.Recenti {
		d.Recenti = append(d.Recenti, rigaDellaPagina(x))
	}
	for _, x := range p.InSospeso {
		d.InSospeso = append(d.InSospeso, rigaDellaPagina(x))
	}
	f.mostraCon(w, r, "provider", http.StatusOK, "", d)
}

// paginaProvider è GET /settings/providers.
func (f *facciaWeb) paginaProvider(w http.ResponseWriter, r *http.Request) {
	f.mostraProvider(w, r, nil, nil)
}

// riquadroTelegram prepara il riquadro Telegram (load/1 e TelegramInbound.status/0); rif è un salvataggio
// rifiutato. Il token non torna mai: la casella è sempre vuota, il segnaposto dice se ce n'è uno.
func (f *facciaWeb) riquadroTelegram(ctx context.Context, rif *uscita.RifiutoTelegram) (datiTelegram, error) {
	p, err := f.uscita.PaginaTelegram(ctx)
	if err != nil {
		return datiTelegram{}, err
	}
	d := datiTelegram{Configurata: p.Configurata, Acceso: p.Acceso, NotificheClienti: p.NotificheClienti, AiutoChat: aiutoChatTelegram,
		NomeBot: p.NomeBot, TestoNotifiche: testoNotificheClienti, ChatSconosciute: p.ChatSconosciute, ErroreLettore: p.UltimoErrore,
		ConfermaScarta: confermaScartaTelegram}
	d.Token = campoProvider{ID: "telegram_bot_token", Nome: "telegram[bot_token]", Etichetta: "Token del bot", Tipo: "password", Password: true}
	if p.TokenSalvato {
		d.Token.Segnaposto = segnapostoToken
	}
	d.Chat = campoProvider{ID: "telegram_chat_id", Nome: "telegram[chat_id]", Etichetta: "Chat di destinazione", Tipo: "text", Valore: p.Chat}
	if rif != nil {
		d.Token.Errori, d.Chat.Errori, d.Chat.Valore = rif.Errori["bot_token"], rif.Errori["chat_id"], rif.Valori["chat_id"]
	}
	if p.NotificheClienti && !p.Configurata {
		d.BotNonConfigurato = msgBotNonConfigurato
	}
	// Come la posta (069c): l'invio acceso con il bot non più configurato non manda niente, e la pagina lo dice.
	if p.Acceso && !p.Configurata {
		d.AccesoNonConfig = msgTelegramAccesoNonCfg
	}
	if p.UltimoErroreIl != nil {
		d.ErroreLettoreIl = clock.Timbro(*p.UltimoErroreIl)
	}
	if p.Conflitto {
		d.Conflitto = msgConflittoLettore
	}
	for _, x := range p.InSospeso {
		d.InSospeso = append(d.InSospeso, rigaDellaPagina(x))
	}
	return d, nil
}

// tornaAiProvider torna alla pagina con il messaggio: info, o errore se errore è vero.
func (f *facciaWeb) tornaAiProvider(w http.ResponseWriter, r *http.Request, msg string, errore bool) {
	st := statoDi(r)
	if errore {
		st.dati.Errore = msg
	} else {
		st.dati.Info = msg
	}
	st.cambiata = true
	f.vaiA(w, r, paginaProviderPercorso)
}

// salvaProvider è save_smtp: i campi smtp[…] che il modulo manda (uno che manca resta quello salvato).
func (f *facciaWeb) salvaProvider(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	m := uscita.ModuloSMTP{}
	for _, c := range append(append([]string(nil), uscita.CampiSMTP...), "clear_secret") {
		if v, ok := ultimo(r, "smtp["+c+"]"); ok {
			m[c] = v
		}
	}
	rif, err := f.uscita.SalvaSMTP(r.Context(), m, contestoDi(r))
	switch {
	case err != nil:
		f.registro.Error("provider di posta non salvato")
		Errore(w, r)
	case rif != nil:
		f.mostraProvider(w, r, rif, nil)
	default:
		f.tornaAiProvider(w, r, msgProviderSalvato, false)
	}
}

// provaProvider è test di «email», subito (069c): uscita.MandaProva. L'esito dice solo se il server l'ha
// accettata o il genere dell'errore (quello che salverebbe la coda), mai testo del server, indirizzo o
// password.
func (f *facciaWeb) provaProvider(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	errore, err := f.uscita.MandaProva(r.Context(), contestoDi(r))
	switch {
	case err == nil && errore == "":
		f.tornaAiProvider(w, r, msgProvaAccettata, false)
	case err == nil:
		f.tornaAiProvider(w, r, fmt.Sprintf("Messaggio di prova non partito (%s): controlla i dati del server e riprova.", errore), true)
	case errors.Is(err, uscita.ErrNonConfigurato):
		f.tornaAiProvider(w, r, msgPostaNonConfig, true)
	default:
		f.registro.Error("prova della posta non mandata")
		f.tornaAiProvider(w, r, msgProvaNonMandata, true)
	}
}

// accensioneEmail accende o spegne l'invio delle email (D-03, solo Go).
func (f *facciaWeb) accensioneEmail(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	v, _ := ultimo(r, "acceso")
	acceso := v == "1"
	err := f.uscita.ImpostaEmail(r.Context(), acceso, contestoDi(r))
	switch {
	case err == nil && acceso:
		f.tornaAiProvider(w, r, msgEmailAccesa, false)
	case err == nil:
		f.tornaAiProvider(w, r, msgEmailSpenta, false)
	case errors.Is(err, uscita.ErrEmailInSospeso):
		f.tornaAiProvider(w, r, msgPrimaGuarda, true)
	case errors.Is(err, uscita.ErrNonConfigurato):
		f.tornaAiProvider(w, r, msgAccendiNonConfig, true)
	default:
		f.registro.Error("interruttore dell'email non salvato")
		f.tornaAiProvider(w, r, msgInterruttore, true)
	}
}

// scartaSospesa scarta un messaggio rimasto in coda (D-27, solo Go): un'email, o un messaggio Telegram
// (slice 071), con le frasi del suo canale.
func (f *facciaWeb) scartaSospesa(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	err, canale := uscita.ErrNonInSospeso, ""
	if id, ok := idPositivo(r.PathValue("id")); ok {
		canale, err = f.uscita.ScartaInSospeso(r.Context(), id, contestoDi(r))
	}
	tg := canale == uscita.CanaleTelegram
	switch {
	case err == nil && tg:
		f.tornaAiProvider(w, r, msgTelegramScartato, false)
	case err == nil:
		f.tornaAiProvider(w, r, msgEmailScartata, false)
	case errors.Is(err, uscita.ErrNonInSospeso):
		f.tornaAiProvider(w, r, msgNonPiuInCoda, true)
	case errors.Is(err, uscita.ErrEmailAccesa):
		f.tornaAiProvider(w, r, msgLaMandaLaCoda, true)
	case errors.Is(err, uscita.ErrTelegramAcceso):
		f.tornaAiProvider(w, r, msgLaMandaLaCodaTg, true)
	case tg:
		f.registro.Error("messaggio Telegram in coda non scartato")
		f.tornaAiProvider(w, r, msgTelegramNonScartato, true)
	default:
		f.registro.Error("email in coda non scartata")
		f.tornaAiProvider(w, r, msgEmailNonScartata, true)
	}
}

// mandaSospesa manda adesso un messaggio rimasto in coda (D-27, solo Go): l'esito dice se il provider l'ha
// preso, o l'errore senza dati (quello che la coda salva) e cosa succede alla riga, con le parole del suo
// canale.
func (f *facciaWeb) mandaSospesa(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	err := uscita.ErrNonInSospeso
	var e uscita.EsitoAMano
	if id, ok := idPositivo(r.PathValue("id")); ok {
		e, err = f.uscita.MandaInSospeso(r.Context(), id, contestoDi(r))
	}
	tg := e.Canale == uscita.CanaleTelegram
	cosa, femminile := "Email", true
	if tg {
		cosa, femminile = "Messaggio Telegram", false
	}
	switch {
	case err == nil && e.Stato == "accepted" && tg:
		f.tornaAiProvider(w, r, msgTelegramAccettato, false)
	case err == nil && e.Stato == "accepted":
		f.tornaAiProvider(w, r, msgEmailAccettata, false)
	case err == nil && e.Stato == "failed":
		segnata := "segnata come fallita"
		if !femminile {
			segnata = "segnato come fallito"
		}
		f.tornaAiProvider(w, r, fmt.Sprintf("%s non partit%s (%s): %s.", cosa, finale(femminile), e.Errore, segnata), true)
	case err == nil:
		f.tornaAiProvider(w, r, fmt.Sprintf("%s non partit%s (%s): resta in coda, tentativi %d di %d.", cosa, finale(femminile), e.Errore, e.Tentativi,
			uscita.MaxTentativi), true)
	case errors.Is(err, uscita.ErrNonInSospeso):
		f.tornaAiProvider(w, r, msgNonPiuInCoda, true)
	case errors.Is(err, uscita.ErrEmailAccesa):
		f.tornaAiProvider(w, r, msgLaMandaLaCoda, true)
	case errors.Is(err, uscita.ErrTelegramAcceso):
		f.tornaAiProvider(w, r, msgLaMandaLaCodaTg, true)
	case errors.Is(err, uscita.ErrNonConfigurato) && tg:
		f.tornaAiProvider(w, r, msgMandaTelegramNo, true)
	case errors.Is(err, uscita.ErrNonConfigurato):
		f.tornaAiProvider(w, r, msgMandaNonConfig, true)
	case tg:
		f.registro.Error("messaggio Telegram in coda non mandato")
		f.tornaAiProvider(w, r, msgTelegramNonMandato, true)
	default:
		f.registro.Error("email in coda non mandata")
		f.tornaAiProvider(w, r, msgEmailNonMandata, true)
	}
}

// finale è la vocale finale del participio: «partita» per l'email, «partito» per il messaggio Telegram.
func finale(femminile bool) string {
	if femminile {
		return "a"
	}
	return "o"
}

// salvaTelegram è save_telegram: i campi telegram[…] che il modulo manda (uno che manca resta il salvato).
func (f *facciaWeb) salvaTelegram(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	m := uscita.ModuloTelegram{}
	for _, c := range append(append([]string(nil), uscita.CampiTelegram...), "clear_secret") {
		if v, ok := ultimo(r, "telegram["+c+"]"); ok {
			m[c] = v
		}
	}
	rif, err := f.uscita.SalvaTelegram(r.Context(), m, contestoDi(r))
	switch {
	case err != nil:
		f.registro.Error("provider Telegram non salvato")
		Errore(w, r)
	case rif != nil:
		f.mostraProvider(w, r, nil, rif)
	default:
		f.tornaAiProvider(w, r, msgProviderSalvato, false)
	}
}

// provaTelegram è test di «telegram», subito (come la posta della 069c): getMe, poi il messaggio di prova
// alla chat del negozio. L'esito dice il motivo senza token.
func (f *facciaWeb) provaTelegram(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	e, err := f.uscita.MandaProvaTelegram(r.Context(), contestoDi(r))
	switch {
	case errors.Is(err, uscita.ErrNonConfigurato):
		f.tornaAiProvider(w, r, msgTelegramNonConfig, true)
	case err != nil:
		f.registro.Error("prova di Telegram non mandata")
		f.tornaAiProvider(w, r, msgProvaNonMandata, true)
	case e.GetMe != "":
		f.tornaAiProvider(w, r, fmt.Sprintf("Il bot non risponde a getMe (%s): niente è stato accodato.", e.GetMe), true)
	case e.Errore != "":
		f.tornaAiProvider(w, r, fmt.Sprintf("Messaggio di prova non partito (%s): controlla il token e la chat e riprova.", e.Errore), true)
	default:
		f.tornaAiProvider(w, r, msgProvaTelegramOK, false)
	}
}

// notificheClienti è toggle_customer_notifications: il modulo porta lo stato che chiede (enabled «true»
// accende, ogni altro valore spegne).
func (f *facciaWeb) notificheClienti(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	v, _ := ultimo(r, "enabled")
	accese := v == "true"
	switch err := f.uscita.ImpostaNotificheClienti(r.Context(), accese, contestoDi(r)); {
	case err != nil:
		f.registro.Error("interruttore delle notifiche Telegram non salvato")
		f.tornaAiProvider(w, r, msgTelegramInterruttore, true)
	case accese:
		f.tornaAiProvider(w, r, msgNotificheAccese, false)
	default:
		f.tornaAiProvider(w, r, msgNotificheSpente, false)
	}
}

// accensioneTelegram accende o spegne l'invio Telegram (D-03, solo Go).
func (f *facciaWeb) accensioneTelegram(w http.ResponseWriter, r *http.Request) {
	if f.uscita == nil {
		Errore(w, r)
		return
	}
	v, _ := ultimo(r, "acceso")
	acceso := v == "1"
	err := f.uscita.ImpostaTelegram(r.Context(), acceso, contestoDi(r))
	switch {
	case err == nil && acceso:
		f.tornaAiProvider(w, r, msgTelegramAcceso, false)
	case err == nil:
		f.tornaAiProvider(w, r, msgTelegramSpento, false)
	case errors.Is(err, uscita.ErrTelegramInSospeso):
		f.tornaAiProvider(w, r, msgPrimaGuardaTelegram, true)
	case errors.Is(err, uscita.ErrNonConfigurato):
		f.tornaAiProvider(w, r, msgAccendiTelegramNo, true)
	default:
		f.registro.Error("interruttore di Telegram non salvato")
		f.tornaAiProvider(w, r, msgTelegramInterruttore, true)
	}
}
