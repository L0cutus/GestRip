-- La coda durevole in uscita (slice 068, CLI-51): la tabella messaggi_in_uscita c'è dalla 0008, con
-- l'indice della coda (stato, prossimo_tentativo_il). Qui l'indice per busta: la lista «📨 Invii
-- registrati» della scheda (slice 067) e l'avviso di ritiro leggono le righe di una busta, e senza indice
-- leggevano tutta la tabella (Dubbio 5 della 067). Lo standalone non ce l'ha: solo del Go, in IndiciSoloGo.
CREATE INDEX messaggi_in_uscita_busta ON messaggi_in_uscita (busta_id);
