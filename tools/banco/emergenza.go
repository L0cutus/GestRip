package main

// Il passo del registro d'emergenza (slice 028): dopo la vendita, nello standalone ancora fermo,
// un evento aperto, una riga, il suo storno, la scansione di una busta del legacy import e la
// chiusura dell'evento, con le stesse funzioni pubbliche che chiama la pagina del registro
// (emergency_live.ex a go-baseline: EmergencyRegister.open_event/3, create_entry/5,
// reverse_entry/5, scan/4, close_event/2, contesto d'audit %{actor: email}). Lo standalone
// salva senza zona emergency_entries.inserted_at (insert_entry e insert_reversal),
// emergency_events.closed_at e updated_at (close_event), repairs.retention_started_at e
// updated_at (la scansione): l'import Go li accetta come UTC con l'avviso
// traduzione/istante_senza_zona (D-42, decisione del 31/10/2026), e la catena pretende l'avviso
// su ognuna di queste colonne, per le righe esatte che il passo ha scritto.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// righeRegistro sono le righe che il passo scrive nell'evento che apre: la riga e il suo storno.
const righeRegistro = 2

// emergenzaStandalone avvia l'applicazione come il legacy import (Release.quiet!/0) e fa ciò che
// fa il titolare dalle pagine. La cattura sintetica non ha QR sulle buste e scan/4 trova la busta
// solo dal QR: prima lo si mette dalla scheda della busta (repair_live.ex: Workshop.update_repair/4
// con il contesto della scheda, actor e session_user), poi il registro. La busta è la 2/26 del
// legacy import, «pronta»: la consegna al banco di una busta pronta. La riga è libera (nessuna
// busta), come la terza via della pagina. Tutti i dati sono inventati. Gli id vanno come li manda la
// pagina: interi dall'evento in pagina, testo dai moduli di storno e chiusura. L'esito è una sola
// riga GR_EMERGENZA: con ciò che il passo ha scritto, riletto con le letture dello standalone; un
// errore esce con stato 1.
const emergenzaStandalone = `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{EmergencyRegister, Repo, Workshop}
alias GestripStandalone.Accounts.User
alias GestripStandalone.Workshop.Repair
email = "/run/secrets/admin_email" |> File.read!() |> String.trim_trailing()
audit = %{actor: email}
qr = "SINT-QR-0002"
risultato =
  with %User{} = titolare <- Repo.get_by(User, email: email),
       audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}},
       %Repair{} = busta <- Repo.get_by(Repair, bag_number: "2/26"),
       {:ok, _} <- Workshop.update_repair(busta, %{"qr_code" => qr}, audit_scheda, compro_oro: nil),
       {:ok, evento} <- EmergencyRegister.open_event(%{"reason" => "Registratore sintetico non disponibile"}, audit, notify_owner: false),
       {:ok, riga} <- EmergencyRegister.create_entry(evento.id, %{"description" => "Riparazione sintetica di prova",
         "gross_euros" => "35,50", "vat_rate_percent" => "22", "payment_method" => "contanti",
         "customer_name" => "Cliente Sintetico"}, audit, nil),
       {:ok, storno} <- EmergencyRegister.reverse_entry(evento.id, Integer.to_string(riga.id), "Importo sbagliato, prova sintetica", audit),
       {:ok, %{repair_id: scansionata}} <- EmergencyRegister.scan(evento.id, qr, audit),
       {:ok, chiuso} <- EmergencyRegister.close_event(Integer.to_string(evento.id), audit) do
    dopo = Repo.get!(Repair, scansionata)
    %{esito: "ok", evento: chiuso.id, chiuso: not is_nil(chiuso.closed_at), riga: riga.id, storno_di: storno.reversal_of_id,
      righe: length(EmergencyRegister.entries(evento.id)), busta: dopo.id, stato_busta: dopo.status,
      conservazione: not is_nil(dopo.retention_started_at)}
  else
    errore -> %{esito: "errore", motivo: inspect(errore)}
  end
IO.puts("GR_EMERGENZA:" <> Jason.encode!(risultato))
if risultato.esito != "ok", do: System.halt(1)`

// comandoEmergenza gira come il legacy import e la vendita: stessa immagine, senza rete, stesse
// protezioni e stessi dati dell'istanza, e nient'altro.
func comandoEmergenza(n nomi, img string, env []string) []string {
	a := append(passoSenzaRete(n, passoEmergenza), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", emergenzaStandalone)
}

// esitoEmergenza è la riga GR_EMERGENZA: del passo del registro.
type esitoEmergenza struct {
	Esito         string `json:"esito"`
	Motivo        string `json:"motivo"`
	Evento        int64  `json:"evento"`
	Chiuso        bool   `json:"chiuso"`
	Riga          int64  `json:"riga"`
	StornoDi      int64  `json:"storno_di"`
	Righe         int64  `json:"righe"`
	Busta         int64  `json:"busta"`
	StatoBusta    string `json:"stato_busta"`
	Conservazione bool   `json:"conservazione"`
}

// riassuntoEmergenza è ciò che il passo del registro ha scritto: le righe dell'evento, gli eventi
// chiusi, le buste scansionate. Sono le righe che gli avvisi sugli istanti senza zona devono avere.
type riassuntoEmergenza struct {
	Righe            int64 `json:"righe"`
	EventiChiusi     int64 `json:"eventi_chiusi"`
	BusteScansionate int64 `json:"buste_scansionate"`
}

// scritto dà le righe che il passo ha scritto; un esito non riuscito non ha scritto niente.
func (e esitoEmergenza) scritto() riassuntoEmergenza {
	if e.Esito != "ok" {
		return riassuntoEmergenza{}
	}
	r := riassuntoEmergenza{Righe: e.Righe}
	if e.Chiuso && e.Evento > 0 {
		r.EventiChiusi = 1
	}
	if e.Busta > 0 && e.StatoBusta == "consegnata" && e.Conservazione {
		r.BusteScansionate = 1
	}
	return r
}

// leggiEsitoEmergenza vuole una sola riga GR_EMERGENZA: riuscita, senza campi in più: l'evento
// chiuso, la riga e il suo storno (due righe nell'evento, lo storno di quella riga), la busta
// scansionata consegnata e con la conservazione avviata.
func leggiEsitoEmergenza(out string) (esitoEmergenza, error) {
	righe := righeMarcatore(out, "GR_EMERGENZA:")
	if len(righe) != 1 {
		return esitoEmergenza{}, fmt.Errorf("registro d'emergenza nello standalone: %d righe GR_EMERGENZA, ne serve una", len(righe))
	}
	var e esitoEmergenza
	dec := json.NewDecoder(strings.NewReader(righe[0]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return esitoEmergenza{}, fmt.Errorf("registro d'emergenza nello standalone: riga GR_EMERGENZA non valida: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return esitoEmergenza{}, errors.New("registro d'emergenza nello standalone: dati dopo la riga GR_EMERGENZA")
	}
	switch {
	case e.Esito != "ok":
		return esitoEmergenza{}, fmt.Errorf("registro d'emergenza nello standalone: esito %q: %s", e.Esito, e.Motivo)
	case e.Evento < 1 || !e.Chiuso:
		return esitoEmergenza{}, fmt.Errorf("registro d'emergenza nello standalone: evento %d chiuso %v", e.Evento, e.Chiuso)
	case e.Riga < 1 || e.StornoDi != e.Riga || e.Righe != righeRegistro:
		return esitoEmergenza{}, fmt.Errorf("registro d'emergenza nello standalone: riga %d, storno di %d, %d righe nell'evento (attese %d)",
			e.Riga, e.StornoDi, e.Righe, righeRegistro)
	case e.Busta < 1 || e.StatoBusta != "consegnata" || !e.Conservazione:
		return esitoEmergenza{}, fmt.Errorf("registro d'emergenza nello standalone: busta %d %q, conservazione avviata %v",
			e.Busta, e.StatoBusta, e.Conservazione)
	}
	return e, nil
}

// registroEmergenza fa il passo del registro d'emergenza e ne dà l'esito controllato.
func registroEmergenza(n nomi, img string, env []string) (esitoEmergenza, error) {
	fmt.Println("catena: registro d'emergenza nello standalone (QR della busta, evento, riga, storno, scansione, chiusura)…")
	out, errDocker := dockerTiene(comandoEmergenza(n, img, env)...)
	e, err := leggiEsitoEmergenza(out)
	if err != nil {
		return esitoEmergenza{}, errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return esitoEmergenza{}, fmt.Errorf("registro d'emergenza nello standalone: %w", errDocker)
	}
	return e, nil
}
