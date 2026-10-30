-- Segreti dei provider (D-03, OPS-02): le chiavi segrete di `settings` dello standalone
-- (outgoing.smtp.password, outgoing.telegram.bot_token, assist.ai.api_key, assist.stt.api_key: in
-- Outgoing.Config e Assist.Config a go-baseline), a parte dalle impostazioni come la chiave del
-- server. Un export, una copia per prove o una pagina d'elenco non le portano (db.TabelleSegrete);
-- il backup sì (D-10). Il nome resta la chiave dello standalone (importer.ChiaviSegreteProvider).
-- L'import le porta con email e Telegram spenti finché il titolare non li accende (D-03):
-- l'interruttore non sta qui, sta con i canali.
CREATE TABLE segreti_provider (
  nome          TEXT PRIMARY KEY CHECK (length(nome) > 0),
  valore        TEXT NOT NULL,
  aggiornato_il TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;
