package web

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/imaging"
)

// Le foto sulla scheda della busta (slice 038), come la sezione #repair-photos di RepairLive, il
// componente PhotoViewer e gli eventi delle foto dello standalone a go-baseline, senza LiveView:
//   - la galleria (RIP-S-28): «Foto (N)», una cella per foto nell'ordine di caricamento con la
//     miniatura (size=thumb); il visualizzatore è fatto di ancore e :target, quindi funziona senza
//     JavaScript, e chiede la foto intera solo quando un riquadro si apre (loading="lazy" e
//     display: none); app.js aggiunge scorrimento, frecce ed Esc (photo_viewer.js);
//   - POST /repairs/{id}/foto (RIP-S-29) è «Fotocamera» e «Galleria»: un modulo multipart vero, con
//     il token in testa al corpo (csrf.ModuloConFile). Con JavaScript il modulo parte da solo appena
//     si sceglie una foto (auto_upload), dopo il controllo del preflight fatto nel browser; senza,
//     con «Carica». Le regole di allow_upload/3 e handle_photo_progress/3 con LiveView 1.2.11
//     (put_entries/2, generate_preflight_response/4):
//       - camera e gallery sono due upload, ognuno con al più 2 voci per volta: le prime 2 parti con
//         un file di un campo sono le sue voci, le altre si leggono (per la misura e il formato) ma
//         non si scrivono, e il riquadro dice «Massimo 2 foto per volta.»;
//       - ogni voce, anche oltre la seconda, si misura (oltre 25 MiB: «Foto oltre il limite di 25
//         MiB.») e poi si guarda il formato (estensione .jpg .jpeg .png .webp o tipo dichiarato
//         image/jpeg, image/png, image/webp: altrimenti «Formato non supportato.»); una voce
//         rifiutata fra le prime 2 occupa il suo posto (put_invalid_entry/3);
//       - il riquadro #repair-photo-upload-errors ha gli errori nell'ordine di repair_live.ex: prima
//         «Massimo 2 foto per volta.» di camera e di gallery, poi gli errori delle voci di camera e
//         di gallery; con JavaScript gli errori del preflight fatto nel browser arrivano come campi
//         «anteprima» in testa al modulo e vengono prima;
//       - la sessione si ricontrolla quando le foto sono arrivate (get_session_user) e di nuovo
//         nella transazione dopo la normalizzazione (buste.AllegaFoto); ogni voce si allega da sola,
//         come consume_uploaded_entry, nel processo figlio (imaging.Lavoratore); il flash ha «Foto
//         caricata.» se almeno una è salvata, e l'ultimo errore del dominio («Foto rifiutata o non
//         salvata.», o buste.MsgFotoNonElaborata se il figlio non ce l'ha fatta adesso, D-48), come
//         put_flash per ogni voce;
//       - il corpo si legge fino in fondo e si decide leggendo: il Content-Length dichiarato non
//         conta. Oltre limiteCaricamento la lettura si ferma: se c'era già una violazione (una voce
//         oltre 25 MiB o di troppo) la risposta è il redirect con i messaggi (la voce interrotta è
//         «Foto oltre il limite di 25 MiB.»), altrimenti il corpo è abusivo e la risposta è il 413;
//       - ogni pezzo di 64 000 byte deve arrivare entro tempoPezzo (chunk_timeout di LiveView,
//         10 s): il ReadTimeout del server vale per la testa, non per le foto;
//     I file in arrivo stanno in una cartella privata della richiesta, a 0600, con un nome che non
//     viene dal browser, e si tolgono sempre;
//   - POST /repairs/{id}/foto/{foto}/visibilita (RIP-S-31) è un pulsante «Cliente», «Orefice» o
//     «Artigiano» (audience e visible, i valori di phx-value): «Visibilità non aggiornata.» per ogni
//     rifiuto;
//   - POST /repairs/{id}/foto/{foto}/elimina (RIP-S-32) è «Elimina foto»: la conferma «Eliminare la
//     foto?» la chiede app.js (data-confirm, come gli altri pulsanti della scheda); toglie la riga,
//     mai il file; «Foto eliminata.» o «Foto non disponibile.».
//
// Come le altre azioni della scheda, un id di busta malformato, una busta che non c'è o nel cestino
// portano alla home con «Riparazione non trovata.»; ogni altro ritorno segue D-44 (segni.go). Nel
// registro mai nomi di file, percorsi, chiavi o byte delle foto.

const (
	// limiteCaricamento è il corpo più grande di un caricamento: due foto da 25 MiB, con un MiB per
	// le intestazioni delle parti e i campi.
	limiteCaricamento = 2*imaging.MaxByteIngresso + 1<<20
	// maxParti sono le parti al più di un caricamento dopo i campi di testa.
	maxParti = 16
	// tempoPezzo è @default_chunk_timeout di LiveView: il tempo per ogni pezzo di 64 000 byte.
	tempoPezzo = 10 * time.Second

	msgFotoCaricata        = "Foto caricata."
	msgFotoOltreLimite     = "Foto oltre il limite di 25 MiB."
	msgTroppeFoto          = "Massimo 2 foto per volta."
	msgFormatoFoto         = "Formato non supportato."
	msgFotoRifiutata       = "Foto rifiutata o non salvata."
	msgVisibilitaNonAgg    = "Visibilità non aggiornata."
	msgFotoEliminata       = "Foto eliminata."
	msgFotoNonDisponibileS = "Foto non disponibile."
	// prefissoRichiesta è il prefisso delle cartelle di una richiesta nei caricamenti.
	prefissoRichiesta = "richiesta-"
)

// campiFoto sono i due campi file della scheda, nell'ordine dei riquadri di repair_live.ex
// (allow_upload :camera e :gallery).
var campiFoto = []string{"camera", "gallery"}

// tipiFoto sono i tipi che accept: ~w(.jpg .jpeg .png .webp) ammette in LiveView (acceptable_types).
var tipiFoto = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// codiciAnteprima sono gli errori del preflight fatto nel browser (app.js), campo «anteprima».
var codiciAnteprima = map[string]string{"troppe": msgTroppeFoto, "misura": msgFotoOltreLimite, "formato": msgFormatoFoto}

// fotoDellaScheda è una foto come la disegna la galleria.
type fotoDellaScheda struct {
	ID                             int64
	Posto                          int   // da 1, nell'ordine della galleria
	Prec, Succ                     int64 // le foto vicine nel visualizzatore, 0 se non c'è
	Cliente, Artigiano, Oreficeria bool
}

// fotoPerLaScheda mette le foto nell'ordine della galleria con le vicine di ognuna.
func fotoPerLaScheda(ff []buste.Foto) []fotoDellaScheda {
	v := make([]fotoDellaScheda, len(ff))
	for i, f := range ff {
		v[i] = fotoDellaScheda{ID: f.ID, Posto: i + 1, Cliente: f.Cliente, Artigiano: f.Artigiano, Oreficeria: f.Oreficeria}
		if i > 0 {
			v[i].Prec = ff[i-1].ID
		}
		if i < len(ff)-1 {
			v[i].Succ = ff[i+1].ID
		}
	}
	return v
}

// voceFoto è una voce di un upload: il campo, il nome ripulito, il file della richiesta se è una
// delle prime 2 e passa misura e formato, o il rifiuto di LiveView (misura o formato).
type voceFoto struct {
	campo, percorso, nome, rifiuto string
}

// arrivo è quello che il corpo ha portato: le voci in ordine e quante voci ha ogni campo.
type arrivo struct {
	voci  []voceFoto
	conti map[string]int
}

// violazione dice se c'è già un errore che il browser con LiveView non avrebbe mandato: una voce
// oltre 25 MiB o di troppo.
func (a *arrivo) violazione() bool {
	for _, n := range a.conti {
		if n > buste.MaxFotoPerVolta {
			return true
		}
	}
	for _, v := range a.voci {
		if v.rifiuto == msgFotoOltreLimite {
			return true
		}
	}
	return false
}

// errori sono i messaggi del riquadro, nell'ordine di repair_live.ex.
func (a *arrivo) errori() []string {
	var v []string
	for _, c := range campiFoto {
		if a.conti[c] > buste.MaxFotoPerVolta {
			v = append(v, msgTroppeFoto)
		}
	}
	for _, c := range campiFoto {
		for _, x := range a.voci {
			if x.campo == c && x.rifiuto != "" {
				v = append(v, x.rifiuto)
			}
		}
	}
	return v
}

// erroriAnteprima sono gli errori del preflight del browser (campi «anteprima» della testa), solo
// i codici noti, al più maxParti.
func erroriAnteprima(r *http.Request) []string {
	var v []string
	for _, c := range r.PostForm["anteprima"] {
		if m, ok := codiciAnteprima[c]; ok && len(v) < maxParti {
			v = append(v, m)
		}
	}
	return v
}

// caricaFoto è POST /repairs/{id}/foto (handle_photo_progress/3 per ogni voce).
func (f *facciaWeb) caricaFoto(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	// Token e sessione sono a posto: da qui il tempo è per pezzo, non per tutto il corpo.
	corpo := fileDelModulo(r)
	if corpo != nil {
		corpo.lettore.alPezzo = allungaLettura(w, f.tempoPezzo)
	}
	// La cartella della richiesta si crea solo alla prima foto da scrivere, e si toglie sempre.
	cartella := &cartellaPigra{crea: f.cartellaRichiesta}
	defer func() {
		if err := cartella.togli(); err != nil {
			f.registro.Error("file dei caricamenti non tolti", "errore", erroreSenzaPercorso(err))
		}
	}()
	a, err := riceviFoto(corpo, cartella)
	if err != nil {
		if errors.Is(err, errDisco) {
			f.registro.Error("foto in arrivo non scritta", "errore", erroreSenzaPercorso(err))
			Errore(w, r)
			return
		}
		erroreDelCorpo(w, r, err)
		return
	}
	// Le foto sono arrivate: la sessione si ricontrolla prima di salvare (get_session_user).
	st := statoDi(r)
	u, ok, err := f.utenti.DiSessione(r.Context(), st.dati.Utente, st.dati.Versione)
	if err != nil {
		f.registro.Error("sessione non riletta")
		Errore(w, r)
		return
	}
	if !ok {
		f.fuoriPerLeFoto(w, r)
		return
	}
	anteprima := erroriAnteprima(r)
	riquadro := anteprima
	for _, m := range a.errori() {
		// «Massimo 2 foto per volta.» già detto dal browser non si ripete.
		if m != msgTroppeFoto || !slices.Contains(anteprima, msgTroppeFoto) {
			riquadro = append(riquadro, m)
		}
	}
	sessione := &buste.Sessione{ID: u.ID, Versione: st.dati.Versione}
	var caricate int
	var errore string
	for _, v := range a.voci {
		if v.percorso == "" {
			continue
		}
		_, err := f.buste.AllegaFoto(r.Context(), id, []buste.Caricamento{{Percorso: v.percorso, NomeOriginale: v.nome}}, sessione, contestoDi(r))
		switch {
		case err == nil:
			caricate++
		case errors.Is(err, buste.ErrSessioneRevocata):
			f.fuoriPerLeFoto(w, r)
			return
		case errors.Is(err, buste.ErrBustaNonTrovata):
			f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
			return
		case errors.Is(err, imaging.ErrRisorse):
			f.registro.Error("foto non elaborata adesso", "causa", string(imaging.CausaDi(err)))
			errore = buste.MsgFotoNonElaborata
		default:
			if !errors.Is(err, imaging.ErrImmagine) && !errors.Is(err, buste.ErrCaricamento) {
				f.registro.Error("foto non salvata")
			}
			errore = msgFotoRifiutata
		}
	}
	st.dati.Errore, st.cambiata = errore, true
	st.dati.ErroriFoto, st.dati.ErroriFotoBusta = riquadro, id
	if len(riquadro) == 0 {
		st.dati.ErroriFotoBusta = 0
	}
	if caricate > 0 {
		st.dati.Info = msgFotoCaricata
		f.vaiA(w, r, f.allaScheda(r, id, ""))
		return
	}
	f.vaiA(w, r, f.dopoRifiuto(r, id, conSegno))
}

// fuoriPerLeFoto è redirect(socket, to: ~p"/login") di handle_photo_progress/3: la sessione non
// vale più (account disattivato o sessioni chiuse) e si torna all'accesso, senza messaggio.
func (f *facciaWeb) fuoriPerLeFoto(w http.ResponseWriter, r *http.Request) {
	st := statoDi(r)
	st.utente = nil
	st.dati.Utente, st.dati.Versione, st.dati.Accesso, st.cambiata = 0, 0, 0, true
	f.vaiA(w, r, "/login")
}

// cartellaRichiesta crea la cartella privata (0700) dei file di questa richiesta dentro i
// caricamenti, che devono essere una cartella 0700 e non un link.
func (f *facciaWeb) cartellaRichiesta() (string, error) {
	if f.caricamenti == "" {
		return "", errors.New("cartella dei caricamenti non configurata")
	}
	if err := os.Mkdir(f.caricamenti, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	fi, err := os.Lstat(f.caricamenti)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		return "", errors.New("cartella dei caricamenti non privata")
	}
	return os.MkdirTemp(f.caricamenti, prefissoRichiesta)
}

// PulisciCaricamenti prepara all'avvio di serve, prima di ascoltare, la cartella dei caricamenti:
// se manca la crea; deve essere una cartella vera (non un link né un file) a 0700, e se ha altri
// permessi li corregge (riesce solo a chi la possiede: altrimenti è un errore); toglie le cartelle
// di richieste rimaste da un arresto a metà.
func PulisciCaricamenti(cartella string) error {
	fi, err := os.Lstat(cartella)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(cartella, 0o700); err != nil {
			return fmt.Errorf("caricamenti: cartella non creata: %s", erroreSenzaPercorso(err))
		}
		fi, err = os.Lstat(cartella)
	}
	if err != nil {
		return fmt.Errorf("caricamenti: %s", erroreSenzaPercorso(err))
	}
	if !fi.IsDir() {
		return errors.New("caricamenti: al posto della cartella c'è un link o un file")
	}
	if fi.Mode().Perm() != 0o700 {
		if err := os.Chmod(cartella, 0o700); err != nil {
			return fmt.Errorf("caricamenti: la cartella ha i permessi %04o e non si portano a 0700: %s", fi.Mode().Perm(), erroreSenzaPercorso(err))
		}
	}
	voci, err := os.ReadDir(cartella)
	if err != nil {
		return fmt.Errorf("caricamenti: %s", erroreSenzaPercorso(err))
	}
	for _, v := range voci {
		if strings.HasPrefix(v.Name(), prefissoRichiesta) {
			if err := os.RemoveAll(filepath.Join(cartella, v.Name())); err != nil {
				return fmt.Errorf("caricamenti: %s", erroreSenzaPercorso(err))
			}
		}
	}
	return nil
}

// errDisco segna un errore della cartella o di un file dei caricamenti (non del corpo).
var errDisco = errors.New("caricamenti: errore del disco")

// cartellaPigra è la cartella di una richiesta, creata alla prima foto da scrivere.
type cartellaPigra struct {
	crea     func() (string, error)
	percorso string
}

func (c *cartellaPigra) prendi() (string, error) {
	if c.percorso == "" {
		p, err := c.crea()
		if err != nil {
			return "", errors.Join(errDisco, err)
		}
		c.percorso = p
	}
	return c.percorso, nil
}

func (c *cartellaPigra) togli() error {
	if c.percorso == "" {
		return nil
	}
	return os.RemoveAll(c.percorso)
}

// erroreSenzaPercorso è il testo di un errore del file system senza il percorso (che ha il nome
// della cartella dei dati): solo l'operazione e la causa.
func erroreSenzaPercorso(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Op + ": " + le.Err.Error()
	}
	return err.Error()
}

// errCorpoMalformato è un caricamento che un browser non manda: troppe parti, un file in un campo
// che non è un upload della scheda.
var errCorpoMalformato = errors.New("caricamento: corpo malformato")

// riceviFoto legge le parti dopo i campi di testa: le voci dei campi camera e gallery con un nome; un
// campo file senza scelta e il testo dopo i file si scartano; un file in un altro campo, o più di
// maxParti parti, è un corpo malformato. Le prime 2 voci di un campo che passano misura e formato si
// scrivono; le altre si leggono senza tenerle. Oltre il limite del corpo la lettura si ferma: con
// una violazione già vista si decide con quello che c'è, altrimenti è l'errore del limite (413).
func riceviFoto(c *corpoConFile, cartella *cartellaPigra) (*arrivo, error) {
	a := &arrivo{conti: map[string]int{}}
	if c == nil {
		return a, nil
	}
	p := c.primo
	for n := 0; p != nil; n++ {
		if n >= maxParti {
			return nil, errCorpoMalformato
		}
		nome, conFile := nomeDelFile(p)
		var err error
		switch {
		case !conFile || nome == "":
			_, err = io.Copy(io.Discard, p)
		case !slices.Contains(campiFoto, p.FormName()):
			return nil, errCorpoMalformato
		default:
			campo := p.FormName()
			a.conti[campo]++
			var v voceFoto
			v, err = riceviUna(p, campo, buste.PulisciNomeFile(nome), cartella, a.conti[campo] <= buste.MaxFotoPerVolta, n)
			if _, oltre := errors.AsType[*http.MaxBytesError](err); oltre && a.violazione() {
				// La voce interrotta dal limite: oltre 25 MiB o no, non è arrivata intera.
				v.rifiuto, v.percorso = msgFotoOltreLimite, ""
			}
			a.voci = append(a.voci, v)
		}
		if err != nil {
			if _, oltre := errors.AsType[*http.MaxBytesError](err); oltre && a.violazione() {
				return a, nil
			}
			return nil, err
		}
		p, err = c.mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if _, oltre := errors.AsType[*http.MaxBytesError](err); oltre && a.violazione() {
				return a, nil
			}
			return nil, err
		}
	}
	return a, nil
}

// riceviUna riceve una voce: validate_max_file_size, poi validate_accepted (accepted?/2: il tipo
// dichiarato o l'estensione). Una voce da tenere (scrivi) che passa le due prove finisce nel file n
// della cartella (0600, creato nuovo); le altre si leggono senza tenerle. Con un errore di lettura
// la voce torna con quello che si sa (oltre 25 MiB se lo era già) e senza file.
func riceviUna(p *multipart.Part, campo, nome string, cartella *cartellaPigra, scrivi bool, n int) (voceFoto, error) {
	v := voceFoto{campo: campo, nome: nome}
	tipo, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
	accettata := tipiFoto[tipo] || buste.EstensioneAmmessa(nome)
	var dest io.Writer = io.Discard
	var fh *os.File
	if scrivi && accettata {
		dir, err := cartella.prendi()
		if err != nil {
			return v, err
		}
		v.percorso = filepath.Join(dir, strconv.Itoa(n))
		if fh, err = os.OpenFile(v.percorso, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err != nil {
			return v, errors.Join(errDisco, err)
		}
		dest = scrittoreDisco{fh}
	}
	scritti, err := io.Copy(dest, io.LimitReader(p, imaging.MaxByteIngresso+1))
	if err == nil {
		_, err = io.Copy(io.Discard, p)
	}
	if fh != nil {
		if errc := fh.Close(); err == nil && errc != nil {
			err = errors.Join(errDisco, errc)
		}
	}
	switch {
	case scritti > imaging.MaxByteIngresso:
		v.rifiuto = msgFotoOltreLimite
	case !accettata:
		v.rifiuto = msgFormatoFoto
	}
	if (v.rifiuto != "" || err != nil) && v.percorso != "" {
		if errr := os.Remove(v.percorso); errr != nil && err == nil {
			err = errors.Join(errDisco, errr)
		}
		v.percorso = ""
	}
	return v, err
}

// scrittoreDisco segna gli errori di scrittura del file come errori del disco, per distinguerli da
// quelli di lettura del corpo nella stessa io.Copy.
type scrittoreDisco struct{ f *os.File }

func (s scrittoreDisco) Write(p []byte) (int, error) {
	n, err := s.f.Write(p)
	if err != nil {
		err = errors.Join(errDisco, err)
	}
	return n, err
}

// visibilitaFoto è POST /repairs/{id}/foto/{foto}/visibilita (l'evento toggle-photo-visibility).
func (f *facciaWeb) visibilitaFoto(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	pubblico, _ := ultimo(r, "audience")
	valore, _ := ultimo(r, "visible")
	// parse_boolean/1: solo «true» e «false».
	visibile, okValore := map[string]bool{"true": true, "false": false}[valore]
	foto, okFoto := idPositivo(r.PathValue("foto"))
	if !okValore || !okFoto {
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgVisibilitaNonAgg)
		return
	}
	_, err := f.buste.ImpostaVisibilita(r.Context(), id, foto, pubblico, visibile, contestoDi(r))
	switch {
	case err == nil:
		f.vaiA(w, r, f.allaScheda(r, id, ""))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	default:
		if !errors.Is(err, buste.ErrFotoNonTrovata) && !errors.Is(err, buste.ErrPubblicoNonValido) {
			f.registro.Error("visibilità della foto non cambiata")
		}
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgVisibilitaNonAgg)
	}
}

// eliminaFoto è POST /repairs/{id}/foto/{foto}/elimina (l'evento remove-photo).
func (f *facciaWeb) eliminaFoto(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	foto, ok := idPositivo(r.PathValue("foto"))
	if !ok {
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgFotoNonDisponibileS)
		return
	}
	err := f.buste.RimuoviFoto(r.Context(), id, foto, contestoDi(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgFotoEliminata, true
		f.vaiA(w, r, f.allaScheda(r, id, ""))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	default:
		if !errors.Is(err, buste.ErrFotoNonTrovata) {
			f.registro.Error("foto non tolta")
		}
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgFotoNonDisponibileS)
	}
}
