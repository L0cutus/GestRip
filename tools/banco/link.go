package main

// Il passo del link pubblico (slice 035): dopo gli stati, nello standalone ancora fermo, i due
// comandi del riquadro «🔗 Link» della scheda con le funzioni che chiamano gli eventi revoke-link e
// regenerate-link (go-baseline): Workshop.revoke_public_access/2 e regenerate_public_link/2, con il
// contesto della scheda (actor e session_user); le buste nuove con Workshop.create_customer/2 e
// create_repair/3 come la nuova busta. Tutti i dati sono inventati. In ordine:
//   - 1/26 (della cattura, link scaduto con la sola impronta, vecchio link con codice vivo): nuovo
//     link, che revoca per sempre il vecchio link con codice;
//   - 3/26 (della cattura, come la 1/26): revoca, che revoca anche il vecchio link con codice;
//   - 16/26: nata qui, revocata;
//   - 17/26: nata qui, dell'oreficeria 2 della cattura, nuovo link;
//   - 18/26: nata qui, revocata e poi con un nuovo link (la revoca si toglie).
//
// I token non escono mai dallo script: l'esito dice solo lo stato del link (public_link_status/1) e
// del vecchio link (legacy_link_status/1) delle buste rilette, e quante righe d'audit hanno scritto i
// due comandi. Un link scaduto, uno con la sola impronta ancora valido, «inviato il» e i canali
// Telegram ed email tagliati non li fa nessuna funzione delle pagine su questi dati: li provano i
// test Go. Le revoche e i nuovi link nel Go non si confrontano via HTTP (nello standalone sono eventi
// LiveView): il banco confronta le schede che ne escono.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Le buste del passo, in ordine, e quello che lo standalone deve dirne dopo.
var (
	busteLink      = []int64{1, 3, 16, 17, 18}
	statiLink      = []string{"usable", "revoked", "revoked", "usable", "usable"}
	storiciLink    = []string{"revoked", "revoked", "none", "none", "none"}
	totaleBusteLnk = int64(18)
)

const linkStandalone = `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{Repo, Workshop}
alias GestripStandalone.Accounts.User
alias GestripStandalone.Workshop.Repair
email = "/run/secrets/admin_email" |> File.read!() |> String.trim_trailing()
audit = %{actor: email}
riletta = fn busta -> Repo.get!(Repair, busta.id) end
nuova = fn nome, telefono, campi ->
  with {:ok, cliente} <- Workshop.create_customer(%{name: nome, phone: telefono}, audit),
       {:ok, busta, _token} <- Workshop.create_repair(cliente,
         Map.merge(%{"accepted_on" => "2026-03-02", "description" => "Lavoro sintetico"}, campi), audit),
       do: {:ok, busta}
end
nuovo_link = fn busta, contesto ->
  case Workshop.regenerate_public_link(riletta.(busta), contesto) do
    {:ok, rinnovata, _token} -> {:ok, rinnovata}
    errore -> errore
  end
end
revoca = fn busta, contesto -> Workshop.revoke_public_access(riletta.(busta), contesto) end
risultato =
  with %User{} = titolare <- Repo.get_by(User, email: email),
       audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}},
       %Repair{} = b1 <- Repo.get_by(Repair, bag_number: "1/26"),
       %Repair{} = b3 <- Repo.get_by(Repair, bag_number: "3/26"),
       {:ok, _} <- nuovo_link.(b1, audit_scheda),
       {:ok, _} <- revoca.(b3, audit_scheda),
       {:ok, b16} <- nuova.("Cliente Sintetico 16", "+390000000016", %{"bag_number" => "16/26", "qr_code" => "SINT-QR-0016"}),
       {:ok, _} <- revoca.(b16, audit_scheda),
       {:ok, b17} <- nuova.("Cliente Sintetico 17", "+390000000017", %{"bag_number" => "17/26", "qr_code" => "SINT-QR-0017", "oreficeria_id" => "2"}),
       {:ok, _} <- nuovo_link.(b17, audit_scheda),
       {:ok, b18} <- nuova.("Cliente Sintetico 18", "+390000000018", %{"bag_number" => "18/26", "qr_code" => "SINT-QR-0018"}),
       {:ok, _} <- revoca.(b18, audit_scheda),
       {:ok, _} <- nuovo_link.(b18, audit_scheda) do
    buste = Enum.map([b1, b3, b16, b17, b18], riletta)
    eventi = Workshop.list_audit_events()
    emessi = Enum.filter(eventi, &(&1.action == "repair.public_link_issued" and &1.metadata["reason"] == "manual"))
    revocati = Enum.filter(eventi, &(&1.action == "repair.public_link_revoked" and not Map.has_key?(&1.metadata, "reason")))
    %{esito: "ok", buste: Enum.map(buste, & &1.id),
      link: Enum.map(buste, &(&1 |> Workshop.public_link_status() |> elem(0) |> Atom.to_string())),
      storico: Enum.map(buste, &(&1 |> Workshop.legacy_link_status() |> Atom.to_string())),
      emessi: length(emessi), emessi_storico: Enum.count(emessi, &(&1.metadata["legacy"] == true)),
      revocati: length(revocati), revocati_storico: Enum.count(revocati, &(&1.metadata["legacy"] == true)),
      totale_buste: Repo.aggregate(Repair, :count)}
  else
    errore -> %{esito: "errore", motivo: inspect(errore)}
  end
IO.puts("GR_LINK:" <> Jason.encode!(risultato))
if risultato.esito != "ok", do: System.halt(1)`

// comandoLink gira come gli stati: stessa immagine, senza rete, stesse protezioni e stessi dati
// dell'istanza, e nient'altro.
func comandoLink(n nomi, img string, env []string) []string {
	a := append(passoSenzaRete(n, passoLink), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", linkStandalone)
}

// esitoLink è la riga GR_LINK: del passo del link.
type esitoLink struct {
	Esito           string   `json:"esito"`
	Motivo          string   `json:"motivo"`
	Buste           []int64  `json:"buste"`
	Link            []string `json:"link"`
	Storico         []string `json:"storico"`
	Emessi          int64    `json:"emessi"`
	EmessiStorico   int64    `json:"emessi_storico"`
	Revocati        int64    `json:"revocati"`
	RevocatiStorico int64    `json:"revocati_storico"`
	TotaleBuste     int64    `json:"totale_buste"`
}

// leggiEsitoLink vuole una sola riga GR_LINK: riuscita, senza campi in più: le cinque buste con lo
// stato del link e del vecchio link attesi, tre nuovi link manuali (uno con il vecchio link vivo),
// tre revoche dei comandi (una con il vecchio link vivo; la revoca per cambio di controparte delle
// modifiche ha il motivo e non conta) e le buste in tutto.
func leggiEsitoLink(out string) (esitoLink, error) {
	righe := righeMarcatore(out, "GR_LINK:")
	if len(righe) != 1 {
		return esitoLink{}, fmt.Errorf("link nello standalone: %d righe GR_LINK, ne serve una", len(righe))
	}
	var e esitoLink
	dec := json.NewDecoder(strings.NewReader(righe[0]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return esitoLink{}, fmt.Errorf("link nello standalone: riga GR_LINK non valida: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return esitoLink{}, errors.New("link nello standalone: dati dopo la riga GR_LINK")
	}
	switch {
	case e.Esito != "ok":
		return esitoLink{}, fmt.Errorf("link nello standalone: esito %q: %s", e.Esito, e.Motivo)
	case !slices.Equal(e.Buste, busteLink):
		return esitoLink{}, fmt.Errorf("link nello standalone: buste %v, attese %v", e.Buste, busteLink)
	case !slices.Equal(e.Link, statiLink) || !slices.Equal(e.Storico, storiciLink):
		return esitoLink{}, fmt.Errorf("link nello standalone: link %v vecchi link %v, attesi %v %v", e.Link, e.Storico, statiLink, storiciLink)
	case e.Emessi != 3 || e.EmessiStorico != 1 || e.Revocati != 3 || e.RevocatiStorico != 1:
		return esitoLink{}, fmt.Errorf("link nello standalone: %d nuovi link (%d con il vecchio link), %d revoche (%d con il vecchio link)",
			e.Emessi, e.EmessiStorico, e.Revocati, e.RevocatiStorico)
	case e.TotaleBuste != totaleBusteLnk:
		return esitoLink{}, fmt.Errorf("link nello standalone: %d buste in tutto, attese %d", e.TotaleBuste, totaleBusteLnk)
	}
	return e, nil
}

// linkBuste fa il passo del link e ne dà l'esito controllato.
func linkBuste(n nomi, img string, env []string) (esitoLink, error) {
	fmt.Println("catena: revoche e nuovi link della scheda nello standalone…")
	out, errDocker := dockerTiene(comandoLink(n, img, env)...)
	e, err := leggiEsitoLink(out)
	if err != nil {
		return esitoLink{}, errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return esitoLink{}, fmt.Errorf("link nello standalone: %w", errDocker)
	}
	return e, nil
}
