package main

// Il passo delle modifiche (slice 033): dopo il registro d'emergenza, nello standalone ancora fermo,
// sei buste nuove con i dati che la cattura sintetica non ha, fatte con le funzioni pubbliche che
// chiamano le pagine (go-baseline): Workshop.create_customer/2 e create_repair/3 come la nuova busta,
// Workshop.update_repair/4 e clear_expected_on/2 con il contesto della scheda (repair_live.ex:
// actor e session_user), Workshop.transition_repair/3 per gli stati, e QuoteDecision.approve/2 e
// reject/3 con il contesto della pagina pubblica (actor «public»). Tutti i dati sono inventati.
// Le buste, in ordine:
//   - 5/26: preventivo e prezzo finale a testo, consegna a testo, note visibili e interne con a capo
//     e caratteri da scappare, giorni in più, nome e telefono del cliente da scappare;
//   - 6/26: prezzi numerici, consegna a data, data d'accettazione cambiata, un artigiano nuovo;
//     pronta;
//   - 7/26: diventa compro oro dalla casella (stato riallineato), preventivo in soli centesimi
//     (solo dal codice), valutata;
//   - 8/26: da diretta a un'oreficeria senza telefono (link revocato, «Nessun recapito per
//     l'oreficeria», telefono del cliente finale come sola anagrafica);
//   - 9/26: cliente senza telefono, QR tolto, consegna tolta con «Togli data», preventivo accettato;
//   - 10/26: oreficeria con telefono (la 2 della cattura), lo stesso QR della 5 (nessuna unicità),
//     preventivo rifiutato.
//
// L'esito è una sola riga GR_MODIFICHE: con ciò che il passo ha scritto, riletto con le letture
// dello standalone; un errore esce con stato 1.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Le buste del passo: le quattro della cattura sono 1–4, queste le seguono.
var (
	busteModifiche     = []int64{5, 6, 7, 8, 9, 10}
	statiModifiche     = []string{"in_lavorazione", "pronta", "valutata", "in_lavorazione", "in_lavorazione", "preventivo"}
	compraOroModifiche = []bool{false, false, true, false, false, false}
)

const modificheStandalone = `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{QuoteDecision, Repo, Workshop}
alias GestripStandalone.Accounts.User
alias GestripStandalone.Workshop.Repair
email = "/run/secrets/admin_email" |> File.read!() |> String.trim_trailing()
audit = %{actor: email}
pubblico = %{actor: "public", ip: "192.0.2.10"}
riletta = fn busta -> Repo.get!(Repair, busta.id) end
nuova = fn nome, telefono, campi ->
  with {:ok, cliente} <- Workshop.create_customer(%{name: nome, phone: telefono}, audit),
       {:ok, busta, _token} <- Workshop.create_repair(cliente,
         Map.merge(%{"accepted_on" => "2026-03-02", "description" => "Lavoro sintetico"}, campi), audit),
       do: {:ok, busta}
end
risultato =
  with %User{} = titolare <- Repo.get_by(User, email: email),
       audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}},
       {:ok, senza_recapito} <- Workshop.create_oreficeria(%{name: "Oreficeria Senza Recapito"}, audit),
       {:ok, artigiano} <- Workshop.create_artisan(%{name: "Incastonatore Sintetico"}, audit),
       {:ok, b5} <- nuova.("Cliente <Sintetico> & 5", "+39 000 <5> & \"5\"", %{"bag_number" => "5/26", "qr_code" => "SINT-QR-0005"}),
       {:ok, _} <- Workshop.update_repair(b5, %{"estimate_euros" => "400/500", "final_price_euros" => "da definire",
         "public_notes" => "Prima riga\nSeconda <b>riga</b> & \"virgolette\"", "internal_notes" => "Nota interna\n\ncon una riga vuota",
         "expected_on_text" => "fine mese", "extra_days" => "3"}, audit_scheda, compro_oro: nil),
       {:ok, b6} <- nuova.("Cliente Sintetico 6", "+390000000006", %{"bag_number" => "6/26", "qr_code" => "SINT-QR-0006"}),
       {:ok, _} <- Workshop.update_repair(b6, %{"estimate_euros" => "120,50", "final_price_euros" => "85", "expected_on" => "2026-04-15",
         "accepted_on" => "2026-03-01", "artisan_id" => Integer.to_string(artigiano.id)}, audit_scheda, compro_oro: nil),
       {:ok, _} <- Workshop.transition_repair(riletta.(b6), "pronta", audit_scheda),
       {:ok, b7} <- nuova.("Cliente Sintetico 7", "+390000000007", %{"bag_number" => "7/26", "qr_code" => "SINT-QR-0007"}),
       {:ok, _} <- Workshop.update_repair(b7, %{"description" => "Bracciale da valutare"}, audit_scheda, compro_oro: true),
       {:ok, _} <- Workshop.update_repair(riletta.(b7), %{"estimate_cents" => 5000}, audit_scheda, compro_oro: nil),
       {:ok, _} <- Workshop.transition_repair(riletta.(b7), "valutata", audit_scheda),
       {:ok, b8} <- nuova.("Cliente Sintetico 8", "333 000 0008", %{"bag_number" => "8/26", "qr_code" => "SINT-QR-0008"}),
       {:ok, _} <- Workshop.update_repair(b8, %{"oreficeria_id" => Integer.to_string(senza_recapito.id)}, audit_scheda, compro_oro: nil),
       {:ok, b9} <- nuova.("Cliente Senza Telefono", "", %{"bag_number" => "9/26", "qr_code" => "SINT-QR-0009", "expected_on" => "2026-05-20"}),
       {:ok, _} <- Workshop.update_repair(b9, %{"qr_code" => "", "estimate_euros" => "60"}, audit_scheda, compro_oro: nil),
       {:ok, _} <- Workshop.clear_expected_on(riletta.(b9), audit_scheda),
       {:ok, _} <- Workshop.transition_repair(riletta.(b9), "preventivo", audit_scheda),
       {:ok, _} <- QuoteDecision.approve(riletta.(b9), pubblico),
       {:ok, b10} <- nuova.("Cliente Sintetico 10", "+390000000010", %{"bag_number" => "10/26", "qr_code" => "SINT-QR-0005", "oreficeria_id" => "2"}),
       {:ok, _} <- Workshop.transition_repair(riletta.(b10), "preventivo", audit_scheda),
       {:ok, _} <- QuoteDecision.reject(riletta.(b10), "Troppo caro, prova sintetica", pubblico) do
    buste = Enum.map([b5, b6, b7, b8, b9, b10], riletta)
    revocate = Enum.count(Workshop.list_audit_events(), &(&1.action == "repair.public_link_revoked" and
      &1.metadata == %{"reason" => "counterparty_changed"}))
    %{esito: "ok", buste: Enum.map(buste, & &1.id), stati: Enum.map(buste, & &1.status),
      compro_oro: Enum.map(buste, & &1.compro_oro), link_revocati: revocate,
      accettato: not is_nil(Enum.at(buste, 4).quote_approved_at), rifiutato: not is_nil(Enum.at(buste, 5).quote_rejected_at),
      totale_buste: Repo.aggregate(Repair, :count)}
  else
    errore -> %{esito: "errore", motivo: inspect(errore)}
  end
IO.puts("GR_MODIFICHE:" <> Jason.encode!(risultato))
if risultato.esito != "ok", do: System.halt(1)`

// comandoModifiche gira come il legacy import, la vendita e il registro: stessa immagine, senza
// rete, stesse protezioni e stessi dati dell'istanza, e nient'altro.
func comandoModifiche(n nomi, img string, env []string) []string {
	a := append(passoSenzaRete(n, passoModifiche), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", modificheStandalone)
}

// esitoModifiche è la riga GR_MODIFICHE: del passo delle modifiche.
type esitoModifiche struct {
	Esito        string   `json:"esito"`
	Motivo       string   `json:"motivo"`
	Buste        []int64  `json:"buste"`
	Stati        []string `json:"stati"`
	CompraOro    []bool   `json:"compro_oro"`
	LinkRevocati int64    `json:"link_revocati"`
	Accettato    bool     `json:"accettato"`
	Rifiutato    bool     `json:"rifiutato"`
	TotaleBuste  int64    `json:"totale_buste"`
}

// leggiEsitoModifiche vuole una sola riga GR_MODIFICHE: riuscita, senza campi in più: le sei buste
// con gli id, gli stati e i tipi attesi (gli scenari le cercano per id), un solo link revocato per
// il cambio di controparte, il preventivo accettato e quello rifiutato, e le buste in tutto (le
// quattro della cattura più queste).
func leggiEsitoModifiche(out string) (esitoModifiche, error) {
	righe := righeMarcatore(out, "GR_MODIFICHE:")
	if len(righe) != 1 {
		return esitoModifiche{}, fmt.Errorf("modifiche nello standalone: %d righe GR_MODIFICHE, ne serve una", len(righe))
	}
	var e esitoModifiche
	dec := json.NewDecoder(strings.NewReader(righe[0]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return esitoModifiche{}, fmt.Errorf("modifiche nello standalone: riga GR_MODIFICHE non valida: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return esitoModifiche{}, errors.New("modifiche nello standalone: dati dopo la riga GR_MODIFICHE")
	}
	switch {
	case e.Esito != "ok":
		return esitoModifiche{}, fmt.Errorf("modifiche nello standalone: esito %q: %s", e.Esito, e.Motivo)
	case !slices.Equal(e.Buste, busteModifiche):
		return esitoModifiche{}, fmt.Errorf("modifiche nello standalone: buste %v, attese %v", e.Buste, busteModifiche)
	case !slices.Equal(e.Stati, statiModifiche) || !slices.Equal(e.CompraOro, compraOroModifiche):
		return esitoModifiche{}, fmt.Errorf("modifiche nello standalone: stati %v compro oro %v, attesi %v %v", e.Stati, e.CompraOro,
			statiModifiche, compraOroModifiche)
	case e.LinkRevocati != 1 || !e.Accettato || !e.Rifiutato:
		return esitoModifiche{}, fmt.Errorf("modifiche nello standalone: %d link revocati, accettato %v, rifiutato %v",
			e.LinkRevocati, e.Accettato, e.Rifiutato)
	case e.TotaleBuste != int64(len(busteModifiche))+busteModifiche[0]-1:
		return esitoModifiche{}, fmt.Errorf("modifiche nello standalone: %d buste in tutto, attese %d", e.TotaleBuste,
			int64(len(busteModifiche))+busteModifiche[0]-1)
	}
	return e, nil
}

// modificheBuste fa il passo delle modifiche e ne dà l'esito controllato.
func modificheBuste(n nomi, img string, env []string) (esitoModifiche, error) {
	fmt.Println("catena: buste modificate nello standalone (prezzi, consegna, note, tipo, oreficeria, artigiano, QR, risposte)…")
	out, errDocker := dockerTiene(comandoModifiche(n, img, env)...)
	e, err := leggiEsitoModifiche(out)
	if err != nil {
		return esitoModifiche{}, errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return esitoModifiche{}, fmt.Errorf("modifiche nello standalone: %w", errDocker)
	}
	return e, nil
}

// controllaModifiche vuole nel report dell'import Go tutte le buste dello standalone lette e
// scritte, quelle del passo comprese. Senza il passo (nessuna busta contata) non controlla niente.
func controllaModifiche(lette map[string]int64, scritte map[string]int64, e esitoModifiche) error {
	if e.TotaleBuste == 0 {
		return nil
	}
	if lette["repairs"] != e.TotaleBuste || scritte["buste"] != e.TotaleBuste {
		return fmt.Errorf("report dell'import Go: repairs %d righe lette, buste %d scritte; nello standalone %d",
			lette["repairs"], scritte["buste"], e.TotaleBuste)
	}
	return nil
}
