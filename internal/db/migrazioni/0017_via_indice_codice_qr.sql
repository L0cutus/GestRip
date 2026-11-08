-- Via l'indice buste_codice_qr della 0016 (slice 057). La scansione dalla home lo usava per il codice
-- QR uguale esatto (slice 052); dalla slice 055d ogni lettura del codice (scansione, gemelle, ricerca
-- per numero d'etichetta, ricerca scritta, «senza QR») confronta il codice normalizzato, con instr,
-- LIKE o trim, e nessuna query lo usa più (EXPLAIN QUERY PLAN nella ricevuta della 057): costava una
-- scrittura in più a ogni busta salvata. Lo standalone non l'ha mai avuto: con questa migrazione gli
-- indici di buste tornano quelli dello standalone (IndiciSoloGo non nomina più buste).
DROP INDEX buste_codice_qr;
