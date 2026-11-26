package backup

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// Il giro intero di un backup (slice 124): la sequenza che fino alla 123 stava nel comando (faiBackup in
// cmd/gestrip/backup.go), ora qui perché la usano tutti e tre, il comando gestrip backup, il lavoro notturno
// e «Esegui adesso». Le guardie che cancellano restano dove erano (Ruota, PulisciPending, PulisciResti):
// un ordine sbagliato del chiamante non cancella niente.

// ErrSenzaLucchetto: un giro senza il lucchetto dei backup preso. Non parte.
var ErrSenzaLucchetto = errors.New("backup: il giro vuole il lucchetto dei backup preso")

// Giro è un backup da fare: dove (Chiavetta o Cartella, una sola), con quale database e archivio dei media, e
// a chi dire cosa succede.
type Giro struct {
	// Lucchetto è il lucchetto dei backup, già preso da chi chiama (PrendiLucchetto) e rilasciato da lui.
	Lucchetto *Lucchetto
	DB        *db.DB
	Archivio  *media.Archivio
	// Chiavetta è il punto di montaggio della chiavetta (123): controlli, pulizia dei .pending, registro
	// «chiavetta» e rotazione 7 + 4. Cartella è una cartella qualsiasi (solo il comando, -cartella): niente
	// pulizia né rotazione, registro «cartella».
	Chiavetta, Cartella string
	// Cattura è l'orologio della copia del database (snapshot.json); Registro quello della verifica scritta
	// nel registro e della rotazione. nil: l'orologio del sistema.
	Cattura, Registro clock.Orologio
	// Verifica è la verifica dell'archivio scritto; nil: VerificaArchivio.
	Verifica func(ctx context.Context, dati, file string) (Verificato, error)
	// Dire riceve le righe per chi guarda (il comando le stampa); un suo errore ferma il giro. nil: nessuna.
	Dire func(riga string) error
	// Remoto è la copia remota (128) da fare dopo un backup riuscito sulla chiavetta; nil: nessuna (con -cartella
	// non si fa mai). Avvio è la parola della riga di copie_remote ("" vale a_mano).
	Remoto *Remoto
	Avvio  string
}

// EsitoGiro è quello che il giro ha fatto: l'archivio scritto (vuoto se Cifra non è arrivata in fondo),
// se il backup è riuscito (verificato e registrato con la destinazione giusta: «chiavetta» per la chiavetta)
// e la rotazione. Riuscito con un errore vuol dire che il backup è buono ma qualcosa dopo (la rotazione) non
// è andato.
type EsitoGiro struct {
	Risultato Risultato
	Riuscito  bool
	Rotazione EsitoRotazione
	// Remoto è quello che la copia remota ha fatto (vuoto senza copia remota).
	Remoto EsitoCopiaRemota
}

func (g Giro) dire(formato string, arg ...any) error {
	if g.Dire == nil {
		return nil
	}
	return g.Dire(fmt.Sprintf(formato, arg...))
}

func orologioODi(o clock.Orologio) clock.Orologio {
	if o == nil {
		return clock.Sistema{}
	}
	return o
}

// Esegui fa il giro, nell'ordine: le cartelle temporanee rimaste nella cartella dei dati (PulisciResti) →
// la destinazione con il suo lucchetto (ApriChiavetta o BloccaDestinazione) → sulla chiavetta i .pending
// rimasti → Cifra → la verifica completa (stesso SHA-256 e byte) → la destinazione da registrare
// (ConfermaChiavetta) → la riga nel registro, anche per una verifica fallita → sulla chiavetta la rotazione
// 7 + 4 → sulla chiavetta, se configurata, la copia remota (128). Solo una riga 'ok' con la destinazione giusta
// fa Riuscito; la copia remota non lo cambia (un suo errore è un errore con Riuscito, come la rotazione).
//
// Le righe per chi guarda sono quelle del comando gestrip backup della 123, più quella delle cartelle
// temporanee tolte.
func (g Giro) Esegui(ctx context.Context) (e EsitoGiro, err error) {
	if !g.Lucchetto.Preso() {
		return EsitoGiro{}, ErrSenzaLucchetto
	}
	if (g.Chiavetta == "") == (g.Cartella == "") {
		return EsitoGiro{}, errors.New("backup: il giro vuole una sola destinazione, chiavetta o cartella")
	}
	dati := filepath.Dir(g.DB.Percorso())
	if !g.Lucchetto.diDati(dati) {
		return EsitoGiro{}, fmt.Errorf("%w: il lucchetto è di un'altra cartella dei dati", ErrSenzaLucchetto)
	}
	resti, err := g.Lucchetto.PulisciResti()
	for _, n := range resti {
		if errOut := g.dire("tolta %s, cartella temporanea rimasta da un backup interrotto\n", filepath.Join(dati, n)); errOut != nil {
			return EsitoGiro{}, errors.Join(err, errOut)
		}
	}
	if err != nil {
		return EsitoGiro{}, err
	}
	var dest *Destinazione
	if g.Chiavetta != "" {
		dest, err = ApriChiavetta(g.Chiavetta, dati)
	} else {
		dest, err = BloccaDestinazione(g.Cartella, dati)
	}
	if err != nil {
		return EsitoGiro{}, err
	}
	defer func() { err = errors.Join(err, dest.Chiudi()) }()
	if dest.Chiavetta() {
		tolti, err := dest.PulisciPending()
		for _, n := range tolti {
			if errOut := g.dire("tolto %s, rimasto da un backup interrotto\n", n); errOut != nil {
				return EsitoGiro{}, errors.Join(err, errOut)
			}
		}
		if err != nil {
			return EsitoGiro{}, err
		}
	}
	s := Sorgente{DB: g.DB, Archivio: g.Archivio, Orologio: orologioODi(g.Cattura)}
	r, err := Cifra(ctx, s, dest.Cartella)
	if err != nil {
		return EsitoGiro{}, err
	}
	e.Risultato = r
	if err := g.dire("archivio scritto: %s\nbyte: %d\nsha256: %s\nmedia: %d\n",
		r.File, r.ByteCifrati, r.SHA256, contaMedia(r.Inventario)); err != nil {
		return e, err
	}
	verifica := g.Verifica
	if verifica == nil {
		verifica = VerificaArchivio
	}
	v, err := verifica(ctx, dati, r.File)
	if err == nil && (v.SHA256 != r.SHA256 || v.ByteCifrati != r.ByteCifrati) {
		err = ErrImpronta
	}
	// «chiavetta» solo se la chiavetta ha passato i controlli all'inizio e li ripassa adesso, con l'archivio
	// dentro (dalla 122 una riga 'ok' sulla chiavetta sblocca le cancellazioni dopo un import).
	destinazione, errConferma := dest.DestinazioneDaRegistrare(r.File)
	if err != nil {
		// Anche la verifica fallita va nel registro (slice 122): non sblocca niente, la vede la pagina (126).
		errReg := g.registra(ctx, r, v, err, destinazione)
		errOut := g.dire("BACKUP NON RIUSCITO: la verifica dell'archivio non è passata.\n"+
			"L'archivio resta in %s, ma non conta come backup.\n", r.File)
		return e, errors.Join(fmt.Errorf("backup non riuscito, verifica di %s: %w", r.File, err), errReg, errOut)
	}
	if err := g.dire("verificato: decifrato, %d file con l'impronta giusta, database integro, %d tabelle con i conteggi giusti\n",
		len(v.Inventario), len(v.Conteggi)); err != nil {
		return e, err
	}
	// D-116 (slice 125): i media rovinati non cambiano l'esito, vanno nella riga del registro.
	if len(v.MediaRovinati) > 0 {
		if err := g.dire("ATTENZIONE: %d media con il contenuto diverso dal nome (rovinati sul disco prima del backup): "+
			"il backup vale lo stesso, i media rovinati sono annotati nel registro\n", len(v.MediaRovinati)); err != nil {
			return e, err
		}
	}
	// Solo la riga del registro fa contare il backup (D-11): senza, l'archivio è buono ma non sblocca niente.
	if err := g.registra(ctx, r, v, nil, destinazione); err != nil {
		errOut := g.dire("BACKUP NON RIUSCITO: l'archivio è verificato ma il registro dei backup non si è potuto scrivere.\n"+
			"L'archivio resta in %s, ma non conta come backup.\n", r.File)
		return e, errors.Join(fmt.Errorf("backup non registrato, %s: %w", r.File, err), errOut)
	}
	if errConferma != nil {
		errOut := g.dire("BACKUP NON RIUSCITO sulla chiavetta: la chiavetta è cambiata durante il backup.\n"+
			"L'archivio %s è verificato ma è registrato come cartella: non conta come backup sulla chiavetta.\n", r.File)
		return e, errors.Join(fmt.Errorf("backup non riuscito sulla chiavetta: %w", errConferma), errOut)
	}
	e.Riuscito = true
	if err := g.dire("registrato nel registro dei backup verificati\n"); err != nil {
		return e, err
	}
	if !dest.Chiavetta() {
		return e, g.dire("backup riuscito\n")
	}
	// La rotazione solo adesso: archivio verificato e registrato come chiavetta (D-114).
	rot, errRot := dest.Ruota(ctx, g.DB, r, orologioODi(g.Registro).Adesso())
	e.Rotazione = rot
	for _, n := range rot.Tolti {
		if err := g.dire("tolto %s (rotazione 7 + 4)\n", n); err != nil {
			return e, errors.Join(errRot, err)
		}
	}
	for _, n := range rot.Lasciati {
		if err := g.dire("lasciato %s: non è nel registro o non è un file normale, non lo tocco\n", n); err != nil {
			return e, errors.Join(errRot, err)
		}
	}
	if err := g.dire("archivi sulla chiavetta: %d\nbackup riuscito\n", len(rot.Tenuti)); err != nil {
		return e, errors.Join(errRot, err)
	}
	if errRot != nil {
		errOut := g.dire("ROTAZIONE NON RIUSCITA: il backup è buono, ma gli archivi vecchi non sono stati tolti tutti.\n")
		errRot = errors.Join(fmt.Errorf("rotazione della chiavetta: %w", errRot), errOut)
	}
	// La copia remota (128) dopo la chiavetta, anche con la rotazione della chiavetta non riuscita (il backup è
	// buono): un suo fallimento non tocca il backup sulla chiavetta, che resta riuscito.
	return e, errors.Join(errRot, g.copiaRemota(ctx, dest, r, v, &e))
}

// copiaRemota fa la copia remota (Remoto.Copia) dell'archivio r, verificato (v) e registrato sulla chiavetta, se è
// configurata, dice cosa ha fatto e ne scrive la riga in copie_remote (anche dopo lo stop, come il registro). Uno
// stop a metà non lascia righe (la copia si rifà al backup seguente). Un errore vuol dire «copia remota non
// riuscita»: il backup sulla chiavetta resta riuscito.
func (g Giro) copiaRemota(ctx context.Context, dest *Destinazione, r Risultato, v Verificato, e *EsitoGiro) error {
	if g.Remoto == nil {
		return nil
	}
	o := orologioODi(g.Registro)
	riga := db.CopiaRemota{Avvio: g.Avvio, IniziatoIl: o.Adesso()}
	if riga.Avvio == "" {
		riga.Avvio = db.AvvioProvaAMano
	}
	if err := g.dire("copia remota: caricamento sul server\n"); err != nil {
		return err
	}
	er, err := g.Remoto.Copia(ctx, g.DB, dest, r, v, o)
	e.Remoto = er
	var errOut error
	dire := func(formato string, arg ...any) {
		if errOut == nil {
			errOut = g.dire(formato, arg...)
		}
	}
	for _, n := range er.PendingTolti {
		dire("tolto %s dal server remoto, rimasto da un caricamento interrotto\n", n)
	}
	for _, n := range er.Rotazione.Tolti {
		dire("tolto %s dal server remoto (rotazione 7 + 4)\n", n)
	}
	for _, n := range er.Rotazione.Lasciati {
		dire("lasciato %s sul server remoto: non è nel registro o non è un file normale, non lo tocco\n", n)
	}
	riga.FinitoIl, riga.Archivio = o.Adesso(), er.Archivio
	switch {
	case err == nil:
		riga.Esito = db.EsitoCopiaOK
		dire("copia remota riuscita: %s (archivi sul server: %d)\n", path.Base(er.Archivio), len(er.Rotazione.Tenuti))
	case er.Registrato:
		riga.Esito, riga.Codice, riga.Errore = db.EsitoCopiaAvviso, CodiceRemoto(err), err.Error()
		dire("copia remota riuscita: %s\nROTAZIONE REMOTA NON RIUSCITA: la copia è buona, ma gli archivi vecchi sul server "+
			"non sono stati tolti tutti.\n", path.Base(er.Archivio))
	default:
		riga.Esito, riga.Codice, riga.Errore = db.EsitoCopiaFallita, CodiceRemoto(err), err.Error()
		dire("COPIA REMOTA NON RIUSCITA: il backup sulla chiavetta è buono, la copia fuori sede no.\n")
	}
	if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		// Lo stop: niente riga, come per il backup (si rifà al giro seguente).
		return errors.Join(fmt.Errorf("copia remota: %w", err), errOut)
	}
	ctxScrivi, annulla := staccato(ctx)
	defer annulla()
	_, errRiga := db.RegistraCopiaRemota(ctxScrivi, g.DB, riga)
	if errRiga != nil {
		errRiga = fmt.Errorf("copia remota, riga non scritta: %w", errRiga)
	}
	if err != nil {
		err = fmt.Errorf("copia remota: %w", err)
	}
	return errors.Join(err, errRiga, errOut)
}

// registra scrive nel registro dei backup verificati (db.RegistraVerificaBackup, slice 122) la verifica
// dell'archivio r: riuscita se errVerifica è nil, con impronta, byte e conteggi letti dalla verifica v;
// altrimenti fallita, con il motivo e quello che Cifra ha scritto. L'ora della verifica è adesso, alla fine
// della verifica; il percorso è quello assoluto.
func (g Giro) registra(ctx context.Context, r Risultato, v Verificato, errVerifica error, destinazione string) error {
	file, err := filepath.Abs(r.File)
	if err != nil {
		return fmt.Errorf("registro dei backup: %w", err)
	}
	catturato, err := time.Parse(db.FormatoIstante, r.CatturatoIl)
	if err != nil {
		return fmt.Errorf("registro dei backup: ora di cattura: %w", err)
	}
	riga := db.VerificaBackup{Riuscita: errVerifica == nil, Destinazione: destinazione, Archivio: file,
		SHA256: v.SHA256, Byte: v.ByteCifrati, CatturatoIl: catturato, VerificatoIl: orologioODi(g.Registro).Adesso(),
		Conteggi: v.Conteggi, MediaRovinati: v.MediaRovinati}
	if errVerifica != nil {
		riga.SHA256, riga.Byte, riga.Conteggi, riga.Errore = r.SHA256, r.ByteCifrati, r.Conteggi, errVerifica.Error()
	}
	// Anche con lo stop arrivato intanto (serve che si ferma, Ctrl-C): l'archivio è sulla destinazione e solo una
	// riga del registro lo fa togliere dalla rotazione (123); senza resterebbe per sempre (slice 124). Al più
	// TempoScritturaStorico (revisione della 124).
	ctxScrivi, annulla := staccato(ctx)
	defer annulla()
	return db.RegistraVerificaBackup(ctxScrivi, g.DB, riga)
}

// contaMedia conta le voci media/ di un inventario.
func contaMedia(inv map[string]string) int {
	n := 0
	for k := range inv {
		if len(k) > len("media/") && k[:len("media/")] == "media/" {
			n++
		}
	}
	return n
}
