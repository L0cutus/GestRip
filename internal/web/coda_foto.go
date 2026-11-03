package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/imaging"
)

// La coda delle foto di «Nuova riparazione» (slice 039, RIP-S-35): POST /repairs/{id}/photos, come
// RepairPhotoUploadController.create dello standalone a go-baseline. Le foto scelte nel modulo
// partono dal browser (photo_queue.js, slice 040) dopo che la busta esiste, una per richiesta: il
// corpo è la foto, il tipo è Content-Type, il token CSRF è nell'intestazione x-csrf-token (il corpo
// non è un modulo: csrf.Modulo non lo legge) e il nome è x-file-name, codificato dalla coda con
// encodeURIComponent. Le risposte sono JSON per lo script della coda, che legge solo il codice.
//
// La catena è quella delle rotte del personale (rotta, permesso personale), nell'ordine dello
// standalone: CSRF, sessione, accesso; il corpo non si legge prima. Poi, come il controller:
//   - la busta (Workshop.get_repair/1: id positivo, fuori dal cestino), altrimenti 404 «Busta non
//     trovata.»;
//   - il tipo: il primo Content-Type, prima del «;», senza spazi ai bordi e in minuscolo, deve essere
//     image/jpeg, image/png o image/webp, altrimenti 415 «Formato non supportato.», senza leggere;
//   - la lunghezza dichiarata oltre 25 MiB è 413 «Foto oltre il limite di 25 MiB.», senza leggere;
//   - il corpo si legge in un file della cartella dei caricamenti (cartella della richiesta 0700,
//     file 0600 nuovo, nome non del browser, tolto sempre), contando i byte: oltre 25 MiB 413 anche
//     se la lunghezza mente; un errore di lettura (invio interrotto, pezzo che non arriva in tempo)
//     400 «Invio interrotto.»; un errore del disco 500 «Foto non salvata.» (come random_file/1);
//   - la foto passa da buste.AllegaFoto (il processo figlio, la riga, repair.photo_uploaded): 201
//     {"ok":true,"photo_id":…}; la sessione revocata nella transazione 401 «Accesso scaduto.»; il
//     figlio che non ce la fa adesso 503 con buste.MsgFotoNonElaborata (D-48, domanda 7 della
//     037c); una foto non valida, o la busta cestinata nel frattempo, 422 «Foto rifiutata.»; ogni
//     guasto del server (database, archivio, file del caricamento che non si rilegge, contesto
//     finito) 500 «Foto non salvata.» con la causa nel registro (D-50, D-51). Se il client se ne va mentre la foto si
//     normalizza, il figlio si ferma e niente si salva (lo standalone finisce il salvataggio).
//
// Non è la scheda: nessuna riga repair.viewed, nessun segno D-44, nessun redirect. Nel registro mai
// nomi, percorsi, chiavi o byte.
//
// Solo del Go (slice 044b, D-58): la coda manda con ogni foto il suo identificativo casuale in
// x-upload-id (invioCoda). Un invio riuscito ne lascia la traccia (buste.AllegaFotoConInvio, tabella
// invii_foto); un nuovo invio con lo stesso identificativo per la stessa busta (la pagina cambiata o
// ricaricata a metà invio) passa dalla stessa catena e dagli stessi controlli, si legge come gli
// altri, e risponde 201 con la foto già salvata, senza crearne un'altra né scrivere l'audit; con
// photo_id null se quella foto è stata poi tolta.

const (
	// tempoPezzoCoda è read_timeout di read_body nel controller: il tempo di ogni lettura.
	tempoPezzoCoda = 30 * time.Second
	// pezzoCoda è @chunk del controller (read_length e length di read_body).
	pezzoCoda = 1 << 20

	msgCodaBustaNonTrovata = "Busta non trovata."
	msgCodaInvioInterrotto = "Invio interrotto."
	msgCodaAccessoScaduto  = "Accesso scaduto."
	msgCodaFotoRifiutata   = "Foto rifiutata."
	msgCodaFotoNonSalvata  = "Foto non salvata."
)

// estensioniCoda sono @types del controller: i tipi ammessi e l'estensione che danno a un nome che
// non ne ha una delle foto.
var estensioniCoda = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

// caricaFotoCoda è POST /repairs/{id}/photos (create/2).
func (f *facciaWeb) caricaFotoCoda(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if ok {
		var err error
		if ok, err = f.buste.EsisteBusta(r.Context(), id); err != nil {
			f.registro.Error("busta non letta")
			Errore(w, r)
			return
		}
	}
	if !ok {
		f.rifiutoCoda(w, r, http.StatusNotFound, msgCodaBustaNonTrovata)
		return
	}
	tipo, ok := tipoCoda(r)
	if !ok {
		f.rifiutoCoda(w, r, http.StatusUnsupportedMediaType, msgFormatoFoto)
		return
	}
	if r.ContentLength > imaging.MaxByteIngresso {
		f.rifiutoCoda(w, r, http.StatusRequestEntityTooLarge, msgFotoOltreLimite)
		return
	}
	cartella := &cartellaPigra{crea: f.cartellaRichiesta}
	defer func() {
		if err := cartella.togli(); err != nil {
			f.registro.Error("file dei caricamenti non tolti", "errore", erroreSenzaPercorso(err))
		}
	}()
	percorso, err := f.riceviFotoCoda(w, r, cartella)
	if err != nil {
		_, oltre := errors.AsType[*http.MaxBytesError](err)
		switch {
		case errors.Is(err, errDisco):
			f.registro.Error("foto in arrivo non scritta", "errore", erroreSenzaPercorso(err))
			f.rifiutoCoda(w, r, http.StatusInternalServerError, msgCodaFotoNonSalvata)
		case oltre:
			f.rifiutoCoda(w, r, http.StatusRequestEntityTooLarge, msgFotoOltreLimite)
		default:
			f.rifiutoCoda(w, r, http.StatusBadRequest, msgCodaInvioInterrotto)
		}
		return
	}
	st := statoDi(r)
	sessione := &buste.Sessione{ID: st.utente.ID, Versione: st.dati.Versione}
	foto, gia, err := f.buste.AllegaFotoConInvio(r.Context(), id, buste.Caricamento{Percorso: percorso, NomeOriginale: nomeCoda(r, tipo)},
		f.invioCoda(r), sessione, contestoFoto(r))
	switch {
	case err == nil && (foto.ID > 0 || gia):
		// Un invio ripetuto (stesso identificativo, stessa busta) risponde come il primo; se la foto di
		// allora è stata tolta, 201 con photo_id null: già fatto, e la coda la toglie come salita.
		var photoID *int64
		if foto.ID > 0 {
			photoID = &foto.ID
		}
		corpo, _ := json.Marshal(struct {
			OK      bool   `json:"ok"`
			PhotoID *int64 `json:"photo_id"`
		}{true, photoID}) // una struttura fissa: Marshal non fallisce
		f.jsonCoda(w, r, http.StatusCreated, corpo)
	case errors.Is(err, buste.ErrSessioneRevocata):
		// Revocata fra il controllo della richiesta e la scrittura: come nessuna sessione.
		f.rifiutoCoda(w, r, http.StatusUnauthorized, msgCodaAccessoScaduto)
	case errors.Is(err, imaging.ErrRisorse):
		f.registro.Error("foto non elaborata adesso", "causa", string(imaging.CausaDi(err)))
		f.rifiutoCoda(w, r, http.StatusServiceUnavailable, buste.MsgFotoNonElaborata)
	case fotoNonValida(err):
		f.rifiutoCoda(w, r, http.StatusUnprocessableEntity, msgCodaFotoRifiutata)
	default:
		// Un guasto del server (D-51): database, archivio che non scrive o non configurato, contesto
		// finito (il client se n'è andato). La coda tiene la foto per «Riprova».
		causa := "nessuna foto salvata"
		if err != nil {
			causa = erroreSenzaPercorso(err)
		}
		f.registro.Error("foto non salvata", "causa", causa)
		f.rifiutoCoda(w, r, http.StatusInternalServerError, msgCodaFotoNonSalvata)
	}
}

// fotoNonValida dice se AllegaFoto ha rifiutato la foto stessa (422 «Foto rifiutata.», che la coda
// scarta): byte che non sono un'immagine (imaging.ErrImmagine), un caricamento non valido
// (estensione, vuoto, oltre il limite: buste.ErrCaricamento) o un gruppo di troppe foto (buste.ErrTroppeFoto, che con una foto
// per richiesta non succede); e, come answer(_refused) dello standalone, la busta cestinata fra il
// controllo della porta e la transazione (buste.ErrBustaNonTrovata: il 404 è solo della porta). Il
// file del caricamento che non si rilegge (buste.ErrFileCaricamento) è un guasto, non un rifiuto.
func fotoNonValida(err error) bool {
	return errors.Is(err, imaging.ErrImmagine) || errors.Is(err, buste.ErrCaricamento) || errors.Is(err, buste.ErrTroppeFoto) ||
		errors.Is(err, buste.ErrBustaNonTrovata)
}

// tipoCoda è content_type/1: il primo Content-Type, prima del «;», senza spazi ai bordi, in
// minuscolo (solo le lettere ASCII: un carattere non ASCII non diventa mai una lettera di un tipo
// ammesso), deve essere uno di estensioniCoda.
func tipoCoda(r *http.Request) (string, bool) {
	v := r.Header.Values("Content-Type")
	if len(v) == 0 {
		return "", false
	}
	t, _, _ := strings.Cut(v[0], ";")
	t = strings.Map(func(c rune) rune {
		if 'A' <= c && c <= 'Z' {
			return c + 'a' - 'A'
		}
		return c
	}, strings.TrimFunc(t, unicode.IsSpace))
	_, ok := estensioniCoda[t]
	return t, ok
}

// invioCoda è l'identificativo d'invio della foto (slice 044b, D-58): la prima intestazione
// x-upload-id, se ha la forma di buste.InvioValido (22 caratteri base64url, quella che la coda di
// app.js genera). Altrimenti "": la foto si salva come senza identificativo. Un valore malformato si
// ignora invece di rifiutarlo, come lo standalone ignora ogni intestazione che non conosce: lo manda
// solo un client che non è la coda, e un 400 la coda lo riproverebbe per sempre. Un valore ignorato
// lascia una riga nel registro, senza il valore.
func (f *facciaWeb) invioCoda(r *http.Request) string {
	v := r.Header.Values("X-Upload-Id")
	if len(v) == 0 {
		return ""
	}
	if !buste.InvioValido(v[0]) {
		f.registro.Warn("x-upload-id malformato, ignorato")
		return ""
	}
	return v[0]
}

// nomeCoda è filename/2: x-file-name decodificato (URI.decode/1: solo i %XX, il «+» resta),
// ripulito (buste.PulisciNomeFile), «foto» se vuoto, con l'estensione del tipo se non ne ha una delle
// foto. Una codifica rotta è «foto» con l'estensione del tipo (il rescue di ArgumentError).
func nomeCoda(r *http.Request, tipo string) string {
	est := estensioniCoda[tipo]
	nome := ""
	if v := r.Header.Values("X-File-Name"); len(v) > 0 {
		d, err := url.PathUnescape(v[0])
		if err != nil {
			return "foto" + est
		}
		nome = buste.PulisciNomeFile(d)
	}
	if nome == "" {
		nome = "foto"
	}
	if buste.EstensioneAmmessa(nome) {
		return nome
	}
	return nome + est
}

// riceviFotoCoda è read_photo/2: il corpo nel file della richiesta, al più imaging.MaxByteIngresso
// byte (oltre, *http.MaxBytesError), con la scadenza di lettura spostata a ogni pezzo. Dà il
// percorso del file. Gli errori del file sono errDisco, gli altri vengono dal corpo.
func (f *facciaWeb) riceviFotoCoda(w http.ResponseWriter, r *http.Request, cartella *cartellaPigra) (string, error) {
	dir, err := cartella.prendi()
	if err != nil {
		return "", err
	}
	percorso := filepath.Join(dir, "0")
	fh, err := os.OpenFile(percorso, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", errors.Join(errDisco, err)
	}
	l := &lettoreLimitato{r: r.Body, limite: imaging.MaxByteIngresso, misura: pezzoCoda, alPezzo: allungaLettura(w, f.tempoCoda)}
	_, err = io.Copy(scrittoreDisco{fh}, l)
	if errc := fh.Close(); err == nil && errc != nil {
		err = errors.Join(errDisco, errc)
	}
	return percorso, err
}

// rifiutoCoda è refuse/3: {"error":msg,"ok":false} (le chiavi nell'ordine di Jason per una mappa
// piccola) con il codice.
func (f *facciaWeb) rifiutoCoda(w http.ResponseWriter, r *http.Request, codice int, msg string) {
	corpo, _ := json.Marshal(struct {
		Error string `json:"error"`
		OK    bool   `json:"ok"`
	}{msg, false}) // una struttura fissa: Marshal non fallisce
	f.jsonCoda(w, r, codice, corpo)
}

// jsonCoda è json/2 di Phoenix: application/json; charset=utf-8, il resto delle intestazioni come le
// altre risposte della catena.
func (f *facciaWeb) jsonCoda(w http.ResponseWriter, r *http.Request, codice int, corpo []byte) {
	f.salva(w, statoDi(r))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(codice)
	_, _ = w.Write(corpo) // client andato: niente da fare
}
