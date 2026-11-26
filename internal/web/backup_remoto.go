package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"syscall"

	"github.com/L0cutus/gestrip/internal/backup"
)

// La copia remota dei backup nella pagina Impostazioni → Backup (slice 129, T5.2, OPS-16 destinazione remota), solo
// per il titolare (CSRF e ruolo dalla catena di f.rotta). Le regole sono quelle del comando gestrip backup-remoto
// della 128, con le stesse funzioni (backup.FissaRemoto):
//
//   - POST /settings/backup/chiave-sftp crea la chiave dell'apparecchio (se manca; con il lucchetto dei backup). La
//     pagina mostra la parte pubblica, da copiare nel pannello della Storage Box; la privata non esce mai.
//   - POST /settings/backup/remoto/impronta è il primo passo: con host, porta, utente e cartella si collega solo
//     fino alla chiave del server e ne mostra tipo e impronta. Non scrive niente.
//   - POST /settings/backup/remoto è il secondo passo, la conferma esplicita: il titolare riscrive (o incolla)
//     l'impronta pubblicata dal fornitore; la configurazione si scrive solo se il server si presenta con quella, se
//     la chiave dell'apparecchio entra e se la cartella c'è. La chiave del server si rilegge sempre e il confronto è
//     con quella vera; dopo un errore il secondo passo torna solo con l'impronta appena riletta dal server (impronta
//     diversa), mai con un valore arrivato dal modulo (revisione 129, B6).
//   - POST /settings/backup/remoto/prova prova la configurazione salvata (senza scrivere niente).
//   - POST /settings/backup/remoto/togli spegne la copia remota, con una spunta di conferma (anche una configurazione
//     che non si legge), non durante una prova in corso.
//
// Ogni operazione di rete ha il tempo massimo di backup.TempoMassimoProvaRemota e ce n'è una alla volta (la seconda
// risponde subito «prova in corso»): gira nella richiesta del titolare e non tiene lucchetti che le altre pagine
// aspettano. Funziona senza JavaScript (moduli normali). Nei messaggi niente segreti né errori grezzi del server:
// host, utente e cartella li ha scritti il titolare e si vedono solo nella sua pagina (mai nel banner né negli
// avvisi, come nella 128).

// CopiaRemotaDelNegozio è la configurazione della copia remota vista dalla faccia (backup.ConfigurazioneRemota).
type CopiaRemotaDelNegozio interface {
	Leggi(ctx context.Context) (backup.ConfigRemota, bool, error)
	ChiavePubblica() (string, error)
	CreaChiave() (pubblica string, creata bool, err error)
	ChiaveDelServer(ctx context.Context, nuova backup.ConfigRemota) (tipo, impronta string, err error)
	Fissa(ctx context.Context, nuova backup.ConfigRemota, voluta string) (backup.ConfigRemota, error)
	Prova(ctx context.Context) (cartella string, err error)
	Togli(ctx context.Context) error
}

const (
	rottaChiaveSFTP      = rottaPaginaBackup + "/chiave-sftp"
	rottaRemoto          = rottaPaginaBackup + "/remoto"
	rottaImprontaRemoto  = rottaRemoto + "/impronta"
	rottaProvaRemoto     = rottaRemoto + "/prova"
	rottaTogliRemoto     = rottaRemoto + "/togli"
	msgChiaveCreata      = "Chiave dell'apparecchio creata. Copia la chiave pubblica qui sotto nel pannello della Storage Box, nelle chiavi SSH del sottoaccount dei backup."
	msgChiaveGiaPresente = "La chiave dell'apparecchio c'era già e non è cambiata."
	msgChiaveInBackup    = "Un backup è in corso: crea la chiave quando ha finito."
	msgChiaveNonCreata   = "La chiave dell'apparecchio non si è potuta creare. Riprova; se si ripete, chiedi assistenza."
	msgRemotoSalvato     = "Copia remota configurata: impronta del server fissata, chiave dell'apparecchio accettata, cartella trovata. Da stanotte, dopo il backup sulla chiavetta, il backup va anche sul server."
	msgProvaRemotaOK     = "Prova riuscita: il server è quello configurato, ha accettato la chiave dell'apparecchio e la cartella c'è."
	msgRemotoSpento      = "Copia remota spenta. Gli archivi già sul server restano: toglili dal pannello della Storage Box se non servono più."
	msgSpegniConferma    = "Per spegnere la copia remota spunta la conferma."
	msgRemotoNonSpento   = "La copia remota non si è potuta spegnere. Riprova; se si ripete, chiedi assistenza."
	msgVediImpronta      = "Controlla l'impronta del server qui sotto prima di andare avanti."
)

// testoErroreRemoto è l'errore di un'operazione con il server in parole del titolare, con il campo del modulo da
// segnare ("" se nessuno). Mai il testo grezzo dell'errore (può avere indirizzi, percorsi del server).
func testoErroreRemoto(err error) (testo, campo string) {
	var campoErr *backup.ErrCampoRemoto
	var diversa *backup.ErrImprontaDiversa
	switch {
	case errors.Is(err, backup.ErrProvaInCorso):
		return "Un'altra prova del collegamento è in corso: riprova fra poco.", ""
	case errors.Is(err, backup.ErrRemotoNonConfigurato):
		return "La copia remota non è configurata: non c'è niente da provare.", ""
	case errors.As(err, &campoErr):
		if t, ok := testoCampoRemoto[campoErr.Campo]; ok {
			return t, campoErr.Campo
		}
		return msgCampoRemotoSbagliato, campoErr.Campo
	case errors.Is(err, backup.ErrImprontaMalScritta):
		return "L'impronta va scritta come la pubblica il fornitore: «SHA256:» seguito da 43 caratteri, senza spazi. " +
			"Rifai il primo passo per rivedere l'impronta del server.", backup.CampoImpronta
	case errors.As(err, &diversa):
		return "IMPRONTA DIVERSA: il server si presenta con l'impronta " + diversa.Vista + ", non con quella che hai scritto. " +
			"Niente è stato salvato. Se l'impronta pubblicata dal fornitore è quella che hai scritto, non andare avanti: " +
			"potrebbe non essere il server giusto. Chiedi assistenza.", backup.CampoImpronta
	case errors.Is(err, backup.ErrChiaveSFTPAssente):
		return "Manca la chiave dell'apparecchio: creala con il pulsante «Crea la chiave dell'apparecchio» e copia la " +
			"chiave pubblica nel pannello della Storage Box.", ""
	case errors.Is(err, context.Canceled):
		return "La prova si è interrotta prima di finire: riprova.", ""
	}
	switch backup.CodiceRemoto(err) {
	case backup.CodiceRemotoImpronta:
		return "Il server si è presentato con un'impronta diversa da quella salvata: l'apparecchio non gli ha mandato " +
			"niente. Potrebbe non essere il server giusto: chiedi assistenza prima di cambiare la configurazione.", ""
	case backup.CodiceRemotoAccesso:
		return "Il server ha rifiutato la chiave dell'apparecchio: controlla di aver copiato la chiave pubblica qui " +
			"sopra nel pannello della Storage Box, nel sottoaccount giusto, e che l'utente sia quello del sottoaccount.", backup.CampoUtente
	case backup.CodiceRemotoCartella:
		return "La cartella non c'è sul server (o non è una cartella): creala nel sottoaccount dal pannello, oppure " +
			"correggi il nome.", backup.CampoCartella
	case backup.CodiceRemotoChiave:
		return "La chiave dell'apparecchio non si legge: chiedi assistenza.", ""
	case backup.CodiceRemotoConfigurazione:
		return "La configurazione salvata non si legge: rifalla qui sotto.", ""
	case backup.CodiceRemotoRete:
		switch {
		case errors.Is(err, backup.ErrTempoRemoto):
			return "Il server non ha risposto entro il tempo massimo: controlla il nome del server, la porta e la " +
				"connessione a internet del negozio, poi riprova.", ""
		case errors.Is(err, syscall.ECONNREFUSED):
			return "Il server ha rifiutato la connessione su quella porta (porta chiusa): per la Storage Box la porta " +
				"è 22 o 23.", backup.CampoPorta
		case errors.Is(err, backup.ErrNomeServer):
			return "Il nome del server non si trova: controlla di averlo scritto bene.", backup.CampoHost
		}
		return "Il collegamento con il server non è riuscito: controlla il nome del server, la porta e la connessione " +
			"a internet del negozio, poi riprova.", ""
	}
	return "La prova non è riuscita per un motivo imprevisto. Riprova; se si ripete, chiedi assistenza.", ""
}

// msgCampoRemotoSbagliato è il testo di ripiego di un campo senza testo suo: mai un messaggio muto.
const msgCampoRemotoSbagliato = "Un dato della configurazione non va: controlla server, porta, utente e cartella e rifai il primo passo."

var testoCampoRemoto = map[string]string{
	backup.CampoTipo:     "Il server si presenta con un tipo di chiave che il programma non accetta: chiedi assistenza.",
	backup.CampoImpronta: "L'impronta va scritta come la pubblica il fornitore: «SHA256:» seguito da 43 caratteri, senza spazi.",
	backup.CampoHost:     "Scrivi il nome del server (per la Storage Box: uXXXXXX.your-storagebox.de), senza spazi, «/» né «@».",
	backup.CampoPorta:    "La porta è un numero da 1 a 65535 (per la Storage Box: 22 o 23).",
	backup.CampoUtente:   "Scrivi l'utente del sottoaccount (per la Storage Box: uXXXXXX-subN), senza spazi né «@», «:», «/».",
	backup.CampoCartella: "Scrivi la cartella dei backup sul server (per esempio «backup»): non la radice «/».",
}

// moduloRemoto sono i campi scritti dal titolare e, nel secondo passo, tipo e impronta letti dal server in questa
// stessa richiesta (mai dal modulo).
type moduloRemoto struct {
	Host, Porta, Utente, Cartella string
	TipoVisto, ImprontaVista      string
}

func moduloRemotoDi(r *http.Request) moduloRemoto {
	return moduloRemoto{Host: r.PostForm.Get("remoto[host]"), Porta: r.PostForm.Get("remoto[porta]"),
		Utente: r.PostForm.Get("remoto[utente]"), Cartella: r.PostForm.Get("remoto[cartella]")}
}

// config è la configurazione nuova del modulo (campi controllati prima di collegarsi).
func (m moduloRemoto) config() (backup.ConfigRemota, error) {
	porta, err := strconv.Atoi(strings.TrimSpace(m.Porta))
	if err != nil {
		return backup.ConfigRemota{}, &backup.ErrCampoRemoto{Campo: backup.CampoPorta}
	}
	return backup.NuovaConfigRemota(m.Host, porta, m.Utente, m.Cartella)
}

// datiRemoto è la sezione «Copia remota» della pagina Backup.
type datiRemoto struct {
	Configurata       bool
	ConfigIlleggibile bool
	Salvata           backup.ConfigRemota
	Pubblica          string // la riga pubblica della chiave dell'apparecchio ("" se non c'è)
	ChiaveIlleggibile bool
	Passo2            bool // il secondo passo: l'impronta vista e la conferma
	Modulo            moduloRemoto
	Campi             []campoRemoto
	Impronta          campoRemoto // il campo dell'impronta del secondo passo
}

// campoRemoto è un campo del modulo, con il suo errore (aria-invalid e aria-describedby).
type campoRemoto struct {
	ID, Nome, Etichetta, Valore, Tipo, Aiuto, Errore, Campo string
}

// campiRemoto sono i campi del primo passo con i valori di m e l'errore sul campo sbagliato.
func campiRemoto(m moduloRemoto, sbagliato, errore string) []campoRemoto {
	cc := []campoRemoto{
		{ID: "backup-remote-host", Nome: "remoto[host]", Etichetta: "Server (host)", Valore: m.Host, Tipo: "text",
			Aiuto: "Per la Storage Box: uXXXXXX.your-storagebox.de", Campo: backup.CampoHost},
		{ID: "backup-remote-port", Nome: "remoto[porta]", Etichetta: "Porta", Valore: m.Porta, Tipo: "number",
			Aiuto: "Per la Storage Box: 22 o 23", Campo: backup.CampoPorta},
		{ID: "backup-remote-user", Nome: "remoto[utente]", Etichetta: "Utente", Valore: m.Utente, Tipo: "text",
			Aiuto: "Il sottoaccount dei backup, per esempio uXXXXXX-sub1", Campo: backup.CampoUtente},
		{ID: "backup-remote-dir", Nome: "remoto[cartella]", Etichetta: "Cartella sul server", Valore: m.Cartella, Tipo: "text",
			Aiuto: "La cartella dei backup nel sottoaccount, per esempio «backup»", Campo: backup.CampoCartella},
	}
	for i := range cc {
		if cc[i].Campo == sbagliato {
			cc[i].Errore = errore
		}
	}
	return cc
}

// datiCopiaRemota è la sezione della copia remota con il modulo m (nil: quello della configurazione salvata),
// nel secondo passo se passo2, con l'errore sul campo sbagliato.
func (f *facciaWeb) datiCopiaRemota(ctx context.Context, m *moduloRemoto, passo2 bool, sbagliato, errore string) *datiRemoto {
	if f.remoto == nil {
		return nil
	}
	d := &datiRemoto{Passo2: passo2}
	c, ok, err := f.remoto.Leggi(ctx)
	switch {
	case err != nil && ok:
		d.ConfigIlleggibile = true
	case err != nil:
		f.registro.Error("configurazione della copia remota non letta", "tipo", fmt.Sprintf("%T", err))
		d.ConfigIlleggibile = true
	case ok:
		d.Configurata, d.Salvata = true, c
	}
	pub, err := f.remoto.ChiavePubblica()
	switch {
	case err == nil:
		d.Pubblica = pub
	case !errors.Is(err, backup.ErrChiaveSFTPAssente):
		f.registro.Error("chiave della copia remota non letta", "tipo", fmt.Sprintf("%T", err))
		d.ChiaveIlleggibile = true
	}
	switch {
	case m != nil:
		d.Modulo = *m
	case d.Configurata:
		d.Modulo = moduloRemoto{Host: c.Host, Porta: strconv.Itoa(c.Porta), Utente: c.Utente, Cartella: c.Cartella}
	default:
		d.Modulo = moduloRemoto{Porta: strconv.Itoa(backup.PortaRemotaPredefinita)}
	}
	d.Campi = campiRemoto(d.Modulo, sbagliato, errore)
	d.Impronta = campoRemoto{ID: "backup-remote-fingerprint", Nome: "remoto[impronta]",
		Etichetta: "Impronta pubblicata dal fornitore", Tipo: "text", Aiuto: "SHA256: e 43 caratteri"}
	if sbagliato == backup.CampoImpronta {
		d.Impronta.Errore = errore
	}
	return d
}

// mostraRemoto ridisegna la pagina Backup con il modulo della copia remota m (primo o secondo passo) e l'esito.
func (f *facciaWeb) mostraRemoto(w http.ResponseWriter, r *http.Request, m moduloRemoto, passo2 bool, errore, info, sbagliato string) {
	campoErr := ""
	if sbagliato != "" {
		campoErr = errore
	}
	f.mostraBackup(w, r, errore, info, f.datiCopiaRemota(r.Context(), &m, passo2, sbagliato, campoErr))
}

// creaChiaveSFTP crea la chiave dell'apparecchio (o lascia quella che c'è) e torna alla pagina.
func (f *facciaWeb) creaChiaveSFTP(w http.ResponseWriter, r *http.Request) {
	if f.remoto == nil {
		Errore(w, r)
		return
	}
	st := statoDi(r)
	switch _, creata, err := f.remoto.CreaChiave(); {
	case err == nil && creata:
		st.dati.Info = msgChiaveCreata
	case err == nil:
		st.dati.Info = msgChiaveGiaPresente
	case errors.Is(err, backup.ErrBackupInCorso):
		st.dati.Errore = msgChiaveInBackup
	default:
		f.registro.Error("chiave della copia remota non creata", "tipo", fmt.Sprintf("%T", err))
		st.dati.Errore = msgChiaveNonCreata
	}
	st.cambiata = true
	f.vaiA(w, r, rottaPaginaBackup)
}

// improntaRemota è il primo passo: mostra la chiave del server della configurazione scritta, senza scrivere niente.
func (f *facciaWeb) improntaRemota(w http.ResponseWriter, r *http.Request) {
	if f.remoto == nil {
		Errore(w, r)
		return
	}
	m := moduloRemotoDi(r)
	c, err := m.config()
	if err == nil {
		m.TipoVisto, m.ImprontaVista, err = f.remoto.ChiaveDelServer(r.Context(), c)
	}
	if err != nil {
		f.registraErroreRemoto("impronta del server non vista", err)
		testo, campo := testoErroreRemoto(err)
		m.TipoVisto, m.ImprontaVista = "", ""
		f.mostraRemoto(w, r, m, false, testo, "", campo)
		return
	}
	f.mostraRemoto(w, r, m, true, "", msgVediImpronta, "")
}

// fissaRemoto è il secondo passo: la conferma con l'impronta riscritta dal titolare; scrive solo se la prova riesce.
func (f *facciaWeb) fissaRemoto(w http.ResponseWriter, r *http.Request) {
	if f.remoto == nil {
		Errore(w, r)
		return
	}
	m := moduloRemotoDi(r)
	c, err := m.config()
	if err == nil {
		_, err = f.remoto.Fissa(r.Context(), c, r.PostForm.Get("remoto[impronta]"))
	}
	if err != nil {
		f.registraErroreRemoto("copia remota non configurata", err)
		testo, campo := testoErroreRemoto(err)
		// Il secondo passo resta solo con l'impronta appena riletta dal server (impronta diversa); per ogni altro
		// errore si torna al primo, con i campi scritti.
		var diversa *backup.ErrImprontaDiversa
		passo2 := errors.As(err, &diversa)
		if passo2 {
			m.TipoVisto, m.ImprontaVista = diversa.Tipo, diversa.Vista
		}
		f.mostraRemoto(w, r, m, passo2, testo, "", campo)
		return
	}
	st := statoDi(r)
	st.dati.Info, st.cambiata = msgRemotoSalvato, true
	f.vaiA(w, r, rottaPaginaBackup)
}

// provaRemota prova la configurazione salvata e torna alla pagina con l'esito.
func (f *facciaWeb) provaRemota(w http.ResponseWriter, r *http.Request) {
	if f.remoto == nil {
		Errore(w, r)
		return
	}
	st := statoDi(r)
	if _, err := f.remoto.Prova(r.Context()); err != nil {
		f.registraErroreRemoto("prova della copia remota non riuscita", err)
		st.dati.Errore, _ = testoErroreRemoto(err)
	} else {
		st.dati.Info = msgProvaRemotaOK
	}
	st.cambiata = true
	f.vaiA(w, r, rottaPaginaBackup)
}

// togliRemoto spegne la copia remota, solo con la spunta di conferma.
func (f *facciaWeb) togliRemoto(w http.ResponseWriter, r *http.Request) {
	if f.remoto == nil {
		Errore(w, r)
		return
	}
	st := statoDi(r)
	switch {
	case r.PostForm.Get("remoto[conferma]") != "1":
		st.dati.Errore = msgSpegniConferma
	default:
		switch err := f.remoto.Togli(r.Context()); {
		case errors.Is(err, backup.ErrProvaInCorso):
			st.dati.Errore, _ = testoErroreRemoto(err)
		case err != nil:
			f.registro.Error("copia remota non spenta", "tipo", fmt.Sprintf("%T", err))
			st.dati.Errore = msgRemotoNonSpento
		default:
			st.dati.Info = msgRemotoSpento
		}
	}
	st.cambiata = true
	f.vaiA(w, r, rottaPaginaBackup)
}

// registraErroreRemoto scrive nel registro del server il codice e il tipo dell'errore, mai il testo (indirizzi).
func (f *facciaWeb) registraErroreRemoto(cosa string, err error) {
	f.registro.Info(cosa, "codice", backup.CodiceRemoto(err), "tipo", fmt.Sprintf("%T", errors.Unwrap(err)))
}

// registraCopiaRemota registra le azioni della copia remota (solo il titolare): i due passi ridisegnano la pagina,
// le altre tornano alla pagina con un messaggio.
func (f *facciaWeb) registraCopiaRemota(mux *http.ServeMux) {
	f.rotta(mux, "POST "+rottaChiaveSFTP, soloTitolare, f.creaChiaveSFTP)
	f.rottaConGuscio(mux, "POST "+rottaImprontaRemoto, soloTitolare, f.improntaRemota)
	f.rottaConGuscio(mux, "POST "+rottaRemoto, soloTitolare, f.fissaRemoto)
	f.rotta(mux, "POST "+rottaProvaRemoto, soloTitolare, f.provaRemota)
	f.rotta(mux, "POST "+rottaTogliRemoto, soloTitolare, f.togliRemoto)
}
