package main

// Il passo dei token della pagina del cliente (slice 061): per ultimo, nello standalone ancora
// fermo e prima della copia del database, cinque buste ricevono un token **inventato e noto**,
// salvato come lo salva lo standalone (impronta SHA-256 del testo, hash_token/1, e il testo per i
// link vivi), così gli scenari del banco possono aprire /r/<token> sulle due istanze. Il Go li riceve
// con l'import, come i link già mandati ai clienti: il banco prova anche che un token emesso dallo
// standalone continua a funzionare nel Go. La scadenza e la revoca restano quelle che le buste hanno
// già, quindi le schede non cambiano. È l'unico passo che scrive con Repo.update_all: nessuna
// funzione dello standalone emette un token scelto da fuori.
//   - 21/26 (della home, consegnata, descrizione con <, & e "): link vivo;
//   - 23/26 (della home, etichetta RIP-2026-0046): link vivo;
//   - 17/26 (del passo del link, dell'oreficeria 2): link vivo;
//   - 3/26 (revocata dal passo del link): solo l'impronta, resta revocata;
//   - 2/26 (della cattura, scaduta con la sola impronta): solo l'impronta, resta scaduta;
//   - dalla slice 062: 1/26 (della cattura, nuovo link dal passo del link, con le foto: due della
//     cattura e quella vera del passo delle foto), 9/26 (delle modifiche, preventivo approvato e
//     passata in lavorazione) e 10/26 (delle modifiche, dell'oreficeria 2, in preventivo e
//     rifiutata con un motivo): link vivi, per le foto e il preventivo della pagina del cliente;
//   - dalla slice 063: 27/26 (della home, con un messaggio del cliente non letto) e 28/26 (della home,
//     con un messaggio del negozio): link vivi, per la conversazione della pagina del cliente. I
//     messaggi che lo scenario scrive vanno sulla 27/26, già non letta e già con messaggi: i
//     contatori della home non cambiano.
//
// Lo script controlla con Workshop.get_public_repair/1 che i tre vivi aprano la loro busta e che gli
// altri due no, e che lo stato del link della scheda non cambi.
//
// Dalla slice 064 lo stesso passo dà il codice breve storico (?pw=) a due buste della cattura Flask,
// come lo scrive il legacy import (LegacyPublicLinks.repair_code_digest/2 sul numero salvato): 4/26
// (aperta, diretta, il link storico vivo) e 3/26 (revocata dal passo del link). La 1/26, con le foto,
// ha il link storico revocato dal nuovo link del passo del link. Le due buste vengono dalla cattura, quindi la
// scheda già dice che hanno una credenziale storica (legacy_credential?/2): niente cambia nelle
// pagine degli altri scenari. Lo script controlla con verify_repair_code/3 che il codice passi.

import (
	"errors"
	"fmt"
	"strings"
)

const passoToken = "token"

// tokenBanco sono i token inventati, per busta; gli scenari li scrivono nei percorsi.
var tokenBanco = []struct{ numero, token, stato string }{
	{"21/26", "SINTETICO-banco-r-diretta-21-xxxxxxxxxxxxxx", "usable"},
	{"23/26", "SINTETICO-banco-r-diretta-23-xxxxxxxxxxxxxx", "usable"},
	{"17/26", "SINTETICO-banco-r-oreficeria-17-xxxxxxxxxxx", "usable"},
	{"3/26", "SINTETICO-banco-r-revocata-3-xxxxxxxxxxxxxx", "revoked"},
	{"2/26", "SINTETICO-banco-r-scaduta-2-xxxxxxxxxxxxxxx", "expired"},
	{"1/26", "SINTETICO-banco-r-foto-1-xxxxxxxxxxxxxxxxxx", "usable"},
	{"9/26", "SINTETICO-banco-r-approvata-9-xxxxxxxxxxxxx", "usable"},
	{"10/26", "SINTETICO-banco-r-rifiutata-10-xxxxxxxxxxxx", "usable"},
	{"27/26", "SINTETICO-banco-r-messaggi-27-xxxxxxxxxxxxx", "usable"},
	{"28/26", "SINTETICO-banco-r-negozio-28-xxxxxxxxxxxxxx", "usable"},
}

// codiciStoriciBanco sono i codici brevi inventati dei link storici /stato, per busta.
var codiciStoriciBanco = []struct{ numero, codice string }{
	{"4/26", "SINT-4"},
	{"3/26", "SINT-3"},
}

// scriptToken è lo script del passo, con le buste e i token di tokenBanco.
func scriptToken() string {
	var b strings.Builder
	b.WriteString(`GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
import Ecto.Query
alias GestripStandalone.{Repo, Workshop}
alias GestripStandalone.Workshop.Repair
stato = fn r -> r |> Workshop.public_link_status() |> elem(0) |> Atom.to_string() end
imposta = fn numero, token, atteso ->
  with %Repair{} = r <- Repo.get_by(Repair, bag_number: numero),
       ^atteso <- stato.(r),
       campi = if(atteso == "usable", do: [public_token: token, public_token_hash: :crypto.hash(:sha256, token)],
         else: [public_token_hash: :crypto.hash(:sha256, token)]),
       {1, _} <- Repo.update_all(from(x in Repair, where: x.id == ^r.id), set: campi),
       riletta = Repo.get!(Repair, r.id),
       ^atteso <- stato.(riletta),
       trovata = Workshop.get_public_repair(token),
       rid = r.id,
       true <- if(atteso == "usable", do: match?(%Repair{id: ^rid}, trovata), else: is_nil(trovata)) do
    :ok
  else
    errore -> {:errore, numero, inspect(errore)}
  end
end
risultati = [
`)
	for i, t := range tokenBanco {
		sep := ","
		if i == len(tokenBanco)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "  imposta.(%q, %q, %q)%s\n", t.numero, t.token, t.stato, sep)
	}
	b.WriteString(`]
codice = fn numero, testo ->
  with %Repair{} = r <- Repo.get_by(Repair, bag_number: numero),
       stato_prima = stato.(r),
       impronta = GestripStandalone.LegacyPublicLinks.repair_code_digest(r.bag_number, testo),
       {1, _} <- Repo.update_all(from(x in Repair, where: x.id == ^r.id), set: [legacy_public_code_hash: impronta]),
       riletta = Repo.get!(Repair, r.id),
       ^stato_prima <- stato.(riletta),
       true <- GestripStandalone.LegacyPublicLinks.verify_repair_code(riletta.bag_number, " " <> String.downcase(testo) <> " ", riletta.legacy_public_code_hash) do
    :ok
  else
    errore -> {:errore, numero, inspect(errore)}
  end
end
risultati = risultati ++ [
`)
	for i, c := range codiciStoriciBanco {
		sep := ","
		if i == len(codiciStoriciBanco)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "  codice.(%q, %q)%s\n", c.numero, c.codice, sep)
	}
	b.WriteString(`]
case Enum.reject(risultati, &(&1 == :ok)) do
  [] -> IO.puts("GR_TOKEN:ok")
  errori -> IO.puts("GR_TOKEN:errore " <> inspect(errori)); System.halt(1)
end`)
	return b.String()
}

// comandoToken gira come gli altri passi: stessa immagine, senza rete, stesse protezioni e stessi
// dati dell'istanza, e nient'altro.
func comandoToken(n nomi, img string, env []string) []string {
	a := append(passoSenzaRete(n, passoToken), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", scriptToken())
}

// leggiEsitoToken vuole una sola riga GR_TOKEN:, riuscita.
func leggiEsitoToken(out string) error {
	righe := righeMarcatore(out, "GR_TOKEN:")
	switch {
	case len(righe) != 1:
		return fmt.Errorf("token nello standalone: %d righe GR_TOKEN, ne serve una", len(righe))
	case righe[0] != "ok":
		return fmt.Errorf("token nello standalone: %s", righe[0])
	}
	return nil
}

// tokenBuste fa il passo dei token e ne controlla l'esito.
func tokenBuste(n nomi, img string, env []string) error {
	fmt.Println("catena: token inventati della pagina del cliente nello standalone…")
	out, errDocker := dockerTiene(comandoToken(n, img, env)...)
	if err := leggiEsitoToken(out); err != nil {
		return errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return fmt.Errorf("token nello standalone: %w", errDocker)
	}
	return nil
}
